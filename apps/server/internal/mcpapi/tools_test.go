package mcpapi

import (
	"net/http"
	"testing"

	"danny.vn/aboutme/apps/server/internal/resumeapi"
)

func TestToolErrorMapIsClosed(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		code   string
		want   string
	}{
		{"bad request", http.StatusBadRequest, "request_invalid", "validation_failed"},
		{"document", http.StatusUnprocessableEntity, "document_invalid", "validation_failed"},
		{"media type", http.StatusUnsupportedMediaType, "media_type_unsupported", "validation_failed"},
		{"stale", http.StatusPreconditionFailed, "revision_mismatch", "revision_conflict"},
		{"idempotency reuse", http.StatusConflict, "idempotency_key_reuse", "validation_failed"},
		{"missing", http.StatusNotFound, "resume_not_found", "not_found"},
		{"missing project", http.StatusNotFound, "project_not_found", "not_found"},
		{"duplicate project link", http.StatusConflict, "project_already_linked", "validation_failed"},
		{"project link scope", http.StatusForbidden, "scope_denied", "scope_denied"},
		{"resume cap", http.StatusConflict, "resume_cap_exceeded", "limit_reached"},
		{"project cap", http.StatusConflict, "project_limit", "limit_reached"},
		{"project slug taken", http.StatusConflict, "project_slug_taken", "slug_unavailable"},
		{"project slug held", http.StatusConflict, "project_slug_held", "slug_unavailable"},
		{"transport size", http.StatusRequestEntityTooLarge, "photo_too_large", "payload_too_large"},
		{"route limit", http.StatusTooManyRequests, "rate_limited", "rate_limited"},
		{"media admission", http.StatusServiceUnavailable, "media_busy", "rate_limited"},
		{"public admission", http.StatusServiceUnavailable, "public_state_busy", "rate_limited"},
		{"authority", http.StatusServiceUnavailable, "agent_access_unavailable", "agent_access_unavailable"},
		{"unknown internal", http.StatusInternalServerError, "internal_error", "agent_access_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := resumeapi.AgentResponse{
				Status: tc.status,
				Body:   []byte(`{"error":{"code":"` + tc.code + `","message":"must not escape"}}`),
			}
			if got := mapAgentResponseError(response).Error(); got != tc.want {
				t.Fatalf("mapAgentResponseError(%d, %q) = %q, want %q", tc.status, tc.code, got, tc.want)
			}
		})
	}
}

func TestResponseDataStripsSignInToView(t *testing.T) {
	body := []byte(`{"data":{"id":"018f5b6a-9a3e-7c21-8b1e-000000000010","revision":"1","signInToView":true}}`)
	data, err := responseData(body)
	if err != nil {
		t.Fatalf("responseData() error: %v", err)
	}
	if _, present := data["signInToView"]; present {
		t.Fatalf("responseData() = %#v, must not carry signInToView", data)
	}
	if data["id"] != "018f5b6a-9a3e-7c21-8b1e-000000000010" || data["revision"] != "1" {
		t.Fatalf("responseData() = %#v, want every other field preserved", data)
	}
}

func TestResponseDataStripsShowcase(t *testing.T) {
	body := []byte(`{"data":{"id":"018f5b6a-9a3e-7c21-8b1e-000000000010","revision":"1","showcase":{"state":"listed","role":"backend"}}}`)
	data, err := responseData(body)
	if err != nil {
		t.Fatalf("responseData() error: %v", err)
	}
	if _, present := data["showcase"]; present {
		t.Fatalf("responseData() = %#v, must not carry showcase", data)
	}
	if data["id"] != "018f5b6a-9a3e-7c21-8b1e-000000000010" || data["revision"] != "1" {
		t.Fatalf("responseData() = %#v, want every other field preserved", data)
	}
	resumes := []map[string]any{{"id": "a", "showcase": nil}, {"id": "b", "showcase": map[string]any{"state": "listed"}}}
	for _, resume := range resumes {
		stripAgentOnlyFields(resume)
		if _, present := resume["showcase"]; present {
			t.Fatalf("resume = %#v, must not carry showcase", resume)
		}
	}
}

func TestStripAgentOnlyFieldsAppliesToEachListedResume(t *testing.T) {
	resumes := []map[string]any{
		{"id": "a", "signInToView": true},
		{"id": "b", "signInToView": false},
	}
	for _, resume := range resumes {
		stripAgentOnlyFields(resume)
	}
	for _, resume := range resumes {
		if _, present := resume["signInToView"]; present {
			t.Fatalf("resume = %#v, must not carry signInToView", resume)
		}
	}
}
