package mcpapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"danny.vn/aboutme/apps/server/internal/media"
	"danny.vn/aboutme/apps/server/internal/oauthsrv"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
)

const heldProjectScopes = "resumes:read resumes:write projects:read projects:write"

func heldScopeResumeSession(t *testing.T, h *bearerHarness, raw string) *mcp.ClientSession {
	t.Helper()
	projector := docmigrate.NewIdentityProjector()
	backend, err := media.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	public, err := h.q.GetPublicState(context.Background())
	if err != nil {
		t.Fatalf("GetPublicState: %v", err)
	}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: public.DiscoveryGeneration})
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	resumes := resumeapi.New(resume.NewStore(h.pool, projector), resume.NewIdempotencyStore(h.pool), projector, backend,
		resumeapi.Options{
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Clock: h.clock.Now,
			Coordinator: coordinator, RecoveryPool: h.pool,
		})
	handler, err := NewServer(ServerDependencies{Bearer: h.bearer, Resumes: resumes, Projects: &recordingProjectExecutor{}, Rates: mustTestMCPRates(t), MaxRequestBodyBytes: maxMCPRequestBytes})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	return connectMCPClient(t, httpServer.URL, raw, "")
}

func TestBearer_GrantHoldingProjectScopesRunsResumeTools(t *testing.T) {
	for _, tc := range []struct {
		name   string
		scopes string
	}{
		{"resume scopes only", "resumes:read resumes:write"},
		{"resume and project scopes", heldProjectScopes},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newBearerHarness(t, tc.scopes)
			raw, token := h.createToken(t, oauthsrv.TokenKindAccess)

			principal, err := h.bearer.Authenticate(bearerRequest("Bearer " + raw))
			if err != nil {
				t.Fatalf("Authenticate with a grant holding %q: %v, want the access token accepted", tc.scopes, err)
			}
			if principal.UserID != h.user.ID || principal.GrantID != h.grant.ID || principal.TokenID != token.ID {
				t.Fatalf("principal = %#v, want the harness user, grant and token", principal)
			}
			held := strings.Fields(tc.scopes)
			if len(principal.Scopes) != len(held) {
				t.Fatalf("principal scopes = %q, want exactly %q", principal.Scopes.String(), tc.scopes)
			}
			for _, scope := range held {
				if !principal.Scopes.Has(oauthsrv.Scope(scope)) {
					t.Fatalf("principal scopes = %q, missing %q", principal.Scopes.String(), scope)
				}
			}

			session := heldScopeResumeSession(t, h, raw)
			listed, err := session.ListTools(context.Background(), nil)
			if err != nil {
				t.Fatalf("ListTools: %v", err)
			}
			names := make([]string, len(listed.Tools))
			for i, tool := range listed.Tools {
				names[i] = tool.Name
			}
			sort.Strings(names)
			want := []string{
				"create_project", "create_resume", "delete_entry", "delete_photo", "delete_project", "delete_resume",
				"get_photo", "get_project", "get_resume", "link_project", "list_projects", "list_resumes",
				"refresh_project_entry", "update_customization", "update_personal_details", "update_photo_crop",
				"update_project", "update_resume_metadata", "update_section", "update_structure",
				"upload_photo", "upsert_entry",
			}
			if !reflect.DeepEqual(names, want) {
				t.Fatalf("tool names = %v, want the resume and project tools %v", names, want)
			}

			created := callMutationTool(t, session, "create_resume", map[string]any{
				"idempotency_key": uuid.NewString(), "title": "Held scopes",
			})
			resumeID, ok := created.State["id"].(string)
			if !ok || resumeID == "" || created.Revision != "1" {
				t.Fatalf("create_resume output = %#v, want a new resume at revision 1", created)
			}

			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_resumes"})
			if err != nil || result.IsError {
				t.Fatalf("list_resumes = %#v, tool error = %q, error = %v", result, toolErrorText(result), err)
			}
			structured, err := json.Marshal(result.StructuredContent)
			if err != nil {
				t.Fatalf("marshal list_resumes output: %v", err)
			}
			var resumes listResumesOutput
			if err := json.Unmarshal(structured, &resumes); err != nil {
				t.Fatalf("decode list_resumes output: %v (raw=%s)", err, structured)
			}
			if len(resumes.Resumes) != 1 || resumes.Resumes[0]["id"] != resumeID {
				t.Fatalf("list_resumes resumes = %#v, want only the created resume %s", resumes.Resumes, resumeID)
			}
		})
	}
}
