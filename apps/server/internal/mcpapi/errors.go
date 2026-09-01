package mcpapi

import (
	"errors"
	"net/http"
)

type mcpError struct {
	status    int
	code      string
	challenge string
}

func (e *mcpError) Error() string { return e.code }

var (
	errScopeDenied            = &mcpError{status: http.StatusForbidden, code: "scope_denied"}
	errPayloadTooLarge        = &mcpError{status: http.StatusRequestEntityTooLarge, code: "payload_too_large"}
	errRateLimited            = &mcpError{status: http.StatusTooManyRequests, code: "rate_limited"}
	errInvalidRequest         = &mcpError{status: http.StatusBadRequest, code: "invalid_request"}
	errAgentAccessUnavailable = &mcpError{status: http.StatusServiceUnavailable, code: "agent_access_unavailable"}
	errInternal               = &mcpError{status: http.StatusInternalServerError, code: "internal_error"}
)

// writeMCPError writes a closed HTTP-boundary error before any JSON-RPC work.
func writeMCPError(w http.ResponseWriter, err error) {
	var mcpErr *mcpError
	if !errors.As(err, &mcpErr) {
		mcpErr = errInternal
	}
	if mcpErr.status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+mcpErr.challenge+`/.well-known/oauth-protected-resource"`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(mcpErr.status)
	if _, writeErr := w.Write([]byte(`{"error":"` + mcpErr.code + `"}`)); writeErr != nil {
		return
	}
}
