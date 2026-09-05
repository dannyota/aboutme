package printrender

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
)

func TestPinnedBrowserOutputIsByteDeterministic(t *testing.T) {
	if os.Getenv("ABOUTME_RUN_BROWSER_TEST") != "1" {
		t.Skip("set ABOUTME_RUN_BROWSER_TEST=1 for the pinned-browser proof")
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/print/00000000-0000-0000-0000-000000000001":
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			writer.Header().Set("Content-Security-Policy", printContentSecurityPolicy)
			writeTestResponse(t, writer, `<!doctype html><html><head><link rel="stylesheet" href="/_nuxt/assets/print-fonts.css"><link rel="stylesheet" href="/_nuxt/assets/print.css"></head><body><main data-print-document="true"><h1>Resume</h1></main></body></html>`)
		case "/_nuxt/assets/print-fonts.css", "/_nuxt/assets/print.css":
			writer.Header().Set("Content-Type", "text/css")
		default:
			http.Error(writer, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	renderer := newPinnedTestRenderer(t, server.URL)
	for _, format := range []renderjob.Format{renderjob.PDF, renderjob.PNG} {
		t.Run(string(format), func(t *testing.T) {
			first, err := renderer.Render(context.Background(), validTestNavigation(format))
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(1100 * time.Millisecond)
			second, err := renderer.Render(context.Background(), validTestNavigation(format))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(first, second) {
				t.Fatalf("identical renders differ: first %d bytes, second %d bytes; differences: %s", len(first), len(second), describeByteDifferences(first, second))
			}
			if format == renderjob.PDF && bytes.Count(first, []byte("("+canonicalPDFDate+")")) != 2 {
				t.Fatal("PDF does not contain the two canonical UTC metadata dates")
			}
		})
	}
}

func describeByteDifferences(first, second []byte) string {
	var ranges []string
	for index := 0; index < len(first) && index < len(second); {
		if first[index] == second[index] {
			index++
			continue
		}
		start := index
		for index < len(first) && index < len(second) && first[index] != second[index] {
			index++
		}
		ranges = append(ranges, fmt.Sprintf("%d:%q/%q", start, first[start:index], second[start:index]))
	}
	return strings.Join(ranges, ", ")
}

func TestPinnedBrowserClosedNetworkAndCapture(t *testing.T) {
	if os.Getenv("ABOUTME_RUN_BROWSER_TEST") != "1" {
		t.Skip("set ABOUTME_RUN_BROWSER_TEST=1 for the pinned-browser proof")
	}
	var mu sync.Mutex
	requests := make([]string, 0, 3)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		requests = append(requests, request.Method+" "+request.URL.RequestURI())
		mu.Unlock()
		switch request.URL.Path {
		case "/print/00000000-0000-0000-0000-000000000001":
			if request.Header.Get("Authorization") != "RenderCapability abcdefghijklmnopqrstuvwxyzABCDEFGH012345678" || request.Header.Get("X-Render-Job-ID") != "00000000-0000-0000-0000-000000000002" || request.Header.Get("Cookie") != "" {
				http.Error(writer, "not found", http.StatusNotFound)
				return
			}
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			writer.Header().Set("Content-Security-Policy", printContentSecurityPolicy)
			writeTestResponse(t, writer, `<!doctype html><html><head><link rel="stylesheet" href="/_nuxt/assets/print-fonts.css"><link rel="stylesheet" href="/_nuxt/assets/print.css"></head><body><main data-print-document="true"><h1>Resume</h1><img alt="pixel" src="data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw=="></main></body></html>`)
		case "/_nuxt/assets/print-fonts.css":
			writer.Header().Set("Content-Type", "text/css")
			writeTestResponse(t, writer, "@font-face{font-family:unused;src:url('/_nuxt/fonts/inter-var.woff2')}")
		case "/_nuxt/assets/print.css":
			writer.Header().Set("Content-Type", "text/css")
			writeTestResponse(t, writer, "@page{size:A4;margin:15mm}html,body{margin:0;background:white}h1{color:#123456}")
		default:
			http.Error(writer, "not found", http.StatusNotFound)
		}
	}))
	defer server.Close()

	renderer := newPinnedTestRenderer(t, server.URL)
	if err := renderer.Ready(); err != nil {
		t.Fatalf("Ready() = %v", err)
	}
	for _, format := range []renderjob.Format{renderjob.PDF, renderjob.PNG} {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		output, err := renderer.Render(ctx, renderjob.Navigation{
			ResumeID:   uuid.MustParse("00000000-0000-0000-0000-000000000001"),
			JobID:      uuid.MustParse("00000000-0000-0000-0000-000000000002"),
			Capability: "abcdefghijklmnopqrstuvwxyzABCDEFGH012345678",
			Format:     format,
		})
		cancel()
		if err != nil {
			mu.Lock()
			gotRequests := append([]string(nil), requests...)
			mu.Unlock()
			t.Fatalf("Render(%s) = %v; requests = %v", format, err, gotRequests)
		}
		if len(output) == 0 {
			t.Fatalf("Render(%s) returned no bytes", format)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 6 {
		t.Fatalf("upstream requests = %v, want two documents and four stylesheets", requests)
	}
	for _, request := range requests {
		if request != "GET /print/00000000-0000-0000-0000-000000000001" && request != "GET /_nuxt/assets/print-fonts.css" && request != "GET /_nuxt/assets/print.css" {
			t.Fatalf("unexpected outbound request: %s", request)
		}
	}
}

func TestPinnedBrowserCancelsMaliciousSubresource(t *testing.T) {
	if os.Getenv("ABOUTME_RUN_BROWSER_TEST") != "1" {
		t.Skip("set ABOUTME_RUN_BROWSER_TEST=1 for the pinned-browser proof")
	}
	var unexpected atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/print/00000000-0000-0000-0000-000000000001" {
			unexpected.Store(true)
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.Header().Set("Content-Security-Policy", printContentSecurityPolicy)
		writeTestResponse(t, writer, `<!doctype html><html><head><link rel="stylesheet" href="/_nuxt/assets/evil.css"></head><body><main data-print-document="true">Resume</main></body></html>`)
	}))
	defer server.Close()
	renderer := newPinnedTestRenderer(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err := renderer.Render(ctx, renderjob.Navigation{
		ResumeID:   uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		JobID:      uuid.MustParse("00000000-0000-0000-0000-000000000002"),
		Capability: "abcdefghijklmnopqrstuvwxyzABCDEFGH012345678",
		Format:     renderjob.PDF,
	})
	if !errors.Is(err, ErrRenderFailed) {
		t.Fatalf("error = %v, want ErrRenderFailed", err)
	}
	if unexpected.Load() {
		t.Fatal("unlisted subresource reached the fixed upstream")
	}
}

func TestPinnedBrowserRejectsFailedFontsAndImages(t *testing.T) {
	if os.Getenv("ABOUTME_RUN_BROWSER_TEST") != "1" {
		t.Skip("set ABOUTME_RUN_BROWSER_TEST=1 for the pinned-browser proof")
	}
	for _, test := range []struct {
		name     string
		fontCSS  string
		fontBody string
		image    string
	}{
		{
			name:     "failed font",
			fontCSS:  "@font-face{font-family:broken;src:url('/_nuxt/fonts/inter-var.woff2')}body{font-family:broken}",
			fontBody: "not-a-font",
			image:    "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==",
		},
		{
			name:     "failed print-only font",
			fontCSS:  "@font-face{font-family:broken;src:url('/_nuxt/fonts/inter-var.woff2')}@media print{body{font-family:broken}}",
			fontBody: "not-a-font",
			image:    "data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==",
		},
		{
			name:  "failed image",
			image: "data:image/png;base64,AAAA",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var fontRequested atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/print/00000000-0000-0000-0000-000000000001":
					writer.Header().Set("Content-Type", "text/html; charset=utf-8")
					writer.Header().Set("Content-Security-Policy", printContentSecurityPolicy)
					writeTestResponse(t, writer, `<!doctype html><html><head><link rel="stylesheet" href="/_nuxt/assets/print-fonts.css"><link rel="stylesheet" href="/_nuxt/assets/print.css"></head><body><main data-print-document="true">Resume<img alt="test" src="`+test.image+`"></main></body></html>`)
				case "/_nuxt/assets/print-fonts.css":
					writer.Header().Set("Content-Type", "text/css")
					writeTestResponse(t, writer, test.fontCSS)
				case "/_nuxt/assets/print.css":
					writer.Header().Set("Content-Type", "text/css")
				case "/_nuxt/fonts/inter-var.woff2":
					fontRequested.Store(true)
					writer.Header().Set("Content-Type", "font/woff2")
					writeTestResponse(t, writer, test.fontBody)
				default:
					http.Error(writer, "not found", http.StatusNotFound)
				}
			}))
			defer server.Close()
			renderer := newPinnedTestRenderer(t, server.URL)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_, err := renderer.Render(ctx, validTestNavigation(renderjob.PDF))
			if !errors.Is(err, ErrRenderFailed) {
				t.Fatalf("error = %v, want ErrRenderFailed; font requested = %v", err, fontRequested.Load())
			}
		})
	}
}

func TestPinnedBrowserRejectsFailedStylesheets(t *testing.T) {
	if os.Getenv("ABOUTME_RUN_BROWSER_TEST") != "1" {
		t.Skip("set ABOUTME_RUN_BROWSER_TEST=1 for the pinned-browser proof")
	}
	for _, test := range []struct {
		name        string
		status      int
		contentType string
		disconnect  bool
	}{
		{name: "404", status: http.StatusNotFound, contentType: "text/css"},
		{name: "wrong MIME", status: http.StatusOK, contentType: "text/plain"},
		{name: "network failure", disconnect: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/print/00000000-0000-0000-0000-000000000001":
					writer.Header().Set("Content-Type", "text/html; charset=utf-8")
					writer.Header().Set("Content-Security-Policy", printContentSecurityPolicy)
					writeTestResponse(t, writer, `<!doctype html><html><head><link rel="stylesheet" href="/_nuxt/assets/print-fonts.css"><link rel="stylesheet" href="/_nuxt/assets/print.css"></head><body><main data-print-document="true">Resume</main></body></html>`)
				case "/_nuxt/assets/print-fonts.css":
					writer.Header().Set("Content-Type", "text/css")
				case "/_nuxt/assets/print.css":
					if test.disconnect {
						hijacker, ok := writer.(http.Hijacker)
						if !ok {
							t.Error("response writer does not support hijacking")
							return
						}
						connection, _, err := hijacker.Hijack()
						if err == nil {
							if closeErr := connection.Close(); closeErr != nil {
								t.Error(closeErr)
							}
						}
						return
					}
					writer.Header().Set("Content-Type", test.contentType)
					writer.WriteHeader(test.status)
				default:
					http.Error(writer, "not found", http.StatusNotFound)
				}
			}))
			defer server.Close()

			renderer := newPinnedTestRenderer(t, server.URL)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if _, err := renderer.Render(ctx, validTestNavigation(renderjob.PDF)); !errors.Is(err, ErrRenderFailed) {
				t.Fatalf("error = %v, want ErrRenderFailed", err)
			}
		})
	}
}

func writeTestResponse(t *testing.T, writer http.ResponseWriter, body string) {
	t.Helper()
	if _, err := writer.Write([]byte(body)); err != nil {
		t.Error(err)
	}
}

func TestPinnedBrowserCancellationJoinsProxyAndBrowser(t *testing.T) {
	if os.Getenv("ABOUTME_RUN_BROWSER_TEST") != "1" {
		t.Skip("set ABOUTME_RUN_BROWSER_TEST=1 for the pinned-browser proof")
	}
	started := make(chan struct{})
	finished := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-finished
	}))
	defer server.Close()
	renderer := newPinnedTestRenderer(t, server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := renderer.Render(ctx, validTestNavigation(renderjob.PDF))
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		close(finished)
		t.Fatal("browser did not reach the private document")
	}
	cancel()
	close(finished)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled render retained browser or proxy work")
	}
}

func validTestNavigation(format renderjob.Format) renderjob.Navigation {
	return renderjob.Navigation{
		ResumeID:   uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		JobID:      uuid.MustParse("00000000-0000-0000-0000-000000000002"),
		Capability: "abcdefghijklmnopqrstuvwxyzABCDEFGH012345678",
		Format:     format,
	}
}

func newPinnedTestRenderer(t *testing.T, forwardOrigin string) *Renderer {
	t.Helper()
	executable := os.Getenv("ABOUTME_CHROMIUM_PATH")
	if executable == "" {
		t.Fatal("ABOUTME_CHROMIUM_PATH must name pinned Chromium 151.0.7922.34")
	}
	renderer, err := New(Config{
		BrowserExecutable: executable,
		RenderOrigin:      testRenderOrigin(t),
		testForwardOrigin: forwardOrigin,
	})
	if err != nil {
		t.Fatal(err)
	}
	return renderer
}
