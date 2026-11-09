package printrender

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/chromedp/cdproto"
	jsonv2 "github.com/chromedp/cdproto/cdp/jsonv2"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"

	"danny.vn/aboutme/apps/server/internal/renderjob"
)

const (
	scriptedOrigin     = "http://127.0.0.1:20030"
	scriptedInitialURL = scriptedOrigin + "/print/00000000-0000-0000-0000-000000000001"
)

// scriptedAsset is a request that the scripted page makes after the document.
type scriptedAsset struct {
	url, resourceType, contentType, mimeType, charset string
}

// scriptedBrowser is a chromedp.Transport that plays the browser side of one
// render. It answers each request, emits the events of a page that loads the
// print document and then each asset, and records every request of the
// browser and page sessions in one ordered transcript.
type scriptedBrowser struct {
	assets    []scriptedAsset
	pdf, png  []byte
	incoming  chan *cdproto.Message
	closed    chan struct{}
	closeOnce sync.Once

	mu        sync.Mutex
	requests  []string
	continued int
}

func newScriptedBrowser(assets []scriptedAsset, pdf, png []byte) *scriptedBrowser {
	return &scriptedBrowser{assets: assets, pdf: pdf, png: png, incoming: make(chan *cdproto.Message, 64), closed: make(chan struct{})}
}

func (b *scriptedBrowser) Read(ctx context.Context, msg *cdproto.Message) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-b.closed:
		return io.EOF
	case next := <-b.incoming:
		*msg = *next
		return nil
	}
}

func (b *scriptedBrowser) Write(ctx context.Context, msg *cdproto.Message) error {
	session := "browser"
	if msg.SessionID != "" {
		session = "page"
	}
	b.mu.Lock()
	b.requests = append(b.requests, session+" "+string(msg.Method)+" "+string(msg.Params))
	result, events := b.answer(msg)
	b.mu.Unlock()
	for _, reply := range append([]*cdproto.Message{{ID: msg.ID, SessionID: msg.SessionID, Result: jsonv2.Value(result)}}, events...) {
		select {
		case b.incoming <- reply:
		case <-ctx.Done():
			return ctx.Err()
		case <-b.closed:
			return io.ErrClosedPipe
		}
	}
	return nil
}

func (b *scriptedBrowser) Close() error {
	b.closeOnce.Do(func() { close(b.closed) })
	return nil
}

// answer returns the result of msg and the events that follow it.
func (b *scriptedBrowser) answer(msg *cdproto.Message) (string, []*cdproto.Message) {
	switch msg.Method {
	case "Target.setDiscoverTargets":
		if msg.SessionID == "" {
			return "{}", []*cdproto.Message{scriptedEvent("", "Target.targetCreated", map[string]any{
				"targetInfo": map[string]any{"targetId": "page-1", "type": "page", "title": "", "url": "about:blank", "attached": false, "canAccessOpener": false},
			})}
		}
	case "Target.attachToTarget":
		return `{"sessionId":"session-1"}`, nil
	case "Runtime.evaluate":
		var params struct {
			Expression string `json:"expression"`
		}
		if json.Unmarshal(msg.Params, &params) == nil && params.Expression == "self" {
			return `{"result":{"type":"object","className":"Window"}}`, nil
		}
		return `{"result":{"type":"boolean","value":true}}`, nil
	case "Page.navigate":
		return `{"frameId":"page-1","loaderId":"loader-1"}`, []*cdproto.Message{
			scriptedEvent("session-1", "Page.lifecycleEvent", map[string]any{"frameId": "page-1", "loaderId": "loader-1", "name": "init", "timestamp": 1}),
			scriptedEvent("session-1", "Network.requestWillBeSent", map[string]any{
				"requestId": "request-1", "loaderId": "loader-1", "documentURL": scriptedInitialURL, "timestamp": 1, "wallTime": 1, "type": "Document", "frameId": "page-1",
				"request": map[string]any{"url": scriptedInitialURL, "method": "GET", "headers": map[string]any{}},
			}),
			scriptedPaused(1, scriptedInitialURL, "Document", map[string]any{"Accept": "text/html", "Cookie": "ambient=1"}),
		}
	case "Fetch.continueRequest":
		b.continued++
		return "{}", b.afterContinue()
	case "Page.printToPDF":
		return `{"stream":"stream-1"}`, nil
	case "IO.read":
		return encodeScripted(map[string]any{"base64Encoded": true, "data": base64.StdEncoding.EncodeToString(b.pdf), "eof": true}), nil
	case "Page.captureScreenshot":
		return encodeScripted(map[string]any{"data": base64.StdEncoding.EncodeToString(b.png)}), nil
	}
	return "{}", nil
}

// afterContinue pauses the next asset, or loads every continued request.
func (b *scriptedBrowser) afterContinue() []*cdproto.Message {
	var events []*cdproto.Message
	if b.continued == 1 {
		events = append(events, scriptedResponse(1, scriptedAsset{
			url: scriptedInitialURL, resourceType: "Document", contentType: "text/html; charset=utf-8", mimeType: "text/html", charset: "utf-8",
		}, map[string]any{"Content-Security-Policy": printContentSecurityPolicy}))
	}
	if next := b.continued - 1; next < len(b.assets) {
		return append(events, scriptedPaused(next+2, b.assets[next].url, b.assets[next].resourceType, map[string]any{}))
	}
	for index, asset := range b.assets {
		events = append(events,
			scriptedResponse(index+2, asset, nil),
			scriptedEvent("session-1", "Network.loadingFinished", map[string]any{"requestId": fmt.Sprintf("request-%d", index+2), "timestamp": 1, "encodedDataLength": 0}),
		)
	}
	return append(events, scriptedEvent("session-1", "Page.loadEventFired", map[string]any{"timestamp": 1}))
}

func scriptedPaused(index int, url, resourceType string, headers map[string]any) *cdproto.Message {
	return scriptedEvent("session-1", "Fetch.requestPaused", map[string]any{
		"requestId": fmt.Sprintf("interception-%d", index), "networkId": fmt.Sprintf("request-%d", index),
		"frameId": "page-1", "resourceType": resourceType,
		"request": map[string]any{"url": url, "method": "GET", "headers": headers},
	})
}

func scriptedResponse(index int, asset scriptedAsset, headers map[string]any) *cdproto.Message {
	all := map[string]any{"Content-Type": asset.contentType}
	for name, value := range headers {
		all[name] = value
	}
	return scriptedEvent("session-1", "Network.responseReceived", map[string]any{
		"requestId": fmt.Sprintf("request-%d", index), "loaderId": "loader-1", "timestamp": 1, "type": asset.resourceType, "frameId": "page-1",
		"response": map[string]any{
			"url": asset.url, "status": 200, "statusText": "OK", "headers": all, "mimeType": asset.mimeType, "charset": asset.charset,
			"connectionReused": false, "connectionId": 0, "encodedDataLength": 0, "securityState": "neutral",
		},
	})
}

func scriptedEvent(session, method string, params map[string]any) *cdproto.Message {
	return &cdproto.Message{SessionID: target.SessionID(session), Method: cdproto.MethodType(method), Params: jsonv2.Value(encodeScripted(params))}
}

func encodeScripted(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

// scriptedAllocator gives a chromedp context the scripted browser.
type scriptedAllocator struct{ browser *scriptedBrowser }

func (a scriptedAllocator) Allocate(ctx context.Context, opts ...chromedp.BrowserOption) (*chromedp.Browser, error) {
	return chromedp.NewBrowserTransport(ctx, a.browser, opts...)
}

func (scriptedAllocator) Wait() {}

// chromedpTargetSetup is what chromedp sends to find, attach and prepare the
// first page target before the renderer sends anything.
func chromedpTargetSetup() []string {
	return []string{
		`browser Target.setDiscoverTargets {"discover":true}`,
		`browser Target.attachToTarget {"targetId":"page-1","flatten":true}`,
		`page Runtime.enable {}`,
		`page Runtime.evaluate {"expression":"self"}`,
		`page Log.enable {}`,
		`page Network.enable {}`,
		`page Inspector.enable {}`,
		`page Page.enable {}`,
		`page DOM.enable {}`,
		`page CSS.enable {}`,
		`page Target.setDiscoverTargets {"discover":true}`,
		`page Target.setAutoAttach {"autoAttach":true,"waitForDebuggerOnStart":false,"flatten":true}`,
		`page Page.setLifecycleEventsEnabled {"enabled":true}`,
	}
}

func TestRenderRequestsMatchTheProtocolFixtures(t *testing.T) {
	dynamic, revised := "D:20260905174051+00'00'", "D:20260919083005+00'00'"
	pdf, png := classicPDF(t, dynamic, dynamic, dynamic), encodePNG(t, 1200, 630)
	styles := []scriptedAsset{
		{url: scriptedOrigin + "/_nuxt/assets/print.css", resourceType: "Stylesheet", contentType: "text/css; charset=utf-8", mimeType: "text/css", charset: "utf-8"},
		{url: scriptedOrigin + "/_nuxt/assets/print-fonts.css", resourceType: "Stylesheet", contentType: "text/css", mimeType: "text/css"},
	}
	for _, test := range []struct {
		name    string
		format  renderjob.Format
		assets  []scriptedAsset
		fixture string
		want    []byte
	}{
		{"pdf", renderjob.PDF, styles, "requests-pdf.txt", classicPDF(t, dynamic, revised, revised)},
		{"png", renderjob.PNG, styles, "requests-png.txt", png},
		{"denied request", renderjob.PDF, []scriptedAsset{{url: scriptedOrigin + "/api/v1/resumes", resourceType: "Fetch"}}, "requests-denied.txt", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			browser := newScriptedBrowser(test.assets, pdf, png)
			output, failed, err := runScriptedNavigation(t, browser, test.format)
			if test.want == nil && (err == nil || !failed) {
				t.Fatalf("denied render: error = %v, failed = %t", err, failed)
			}
			if test.want != nil && (err != nil || failed || !bytes.Equal(output, test.want)) {
				t.Fatalf("render: error = %v, failed = %t, %d bytes", err, failed, len(output))
			}
			fixture, readErr := os.ReadFile(filepath.Join("testdata", test.fixture))
			if readErr != nil {
				t.Fatal(readErr)
			}
			want := append(chromedpTargetSetup(), strings.Split(strings.TrimSuffix(string(fixture), "\n"), "\n")...)
			browser.mu.Lock()
			defer browser.mu.Unlock()
			if !slices.Equal(browser.requests, want) {
				t.Fatalf("requests differ\ngot:\n%s\nwant:\n%s", strings.Join(browser.requests, "\n"), strings.Join(want, "\n"))
			}
		})
	}
}

// runScriptedNavigation runs one attempt as renderAttempt does, with the
// scripted browser in place of the launched one.
func runScriptedNavigation(t *testing.T, browser *scriptedBrowser, format renderjob.Format) ([]byte, bool, error) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	allocatorCtx, allocatorCancel := chromedp.NewAllocatorContext(ctx, scriptedAllocator{browser: browser})
	defer allocatorCancel()
	browserCtx, browserCancel := chromedp.NewContext(allocatorCtx)
	defer browserCancel()
	var callbacks joinGroup
	var failure attemptFailure
	err := runNavigation(browserCtx, cancel, &callbacks, &failure, scriptedInitialURL, validTestNavigation(format))
	callbacks.stop()
	cancel()
	callbacks.wait()
	return failure.output(), failure.failed(), err
}
