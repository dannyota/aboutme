package oauthsrv

import "net/http"

// mcpResourcePath is the MCP endpoint path. Origin plus this path is the
// protected resource identifier a person enters in an MCP client.
const mcpResourcePath = "/mcp"

// HandleMetadata serves the configured RFC 8414 authorization-server document.
// Its bytes depend only on the validated canonical public origin.
func (s *Service) HandleMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"issuer":"` + s.publicOrigin + `","authorization_response_iss_parameter_supported":true,"authorization_endpoint":"` + s.publicOrigin + `/oauth/authorize","token_endpoint":"` + s.publicOrigin + `/oauth/token","registration_endpoint":"` + s.publicOrigin + `/oauth/register","revocation_endpoint":"` + s.publicOrigin + `/oauth/revoke","response_types_supported":["code"],"grant_types_supported":["authorization_code","refresh_token"],"code_challenge_methods_supported":["S256"],"token_endpoint_auth_methods_supported":["none"],"scopes_supported":["resumes:read","resumes:write"]}`)); err != nil {
		return
	}
}

// HandleProtectedResourceMetadata serves the configured RFC 9728 document.
// Its bytes depend only on the validated canonical public origin. The resource
// is the MCP endpoint URL a person enters; a client that finds this document
// through the /mcp challenge's resource_metadata URL checks it against the URL
// it called (RFC 9728 section 3.3). The authorization server is the origin.
// See docs/design/mcp-client-compatibility.md.
func (s *Service) HandleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"resource":"` + s.publicOrigin + mcpResourcePath + `","authorization_servers":["` + s.publicOrigin + `"],"scopes_supported":["resumes:read","resumes:write"],"bearer_methods_supported":["header"]}`)); err != nil {
		return
	}
}
