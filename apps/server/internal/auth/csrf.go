package auth

import (
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/url"

	"github.com/dannyota/aboutme/apps/server/internal/api"
)

// CSRFHeaderName is the header carrying a mutating request's CSRF token:
// base64.RawURLEncoding of the authenticated session's csrf_secret.
const CSRFHeaderName = "X-CSRF-Token"

// Content-Type decision (integration owner ruling, task-8 scope
// adjustment -- the brief offered a choice and asked for it to be
// pinned): accept exactly these two values and reject everything else,
// including any other parameter or casing variant. The charset=utf-8
// suffix is allowed because browser fetch()/XHR implementations commonly
// append it to a JSON body automatically, so requiring its exact absence
// would reject ordinary same-origin clients along with attackers.
const (
	contentTypeJSON        = "application/json"
	contentTypeJSONCharset = "application/json; charset=utf-8"
)

// csrfRejectedCode is the single error code every CSRF rejection reason
// returns -- missing/mismatched Origin or Referer, missing/wrong
// Content-Type, missing session, or missing/wrong token are all
// indistinguishable from the response alone. This is the same no-oracle
// reasoning as ErrSessionInvalid and ErrTransactionInvalid: an attacker
// probing a request never learns which specific check rejected it.
const csrfRejectedCode = "csrf_rejected"

const csrfRejectedMessage = "CSRF validation failed"

// RequireCSRF wraps a handler that has already passed through session
// authentication: it reads the session from context via
// SessionFromContext (context.go), populated by Task 9's RequireSession.
// Only mutating methods are checked -- GET, HEAD, and OPTIONS pass
// through untouched regardless of Origin, Referer, Content-Type, or
// token, and without ever reading the session from context.
//
// Ordering in the chain (handlers.go): RequestID -> Logging -> BodyLimit
// -> RequireSession -> RequireCSRF -> handler. RequireCSRF must run after
// RequireSession because it reads the session's CSRFSecret from context;
// running it earlier would always fail closed (SessionFromContext returns
// ok=false), never a bypass.
//
// Every rejection path returns 403 with the single csrfRejectedCode via
// api.WriteError -- fail closed, per AC-SEC-002.
func RequireCSRF(allowedOrigin string) api.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isMutatingMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}

			if !originAllowed(r, allowedOrigin) {
				rejectCSRF(w)
				return
			}

			if !contentTypeAllowed(r.Header.Get("Content-Type")) {
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

// rejectCSRF writes the one response every CSRF rejection reason
// produces, so no call site can accidentally leak which check failed
// through a distinct status or message.
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

// originAllowed implements AC-SEC-002's fail-closed Origin check (design
// spec §3's CSRF row): exact match against allowedOrigin when the request
// carries an Origin header; otherwise fall back to comparing the Referer
// header's scheme+host (its RFC 6454 "origin") against allowedOrigin. A
// request carrying neither a usable Origin nor a usable Referer is
// rejected -- fail closed, never permissive by default.
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

// contentTypeAllowed reports whether ct is exactly one of the two values
// the Content-Type ruling above accepts.
func contentTypeAllowed(ct string) bool {
	return ct == contentTypeJSON || ct == contentTypeJSONCharset
}

// validCSRFToken reports whether headerToken -- the raw X-CSRF-Token
// header value, expected to be base64.RawURLEncoding of a session's
// csrf_secret -- matches secret.
//
// It decodes headerToken first and compares the raw bytes with
// crypto/subtle.ConstantTimeCompare. Comparing the *encoded strings*
// directly (e.g. via == or by encoding secret and comparing byte slices
// of the two encoded forms) is not the same operation as
// constant-time-comparing the secret itself and is a common subtle bug
// this deliberately avoids: decode first, then compare raw bytes.
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
