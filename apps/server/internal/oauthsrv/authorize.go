package oauthsrv

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
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
	w.Header().Set("Cache-Control", "no-store, no-transform")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	values, parseErr := url.ParseQuery(r.URL.RawQuery)
	query, client, trusted, err := s.authorizeRequest(r.Context(), values, r.URL.RawQuery, parseErr, s.requestAddress(r))
	if err != nil {
		var limited *CIMDRateError
		if errors.As(err, &limited) {
			w.Header().Set("Retry-After", strconv.Itoa(limited.RetryAfter))
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		if !errors.Is(err, ErrConsentInvalid) && !errors.Is(err, ErrScopeInvalid) {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		if !trusted {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		http.Redirect(w, r, oauthResultURL(query.RedirectURI, s.publicOrigin, "", oauthErrorFor(err), query.State), http.StatusFound)
		return
	}

	session, ok := auth.SessionFromContext(r.Context())
	if !ok {
		next := authorizePath + "?" + query.values().Encode()
		http.Redirect(w, r, loginPath+"?next="+url.QueryEscape(next), http.StatusFound)
		return
	}
	if client.ID == uuid.Nil || query.metadata != nil && nativeLoopbackRedirect(query.RedirectURI) {
		redirectInternal(w, consentPath+"?"+query.values().Encode())
		return
	}
	grant, err := s.queries.GetLiveOAuthGrant(r.Context(), store.GetLiveOAuthGrantParams{UserID: session.UserID, ClientID: client.ID})
	if err == nil && grant.AuthEpoch == session.AuthEpoch && grantAllows(grant.Scopes, query.Scopes) {
		redirectTo, issueErr := s.issueCode(r.Context(), session, query)
		if issueErr == nil {
			redirectIssuedCode(w, client, query, s.publicOrigin, redirectTo)
			return
		}
		// A stale session or a missing recent second-factor proof cannot mint
		// silent authority; fall back to the interactive consent page instead
		// of a hard failure, mirroring the "no live grant" branch below. See
		// docs/design/second-factor-authentication.md.
		if errors.Is(issueErr, auth.ErrSessionInvalid) || errors.Is(issueErr, auth.ErrReauthRequired) {
			redirectInternal(w, consentPath+"?"+query.values().Encode())
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

func (s *Service) authorizeRequest(ctx context.Context, values url.Values, rawQuery string, parseErr error, address netip.Addr) (ConsentQuery, store.OAuthClient, bool, error) {
	if len(values["client_id"]) != 1 || len(values["redirect_uri"]) != 1 {
		return ConsentQuery{}, store.OAuthClient{}, false, ErrConsentInvalid
	}
	resolved, client, err := s.resolveConsent(ctx, ConsentQuery{PublicClientID: values.Get("client_id"), RedirectURI: values.Get("redirect_uri"), address: address})
	if err != nil {
		return ConsentQuery{}, store.OAuthClient{}, false, err
	}
	resolved.State = values.Get("state")
	if parseErr != nil {
		return resolved, client, true, ErrConsentInvalid
	}
	if !s.canonicalResource(values, rawQuery) {
		return resolved, client, true, ErrConsentInvalid
	}
	query, err := consentQueryFromValues(values)
	if err != nil {
		return resolved, client, true, err
	}
	query.ClientID = resolved.ClientID
	query.metadata = resolved.metadata
	query.address = address
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

// canonicalResource accepts zero or one RFC 8707 resource naming the one
// protected resource: the MCP endpoint URL or the bare origin, byte for byte.
// The first raw resource field must also be the exact url.QueryEscape form of
// that value, so no other percent-encoding, case, or normalization of an
// accepted value passes. See docs/design/mcp-client-compatibility.md.
func (s *Service) canonicalResource(values url.Values, raw string) bool {
	resources, present := values["resource"]
	if !present {
		return true
	}
	if len(resources) != 1 {
		return false
	}
	value := resources[0]
	if value != s.publicOrigin && value != s.publicOrigin+mcpResourcePath {
		return false
	}
	for _, field := range strings.Split(raw, "&") {
		if strings.HasPrefix(field, "resource=") {
			return field == "resource="+url.QueryEscape(value)
		}
	}
	return false
}

// redirectIssuedCode permits only the exact result constructed from a
// registered redirect URI. It keeps the authorize handler's redirect sink
// separate from request-derived values.
func redirectIssuedCode(w http.ResponseWriter, client store.OAuthClient, request ConsentQuery, issuer, target string) {
	targetURL, err := url.Parse(target)
	if err != nil || !consentRedirectMatches(client, request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	values := targetURL.Query()
	if target != oauthResultURL(request.RedirectURI, issuer, values.Get("code"), "", request.State) {
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
	publicID := values.Get(clientIDKey)
	clientID, err := uuid.Parse(publicID)
	if err != nil && !strings.HasPrefix(publicID, "https://") {
		return ConsentQuery{}, ErrConsentInvalid
	}
	return ConsentQuery{
		PublicClientID:      publicID,
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

func oauthResultURL(redirectURI, issuer, code, oauthError, state string) string {
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
	values.Set("iss", issuer)
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
