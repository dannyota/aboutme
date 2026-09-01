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
	errScopeDenied = &mcpError{status: http.StatusForbidden, code: "scope_denied"}
	errInternal    = &mcpError{status: http.StatusInternalServerError, code: "internal_error"}
)

// writeMCPError writes the closed unauthenticated and scope-denied mappings.
// Later MCP transport code uses the same mapping before any JSON-RPC work.
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
	_, _ = w.Write([]byte(`{"error":"` + mcpErr.code + `"}`))
}
