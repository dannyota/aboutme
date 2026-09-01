package auth

import (
	"crypto/subtle"
	"encoding/base64"
	"mime"
	"net/http"
	"net/url"
	"strings"

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
	return requireCSRF(allowedOrigin, jsonContentTypeAllowed, rejectJSONMediaType, false)
}

// RequireExactJSONCSRF applies the same Origin and synchronizer-token checks
// as RequireCSRF, but mutating requests with a body accept exactly one
// application/json field value and map any other media type to the OpenAPI
// 415 response used by OAuth consent.
func RequireExactJSONCSRF(allowedOrigin string) api.Middleware {
	return requireCSRF(allowedOrigin, exactJSONContentTypeAllowed, rejectExactJSONMediaType, false)
}

// RequireCSRFMultipart applies the same origin, session, and synchronizer
// token checks as RequireCSRF, but accepts only one multipart/form-data media
// type with a non-empty boundary. The resume photo POST is its sole caller.
func RequireCSRFMultipart(allowedOrigin string) api.Middleware {
	return requireCSRF(allowedOrigin, multipartContentTypeAllowed, rejectMultipartMediaType, true)
}

func requireCSRF(allowedOrigin string, mediaTypeAllowed func(http.Header) bool,
	rejectMediaType func(http.ResponseWriter), requireMediaType bool,
) api.Middleware {
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

			sess, ok := SessionFromContext(r.Context())
			if !ok {
				rejectCSRF(w)
				return
			}

			token, tokenOK := singletonSecurityHeader(r.Header, CSRFHeaderName)
			if !tokenOK || !validCSRFToken(token, sess.CSRFSecret) {
				rejectCSRF(w)
				return
			}

			if (requireMediaType || hasBody(r)) && !mediaTypeAllowed(r.Header) {
				rejectMediaType(w)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func rejectMultipartMediaType(w http.ResponseWriter) {
	api.WriteError(w, http.StatusUnsupportedMediaType, "media_type_unsupported", "multipart/form-data is required")
}

func rejectJSONMediaType(w http.ResponseWriter) {
	api.WriteError(w, http.StatusBadRequest, "request_invalid", "Content-Type must be one application/json value")
}

func rejectExactJSONMediaType(w http.ResponseWriter) {
	api.WriteError(w, http.StatusUnsupportedMediaType, "media_type_unsupported", "Content-Type must be application/json")
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
	origin, originPresent, originOK := optionalSingletonSecurityHeader(r.Header, "Origin")
	if originPresent {
		if !originOK {
			return false
		}
		return origin == allowedOrigin
	}

	referer, refererOK := singletonSecurityHeader(r.Header, "Referer")
	if !refererOK {
		return false
	}
	u, err := url.Parse(referer)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	return u.Scheme+"://"+u.Host == allowedOrigin
}

func singletonSecurityHeader(header http.Header, name string) (string, bool) {
	value, present, ok := optionalSingletonSecurityHeader(header, name)
	return value, present && ok
}

func optionalSingletonSecurityHeader(header http.Header, name string) (string, bool, bool) {
	values := header.Values(name)
	if len(values) == 0 {
		return "", false, true
	}
	if len(values) != 1 || values[0] == "" || strings.Contains(values[0], ",") {
		return "", true, false
	}
	return values[0], true, true
}

// hasBody treats an unknown Content-Length and any transfer encoding as a body.
// This prevents chunked requests from bypassing the JSON content-type check.
func hasBody(r *http.Request) bool {
	return r.ContentLength != 0 || len(r.TransferEncoding) > 0
}

func jsonContentTypeAllowed(header http.Header) bool {
	values := header.Values("Content-Type")
	if len(values) != 1 {
		return false
	}
	ct := values[0]
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

func exactJSONContentTypeAllowed(header http.Header) bool {
	values := header.Values("Content-Type")
	return len(values) == 1 && values[0] == "application/json"
}

func multipartContentTypeAllowed(header http.Header) bool {
	values := header.Values("Content-Type")
	if len(values) != 1 {
		return false
	}
	mediaType, params, err := mime.ParseMediaType(values[0])
	if err != nil || mediaType != "multipart/form-data" || len(params) != 1 {
		return false
	}
	return params["boundary"] != ""
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
