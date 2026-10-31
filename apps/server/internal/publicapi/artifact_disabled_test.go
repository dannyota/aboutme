package publicapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/printrender"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
)

func TestPublicArtifactsWithDisabledPrintQueue(t *testing.T) {
	queue, err := renderjob.New(renderjob.Config{Renderer: &printrender.Renderer{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Close(); err != nil {
		t.Fatal(err)
	}
	handlers, _, _ := newArtifactHarness(t, queue, 2, time.Now)
	for _, route := range []struct {
		suffix  string
		handler http.Handler
	}{
		{"/pdf", handlers.pdf},
		{"/og.png", handlers.png},
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			request := httptest.NewRequestWithContext(t.Context(), method, "/api/v1/public/resumes/ada-lovelace"+route.suffix, nil)
			request.RemoteAddr = "192.0.2.16:1234"
			response := httptest.NewRecorder()
			route.handler.ServeHTTP(response, request)
			if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "1" {
				t.Fatalf("%s %s = %d %s", method, route.suffix, response.Code, response.Body.String())
			}
			if method == http.MethodGet && !strings.Contains(response.Body.String(), `"code":"temporarily_unavailable"`) {
				t.Fatalf("error envelope = %s", response.Body.String())
			}
			if method == http.MethodHead && response.Body.Len() != 0 {
				t.Fatalf("HEAD body = %s", response.Body.String())
			}
		}
	}
}
