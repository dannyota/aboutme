package auth

import (
	"context"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// sessionCtxKey prevents collisions with context keys from other packages.
type sessionCtxKey int

const (
	// sessionContextKey carries the session authenticated by RequireSession.
	// RequireCSRF reads the same value to validate the synchronizer token.
	sessionContextKey sessionCtxKey = iota
	// pendingAuthenticationContextKey carries pending second-factor authority,
	// which never satisfies SessionFromContext.
	pendingAuthenticationContextKey
)

// ContextWithSession returns a copy of ctx carrying sess, retrievable via
// SessionFromContext.
func ContextWithSession(ctx context.Context, sess store.Session) context.Context {
	return context.WithValue(ctx, sessionContextKey, sess)
}

// SessionFromContext returns the authenticated session, if present.
func SessionFromContext(ctx context.Context) (store.Session, bool) {
	sess, ok := ctx.Value(sessionContextKey).(store.Session)
	return sess, ok
}

// ContextWithPendingAuthentication carries authority restricted to pending
// second-factor routes.
func ContextWithPendingAuthentication(ctx context.Context, pending store.PendingAuthentication) context.Context {
	return context.WithValue(ctx, pendingAuthenticationContextKey, pending)
}

// PendingAuthenticationFromContext returns pending authority, if present.
func PendingAuthenticationFromContext(ctx context.Context) (store.PendingAuthentication, bool) {
	pending, ok := ctx.Value(pendingAuthenticationContextKey).(store.PendingAuthentication)
	return pending, ok
}
