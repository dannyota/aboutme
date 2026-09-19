package publicapi

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/directrender"
	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/publicformat"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

// A second <main> inside the page's own, as a two-column resume once
// rendered, breaks the one-main rule and must name that rule.
const nestedMain = `<main id="public-resume" data-revision="1"><div class="layout-two-columns"><main class="resume-main">body</main></div></main>`

func TestPublicHTMLRejectionNamesTheBrokenRule(t *testing.T) {
	origin := mustPublicOrigin(t)
	resume := publicresume.PublicResume{Slug: "ada", Revision: "1", Document: publicresume.PublicResumeDocument{PersonalDetails: publicresume.PublicPersonalDetails{FullName: "Ada"}}}
	jsonLD, err := publicformat.JSONLD(resume, origin, false)
	if err != nil {
		t.Fatal(err)
	}
	valid := validHTML("Ada", "https://aboutme.example/ada", "1", "")
	if rule := publicHTMLRejection([]byte(valid), resume, origin, jsonLD, false); rule != "" {
		t.Fatalf("valid page rejected by %q", rule)
	}
	for _, test := range []struct{ name, from, to, want string }{
		{"nested main", `<main id="public-resume" data-revision="1">body</main>`, nestedMain, "required_elements"},
		{"http anchor", `</body>`, `<a href="http://example.test">x</a></body>`, "anchor_scheme"},
		{"anchor without href", `</body>`, `<a>x</a></body>`, "anchor_href"},
		{"inline style url", `</body>`, `<span style="background: url(x)">x</span></body>`, "inline_style"},
		{"event attribute", `</body>`, `<span onclick="x">x</span></body>`, "event_attribute"},
		{"style element", `</head>`, `<style>p{}</style></head>`, "style_element"},
		{"wrong title", `<title>Ada — Resume</title>`, `<title>Someone — Resume</title>`, "title"},
		{"iframe", `</body>`, `<iframe></iframe></body>`, "resource_element"},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := strings.Replace(valid, test.from, test.to, 1)
			if candidate == valid {
				t.Fatalf("fixture replacement %q did not apply", test.from)
			}
			if got := publicHTMLRejection([]byte(candidate), resume, origin, jsonLD, false); got != test.want {
				t.Fatalf("rule = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPublicHTMLLogsTheClosedReasonForA503(t *testing.T) {
	slug, reader, _ := formatTestReader(t, false)
	origin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	body := strings.Replace(validHTML("Ada", "https://aboutme.example/ada", "1", ""),
		`<main id="public-resume" data-revision="1">body</main>`, nestedMain, 1)
	renderer := directrender.New(origin, &http.Client{Transport: htmlRoundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(bytes.NewBufferString(body))}, nil
	})})
	cache, err := publiccache.New(2, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	handler, err := NewHTMLHandler(HTMLDependencies{Reader: reader, Cache: cache, Renderer: renderer, PublicOrigin: mustPublicOrigin(t), AppDigest: "sha256:app", RendererDigest: "sha256:renderer", Logger: slog.New(slog.NewTextHandler(&logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/"+slug, nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	logged := logs.String()
	if !strings.Contains(logged, `msg="publicapi: html unavailable" reason=html_rejected rule=required_elements`) {
		t.Fatalf("log = %q, want the closed reason and rule", logged)
	}
	for _, content := range []string{slug, "Ada", "resume-main"} {
		if strings.Contains(logged, content) {
			t.Fatalf("log leaks %q: %q", content, logged)
		}
	}
}

// The page may link exactly the two self-hosted resume stylesheets, once each,
// so the template's CSS and fonts load under style-src 'self'. Any other
// stylesheet is rejected.
func TestPublicHTMLAllowsOnlyTheResumeStylesheets(t *testing.T) {
	origin := mustPublicOrigin(t)
	resume := publicresume.PublicResume{Slug: "ada", Revision: "1", Document: publicresume.PublicResumeDocument{PersonalDetails: publicresume.PublicPersonalDetails{FullName: "Ada"}}}
	jsonLD, err := publicformat.JSONLD(resume, origin, false)
	if err != nil {
		t.Fatal(err)
	}
	const sheets = `<link rel="stylesheet" href="/_nuxt/assets/print-fonts.css?v=0123456789abcdef"><link rel="stylesheet" href="/_nuxt/assets/print.css?v=0123456789abcdef">`
	base := validHTML("Ada", "https://aboutme.example/ada", "1", "")
	styled := strings.Replace(base, `</head>`, sheets+`</head>`, 1)
	if rule := publicHTMLRejection([]byte(styled), resume, origin, jsonLD, false); rule != "" {
		t.Fatalf("page with the resume stylesheets rejected by %q", rule)
	}
	for _, test := range []struct{ name, extra string }{
		{"foreign stylesheet", `<link rel="stylesheet" href="https://evil.example/x.css">`},
		{"other local stylesheet", `<link rel="stylesheet" href="/_nuxt/assets/other.css">`},
		{"duplicate stylesheet", `<link rel="stylesheet" href="/_nuxt/assets/print.css?v=0123456789abcdef">`},
		{"preload", `<link rel="preload" href="/_nuxt/assets/print.css?v=0123456789abcdef" as="style">`},
		{"extra attribute", `<link rel="stylesheet" href="/_nuxt/assets/print.css?v=0123456789abcdef" media="print">`},
		// Unversioned or malformed versions would pin a year-old cached copy.
		{"unversioned", `<link rel="stylesheet" href="/_nuxt/assets/print.css">`},
		{"short version", `<link rel="stylesheet" href="/_nuxt/assets/print.css?v=0123">`},
		{"uppercase version", `<link rel="stylesheet" href="/_nuxt/assets/print.css?v=0123456789ABCDEF">`},
		{"extra query", `<link rel="stylesheet" href="/_nuxt/assets/print.css?v=0123456789abcdef&x=1">`},
		{"other query key", `<link rel="stylesheet" href="/_nuxt/assets/print.css?w=0123456789abcdef">`},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := strings.Replace(styled, `</head>`, test.extra+`</head>`, 1)
			if rule := publicHTMLRejection([]byte(candidate), resume, origin, jsonLD, false); rule != "stylesheet" {
				t.Fatalf("rule = %q, want stylesheet", rule)
			}
		})
	}
}

// The public page may link its own PDF, exactly once, only while download is
// enabled. Every other relative href stays rejected.
func TestPublicHTMLAllowsOnlyTheOwnPDFDownloadLink(t *testing.T) {
	origin := mustPublicOrigin(t)
	resume := publicresume.PublicResume{Slug: "ada", Revision: "1", DownloadEnabled: true,
		Document: publicresume.PublicResumeDocument{PersonalDetails: publicresume.PublicPersonalDetails{FullName: "Ada"}}}
	jsonLD, err := publicformat.JSONLD(resume, origin, false)
	if err != nil {
		t.Fatal(err)
	}
	valid := validHTML("Ada", "https://aboutme.example/ada", "1", "")
	link := `<a class="public-download" href="/api/v1/public/resumes/ada/pdf">Download PDF</a>`
	withLink := strings.Replace(valid, `data-revision="1">`, `data-revision="1">`+link, 1)
	if withLink == valid {
		t.Fatal("fixture replacement did not apply")
	}
	if rule := publicHTMLRejection([]byte(withLink), resume, origin, jsonLD, false); rule != "" {
		t.Fatalf("own PDF link rejected by %q", rule)
	}

	disabled := resume
	disabled.DownloadEnabled = false
	if rule := publicHTMLRejection([]byte(withLink), disabled, origin, jsonLD, false); rule != "download_link" {
		t.Fatalf("link with download disabled: rule = %q, want download_link", rule)
	}
	twice := strings.Replace(withLink, link, link+link, 1)
	if rule := publicHTMLRejection([]byte(twice), resume, origin, jsonLD, false); rule != "download_link" {
		t.Fatalf("two links: rule = %q, want download_link", rule)
	}
	for _, href := range []string{
		"/api/v1/public/resumes/bob/pdf",
		"/api/v1/public/resumes/ada/pdf?x=1",
		"/api/v1/public/resumes/ada/og.png",
		"/api/v1/resumes/ada/pdf",
		"//aboutme.example/api/v1/public/resumes/ada/pdf",
		"/ada",
	} {
		candidate := strings.Replace(withLink, `href="/api/v1/public/resumes/ada/pdf"`, `href="`+href+`"`, 1)
		if rule := publicHTMLRejection([]byte(candidate), resume, origin, jsonLD, false); rule != "anchor_scheme" {
			t.Fatalf("href %q: rule = %q, want anchor_scheme", href, rule)
		}
	}
}
