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
	"net/http"
	"net/http/httptest"
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
// Origin, Referer, Content-Type, and token that determines whether a
// mutating cookie-authenticated request is allowed through.
//
// The first 13 rows are task-8-brief.md's Step 1 matrix verbatim. The
// 14th row ("charset suffix content-type allowed") is required by the
// integration owner's Content-Type ruling (task-8-brief.md's offered
// choice, resolved: accept exactly "application/json" and
// "application/json; charset=utf-8" -- the latter because browser
// fetch()/XHR implementations commonly append that suffix automatically
// -- reject every other value) -- the brief asked for a row proving each
// side of that ruling; the missing/wrong-content-type rows below already
// cover the reject side, so this row adds the accept side.
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
		wantStatus  int
	}{
		{"valid same-origin", "POST", "http://localhost", "", "application/json", validToken, http.StatusOK},
		{"GET never needs CSRF", "GET", "https://evil.example", "", "", "", http.StatusOK},
		{"missing origin, valid referer fallback", "POST", "", "http://localhost/app", "application/json", validToken, http.StatusOK},
		{"missing both origin and referer", "POST", "", "", "application/json", validToken, http.StatusForbidden},
		{"cross-origin", "POST", "https://evil.example", "", "application/json", validToken, http.StatusForbidden},
		{"referer wrong origin", "POST", "", "https://evil.example/x", "application/json", validToken, http.StatusForbidden},
		{"missing content-type", "POST", "http://localhost", "", "", validToken, http.StatusForbidden},
		{"wrong content-type", "POST", "http://localhost", "", "text/plain", validToken, http.StatusForbidden},
		{"missing token", "POST", "http://localhost", "", "application/json", "", http.StatusForbidden},
		{"wrong token", "POST", "http://localhost", "", "application/json", "not-the-real-token", http.StatusForbidden},
		{"another session's valid-shaped token", "POST", "http://localhost", "", "application/json", csrfTokenFor(otherSess), http.StatusForbidden},
		{"PATCH also enforced", "PATCH", "https://evil.example", "", "application/json", validToken, http.StatusForbidden},
		{"DELETE also enforced", "DELETE", "https://evil.example", "", "application/json", validToken, http.StatusForbidden},
		{"charset suffix content-type allowed", "POST", "http://localhost", "", "application/json; charset=utf-8", validToken, http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(context.Background(), tc.method, "/resume", nil)
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
