package mcpapi

import (
	"net/http"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/resumeapi"
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

// TestResponseDataStripsSignInToView proves the sign-in-to-view publish
// switch never reaches an agent: MCP has no publish tool, so agents have no
// path to read or change it, even though the REST resume response carries
// it (docs/design/viewer-analytics/sign-in-to-view.md "Setting"). Every
// resume-shaped tool output (list_resumes, get_resume, create_resume,
// update_resume_metadata, and the photo/content/customization mutations)
// goes through responseData, so this one seam is the closed proof for all
// of them.
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

// TestResponseDataStripsShowcase proves the community showcase state never
// reaches an agent: no tool reads or changes it, even though the REST resume
// response carries it (docs/design/showcase.md "Opt-in"; AC-SHOW-001).
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

// TestStripAgentOnlyFieldsAppliesToEachListedResume proves list_resumes
// strips signInToView from every element, not only the first, since it
// decodes a distinct envelope shape from responseData's single-object case.
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
