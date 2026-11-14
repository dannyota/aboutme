package mcpapi

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/oauthsrv"
	"danny.vn/aboutme/apps/server/internal/store"
)

const lastUsedWindow = time.Minute

type Principal struct {
	UserID  uuid.UUID
	GrantID uuid.UUID
	TokenID uuid.UUID
	Scopes  oauthsrv.Scopes
}

type BearerDependencies struct {
	Queries      store.OAuthQueries
	Clock        func() time.Time
	PublicOrigin string
}

type Bearer struct {
	queries      store.OAuthQueries
	clock        func() time.Time
	publicOrigin string
}

func NewBearer(dependencies BearerDependencies) (*Bearer, error) {
	if isNil(dependencies.Queries) || dependencies.Clock == nil || !oauthsrv.IsCanonicalOrigin(dependencies.PublicOrigin) {
		return nil, errors.New("mcp bearer: invalid dependencies")
	}
	return &Bearer{queries: dependencies.Queries, clock: dependencies.Clock, publicOrigin: dependencies.PublicOrigin}, nil
}

func (b *Bearer) Authenticate(r *http.Request) (Principal, error) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return Principal{}, b.unauthorized()
	}
	raw := strings.TrimPrefix(values[0], "Bearer ")
	kind, digest, err := oauthsrv.ParseToken(raw)
	if err != nil || kind != oauthsrv.TokenKindAccess {
		return Principal{}, b.unauthorized()
	}
	authority, err := b.queries.GetOAuthTokenAuthorityByDigest(r.Context(), digest[:])
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, b.unauthorized()
	}
	if err != nil {
		return Principal{}, errInternal
	}
	now := b.clock()
	token := authority.OAuthToken
	grant := authority.OAuthGrant
	if token.Kind != string(oauthsrv.TokenKindAccess) || token.RevokedAt != nil || token.SupersededAt != nil || grant.RevokedAt != nil ||
		!token.ExpiresAt.After(now) || !token.FamilyExpiresAt.After(now) ||
		token.UserID != authority.User.ID || token.UserID != grant.UserID || token.ClientID != grant.ClientID || token.GrantID != grant.ID ||
		grant.AuthEpoch != authority.User.AuthEpoch {
		return Principal{}, b.unauthorized()
	}
	scopes, err := oauthsrv.ParseStoredScopes(grant.Scopes)
	if err != nil {
		return Principal{}, b.unauthorized()
	}
	if _, err := b.queries.TouchOAuthTokenLastUsed(r.Context(), store.TouchOAuthTokenLastUsedParams{
		Now: now, ID: token.ID, TouchBefore: now.Add(-lastUsedWindow),
	}); err != nil {
		return Principal{}, errInternal
	}
	return Principal{UserID: token.UserID, GrantID: grant.ID, TokenID: token.ID, Scopes: scopes}, nil
}

func (b *Bearer) unauthorized() error {
	return &mcpError{status: http.StatusUnauthorized, code: "unauthorized", challenge: b.publicOrigin}
}

func RequireScope(principal Principal, scope oauthsrv.Scope) error {
	if !principal.Scopes.Has(scope) {
		return errScopeDenied
	}
	return nil
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
