package resumeapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestExecuteAgent_ResumeMutationRereadsTheGrantScopes(t *testing.T) {
	for _, tc := range []struct {
		name       string
		scopes     string
		revoked    bool
		wantStatus int
		wantCode   string
	}{
		{"resume scopes only", "resumes:read resumes:write", false, http.StatusOK, ""},
		{"resume and project scopes", "resumes:read resumes:write projects:read projects:write", false, http.StatusOK, ""},
		{"narrowed to project scopes without resume write", "resumes:read projects:read projects:write", false, http.StatusForbidden, "scope_denied"},
		{"revoked grant", "resumes:read resumes:write", true, http.StatusServiceUnavailable, "agent_access_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			principal := newAgentPrincipalForTest(t, h)
			created := h.createResume(t)
			if _, err := h.pool.Exec(h.ctx, `UPDATE oauth_grants SET scopes = $1 WHERE id = $2`, tc.scopes, principal.GrantID()); err != nil {
				t.Fatalf("store grant scopes %q: %v", tc.scopes, err)
			}
			if tc.revoked {
				if _, err := h.pool.Exec(h.ctx, `UPDATE oauth_grants SET revoked_at = now() WHERE id = $1`, principal.GrantID()); err != nil {
					t.Fatalf("revoke grant: %v", err)
				}
			}

			response := h.service.ExecuteAgent(h.ctx, principal, AgentCall{
				Operation:      AgentUpdateResumeMetadata,
				IdempotencyKey: uuid.NewString(),
				ResumeID:       created.ID.String(),
				Revision:       "1",
				Payload:        json.RawMessage(`{"title":"Held scopes"}`),
			})
			stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if tc.wantStatus == http.StatusOK {
				if response.Status != http.StatusOK || stored.Title != "Held scopes" || stored.Revision != 2 {
					t.Fatalf("ExecuteAgent = %d %s, stored %q/%d; want the mutation committed", response.Status, response.Body, stored.Title, stored.Revision)
				}
				return
			}
			if response.Status != tc.wantStatus || errorCodeOf(t, response.Body) != tc.wantCode ||
				stored.Title != created.Title || stored.Revision != created.Revision {
				t.Fatalf("ExecuteAgent = %d %s, stored %q/%d; want %d %s and no change",
					response.Status, response.Body, stored.Title, stored.Revision, tc.wantStatus, tc.wantCode)
			}
		})
	}
}
