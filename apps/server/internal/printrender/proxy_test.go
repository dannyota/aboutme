package printrender

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestAttemptProxyForwardsOneDocumentAndExactAssetsWithoutAmbientAuthority(t *testing.T) {
	var mu sync.Mutex
	var requests []*http.Request
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		requests = append(requests, request.Clone(context.Background()))
		mu.Unlock()
		writer.Header().Set("Content-Type", "text/plain")
		_, _ = writer.Write([]byte("ok"))
	}))
	defer upstream.Close()

	initial := "http://127.0.0.1:20030/print/00000000-0000-0000-0000-000000000001"
	proxy, err := startAttemptProxy(proxyConfig{
		origin: "http://127.0.0.1:20030", forwardOrigin: upstream.URL, initialURL: initial,
		capability: "abcdefghijklmnopqrstuvwxyzABCDEFGH012345678", jobID: "00000000-0000-0000-0000-000000000002",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.close()
	proxyURL, err := url.Parse(proxy.url())
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	document, _ := http.NewRequest(http.MethodGet, initial, nil)
	document.Header.Set("Authorization", "RenderCapability abcdefghijklmnopqrstuvwxyzABCDEFGH012345678")
	document.Header.Set("X-Render-Job-ID", "00000000-0000-0000-0000-000000000002")
	document.Header.Set("Cookie", "ambient=1")
	response, err := client.Do(document)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("document status = %d", response.StatusCode)
	}

	for _, raw := range []string{
		"http://127.0.0.1:20030/_nuxt/assets/print.css",
		"http://127.0.0.1:20030/_nuxt/assets/print-fonts.css",
		"http://127.0.0.1:20030/_nuxt/fonts/inter-var.woff2",
	} {
		request, _ := http.NewRequest(http.MethodGet, raw, nil)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("asset %s status = %d", raw, response.StatusCode)
		}
	}

	for _, request := range []*http.Request{
		document.Clone(context.Background()),
		mustRequest(t, http.MethodGet, "http://127.0.0.1:20030/_nuxt/fonts/unknown.woff2"),
		mustRequest(t, http.MethodGet, "http://example.com/_nuxt/assets/print.css"),
		mustRequest(t, http.MethodPost, "http://127.0.0.1:20030/_nuxt/assets/print.css"),
		mustRequest(t, http.MethodConnect, "http://example.com:443"),
	} {
		response, err := client.Do(request)
		if err != nil {
			continue
		}
		_ = response.Body.Close()
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
	proxy, err := startAttemptProxy(proxyConfig{
		origin: "http://127.0.0.1:20030", forwardOrigin: upstream.URL,
		initialURL: "http://127.0.0.1:20030/print/00000000-0000-0000-0000-000000000001",
		capability: "abcdefghijklmnopqrstuvwxyzABCDEFGH012345678", jobID: "00000000-0000-0000-0000-000000000002",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.close()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:20030/print/00000000-0000-0000-0000-000000000001", nil)
	request.Header.Set("Authorization", "RenderCapability wrong")
	proxy.serveHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || strings.Contains(recorder.Body.String(), "wrong") {
		t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
	}
}

func mustRequest(t *testing.T, method, rawURL string) *http.Request {
	t.Helper()
	request, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	return request
}
