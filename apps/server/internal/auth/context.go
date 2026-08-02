package auth

import (
	"context"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// sessionCtxKey is a private type for sessionContextKey and
// predecessorSessionContextKey, so neither can ever collide with a
// context key any other package defines (revive's context-keys-type
// convention; see api.contextKey in internal/api/middleware.go for the
// same pattern).
type sessionCtxKey int

// sessionContextKey is the context key RequireCSRF's session lookup uses.
// Task 9's RequireSession middleware populates it (via
// ContextWithSession) once it has authenticated the request's session
// cookie; RequireCSRF (csrf.go) then reads the same session back (via
// SessionFromContext) to compare the request's CSRF token against the
// session's csrf_secret. Defined here, rather than in Task 9's own file,
// because csrf.go depends on it and Task 9 has not landed yet -- this is
// the seam Task 9's RequireSession is expected to populate.
//
// predecessorSessionContextKey (fix round 1, finding I2) is a distinct
// key from sessionContextKey -- iota gives it a different int value
// within this same const block -- so the two never collide despite
// sharing the same key type.
const (
	sessionContextKey sessionCtxKey = iota
	predecessorSessionContextKey
)

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

// ContextWithPredecessorSessionID returns a copy of ctx carrying
// predecessorID, retrievable via PredecessorSessionIDFromContext (fix
// round 1, finding I2 / design owner ruling DD-C14).
//
// RequireSession populates this ONLY for the one request whose own
// Authenticate call triggered a >24h rotation (task-7-brief.md,
// AC-AUTH-004): predecessorID is the rotated-away predecessor's own
// session id, distinct from the governing (successor) session
// ContextWithSession stores for the same request. A handler that revokes
// "the current session" (POST /auth/logout; DELETE /sessions/{id} when
// the target is the caller's own current session) reads this back and
// revokes BOTH rows -- otherwise the predecessor's own raw token would
// keep authenticating for up to rotationGrace (60s) after the caller
// believed they had logged out, since logout only knew about the
// successor it was itself authenticated with. DD-C11's `DELETE /sessions`
// (logout-everywhere) needs no equivalent: it already sweeps every one of
// the caller's sessions, predecessor included.
//
// This is a fast path only, not the sole mechanism: it can never fire for
// either consuming endpoint in practice, because both sit behind
// RequireCSRF, which validates against the POST-rotation (successor)
// session -- a client cannot possibly hold a correct CSRF token for a
// secret that doesn't exist until this exact request mints it, so a
// same-request "rotate and logout" always 403s on CSRF before the handler
// ever runs. sessions_handlers.go's revokeCurrentPredecessorIfAny falls
// back to a database lookup (FindImmediatePredecessorSession) for the
// realistic case: an EARLIER, unrelated request (typically GET /me, which
// is exempt from CSRF) performed the rotation, and the caller's actual
// logout/revoke-current request -- correctly authenticated against the
// successor -- never rotates again itself. Kept here anyway as a correct,
// cheap fast path for the narrow case it does cover, and because it is
// task-9-brief.md's fix round 1's own literal description of the seam.
func ContextWithPredecessorSessionID(ctx context.Context, predecessorID uuid.UUID) context.Context {
	return context.WithValue(ctx, predecessorSessionContextKey, predecessorID)
}

// PredecessorSessionIDFromContext returns the predecessor session id
// stored by ContextWithPredecessorSessionID, and true. It returns
// uuid.Nil and false if ctx carries none -- the overwhelmingly common
// case: a request whose Authenticate call did not trigger a rotation.
func PredecessorSessionIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(predecessorSessionContextKey).(uuid.UUID)
	return id, ok
}
