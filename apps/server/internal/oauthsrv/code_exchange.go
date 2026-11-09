package oauthsrv

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func (s *Service) exchangeAuthorizationCode(ctx context.Context, form url.Values) (response tokenResponse, err error) {
	return s.processAuthorizationCode(ctx, form, true)
}

// A refused grant uses the issuance checks under the same locks, then rolls
// back without any credential write. See docs/design/mcp-cimd-authorization.md.
func (s *Service) processAuthorizationCode(ctx context.Context, form url.Values, issue bool) (response tokenResponse, err error) {
	digest, err := ParseCode(form.Get("code"))
	if err != nil {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	// A code belongs to its client and is deleted with it, so the client is
	// looked up first: a swept client must get invalid_client, not the
	// invalid_grant of a missing code. The row is locked again below.
	client, err := s.storedPublicClient(ctx, s.queries, form.Get("client_id"))
	if err != nil {
		return tokenResponse{}, err
	}
	clientID := client.ID
	// This unlocked lookup only discovers the lock-order identities. The row is
	// loaded again under lock before any validation or mutation.
	preCode, err := s.queries.GetOAuthAuthorizationCodeByDigest(ctx, digest[:])
	if err != nil {
		return tokenResponse{}, tokenStateError(err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return tokenResponse{}, err
	}
	defer func() {
		if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) && (err == nil || !issue) {
			response = tokenResponse{}
			err = rollbackErr
		}
	}()
	q := store.New(tx)
	lockedClient, err := q.GetOAuthClientForUpdate(ctx, clientID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return tokenResponse{}, errOAuthUnknownClient
		}
		return tokenResponse{}, err
	}
	if !publicClientMatches(lockedClient, form.Get("client_id")) {
		return tokenResponse{}, errOAuthUnknownClient
	}
	user, err := q.GetUserForUpdate(ctx, preCode.UserID)
	if err != nil {
		return tokenResponse{}, tokenStateError(err)
	}
	grant, err := q.GetLiveOAuthGrant(ctx, store.GetLiveOAuthGrantParams{UserID: preCode.UserID, ClientID: preCode.ClientID})
	if err != nil {
		return tokenResponse{}, tokenStateError(err)
	}
	grant, err = q.GetOAuthGrantForUpdate(ctx, grant.ID)
	if err != nil {
		return tokenResponse{}, tokenStateError(err)
	}
	// A grant revoked between the unlocked read above and this lock (by a
	// concurrent RFC 7009 revoke or an authentication-epoch change) must
	// still close the exchange rather than mint authority from a dead row.
	if grant.RevokedAt != nil {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	code, err := q.GetOAuthAuthorizationCodeByDigestForUpdate(ctx, digest[:])
	if err != nil {
		return tokenResponse{}, tokenStateError(err)
	}
	now := s.clock()
	if code.ClientID != clientID || code.RedirectURI != form.Get("redirect_uri") || !VerifyS256(code.CodeChallenge, form.Get("code_verifier")) {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	if code.ConsumedAt != nil {
		if !issue {
			return tokenResponse{}, errOAuthInvalidGrant
		}
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
	if code.UserID != user.ID || code.GrantID != grant.ID || grant.UserID != user.ID || grant.ClientID != clientID ||
		code.AuthEpoch != user.AuthEpoch || grant.AuthEpoch != user.AuthEpoch || code.Scopes != grant.Scopes || code.ExpiresAt.Compare(now) <= 0 {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	if !issue {
		return tokenResponse{}, nil
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
