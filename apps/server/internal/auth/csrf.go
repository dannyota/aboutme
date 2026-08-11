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

// DD-C6: Content-Type ruling, corrected. The original task-8 ruling
// required Content-Type on every mutating request; Opus review of task 8
// caught that this 403s every bodiless mutating endpoint (POST
// /auth/logout, most spec'd DELETEs) the moment they're wired, since a
// bodiless request has no reason to ever set Content-Type. Corrected
// rule: the Content-Type check (contentTypeAllowed) applies only when the
// request actually carries a body (hasBody, defined below). A bodiless
// mutating request skips the Content-Type check entirely but still must
// pass every other check (session, Origin/Referer, token) unchanged --
// this is not a CSRF carve-out, only a Content-Type carve-out. When a
// body is present, Content-Type must parse (mime.ParseMediaType) to
// media type "application/json" with no parameters other than an
// optional charset of any value/case -- accepting real clients'
// "application/json;charset=UTF-8" and "application/json; charset=utf-8"
// variants alike, while still rejecting an unrelated or malformed
// Content-Type.

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
// Ordering in the chain (handlers.go's RegisterRoutes/sessionChain,
// mirroring router.go's New): RequestID -> SecurityHeaders -> Logging ->
// NoStoreCache -> RateLimit -> BodyLimit -> RequireSession -> RequireCSRF
// -> handler (fix round 1, M1: corrected from an earlier version of this
// comment that omitted SecurityHeaders, NoStoreCache, and RateLimit).
// RequireCSRF must run after RequireSession because it reads the
// session's CSRFSecret from context; running it earlier would always
// fail closed (SessionFromContext returns ok=false), never a bypass.
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

			// Check order is deliberate: method -> origin -> content-type
			// -> session -> token. Cheapest and most decisive checks run
			// first, so a request that fails an earlier one never pays
			// for the session lookup or the token compare.
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

// rejectCSRF writes the one response every CSRF rejection reason
// produces, so no call site can accidentally leak which check failed
// through a distinct status or message.
func rejectCSRF(w http.ResponseWriter) {
	api.WriteError(w, http.StatusForbidden, csrfRejectedCode, csrfRejectedMessage)
}

// DD-C16's sameSiteInitiated used to live here: a same-site-initiation
// check (Sec-Fetch-Site: same-origin, else originAllowed) that gated
// GET /auth/{provider}/start?purpose=link|reauth, the one flow this
// package could not protect with RequireCSRF because GET is never a
// CSRF-checked method. P1.1 item 2 (docs/plans/phase-1-deferred.md) moved
// that flow to a CSRF-protected POST, so the check has no caller left and
// is gone rather than kept as an unused second authorization primitive.
// The protection it provided is not: the GET refuses those two purposes
// for every caller now, same-site included. See start.go's top-of-file
// comment for the full reasoning, including why the fallback this helper
// depended on (a same-origin Referer, for browsers without Fetch
// Metadata) was itself the reason to replace it -- P8-sec's standard
// `Referrer-Policy: no-referrer` would have silently broken linking.

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

// hasBody reports whether r carries a request body: a non-zero
// Content-Length (this deliberately catches -1, net/http's "unknown
// length" sentinel -- a real server strips the raw Transfer-Encoding
// header off an incoming chunked or unknown-length request and reports
// ContentLength == -1 rather than leaving a header for r.Header.Get to
// see, so a bare ">0" check would wrongly treat a genuinely-bodied
// chunked/h2 request as bodiless and skip the Content-Type gate below --
// or r.TransferEncoding (the field Go's server actually populates for
// chunked bodies, never the raw header) being non-empty. RequireCSRF only
// enforces contentTypeAllowed when this is true -- DD-C6 above. One
// accepted consequence: a zero-length chunked body (ContentLength == 0
// but TransferEncoding present) with no Content-Type now also 403s --
// fail-closed is the correct direction for an edge case that thin.
func hasBody(r *http.Request) bool {
	return r.ContentLength != 0 || len(r.TransferEncoding) > 0
}

// contentTypeAllowed reports whether ct -- a Content-Type header value on
// a request hasBody already confirmed carries a body -- is accepted under
// DD-C6: media type application/json (mime.ParseMediaType already
// lowercases it) with no parameters other than an optional charset of any
// value or case.
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
