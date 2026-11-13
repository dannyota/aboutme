package publicapi

import (
	"net/http"
	"strings"
	"testing"

	"danny.vn/aboutme/apps/server/internal/publicformat"
)

func TestProjectPages_SendTheProjectCSP(t *testing.T) {
	want := strings.Replace(publicformat.BaseCSP, "style-src 'self' 'unsafe-inline'", "style-src 'self'", 1)
	if want == publicformat.BaseCSP || strings.Contains(want, "unsafe-inline") || projectPageCSP != want {
		t.Fatalf("project CSP %q, want %q, without inline styles", projectPageCSP, want)
	}
	for _, test := range []struct {
		name, path string
		store      func(*testing.T) *projectPagesStore
	}{
		{"list", "/ada/projects", projectListFixture},
		{"page", "/ada/projects/note-g", projectPageFixture},
	} {
		_, client := newFakeProjectRenderer(t)
		handlers := newProjectHandlers(t, projectTestReader(t, test.store(t)), client)
		for _, step := range []struct{ name, method string }{{"fresh", http.MethodGet}, {"cached", http.MethodGet}, {"HEAD", http.MethodHead}} {
			response := handlers.serve(step.method, test.path)
			if response.Code != http.StatusOK || response.Header().Get("Content-Security-Policy") != want {
				t.Errorf("%s %s: %d CSP %q, want %q; log %s", test.name, step.name, response.Code, response.Header().Get("Content-Security-Policy"), want, handlers.logs.String())
			}
		}
	}

	gated := projectPageFixture(t)
	gated.row.SignInToView, gated.row.ViewPassEpoch = true, 1
	_, client := newFakeProjectRenderer(t)
	handlers := newProjectHandlers(t, projectTestReader(t, gated), client)
	for _, path := range []string{"/ada/projects", "/ada/projects/note-g"} {
		gate := handlers.serve(http.MethodGet, path)
		if gate.Code != http.StatusOK || !strings.Contains(gate.Body.String(), `id="public-gate"`) ||
			gate.Header().Get("Content-Security-Policy") != publicformat.BaseCSP {
			t.Errorf("gate for %s: %d CSP %q, want the gate with %q", path, gate.Code, gate.Header().Get("Content-Security-Policy"), publicformat.BaseCSP)
		}
	}
}
