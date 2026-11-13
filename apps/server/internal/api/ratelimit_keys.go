package api

import (
	"context"
	"net/http"
	"strings"
)

type KeyFunc func(r *http.Request, trusted TrustedProxies) (string, bool)

func IPKeyFunc(r *http.Request, trusted TrustedProxies) (string, bool) {
	addr, ok := resolveClientIP(r, trusted)
	if !ok {
		return "", false
	}
	return "ip:" + addr.String(), true
}

func AccountKeyFunc(r *http.Request, _ TrustedProxies) (string, bool) {
	id, _ := AccountIDFromContext(r.Context())
	return "acct:" + id, true
}

func CompositeKeyFunc(funcs ...KeyFunc) KeyFunc {
	return func(r *http.Request, trusted TrustedProxies) (string, bool) {
		parts := make([]string, len(funcs))
		for i, f := range funcs {
			part, ok := f(r, trusted)
			if !ok {
				return "", false
			}
			parts[i] = part
		}
		return strings.Join(parts, "|"), true
	}
}

type rateLimitContextKey int

const accountIDContextKey rateLimitContextKey = 0

func WithAccountID(ctx context.Context, accountID string) context.Context {
	return context.WithValue(ctx, accountIDContextKey, accountID)
}

func AccountIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(accountIDContextKey).(string)
	return id, ok
}
