package publicapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/publicroots"
)

func TestPublicLiveMissingHandlerUsesStreamErrorContract(t *testing.T) {
	t.Parallel()

	routes := &Service{}
	response := httptest.NewRecorder()
	routes.ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/live/ada-lovelace", nil))
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Cache-Control") != "no-store, no-transform" || response.Header().Get("Retry-After") != "5" {
		t.Fatalf("missing live handler response = %d %#v", response.Code, response.Header())
	}
}

func TestPublicRoutesRecognizesOnlyCaddyPublicPaths(t *testing.T) {
	t.Parallel()

	routes := &Service{}
	for _, test := range []struct {
		path string
		want bool
	}{
		{"/api/v1/public/resumes/ada-lovelace", true},
		{"/api/v1/live/ada-lovelace", true},
		{"/api/v1/live/%61da-lovelace", true},
		{"/api/v1/public/resumes/ada-lovelace/photo", true},
		{"/api/v1/public/resumes/ada-lovelace/pdf", true},
		{"/api/v1/public/resumes/ada-lovelace/og.png", true},
		{"/ada-lovelace", true},
		{"/ada-lovelace.md", true},
		{"/sitemap.xml", true},
		{"/robots.txt", true},
		{"/llms.txt", true},
		{"/api/v1/public/resumes/a", false},
		{"/api/v1/public/resumes/ada/extra", false},
		{"/api/v1/public/resumes/ada-lovelace/og.jpg", false},
		{"/ada/child", false},
		{"/%61da", false},
		{"/api/v1/resumes/ada", false},
		{"/app", false},
	} {
		if got := routes.Recognizes(test.path); got != test.want {
			t.Errorf("Recognizes(%q) = %t, want %t", test.path, got, test.want)
		}
	}
}

func TestPublicRoutesRejectEveryGeneratedReservedRoot(t *testing.T) {
	t.Parallel()

	routes := &Service{}
	for _, root := range publicroots.Routes {
		if !publicroots.Reserved(root.Root) {
			continue
		}
		if validPublicSlug(root.Root) {
			t.Errorf("validPublicSlug(%q) = true for generated reserved root", root.Root)
		}
		for _, path := range []string{
			"/" + root.Root + ".md",
			"/api/v1/public/resumes/" + root.Root,
			"/api/v1/public/resumes/" + root.Root + "/photo",
		} {
			if routes.Recognizes(path) {
				t.Errorf("Recognizes(%q) = true for generated reserved root %q", path, root.Root)
			}
		}
	}
}
