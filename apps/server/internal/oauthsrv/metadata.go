package oauthsrv

import "net/http"

const mcpResourcePath = "/mcp"

func (s *Service) HandleMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	cimd := ""
	if s.cimdEnabled {
		cimd = `,"client_id_metadata_document_supported":true`
	}
	if _, err := w.Write([]byte(`{"issuer":"` + s.publicOrigin + `","authorization_response_iss_parameter_supported":true,"authorization_endpoint":"` + s.publicOrigin + `/oauth/authorize","token_endpoint":"` + s.publicOrigin + `/oauth/token","registration_endpoint":"` + s.publicOrigin + `/oauth/register","revocation_endpoint":"` + s.publicOrigin + `/oauth/revoke","response_types_supported":["code"],"grant_types_supported":["authorization_code","refresh_token"],"code_challenge_methods_supported":["S256"],"token_endpoint_auth_methods_supported":["none"],"scopes_supported":["resumes:read","resumes:write","projects:read","projects:write"]` + cimd + `}`)); err != nil {
		return
	}
}

func (s *Service) HandleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"resource":"` + s.publicOrigin + mcpResourcePath + `","authorization_servers":["` + s.publicOrigin + `"],"scopes_supported":["resumes:read","resumes:write","projects:read","projects:write"],"bearer_methods_supported":["header"]}`)); err != nil {
		return
	}
}
