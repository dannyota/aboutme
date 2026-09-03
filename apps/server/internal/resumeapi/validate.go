package resumeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
)

// ErrAgentAccessUnavailable is the closed cross-package signal for bearer
// authority that is no longer valid at the transaction boundary.
var ErrAgentAccessUnavailable = errors.New("resumeapi: agent access unavailable")

// AgentPrincipal carries only stored bearer authority identifiers and its
// digest. Raw bearer material never crosses into resumeapi.
type AgentPrincipal struct {
	userID      uuid.UUID
	grantID     uuid.UUID
	tokenID     uuid.UUID
	tokenDigest [32]byte
}

// NewAgentPrincipal constructs the closed bearer mutation principal.
func NewAgentPrincipal(userID, grantID, tokenID uuid.UUID, tokenDigest [32]byte) (AgentPrincipal, error) {
	if userID == uuid.Nil || grantID == uuid.Nil || tokenID == uuid.Nil || tokenDigest == [32]byte{} {
		return AgentPrincipal{}, ErrAgentAccessUnavailable
	}
	return AgentPrincipal{userID: userID, grantID: grantID, tokenID: tokenID, tokenDigest: tokenDigest}, nil
}

func (p AgentPrincipal) valid() bool {
	return p.userID != uuid.Nil && p.grantID != uuid.Nil && p.tokenID != uuid.Nil && p.tokenDigest != [32]byte{}
}

// GrantID returns the grant identity without exposing token material.
func (p AgentPrincipal) GrantID() uuid.UUID { return p.grantID }

type agentPrincipalContextKey struct{}

func contextWithAgentPrincipal(ctx context.Context, principal AgentPrincipal) context.Context {
	return context.WithValue(ctx, agentPrincipalContextKey{}, principal)
}

func agentPrincipalFromContext(ctx context.Context) (AgentPrincipal, bool) {
	principal, ok := ctx.Value(agentPrincipalContextKey{}).(AgentPrincipal)
	return principal, ok
}

// AgentOperation is the closed resume operation set exposed to MCP.
type AgentOperation string

const (
	// AgentListResumes lists the authenticated agent's resumes.
	AgentListResumes AgentOperation = "list_resumes"
	// AgentGetResume retrieves one authenticated agent resume.
	AgentGetResume AgentOperation = "get_resume"
	// AgentCreateResume creates a resume for the authenticated agent.
	AgentCreateResume AgentOperation = "create_resume"
	// AgentDeleteResume deletes a resume for the authenticated agent.
	AgentDeleteResume AgentOperation = "delete_resume"
	// AgentUpdateResumeMetadata updates resume metadata for the authenticated agent.
	AgentUpdateResumeMetadata AgentOperation = "update_resume_metadata"
	// AgentUpsertEntry inserts or updates a resume entry for the authenticated agent.
	AgentUpsertEntry AgentOperation = "upsert_entry"
	// AgentDeleteEntry deletes a resume entry for the authenticated agent.
	AgentDeleteEntry AgentOperation = "delete_entry"
	// AgentUpdateSection updates a resume section for the authenticated agent.
	AgentUpdateSection AgentOperation = "update_section"
	// AgentUpdateStructure updates resume structure for the authenticated agent.
	AgentUpdateStructure AgentOperation = "update_structure"
	// AgentUpdatePersonalDetails updates resume personal details for the authenticated agent.
	AgentUpdatePersonalDetails AgentOperation = "update_personal_details"
	// AgentUpdateCustomization updates resume customization for the authenticated agent.
	AgentUpdateCustomization AgentOperation = "update_customization"
	// AgentGetPhoto retrieves a resume photo for the authenticated agent.
	AgentGetPhoto AgentOperation = "get_photo"
	// AgentUploadPhoto uploads a resume photo for the authenticated agent.
	AgentUploadPhoto AgentOperation = "upload_photo"
	// AgentUpdatePhotoCrop updates a resume photo crop for the authenticated agent.
	AgentUpdatePhotoCrop AgentOperation = "update_photo_crop"
	// AgentDeletePhoto deletes a resume photo for the authenticated agent.
	AgentDeletePhoto AgentOperation = "delete_photo"
)

// AgentCall is the validated bridge from typed MCP inputs to the existing REST
// validation and mutation kernel.
type AgentCall struct {
	Operation      AgentOperation
	IdempotencyKey string
	ResumeID       string
	Revision       string
	SectionKey     string
	EntryID        string
	Payload        json.RawMessage
	File           []byte
}

// AgentResponse preserves the canonical REST response for closed MCP mapping.
type AgentResponse struct {
	Status int
	Header http.Header
	Body   []byte
}

type agentResponseWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

// Header implements http.ResponseWriter.
func (w *agentResponseWriter) Header() http.Header { return w.header }

// WriteHeader implements http.ResponseWriter.
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

// SetReadDeadline implements http.ResponseController's read-deadline hook.
func (w *agentResponseWriter) SetReadDeadline(time.Time) error { return nil }

// ExecuteAgent routes one closed agent operation through the same request
// validators, idempotency transaction, sanitizer, and store callback as REST.
func (s *Service) ExecuteAgent(ctx context.Context, principal AgentPrincipal, call AgentCall) AgentResponse {
	w := &agentResponseWriter{header: make(http.Header)}
	if !principal.valid() {
		writeResumeError(w, mapMutationError(ErrAgentAccessUnavailable))
		return AgentResponse{Status: w.status, Header: w.header.Clone(), Body: bytes.Clone(w.body.Bytes())}
	}
	method := http.MethodGet
	handler := s.handleListResumes
	mutation := false
	requireRevision := false
	body := []byte(call.Payload)
	contentType := ""
	switch call.Operation {
	case AgentListResumes:
	case AgentGetResume:
		handler = s.handleGetResume
	case AgentCreateResume:
		method, handler, mutation = http.MethodPost, s.handleCreateResume, true
	case AgentDeleteResume:
		method, handler, mutation, requireRevision = http.MethodDelete, s.handleDeleteResume, true, true
	case AgentUpdateResumeMetadata:
		method, handler, mutation, requireRevision = http.MethodPatch, s.handleUpdateResumeMetadata, true, true
	case AgentUpsertEntry:
		method, handler, mutation, requireRevision = http.MethodPatch, s.handleUpsertResumeEntry, true, true
	case AgentDeleteEntry:
		method, handler, mutation, requireRevision = http.MethodDelete, s.handleDeleteResumeEntry, true, true
	case AgentUpdateSection:
		method, handler, mutation, requireRevision = http.MethodPatch, s.handleUpdateResumeSection, true, true
	case AgentUpdateStructure:
		method, handler, mutation, requireRevision = http.MethodPatch, s.handleUpdateResumeStructure, true, true
	case AgentUpdatePersonalDetails:
		method, handler, mutation, requireRevision = http.MethodPatch, s.handleUpdateResumePersonalDetails, true, true
	case AgentUpdateCustomization:
		method, handler, mutation, requireRevision = http.MethodPatch, s.handleUpdateResumeCustomization, true, true
	case AgentGetPhoto:
		handler = s.handleGetResumePhoto
	case AgentUploadPhoto:
		method, handler, mutation, requireRevision = http.MethodPost, s.handleUploadResumePhoto, true, true
		var encodeErr error
		body, contentType, encodeErr = encodeAgentPhoto(call.File)
		if encodeErr != nil {
			writeResumeError(w, internalClientError())
			return AgentResponse{Status: w.status, Header: w.header.Clone(), Body: bytes.Clone(w.body.Bytes())}
		}
	case AgentUpdatePhotoCrop:
		method, handler, mutation, requireRevision = http.MethodPatch, s.handleUpdateResumePhotoCrop, true, true
	case AgentDeletePhoto:
		method, handler, mutation, requireRevision = http.MethodDelete, s.handleDeleteResumePhoto, true, true
	default:
		writeResumeError(w, &clientError{Status: http.StatusBadRequest, Code: "request_invalid", Message: "agent operation is invalid"})
		return AgentResponse{Status: w.status, Header: w.header.Clone(), Body: bytes.Clone(w.body.Bytes())}
	}
	request, err := http.NewRequestWithContext(contextWithAgentPrincipal(ctx, principal), method, "https://agent.invalid/", bytes.NewReader(body))
	if err != nil {
		writeResumeError(w, internalClientError())
		return AgentResponse{Status: w.status, Header: w.header.Clone(), Body: bytes.Clone(w.body.Bytes())}
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	} else if method != http.MethodGet && method != http.MethodDelete {
		request.Header.Set("Content-Type", "application/json")
	}
	if mutation {
		if call.IdempotencyKey != "" {
			request.Header.Set("Idempotency-Key", call.IdempotencyKey)
		}
	}
	if requireRevision {
		request.Header.Set("If-Match", `"r`+call.Revision+`"`)
	}
	request.SetPathValue("id", call.ResumeID)
	request.SetPathValue("sectionKey", call.SectionKey)
	request.SetPathValue("entryId", call.EntryID)
	handler(w, request)
	return AgentResponse{Status: w.status, Header: w.header.Clone(), Body: bytes.Clone(w.body.Bytes())}
}

func encodeAgentPhoto(file []byte) ([]byte, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "photo")
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(file); err != nil {
		return nil, "", err
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return body.Bytes(), writer.FormDataContentType(), nil
}

func requestUserID(r *http.Request) (uuid.UUID, *clientError) {
	if principal, ok := agentPrincipalFromContext(r.Context()); ok {
		return principal.userID, nil
	}
	if session, ok := auth.SessionFromContext(r.Context()); ok {
		return session.UserID, nil
	}
	return uuid.Nil, &clientError{Status: http.StatusUnauthorized, Code: "session_required", Message: "a valid session is required"}
}
