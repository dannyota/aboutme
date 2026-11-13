package api

import "net/http"

const CacheControlNoStore = "no-store, no-transform"

func NoStoreCache() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", CacheControlNoStore)
			next.ServeHTTP(w, r)
		})
	}
}
