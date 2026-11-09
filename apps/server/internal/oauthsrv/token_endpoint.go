package oauthsrv

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	accessTokenTTL   = time.Hour
	refreshFamilyTTL = 30 * 24 * time.Hour
)

type tokenResponse struct {
	// AccessToken is the server-minted opaque bearer access token.
	AccessToken string `json:"access_token"`
	// TokenType is the RFC 6749 token type, always "Bearer".
	TokenType string `json:"token_type"`
	// ExpiresIn is the access token lifetime in seconds.
	ExpiresIn int64 `json:"expires_in"`
	// RefreshToken is the server-minted opaque refresh token for the family.
	RefreshToken string `json:"refresh_token"`
	// Scope is the grant's canonical space-separated scope set.
	Scope string `json:"scope"`
}

// HandleToken exchanges a single-use code or rotates a refresh token. It is a
// bearer-world endpoint: it deliberately never reads a cookie. No response,
// success or error, may be cached (RFC 6749 section 5.1).
func (s *Service) HandleToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		writeOAuthError(w, http.StatusMethodNotAllowed)
		return
	}
	if values := r.Header.Values("Content-Type"); len(values) != 1 || values[0] != "application/x-www-form-urlencoded" {
		writeOAuthError(w, http.StatusUnsupportedMediaType)
		return
	}
	var admissionTime time.Time
	if s.tokenAdmission != nil {
		admissionTime = s.clock()
		allowed, retryAfter := s.tokenAdmission.AdmitToken(admissionTime, r)
		if !allowed {
			if retryAfter < 1 {
				retryAfter = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			writeOAuthError(w, http.StatusTooManyRequests)
			return
		}
	}
	form, err := decodeOAuthForm(r, map[string]bool{
		"grant_type": true, "code": true, "redirect_uri": true, "client_id": true,
		"code_verifier": true, "refresh_token": true, "resource": true,
	})
	if err != nil {
		if errors.Is(err, errOAuthResourceParse) {
			writeOAuthErrorBody(w, http.StatusBadRequest, "invalid_grant", "The request is invalid.")
			return
		}
		writeOAuthError(w, http.StatusBadRequest)
		return
	}
	grantID, hasGrantID, err := s.grantRateID(r.Context(), form)
	if err != nil && !errors.Is(err, errOAuthNoStoredGrant) {
		writeOAuthServerError(w)
		return
	}
	var attempt grantAttempt
	attemptResult := grantAttemptRelease
	grantRetry := 0
	if hasGrantID && s.tokenAdmission != nil {
		var allowed bool
		var retryAfter int
		attempt, allowed, retryAfter = s.tokenAdmission.AdmitGrant(grantID, admissionTime)
		if !allowed {
			grantRetry = max(1, retryAfter)
		} else {
			defer func() { s.tokenAdmission.FinishGrant(attempt, attemptResult) }()
		}
	}
	var response tokenResponse
	switch form.Get("grant_type") {
	case "authorization_code":
		if !exactAuthorizationCodeForm(form) {
			writeOAuthError(w, http.StatusBadRequest)
			return
		}
		if !s.canonicalResource(form, form.Get(rawResourceFormKey)) {
			writeOAuthErrorBody(w, http.StatusBadRequest, "invalid_grant", "The request is invalid.")
			return
		}
		response, err = s.processAuthorizationCode(r.Context(), form, grantRetry == 0)
	case "refresh_token":
		if !exactRefreshForm(form) {
			writeOAuthError(w, http.StatusBadRequest)
			return
		}
		if !s.canonicalResource(form, form.Get(rawResourceFormKey)) {
			writeOAuthErrorBody(w, http.StatusBadRequest, "invalid_grant", "The request is invalid.")
			return
		}
		response, err = s.processRefreshToken(r.Context(), form.Get("refresh_token"), form["client_id"], grantRetry == 0)
	default:
		writeOAuthErrorBody(w, http.StatusBadRequest, "unsupported_grant_type", "The request is invalid.")
		return
	}
	if err != nil {
		if errors.Is(err, errOAuthInvalidGrant) {
			attemptResult = grantAttemptFailure
		}
		switch {
		case errors.Is(err, errOAuthUnknownClient):
			// RFC 6749 section 5.2: a deleted client gets 401,
			// so an MCP client whose registration was swept registers again.
			writeOAuthErrorBody(w, http.StatusUnauthorized, "invalid_client", "The request is invalid.")
		case errors.Is(err, errOAuthInvalidClient):
			writeOAuthErrorBody(w, http.StatusBadRequest, "invalid_client", "The request is invalid.")
		case errors.Is(err, errOAuthInvalidGrant):
			writeOAuthErrorBody(w, http.StatusBadRequest, "invalid_grant", "The request is invalid.")
		default:
			writeOAuthServerError(w)
		}
		return
	}
	if grantRetry > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(grantRetry))
		writeOAuthError(w, http.StatusTooManyRequests)
		return
	}
	attemptResult = grantAttemptSuccess
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	// application/json body of server-minted tokens and a canonical closed-set scope
	if _, writeErr := w.Write([]byte(`{"access_token":"` + response.AccessToken + `","token_type":"Bearer","expires_in":` + strconv.FormatInt(response.ExpiresIn, 10) + `,"refresh_token":"` + response.RefreshToken + `","scope":"` + response.Scope + `"}`)); writeErr != nil {
		return
	}
}

// Only a stored credential selects failed-grant debt. Public client identifiers
// cannot select an owner's budget; token processing rechecks authority under locks.
// errOAuthNoStoredGrant leaves ordinary token validation to classify the request.
func (s *Service) grantRateID(ctx context.Context, form url.Values) (uuid.UUID, bool, error) {
	if isNilDependency(s.queries) {
		return uuid.Nil, false, errOAuthNoStoredGrant
	}
	switch form.Get("grant_type") {
	case "authorization_code":
		digest, err := ParseCode(form.Get("code"))
		if err != nil {
			return uuid.Nil, false, errOAuthNoStoredGrant
		}
		code, err := s.queries.GetOAuthAuthorizationCodeByDigest(ctx, digest[:])
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, false, errOAuthNoStoredGrant
		}
		if err != nil {
			return uuid.Nil, false, err
		}
		if code.GrantID == uuid.Nil {
			return uuid.Nil, false, errOAuthNoStoredGrant
		}
		return code.GrantID, true, nil
	case "refresh_token":
		kind, digest, err := ParseToken(form.Get("refresh_token"))
		if err != nil || kind != TokenKindRefresh {
			return uuid.Nil, false, errOAuthNoStoredGrant
		}
		authority, err := s.queries.GetOAuthTokenAuthorityByDigest(ctx, digest[:])
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, false, errOAuthNoStoredGrant
		}
		if err != nil {
			return uuid.Nil, false, err
		}
		token := authority.OAuthToken
		if token.Kind != string(TokenKindRefresh) || token.GrantID == uuid.Nil {
			return uuid.Nil, false, errOAuthNoStoredGrant
		}
		return token.GrantID, true, nil
	default:
		return uuid.Nil, false, errOAuthNoStoredGrant
	}
}

var (
	errOAuthInvalidClient = errors.New("oauth invalid client")
	errOAuthUnknownClient = errors.New("oauth unknown client")
	errOAuthInvalidGrant  = errors.New("oauth invalid grant")
	errOAuthResourceParse = errors.New("oauth resource parse")
	errOAuthNoStoredGrant = errors.New("oauth credential has no stored grant")
)
