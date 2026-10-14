package oauthsrv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCIMDDiscoveryStartupFlag(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		s := &Service{publicOrigin: "https://aboutme.example", cimdEnabled: enabled}
		w := httptest.NewRecorder()
		s.HandleMetadata(w, httptest.NewRequest(http.MethodGet, "https://aboutme.example/.well-known/oauth-authorization-server", nil))
		var metadata map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &metadata); err != nil {
			t.Fatal(err)
		}
		value, present := metadata["client_id_metadata_document_supported"]
		if present != enabled || enabled && value != true {
			t.Fatal("CIMD metadata differs from startup flag")
		}
		if metadata["registration_endpoint"] != "https://aboutme.example/oauth/register" {
			t.Fatal("DCR discovery changed")
		}
	}
}

func TestCIMDHTTPFailuresAreClosedAndFlagOffNeverResolves(t *testing.T) {
	h := newOAuthSessionHTTPHarness(t)
	f, err := NewCIMDResolver(cimdTestConfig(), h.service.publicOrigin, h.service.clock, 10000)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	f.lookup = func(context.Context, string) ([]netip.Addr, error) { calls++; return nil, errCIMDInvalid }
	h.service.cimd = f
	request := consentQuery(uuid.Nil, "resumes:read")
	request.PublicClientID = "https://client.example/" + uuid.NewString()
	request.RedirectURI = "http://localhost:20095/callback"
	query := request.values().Encode()
	w := httptest.NewRecorder()
	h.service.HandleAuthorize(w, httptest.NewRequest(http.MethodGet, h.service.publicOrigin+"/oauth/authorize?"+query, nil))
	if w.Code != 400 || w.Header().Get("Location") != "" || calls != 0 {
		t.Fatal("disabled authorize fetched or redirected")
	}
	handler := h.service.ConsentHTTPHandler(h.sessions)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		values := map[string]string{}
		for key := range request.values() {
			values[key] = request.values().Get(key)
		}
		values["decision"] = "approve"
		body, err := json.Marshal(values)
		if err != nil {
			t.Fatal(err)
		}
		r := h.request(method, h.service.publicOrigin+"/api/v1/oauth/consent?"+query, string(body), true, true)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 400 || responseErrorCode(t, w) != "request_invalid" || strings.Contains(w.Body.String(), "redirectTo") || calls != 0 {
			t.Fatal("disabled consent fetched or exposed redirect")
		}
	}
	h.service.cimdEnabled = true
	values := map[string]string{}
	for key := range request.values() {
		values[key] = request.values().Get(key)
	}
	values["decision"] = "approve"
	body, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	csrfRequest := h.request(http.MethodPost, h.service.publicOrigin+"/api/v1/oauth/consent", string(body), true, false)
	csrfResponse := httptest.NewRecorder()
	handler.ServeHTTP(csrfResponse, csrfRequest)
	if csrfResponse.Code != 403 || responseErrorCode(t, csrfResponse) != "csrf_rejected" || calls != 0 {
		t.Fatal("CSRF failure reached metadata resolution")
	}
	for range 6 {
		r := h.request(http.MethodGet, h.service.publicOrigin+"/api/v1/oauth/consent?"+query, "", true, false)
		r.RemoteAddr = "8.8.8.8:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 400 || responseErrorCode(t, w) != "request_invalid" {
			t.Fatalf("fetch failure status = %d", w.Code)
		}
	}
	r := h.request(http.MethodGet, h.service.publicOrigin+"/api/v1/oauth/consent?"+query, "", true, false)
	r.RemoteAddr = "8.8.8.8:1234"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 429 || responseErrorCode(t, w) != "rate_limited" || w.Header().Get("Retry-After") == "" || calls != 6 {
		t.Fatal("fetch admission did not close before DNS")
	}
}
