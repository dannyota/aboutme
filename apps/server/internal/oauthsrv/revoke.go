package oauthsrv

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// HandleRevoke implements the RFC 7009 no-oracle response contract. It never
// reads a cookie and returns 200 for every syntactically valid unknown token.
func (s *Service) HandleRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOAuthError(w, http.StatusMethodNotAllowed)
		return
	}
	if values := r.Header.Values("Content-Type"); len(values) != 1 || values[0] != "application/x-www-form-urlencoded" {
		writeOAuthError(w, http.StatusUnsupportedMediaType)
		return
	}
	form, err := decodeOAuthForm(r, map[string]bool{"token": true, "token_type_hint": true})
	if err != nil || !exactRevokeKeys(form) {
		writeOAuthError(w, http.StatusBadRequest)
		return
	}
	if err := s.revokeToken(r.Context(), form.Get("token"), form.Get("token_type_hint")); err != nil {
		writeOAuthServerError(w)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func exactRevokeKeys(form url.Values) bool {
	if len(form) < 1 || len(form) > 2 || form.Get("token") == "" {
		return false
	}
	if hint := form.Get("token_type_hint"); hint != "" && hint != "access_token" && hint != "refresh_token" {
		return false
	}
	return true
}

func (s *Service) revokeToken(ctx context.Context, raw, hint string) (err error) {
	kind, digest, err := ParseToken(raw)
	if err != nil {
		return nil
	}
	_ = kind // token_type_hint is advisory under RFC 7009.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) && err == nil {
			err = rollbackErr
		}
	}()
	q := store.New(tx)
	authority, err := q.GetOAuthTokenAuthorityByDigest(ctx, digest[:])
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	if _, err = q.GetOAuthClientForUpdate(ctx, authority.OAuthToken.ClientID); err != nil {
		return err
	}
	if _, err = q.GetUserForUpdate(ctx, authority.OAuthToken.UserID); err != nil {
		return err
	}
	if _, err = q.GetOAuthGrantForUpdate(ctx, authority.OAuthToken.GrantID); err != nil {
		return err
	}
	now := s.clock()
	if _, err = q.RevokeOAuthGrant(ctx, store.RevokeOAuthGrantParams{ID: authority.OAuthToken.GrantID, RevokedAt: now}); err != nil {
		return err
	}
	if _, err = q.RevokeOAuthTokensForGrant(ctx, store.RevokeOAuthTokensForGrantParams{GrantID: authority.OAuthToken.GrantID, RevokedAt: now}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
