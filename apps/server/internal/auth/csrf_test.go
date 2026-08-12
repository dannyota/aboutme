// Package auth_test exercises the fail-closed CSRF checks for mutating,
// cookie-authenticated requests. These tests are hermetic.
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

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// allowedTestOrigin has the scheme-and-host shape required by PublicOrigin.
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

// requestBody returns nil for a bodiless request so ContentLength remains zero.
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

// TestRequireCSRF_Matrix covers method, origin or Referer, body media type,
// token value, and token encoding. The hostile origins catch substring and
// normalization checks that would accept a different origin.
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
		{"missing content-type, body present", "POST", "http://localhost", "", "", validToken, "{}", http.StatusBadRequest},
		{"wrong content-type", "POST", "http://localhost", "", "text/plain", validToken, "{}", http.StatusBadRequest},
		{"missing token", "POST", "http://localhost", "", "application/json", "", "{}", http.StatusForbidden},
		{"wrong token", "POST", "http://localhost", "", "application/json", "not-the-real-token", "{}", http.StatusForbidden},
		{"another session's valid-shaped token", "POST", "http://localhost", "", "application/json", csrfTokenFor(otherSess), "{}", http.StatusForbidden},
		{"PATCH also enforced", "PATCH", "https://evil.example", "", "application/json", validToken, "{}", http.StatusForbidden},
		{"DELETE also enforced", "DELETE", "https://evil.example", "", "application/json", validToken, "{}", http.StatusForbidden},
		{"charset suffix content-type allowed", "POST", "http://localhost", "", "application/json; charset=utf-8", validToken, "{}", http.StatusOK},
		{"no-space uppercase charset content-type allowed", "POST", "http://localhost", "", "application/json;charset=UTF-8", validToken, "{}", http.StatusOK},
		{"bodiless DELETE skips content-type check", "DELETE", "http://localhost", "", "", validToken, "", http.StatusOK},
		{"bodiless POST (logout-shaped) skips content-type check", "POST", "http://localhost", "", "", validToken, "", http.StatusOK},
		{"body present without content-type still rejected (form-style body)", "POST", "http://localhost", "", "", validToken, "foo=bar", http.StatusBadRequest},
		{"same session's secret, wrong base64 encoding (standard alphabet/padding)", "POST", "http://localhost", "", "application/json", base64.StdEncoding.EncodeToString(sess.CSRFSecret), "{}", http.StatusForbidden},
		// These rows require an exact origin match.
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
// OPTIONS pass through even without a session and with a cross-origin
// Origin header.
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

// TestRequireCSRFMultipart_MediaTypeIsolation proves the photo upload entry
// point accepts only multipart/form-data while the existing entry point stays
// JSON-only. A shared permissive media-type check would let one route weaken
// every cookie-authenticated mutation.
func TestRequireCSRFMultipart_MediaTypeIsolation(t *testing.T) {
	t.Parallel()

	sess := csrfTestSession(0xEF)
	token := csrfTokenFor(sess)

	for _, tc := range []struct {
		name       string
		middleware func(string) api.Middleware
		mediaType  string
		wantStatus int
	}{
		{"multipart accepts multipart", auth.RequireCSRFMultipart, "multipart/form-data; boundary=test", http.StatusOK},
		{"multipart rejects JSON", auth.RequireCSRFMultipart, "application/json", http.StatusUnsupportedMediaType},
		{"JSON accepts JSON", auth.RequireCSRF, "application/json", http.StatusOK},
		{"JSON rejects multipart", auth.RequireCSRF, "multipart/form-data; boundary=test", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/resume/photo", strings.NewReader("body"))
			req.Header.Set("Origin", allowedTestOrigin)
			req.Header.Set("Content-Type", tc.mediaType)
			req.Header.Set(auth.CSRFHeaderName, token)
			req = req.WithContext(auth.ContextWithSession(req.Context(), sess))

			rec := httptest.NewRecorder()
			tc.middleware(allowedTestOrigin)(passThroughHandler()).ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestRequireCSRFMultipart_TokenAndOriginPrecedeMediaType(t *testing.T) {
	t.Parallel()

	sess := csrfTestSession(0xAC)
	for _, tc := range []struct {
		name   string
		origin string
		token  string
	}{
		{"foreign origin", "https://foreign.example", csrfTokenFor(sess)},
		{"missing token", allowedTestOrigin, ""},
		{"wrong token", allowedTestOrigin, "wrong"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/resume/photo", strings.NewReader("body"))
			req.Header.Set("Origin", tc.origin)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set(auth.CSRFHeaderName, tc.token)
			req = req.WithContext(auth.ContextWithSession(req.Context(), sess))
			rec := httptest.NewRecorder()
			auth.RequireCSRFMultipart(allowedTestOrigin)(passThroughHandler()).ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 (body=%s)", rec.Code, rec.Body.String())
			}
			assertCSRFRejected(t, rec)
		})
	}
}

func TestRequireCSRFMultipart_ZeroBodyRequiresSingletonMediaType(t *testing.T) {
	t.Parallel()

	sess := csrfTestSession(0xBD)
	for _, test := range []struct {
		name        string
		contentType []string
	}{
		{name: "missing"},
		{name: "duplicate", contentType: []string{"multipart/form-data; boundary=test", "multipart/form-data; boundary=test"}},
		{name: "folded", contentType: []string{"multipart/form-data; boundary=test, multipart/form-data; boundary=test"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/resume/photo", nil)
			req.Header.Set("Origin", allowedTestOrigin)
			req.Header.Set(auth.CSRFHeaderName, csrfTokenFor(sess))
			for _, value := range test.contentType {
				req.Header.Add("Content-Type", value)
			}
			req = req.WithContext(auth.ContextWithSession(req.Context(), sess))

			rec := httptest.NewRecorder()
			auth.RequireCSRFMultipart(allowedTestOrigin)(passThroughHandler()).ServeHTTP(rec, req)
			if rec.Code != http.StatusUnsupportedMediaType {
				t.Fatalf("status = %d, want 415 (body=%s)", rec.Code, rec.Body.String())
			}
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.Error.Code != "media_type_unsupported" {
				t.Fatalf("code = %q, want media_type_unsupported", body.Error.Code)
			}
		})
	}
}
