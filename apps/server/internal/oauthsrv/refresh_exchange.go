package oauthsrv

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/store"
)

func (s *Service) rotateRefreshToken(ctx context.Context, raw string, claimedClientIDs []string) (response tokenResponse, err error) {
	return s.processRefreshToken(ctx, raw, claimedClientIDs, true)
}

func (s *Service) processRefreshToken(ctx context.Context, raw string, claimedClientIDs []string, issue bool) (response tokenResponse, err error) {
	kind, digest, err := ParseToken(raw)
	if err != nil || kind != TokenKindRefresh {
		return tokenResponse{}, s.missingRefreshTokenError(ctx, s.queries, claimedClientIDs)
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
	authority, err := q.GetOAuthTokenAuthorityByDigest(ctx, digest[:])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return tokenResponse{}, s.missingRefreshTokenError(ctx, q, claimedClientIDs)
		}
		return tokenResponse{}, err
	}
	client, err := q.GetOAuthClientForUpdate(ctx, authority.OAuthToken.ClientID)
	if err != nil {
		return tokenResponse{}, tokenStateError(err)
	}
	user, err := q.GetUserForUpdate(ctx, authority.OAuthToken.UserID)
	if err != nil {
		return tokenResponse{}, tokenStateError(err)
	}
	grant, err := q.GetOAuthGrantForUpdate(ctx, authority.OAuthToken.GrantID)
	if err != nil {
		return tokenResponse{}, tokenStateError(err)
	}
	authority, err = q.GetOAuthTokenAuthorityByDigest(ctx, digest[:])
	if err != nil {
		return tokenResponse{}, tokenStateError(err)
	}
	now := s.clock()
	token := authority.OAuthToken
	if len(claimedClientIDs) == 1 && !publicClientMatches(client, claimedClientIDs[0]) {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	if token.Kind != string(TokenKindRefresh) || grant.RevokedAt != nil || token.RevokedAt != nil || token.ExpiresAt.Compare(now) <= 0 || token.FamilyExpiresAt.Compare(now) <= 0 ||
		token.ClientID != client.ID || token.UserID != user.ID || token.GrantID != grant.ID || grant.UserID != user.ID || grant.ClientID != client.ID || grant.AuthEpoch != user.AuthEpoch {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	if token.SupersededAt != nil {
		if !issue {
			return tokenResponse{}, errOAuthInvalidGrant
		}
		if _, err = q.RevokeOAuthTokenFamily(ctx, store.RevokeOAuthTokenFamilyParams{FamilyID: token.FamilyID, RevokedAt: now}); err != nil {
			return tokenResponse{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return tokenResponse{}, err
		}
		return tokenResponse{}, errOAuthInvalidGrant
	}
	if !issue {
		return tokenResponse{}, nil
	}
	scopes, err := ParseStoredScopes(grant.Scopes)
	if err != nil {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	pair, err := s.newTokenPair()
	if err != nil {
		return tokenResponse{}, err
	}
	if _, err = q.SupersedeOAuthToken(ctx, store.SupersedeOAuthTokenParams{ID: token.ID, FamilyID: token.FamilyID, SupersededAt: now}); err != nil {
		return tokenResponse{}, errOAuthInvalidGrant
	}
	if _, err = q.InsertRotatedOAuthToken(ctx, store.InsertRotatedOAuthTokenParams{TokenDigest: pair.refreshDigest[:], Kind: string(TokenKindRefresh), CreatedAt: now, ExpiresAt: token.FamilyExpiresAt, RotatedFrom: token.ID}); err != nil {
		return tokenResponse{}, err
	}
	accessExpiresAt := now.Add(accessTokenTTL)
	if token.FamilyExpiresAt.Before(accessExpiresAt) {
		accessExpiresAt = token.FamilyExpiresAt
	}
	if _, err = q.CreateOAuthToken(ctx, store.CreateOAuthTokenParams{TokenDigest: pair.accessDigest[:], Kind: string(TokenKindAccess), FamilyID: token.FamilyID, ClientID: token.ClientID, UserID: token.UserID, GrantID: token.GrantID, CreatedAt: now, ExpiresAt: accessExpiresAt, FamilyExpiresAt: token.FamilyExpiresAt}); err != nil {
		return tokenResponse{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return tokenResponse{}, err
	}
	return tokenResponse{AccessToken: pair.access, TokenType: "Bearer", ExpiresIn: int64(accessExpiresAt.Sub(now) / time.Second), RefreshToken: pair.refresh, Scope: scopes.String()}, nil
}

func tokenStateError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return errOAuthInvalidGrant
	}
	return err
}

func publicClientMatches(client store.OAuthClient, raw string) bool {
	if client.MetadataUrl != nil {
		return raw == *client.MetadataUrl
	}
	id, err := uuid.Parse(raw)
	return err == nil && id == client.ID
}

func (s *Service) missingRefreshTokenError(ctx context.Context, q oauthClientReader, claimedClientIDs []string) error {
	if len(claimedClientIDs) != 1 {
		return errOAuthInvalidGrant
	}
	_, err := s.storedPublicClient(ctx, q, claimedClientIDs[0])
	if errors.Is(err, errOAuthInvalidClient) {
		return errOAuthInvalidGrant
	}
	if err != nil {
		return err
	}
	return errOAuthInvalidGrant
}
