package auth

import (
	"context"

	"danny.vn/aboutme/apps/server/internal/store"
)

type sessionCtxKey int

const sessionContextKey sessionCtxKey = 0

func ContextWithSession(ctx context.Context, sess store.Session) context.Context {
	return context.WithValue(ctx, sessionContextKey, sess)
}

func SessionFromContext(ctx context.Context) (store.Session, bool) {
	sess, ok := ctx.Value(sessionContextKey).(store.Session)
	return sess, ok
}
