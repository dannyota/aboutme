package auth

import (
	"context"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// sessionCtxKey prevents collisions with context keys from other packages.
type sessionCtxKey int

// sessionContextKey carries the session authenticated by RequireSession.
// RequireCSRF reads the same value to validate the synchronizer token.
const sessionContextKey sessionCtxKey = iota

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
