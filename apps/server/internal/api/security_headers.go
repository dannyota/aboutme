package api

import "net/http"

// Security header values SecurityHeaders sends, exported so callers and
// tests can assert against the exact strings without duplicating them.
const (
	// ContentSecurityPolicy is the Content-Security-Policy value
	// SecurityHeaders sends. This server never serves HTML; JSON, media, PDFs,
	// and event streams do not need browser subresources. The Nuxt app owns the
	// policy for its server-rendered HTML, so this middleware disables every
	// fetch, script, style, frame, and form source.
	ContentSecurityPolicy = "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"
	// ReferrerPolicy is the Referrer-Policy value SecurityHeaders sends: it
	// withholds the Referer header on any request a client's browser makes
	// after seeing one of this API's responses.
	ReferrerPolicy = "no-referrer"
	// XContentTypeOptions is the X-Content-Type-Options value
	// SecurityHeaders sends: it stops browsers from MIME-sniffing a
	// response into a more dangerous type than the Content-Type this
	// server declared.
	XContentTypeOptions = "nosniff"
	// StrictTransportSecurityValue is the Strict-Transport-Security value
	// SecurityHeaders sends when requestIsHTTPS reports true: two years,
	// including subdomains. It omits "preload" because enrollment in
	// browsers' HSTS preload list is separate and hard to reverse.
	StrictTransportSecurityValue = "max-age=63072000; includeSubDomains"
)

// SecurityHeaders returns middleware that sets baseline security response
// headers on every response it wraps — including
// error responses written by later middleware (RateLimit's 429,
// BodyLimit's 413) and the router's own 404/405 — because it sets headers
// on the ResponseWriter before calling next, and headers set before the
// first WriteHeader/Write are honored no matter which handler downstream
// ends up writing the response.
//
// trusted controls whether proxy scheme assertions affect HSTS, using the
// same trust boundary RateLimit uses for client IPs. See TrustedProxies.
func SecurityHeaders(trusted TrustedProxies) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", ContentSecurityPolicy)
			h.Set("X-Content-Type-Options", XContentTypeOptions)
			h.Set("Referrer-Policy", ReferrerPolicy)
			// HSTS is only meaningful — and only safe — on an HTTPS
			// response: browsers ignore it over plain HTTP, and sending it
			// on http://localhost during local dev would force HTTPS
			// there too, breaking the dev loop for whoever hits it next.
			if requestIsHTTPS(r, trusted) {
				h.Set("Strict-Transport-Security", StrictTransportSecurityValue)
			}
			next.ServeHTTP(w, r)
		})
	}
}
