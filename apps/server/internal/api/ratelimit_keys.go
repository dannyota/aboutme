package api

import (
	"context"
	"net/http"
	"strings"
)

// KeyFunc derives the rate-limit key for a request, and whether one could
// be derived at all. RateLimiterConfig.Key defaults to IPKeyFunc; see
// IPKeyFunc, AccountKeyFunc, and CompositeKeyFunc for the building blocks a
// stricter per-route policy composes from. Every KeyFunc here returns a
// bounded value derived from a parsed IP address or opaque account ID,
// never an unbounded attacker-controlled input.
//
// The second return is false when no key could be safely derived (e.g.
// IPKeyFunc when resolveClientIP fails); RateLimit rejects the request
// outright in that case rather than treating "" as a valid, sharable key.
type KeyFunc func(r *http.Request, trusted TrustedProxies) (string, bool)

// IPKeyFunc keys solely on the request's client IP (see resolveClientIP).
// It is the default RateLimiterConfig.Key.
func IPKeyFunc(r *http.Request, trusted TrustedProxies) (string, bool) {
	addr, ok := resolveClientIP(r, trusted)
	if !ok {
		return "", false
	}
	return "ip:" + addr.String(), true
}

// AccountKeyFunc keys solely on the authenticated account ID that auth
// middleware stores in the request context via
// WithAccountID. An unauthenticated request has no account in context and
// keys on the empty account ID, so every anonymous caller would collide on
// one bucket — AccountKeyFunc must not be used alone on a route reachable
// without authentication; compose it with IPKeyFunc via CompositeKeyFunc,
// or restrict it to routes auth middleware already gates. It always
// succeeds (never rejects a request) since an absent account ID is a valid,
// deliberate anonymous key, not a malformed input.
func AccountKeyFunc(r *http.Request, _ TrustedProxies) (string, bool) {
	id, _ := AccountIDFromContext(r.Context())
	return "acct:" + id, true
}

// CompositeKeyFunc returns a KeyFunc joining every funcs' output into one
// key, so e.g. CompositeKeyFunc(AccountKeyFunc, IPKeyFunc) limits each
// (account, IP) pair independently, which neither dimension alone can
// express. A per-IP-only limit lets one account exhaust it from many IPs
// (or vice versa), and a per-account-only limit can't rate-limit
// pre-authentication attempts by
// IP at all. The composite fails (second return false) if any component
// KeyFunc does, so e.g. an unresolvable client IP still rejects the
// request rather than silently degrading to the account dimension alone.
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

// rateLimitContextKey is an unexported type for this package's context
// keys (google.github.io/styleguide/go/decisions#contexts), so a value
// stored under it can never collide with a key defined elsewhere.
type rateLimitContextKey int

const accountIDContextKey rateLimitContextKey = 0

// WithAccountID returns a copy of ctx carrying accountID for
// AccountKeyFunc (and any CompositeKeyFunc built from it) to read back via
// AccountIDFromContext. Auth middleware calls this once it
// has authenticated the request, before RateLimit's handler runs on it.
func WithAccountID(ctx context.Context, accountID string) context.Context {
	return context.WithValue(ctx, accountIDContextKey, accountID)
}

// AccountIDFromContext returns the account ID stored by WithAccountID, and
// whether one was present — false for an unauthenticated request, or one
// that reached this code outside any auth middleware (e.g. most unit
// tests).
func AccountIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(accountIDContextKey).(string)
	return id, ok
}
