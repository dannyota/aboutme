// Package printrender renders private resume snapshots with a pinned Chromium.
package printrender

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/directrender"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
)

const expectedBrowserVersion = "151.0.7922.34"

var (
	// ErrInvalidConfig reports an unusable browser controller configuration.
	ErrInvalidConfig = errors.New("printrender: invalid configuration")
	// ErrUnavailable reports that the configured browser cannot start safely.
	ErrUnavailable = errors.New("printrender: unavailable")
	// ErrRenderFailed reports a closed browser navigation or capture failure.
	ErrRenderFailed = errors.New("printrender: render failed")
	// ErrOutputTooLarge reports an artifact above its fixed format limit.
	ErrOutputTooLarge = errors.New("printrender: output too large")
)

// Config supplies the pinned browser and validated direct Nuxt listener.
type Config struct {
	BrowserExecutable string
	RenderOrigin      directrender.RenderOrigin
	Logger            *slog.Logger

	testHooks         *runtimeHooks
	testForwardOrigin string
}

type runtimeHooks struct {
	euid             func() int
	sandboxSupported func() bool
	version          func(context.Context, string) (string, error)
	ready            func(context.Context, *Renderer) error
	attempt          func(context.Context, *Renderer, renderjob.Navigation) ([]byte, error)
	supervisor       supervisorCommand
	now              func() time.Time
}

// Renderer owns a fresh browser and closed network authority for each render.
type Renderer struct {
	executable    string
	origin        string
	forwardOrigin string
	hooks         *runtimeHooks
	supervisor    supervisorCommand
	logger        *slog.Logger
	browserOnce   sync.Once
	browserPermit chan struct{}
	readyMu       sync.Mutex
	readyOK       bool
	readyErr      error
	readyAfter    time.Time
	render        func(context.Context, renderjob.Navigation) ([]byte, error)
}

// New validates the fixed browser and direct render origin.
func New(config Config) (*Renderer, error) {
	if config.RenderOrigin.String() == "" {
		return nil, initializationFailure(ErrInvalidConfig, "configuration", "render origin is missing")
	}
	if config.BrowserExecutable == "" {
		return nil, initializationFailure(ErrInvalidConfig, "binary-lookup", "browser executable is missing")
	}
	executable, err := filepath.Abs(config.BrowserExecutable)
	if err != nil || executable != config.BrowserExecutable {
		return nil, initializationFailure(ErrInvalidConfig, "binary-lookup", "browser path must be absolute")
	}
	info, err := os.Stat(executable)
	if err != nil {
		return nil, diagnoseInitialization(context.Background(), ErrInvalidConfig, "binary-lookup", err, nil, 0)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return nil, initializationFailure(ErrInvalidConfig, "binary-lookup", "browser must be a regular executable file")
	}
	hooks := config.testHooks
	if hooks == nil {
		hooks = defaultHooks()
	}
	if hooks.euid == nil || hooks.sandboxSupported == nil || hooks.version == nil {
		return nil, initializationFailure(ErrInvalidConfig, "configuration", "runtime checks are missing")
	}
	if hooks.euid() == 0 {
		return nil, initializationFailure(ErrInvalidConfig, "sandbox", "browser must run as a non-root user")
	}
	if !hooks.sandboxSupported() {
		return nil, initializationFailure(ErrInvalidConfig, "sandbox", "user namespaces are unavailable")
	}
	started := time.Now()
	checkCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	version, versionErr := hooks.version(checkCtx, executable)
	if versionErr != nil || checkCtx.Err() != nil {
		return nil, diagnoseInitialization(checkCtx, ErrInvalidConfig, "version-probe", versionErr, nil, time.Since(started))
	}
	if !hasExactBrowserVersion(version) {
		failure := initializationFailure(ErrInvalidConfig, "version-probe", "browser version mismatch")
		failure.exitStatus, failure.elapsed = 0, time.Since(started)
		return nil, failure
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	renderer := &Renderer{
		executable:    executable,
		origin:        config.RenderOrigin.String(),
		forwardOrigin: config.testForwardOrigin,
		hooks:         hooks,
		supervisor:    hooks.supervisor,
		logger:        logger,
	}
	renderer.render = func(ctx context.Context, navigation renderjob.Navigation) ([]byte, error) {
		if hooks.attempt != nil {
			return hooks.attempt(ctx, renderer, navigation)
		}
		return renderer.renderAttempt(ctx, navigation)
	}
	return renderer, nil
}

// Ready caches a successful probe and rechecks failures after a cooldown.
func (r *Renderer) Ready() error {
	if r == nil || r.hooks == nil {
		return ErrUnavailable
	}
	r.readyMu.Lock()
	defer r.readyMu.Unlock()
	if r.readyOK {
		return nil
	}
	now := time.Now
	if r.hooks.now != nil {
		now = r.hooks.now
	}
	if r.readyErr != nil && now().Before(r.readyAfter) {
		return ErrUnavailable
	}
	// A readiness probe must not overlap a render's browser or joined teardown.
	gate := r.browserGate()
	select {
	case gate <- struct{}{}:
	default:
		return ErrUnavailable
	}
	defer func() { <-gate }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	probe := r.hooks.ready
	if probe == nil {
		probe = readyBrowser
	}
	started := time.Now()
	err := probe(ctx, r)
	if err != nil || ctx.Err() != nil {
		r.readyErr = diagnoseInitialization(ctx, ErrUnavailable, "launch", err, nil, time.Since(started))
		r.readyAfter = now().Add(5 * time.Second)
		r.logger.Error("private browser renderer initialization failed", "error", r.readyErr)
		return ErrUnavailable
	}
	if r.readyErr != nil {
		r.logger.Info("private browser renderer initialization recovered")
	}
	r.readyOK, r.readyErr = true, nil
	return nil
}

// Render performs one controlled navigation and returns only validated bytes.
func (r *Renderer) Render(ctx context.Context, navigation renderjob.Navigation) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.render == nil || !validNavigation(navigation) {
		return nil, ErrRenderFailed
	}
	switch navigation.Format {
	case renderjob.PDF, renderjob.PNG, renderjob.Card:
	default:
		return nil, ErrRenderFailed
	}
	gate := r.browserGate()
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-gate }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	output, err := r.render(ctx, navigation)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if err != nil {
		if errors.Is(err, ErrOutputTooLarge) {
			return nil, ErrOutputTooLarge
		}
		return nil, ErrRenderFailed
	}
	return output, nil
}

func (r *Renderer) browserGate() chan struct{} {
	r.browserOnce.Do(func() { r.browserPermit = make(chan struct{}, 1) })
	return r.browserPermit
}

func validNavigation(navigation renderjob.Navigation) bool {
	if navigation.ResumeID.String() == "00000000-0000-0000-0000-000000000000" || navigation.JobID.String() == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	encoding := base64.RawURLEncoding.Strict()
	authority, err := encoding.DecodeString(navigation.Capability)
	return err == nil && len(authority) == 32 && encoding.EncodeToString(authority) == navigation.Capability
}

func defaultHooks() *runtimeHooks {
	serverExecutable, err := os.Executable()
	supervisorPath := ""
	if err == nil {
		supervisorPath = filepath.Join(filepath.Dir(serverExecutable), "render-browser-supervisor")
	}
	return defaultHooksWithSupervisor(supervisorCommand{path: supervisorPath})
}

func defaultHooksWithSupervisor(supervisor supervisorCommand) *runtimeHooks {
	return &runtimeHooks{
		euid:             os.Geteuid,
		sandboxSupported: linuxSandboxSupported,
		supervisor:       supervisor,
		version: func(ctx context.Context, executable string) (string, error) {
			return probeBrowserVersion(ctx, executable, supervisor)
		},
	}
}

func hasExactBrowserVersion(output string) bool {
	fields := strings.Fields(output)
	return len(fields) > 0 && fields[len(fields)-1] == expectedBrowserVersion
}

func linuxSandboxSupported() bool {
	if runtime.GOOS != "linux" || os.Geteuid() == 0 {
		return false
	}
	value, err := os.ReadFile("/proc/sys/user/max_user_namespaces")
	if err != nil {
		return false
	}
	count, err := strconv.ParseUint(strings.TrimSpace(string(value)), 10, 64)
	return err == nil && count > 0
}
