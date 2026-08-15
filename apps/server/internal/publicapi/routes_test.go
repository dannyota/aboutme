package publicapi

import (
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/publicroots"
)

func TestPublicRoutesRecognizesOnlyCaddyPublicPaths(t *testing.T) {
	t.Parallel()

	routes := &Service{}
	for _, test := range []struct {
		path string
		want bool
	}{
		{"/api/v1/public/resumes/ada-lovelace", true},
		{"/api/v1/public/resumes/ada-lovelace/photo", true},
		{"/ada-lovelace", true},
		{"/ada-lovelace.md", true},
		{"/sitemap.xml", true},
		{"/robots.txt", true},
		{"/llms.txt", true},
		{"/api/v1/public/resumes/a", false},
		{"/api/v1/public/resumes/ada/extra", false},
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
