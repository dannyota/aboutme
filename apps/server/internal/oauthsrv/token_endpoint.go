package oauthsrv

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const (
	oauthFormBodyLimit = 4096
	accessTokenTTL     = time.Hour
	refreshFamilyTTL   = 30 * 24 * time.Hour
)

const rawResourceFormKey = "\x00oauthsrv_raw_resource"

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// HandleToken exchanges a single-use code or rotates a refresh token. It is a
// bearer-world endpoint: it deliberately never reads a cookie.
func (s *Service) HandleToken(w http.ResponseWriter, r *http.Request) {
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
	clientID, hasClientID := s.grantRateClientID(r.Context(), form)
	var attempt grantAttempt
	attemptResult := grantAttemptRelease
	if hasClientID && s.tokenAdmission != nil {
		var allowed bool
		var retryAfter int
		attempt, allowed, retryAfter = s.tokenAdmission.AdmitGrant(clientID, admissionTime)
		if !allowed {
			if retryAfter < 1 {
				retryAfter = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			writeOAuthError(w, http.StatusTooManyRequests)
			return
		}
		defer func() { s.tokenAdmission.FinishGrant(attempt, attemptResult) }()
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
		response, err = s.exchangeAuthorizationCode(r.Context(), form)
	case "refresh_token":
		if !exactFormKeys(form, "grant_type", "refresh_token") {
			writeOAuthError(w, http.StatusBadRequest)
			return
		}
		response, err = s.rotateRefreshToken(r.Context(), form.Get("refresh_token"))
	default:
		writeOAuthErrorBody(w, http.StatusBadRequest, "unsupported_grant_type", "The request is invalid.")
		return
	}
	if err != nil {
		if errors.Is(err, errOAuthInvalidGrant) {
			attemptResult = grantAttemptFailure
		}
		switch {
		case errors.Is(err, errOAuthInvalidClient):
			writeOAuthErrorBody(w, http.StatusBadRequest, "invalid_client", "The request is invalid.")
		case errors.Is(err, errOAuthInvalidGrant):
			writeOAuthErrorBody(w, http.StatusBadRequest, "invalid_grant", "The request is invalid.")
		default:
			writeOAuthServerError(w)
		}
		return
	}
	attemptResult = grantAttemptSuccess
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	// nosemgrep: go.net.xss.no-direct-write-to-responsewriter-taint.no-direct-write-to-responsewriter-taint -- application/json body of server-minted tokens and a canonical closed-set scope
	if _, writeErr := w.Write([]byte(`{"access_token":"` + response.AccessToken + `","token_type":"Bearer","expires_in":` + strconv.FormatInt(response.ExpiresIn, 10) + `,"refresh_token":"` + response.RefreshToken + `","scope":"` + response.Scope + `"}`)); writeErr != nil {
		return
	}
}

func (s *Service) grantRateClientID(ctx context.Context, form url.Values) (uuid.UUID, bool) {
	switch form.Get("grant_type") {
	case "authorization_code":
		clientID, err := uuid.Parse(form.Get("client_id"))
		return clientID, err == nil && clientID != uuid.Nil
	case "refresh_token":
		kind, digest, err := ParseToken(form.Get("refresh_token"))
		if err != nil || kind != TokenKindRefresh || isNilDependency(s.queries) {
			return uuid.Nil, false
		}
		authority, err := s.queries.GetOAuthTokenAuthorityByDigest(ctx, digest[:])
		if err != nil || authority.OAuthToken.ClientID == uuid.Nil {
			return uuid.Nil, false
		}
		return authority.OAuthToken.ClientID, true
	default:
		return uuid.Nil, false
	}
}

var (
	errOAuthInvalidClient = errors.New("oauth invalid client")
	errOAuthInvalidGrant  = errors.New("oauth invalid grant")
	errOAuthResourceParse = errors.New("oauth resource parse")
)

func decodeOAuthForm(r *http.Request, allowed map[string]bool) (url.Values, error) {
	values := r.Header.Values("Content-Type")
	if len(values) != 1 || values[0] != "application/x-www-form-urlencoded" {
		return nil, errors.New("oauth form media type")
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, oauthFormBodyLimit+1))
	if err != nil || len(raw) > oauthFormBodyLimit {
		return nil, errors.New("oauth form body")
	}
	form, err := url.ParseQuery(string(raw))
	if err != nil {
		if malformedAuthorizationCodeResource(form, string(raw)) {
			return nil, errOAuthResourceParse
		}
		return nil, errors.New("oauth form parse")
	}
	for key, values := range form {
		if !allowed[key] || (key != "resource" && len(values) != 1) {
			return nil, errors.New("oauth form keys")
		}
	}
	if _, ok := form["resource"]; ok {
		form[rawResourceFormKey] = []string{string(raw)}
	}
	return form, nil
}

func malformedAuthorizationCodeResource(form url.Values, raw string) bool {
	fields := strings.Split(raw, "&")
	if len(fields) != 6 {
		return false
	}
	resources := 0
	for _, field := range fields {
		if strings.HasPrefix(field, "resource=") {
			resources++
		}
	}
	if resources != 1 || len(form) != 5 || form.Get("grant_type") != "authorization_code" {
		return false
	}
	return exactFormKeys(form, "grant_type", "code", "redirect_uri", "client_id", "code_verifier")
}

func exactFormKeys(form url.Values, keys ...string) bool {
	count := len(form)
	if _, ok := form[rawResourceFormKey]; ok {
		count--
	}
	if count != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := form[key]; !ok || form.Get(key) == "" {
			return false
		}
	}
	return true
}

func exactAuthorizationCodeForm(form url.Values) bool {
	keys := []string{"grant_type", "code", "redirect_uri", "client_id", "code_verifier"}
	if _, ok := form["resource"]; ok {
		keys = append(keys, "resource")
	}
	count := len(form)
	if _, ok := form[rawResourceFormKey]; ok {
		count--
	}
	if count != len(keys) {
		return false
	}
	for _, key := range keys[:5] {
		if len(form[key]) != 1 || form.Get(key) == "" {
			return false
		}
	}
	return true
}

func (s *Service) exchangeAuthorizationCode(ctx context.Context, form url.Values) (response tokenResponse, err error) {
	digest, err := ParseCode(form.Get("code"))
	if err != nil {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	clientID, err := uuid.Parse(form.Get("client_id"))
	if err != nil {
		return tokenResponse{}, errOAuthInvalidClient
	}
	// This unlocked lookup only discovers the lock-order identities. The row is
	// loaded again under lock before any validation or mutation.
	preCode, err := s.queries.GetOAuthAuthorizationCodeByDigest(ctx, digest[:])
	if err != nil {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return tokenResponse{}, err
	}
	defer func() {
		if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) && err == nil {
			response = tokenResponse{}
			err = rollbackErr
		}
	}()
	q := store.New(tx)
	if _, err = q.GetOAuthClientForUpdate(ctx, clientID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return tokenResponse{}, errOAuthInvalidClient
		}
		return tokenResponse{}, err
	}
	user, err := q.GetUserForUpdate(ctx, preCode.UserID)
	if err != nil {
		return tokenResponse{}, err
	}
	grant, err := q.GetLiveOAuthGrant(ctx, store.GetLiveOAuthGrantParams{UserID: preCode.UserID, ClientID: preCode.ClientID})
	if err != nil {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	grant, err = q.GetOAuthGrantForUpdate(ctx, grant.ID)
	if err != nil {
		return tokenResponse{}, err
	}
	// A grant revoked between the unlocked read above and this lock -- by a
	// concurrent RFC 7009 revoke or an authentication-epoch change -- must
	// still close the exchange rather than mint authority from a dead row.
	if grant.RevokedAt != nil {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	code, err := q.GetOAuthAuthorizationCodeByDigestForUpdate(ctx, digest[:])
	if err != nil {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	now := s.clock()
	if code.ClientID != clientID || code.RedirectURI != form.Get("redirect_uri") || !VerifyS256(code.CodeChallenge, form.Get("code_verifier")) {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	if code.ConsumedAt != nil {
		if code.IssuedFamilyID != nil {
			if _, err = q.RevokeOAuthTokenFamily(ctx, store.RevokeOAuthTokenFamilyParams{FamilyID: *code.IssuedFamilyID, RevokedAt: now}); err != nil {
				return tokenResponse{}, err
			}
		}
		if err = tx.Commit(ctx); err != nil {
			return tokenResponse{}, err
		}
		return tokenResponse{}, errOAuthInvalidGrant
	}
	// The code must still name the exact grant it was issued for and the
	// account's current authentication epoch: a foreign grant (narrowed or
	// replaced since issue) and a stale epoch (an intervening factor change)
	// both close the exchange without consuming the code. See
	// docs/design/second-factor-authentication.md.
	if code.GrantID != grant.ID || code.AuthEpoch != user.AuthEpoch || code.Scopes != grant.Scopes || code.ExpiresAt.Compare(now) <= 0 {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	familyID := uuid.New()
	if _, err = q.ConsumeOAuthAuthorizationCode(ctx, store.ConsumeOAuthAuthorizationCodeParams{CodeDigest: digest[:], ConsumedAt: now, IssuedFamilyID: familyID}); err != nil {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	oauthEntropyMu.Lock()
	access, accessDigest, err := NewToken(TokenKindAccess, s.entropy)
	var refresh string
	var refreshDigest [32]byte
	if err == nil {
		var refreshErr error
		refresh, refreshDigest, refreshErr = NewToken(TokenKindRefresh, s.entropy)
		err = refreshErr
	}
	oauthEntropyMu.Unlock()
	if err != nil {
		return tokenResponse{}, err
	}
	familyExpiry := now.Add(refreshFamilyTTL)
	for _, token := range []struct {
		digest  [32]byte
		kind    TokenKind
		expires time.Time
	}{{accessDigest, TokenKindAccess, now.Add(accessTokenTTL)}, {refreshDigest, TokenKindRefresh, familyExpiry}} {
		if _, err = q.CreateOAuthToken(ctx, store.CreateOAuthTokenParams{TokenDigest: token.digest[:], Kind: string(token.kind), FamilyID: familyID, ClientID: code.ClientID, UserID: code.UserID, GrantID: grant.ID, CreatedAt: now, ExpiresAt: token.expires, FamilyExpiresAt: familyExpiry}); err != nil {
			return tokenResponse{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return tokenResponse{}, err
	}
	return tokenResponse{AccessToken: access, TokenType: "Bearer", ExpiresIn: int64(accessTokenTTL / time.Second), RefreshToken: refresh, Scope: code.Scopes}, nil
}

func (s *Service) rotateRefreshToken(ctx context.Context, raw string) (response tokenResponse, err error) {
	kind, digest, err := ParseToken(raw)
	if err != nil || kind != TokenKindRefresh {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return tokenResponse{}, err
	}
	defer func() {
		if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) && err == nil {
			response = tokenResponse{}
			err = rollbackErr
		}
	}()
	q := store.New(tx)
	authority, err := q.GetOAuthTokenAuthorityByDigest(ctx, digest[:])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return tokenResponse{}, errOAuthInvalidGrant
		}
		return tokenResponse{}, err
	}
	if _, err = q.GetOAuthClientForUpdate(ctx, authority.OAuthToken.ClientID); err != nil {
		return tokenResponse{}, err
	}
	user, err := q.GetUserForUpdate(ctx, authority.OAuthToken.UserID)
	if err != nil {
		return tokenResponse{}, err
	}
	grant, err := q.GetOAuthGrantForUpdate(ctx, authority.OAuthToken.GrantID)
	if err != nil {
		return tokenResponse{}, err
	}
	authority, err = q.GetOAuthTokenAuthorityByDigest(ctx, digest[:])
	if err != nil {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	now := s.clock()
	token := authority.OAuthToken
	// The refresh token's grant must still be live, belong to this account,
	// and carry the account's current authentication epoch. A stale epoch --
	// an intervening factor change -- rejects rotation exactly like a revoked
	// grant. See docs/design/second-factor-authentication.md.
	if grant.RevokedAt != nil || token.RevokedAt != nil || token.ExpiresAt.Compare(now) <= 0 || token.FamilyExpiresAt.Compare(now) <= 0 ||
		token.UserID != user.ID || token.GrantID != grant.ID || grant.AuthEpoch != user.AuthEpoch {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	if token.SupersededAt != nil {
		if _, err = q.RevokeOAuthTokenFamily(ctx, store.RevokeOAuthTokenFamilyParams{FamilyID: token.FamilyID, RevokedAt: now}); err != nil {
			return tokenResponse{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return tokenResponse{}, err
		}
		return tokenResponse{}, errOAuthInvalidGrant
	}
	oauthEntropyMu.Lock()
	access, accessDigest, err := NewToken(TokenKindAccess, s.entropy)
	var refresh string
	var refreshDigest [32]byte
	if err == nil {
		var refreshErr error
		refresh, refreshDigest, refreshErr = NewToken(TokenKindRefresh, s.entropy)
		err = refreshErr
	}
	oauthEntropyMu.Unlock()
	if err != nil {
		return tokenResponse{}, err
	}
	if _, err = q.SupersedeOAuthToken(ctx, store.SupersedeOAuthTokenParams{ID: token.ID, FamilyID: token.FamilyID, SupersededAt: now}); err != nil {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	if _, err = q.InsertRotatedOAuthToken(ctx, store.InsertRotatedOAuthTokenParams{TokenDigest: refreshDigest[:], Kind: string(TokenKindRefresh), CreatedAt: now, ExpiresAt: token.FamilyExpiresAt, RotatedFrom: token.ID}); err != nil {
		return tokenResponse{}, err
	}
	accessExpiresAt := now.Add(accessTokenTTL)
	if token.FamilyExpiresAt.Before(accessExpiresAt) {
		accessExpiresAt = token.FamilyExpiresAt
	}
	if _, err = q.CreateOAuthToken(ctx, store.CreateOAuthTokenParams{TokenDigest: accessDigest[:], Kind: string(TokenKindAccess), FamilyID: token.FamilyID, ClientID: token.ClientID, UserID: token.UserID, GrantID: token.GrantID, CreatedAt: now, ExpiresAt: accessExpiresAt, FamilyExpiresAt: token.FamilyExpiresAt}); err != nil {
		return tokenResponse{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return tokenResponse{}, err
	}
	return tokenResponse{AccessToken: access, TokenType: "Bearer", ExpiresIn: int64(accessExpiresAt.Sub(now) / time.Second), RefreshToken: refresh, Scope: grant.Scopes}, nil
}
