package mcpapi

import (
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/oauthsrv"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const lastUsedWindow = time.Minute

// Principal is the validated bearer authority passed to MCP tools.
type Principal struct {
	UserID  uuid.UUID
	GrantID uuid.UUID
	TokenID uuid.UUID
	Scopes  oauthsrv.Scopes
}

// BearerDependencies are the fixed resource-server dependencies.
type BearerDependencies struct {
	Queries      store.OAuthQueries
	Clock        func() time.Time
	PublicOrigin string
}

// Bearer validates OAuth access tokens for the MCP bearer-only boundary.
type Bearer struct {
	queries      store.OAuthQueries
	clock        func() time.Time
	publicOrigin string
}

// NewBearer builds a bearer boundary from the configured canonical origin.
func NewBearer(dependencies BearerDependencies) (*Bearer, error) {
	if isNil(dependencies.Queries) || dependencies.Clock == nil || !canonicalOrigin(dependencies.PublicOrigin) {
		return nil, errors.New("mcp bearer: invalid dependencies")
	}
	return &Bearer{queries: dependencies.Queries, clock: dependencies.Clock, publicOrigin: dependencies.PublicOrigin}, nil
}

// Authenticate validates one exact Bearer access-token header. It never reads
// cookies or another browser credential.
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
		token.UserID != authority.User.ID || token.UserID != grant.UserID || token.ClientID != grant.ClientID || token.GrantID != grant.ID {
		return Principal{}, b.unauthorized()
	}
	scopes, err := oauthsrv.ParseScopes(grant.Scopes)
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

// RequireScope checks the closed grant scope set before a tool touches state.
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

func canonicalOrigin(raw string) bool {
	if raw == "" || len(raw) > 512 {
		return false
	}
	for i := range raw {
		if raw[i] < 0x21 || raw[i] > 0x7e {
			return false
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Host == "" || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return false
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	if port := u.Port(); port != "" && !(scheme == "http" && port == "80") && !(scheme == "https" && port == "443") {
		host += ":" + port
	}
	return raw == scheme+"://"+host
}
