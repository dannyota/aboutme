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
		return chromedp.Run(browserCtx)
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
		// Base64Encoded reports whether Data holds base64 rather than raw text.
		Base64Encoded bool `json:"base64Encoded"`
		// Data is the chunk of stream content that IO.read returned.
		Data string `json:"data"`
		// EOF reports whether the stream has no more content after Data.
		EOF bool `json:"eof"`
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
