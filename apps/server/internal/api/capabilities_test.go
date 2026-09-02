package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/api"
)

func TestCapabilitiesHandler_ReflectsFlagsAndRejectsOtherMethods(t *testing.T) {
	t.Parallel()
	h := api.CapabilitiesHandler(api.Capabilities{ProviderLogin: true, AgentAccess: false})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/capabilities", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", rec.Code)
	}
	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Data) != 2 || body.Data["providerLogin"] != true || body.Data["agentAccess"] != false {
		t.Fatalf("data = %v, want exactly providerLogin=true agentAccess=false", body.Data)
	}

	for _, method := range []string{http.MethodPost, http.MethodHead, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, "/api/v1/capabilities", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s status = %d, want 405", method, rec.Code)
		}
	}
}

func TestCapabilitiesHandler_IsNoStoreThroughTheRouter(t *testing.T) {
	t.Parallel()
	handler := api.New(testLogger(), fakePinger{}, api.Options{}, nil, func(mux *http.ServeMux) {
		mux.Handle("/api/v1/capabilities", api.CapabilitiesHandler(api.Capabilities{}))
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/capabilities", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != api.CacheControlNoStore {
		t.Fatalf("Cache-Control = %q, want the router's no-store policy %q", got, api.CacheControlNoStore)
	}
}
