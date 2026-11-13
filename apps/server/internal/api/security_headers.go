package api

import "net/http"

const (
	ContentSecurityPolicy        = "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
	ReferrerPolicy               = "no-referrer"
	XContentTypeOptions          = "nosniff"
	StrictTransportSecurityValue = "max-age=63072000; includeSubDomains"
)

func SecurityHeaders(trusted TrustedProxies) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", ContentSecurityPolicy)
			h.Set("X-Content-Type-Options", XContentTypeOptions)
			h.Set("Referrer-Policy", ReferrerPolicy)
			// Browsers ignore HSTS received over plain HTTP (RFC 6797 section 8.1).
			if requestIsHTTPS(r, trusted) {
				h.Set("Strict-Transport-Security", StrictTransportSecurityValue)
			}
			next.ServeHTTP(w, r)
		})
	}
}
