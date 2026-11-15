package resumeapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/auth"
)

type callerEntryWrite struct {
	resumeID uuid.UUID
	revision int64
	key      uuid.UUID
	body     string
}

func newCallerEntryWrite(t *testing.T, h *resumeAPITestHarness) callerEntryWrite {
	t.Helper()
	created := h.createResume(t)
	revision, err := h.resumes.SaveDocument(h.ctx, h.userID, created.ID, withProjectSection(created.Doc, "projects"), created.Revision)
	if err != nil {
		t.Fatalf("seed project section: %v", err)
	}
	title := "Caller-bound entry"
	return callerEntryWrite{
		resumeID: created.ID, revision: revision, key: uuid.New(),
		body: entryJSON(t, schema.ProjectEntry{ID: uuid.NewString(), Title: &title}),
	}
}

func (c callerEntryWrite) path() string {
	return apiResumePath + "/" + c.resumeID.String() + "/entries/projects"
}

func (c callerEntryWrite) session(t *testing.T, h *resumeAPITestHarness) testHTTPResponse {
	t.Helper()
	return resumeRequest(t, h, http.MethodPatch, c.path(), c.body, c.revision, c.key, "")
}

func (c callerEntryWrite) agent(h *resumeAPITestHarness, principal AgentPrincipal) AgentResponse {
	return h.service.ExecuteAgent(h.ctx, principal, AgentCall{
		Operation: AgentUpsertEntry, IdempotencyKey: c.key.String(), ResumeID: c.resumeID.String(),
		Revision: formatRevision(c.revision), SectionKey: "projects", Payload: json.RawMessage(c.body),
	})
}

func (c callerEntryWrite) otherSession(t *testing.T, h *resumeAPITestHarness) testHTTPResponse {
	t.Helper()
	raw, session, err := auth.NewSessionManager(h.queries).Issue(h.ctx, h.userID, "resumeapi-second-session", "127.0.0.1")
	if err != nil {
		t.Fatalf("issue second session: %v", err)
	}
	req, err := http.NewRequestWithContext(h.ctx, http.MethodPatch, h.server.URL+c.path(), strings.NewReader(c.body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "__Host-session", Value: raw})
	req.Header.Set("Origin", resumeAPITestOrigin)
	req.Header.Set(auth.CSRFHeaderName, base64.RawURLEncoding.EncodeToString(session.CSRFSecret))
	req.Header.Set("Idempotency-Key", c.key.String())
	req.Header.Set("If-Match", fmt.Sprintf(`"r%d"`, c.revision))
	req.Header.Set("Content-Type", "application/json")
	response, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	return snapshotHTTPResponse(t, response)
}

func TestIdempotency_AgentCannotReplayASessionAnswer(t *testing.T) {
	h := newResumeAPITestHarness(t)
	write := newCallerEntryWrite(t, h)
	first := write.session(t, h)
	if first.status != http.StatusOK {
		t.Fatalf("session write = %d %s", first.status, first.body)
	}
	before := storedResume(t, h, write.resumeID)
	agent := write.agent(h, newAgentPrincipalForTest(t, h))
	if agent.Status != http.StatusConflict || errorCodeOf(t, agent.Body) != "idempotency_key_reuse" {
		t.Fatalf("agent reuse of a session key = %d %s, want 409 idempotency_key_reuse", agent.Status, agent.Body)
	}
	requireUnchanged(t, h, "the agent reuse", before)
	if retry := write.session(t, h); retry.status != first.status || !bytes.Equal(retry.body, first.body) {
		t.Fatalf("session retry = %d %s, want the first answer", retry.status, retry.body)
	}
}

func TestIdempotency_SessionCannotReplayAnAgentAnswer(t *testing.T) {
	h := newResumeAPITestHarness(t)
	write := newCallerEntryWrite(t, h)
	principal := newAgentPrincipalForTest(t, h)
	first := write.agent(h, principal)
	if first.Status != http.StatusOK {
		t.Fatalf("agent write = %d %s", first.Status, first.Body)
	}
	before := storedResume(t, h, write.resumeID)
	session := write.session(t, h)
	if session.status != http.StatusConflict || errorCodeOf(t, session.body) != "idempotency_key_reuse" {
		t.Fatalf("session reuse of an agent key = %d %s, want 409 idempotency_key_reuse", session.status, session.body)
	}
	requireUnchanged(t, h, "the session reuse", before)
	if retry := write.agent(h, principal); retry.Status != first.Status || !bytes.Equal(retry.Body, first.Body) {
		t.Fatalf("agent retry = %d %s, want the first answer", retry.Status, retry.Body)
	}
}

func TestIdempotency_SessionsOfTheOwnerReplayAsOneCaller(t *testing.T) {
	h := newResumeAPITestHarness(t)
	write := newCallerEntryWrite(t, h)
	first := write.session(t, h)
	if first.status != http.StatusOK {
		t.Fatalf("session write = %d %s", first.status, first.body)
	}
	before := storedResume(t, h, write.resumeID)
	retry := write.otherSession(t, h)
	if retry.status != first.status || !bytes.Equal(retry.body, first.body) {
		t.Fatalf("retry from a new session = %d %s, want the first answer", retry.status, retry.body)
	}
	requireUnchanged(t, h, "the replay from a new session", before)
}
