package mcpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMCPError_ClosedMappings(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
		wantAuth   string
	}{
		{"unauthorized", &mcpError{status: http.StatusUnauthorized, code: "unauthorized", challenge: bearerPublicOrigin}, http.StatusUnauthorized, `{"error":"unauthorized"}`, `Bearer resource_metadata="https://aboutme.example/.well-known/oauth-protected-resource"`},
		{"scope denied", errScopeDenied, http.StatusForbidden, `{"error":"scope_denied"}`, ""},
		{"unknown is internal", errors.New("untrusted detail"), http.StatusInternalServerError, `{"error":"internal_error"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeMCPError(recorder, tc.err)
			if recorder.Code != tc.wantStatus || recorder.Body.String() != tc.wantBody || recorder.Header().Get("WWW-Authenticate") != tc.wantAuth {
				t.Fatalf("response = %d %q %q", recorder.Code, recorder.Body.String(), recorder.Header().Get("WWW-Authenticate"))
			}
		})
	}
}
