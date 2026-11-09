package printrender

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	jsonv2 "github.com/chromedp/cdproto/cdp/jsonv2"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
)

// recordingSession is a cdp.Session that records each request as the method
// and the JSON parameters that chromedp sends, and answers from results.
type recordingSession struct {
	mu       sync.Mutex
	requests []string
	results  map[string]string
}

func (s *recordingSession) Call(_ context.Context, method string, params, result any) error {
	encoded, err := jsonv2.Marshal(params, chromedp.DefaultMarshalOptions)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.requests = append(s.requests, method+" "+string(encoded))
	s.mu.Unlock()
	raw, ok := s.results[method]
	if !ok {
		return nil
	}
	return jsonv2.Unmarshal([]byte(raw), result, chromedp.DefaultUnmarshalOptions)
}

func (s *recordingSession) Subscribe(string) (<-chan jsonv2.Value, func()) {
	events := make(chan jsonv2.Value)
	close(events)
	return events, func() {}
}

func (s *recordingSession) assertRequests(t *testing.T, want ...string) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if !slices.Equal(s.requests, want) {
		t.Fatalf("requests differ\ngot:  %q\nwant: %q", s.requests, want)
	}
}

func TestRequestControlSetsEveryFlagExplicitly(t *testing.T) {
	browser, page := &recordingSession{}, &recordingSession{}
	if err := enableRequestControl(t.Context(), browser, page); err != nil {
		t.Fatal(err)
	}
	browser.assertRequests(t, `Target.setAutoAttach {"autoAttach":true,"waitForDebuggerOnStart":true,"flatten":true}`)
	page.assertRequests(t,
		`Fetch.enable {"patterns":[{"urlPattern":"*","requestStage":"Request"}],"handleAuthRequests":false}`,
		`Network.setCacheDisabled {"cacheDisabled":true}`,
	)
}

func TestCaptureEnvironmentSetsEveryFlagExplicitly(t *testing.T) {
	for _, test := range []struct {
		name   string
		format renderjob.Format
		want   []string
	}{
		{"pdf", renderjob.PDF, []string{`Emulation.setEmulatedMedia {"media":"print"}`}},
		{"png", renderjob.PNG, []string{
			`Emulation.setDeviceMetricsOverride {"width":1200,"height":630,"deviceScaleFactor":1,"mobile":false,"dontSetVisibleSize":false,"screenOrientationLockEmulation":false}`,
			`Emulation.setEmulatedMedia {"media":"screen"}`,
			`Emulation.setDefaultBackgroundColorOverride {"color":{"r":255,"g":255,"b":255,"a":1}}`,
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := &recordingSession{}
			if err := configureCaptureEnvironment(t.Context(), session, test.format); err != nil {
				t.Fatal(err)
			}
			session.assertRequests(t, test.want...)
		})
	}
}

func TestReadinessEvaluationSetsEveryFlagExplicitly(t *testing.T) {
	// chromedp.Evaluate[bool] starts with the expression and returnByValue.
	params := runtime.EvaluateParams{Expression: "EXPR", ReturnByValue: new(true)}
	readinessEvaluation(&params)
	encoded, err := jsonv2.Marshal(params, chromedp.DefaultMarshalOptions)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"expression":"EXPR","includeCommandLineAPI":false,"silent":false,"returnByValue":true,"generatePreview":false,"userGesture":false,"awaitPromise":true,"throwOnSideEffect":false,"disableBreaks":false,"replMode":false,"allowUnsafeEvalBlockedByCSP":true}`
	if string(encoded) != want {
		t.Fatalf("evaluation parameters = %s, want %s", encoded, want)
	}
}

func TestPDFCaptureStreamsAndClosesTheReturnedHandle(t *testing.T) {
	dynamic := "D:20260905174051+00'00'"
	pdf := classicPDF(t, dynamic, dynamic, dynamic)
	session := &recordingSession{results: map[string]string{
		"Page.printToPDF": `{"stream":"stream-1"}`,
		"IO.read":         fmt.Sprintf(`{"base64Encoded":true,"data":%q,"eof":true}`, base64.StdEncoding.EncodeToString(pdf)),
	}}
	output, err := capturePDF(t.Context(), session, time.Date(2026, 9, 19, 8, 30, 5, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if want := classicPDF(t, dynamic, "D:20260919083005+00'00'", "D:20260919083005+00'00'"); !bytes.Equal(output, want) {
		t.Fatalf("PDF differs: %s", describeByteDifferences(output, want))
	}
	session.assertRequests(t,
		`Page.printToPDF {"landscape":false,"displayHeaderFooter":false,"printBackground":true,"scale":1,"marginTop":0,"marginBottom":0,"marginLeft":0,"marginRight":0,"preferCSSPageSize":true,"transferMode":"ReturnAsStream","generateTaggedPDF":false,"generateDocumentOutline":false}`,
		`IO.read {"handle":"stream-1","size":65536}`,
		`IO.close {"handle":"stream-1"}`,
	)
}

func TestPDFStreamKeepsRawAndBase64ChunksUndecoded(t *testing.T) {
	for _, test := range []struct {
		name   string
		result string
		want   streamChunk
	}{
		{"raw text", `{"base64Encoded":false,"data":"%PDF-1.7\n","eof":false}`, streamChunk{data: "%PDF-1.7\n"}},
		{"base64", `{"base64Encoded":true,"data":"JVBERi0=","eof":true}`, streamChunk{data: "JVBERi0=", base64: true, eof: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := &recordingSession{results: map[string]string{"IO.read": test.result}}
			stream := &cdpPDFStream{session: session, handle: "stream-1"}
			chunk, err := stream.read(t.Context(), 64*1024)
			if err != nil || chunk != test.want {
				t.Fatalf("chunk = %+v, %v; want %+v", chunk, err, test.want)
			}
		})
	}
}

func TestPNGCaptureSetsEveryFlagExplicitly(t *testing.T) {
	image := encodePNG(t, 1200, 630)
	session := &recordingSession{results: map[string]string{
		"Page.captureScreenshot": fmt.Sprintf(`{"data":%q}`, base64.StdEncoding.EncodeToString(image)),
	}}
	output, err := capturePNG(t.Context(), session, pngMaxBytes)
	if err != nil || !bytes.Equal(output, image) {
		t.Fatalf("capture = %d bytes, %v; want the %d byte image", len(output), err, len(image))
	}
	session.assertRequests(t,
		`Page.captureScreenshot {"format":"png","clip":{"x":0,"y":0,"width":1200,"height":630,"scale":1},"fromSurface":true,"captureBeyondViewport":false,"optimizeForSpeed":false}`,
	)
}

func TestPausedRequestDecisionsSetEveryFlagExplicitly(t *testing.T) {
	initialURL := "http://127.0.0.1:20030/print/00000000-0000-0000-0000-000000000001"
	policy := newRequestPolicy(initialURL, "capability-value", "job-value")
	session := &recordingSession{}
	var failure attemptFailure
	canceled := 0
	cancel := func() { canceled++ }

	handlePausedRequest(t.Context(), session, cancel, &failure, policy, &fetch.EventRequestPaused{
		RequestID: "interception-1", ResourceType: network.ResourceTypeDocument,
		Request: &network.Request{URL: initialURL, Method: "GET", Headers: network.Headers{"Accept": "text/html", "Cookie": "ambient=1"}},
	})
	if failure.failed() || canceled != 0 {
		t.Fatal("the initial document did not continue")
	}
	handlePausedRequest(t.Context(), session, cancel, &failure, policy, &fetch.EventRequestPaused{
		RequestID: "interception-2", ResourceType: network.ResourceTypeDocument,
		Request: &network.Request{URL: initialURL, Method: "POST"},
	})
	if !failure.failed() || canceled != 1 {
		t.Fatal("a denied request did not fail the attempt")
	}
	session.assertRequests(t,
		`Fetch.continueRequest {"requestId":"interception-1","headers":[{"name":"Accept","value":"text/html"},{"name":"Authorization","value":"RenderCapability capability-value"},{"name":"X-Render-Job-ID","value":"job-value"}],"interceptResponse":false}`,
		`Fetch.failRequest {"requestId":"interception-2","errorReason":"BlockedByClient"}`,
	)
}

func TestBrowserLaunchReadsTheDevToolsAddressFromOutput(t *testing.T) {
	record := filepath.Join(t.TempDir(), "arguments")
	executable := writeExecutable(t, fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$@\" > '%s'\nexit 7\n", record))
	err := withBrowser(t.Context(), executable, "http://127.0.0.1:20030", testSupervisorCommand(t), false, func(ctx context.Context) error {
		return chromedp.Do(ctx)
	})
	if err == nil {
		t.Fatal("a browser that exits at once started")
	}
	recorded, readErr := os.ReadFile(record)
	if readErr != nil {
		t.Fatal(readErr)
	}
	arguments := strings.Split(strings.TrimSpace(string(recorded)), "\n")
	if !slices.Contains(arguments, "--remote-debugging-port=0") || slices.Contains(arguments, "--remote-debugging-pipe") {
		t.Fatalf("browser transport arguments = %q", arguments)
	}
}
