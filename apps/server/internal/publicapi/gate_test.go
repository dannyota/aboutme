package publicapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/directrender"
	"github.com/dannyota/aboutme/apps/server/internal/previewmeta"
	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/viewpass"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

// gateTestKey is a fixed 32-byte VIEW_PASS_KEY for gate tests.
var gateTestKey = bytes.Repeat([]byte{0x11}, 32)

var gateTestResumeID = uuid.MustParse("00000000-0000-0000-0000-0000000000a1")

// gateValidHTML builds gate HTML that passes gateHTMLRejection for envelope,
// mirroring how Nuxt is expected to render it (design "Gate"). With no card
// image URL the head carries no image element.
func gateValidHTML(envelope directrender.GateRenderRequest, origin publicresume.PublicOrigin) string {
	head := gatePreviewHead(envelope, origin)
	favicon := ""
	if envelope.FaviconHref != "" {
		favicon = `<link rel="icon" href="` + envelope.FaviconHref + `">`
	}
	anchors := ""
	for _, provider := range envelope.Providers {
		anchors += `<a href="/api/v1/auth/` + provider + `/start?purpose=view&amp;slug=` + envelope.Slug + `">Continue</a>`
	}
	anchors += `<a href="` + origin.Resolve("/") + `">Home</a>`
	return "<!doctype html><html lang=\"" + envelope.Lng + "\"><head>" +
		`<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">` +
		"<title>" + envelope.PageTitle + "</title>" + favicon +
		`<link rel="canonical" href="` + origin.Resolve("/"+envelope.Slug) + `">` + head +
		`<link rel="stylesheet" href="/_nuxt/assets/print-fonts.css?v=0123456789abcdef">` +
		"</head><body>" + `<a href="#public-gate">Skip to content</a>` +
		`<main id="public-gate">` + anchors + "</main></body></html>"
}

// gatePreviewHead is the preview head Nuxt writes for the gate: every preview
// element, less the image elements when the envelope has no card image URL.
func gatePreviewHead(envelope directrender.GateRenderRequest, origin publicresume.PublicOrigin) string {
	var out strings.Builder
	for _, tag := range previewTags(publicresume.PublicResume{Slug: envelope.Slug}, envelope.Preview, origin.String()) {
		if envelope.Preview.ImageURL == "" && (strings.Contains(tag.name, "image") || tag.name == "twitter:card") {
			continue
		}
		out.WriteString(tag.html())
	}
	return out.String()
}

func gateTestEnvelope() directrender.GateRenderRequest {
	return directrender.GateRenderRequest{
		Mode: directrender.GateRenderMode, CanonicalOrigin: "https://aboutme.example",
		Slug: "ada", Lng: "en", PageTitle: "Ada — Resume",
		Preview:   previewmeta.Meta{Title: "Ada", Description: "Ada's resume"},
		Providers: []string{"google", "linkedin"}, Message: "none",
	}
}

func TestGateHTMLRejection_AcceptsValidGate(t *testing.T) {
	envelope := gateTestEnvelope()
	origin := mustPublicOrigin(t)
	source := gateValidHTML(envelope, origin)
	if rule := gateHTMLRejection([]byte(source), envelope, origin); rule != "" {
		t.Fatalf("gateHTMLRejection() = %q, want \"\" for valid gate HTML:\n%s", rule, source)
	}
}

func TestGateHTMLRejection_RejectsClosedViolations(t *testing.T) {
	envelope := gateTestEnvelope()
	origin := mustPublicOrigin(t)
	valid := gateValidHTML(envelope, origin)

	cases := []struct {
		name string
		body string
	}{
		{"script", replaceOnce(valid, "</head>", "<script>alert(1)</script></head>")},
		{"extra anchor", replaceOnce(valid, "</main>", `<a href="https://evil.example/">x</a></main>`)},
		{"form", replaceOnce(valid, "<main", `<form></form><main`)},
		{"image", replaceOnce(valid, "<main", `<img src="https://aboutme.example/x.png"><main`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if rule := gateHTMLRejection([]byte(tc.body), envelope, origin); rule == "" {
				t.Fatalf("gateHTMLRejection() accepted a %s violation, want a rejection rule", tc.name)
			}
		})
	}
}

// TestGateHTMLRejection_ImageMetaNeedsCardURL proves a gate with no card
// image URL may carry no og:image or twitter:image meta, and that a gate with
// one carries exactly that URL (design "Gate"; AC-VIEW-002).
func TestGateHTMLRejection_ImageMetaNeedsCardURL(t *testing.T) {
	origin := mustPublicOrigin(t)
	noCard := gateTestEnvelope()
	valid := gateValidHTML(noCard, origin)
	if strings.Contains(valid, "og:image") || strings.Contains(valid, "twitter:image") {
		t.Fatalf("fixture for a gate with no card URL carries image meta:\n%s", valid)
	}
	if rule := gateHTMLRejection([]byte(valid), noCard, origin); rule != "" {
		t.Fatalf("gateHTMLRejection() = %q for a gate with no image meta, want \"\"", rule)
	}
	for _, name := range []string{"og:image", "twitter:image"} {
		attr := "property"
		if strings.HasPrefix(name, "twitter") {
			attr = "name"
		}
		injected := replaceOnce(valid, "</head>",
			`<meta `+attr+`="`+name+`" content="`+origin.Resolve("/api/v1/public/resumes/ada/og.png")+`"></head>`)
		if rule := gateHTMLRejection([]byte(injected), noCard, origin); rule == "" {
			t.Errorf("gateHTMLRejection() accepted %s with no card image URL, want a rejection", name)
		}
	}

	withCard := gateTestEnvelope()
	withCard.Preview.ImageURL = origin.Resolve("/api/v1/public/resumes/ada/og/abc.png")
	cardHTML := gateValidHTML(withCard, origin)
	if rule := gateHTMLRejection([]byte(cardHTML), withCard, origin); rule != "" {
		t.Fatalf("gateHTMLRejection() = %q for a gate with a card image, want \"\"", rule)
	}
	if rule := gateHTMLRejection([]byte(valid), withCard, origin); rule == "" {
		t.Error("gateHTMLRejection() accepted a gate missing its card image meta, want a rejection")
	}
}

// replaceOnce replaces the first occurrence of old with new. old must be
// present exactly once in the fixture, or the case is checking the wrong
// baseline.
func replaceOnce(s, old, replacement string) string {
	if strings.Count(s, old) != 1 {
		panic("replaceOnce: " + old + " must appear exactly once in the fixture")
	}
	return strings.Replace(s, old, replacement, 1)
}

func TestHasValidPass(t *testing.T) {
	now := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	resumeID := gateTestResumeID
	valid := viewpass.Seal(gateTestKey, resumeID, 1, now.Add(time.Hour))

	makeRequest := func(cookieValue string) *http.Request {
		r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ada", nil)
		if cookieValue != "" {
			r.AddCookie(&http.Cookie{Name: viewpass.CookieName, Value: cookieValue})
		}
		return r
	}

	if !hasValidPass(makeRequest(valid), gateTestKey, resumeID, 1, clock) {
		t.Error("hasValidPass() = false for a fresh, matching pass, want true")
	}
	if hasValidPass(makeRequest(""), gateTestKey, resumeID, 1, clock) {
		t.Error("hasValidPass() = true with no cookie, want false")
	}
	if hasValidPass(makeRequest(valid), gateTestKey, resumeID, 2, clock) {
		t.Error("hasValidPass() = true for an old pass epoch, want false")
	}
	if hasValidPass(makeRequest(valid), gateTestKey, uuid.New(), 1, clock) {
		t.Error("hasValidPass() = true for another resume's pass, want false")
	}
	expired := viewpass.Seal(gateTestKey, resumeID, 1, now.Add(-time.Second))
	if hasValidPass(makeRequest(expired), gateTestKey, resumeID, 1, clock) {
		t.Error("hasValidPass() = true for an expired pass, want false")
	}
	badTag := valid[:len(valid)-2] + "zz"
	if hasValidPass(makeRequest(badTag), gateTestKey, resumeID, 1, clock) {
		t.Error("hasValidPass() = true for a tampered tag, want false")
	}
}

func TestGateMessage_OnlyClosedValues(t *testing.T) {
	cases := map[string]string{
		"":                  "none",
		"?signin=cancelled": "cancelled", //nolint:misspell // Exact wire value uses double-L "cancelled".
		"?signin=failed":    "failed",
		"?signin=anything":  "none",
		"?other=1":          "none",
	}
	for query, want := range cases {
		r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ada"+query, nil)
		if got := gateMessage(r); got != want {
			t.Errorf("gateMessage(%q) = %q, want %q", query, got, want)
		}
	}
}

// gateHTMLRenderer renders body for every RenderGate call, for the
// integration tests below.
type gateHTMLRoundTrip func(*http.Request) (*http.Response, error)

func (f gateHTMLRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// signInResumeReader builds a Reader over one live, sign-in-to-view resume.
func signInResumeReader(t *testing.T, epoch int32) (*publicresume.Reader, string) {
	t.Helper()
	slug := "ada"
	pd := []byte(`{"fullName":"Ada"}`)
	content := []byte(`{}`)
	customization := []byte(`{}`)
	lng := "en"
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	origin := mustPublicOrigin(t)
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{
		Store: formatReadStore{row: store.Resume{
			ID: gateTestResumeID, Slug: &slug, Live: true, SEOGeoEnabled: false, Revision: 1, Lng: &lng,
			SchemaVersion: int32(schema.CurrentVersion), PersonalDetails: pd, Content: content, Customization: customization,
			SignInToView: true, ViewPassEpoch: epoch,
		}},
		Projector: docmigrate.NewIdentityProjector(), Coordinator: coordinator, Origin: origin,
	})
	if err != nil {
		t.Fatal(err)
	}
	return reader, slug
}

func TestServeGate_NoPassServesGateAndValidPassServesResume(t *testing.T) {
	reader, slug := signInResumeReader(t, 1)
	origin := mustPublicOrigin(t)
	renderOrigin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	renderer := directrender.New(renderOrigin, &http.Client{Transport: gateHTMLRoundTrip(func(r *http.Request) (*http.Response, error) {
		body, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Fatal(readErr)
		}
		var mode struct {
			Mode string `json:"mode"`
		}
		if unmarshalErr := json.Unmarshal(body, &mode); unmarshalErr != nil {
			t.Fatal(unmarshalErr)
		}
		var html string
		if mode.Mode == directrender.GateRenderMode {
			var envelope directrender.GateRenderRequest
			if unmarshalErr := json.Unmarshal(body, &envelope); unmarshalErr != nil {
				t.Fatal(unmarshalErr)
			}
			html = gateValidHTML(envelope, origin)
		} else {
			html = strings.Replace(
				validHTMLIn("en", "Ada", origin.Resolve("/"+slug), "1", ""),
				`data-revision="1">`, `data-revision="1" data-join-invite="/register">`, 1,
			)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(bytes.NewBufferString(html))}, nil
	})})
	cache, err := publiccache.New(4, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	logger, logBuf := newGateCapturingLogger()
	handler, err := NewHTMLHandler(HTMLDependencies{
		Reader: reader, Cache: cache, Renderer: renderer, PublicOrigin: origin,
		AppDigest: "sha256:app", RendererDigest: "sha256:renderer", Logger: logger,
		ViewPassKey: gateTestKey, GateProviders: []string{"google", "linkedin"}, JoinInviteTarget: "/register",
	})
	_ = logBuf
	if err != nil {
		t.Fatal(err)
	}

	// No pass: the gate, never cached, private is moot since gate is no-store.
	noPass := httptest.NewRecorder()
	handler.ServeHTTP(noPass, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/"+slug, nil))
	if noPass.Code != http.StatusOK {
		t.Fatalf("no-pass status = %d, want 200 (the gate); log = %s", noPass.Code, logBuf.String())
	}
	if got := noPass.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("no-pass Cache-Control = %q, want %q", got, "no-store")
	}
	if got := noPass.Header().Get("X-Robots-Tag"); got != "noindex, noarchive" {
		t.Errorf("no-pass X-Robots-Tag = %q, want %q", got, "noindex, noarchive")
	}
	if !bytes.Contains(noPass.Body.Bytes(), []byte(`id="public-gate"`)) {
		t.Errorf("no-pass body = %q, want the gate markup", noPass.Body.String())
	}

	// A valid pass reaches the resume, with a private Cache-Control and the
	// join-invite marker.
	pass := viewpass.Seal(gateTestKey, gateTestResumeID, 1, time.Now().Add(time.Hour))
	withPass := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/"+slug, nil)
	withPass.AddCookie(&http.Cookie{Name: viewpass.CookieName, Value: pass})
	passResp := httptest.NewRecorder()
	handler.ServeHTTP(passResp, withPass)
	if passResp.Code != http.StatusOK {
		t.Fatalf("valid-pass status = %d, want 200 (the resume)", passResp.Code)
	}
	if bytes.Contains(passResp.Body.Bytes(), []byte(`id="public-gate"`)) {
		t.Error("valid-pass body served the gate, want the resume")
	}
	if cc := passResp.Header().Get("Cache-Control"); !bytesContainsString(cc, "private") {
		t.Errorf("valid-pass Cache-Control = %q, want it to contain %q", cc, "private")
	}
	if !bytes.Contains(passResp.Body.Bytes(), []byte(`data-join-invite="/register"`)) {
		t.Errorf("valid-pass body = %q, want the join-invite marker", passResp.Body.String())
	}

	// An old-epoch pass is rejected exactly like no pass, even once the
	// resume-shaped response is cached.
	oldPass := viewpass.Seal(gateTestKey, gateTestResumeID, 0, time.Now().Add(time.Hour))
	oldReq := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/"+slug, nil)
	oldReq.AddCookie(&http.Cookie{Name: viewpass.CookieName, Value: oldPass})
	oldResp := httptest.NewRecorder()
	handler.ServeHTTP(oldResp, oldReq)
	if !bytes.Contains(oldResp.Body.Bytes(), []byte(`id="public-gate"`)) {
		t.Error("old-epoch pass did not receive the gate, want cached resume bytes never served without a valid pass")
	}
}

func newGateCapturingLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

func bytesContainsString(haystack, needle string) bool {
	return bytes.Contains([]byte(haystack), []byte(needle))
}
