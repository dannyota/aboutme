package resumeapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestExecuteAgent_GrantHoldingProjectScopesCommitsResumeMutation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		scopes string
		wantOK bool
	}{
		{"resume scopes only", "resumes:read resumes:write", true},
		{"resume and project scopes", "resumes:read resumes:write projects:read projects:write", true},
		{"project scopes without resume write", "resumes:read projects:read projects:write", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			principal := newAgentPrincipalForTest(t, h)
			created := h.createResume(t)
			if _, err := h.pool.Exec(h.ctx, `UPDATE oauth_grants SET scopes = $1 WHERE id = $2`, tc.scopes, principal.GrantID()); err != nil {
				t.Fatalf("store grant scopes %q: %v", tc.scopes, err)
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
			if tc.wantOK {
				if response.Status != http.StatusOK || stored.Title != "Held scopes" || stored.Revision != 2 {
					t.Fatalf("ExecuteAgent = %d %s, stored %q/%d; want the mutation committed", response.Status, response.Body, stored.Title, stored.Revision)
				}
				return
			}
			if response.Status != http.StatusServiceUnavailable || stored.Title != created.Title || stored.Revision != created.Revision {
				t.Fatalf("ExecuteAgent = %d %s, stored %q/%d; want agent_access_unavailable and no change", response.Status, response.Body, stored.Title, stored.Revision)
			}
		})
	}
}
