package oauthsrv

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
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
	form, err := decodeOAuthForm(r, map[string]bool{
		"grant_type": true, "code": true, "redirect_uri": true, "client_id": true,
		"code_verifier": true, "refresh_token": true,
	})
	if err != nil {
		writeOAuthError(w, http.StatusBadRequest)
		return
	}
	var response tokenResponse
	switch form.Get("grant_type") {
	case "authorization_code":
		if !exactFormKeys(form, "grant_type", "code", "redirect_uri", "client_id", "code_verifier") {
			writeOAuthError(w, http.StatusBadRequest)
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
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"access_token":"` + response.AccessToken + `","token_type":"Bearer","expires_in":` + strconv.FormatInt(response.ExpiresIn, 10) + `,"refresh_token":"` + response.RefreshToken + `","scope":"` + response.Scope + `"}`))
}

var (
	errOAuthInvalidClient = errors.New("oauth invalid client")
	errOAuthInvalidGrant  = errors.New("oauth invalid grant")
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
		return nil, errors.New("oauth form parse")
	}
	for key, values := range form {
		if !allowed[key] || len(values) != 1 {
			return nil, errors.New("oauth form keys")
		}
	}
	return form, nil
}

func exactFormKeys(form url.Values, keys ...string) bool {
	if len(form) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := form[key]; !ok || form.Get(key) == "" {
			return false
		}
	}
	return true
}

func (s *Service) exchangeAuthorizationCode(ctx context.Context, form url.Values) (tokenResponse, error) {
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
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := store.New(tx)
	if _, err := q.GetOAuthClientForUpdate(ctx, clientID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return tokenResponse{}, errOAuthInvalidClient
		}
		return tokenResponse{}, err
	}
	if _, err := q.GetUserForUpdate(ctx, preCode.UserID); err != nil {
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
			if _, err := q.RevokeOAuthTokenFamily(ctx, store.RevokeOAuthTokenFamilyParams{FamilyID: *code.IssuedFamilyID, RevokedAt: now}); err != nil {
				return tokenResponse{}, err
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return tokenResponse{}, err
		}
		return tokenResponse{}, errOAuthInvalidGrant
	}
	if code.Scopes != grant.Scopes || code.ExpiresAt.Compare(now) <= 0 {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	familyID := uuid.New()
	if _, err := q.ConsumeOAuthAuthorizationCode(ctx, store.ConsumeOAuthAuthorizationCodeParams{CodeDigest: digest[:], ConsumedAt: now, IssuedFamilyID: familyID}); err != nil {
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
		if _, err := q.CreateOAuthToken(ctx, store.CreateOAuthTokenParams{TokenDigest: token.digest[:], Kind: string(token.kind), FamilyID: familyID, ClientID: code.ClientID, UserID: code.UserID, GrantID: grant.ID, CreatedAt: now, ExpiresAt: token.expires, FamilyExpiresAt: familyExpiry}); err != nil {
			return tokenResponse{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return tokenResponse{}, err
	}
	return tokenResponse{AccessToken: access, TokenType: "Bearer", ExpiresIn: int64(accessTokenTTL / time.Second), RefreshToken: refresh, Scope: code.Scopes}, nil
}

func (s *Service) rotateRefreshToken(ctx context.Context, raw string) (tokenResponse, error) {
	kind, digest, err := ParseToken(raw)
	if err != nil || kind != TokenKindRefresh {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return tokenResponse{}, err
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	q := store.New(tx)
	authority, err := q.GetOAuthTokenAuthorityByDigest(ctx, digest[:])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return tokenResponse{}, errOAuthInvalidGrant
		}
		return tokenResponse{}, err
	}
	if _, err := q.GetOAuthClientForUpdate(ctx, authority.OAuthToken.ClientID); err != nil {
		return tokenResponse{}, err
	}
	if _, err := q.GetUserForUpdate(ctx, authority.OAuthToken.UserID); err != nil {
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
	if grant.RevokedAt != nil || token.RevokedAt != nil || token.ExpiresAt.Compare(now) <= 0 || token.FamilyExpiresAt.Compare(now) <= 0 {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	if token.SupersededAt != nil {
		if _, err := q.RevokeOAuthTokenFamily(ctx, store.RevokeOAuthTokenFamilyParams{FamilyID: token.FamilyID, RevokedAt: now}); err != nil {
			return tokenResponse{}, err
		}
		if err := tx.Commit(ctx); err != nil {
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
	if _, err := q.SupersedeOAuthToken(ctx, store.SupersedeOAuthTokenParams{ID: token.ID, FamilyID: token.FamilyID, SupersededAt: now}); err != nil {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	if _, err := q.InsertRotatedOAuthToken(ctx, store.InsertRotatedOAuthTokenParams{TokenDigest: refreshDigest[:], Kind: string(TokenKindRefresh), CreatedAt: now, ExpiresAt: token.FamilyExpiresAt, RotatedFrom: token.ID}); err != nil {
		return tokenResponse{}, err
	}
	accessExpiresAt := now.Add(accessTokenTTL)
	if token.FamilyExpiresAt.Before(accessExpiresAt) {
		accessExpiresAt = token.FamilyExpiresAt
	}
	if _, err := q.CreateOAuthToken(ctx, store.CreateOAuthTokenParams{TokenDigest: accessDigest[:], Kind: string(TokenKindAccess), FamilyID: token.FamilyID, ClientID: token.ClientID, UserID: token.UserID, GrantID: token.GrantID, CreatedAt: now, ExpiresAt: accessExpiresAt, FamilyExpiresAt: token.FamilyExpiresAt}); err != nil {
		return tokenResponse{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return tokenResponse{}, err
	}
	return tokenResponse{AccessToken: access, TokenType: "Bearer", ExpiresIn: int64(accessExpiresAt.Sub(now) / time.Second), RefreshToken: refresh, Scope: grant.Scopes}, nil
}
