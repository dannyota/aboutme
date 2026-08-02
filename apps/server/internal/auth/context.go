package auth

import (
	"context"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// sessionCtxKey is a private type for sessionContextKey, so it can never
// collide with a context key any other package defines (revive's
// context-keys-type convention; see api.contextKey in
// internal/api/middleware.go for the same pattern).
type sessionCtxKey int

// sessionContextKey is the context key RequireCSRF's session lookup uses.
// Task 9's RequireSession middleware populates it (via
// ContextWithSession) once it has authenticated the request's session
// cookie; RequireCSRF (csrf.go) then reads the same session back (via
// SessionFromContext) to compare the request's CSRF token against the
// session's csrf_secret. Defined here, rather than in Task 9's own file,
// because csrf.go depends on it and Task 9 has not landed yet -- this is
// the seam Task 9's RequireSession is expected to populate.
const sessionContextKey sessionCtxKey = iota

// ContextWithSession returns a copy of ctx carrying sess, retrievable via
// SessionFromContext.
func ContextWithSession(ctx context.Context, sess store.Session) context.Context {
	return context.WithValue(ctx, sessionContextKey, sess)
}

// SessionFromContext returns the session stored by ContextWithSession, and
// true. It returns the zero store.Session and false if ctx carries none --
// e.g. a request that has not (yet, or ever) passed through session
// authentication.
func SessionFromContext(ctx context.Context) (store.Session, bool) {
	sess, ok := ctx.Value(sessionContextKey).(store.Session)
	return sess, ok
}
