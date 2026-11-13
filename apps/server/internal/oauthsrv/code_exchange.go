package oauthsrv

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/store"
)

func (s *Service) exchangeAuthorizationCode(ctx context.Context, form url.Values) (response tokenResponse, err error) {
	return s.processAuthorizationCode(ctx, form, true)
}

func (s *Service) processAuthorizationCode(ctx context.Context, form url.Values, issue bool) (response tokenResponse, err error) {
	digest, err := ParseCode(form.Get("code"))
	if err != nil {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	client, err := s.storedPublicClient(ctx, s.queries, form.Get("client_id"))
	if err != nil {
		return tokenResponse{}, err
	}
	clientID := client.ID
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
	pair, err := s.newTokenPair()
	if err != nil {
		return tokenResponse{}, err
	}
	familyExpiry := now.Add(refreshFamilyTTL)
	for _, token := range []struct {
		digest  [32]byte
		kind    TokenKind
		expires time.Time
	}{{pair.accessDigest, TokenKindAccess, now.Add(accessTokenTTL)}, {pair.refreshDigest, TokenKindRefresh, familyExpiry}} {
		if _, err = q.CreateOAuthToken(ctx, store.CreateOAuthTokenParams{TokenDigest: token.digest[:], Kind: string(token.kind), FamilyID: familyID, ClientID: code.ClientID, UserID: code.UserID, GrantID: grant.ID, CreatedAt: now, ExpiresAt: token.expires, FamilyExpiresAt: familyExpiry}); err != nil {
			return tokenResponse{}, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return tokenResponse{}, err
	}
	return tokenResponse{AccessToken: pair.access, TokenType: "Bearer", ExpiresIn: int64(accessTokenTTL / time.Second), RefreshToken: pair.refresh, Scope: code.Scopes}, nil
}
