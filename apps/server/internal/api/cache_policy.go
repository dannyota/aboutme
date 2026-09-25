package api

import "net/http"

// CacheControlNoStore is the Cache-Control value NoStoreCache sends. Every
// authenticated API response uses it so no intermediary (a browser cache,
// a CDN, a corporate proxy) ever stores a copy that could later be served
// to a different account sharing that cache.
const CacheControlNoStore = "no-store, no-transform"

// NoStoreCache returns middleware that sets Cache-Control: no-store,
// no-transform on every response it wraps, including error responses. It
// sets the header before calling next, the same pattern SecurityHeaders
// uses, so a rejection path carries the header as well as a success path.
//
// It serves two roles (see router.go): the cache policy for the health
// endpoints, and the outermost default on the non-health chain so that a
// rejection (404/405/413/429/400) never escapes without a cache policy.
// That default does not defeat a route's own policy: a handler inside the
// mux, such as the public API with no-cache, must-revalidate, sets
// Cache-Control after this middleware did, and the later Set wins.
// TestCachePolicy_InnerGroupOverridesOuterDefault pins this precedence.
func NoStoreCache() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", CacheControlNoStore)
			next.ServeHTTP(w, r)
		})
	}
}
