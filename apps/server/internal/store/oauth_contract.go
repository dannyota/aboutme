package store

import (
	"context"

	"github.com/google/uuid"
)

type OAuthQueries interface {
	CreateOAuthClient(context.Context, CreateOAuthClientParams) (OAuthClient, error)
	GetDCRClient(context.Context, uuid.UUID) (OAuthClient, error)
	GetOAuthClientByMetadataURL(context.Context, string) (OAuthClient, error)
	UpsertOAuthMetadataClient(context.Context, UpsertOAuthMetadataClientParams) (OAuthClient, error)
	GetOAuthClient(context.Context, uuid.UUID) (OAuthClient, error)
	GetOAuthClientForUpdate(context.Context, uuid.UUID) (OAuthClient, error)
	TouchOAuthClientLastUsed(context.Context, TouchOAuthClientLastUsedParams) (int64, error)
	DeleteOAuthClient(context.Context, uuid.UUID) (int64, error)
	ListIdleOAuthClientCandidates(context.Context, ListIdleOAuthClientCandidatesParams) ([]uuid.UUID, error)
	DeleteOAuthClients(context.Context, []uuid.UUID) (int64, error)

	CreateOAuthAuthorizationCode(context.Context, CreateOAuthAuthorizationCodeParams) (OAuthAuthorizationCode, error)
	GetOAuthAuthorizationCodeByDigest(context.Context, []byte) (OAuthAuthorizationCode, error)
	GetOAuthAuthorizationCodeByDigestForUpdate(context.Context, []byte) (OAuthAuthorizationCode, error)
	ConsumeOAuthAuthorizationCode(context.Context, ConsumeOAuthAuthorizationCodeParams) (OAuthAuthorizationCode, error)
	DeleteExpiredOAuthAuthorizationCodes(context.Context, DeleteExpiredOAuthAuthorizationCodesParams) (int64, error)

	GetUserForUpdate(context.Context, uuid.UUID) (User, error)
	UpsertOAuthGrant(context.Context, UpsertOAuthGrantParams) (OAuthGrant, error)
	GetLiveOAuthGrant(context.Context, GetLiveOAuthGrantParams) (OAuthGrant, error)
	GetOAuthGrantForUpdate(context.Context, uuid.UUID) (OAuthGrant, error)
	CountLiveOAuthGrantsForUser(context.Context, uuid.UUID) (int64, error)
	ListLiveOAuthGrantsForUser(context.Context, ListLiveOAuthGrantsForUserParams) ([]ListLiveOAuthGrantsForUserRow, error)
	RevokeOAuthGrant(context.Context, RevokeOAuthGrantParams) (int64, error)
	RevokeOAuthGrantForUser(context.Context, RevokeOAuthGrantForUserParams) (OAuthGrant, error)

	CreateOAuthToken(context.Context, CreateOAuthTokenParams) (OAuthToken, error)
	InsertRotatedOAuthToken(context.Context, InsertRotatedOAuthTokenParams) (OAuthToken, error)
	GetOAuthTokenAuthorityByDigest(context.Context, []byte) (GetOAuthTokenAuthorityByDigestRow, error)
	SupersedeOAuthToken(context.Context, SupersedeOAuthTokenParams) (OAuthToken, error)
	RevokeOAuthTokenFamily(context.Context, RevokeOAuthTokenFamilyParams) (int64, error)
	RevokeOAuthTokensForGrant(context.Context, RevokeOAuthTokensForGrantParams) (int64, error)
	TouchOAuthTokenLastUsed(context.Context, TouchOAuthTokenLastUsedParams) (int64, error)
	DeleteTerminalOAuthTokens(context.Context, DeleteTerminalOAuthTokensParams) (int64, error)
}

var _ OAuthQueries = (*Queries)(nil)
