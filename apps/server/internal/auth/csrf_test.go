// Package auth_test exercises RequireCSRF (csrf.go): the fail-closed CSRF
// check AC-SEC-002 requires on every mutating cookie-authenticated
// request -- exact Origin (Referer scheme+host fallback), Content-Type,
// and a constant-time token match against the session's csrf_secret.
// Hermetic: no database. Sessions are store.Session values built directly
// in-process with a controlled CSRFSecret, the same "package auth_test
// constructs store.Session literals directly" convention cookie_test.go
// and session_adversarial_test.go already use for hermetic and live-DB
// tests respectively.
package auth_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// allowedTestOrigin is the allowedOrigin RequireCSRF is configured with
// throughout this file -- the shape config.PublicOrigin has in
// production (scheme://host, no trailing slash), an arbitrary stand-in
// value here.
const allowedTestOrigin = "http://localhost"

// csrfTokenFor returns the X-CSRF-Token header value a legitimate client
// holding sess's csrf_secret would send: base64.RawURLEncoding of the raw
// secret bytes, matching csrf.go's decode-then-compare step.
func csrfTokenFor(sess store.Session) string {
	return base64.RawURLEncoding.EncodeToString(sess.CSRFSecret)
}

// csrfTestSession returns a store.Session carrying a csrf_secret of the
// production size (32 bytes), every byte set to fill so two sessions
// built with different fill values are guaranteed to differ (these are
// test fixtures, not real secrets -- no randomness needed).
func csrfTestSession(fill byte) store.Session {
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = fill
	}
	return store.Session{ID: uuid.New(), CSRFSecret: secret}
}

// requestBody returns an io.Reader for body, or nil for an empty string --
// nil produces a request with no body at all (Body == nil, ContentLength
// == 0), matching a real bodiless mutating request (e.g. logout, most
// spec'd DELETEs) rather than a request with an empty JSON body. A
// non-empty body goes through strings.NewReader, which
// httptest.NewRequestWithContext (via http.NewRequestWithContext)
// recognizes and uses to set ContentLength automatically -- exactly what
// hasBody (csrf.go) inspects.
func requestBody(body string) io.Reader {
	if body == "" {
		return nil
	}
	return strings.NewReader(body)
}

// passThroughHandler is the downstream handler RequireCSRF wraps in these
// tests: it just proves next.ServeHTTP ran.
func passThroughHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// assertCSRFRejected fails the test unless rec's body is the standard
// error envelope carrying code "csrf_rejected" -- the single code every
// rejection reason returns (no oracle differentiating which check
// failed).
func assertCSRFRejected(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error envelope: %v (body=%s)", err, rec.Body.String())
	}
	if body.Error.Code != "csrf_rejected" {
		t.Errorf("error.code = %q, want %q", body.Error.Code, "csrf_rejected")
	}
	if body.Error.Message == "" {
		t.Error("error.message is empty, want a non-empty message")
	}
}

// TestRequireCSRF_Matrix is the CSRF matrix: every combination of method,
// Origin, Referer, Content-Type, request body, and token that determines
// whether a mutating cookie-authenticated request is allowed through.
//
// Rows 1-13 are task-8-brief.md's Step 1 matrix verbatim (each given a
// "{}" body so the Content-Type check in rows that set one is actually
// exercised rather than skipped by DD-C6's bodiless carve-out below).
// Row 14 ("charset suffix content-type allowed") is required by the
// integration owner's original Content-Type ruling: the brief asked for a
// row proving each side of the decision, and the missing/wrong-content-
// type rows already cover the reject side.
//
// Row 15 pins the newly-accepted Content-Type variants that motivated the
// switch to mime.ParseMediaType in the first place: row 14 only covers the
// old two-literal allowlist's exact charset spelling
// ("; charset=utf-8", with the leading space); row 15 uses
// "application/json;charset=UTF-8" (no space before charset, uppercase
// encoding name) -- a shape real fetch()/XHR clients also send, which the
// old literal allowlist would have rejected.
//
// Rows 16-19 were added after Opus review of task 8 caught a defect in
// that original ruling (not in its implementation): requiring
// Content-Type on every mutating request 403s every bodiless mutating
// endpoint (POST /auth/logout, most spec'd DELETEs) the moment they're
// wired. The corrected ruling, DD-C6 (csrf.go), only checks Content-Type
// when the request actually carries a body:
//
//   - Row 16/17 prove a bodiless DELETE/POST with a valid Origin and
//     token succeeds with no Content-Type at all.
//   - Row 18 proves the carve-out is about Content-Type, not about CSRF
//     itself: a request that *does* carry a body (deliberately
//     form-encoded-shaped, "foo=bar" -- the classic auto-submitting
//     cross-site HTML form vector, which never sets Content-Type:
//     application/json) is still rejected. This is the defense-in-depth
//     row; row 7 (adjusted to also carry a "{}" body under the new
//     ruling) is the generic version of the same check.
//   - Row 19 completes the original decode-first requirement: a token
//     that is the *same* session's secret, correctly shaped, but encoded
//     with the standard base64 alphabet/padding (StdEncoding) instead of
//     the RawURLEncoding the header contract requires, must still be
//     rejected -- proving the compare is anchored to the documented
//     encoding, not merely "some encoding of the right bytes."
//
// A second review pass (round 2) confirmed hasBody's original
// ContentLength > 0 check classified a real server's chunked/h2 request
// (ContentLength == -1) as bodiless, letting it silently skip the
// Content-Type gate -- see hasBody's own doc comment (csrf.go) for the
// fix; no new row was added for that specific fix since httptest cannot
// produce a request shaped the way a real chunked request would be by the
// time RequireCSRF sees it (net/http.Server, not this test file, is what
// sets ContentLength = -1) -- the RED evidence for that fix instead used
// a hand-built *http.Request, recorded in task-8-report.md, not this
// matrix.
//
// Rows 20-22 (phase gate hardening) pin three Origin shapes an attacker
// might expect a looser check to accept: the literal string "null" (an
// opaque origin -- a sandboxed iframe, a file:// page, or certain
// redirected cross-origin requests all send exactly this), a host that
// carries the allowed origin as a literal PREFIX with an attacker-chosen
// suffix appended (".evil.test" is a different host entirely, not a
// subdomain relationship, but a prefix/substring check would wrongly
// treat it as related), and a trailing-slash variant of the allowed
// origin. originAllowed's exact `==` comparison already rejects all
// three -- these are pinned regressions, not fixes for a found defect.
func TestRequireCSRF_Matrix(t *testing.T) {
	t.Parallel()

	sess := csrfTestSession(0xAB)
	otherSess := csrfTestSession(0xCD)
	validToken := csrfTokenFor(sess)

	cases := []struct {
		name        string
		method      string
		origin      string
		referer     string
		contentType string
		token       string
		body        string
		wantStatus  int
	}{
		{"valid same-origin", "POST", "http://localhost", "", "application/json", validToken, "{}", http.StatusOK},
		{"GET never needs CSRF", "GET", "https://evil.example", "", "", "", "", http.StatusOK},
		{"missing origin, valid referer fallback", "POST", "", "http://localhost/app", "application/json", validToken, "{}", http.StatusOK},
		{"missing both origin and referer", "POST", "", "", "application/json", validToken, "{}", http.StatusForbidden},
		{"cross-origin", "POST", "https://evil.example", "", "application/json", validToken, "{}", http.StatusForbidden},
		{"referer wrong origin", "POST", "", "https://evil.example/x", "application/json", validToken, "{}", http.StatusForbidden},
		{"missing content-type, body present", "POST", "http://localhost", "", "", validToken, "{}", http.StatusForbidden},
		{"wrong content-type", "POST", "http://localhost", "", "text/plain", validToken, "{}", http.StatusForbidden},
		{"missing token", "POST", "http://localhost", "", "application/json", "", "{}", http.StatusForbidden},
		{"wrong token", "POST", "http://localhost", "", "application/json", "not-the-real-token", "{}", http.StatusForbidden},
		{"another session's valid-shaped token", "POST", "http://localhost", "", "application/json", csrfTokenFor(otherSess), "{}", http.StatusForbidden},
		{"PATCH also enforced", "PATCH", "https://evil.example", "", "application/json", validToken, "{}", http.StatusForbidden},
		{"DELETE also enforced", "DELETE", "https://evil.example", "", "application/json", validToken, "{}", http.StatusForbidden},
		{"charset suffix content-type allowed", "POST", "http://localhost", "", "application/json; charset=utf-8", validToken, "{}", http.StatusOK},
		{"no-space uppercase charset content-type allowed", "POST", "http://localhost", "", "application/json;charset=UTF-8", validToken, "{}", http.StatusOK},
		{"bodiless DELETE skips content-type check", "DELETE", "http://localhost", "", "", validToken, "", http.StatusOK},
		{"bodiless POST (logout-shaped) skips content-type check", "POST", "http://localhost", "", "", validToken, "", http.StatusOK},
		{"body present without content-type still rejected (form-style body)", "POST", "http://localhost", "", "", validToken, "foo=bar", http.StatusForbidden},
		{"same session's secret, wrong base64 encoding (standard alphabet/padding)", "POST", "http://localhost", "", "application/json", base64.StdEncoding.EncodeToString(sess.CSRFSecret), "{}", http.StatusForbidden},
		// Rows 20-22 (security-relevant cheap-win hardening): pin
		// originAllowed's exact-string-match contract (csrf.go) against
		// three shapes a naive Origin check (substring/prefix, or a
		// scheme+host-only comparison that tolerates a trailing slash)
		// could wrongly accept. originAllowed already does a bare Go `==`
		// against allowedOrigin, so these pass today -- the rows exist to
		// keep it that way as a pinned regression, not because a defect
		// was found.
		{"Origin: null (opaque origin -- sandboxed iframe, file://, etc.)", "POST", "null", "", "application/json", validToken, "{}", http.StatusForbidden},
		{"suffix-host origin (allowed origin as a literal prefix, evil suffix appended)", "POST", "http://localhost.evil.test", "", "application/json", validToken, "{}", http.StatusForbidden},
		{"trailing-slash origin", "POST", "http://localhost/", "", "application/json", validToken, "{}", http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(context.Background(), tc.method, "/resume", requestBody(tc.body))
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.referer != "" {
				req.Header.Set("Referer", tc.referer)
			}
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			if tc.token != "" {
				req.Header.Set(auth.CSRFHeaderName, tc.token)
			}
			req = req.WithContext(auth.ContextWithSession(req.Context(), sess))

			rec := httptest.NewRecorder()
			handler := auth.RequireCSRF(allowedTestOrigin)(passThroughHandler())
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus == http.StatusForbidden {
				assertCSRFRejected(t, rec)
			}
		})
	}
}

// TestRequireCSRF_SafeMethodsBypassWithoutSession proves GET, HEAD, and
// OPTIONS pass through untouched -- not merely "with a valid session
// present" (the matrix's GET row), but even when no session was ever put
// in context at all, and even with a cross-origin Origin header. Only
// task-8-brief.md's matrix exercises GET; this covers HEAD and OPTIONS
// too, per the brief's own "GET/HEAD/OPTIONS pass through untouched"
// requirement.
func TestRequireCSRF_SafeMethodsBypassWithoutSession(t *testing.T) {
	t.Parallel()

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(context.Background(), method, "/resume", nil)
			req.Header.Set("Origin", "https://evil.example")
			// Deliberately no auth.ContextWithSession call: a safe method
			// must never need to read the session from context.

			rec := httptest.NewRecorder()
			handler := auth.RequireCSRF(allowedTestOrigin)(passThroughHandler())
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, http.StatusOK, rec.Body.String())
			}
		})
	}
}

// TestRequireCSRF_MutatingWithoutSessionInContextRejects is a
// defense-in-depth check beyond the matrix: RequireCSRF is documented to
// run after RequireSession in the chain (so a session is always in
// context by the time it runs on a mutating request), but if that
// ordering were ever violated, a missing session must still fail closed
// rather than panic or fall through.
func TestRequireCSRF_MutatingWithoutSessionInContextRejects(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/resume", nil)
	req.Header.Set("Origin", allowedTestOrigin)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(auth.CSRFHeaderName, "irrelevant")
	// Deliberately no auth.ContextWithSession call.

	rec := httptest.NewRecorder()
	handler := auth.RequireCSRF(allowedTestOrigin)(passThroughHandler())
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d (body=%s)", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	assertCSRFRejected(t, rec)
}
