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
