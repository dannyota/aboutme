// Package printrender renders private resume snapshots with a pinned Chromium.
package printrender

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
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

	testHooks         *runtimeHooks
	testForwardOrigin string
}

type runtimeHooks struct {
	euid             func() int
	sandboxSupported func() bool
	version          func(context.Context, string) (string, error)
	ready            func(context.Context, *Renderer) error
	attempt          func(context.Context, *Renderer, renderjob.Navigation) ([]byte, error)
}

// Renderer owns a fresh browser and closed network authority for each render.
type Renderer struct {
	executable    string
	origin        string
	forwardOrigin string
	hooks         *runtimeHooks
	readyOnce     sync.Once
	readyErr      error
	render        func(context.Context, renderjob.Navigation) ([]byte, error)
}

// New validates the fixed browser and direct render origin.
func New(config Config) (*Renderer, error) {
	if config.BrowserExecutable == "" || config.RenderOrigin.String() == "" {
		return nil, ErrInvalidConfig
	}
	executable, err := filepath.Abs(config.BrowserExecutable)
	if err != nil || executable != config.BrowserExecutable {
		return nil, ErrInvalidConfig
	}
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return nil, ErrInvalidConfig
	}
	hooks := config.testHooks
	if hooks == nil {
		hooks = defaultHooks()
	}
	if hooks.euid == nil || hooks.euid() == 0 || hooks.sandboxSupported == nil || !hooks.sandboxSupported() || hooks.version == nil {
		return nil, ErrInvalidConfig
	}
	checkCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	version, versionErr := hooks.version(checkCtx, executable)
	cancel()
	if versionErr != nil || !hasExactBrowserVersion(version) {
		return nil, ErrInvalidConfig
	}
	renderer := &Renderer{
		executable:    executable,
		origin:        config.RenderOrigin.String(),
		forwardOrigin: config.testForwardOrigin,
		hooks:         hooks,
	}
	renderer.render = func(ctx context.Context, navigation renderjob.Navigation) ([]byte, error) {
		if hooks.attempt != nil {
			return hooks.attempt(ctx, renderer, navigation)
		}
		return renderer.renderAttempt(ctx, navigation)
	}
	return renderer, nil
}

// Ready runs at most one controlled browser startup probe and caches the result.
func (r *Renderer) Ready() error {
	if r == nil || r.hooks == nil {
		return ErrUnavailable
	}
	r.readyOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		probe := r.hooks.ready
		if probe == nil {
			probe = readyBrowser
		}
		if err := probe(ctx, r); err != nil {
			r.readyErr = ErrUnavailable
		}
	})
	return r.readyErr
}

// Render performs one controlled navigation and returns only validated bytes.
func (r *Renderer) Render(ctx context.Context, navigation renderjob.Navigation) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.render == nil || !validNavigation(navigation) {
		return nil, ErrRenderFailed
	}
	if navigation.Format != renderjob.PDF && navigation.Format != renderjob.PNG {
		return nil, ErrRenderFailed
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

func validNavigation(navigation renderjob.Navigation) bool {
	if navigation.ResumeID.String() == "00000000-0000-0000-0000-000000000000" || navigation.JobID.String() == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	encoding := base64.RawURLEncoding.Strict()
	authority, err := encoding.DecodeString(navigation.Capability)
	return err == nil && len(authority) == 32 && encoding.EncodeToString(authority) == navigation.Capability
}

func defaultHooks() *runtimeHooks {
	return &runtimeHooks{
		euid:             os.Geteuid,
		sandboxSupported: linuxSandboxSupported,
		version: func(ctx context.Context, executable string) (string, error) {
			arguments := controlledBrowserArguments(executable, "--version")
			cmd := exec.CommandContext(ctx, browserEnvironmentExecutable, arguments...)
			cmd.Env = make([]string, 0)
			output, err := cmd.Output()
			return strings.TrimSpace(string(output)), err
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
