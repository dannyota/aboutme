package oauthsrv

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"danny.vn/aboutme/apps/server/internal/store"
)

// HandleRegister registers a public OAuth client. It intentionally never
// reads a session cookie and has no CSRF check because it is a raw protocol
// endpoint rather than a browser-authenticated route.
func (s *Service) HandleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOAuthError(w, http.StatusMethodNotAllowed)
		return
	}
	if !exactJSONContentType(r.Header) {
		writeOAuthError(w, http.StatusUnsupportedMediaType)
		return
	}
	allowed, retryAfter := s.registerAdmission.AdmitRegister(s.clock(), r)
	if !allowed {
		if retryAfter < 1 {
			retryAfter = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		writeOAuthError(w, http.StatusTooManyRequests)
		return
	}

	registration, err := decodeRegistration(r.Body)
	if err != nil {
		if errors.Is(err, errRegisterRequestTooLarge) {
			writeOAuthError(w, http.StatusRequestEntityTooLarge)
			return
		}
		writeOAuthError(w, http.StatusBadRequest)
		return
	}
	clientName, err := ValidateClientName(registration.ClientName)
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest)
		return
	}
	if len(registration.RedirectURIs) < 1 || len(registration.RedirectURIs) > maxRedirectURIs {
		writeOAuthError(w, http.StatusBadRequest)
		return
	}
	for _, redirectURI := range registration.RedirectURIs {
		if ValidateRedirectURI(redirectURI) != nil {
			writeOAuthError(w, http.StatusBadRequest)
			return
		}
	}
	if registration.ApplicationTypeSet {
		switch registration.ApplicationType {
		case "native":
			for _, redirectURI := range registration.RedirectURIs {
				if !nativeLoopbackRedirect(redirectURI) {
					writeOAuthError(w, http.StatusBadRequest)
					return
				}
			}
		case "web":
			for _, redirectURI := range registration.RedirectURIs {
				if !webRedirect(redirectURI) {
					writeOAuthError(w, http.StatusBadRequest)
					return
				}
			}
		default:
			writeOAuthError(w, http.StatusBadRequest)
			return
		}
	}
	if registration.GrantTypesSet && !validGrantTypesSet(registration.GrantTypes) {
		writeOAuthError(w, http.StatusBadRequest)
		return
	}
	if registration.ResponseTypesSet && !validResponseTypesSet(registration.ResponseTypes) {
		writeOAuthError(w, http.StatusBadRequest)
		return
	}
	if registration.ScopeSet {
		if _, scopeErr := ParseScopes(registration.Scope); scopeErr != nil {
			writeOAuthError(w, http.StatusBadRequest)
			return
		}
	}
	if registration.TokenEndpointAuthMethod != "" && registration.TokenEndpointAuthMethod != "none" {
		writeOAuthError(w, http.StatusBadRequest)
		return
	}

	redirectURIs, err := json.Marshal(registration.RedirectURIs)
	if err != nil {
		writeOAuthServerError(w)
		return
	}
	if gcErr := s.CollectIdleClients(r.Context()); gcErr != nil {
		writeOAuthServerError(w)
		return
	}
	client, err := s.queries.CreateOAuthClient(r.Context(), store.CreateOAuthClientParams{
		ClientName:   clientName,
		RedirectURIs: redirectURIs,
		CreatedAt:    s.clock(),
	})
	if err != nil {
		writeOAuthServerError(w)
		return
	}

	response := registerResponse{
		ClientID:                client.ID.String(),
		ClientName:              clientName,
		RedirectURIs:            registration.RedirectURIs,
		TokenEndpointAuthMethod: "none",
	}
	if registration.ApplicationTypeSet {
		response.ApplicationType = registration.ApplicationType
	}
	responseJSON, err := json.Marshal(response)
	if err != nil {
		writeOAuthServerError(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if _, err := w.Write(responseJSON); err != nil {
		return
	}
}

// registerResponse is the 201 body of a successful registration.
type registerResponse struct {
	// ClientID is the issued client_id, the new client row's UUID.
	ClientID string `json:"client_id"`
	// ClientName is the canonical client name ValidateClientName returned, as
	// stored.
	ClientName string `json:"client_name"`
	// RedirectURIs echoes the registered redirect URIs in request order.
	RedirectURIs []string `json:"redirect_uris"`
	// TokenEndpointAuthMethod is always "none": every registered client is
	// public.
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method"`
	// ApplicationType echoes the request's application_type and is omitted
	// when the request did not name it.
	ApplicationType string `json:"application_type,omitempty"`
}

func exactJSONContentType(header http.Header) bool {
	values := header.Values("Content-Type")
	return len(values) == 1 && values[0] == "application/json"
}

func writeOAuthError(w http.ResponseWriter, status int) {
	writeOAuthErrorBody(w, status, "invalid_request", "The request is invalid.")
}

func writeOAuthServerError(w http.ResponseWriter) {
	writeOAuthErrorBody(w, http.StatusInternalServerError, "server_error", "The server encountered an error.")
}

func writeOAuthErrorBody(w http.ResponseWriter, status int, code, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := fmt.Fprintf(w, `{"error":%q,"error_description":%q}`, code, description); err != nil {
		return
	}
}
