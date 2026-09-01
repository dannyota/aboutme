package oauthsrv

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMetadata_StableConfiguredDocuments(t *testing.T) {
	s, _, _, _ := newAuthorizeHarness(t)

	tests := []struct {
		name    string
		handler http.HandlerFunc
		path    string
		want    string
	}{
		{
			name:    "authorization server",
			handler: s.HandleMetadata,
			path:    "/.well-known/oauth-authorization-server",
			want:    `{"issuer":"https://aboutme.example","authorization_endpoint":"https://aboutme.example/oauth/authorize","token_endpoint":"https://aboutme.example/oauth/token","registration_endpoint":"https://aboutme.example/oauth/register","revocation_endpoint":"https://aboutme.example/oauth/revoke","response_types_supported":["code"],"grant_types_supported":["authorization_code","refresh_token"],"code_challenge_methods_supported":["S256"],"token_endpoint_auth_methods_supported":["none"],"scopes_supported":["resumes:read","resumes:write"]}`,
		},
		{
			name:    "protected resource",
			handler: s.HandleProtectedResourceMetadata,
			path:    "/.well-known/oauth-protected-resource",
			want:    `{"resource":"https://aboutme.example","authorization_servers":["https://aboutme.example"],"bearer_methods_supported":["header"]}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "https://attacker.example"+tc.path+"?substitution=1", nil)
			request.Host = "attacker.example"
			request.Header.Set("X-Forwarded-Host", "attacker.example")
			request.Header.Set("X-Forwarded-Proto", "http")
			recorder := httptest.NewRecorder()
			tc.handler(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", recorder.Code)
			}
			if got := recorder.Header().Get("Cache-Control"); got != "public, max-age=3600" {
				t.Fatalf("Cache-Control = %q", got)
			}
			if got := recorder.Body.String(); got != tc.want {
				t.Fatalf("body = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMetadata_GetOnly(t *testing.T) {
	s, _, _, _ := newAuthorizeHarness(t)
	for _, handler := range []http.HandlerFunc{s.HandleMetadata, s.HandleProtectedResourceMetadata} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
			recorder := httptest.NewRecorder()
			handler(recorder, httptest.NewRequest(method, "https://aboutme.example/.well-known/test", nil))
			if recorder.Code != http.StatusMethodNotAllowed {
				t.Fatalf("%s status = %d, want 405", method, recorder.Code)
			}
		}
	}
}
