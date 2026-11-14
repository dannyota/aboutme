package oauthsrv_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"danny.vn/aboutme/apps/server/internal/mcpapi"
	"danny.vn/aboutme/apps/server/internal/oauthsrv"
)

func authenticateRefreshedAccess(t *testing.T, f oauthsrv.ScopedRefreshFixture, access string) (mcpapi.Principal, error) {
	t.Helper()
	bearer, err := mcpapi.NewBearer(mcpapi.BearerDependencies{Queries: f.Queries, Clock: f.Clock, PublicOrigin: "https://aboutme.example"})
	if err != nil {
		t.Fatalf("NewBearer: %v", err)
	}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://aboutme.example/mcp", nil)
	r.Header.Set("Authorization", "Bearer "+access)
	return bearer.Authenticate(r)
}

func TestRefreshKeepsEveryGrantScopeAndTheNewAccessTokenAuthenticates(t *testing.T) {
	for _, tc := range []struct {
		name   string
		scopes string
	}{
		{"resume scopes only", "resumes:read resumes:write"},
		{"resume and project scopes", "resumes:read resumes:write projects:read projects:write"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := oauthsrv.NewScopedRefreshFixture(t, tc.scopes)
			raw := f.RefreshRaw
			for rotation := 1; rotation <= 2; rotation++ {
				rotated := f.Rotate(t, raw)
				if rotated.Status != http.StatusOK {
					t.Fatalf("rotation %d = %d %q, want 200", rotation, rotated.Status, rotated.Body)
				}
				if rotated.TokenType != "Bearer" || rotated.Scope != tc.scopes || rotated.AccessToken == "" || rotated.RefreshToken == "" {
					t.Fatalf("rotation %d response type %q scope %q, want Bearer with scope %q and both tokens", rotation, rotated.TokenType, rotated.Scope, tc.scopes)
				}
				principal, err := authenticateRefreshedAccess(t, f, rotated.AccessToken)
				if err != nil {
					t.Fatalf("rotation %d access token on the MCP bearer path: %v, want accepted", rotation, err)
				}
				if principal.UserID != f.UserID || principal.GrantID != f.GrantID {
					t.Fatalf("rotation %d principal = %#v, want the fixture user and grant", rotation, principal)
				}
				held := strings.Fields(tc.scopes)
				if len(principal.Scopes) != len(held) {
					t.Fatalf("rotation %d principal scopes = %q, want exactly %q", rotation, principal.Scopes.String(), tc.scopes)
				}
				for _, scope := range held {
					if !principal.Scopes.Has(oauthsrv.Scope(scope)) {
						t.Fatalf("rotation %d principal scopes = %q, missing %q", rotation, principal.Scopes.String(), scope)
					}
				}
				for _, need := range []oauthsrv.Scope{oauthsrv.ScopeResumesRead, oauthsrv.ScopeResumesWrite} {
					if err := mcpapi.RequireScope(principal, need); err != nil {
						t.Fatalf("rotation %d RequireScope(%s): %v", rotation, need, err)
					}
				}
				raw = rotated.RefreshToken
			}
		})
	}
}
