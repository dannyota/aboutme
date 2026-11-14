package projectapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/oauthsrv"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
	"danny.vn/aboutme/apps/server/internal/store"
)

func agentPrincipal(t *testing.T, h *harness) resumeapi.AgentPrincipal {
	t.Helper()
	now := time.Now()
	client, err := h.q.CreateOAuthClient(t.Context(), store.CreateOAuthClientParams{
		ClientName: "Project agent", RedirectURIs: json.RawMessage(`["https://agent.example/callback"]`), CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateOAuthClient: %v", err)
	}
	t.Cleanup(func() {
		if _, cleanupErr := h.q.DeleteOAuthClient(context.Background(), client.ID); cleanupErr != nil {
			t.Errorf("delete OAuth client: %v", cleanupErr)
		}
	})
	grant, err := h.q.UpsertOAuthGrant(t.Context(), store.UpsertOAuthGrantParams{
		UserID: h.user.ID, ClientID: client.ID, Scopes: "resumes:read resumes:write projects:read projects:write",
		CreatedAt: now, AuthEpoch: h.user.AuthEpoch,
	})
	if err != nil {
		t.Fatalf("UpsertOAuthGrant: %v", err)
	}
	_, digest, err := oauthsrv.NewToken(oauthsrv.TokenKindAccess, rand.Reader)
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	token, err := h.q.CreateOAuthToken(t.Context(), store.CreateOAuthTokenParams{
		TokenDigest: digest[:], Kind: string(oauthsrv.TokenKindAccess), FamilyID: uuid.New(), ClientID: client.ID,
		UserID: h.user.ID, GrantID: grant.ID, CreatedAt: now, ExpiresAt: now.Add(time.Hour), FamilyExpiresAt: now.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateOAuthToken: %v", err)
	}
	principal, err := resumeapi.NewAgentPrincipal(h.user.ID, grant.ID, token.ID, digest)
	if err != nil {
		t.Fatalf("NewAgentPrincipal: %v", err)
	}
	return principal
}

func agentCreate(t *testing.T, h *harness, principal resumeapi.AgentPrincipal, key string) resumeapi.AgentResponse {
	return h.svc.ExecuteAgent(t.Context(), principal, AgentCall{
		Operation: AgentCreateProject, IdempotencyKey: key, Payload: []byte(projectBody),
	})
}

func TestProjectAgent_AgentCannotReplayASessionAnswer(t *testing.T) {
	h := newHarness(t)
	key := uuid.NewString()
	first := h.request(t, http.MethodPost, projectsPath, projectBody, key, "")
	if first.Code != http.StatusCreated {
		t.Fatalf("session create = %d %s", first.Code, first.Body.String())
	}
	agent := agentCreate(t, h, agentPrincipal(t, h), key)
	if agent.Status != http.StatusConflict || !bytes.Contains(agent.Body, []byte(`"idempotency_key_reuse"`)) {
		t.Fatalf("agent reuse of a session key = %d %s, want 409 idempotency_key_reuse", agent.Status, agent.Body)
	}
	if count := h.projectCount(t); count != 1 {
		t.Fatalf("projects after the agent reuse = %d, want 1", count)
	}
	if retry := h.request(t, http.MethodPost, projectsPath, projectBody, key, ""); retry.Code != first.Code || retry.Body.String() != first.Body.String() {
		t.Fatalf("session retry = %d %s, want the first answer", retry.Code, retry.Body.String())
	}
}

func TestProjectAgent_SessionCannotReplayAnAgentAnswer(t *testing.T) {
	h := newHarness(t)
	key := uuid.NewString()
	principal := agentPrincipal(t, h)
	first := agentCreate(t, h, principal, key)
	if first.Status != http.StatusCreated {
		t.Fatalf("agent create = %d %s", first.Status, first.Body)
	}
	session := h.request(t, http.MethodPost, projectsPath, projectBody, key, "")
	if session.Code != http.StatusConflict || !bytes.Contains(session.Body.Bytes(), []byte(`"idempotency_key_reuse"`)) {
		t.Fatalf("session reuse of an agent key = %d %s, want 409 idempotency_key_reuse", session.Code, session.Body.String())
	}
	if count := h.projectCount(t); count != 1 {
		t.Fatalf("projects after the session reuse = %d, want 1", count)
	}
	if retry := agentCreate(t, h, principal, key); retry.Status != first.Status || !bytes.Equal(retry.Body, first.Body) {
		t.Fatalf("agent retry = %d %s, want the first answer", retry.Status, retry.Body)
	}
}
