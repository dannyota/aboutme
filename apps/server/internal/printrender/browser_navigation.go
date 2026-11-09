package printrender

import (
	"context"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"

	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
)

func runNavigation(ctx context.Context, cancel context.CancelFunc, callbacks *joinGroup, failure *attemptFailure, initialURL string, navigation renderjob.Navigation) error {
	if err := chromedp.Do(ctx); err != nil {
		return err
	}
	state := chromedp.FromContext(ctx)
	if state == nil || state.Browser == nil || state.Target == nil {
		return ErrRenderFailed
	}
	mainTarget := state.Target.TargetID
	session := state.Target
	policy := newRequestPolicy(initialURL, navigation.Capability, navigation.JobID.String())

	if err := chromedp.Listen(ctx, func(event any) {
		switch value := event.(type) {
		case *fetch.EventRequestPaused:
			scheduleCallback(callbacks, failure, cancel, func() {
				handlePausedRequest(ctx, session, cancel, failure, policy, value)
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
	}); err != nil {
		return err
	}
	if err := chromedp.ListenBrowser(ctx, func(event any) {
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
	}); err != nil {
		return err
	}

	if runErr := chromedp.Do(ctx, chromedp.Func(func(actionCtx context.Context, page *chromedp.Target) error {
		return enableRequestControl(actionCtx, chromedp.FromContext(actionCtx).Browser, page)
	})); runErr != nil {
		return runErr
	}
	if configureErr := configureCaptureEnvironment(ctx, session, navigation.Format); configureErr != nil {
		return configureErr
	}
	response, err := chromedp.RunResponse(ctx, chromedp.Navigate(initialURL))
	if err != nil {
		return err
	}
	if response == nil || validatePrintResponse(response.Status, response.FromServiceWorker, response.Headers) != nil {
		return ErrRenderFailed
	}
	if configureErr := configureCaptureEnvironment(ctx, session, navigation.Format); configureErr != nil {
		return configureErr
	}
	if readinessErr := awaitPageReadiness(ctx); readinessErr != nil {
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
		output, err = capturePDF(ctx, session, navigation.RevisionTime)
	case renderjob.Card:
		output, err = capturePNG(ctx, session, cardMaxBytes)
	default:
		output, err = capturePNG(ctx, session, pngMaxBytes)
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

// enableRequestControl makes the browser attach each new target paused, so the
// target listener can reject it, and sends every page request through the
// request policy with the browser cache disabled.
func enableRequestControl(ctx context.Context, browser, page cdp.Session) error {
	if _, err := cdp.Call(ctx, browser, target.SetAutoAttach, target.SetAutoAttachParams{
		AutoAttach: true, WaitForDebuggerOnStart: true, Flatten: new(true),
	}); err != nil {
		return err
	}
	if _, err := cdp.Call(ctx, page, fetch.Enable, fetch.EnableParams{
		Patterns:           []*fetch.RequestPattern{{URLPattern: "*", RequestStage: fetch.RequestStageRequest}},
		HandleAuthRequests: new(false),
	}); err != nil {
		return err
	}
	_, err := cdp.Call(ctx, page, network.SetCacheDisabled, network.SetCacheDisabledParams{CacheDisabled: true})
	return err
}

func scheduleCallback(callbacks *joinGroup, failure *attemptFailure, cancel context.CancelFunc, fn func()) bool {
	if callbacks.goRun(fn) {
		return true
	}
	failure.fail()
	cancel()
	return false
}

func unexpectedTarget(info *target.Info, main target.ID) bool {
	if info == nil || (info.TargetID == main && info.Type == "page") {
		return false
	}
	return info.Type != "browser_ui"
}

func handlePausedRequest(ctx context.Context, session cdp.Session, cancel context.CancelFunc, failure *attemptFailure, policy *requestPolicy, event *fetch.EventRequestPaused) {
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
		//nolint:errcheck // The denial fails and cancels the whole render attempt either way.
		cdp.Call(ctx, session, fetch.FailRequest, fetch.FailRequestParams{RequestID: event.RequestID, ErrorReason: network.ErrorReasonBlockedByClient})
		cancel()
		return
	}
	if _, err := cdp.Call(ctx, session, fetch.ContinueRequest, fetch.ContinueRequestParams{
		RequestID: event.RequestID, Headers: decision.headers, InterceptResponse: new(false),
	}); err != nil {
		failure.fail()
		cancel()
	}
}
