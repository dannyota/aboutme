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
		//nolint:errcheck // The denial fails and cancels the whole render attempt either way.
		fetch.FailRequest(event.RequestID, network.ErrorReasonBlockedByClient).Do(ctx)
		cancel()
		return
	}
	if err := fetch.ContinueRequest(event.RequestID).WithHeaders(decision.headers).Do(ctx); err != nil {
		failure.fail()
		cancel()
	}
}
