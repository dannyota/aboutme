package printrender

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
	"golang.org/x/sys/unix"
)

const (
	browserEnvironmentExecutable = "/usr/bin/env"
	browserTimezone              = "TZ=UTC"
	browserLanguage              = "LANG=C.UTF-8"
	browserLocale                = "LC_ALL=C.UTF-8"
	browserProfilePrefix         = "printrender-profile-"
)

func browserEnvironment() []string {
	return []string{
		browserTimezone,
		browserLanguage,
		browserLocale,
	}
}

func browserFlags(proxyURL string) map[string]any {
	return map[string]any{
		"headless":                                           true,
		"no-sandbox":                                         false,
		"no-first-run":                                       true,
		"no-default-browser-check":                           true,
		"disable-background-networking":                      true,
		"disable-background-timer-throttling":                true,
		"disable-backgrounding-occluded-windows":             true,
		"disable-breakpad":                                   true,
		"disable-client-side-phishing-detection":             true,
		"disable-component-extensions-with-background-pages": true,
		"disable-component-update":                           true,
		"disable-crash-reporter":                             true,
		"disable-default-apps":                               true,
		"disable-domain-reliability":                         true,
		"disable-extensions":                                 true,
		"disable-features":                                   "AutofillServerCommunication,CertificateTransparencyComponentUpdater,IdleDetection,InterestFeedContentSuggestions,MediaRouter,OptimizationHints,PreconnectToSearch,Prerender2,PrivacySandboxSettings4,SpeculationRulesPrefetchProxy,Translate",
		"disable-gpu":                                        true,
		"disable-lcd-text":                                   true,
		"disable-renderer-backgrounding":                     true,
		"disable-search-engine-choice-screen":                true,
		"disable-sync":                                       true,
		"dns-prefetch-disable":                               true,
		"font-render-hinting":                                "none",
		"force-color-profile":                                "srgb",
		"force-device-scale-factor":                          "1",
		"hide-scrollbars":                                    true,
		"metrics-recording-only":                             true,
		"mute-audio":                                         true,
		"proxy-bypass-list":                                  "<-loopback>",
		"proxy-server":                                       proxyURL,
		"safebrowsing-disable-auto-update":                   true,
	}
}

func allocatorOptions(executable, proxyURL, profile string, configure func(*exec.Cmd)) []chromedp.ExecAllocatorOption {
	flags := browserFlags(proxyURL)
	names := make([]string, 0, len(flags))
	for name := range flags {
		names = append(names, name)
	}
	sort.Strings(names)
	options := []chromedp.ExecAllocatorOption{
		chromedp.ExecPath(browserEnvironmentExecutable),
		chromedp.ModifyCmdFunc(configure),
		// Read the DevTools address from the browser output within the timeout
		// below; the pipe transport has no startup limit of its own.
		remote.WebSocket,
		chromedp.WSURLReadTimeout(5 * time.Second),
	}
	for _, name := range names {
		options = append(options, chromedp.Flag(name, flags[name]))
	}
	// Given a profile, the allocator neither creates nor removes one, and never
	// scans for leftover profile users to kill by numeric PID.
	return append(options, chromedp.UserDataDir(profile))
}

func controlledBrowserArguments(executable string, arguments ...string) []string {
	environment := browserEnvironment()
	result := make([]string, 0, len(environment)+len(arguments)+2)
	result = append(result, "-i")
	result = append(result, environment...)
	result = append(result, executable)
	return append(result, arguments...)
}

type supervisorCommand struct {
	path   string
	prefix []string
	// removeProfile removes the browser profile after joined teardown. Nil
	// means os.RemoveAll; tests inject removal failures.
	removeProfile func(string) error
}

type browserJoinProof struct {
	reader, writer *os.File
	passedWriter   *os.File
	done           chan struct{}
	readerDone     chan struct{}
	mu             sync.Mutex
	closeOnce      sync.Once
	doneOnce       sync.Once
	cmd            *exec.Cmd
}

func newBrowserCommand(executable, supervisor string, prefix []string) (func(*exec.Cmd), *browserJoinProof) {
	reader, writer, err := os.Pipe()
	if err != nil {
		proof := &browserJoinProof{done: make(chan struct{}), readerDone: make(chan struct{})}
		close(proof.readerDone)
		return func(cmd *exec.Cmd) { cmd.Err = err }, proof
	}
	proof := &browserJoinProof{reader: reader, writer: writer, done: make(chan struct{}), readerDone: make(chan struct{})}
	go proof.read()
	return func(cmd *exec.Cmd) {
		proof.mu.Lock()
		proof.cmd = cmd
		proof.mu.Unlock()
		arguments := controlledBrowserArguments(executable, cmd.Args[1:]...)
		cmd.Path = supervisor
		cmd.Args = append([]string{supervisor}, prefix...)
		passedFD, duplicateErr := unix.Dup(int(proof.writer.Fd()))
		if duplicateErr != nil {
			cmd.Err = duplicateErr
			proof.noChildStarted()
			return
		}
		proof.mu.Lock()
		proof.passedWriter = os.NewFile(uintptr(passedFD), "join-proof-pass")
		passedWriter := proof.passedWriter
		proof.mu.Unlock()
		proofFD := 3 + len(cmd.ExtraFiles)
		cmd.Args = append(cmd.Args, "--extra-files", strconv.Itoa(len(cmd.ExtraFiles)), "--proof-fd", strconv.Itoa(proofFD), "--", browserEnvironmentExecutable)
		cmd.Args = append(cmd.Args, arguments...)
		cmd.ExtraFiles = append(cmd.ExtraFiles, passedWriter)
		cmd.Env = make([]string, 0)
		cmd.SysProcAttr = browserProcessAttributes()
		cmd.Cancel = func() error {
			if cmd.Process == nil {
				return os.ErrProcessDone
			}
			err := cmd.Process.Signal(syscall.SIGTERM)
			if errors.Is(err, syscall.ESRCH) {
				return os.ErrProcessDone
			}
			return err
		}
	}, proof
}

func (p *browserJoinProof) read() {
	defer close(p.readerDone)
	defer p.reader.Close() //nolint:errcheck
	buffer := make([]byte, 1)
	state := byte(0)
	for {
		if _, err := p.reader.Read(buffer); err != nil {
			return
		}
		p.closeWriters()
		if buffer[0] == 'D' && state == 0 || buffer[0] == 'D' && state == 'S' {
			p.doneOnce.Do(func() { close(p.done) })
			return
		}
		if buffer[0] != 'S' || state != 0 {
			return
		}
		state = 'S'
	}
}

func (p *browserJoinProof) wait() {
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.passedWriter != nil {
		ignoreProcessError(p.passedWriter.Close())
	}
}

func (p *browserJoinProof) noChildStarted() {
	p.closeWriters()
	p.mu.Lock()
	if p.passedWriter != nil {
		ignoreProcessError(p.passedWriter.Close())
	}
	p.mu.Unlock()
	p.doneOnce.Do(func() { close(p.done) })
}

func (p *browserJoinProof) completeIfNotStarted() {
	p.mu.Lock()
	cmd := p.cmd
	p.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		p.noChildStarted()
	}
}

func (p *browserJoinProof) closeWriters() {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.writer != nil {
			ignoreProcessError(p.writer.Close())
		}
	})
}

func ignoreProcessError(error) {}

func withBrowser(ctx context.Context, executable, proxyURL string, supervisor supervisorCommand, diagnose bool, run func(context.Context) error) (resultErr error) {
	started := time.Now()
	removeProfile := supervisor.removeProfile
	if removeProfile == nil {
		removeProfile = os.RemoveAll
	}
	profile, err := os.MkdirTemp("", browserProfilePrefix)
	if err != nil {
		if diagnose {
			return diagnoseInitialization(ctx, ErrUnavailable, "launch", err, nil, time.Since(started))
		}
		return err
	}
	configure, proof := newBrowserCommand(executable, supervisor.path, supervisor.prefix)
	allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(ctx, allocatorOptions(executable, proxyURL, profile, configure)...)
	browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
	defer func() {
		browserCancel()
		allocatorCancel()
		proof.completeIfNotStarted()
		proof.wait()
		// The proof completes only after every browser process is terminal, so
		// no browser process can still write to the profile. A profile left
		// behind fails the attempt unless an earlier error already does.
		if removeErr := removeProfile(profile); removeErr != nil && resultErr == nil {
			failure := initializationFailure(ErrRenderFailed, "profile-cleanup", initializationReason(removeErr))
			failure.elapsed = time.Since(started)
			resultErr = failure
		}
		if diagnose && resultErr != nil {
			proof.mu.Lock()
			var state *os.ProcessState
			if proof.cmd != nil {
				state = proof.cmd.ProcessState
			}
			proof.mu.Unlock()
			resultErr = diagnoseInitialization(ctx, ErrUnavailable, "launch", resultErr, state, time.Since(started))
		}
	}()
	return run(browserCtx)
}
