package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestViewPage_ClosedGrammar(t *testing.T) {
	t.Parallel()
	slug40 := strings.Repeat("a", 40)
	for _, test := range []struct {
		name, query, want string
		ok                bool
	}{
		{"absent", "purpose=view&slug=ada-lovelace", "", true},
		{"list", "purpose=view&slug=ada-lovelace&page=projects", "/projects", true},
		{"project", "purpose=view&slug=ada-lovelace&page=projects/note-g", "/projects/note-g", true},
		{"shortest project slug", "page=projects/ab", "/projects/ab", true},
		{"longest project slug", "page=projects/" + slug40, "/projects/" + slug40, true},
		{"empty", "page=", "", false},
		{"repeated", "page=projects&page=projects", "", false},
		{"absolute URL", "page=https://evil.example/projects", "", false},
		{"network path", "page=//evil.example/projects", "", false},
		{"leading slash", "page=/projects", "", false},
		{"double slash", "page=projects//note-g", "", false},
		{"trailing slash", "page=projects/", "", false},
		{"backslash", `page=projects\note-g`, "", false},
		{"encoded backslash", "page=projects%5Cnote-g", "", false},
		{"dot dot", "page=..", "", false},
		{"dot segment", "page=projects/../note-g", "", false},
		{"encoded slash", "page=projects%2Fnote-g", "", false},
		{"encoded letter", "page=projects/%6eote-g", "", false},
		{"double encoding", "page=projects%252Fnote-g", "", false},
		{"malformed escape", "page=projects/%zz", "", false},
		{"encoded key", "pag%65=projects", "", false},
		{"extra segment", "page=projects/note-g/extra", "", false},
		{"uppercase literal", "page=Projects", "", false},
		{"uppercase slug", "page=projects/Note-G", "", false},
		{"short slug", "page=projects/a", "", false},
		{"long slug", "page=projects/" + slug40 + "a", "", false},
		{"double hyphen", "page=projects/a--b", "", false},
		{"other page", "page=project", "", false},
		{"markdown", "page=projects.md", "", false},
		{"space", "page=projects+", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/auth/google/start?"+test.query, nil)
			got, ok := viewPage(request)
			if got != test.want || ok != test.ok {
				t.Fatalf("viewPage(%q) = %q, %t; want %q, %t", test.query, got, ok, test.want, test.ok)
			}
		})
	}
}

func TestViewReturnSuffix_RevalidatesTheStoredPage(t *testing.T) {
	t.Parallel()
	for stored, want := range map[string]string{
		"/projects":                   "/projects",
		"/projects/note-g":            "/projects/note-g",
		defaultLoginReturnPath:        "",
		"":                            "",
		"/projects/":                  "",
		"//evil.example/projects":     "",
		"https://evil.example":        "",
		"/projects/../note-g":         "",
		"/projects/note-g?signin=x":   "",
		"/projects/note-g/extra":      "",
		"/projects/%6eote-g":          "",
		"/app/new?template=projects":  "",
		"/projects\\evil.example":     "",
		"/Projects/note-g":            "",
		"/projects/note-g#fragment":   "",
		"/projects/" + "note-g\n":     "",
		"/projects/" + "note-g\x00":   "",
		"/projects/" + "note-g extra": "",
	} {
		if got := viewReturnSuffix(stored); got != want {
			t.Errorf("viewReturnSuffix(%q) = %q, want %q", stored, got, want)
		}
	}
}
