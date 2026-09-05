package printrender

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAttemptProxyForwardsOneDocumentAndExactAssetsWithoutAmbientAuthority(t *testing.T) {
	var mu sync.Mutex
	var requests []*http.Request
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		requests = append(requests, request.Clone(request.Context()))
		mu.Unlock()
		writer.Header().Set("Content-Type", "text/plain")
		if _, err := writer.Write([]byte("ok")); err != nil {
			t.Error(err)
		}
	}))
	defer upstream.Close()

	initial := "http://127.0.0.1:20030/print/00000000-0000-0000-0000-000000000001"
	proxy, err := startAttemptProxy(t.Context(), proxyConfig{
		origin: "http://127.0.0.1:20030", forwardOrigin: upstream.URL, initialURL: initial,
		capability: "abcdefghijklmnopqrstuvwxyzABCDEFGH012345678", jobID: "00000000-0000-0000-0000-000000000002",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := proxy.close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	proxyURL, err := url.Parse(proxy.url())
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	document, err := http.NewRequestWithContext(t.Context(), http.MethodGet, initial, nil)
	if err != nil {
		t.Fatal(err)
	}
	document.Header.Set("Authorization", "RenderCapability abcdefghijklmnopqrstuvwxyzABCDEFGH012345678")
	document.Header.Set("X-Render-Job-ID", "00000000-0000-0000-0000-000000000002")
	document.Header.Set("Cookie", "ambient=1")
	response, err := client.Do(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("document status = %d", response.StatusCode)
	}

	for _, raw := range []string{
		"http://127.0.0.1:20030/_nuxt/assets/print.css",
		"http://127.0.0.1:20030/_nuxt/assets/print-fonts.css",
		"http://127.0.0.1:20030/_nuxt/fonts/inter-var.woff2",
	} {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if err := response.Body.Close(); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("asset %s status = %d", raw, response.StatusCode)
		}
	}

	for _, request := range []*http.Request{
		document.Clone(document.Context()),
		mustRequest(t, http.MethodGet, "http://127.0.0.1:20030/_nuxt/fonts/unknown.woff2"),
		mustRequest(t, http.MethodGet, "http://example.com/_nuxt/assets/print.css"),
		mustRequest(t, http.MethodPost, "http://127.0.0.1:20030/_nuxt/assets/print.css"),
		mustRequest(t, http.MethodConnect, "http://example.com:443"),
	} {
		response, err := client.Do(request)
		if err != nil {
			continue
		}
		if err := response.Body.Close(); err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("denied %s %s status = %d", request.Method, request.URL, response.StatusCode)
		}
	}

	if err := proxy.close(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 4 {
		t.Fatalf("upstream requests = %d, want 4", len(requests))
	}
	if requests[0].Host != "127.0.0.1:20030" || requests[0].Header.Get("Cookie") != "" {
		t.Fatalf("document host/cookie = %q / %q", requests[0].Host, requests[0].Header.Get("Cookie"))
	}
	for _, request := range requests[1:] {
		if request.Header.Get("Authorization") != "" || request.Header.Get("X-Render-Job-ID") != "" || request.Header.Get("Cookie") != "" {
			t.Fatalf("asset leaked authority: %#v", request.Header)
		}
	}
}

func TestAttemptProxyRejectsWrongDocumentAuthority(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("upstream reached") }))
	defer upstream.Close()
	proxy, err := startAttemptProxy(t.Context(), proxyConfig{
		origin: "http://127.0.0.1:20030", forwardOrigin: upstream.URL,
		initialURL: "http://127.0.0.1:20030/print/00000000-0000-0000-0000-000000000001",
		capability: "abcdefghijklmnopqrstuvwxyzABCDEFGH012345678", jobID: "00000000-0000-0000-0000-000000000002",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := proxy.close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:20030/print/00000000-0000-0000-0000-000000000001", nil)
	request.Header.Set("Authorization", "RenderCapability wrong")
	proxy.serveHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || strings.Contains(recorder.Body.String(), "wrong") {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func TestAttemptProxyReportsUpstreamFailuresAfterHandlerJoin(t *testing.T) {
	closeFailure := errors.New("close failure")
	for _, test := range []struct {
		name     string
		readErr  error
		closeErr error
		want     error
	}{
		{name: "truncated response", readErr: io.ErrUnexpectedEOF, want: io.ErrUnexpectedEOF},
		{name: "response close failure", readErr: io.EOF, closeErr: closeFailure, want: closeFailure},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := &blockingResponseBody{
				started: make(chan struct{}), release: make(chan struct{}),
				readErr: test.readErr, closeErr: test.closeErr,
			}
			proxy, err := startAttemptProxy(t.Context(), proxyConfig{
				origin:     "http://127.0.0.1:20030",
				initialURL: "http://127.0.0.1:20030/print/00000000-0000-0000-0000-000000000001",
				capability: "abcdefghijklmnopqrstuvwxyzABCDEFGH012345678",
				jobID:      "00000000-0000-0000-0000-000000000002",
			})
			if err != nil {
				t.Fatal(err)
			}
			proxy.transport = &fixedProxyTransport{response: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"text/plain"}},
				Body:       body,
			}}
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(body.release) }) }
			t.Cleanup(func() {
				release()
				if closeErr := proxy.close(); closeErr != nil && !errors.Is(closeErr, test.want) {
					t.Error("proxy cleanup returned an unrelated error")
				}
			})

			request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, proxy.admission.config.initialURL, nil)
			request.Header.Set("Authorization", "RenderCapability "+proxy.admission.config.capability)
			request.Header.Set("X-Render-Job-ID", proxy.admission.config.jobID)
			handlerDone := make(chan struct{})
			go func() {
				proxy.serveHTTP(httptest.NewRecorder(), request)
				close(handlerDone)
			}()
			<-body.started
			closeResult := make(chan error, 1)
			go func() { closeResult <- proxy.close() }()
			select {
			case <-closeResult:
				t.Fatal("proxy close returned before the admitted handler finished")
			case <-time.After(25 * time.Millisecond):
			}
			release()
			<-handlerDone
			if closeErr := <-closeResult; !errors.Is(closeErr, test.want) {
				t.Fatal("proxy close did not report the upstream response failure")
			}
		})
	}
}

type fixedProxyTransport struct {
	response *http.Response
}

func (t *fixedProxyTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return t.response, nil
}

func (*fixedProxyTransport) CloseIdleConnections() {}

type blockingResponseBody struct {
	started  chan struct{}
	release  chan struct{}
	readErr  error
	closeErr error
	read     bool
}

func (b *blockingResponseBody) Read(bytes []byte) (int, error) {
	if b.read {
		return 0, b.readErr
	}
	b.read = true
	close(b.started)
	<-b.release
	return copy(bytes, "partial"), nil
}

func (b *blockingResponseBody) Close() error {
	return b.closeErr
}

func mustRequest(t *testing.T, method, rawURL string) *http.Request {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), method, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	return request
}
