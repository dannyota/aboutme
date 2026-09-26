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

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/fetch"
	cdpio "github.com/chromedp/cdproto/io"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
	"golang.org/x/sys/unix"

	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
)

const (
	pdfMaxBytes                  = 16_777_216
	pngMaxBytes                  = 4_194_304
	cardMaxBytes                 = 524_288
	browserEnvironmentExecutable = "/usr/bin/env"
	browserTimezone              = "TZ=UTC"
	browserLanguage              = "LANG=C.UTF-8"
	browserLocale                = "LC_ALL=C.UTF-8"
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

func allocatorOptions(executable, proxyURL string, configure func(*exec.Cmd)) []chromedp.ExecAllocatorOption {
	flags := browserFlags(proxyURL)
	names := make([]string, 0, len(flags))
	for name := range flags {
		names = append(names, name)
	}
	sort.Strings(names)
	options := []chromedp.ExecAllocatorOption{
		chromedp.ExecPath(browserEnvironmentExecutable),
		chromedp.ModifyCmdFunc(configure),
		chromedp.WSURLReadTimeout(5 * time.Second),
	}
	for _, name := range names {
		options = append(options, chromedp.Flag(name, flags[name]))
	}
	return options
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
		return func(cmd *exec.Cmd) { cmd.Path = "" }, proof
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
			cmd.Path = ""
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
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
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
		ignoreProcessError(p.writer.Close())
	})
}

func ignoreProcessError(error) {}

func readyBrowser(ctx context.Context, renderer *Renderer) (resultErr error) {
	proxy, err := startAttemptProxy(ctx, proxyConfig{
		origin: renderer.origin, forwardOrigin: renderer.forwardOrigin,
		initialURL: renderer.origin + "/forbidden-readiness-navigation",
		capability: "readiness-does-not-have-render-authority", jobID: "00000000-0000-0000-0000-000000000000",
	})
	if err != nil {
		return ErrUnavailable
	}
	defer func() {
		if err := proxy.close(); resultErr == nil && err != nil {
			resultErr = ErrUnavailable
		}
	}()
	if err := withBrowser(ctx, renderer.executable, proxy.url(), renderer.supervisor, func(browserCtx context.Context) error {
		return chromedp.Run(browserCtx)
	}); err != nil {
		return ErrUnavailable
	}
	return nil
}

func withBrowser(ctx context.Context, executable, proxyURL string, supervisor supervisorCommand, run func(context.Context) error) error {
	configure, proof := newBrowserCommand(executable, supervisor.path, supervisor.prefix)
	allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(ctx, allocatorOptions(executable, proxyURL, configure)...)
	browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
	defer func() {
		browserCancel()
		allocatorCancel()
		proof.completeIfNotStarted()
		proof.wait()
	}()
	return run(browserCtx)
}

func (r *Renderer) renderAttempt(parent context.Context, navigation renderjob.Navigation) (output []byte, resultErr error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	initialURL := r.origin + "/print/" + navigation.ResumeID.String()
	proxy, err := startAttemptProxy(ctx, proxyConfig{
		origin: r.origin, forwardOrigin: r.forwardOrigin, initialURL: initialURL,
		capability: navigation.Capability, jobID: navigation.JobID.String(),
	})
	if err != nil {
		return nil, ErrRenderFailed
	}
	defer func() {
		if closeErr := proxy.close(); resultErr == nil && closeErr != nil {
			output, resultErr = nil, ErrRenderFailed
		}
	}()

	var callbacks joinGroup
	var failure attemptFailure
	defer func() {
		callbacks.stop()
		cancel()
		callbacks.wait()
	}()
	err = withBrowser(ctx, r.executable, proxy.url(), r.supervisor, func(browserCtx context.Context) error {
		return runNavigation(browserCtx, cancel, &callbacks, &failure, initialURL, navigation)
	})
	if parentErr := parent.Err(); parentErr != nil {
		return nil, parentErr
	}
	if err != nil || failure.failed() {
		if errors.Is(err, ErrOutputTooLarge) {
			return nil, ErrOutputTooLarge
		}
		return nil, ErrRenderFailed
	}
	return failure.output(), nil
}

type attemptFailure struct {
	mu       sync.Mutex
	violated bool
	bytes    []byte
}

func (f *attemptFailure) fail() {
	f.mu.Lock()
	f.violated = true
	f.mu.Unlock()
}

func (f *attemptFailure) failed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.violated
}

func (f *attemptFailure) setOutput(value []byte) {
	f.mu.Lock()
	f.bytes = value
	f.mu.Unlock()
}

func (f *attemptFailure) output() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bytes
}

func runNavigation(ctx context.Context, cancel context.CancelFunc, callbacks *joinGroup, failure *attemptFailure, initialURL string, navigation renderjob.Navigation) error {
	if err := chromedp.Run(ctx); err != nil {
		return err
	}
	state := chromedp.FromContext(ctx)
	if state == nil || state.Browser == nil || state.Target == nil {
		return ErrRenderFailed
	}
	mainTarget := state.Target.TargetID
	targetCtx := cdp.WithExecutor(ctx, state.Target)
	policy := newRequestPolicy(initialURL, navigation.Capability, navigation.JobID.String())

	chromedp.ListenTarget(ctx, func(event any) {
		switch value := event.(type) {
		case *fetch.EventRequestPaused:
			scheduleCallback(callbacks, failure, cancel, func() {
				handlePausedRequest(targetCtx, cancel, failure, policy, value)
			})
		case *network.EventResponseReceived:
			if policy.tracks(value.RequestID) && (value.Response == nil || !policy.acceptResponse(pausedResponse{
				id: value.RequestID, url: value.Response.URL, resourceType: value.Type,
				status: value.Response.Status, headers: value.Response.Headers, mimeType: value.Response.MimeType,
				charset: value.Response.Charset, serviceWorker: value.Response.FromServiceWorker,
			})) {
				failure.fail()
				cancel()
			}
		case *network.EventLoadingFinished:
			if policy.tracks(value.RequestID) && !policy.finishResponse(value.RequestID) {
				failure.fail()
				cancel()
			}
		case *network.EventLoadingFailed:
			if policy.failResponse(value.RequestID) {
				failure.fail()
				cancel()
			}
		}
	})
	chromedp.ListenBrowser(ctx, func(event any) {
		switch value := event.(type) {
		case *target.EventAttachedToTarget:
			if unexpectedTarget(value.TargetInfo, mainTarget) {
				failure.fail()
				cancel()
			}
		case *target.EventTargetCreated:
			if unexpectedTarget(value.TargetInfo, mainTarget) {
				failure.fail()
				cancel()
			}
		}
	})

	if runErr := chromedp.Run(ctx,
		chromedp.ActionFunc(func(actionCtx context.Context) error {
			browserCtx := cdp.WithExecutor(actionCtx, chromedp.FromContext(actionCtx).Browser)
			return target.SetAutoAttach(true, true).WithFlatten(true).Do(browserCtx)
		}),
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{{URLPattern: "*", RequestStage: fetch.RequestStageRequest}}),
		network.SetCacheDisabled(true),
	); runErr != nil {
		return runErr
	}
	if configureErr := configureCaptureEnvironment(targetCtx, navigation.Format); configureErr != nil {
		return configureErr
	}
	response, err := chromedp.RunResponse(targetCtx, chromedp.Navigate(initialURL))
	if err != nil {
		return err
	}
	if response == nil || validatePrintResponse(response.Status, response.FromServiceWorker, response.Headers) != nil {
		return ErrRenderFailed
	}
	if configureErr := configureCaptureEnvironment(targetCtx, navigation.Format); configureErr != nil {
		return configureErr
	}
	if readinessErr := awaitPageReadiness(targetCtx); readinessErr != nil {
		return readinessErr
	}
	if failure.failed() {
		return ErrRenderFailed
	}
	if !policy.assetsComplete() {
		return ErrRenderFailed
	}
	var output []byte
	switch navigation.Format {
	case renderjob.PDF:
		output, err = capturePDF(targetCtx, navigation.RevisionTime)
	case renderjob.Card:
		output, err = capturePNG(targetCtx, cardMaxBytes)
	default:
		output, err = capturePNG(targetCtx, pngMaxBytes)
	}
	if err != nil {
		return err
	}
	callbacks.stop()
	cancel()
	callbacks.wait()
	if failure.failed() {
		return ErrRenderFailed
	}
	failure.setOutput(output)
	return nil
}

func scheduleCallback(callbacks *joinGroup, failure *attemptFailure, cancel context.CancelFunc, fn func()) bool {
	if callbacks.goRun(fn) {
		return true
	}
	failure.fail()
	cancel()
	return false
}

func configureCaptureEnvironment(ctx context.Context, format renderjob.Format) error {
	if format == renderjob.PDF {
		if err := emulation.SetEmulatedMedia().WithMedia("print").Do(ctx); err != nil {
			return ErrRenderFailed
		}
		return nil
	}
	white := &cdp.RGBA{R: 255, G: 255, B: 255, A: 1}
	if err := emulation.SetDeviceMetricsOverride(1200, 630, 1, false).Do(ctx); err != nil {
		return ErrRenderFailed
	}
	if err := emulation.SetEmulatedMedia().WithMedia("screen").Do(ctx); err != nil {
		return ErrRenderFailed
	}
	if err := emulation.SetDefaultBackgroundColorOverride().WithColor(white).Do(ctx); err != nil {
		return ErrRenderFailed
	}
	return nil
}

func unexpectedTarget(info *target.Info, main target.ID) bool {
	if info == nil || (info.TargetID == main && info.Type == "page") {
		return false
	}
	return info.Type != "browser_ui"
}

func handlePausedRequest(ctx context.Context, cancel context.CancelFunc, failure *attemptFailure, policy *requestPolicy, event *fetch.EventRequestPaused) {
	if event == nil || event.Request == nil {
		failure.fail()
		cancel()
		return
	}
	decision := policy.decide(pausedRequest{
		id: event.NetworkID, url: event.Request.URL, method: event.Request.Method, resourceType: event.ResourceType,
		headers: event.Request.Headers, redirected: event.RedirectedRequestID != "",
	})
	if !decision.allow {
		failure.fail()
		//nolint:errcheck // Denial already fails and cancels the whole render attempt.
		fetch.FailRequest(event.RequestID, network.ErrorReasonBlockedByClient).Do(ctx)
		cancel()
		return
	}
	if err := fetch.ContinueRequest(event.RequestID).WithHeaders(decision.headers).Do(ctx); err != nil {
		failure.fail()
		cancel()
	}
}

const pageReadinessExpression = `(async () => {
  const roots = document.querySelectorAll('[data-print-document="true"]');
  if (roots.length !== 1 || document.scripts.length !== 0) throw new Error('invalid document');
  void document.documentElement.offsetHeight;
  await document.fonts.ready;
  const fonts = Array.from(document.fonts);
  if (document.fonts.status !== 'loaded' || fonts.some((font) => font.status === 'error')) throw new Error('fonts failed');
  const images = Array.from(document.images);
  await Promise.all(images.map((image) => image.decode()));
  if (images.some((image) => !image.complete || image.naturalWidth <= 0 || image.naturalHeight <= 0)) throw new Error('image failed');
  return true;
})()`

func awaitPageReadiness(ctx context.Context) error {
	var ready bool
	err := chromedp.Run(ctx, chromedp.Evaluate(pageReadinessExpression, &ready, func(params *runtime.EvaluateParams) *runtime.EvaluateParams {
		return params.WithAwaitPromise(true)
	}))
	if err != nil || !ready {
		return ErrRenderFailed
	}
	return nil
}

func capturePDF(ctx context.Context, revisionTime time.Time) ([]byte, error) {
	data, handle, err := page.PrintToPDF().
		WithLandscape(false).
		WithDisplayHeaderFooter(false).
		WithPrintBackground(true).
		WithScale(1).
		WithMarginTop(0).
		WithMarginBottom(0).
		WithMarginLeft(0).
		WithMarginRight(0).
		WithPreferCSSPageSize(true).
		WithTransferMode(page.PrintToPDFTransferModeReturnAsStream).
		Do(ctx)
	if err != nil || len(data) != 0 || handle == "" {
		return nil, ErrRenderFailed
	}
	output, err := readPDFStream(ctx, &cdpPDFStream{handle: handle}, pdfMaxBytes)
	if err != nil {
		return nil, err
	}
	if err := setPDFMetadataDates(output, revisionTime); err != nil {
		return nil, ErrRenderFailed
	}
	return output, nil
}

type cdpPDFStream struct{ handle cdpio.StreamHandle }

func (s *cdpPDFStream) read(ctx context.Context, size int64) (streamChunk, error) {
	params := cdpio.Read(s.handle).WithSize(size)
	var result struct {
		Base64Encoded bool   `json:"base64Encoded"`
		Data          string `json:"data"`
		EOF           bool   `json:"eof"`
	}
	if err := cdp.Execute(ctx, cdpio.CommandRead, params, &result); err != nil {
		return streamChunk{}, err
	}
	return streamChunk{data: result.Data, base64: result.Base64Encoded, eof: result.EOF}, nil
}

func (s *cdpPDFStream) close(ctx context.Context) error { return cdpio.Close(s.handle).Do(ctx) }

func capturePNG(ctx context.Context, limit int) ([]byte, error) {
	data, err := page.CaptureScreenshot().
		WithFormat(page.CaptureScreenshotFormatPng).
		WithClip(&page.Viewport{X: 0, Y: 0, Width: 1200, Height: 630, Scale: 1}).
		WithFromSurface(true).
		WithCaptureBeyondViewport(false).
		Do(ctx)
	if err != nil {
		return nil, ErrRenderFailed
	}
	if err := validatePNG(data, limit); err != nil {
		return nil, err
	}
	return data, nil
}
