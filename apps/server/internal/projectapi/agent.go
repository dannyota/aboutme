package projectapi

import (
	"bytes"
	"context"
	"net/http"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/oauthsrv"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
	"danny.vn/aboutme/apps/server/internal/store"
)

type AgentOperation string

const (
	AgentListProjects  AgentOperation = "list_projects"
	AgentGetProject    AgentOperation = "get_project"
	AgentCreateProject AgentOperation = "create_project"
	AgentUpdateProject AgentOperation = "update_project"
	AgentDeleteProject AgentOperation = "delete_project"
)

type AgentCall struct {
	Operation      AgentOperation
	IdempotencyKey string
	ProjectID      string
	Revision       string
	Payload        []byte
}

type agentResponseWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *agentResponseWriter) Header() http.Header { return w.header }

func (w *agentResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *agentResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(body)
}

func (w *agentResponseWriter) response() resumeapi.AgentResponse {
	return resumeapi.AgentResponse{Status: w.status, Header: w.header.Clone(), Body: bytes.Clone(w.body.Bytes())}
}

func (s *Service) ExecuteAgent(ctx context.Context, principal resumeapi.AgentPrincipal, call AgentCall) resumeapi.AgentResponse {
	w := &agentResponseWriter{header: make(http.Header)}
	owner := principal.UserID()
	if owner == uuid.Nil || s.allowSlug == nil || s.links == nil {
		writeError(w, agentUnavailable())
		return w.response()
	}
	method, item, required := http.MethodGet, false, []oauthsrv.Scope(nil)
	switch call.Operation {
	case AgentListProjects:
	case AgentGetProject:
		item = true
	case AgentCreateProject:
		method, required = http.MethodPost, []oauthsrv.Scope{oauthsrv.ScopeProjectsWrite}
	case AgentUpdateProject:
		method, item, required = http.MethodPut, true, []oauthsrv.Scope{oauthsrv.ScopeProjectsWrite, oauthsrv.ScopeResumesWrite}
	case AgentDeleteProject:
		method, item, required = http.MethodDelete, true, []oauthsrv.Scope{oauthsrv.ScopeProjectsWrite, oauthsrv.ScopeResumesWrite}
	default:
		writeError(w, requestError())
		return w.response()
	}
	r, err := http.NewRequestWithContext(ctx, method, "https://agent.invalid"+projectsPath, bytes.NewReader(call.Payload))
	if err != nil {
		writeError(w, internalError())
		return w.response()
	}
	if item {
		r.SetPathValue("id", call.ProjectID)
	}
	if method == http.MethodGet {
		s.readFor(w, r, owner)
		return w.response()
	}
	r.Header.Set("Idempotency-Key", call.IdempotencyKey)
	if method != http.MethodPost {
		r.Header.Set("If-Match", `"r`+call.Revision+`"`)
	}
	if method != http.MethodDelete {
		r.Header.Set("Content-Type", "application/json")
	}
	s.mutateAs(w, r, func(context.Context) (mutationActor, bool) {
		return mutationActor{owner: owner, authorize: func(ctx context.Context, q *store.Queries) error {
			_, err := resumeapi.RequireAgentScopesTx(ctx, q, principal, s.now(), required...)
			return err
		}}, true
	})
	return w.response()
}

func agentUnavailable() *clientError {
	return failure(http.StatusServiceUnavailable, "agent_access_unavailable", "agent access is unavailable")
}
