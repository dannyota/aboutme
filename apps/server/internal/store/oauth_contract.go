package store

import (
	"context"

	"github.com/google/uuid"
)

// OAuthQueries is the exact data-layer surface Phase PM's OAuth authorization
// server and MCP resource server (T04–T09) consume. It freezes the row-lock
// protocol the design requires and hides every raw-SQL detail behind one
// transactional contract; the broader Querier interface keeps the unrelated
// session, resume, and password methods off this surface.
//
// Lock order is oauth_clients → users → oauth_grants →
// oauth_authorization_codes → oauth_tokens. Three rules follow from it:
//
//   - A path that writes a grant, code, or token for a client first takes
//     GetOAuthClientForUpdate. That lock is also what makes
//     ListIdleOAuthClientCandidates skip (rather than collect) a client another
//     transaction is consenting to.
//   - The M5 live-grant cap is read under GetUserForUpdate, so two concurrent
//     approvals cannot both observe the same pre-cap count.
//   - A token-family revocation first takes GetOAuthGrantForUpdate, so a
//     rotation committing concurrently cannot resurrect a family the
//     revocation just killed.
//
// Storage is digest-only. Code and token digests are BYTEA of exactly 32
// bytes, checked by the database; no method here accepts, returns, or stores
// raw code or token material, and PKCE verifiers are never stored at all.
// Every list and cleanup method is bounded by an explicit row limit, so a
// request-path sweep costs a constant rather than a function of table size.
type OAuthQueries interface {
	// Clients (M1). The primary key is the public client_id; there is no
	// secret. Deleting a client cascades its codes, grants, and tokens.
	CreateOAuthClient(context.Context, CreateOAuthClientParams) (OAuthClient, error)
	GetOAuthClient(context.Context, uuid.UUID) (OAuthClient, error)
	GetOAuthClientForUpdate(context.Context, uuid.UUID) (OAuthClient, error)
	TouchOAuthClientLastUsed(context.Context, TouchOAuthClientLastUsedParams) (int64, error)
	DeleteOAuthClient(context.Context, uuid.UUID) (int64, error)
	ListIdleOAuthClientCandidates(context.Context, ListIdleOAuthClientCandidatesParams) ([]uuid.UUID, error)
	DeleteOAuthClients(context.Context, []uuid.UUID) (int64, error)

	// Authorization codes (M2). Consumption is a single conditional UPDATE, so
	// two concurrent exchanges of one code yield exactly one success and one
	// pgx.ErrNoRows; the locked lookup serves the consumed-code replay path.
	CreateOAuthAuthorizationCode(context.Context, CreateOAuthAuthorizationCodeParams) (OAuthAuthorizationCode, error)
	GetOAuthAuthorizationCodeByDigest(context.Context, []byte) (OAuthAuthorizationCode, error)
	GetOAuthAuthorizationCodeByDigestForUpdate(context.Context, []byte) (OAuthAuthorizationCode, error)
	ConsumeOAuthAuthorizationCode(context.Context, ConsumeOAuthAuthorizationCodeParams) (OAuthAuthorizationCode, error)
	DeleteExpiredOAuthAuthorizationCodes(context.Context, DeleteExpiredOAuthAuthorizationCodesParams) (int64, error)

	// Grants (M5, M8). GetUserForUpdate is the cap serialization point; the
	// partial unique index behind UpsertOAuthGrant is what makes a second live
	// grant per (user, client) impossible.
	GetUserForUpdate(context.Context, uuid.UUID) (User, error)
	UpsertOAuthGrant(context.Context, UpsertOAuthGrantParams) (OAuthGrant, error)
	GetLiveOAuthGrant(context.Context, GetLiveOAuthGrantParams) (OAuthGrant, error)
	GetOAuthGrantForUpdate(context.Context, uuid.UUID) (OAuthGrant, error)
	CountLiveOAuthGrantsForUser(context.Context, uuid.UUID) (int64, error)
	ListLiveOAuthGrantsForUser(context.Context, ListLiveOAuthGrantsForUserParams) ([]ListLiveOAuthGrantsForUserRow, error)
	RevokeOAuthGrant(context.Context, RevokeOAuthGrantParams) (int64, error)
	RevokeOAuthGrantForUser(context.Context, RevokeOAuthGrantForUserParams) (OAuthGrant, error)

	// Tokens (M3, M7). InsertRotatedOAuthToken derives family, client, user,
	// grant, and family expiry from the predecessor row, so a rotated token
	// cannot cross into another family; SupersedeOAuthToken additionally
	// requires the caller's family to match the row it marks.
	CreateOAuthToken(context.Context, CreateOAuthTokenParams) (OAuthToken, error)
	InsertRotatedOAuthToken(context.Context, InsertRotatedOAuthTokenParams) (OAuthToken, error)
	GetOAuthTokenAuthorityByDigest(context.Context, []byte) (GetOAuthTokenAuthorityByDigestRow, error)
	SupersedeOAuthToken(context.Context, SupersedeOAuthTokenParams) (OAuthToken, error)
	RevokeOAuthTokenFamily(context.Context, RevokeOAuthTokenFamilyParams) (int64, error)
	RevokeOAuthTokensForGrant(context.Context, RevokeOAuthTokensForGrantParams) (int64, error)
	TouchOAuthTokenLastUsed(context.Context, TouchOAuthTokenLastUsedParams) (int64, error)
	DeleteTerminalOAuthTokens(context.Context, DeleteTerminalOAuthTokensParams) (int64, error)
}

// The generated *Queries must satisfy the agent-access surface exactly; a
// missing or renamed query fails the build rather than silently dropping a
// method the OAuth server depends on.
var _ OAuthQueries = (*Queries)(nil)
