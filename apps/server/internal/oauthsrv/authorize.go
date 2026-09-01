package oauthsrv

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const (
	authorizePath = "/oauth/authorize"
	consentPath   = "/authorize"
	loginPath     = "/login"
	stateMaxBytes = 512
)

// HandleAuthorize validates an authorization-code request before redirecting
// either to login, stateless consent, or an exact registered redirect URI.
func (s *Service) HandleAuthorize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	query, client, trusted, err := s.authorizeRequest(r.Context(), r.URL.Query())
	if err != nil {
		if !trusted {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, oauthResultURL(query.RedirectURI, "", oauthErrorFor(err), query.State), http.StatusFound)
		return
	}

	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		next := authorizePath + "?" + query.values().Encode()
		http.Redirect(w, r, loginPath+"?next="+url.QueryEscape(next), http.StatusFound)
		return
	}
	grant, err := s.queries.GetLiveOAuthGrant(r.Context(), store.GetLiveOAuthGrantParams{UserID: session.UserID, ClientID: client.ID})
	if err == nil && grantAllows(grant.Scopes, query.Scopes) {
		redirectTo, issueErr := s.issueCode(r.Context(), session.UserID, query)
		if issueErr == nil {
			redirectIssuedCode(w, client, query, redirectTo)
			return
		}
		if errors.Is(issueErr, ErrConsentInvalid) {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	redirectInternal(w, consentPath+"?"+query.values().Encode())
}

func oauthErrorFor(err error) string {
	if errors.Is(err, ErrScopeInvalid) {
		return "invalid_scope"
	}
	return "invalid_request"
}

func (s *Service) authorizeRequest(ctx context.Context, values url.Values) (ConsentQuery, store.OAuthClient, bool, error) {
	if len(values["client_id"]) != 1 || len(values["redirect_uri"]) != 1 {
		return ConsentQuery{}, store.OAuthClient{}, false, ErrConsentInvalid
	}
	clientID, err := uuid.Parse(values.Get("client_id"))
	if err != nil {
		return ConsentQuery{}, store.OAuthClient{}, false, ErrConsentInvalid
	}
	client, err := s.queries.GetOAuthClient(ctx, clientID)
	if err != nil {
		return ConsentQuery{}, store.OAuthClient{}, false, ErrConsentInvalid
	}
	if !registeredRedirect(client, values.Get("redirect_uri")) {
		return ConsentQuery{}, store.OAuthClient{}, false, ErrConsentInvalid
	}
	query, err := consentQueryFromValues(values)
	if err != nil {
		return ConsentQuery{ClientID: clientID, RedirectURI: values.Get("redirect_uri"), State: values.Get("state")}, client, true, err
	}
	if err := validateConsentQuery(query); err != nil {
		return query, client, true, err
	}
	scopes, scopeErr := query.parsedScopes()
	if scopeErr != nil {
		return query, client, true, ErrConsentInvalid
	}
	query.Scopes = scopes
	return query, client, true, nil
}

// redirectIssuedCode permits only the exact result constructed from a
// registered redirect URI. It keeps the authorize handler's redirect sink
// separate from request-derived values.
func redirectIssuedCode(w http.ResponseWriter, client store.OAuthClient, request ConsentQuery, target string) {
	targetURL, err := url.Parse(target)
	if err != nil || !registeredRedirect(client, request.RedirectURI) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	values := targetURL.Query()
	if target != oauthResultURL(request.RedirectURI, values.Get("code"), "", request.State) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeFoundRedirect(w, target)
}

// redirectInternal only permits an absolute-path redirect on this origin.
func redirectInternal(w http.ResponseWriter, target string) {
	targetURL, err := url.ParseRequestURI(target)
	if err != nil || !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") || targetURL.IsAbs() || targetURL.Host != "" {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeFoundRedirect(w, target)
}

func writeFoundRedirect(w http.ResponseWriter, target string) {
	w.Header().Set("Location", target)
	w.WriteHeader(http.StatusFound)
}

func consentQueryFromValues(values url.Values) (ConsentQuery, error) {
	const (
		clientIDKey  = "client_id"
		redirectKey  = "redirect_uri"
		responseKey  = "response_type"
		scopeKey     = "scope"
		stateKey     = "state"
		challengeKey = "code_challenge"
		methodKey    = "code_challenge_method"
	)
	for _, key := range []string{clientIDKey, redirectKey, responseKey, scopeKey, challengeKey, methodKey} {
		if len(values[key]) != 1 {
			return ConsentQuery{}, ErrConsentInvalid
		}
	}
	if len(values[stateKey]) > 1 {
		return ConsentQuery{}, ErrConsentInvalid
	}
	clientID, err := uuid.Parse(values.Get(clientIDKey))
	if err != nil {
		return ConsentQuery{}, ErrConsentInvalid
	}
	return ConsentQuery{
		ClientID:            clientID,
		RedirectURI:         values.Get(redirectKey),
		ResponseType:        values.Get(responseKey),
		Scope:               values.Get(scopeKey),
		State:               values.Get(stateKey),
		CodeChallenge:       values.Get(challengeKey),
		CodeChallengeMethod: values.Get(methodKey),
	}, nil
}

func registeredRedirect(client store.OAuthClient, redirectURI string) bool {
	if ValidateRedirectURI(redirectURI) != nil {
		return false
	}
	var redirects []string
	if err := json.Unmarshal(client.RedirectURIs, &redirects); err != nil {
		return false
	}
	for _, registered := range redirects {
		if redirectURI == registered {
			return true
		}
	}
	return false
}

func grantAllows(raw string, requested Scopes) bool {
	granted, err := ParseScopes(raw)
	if err != nil {
		return false
	}
	for _, scope := range requested {
		if !granted.Has(scope) {
			return false
		}
	}
	return true
}

func oauthResultURL(redirectURI, code, oauthError, state string) string {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return ""
	}
	values := u.Query()
	if code != "" {
		values.Set("code", code)
	}
	if oauthError != "" {
		values.Set("error", oauthError)
	}
	if state != "" && len(state) <= stateMaxBytes {
		values.Set("state", state)
	}
	u.RawQuery = values.Encode()
	return u.String()
}

func isS256Challenge(raw string) bool {
	if len(raw) != secretBodyChars {
		return false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	return err == nil && len(decoded) == 32
}
