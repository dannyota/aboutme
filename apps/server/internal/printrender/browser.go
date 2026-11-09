package printrender

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	cdpio "github.com/chromedp/cdproto/io"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
)

const (
	pdfMaxBytes  = 16_777_216
	pngMaxBytes  = 4_194_304
	cardMaxBytes = 524_288
)

func readyBrowser(ctx context.Context, renderer *Renderer) (resultErr error) {
	proxy, err := startAttemptProxy(ctx, proxyConfig{
		origin: renderer.origin, forwardOrigin: renderer.forwardOrigin,
		initialURL: renderer.origin + "/forbidden-readiness-navigation",
		capability: "readiness-does-not-have-render-authority", jobID: "00000000-0000-0000-0000-000000000000",
	})
	if err != nil {
		return diagnoseInitialization(ctx, ErrUnavailable, "proxy", err, nil, 0)
	}
	defer func() {
		if closeErr := proxy.close(); resultErr == nil && closeErr != nil {
			resultErr = diagnoseInitialization(ctx, ErrUnavailable, "proxy", closeErr, nil, 0)
		}
	}()
	if err := withBrowser(ctx, renderer.executable, proxy.url(), renderer.supervisor, true, func(browserCtx context.Context) error {
		return chromedp.Do(browserCtx)
	}); err != nil {
		return err
	}
	return nil
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
	err = withBrowser(ctx, r.executable, proxy.url(), r.supervisor, false, func(browserCtx context.Context) error {
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

// configureCaptureEnvironment sets the media and viewport of format. Like every
// protocol request of a render, it sets each optional flag explicitly, so a
// render never depends on browser defaults such as the choice of tagged PDFs.
func configureCaptureEnvironment(ctx context.Context, session cdp.Session, format renderjob.Format) error {
	if format == renderjob.PDF {
		if _, err := cdp.Call(ctx, session, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{Media: "print"}); err != nil {
			return ErrRenderFailed
		}
		return nil
	}
	white := &cdp.RGBA{R: 255, G: 255, B: 255, A: 1}
	if _, err := cdp.Call(ctx, session, emulation.SetDeviceMetricsOverride, emulation.SetDeviceMetricsOverrideParams{
		Width: 1200, Height: 630, DeviceScaleFactor: 1, Mobile: false,
		DontSetVisibleSize: new(false), ScreenOrientationLockEmulation: new(false),
	}); err != nil {
		return ErrRenderFailed
	}
	if _, err := cdp.Call(ctx, session, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{Media: "screen"}); err != nil {
		return ErrRenderFailed
	}
	if _, err := cdp.Call(ctx, session, emulation.SetDefaultBackgroundColorOverride, emulation.SetDefaultBackgroundColorOverrideParams{Color: white}); err != nil {
		return ErrRenderFailed
	}
	return nil
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
	ready, err := chromedp.Run(ctx, chromedp.Evaluate[bool](pageReadinessExpression, readinessEvaluation))
	if err != nil || !ready {
		return ErrRenderFailed
	}
	return nil
}

// readinessEvaluation completes the parameters that chromedp.Evaluate starts
// with the expression and returnByValue.
func readinessEvaluation(params *runtime.EvaluateParams) {
	params.IncludeCommandLineAPI = new(false)
	params.Silent = new(false)
	params.GeneratePreview = new(false)
	params.UserGesture = new(false)
	params.AwaitPromise = new(true)
	params.ThrowOnSideEffect = new(false)
	params.DisableBreaks = new(false)
	params.ReplMode = new(false)
	params.AllowUnsafeEvalBlockedByCSP = new(true)
}

func capturePDF(ctx context.Context, session cdp.Session, revisionTime time.Time) ([]byte, error) {
	result, err := cdp.Call(ctx, session, page.PrintToPDF, page.PrintToPDFParams{
		Landscape:               new(false),
		DisplayHeaderFooter:     new(false),
		PrintBackground:         new(true),
		Scale:                   1,
		MarginTop:               new(0.0),
		MarginBottom:            new(0.0),
		MarginLeft:              new(0.0),
		MarginRight:             new(0.0),
		PreferCSSPageSize:       new(true),
		TransferMode:            page.PrintToPDFTransferModeReturnAsStream,
		GenerateTaggedPDF:       new(false),
		GenerateDocumentOutline: new(false),
	})
	if err != nil || len(result.Data) != 0 || result.Stream == "" {
		return nil, ErrRenderFailed
	}
	output, err := readPDFStream(ctx, &cdpPDFStream{session: session, handle: result.Stream}, pdfMaxBytes)
	if err != nil {
		return nil, err
	}
	if err := setPDFMetadataDates(output, revisionTime); err != nil {
		return nil, ErrRenderFailed
	}
	return output, nil
}

type cdpPDFStream struct {
	session cdp.Session
	handle  cdpio.StreamHandle
}

// read decodes IO.read itself because cdpio.ReadResult decodes data as base64,
// while the protocol returns raw text when base64Encoded is false.
func (s *cdpPDFStream) read(ctx context.Context, size int64) (streamChunk, error) {
	params := cdpio.ReadParams{Handle: s.handle, Size: size}
	var result struct {
		// Base64Encoded reports whether Data holds base64 rather than raw text.
		Base64Encoded bool `json:"base64Encoded"`
		// Data is the chunk of stream content that IO.read returned.
		Data string `json:"data"`
		// EOF reports whether the stream has no more content after Data.
		EOF bool `json:"eof"`
	}
	if err := s.session.Call(ctx, cdpio.Read.Method, params, &result); err != nil {
		return streamChunk{}, err
	}
	return streamChunk{data: result.Data, base64: result.Base64Encoded, eof: result.EOF}, nil
}

func (s *cdpPDFStream) close(ctx context.Context) error {
	_, err := cdp.Call(ctx, s.session, cdpio.Close, cdpio.CloseParams{Handle: s.handle})
	return err
}

func capturePNG(ctx context.Context, session cdp.Session, limit int) ([]byte, error) {
	result, err := cdp.Call(ctx, session, page.CaptureScreenshot, page.CaptureScreenshotParams{
		Format:                page.CaptureScreenshotFormatPng,
		Clip:                  &page.Viewport{X: 0, Y: 0, Width: 1200, Height: 630, Scale: 1},
		FromSurface:           new(true),
		CaptureBeyondViewport: new(false),
		OptimizeForSpeed:      new(false),
	})
	if err != nil {
		return nil, ErrRenderFailed
	}
	if err := validatePNG(result.Data, limit); err != nil {
		return nil, err
	}
	return result.Data, nil
}
