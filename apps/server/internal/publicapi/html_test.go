package publicapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/directrender"
	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/publicformat"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
)

type htmlRoundTrip func(*http.Request) (*http.Response, error)

func (f htmlRoundTrip) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func TestPublicHTMLDiscoverableValidatesWorkerDocumentAndConditionalResponse(t *testing.T) {
	// This fails if an unvalidated worker document, or bytes without the strong body tag, reaches a viewer.
	slug, reader, _ := formatTestReader(t, true)
	snapshot, lease, err := reader.ReadResume(context.Background(), slug, publicstate.RepresentationHTML)
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	origin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	jsonLD, err := publicformat.JSONLD(snapshot.Public, mustPublicOrigin(t), true)
	if err != nil {
		t.Fatal(err)
	}
	body := validHTML("Ada", "https://aboutme.example/ada", "1", string(jsonLD.Script))
	renderer := directrender.New(origin, &http.Client{Transport: htmlRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(bytes.NewBufferString(body))}, nil
	})})
	cache, err := publiccache.New(2, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHTMLHandler(HTMLDependencies{Reader: reader, Cache: cache, Renderer: renderer, PublicOrigin: mustPublicOrigin(t), AppDigest: "sha256:app", RendererDigest: "sha256:renderer"})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ada", nil))
	if w.Code != http.StatusOK || w.Body.String() != body {
		t.Fatalf("response = %d %q", w.Code, w.Body.String())
	}
	if got, want := w.Header().Get("Content-Security-Policy"), jsonLD.CSP; got != want {
		t.Fatalf("CSP = %q, want %q", got, want)
	}
	if got := w.Header().Get("X-Robots-Tag"); got != "" {
		t.Fatalf("X-Robots-Tag = %q", got)
	}
	conditional := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodHead, "/ada", nil)
	request.Header.Set("If-None-Match", w.Header().Get("ETag"))
	handler.ServeHTTP(conditional, request)
	if conditional.Code != http.StatusNotModified || conditional.Body.Len() != 0 || conditional.Header().Get("Content-Length") != "" || conditional.Header().Get("Content-Security-Policy") != jsonLD.CSP {
		t.Fatalf("conditional response = %d %#v %q", conditional.Code, conditional.Header(), conditional.Body.String())
	}
}

func TestPublicHTMLNondiscoverableRejectsUnexpectedScript(t *testing.T) {
	// This fails if a noindex document can add an inline script or miss its robots policy.
	slug, reader, _ := formatTestReader(t, false)
	origin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		html string
		want int
	}{
		{"valid", validHTML("Ada", "https://aboutme.example/ada", "1", ""), http.StatusOK},
		{"extra inline script", validHTML("Ada", "https://aboutme.example/ada", "1", "<script>alert(1)</script>"), http.StatusServiceUnavailable},
		{"wrong canonical", validHTML("Ada", "https://aboutme.example/other", "1", ""), http.StatusServiceUnavailable},
		{"wrong title", validHTML("Grace", "https://aboutme.example/ada", "1", ""), http.StatusServiceUnavailable},
		{"wrong revision", validHTML("Ada", "https://aboutme.example/ada", "2", ""), http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			cache, cacheErr := publiccache.New(2, time.Minute, time.Now)
			if cacheErr != nil {
				t.Fatal(cacheErr)
			}
			renderer := directrender.New(origin, &http.Client{Transport: htmlRoundTrip(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(bytes.NewBufferString(test.html))}, nil
			})})
			handler, handlerErr := NewHTMLHandler(HTMLDependencies{Reader: reader, Cache: cache, Renderer: renderer, PublicOrigin: mustPublicOrigin(t), AppDigest: "sha256:app", RendererDigest: "sha256:renderer"})
			if handlerErr != nil {
				t.Fatal(handlerErr)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/"+slug, nil))
			if w.Code != test.want {
				t.Fatalf("status = %d, want %d", w.Code, test.want)
			}
			if test.want == http.StatusOK {
				if got, want := w.Header().Get("Content-Security-Policy"), publicformat.BaseCSP; got != want {
					t.Fatalf("CSP = %q, want %q", got, want)
				}
				if got, want := w.Header().Get("X-Robots-Tag"), "noindex, noarchive"; got != want {
					t.Fatalf("X-Robots-Tag = %q, want %q", got, want)
				}
			}
		})
	}
}

func TestPublicHTMLRejectsWrongMainAssetJSONLDAndOversize(t *testing.T) {
	// This fails if an incomplete SSR shell, changed data script, or capped renderer output is served.
	slug, reader, _ := formatTestReader(t, true)
	snapshot, lease, err := reader.ReadResume(context.Background(), slug, publicstate.RepresentationHTML)
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	jsonLD, err := publicformat.JSONLD(snapshot.Public, mustPublicOrigin(t), true)
	if err != nil {
		t.Fatal(err)
	}
	valid := validHTML("Ada", "https://aboutme.example/ada", "1", string(jsonLD.Script))
	origin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		html string
	}{
		{"wrong main", strings.Replace(valid, `id="public-resume"`, `id="not-public"`, 1)},
		{"wrong asset", strings.Replace(valid, "/_nuxt/assets/public-resume.mjs", "/_nuxt/assets/other.mjs", 1)},
		{"wrong JSON-LD", strings.Replace(valid, `"@context":"https://schema.org"`, `"@context":"https://invalid.example"`, 1)},
		{"oversize", strings.Repeat("x", 2_097_153)},
	} {
		t.Run(test.name, func(t *testing.T) {
			cache, err := publiccache.New(2, time.Minute, time.Now)
			if err != nil {
				t.Fatal(err)
			}
			renderer := directrender.New(origin, &http.Client{Transport: htmlRoundTrip(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(test.html))}, nil
			})})
			handler, err := NewHTMLHandler(HTMLDependencies{Reader: reader, Cache: cache, Renderer: renderer, PublicOrigin: mustPublicOrigin(t), AppDigest: "sha256:app", RendererDigest: "sha256:renderer"})
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/"+slug, nil))
			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", w.Code)
			}
		})
	}
}

func TestPublicHTMLHoldsLeaseThroughResponseWrite(t *testing.T) {
	// This fails if a revocation can drain before the admitted HTML response finishes writing.
	slug, reader, coordinator := formatTestReader(t, false)
	origin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	cache, err := publiccache.New(2, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	renderer := directrender.New(origin, &http.Client{Transport: htmlRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(validHTML("Ada", "https://aboutme.example/ada", "1", "")))}, nil
	})})
	handler, err := NewHTMLHandler(HTMLDependencies{Reader: reader, Cache: cache, Renderer: renderer, PublicOrigin: mustPublicOrigin(t), AppDigest: "sha256:app", RendererDigest: "sha256:renderer"})
	if err != nil {
		t.Fatal(err)
	}
	writer := &formatBlockingWriter{header: make(http.Header), wrote: make(chan struct{}), unblock: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(writer, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/"+slug, nil))
		close(done)
	}()
	select {
	case <-writer.wrote:
	case <-time.After(time.Second):
		t.Fatal("response never began writing")
	}
	transition, err := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: formatResumeID, ExpectedRevision: 1, Class: publicstate.Revoking}}})
	if err != nil {
		t.Fatal(err)
	}
	drained := make(chan error, 1)
	go func() { drained <- transition.Close(context.Background(), time.Now().Add(time.Second)) }()
	select {
	case err := <-drained:
		t.Fatalf("revocation drained before response completed: %v", err)
	default:
	}
	close(writer.unblock)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not finish response")
	}
	if err := <-drained; err != nil {
		t.Fatal(err)
	}
	if err := transition.Rollback(); err != nil {
		t.Fatal(err)
	}
}

func TestPublicHTMLRejectsUnexpectedResourceLoads(t *testing.T) {
	// This fails if a renderer response can load an unapproved resource despite Go's CSP boundary.
	slug, reader, _ := formatTestReader(t, false)
	origin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	base := validHTML("Ada", "https://aboutme.example/ada", "1", "")
	for _, test := range []struct {
		name, injection string
	}{
		{"unexpected image", `<img src="https://evil.example/pixel" alt="">`},
		{"stylesheet link", `<link rel="stylesheet" href="https://evil.example/style.css">`},
		{"iframe", `<iframe src="https://evil.example/frame"></iframe>`},
		{"form action", `<form action="https://evil.example/post"></form>`},
		{"script event", `<main id="other" onload="x()"></main>`},
	} {
		t.Run(test.name, func(t *testing.T) {
			cache, err := publiccache.New(2, time.Minute, time.Now)
			if err != nil {
				t.Fatal(err)
			}
			html := strings.Replace(base, "</body>", test.injection+"</body>", 1)
			renderer := directrender.New(origin, &http.Client{Transport: htmlRoundTrip(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(html))}, nil
			})})
			handler, err := NewHTMLHandler(HTMLDependencies{Reader: reader, Cache: cache, Renderer: renderer, PublicOrigin: mustPublicOrigin(t), AppDigest: "sha256:app", RendererDigest: "sha256:renderer"})
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/"+slug, nil))
			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", w.Code)
			}
		})
	}
}

func TestPublicHTMLAllowsOnlyTheProjectedPhotoURL(t *testing.T) {
	// This fails if a document can substitute a non-projected image URL.
	origin := mustPublicOrigin(t)
	resume := publicresume.PublicResume{
		Slug:     "ada",
		Revision: "1",
		Document: publicresume.PublicResumeDocument{PersonalDetails: publicresume.PublicPersonalDetails{
			FullName: "Ada",
			Photo:    &publicresume.PublicPhoto{URL: "https://aboutme.example/api/v1/public/resumes/ada/photo"},
		}},
	}
	jsonLD, err := publicformat.JSONLD(resume, origin, false)
	if err != nil {
		t.Fatal(err)
	}
	html := strings.Replace(validHTML("Ada", "https://aboutme.example/ada", "1", ""), ">body</main>", `><img src="https://aboutme.example/api/v1/public/resumes/ada/photo" alt="">body</main>`, 1)
	if !validPublicHTML([]byte(html), resume, origin, jsonLD, false) {
		t.Fatal("projected photo was rejected")
	}
	wrong := strings.Replace(html, "/api/v1/public/resumes/ada/photo", "/api/v1/public/resumes/other/photo", 1)
	if validPublicHTML([]byte(wrong), resume, origin, jsonLD, false) {
		t.Fatal("substituted photo URL was accepted")
	}
}

func TestPublicHTMLClassifiesNotFoundAndUnavailableReads(t *testing.T) {
	// This fails if a closed generation is made indistinguishable from absence, or absence exposes a retry signal.
	slug, reader, coordinator := formatTestReader(t, false)
	origin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	cache, err := publiccache.New(2, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	renderer := directrender.New(origin, &http.Client{Transport: htmlRoundTrip(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("renderer must not run")
	})})
	handler, err := NewHTMLHandler(HTMLDependencies{Reader: reader, Cache: cache, Renderer: renderer, PublicOrigin: mustPublicOrigin(t), AppDigest: "sha256:app", RendererDigest: "sha256:renderer"})
	if err != nil {
		t.Fatal(err)
	}
	notFound := httptest.NewRecorder()
	handler.ServeHTTP(notFound, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/missing", nil))
	if notFound.Code != http.StatusNotFound || notFound.Header().Get("Retry-After") != "" || notFound.Header().Get("ETag") != "" {
		t.Fatalf("not found response = %d %#v", notFound.Code, notFound.Header())
	}
	transition, err := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: formatResumeID, ExpectedRevision: 1, Class: publicstate.Revoking}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := transition.Close(context.Background(), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	unavailable := httptest.NewRecorder()
	handler.ServeHTTP(unavailable, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/"+slug, nil))
	if unavailable.Code != http.StatusServiceUnavailable || unavailable.Header().Get("Retry-After") != "1" || unavailable.Header().Get("ETag") != "" {
		t.Fatalf("unavailable response = %d %#v", unavailable.Code, unavailable.Header())
	}
	if err := transition.Rollback(); err != nil {
		t.Fatal(err)
	}
}

func TestPublicHTMLReleasesLeaseAfterRendererFailure(t *testing.T) {
	// This fails if a renderer failure leaves an admitted generation held after its 503 is complete.
	slug, reader, coordinator := formatTestReader(t, false)
	origin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	cache, err := publiccache.New(2, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	renderer := directrender.New(origin, &http.Client{Transport: htmlRoundTrip(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("direct renderer failed")
	})})
	handler, err := NewHTMLHandler(HTMLDependencies{Reader: reader, Cache: cache, Renderer: renderer, PublicOrigin: mustPublicOrigin(t), AppDigest: "sha256:app", RendererDigest: "sha256:renderer"})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/"+slug, nil))
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") != "1" {
		t.Fatalf("renderer failure response = %d %#v", w.Code, w.Header())
	}
	transition, err := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: formatResumeID, ExpectedRevision: 1, Class: publicstate.Revoking}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := transition.Close(context.Background(), time.Now().Add(time.Second)); err != nil {
		t.Fatalf("lease remained after renderer failure: %v", err)
	}
	if err := transition.Rollback(); err != nil {
		t.Fatal(err)
	}
}

func TestPublicHTMLRejectsMissingAndHostileAnchors(t *testing.T) {
	// This fails if the SSR shell lacks its sole skip link or a public field injects a non-navigation URL.
	origin := mustPublicOrigin(t)
	resume := publicresume.PublicResume{Slug: "ada", Revision: "1", Document: publicresume.PublicResumeDocument{PersonalDetails: publicresume.PublicPersonalDetails{FullName: "Ada"}}}
	jsonLD, err := publicformat.JSONLD(resume, origin, false)
	if err != nil {
		t.Fatal(err)
	}
	valid := validHTML("Ada", "https://aboutme.example/ada", "1", "")
	if !validPublicHTML([]byte(valid), resume, origin, jsonLD, false) {
		t.Fatal("valid skip link was rejected")
	}
	for _, test := range []struct{ name, href string }{
		{"missing skip", ""},
		{"javascript", "javascript:alert(1)"},
		{"data", "data:text/html,x"},
		{"http", "http://example.test"},
		{"protocol relative", "//example.test"},
		{"mixed case HTTPS", "HTTPS://example.test"},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			if test.name == "missing skip" {
				candidate = strings.Replace(candidate, `<a href="#public-resume">Skip to content</a>`, "", 1)
			} else {
				candidate = strings.Replace(candidate, `</body>`, `<a href="`+test.href+`">hostile</a></body>`, 1)
			}
			if validPublicHTML([]byte(candidate), resume, origin, jsonLD, false) {
				t.Fatalf("accepted anchor %q", test.href)
			}
		})
	}
}

func TestPublicHTMLRejectsHostileMetaAndInlineCSS(t *testing.T) {
	// This fails if the direct renderer can trigger a navigation or resource load through CSP-permitted markup.
	origin := mustPublicOrigin(t)
	resume := publicresume.PublicResume{Slug: "ada", Revision: "1", Document: publicresume.PublicResumeDocument{PersonalDetails: publicresume.PublicPersonalDetails{FullName: "Ada"}}}
	jsonLD, err := publicformat.JSONLD(resume, origin, false)
	if err != nil {
		t.Fatal(err)
	}
	valid := validHTML("Ada", "https://aboutme.example/ada", "1", "")
	for _, test := range []struct {
		name, html string
	}{
		{"refresh", strings.Replace(valid, "</head>", `<meta http-equiv="refresh" content="0;url=https://evil.example">`+"</head>", 1)},
		{"extra meta", strings.Replace(valid, "</head>", `<meta name="description" content="untrusted">`+"</head>", 1)},
		{"style element", strings.Replace(valid, "</head>", `<style>body{background:url(https://evil.example/x)}</style>`+"</head>", 1)},
		{"url function", strings.Replace(valid, "</body>", `<div style="background:url(https://evil.example/x)"></div></body>`, 1)},
		{"image set", strings.Replace(valid, "</body>", `<div style="background:IMAGE-SET(url(https://evil.example/x) 1x)"></div></body>`, 1)},
		{"escaped url", strings.Replace(valid, "</body>", `<div style="background:u\72l(https://evil.example/x)"></div></body>`, 1)},
		{"comment obfuscated url", strings.Replace(valid, "</body>", `<div style="background:u/**/rl(https://evil.example/x)"></div></body>`, 1)},
		{"import", strings.Replace(valid, "</body>", `<div style="@import url(https://evil.example/x)"></div></body>`, 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if validPublicHTML([]byte(test.html), resume, origin, jsonLD, false) {
				t.Fatal("hostile document was accepted")
			}
		})
	}
	safe := strings.Replace(valid, "</body>", `<div style="color: rgb(1, 2, 3); --tone: #fff"></div></body>`, 1)
	if !validPublicHTML([]byte(safe), resume, origin, jsonLD, false) {
		t.Fatal("safe renderer inline style was rejected")
	}
}

func mustPublicOrigin(t *testing.T) publicresume.PublicOrigin {
	t.Helper()
	origin, err := publicresume.ParsePublicOrigin("https://aboutme.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	return origin
}

func validHTML(name, canonical, revision, dataScript string) string {
	return "<!doctype html><html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width, initial-scale=1\"><title>" + name + " — Resume</title><link rel=\"canonical\" href=\"" + canonical + "\">" + dataScript + "</head><body><a href=\"#public-resume\">Skip to content</a><main id=\"public-resume\" data-revision=\"" + revision + "\">body</main><script type=\"module\" src=\"/_nuxt/assets/public-resume.mjs\"></script></body></html>"
}
