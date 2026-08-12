package auth

import (
	"crypto/subtle"
	"encoding/base64"
	"mime"
	"net/http"
	"net/url"

	"github.com/dannyota/aboutme/apps/server/internal/api"
)

// CSRFHeaderName is the header carrying a mutating request's CSRF token:
// base64.RawURLEncoding of the authenticated session's csrf_secret.
const CSRFHeaderName = "X-CSRF-Token"

// csrfRejectedCode collapses every CSRF failure into one no-oracle response.
const csrfRejectedCode = "csrf_rejected"

const csrfRejectedMessage = "CSRF validation failed"

// RequireCSRF runs after session authentication. Mutations require exact origin
// evidence and the synchronizer token; bodiless requests alone skip the JSON
// content-type check. See docs/design/security.md.
func RequireCSRF(allowedOrigin string) api.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isMutatingMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}

			// Reject cheap boundary failures before session and token work.
			if !originAllowed(r, allowedOrigin) {
				rejectCSRF(w)
				return
			}

			if hasBody(r) && !contentTypeAllowed(r.Header.Get("Content-Type")) {
				rejectCSRF(w)
				return
			}

			sess, ok := SessionFromContext(r.Context())
			if !ok {
				rejectCSRF(w)
				return
			}

			if !validCSRFToken(r.Header.Get(CSRFHeaderName), sess.CSRFSecret) {
				rejectCSRF(w)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// rejectCSRF preserves one response across all failure classes.
func rejectCSRF(w http.ResponseWriter) {
	api.WriteError(w, http.StatusForbidden, csrfRejectedCode, csrfRejectedMessage)
}

// isMutatingMethod reports whether method needs a CSRF check: everything
// except GET, HEAD, and OPTIONS.
func isMutatingMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

// originAllowed requires exact Origin, with exact Referer origin fallback. A
// missing or malformed value fails closed. See docs/design/security.md.
func originAllowed(r *http.Request, allowedOrigin string) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		return origin == allowedOrigin
	}

	referer := r.Header.Get("Referer")
	if referer == "" {
		return false
	}
	u, err := url.Parse(referer)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	return u.Scheme+"://"+u.Host == allowedOrigin
}

// hasBody treats an unknown Content-Length and any transfer encoding as a body.
// This prevents chunked requests from bypassing the JSON content-type check.
func hasBody(r *http.Request) bool {
	return r.ContentLength != 0 || len(r.TransferEncoding) > 0
}

// contentTypeAllowed accepts application/json with only an optional charset.
func contentTypeAllowed(ct string) bool {
	if ct == "" {
		return false
	}
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil || mediaType != "application/json" {
		return false
	}
	delete(params, "charset")
	return len(params) == 0
}

// validCSRFToken decodes base64url before constant-time comparison of the raw
// secret bytes.
func validCSRFToken(headerToken string, secret []byte) bool {
	if headerToken == "" || len(secret) == 0 {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(headerToken)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(decoded, secret) == 1
}
