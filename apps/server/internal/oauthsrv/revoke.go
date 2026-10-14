package oauthsrv

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// grantRevocationSweepLimit bounds one epoch-change revocation pass to a
// constant rather than a function of table size, matching the other bounded
// sweeps in this package. The live-grant cap in docs/design/security.md
// keeps one account's live grants well under this bound, so the pass always
// covers every live grant.
const grantRevocationSweepLimit = 200

// RevokeGrantsForEpochChangeTx revokes every live OAuth grant and its token
// families for userID inside qtx's transaction. The caller has already
// advanced the account's authentication epoch under the user-row lock in the
// same transaction (see auth.SessionManager.ReplaceAfterEpochChangeTx for the
// session-side equivalent); calling this in the same transaction leaves no
// live grant or token family at the old epoch, so a stale-epoch code, token,
// or silent-reuse attempt from before the change authenticates nothing. See
// docs/design/second-factor-authentication.md.
func RevokeGrantsForEpochChangeTx(ctx context.Context, qtx *store.Queries, userID uuid.UUID, now time.Time) error {
	rows, err := qtx.ListLiveOAuthGrantsForUser(ctx, store.ListLiveOAuthGrantsForUserParams{
		UserID: userID, LimitRows: grantRevocationSweepLimit,
	})
	if err != nil {
		return fmt.Errorf("oauth: revoke grants for epoch change: list live grants: %w", err)
	}
	for _, row := range rows {
		if _, err = qtx.GetOAuthGrantForUpdate(ctx, row.ID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue // already revoked by a concurrent commit
			}
			return fmt.Errorf("oauth: revoke grants for epoch change: lock grant: %w", err)
		}
		if _, err = qtx.RevokeOAuthGrantForUser(ctx, store.RevokeOAuthGrantForUserParams{
			ID: row.ID, UserID: userID, RevokedAt: now,
		}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("oauth: revoke grants for epoch change: revoke grant: %w", err)
		}
		if _, err = qtx.RevokeOAuthTokensForGrant(ctx, store.RevokeOAuthTokensForGrantParams{
			GrantID: row.ID, RevokedAt: now,
		}); err != nil {
			return fmt.Errorf("oauth: revoke grants for epoch change: revoke tokens: %w", err)
		}
	}
	return nil
}

// HandleRevoke uses token possession alone to revoke its grant. Claimed client
// IDs supply no authority; see docs/design/mcp-cimd-authorization.md#revocation.
func (s *Service) HandleRevoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeOAuthError(w, http.StatusMethodNotAllowed)
		return
	}
	if values := r.Header.Values("Content-Type"); len(values) != 1 || values[0] != "application/x-www-form-urlencoded" {
		writeOAuthError(w, http.StatusUnsupportedMediaType)
		return
	}
	form, err := decodeOAuthForm(r, map[string]bool{"token": true, "token_type_hint": true, "client_id": true})
	if err != nil || !exactRevokeKeys(form) {
		writeOAuthError(w, http.StatusBadRequest)
		return
	}
	if _, supplied := form["client_id"]; supplied && !s.validRevokeClientID(form.Get("client_id")) {
		writeOAuthError(w, http.StatusBadRequest)
		return
	}
	if err := s.revokeToken(r.Context(), form.Get("token"), form.Get("client_id")); err != nil {
		writeOAuthServerError(w)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func exactRevokeKeys(form url.Values) bool {
	if len(form) < 1 || len(form) > 3 || form.Get("token") == "" {
		return false
	}
	if _, supplied := form["client_id"]; supplied && form.Get("client_id") == "" {
		return false
	}
	// Every hint value is advisory, including an empty or unsupported value.
	return true
}

func (s *Service) validRevokeClientID(raw string) bool {
	if !strings.HasPrefix(raw, "https://") {
		_, err := uuid.Parse(raw)
		return err == nil
	}
	origin, err := url.Parse(s.publicOrigin)
	if err != nil || origin.Hostname() == "" {
		return false
	}
	_, err = parseCIMDURL(raw, origin.Hostname())
	return err == nil
}

func (s *Service) revokeToken(ctx context.Context, raw, claimedClientID string) (err error) {
	_, digest, err := ParseToken(raw)
	if err != nil {
		return nil
	}
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
	client, err := q.GetOAuthClientForUpdate(ctx, authority.OAuthToken.ClientID)
	if err != nil {
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
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if claimedClientID != "" && !publicClientMatches(client, claimedClientID) {
		slog.InfoContext(ctx, "client_id_mismatch",
			"client_id", client.ID.String(), "grant_id", authority.OAuthToken.GrantID.String(), "token_id", authority.OAuthToken.ID.String())
	}
	return nil
}
