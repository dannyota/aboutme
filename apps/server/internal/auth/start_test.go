package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/auth/oidctest"
	"danny.vn/aboutme/apps/server/internal/store"
)

type startEnvelopeBody struct {
	Data struct {
		AuthorizeURL string `json:"authorizeUrl"`
	} `json:"data"`
}

func decodeStartEnvelope(t *testing.T, resp *http.Response) string {
	t.Helper()

	var body startEnvelopeBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode start envelope: %v", err)
	}
	if body.Data.AuthorizeURL == "" {
		t.Fatal("start envelope carries no data.authorizeUrl")
	}
	return body.Data.AuthorizeURL
}

func doStartPOST(t *testing.T, handler http.Handler, path string, sess store.Session, raw string) *http.Response {
	t.Helper()
	return doJSON(t, handler, http.MethodPost, path, testPublicOrigin, csrfTokenFor(sess), "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
}

func TestStartPOST_Link_ReturnsAuthorizeURL(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	pool := newRowInspectorPool(t)

	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID)

	resp := doStartPOST(t, handler, auth.GoogleStartPath+"?purpose=link", sess, raw) //nolint:bodyclose // doStartPOST -> doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	authorizeURL := decodeStartEnvelope(t, resp)
	if !strings.HasPrefix(authorizeURL, p.URL) {
		t.Errorf("authorizeUrl = %q, want it to point at the provider's own authorize endpoint (%s...)", authorizeURL, p.URL)
	}
	for _, param := range []string{"state=", "code_challenge=", "code_challenge_method=S256"} {
		if !strings.Contains(authorizeURL, param) {
			t.Errorf("authorizeUrl = %q, want it to carry %s (the transaction's own state/PKCE binding)", authorizeURL, param)
		}
	}

	if txCookie := extractCookie(resp, auth.OAuthTxCookieName); txCookie == nil || txCookie.Value == "" {
		t.Error("no __Host-oauth-tx cookie was set; the caller cannot complete a flow whose handle it never received")
	}
	if got := oauthTransactionCountForLinkingUser(context.Background(), t, pool, userID); got != 1 {
		t.Errorf("oauth_transactions rows for linking_user_id=%s = %d, want 1", userID, got)
	}
}

func TestStartPOST_Reauth_NeedsNoRecentReauth(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID)
	forceReauthenticatedAtStale(t, sess.ID)

	resp := doStartPOST(t, handler, auth.GoogleStartPath+"?purpose=reauth", sess, raw) //nolint:bodyclose // doStartPOST -> doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d (purpose=reauth must not require a recent reauth)", resp.StatusCode, http.StatusOK)
	}
	decodeStartEnvelope(t, resp)
}

func TestStartPOST_Link_RejectsStaleReauth(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	pool := newRowInspectorPool(t)

	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID)
	forceReauthenticatedAtStale(t, sess.ID)

	resp := doStartPOST(t, handler, auth.GoogleStartPath+"?purpose=link", sess, raw) //nolint:bodyclose // doStartPOST -> doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
	if got := decodeErrorCode(t, resp); got != "reauth_required" {
		t.Errorf("error.code = %q, want %q", got, "reauth_required")
	}
	if got := oauthTransactionCountForLinkingUser(context.Background(), t, pool, userID); got != 0 {
		t.Errorf("oauth_transactions rows for linking_user_id=%s = %d, want 0 (the gate runs before Begin)", userID, got)
	}
}

func TestStartPOST_RejectsWithoutCSRFOrSession(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		origin     string
		withToken  bool
		withCookie bool
		wantStatus int
		wantCode   string
	}{
		{"no CSRF token", testPublicOrigin, false, true, http.StatusForbidden, "csrf_rejected"},
		{"foreign Origin", "https://evil.example", true, true, http.StatusForbidden, "csrf_rejected"},
		{"no Origin at all (fail closed)", "", true, true, http.StatusForbidden, "csrf_rejected"},
		{"no session", testPublicOrigin, true, false, http.StatusUnauthorized, "session_required"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := oidctest.NewProvider(t)
			handler, q := newTestService(t, withGoogleIssuer(p.URL))
			pool := newRowInspectorPool(t)

			userID := createTestUser(t, q)
			raw, sess := issueTestSession(t, q, userID)

			token := ""
			if tc.withToken {
				token = csrfTokenFor(sess)
			}
			var cookies []*http.Cookie
			if tc.withCookie {
				cookies = append(cookies, sessionRequestCookie(raw))
			}

			resp := doJSON(t, handler, http.MethodPost, auth.GoogleStartPath+"?purpose=link", tc.origin, token, "", cookies...) //nolint:bodyclose // doJSON closes the body itself before returning.
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if got := decodeErrorCode(t, resp); got != tc.wantCode {
				t.Errorf("error.code = %q, want %q", got, tc.wantCode)
			}
			if got := oauthTransactionCountForLinkingUser(context.Background(), t, pool, userID); got != 0 {
				t.Errorf("oauth_transactions rows for linking_user_id=%s = %d, want 0 (no transaction may be created by a rejected start)", userID, got)
			}
		})
	}
}

func TestStartPOST_RejectsUnsupportedPurpose(t *testing.T) {
	t.Parallel()

	for _, query := range []string{"", "?purpose=", "?purpose=login", "?purpose=Link", "?purpose=nonsense"} {
		t.Run("purpose"+query, func(t *testing.T) {
			t.Parallel()

			p := oidctest.NewProvider(t)
			handler, q := newTestService(t, withGoogleIssuer(p.URL))
			pool := newRowInspectorPool(t)

			userID := createTestUser(t, q)
			raw, sess := issueTestSession(t, q, userID)

			resp := doStartPOST(t, handler, auth.GoogleStartPath+query, sess, raw) //nolint:bodyclose // doStartPOST -> doJSON closes the body itself before returning.
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
			}
			if got := oauthTransactionCountForLinkingUser(context.Background(), t, pool, userID); got != 0 {
				t.Errorf("oauth_transactions rows for linking_user_id=%s = %d, want 0", userID, got)
			}
		})
	}
}

func TestStartGET_LinkAndReauthPurposesUnavailable(t *testing.T) {
	t.Parallel()

	headerCases := []struct {
		name    string
		headers map[string]string
	}{
		{"same-origin", map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": testPublicOrigin}},
		{"cross-site", map[string]string{"Sec-Fetch-Site": "cross-site"}},
		{"no fetch metadata, matching Origin", map[string]string{"Origin": testPublicOrigin}},
		{"no signals at all", map[string]string{}},
	}

	for _, purpose := range []auth.Purpose{auth.PurposeLink, auth.PurposeReauth} {
		for _, hc := range headerCases {
			t.Run(string(purpose)+"/"+hc.name, func(t *testing.T) {
				t.Parallel()

				p := oidctest.NewProvider(t)
				handler, q := newTestService(t, withGoogleIssuer(p.URL))
				pool := newRowInspectorPool(t)

				userID := createTestUser(t, q)
				raw, _ := issueTestSession(t, q, userID)

				resp := doGetWithHeaders(t, handler, auth.GoogleStartPath+"?purpose="+string(purpose), hc.headers, sessionRequestCookie(raw)) //nolint:bodyclose // doGetWithHeaders closes the body itself before returning.

				if resp.StatusCode != http.StatusMethodNotAllowed {
					t.Fatalf("status = %d, want %d (link/reauth is POST-only now)", resp.StatusCode, http.StatusMethodNotAllowed)
				}
				if got := resp.Header.Get("Allow"); got != http.MethodPost {
					t.Errorf("Allow = %q, want %q", got, http.MethodPost)
				}
				if tx := extractCookie(resp, auth.OAuthTxCookieName); tx != nil && tx.MaxAge >= 0 {
					t.Error("a live __Host-oauth-tx cookie was set on a rejected start")
				}
				if got := oauthTransactionCountForLinkingUser(context.Background(), t, pool, userID); got != 0 {
					t.Errorf("oauth_transactions rows for linking_user_id=%s = %d, want 0", userID, got)
				}
			})
		}
	}
}

func TestStartGET_LoginUnaffected(t *testing.T) {
	t.Parallel()

	for _, path := range []string{auth.GoogleStartPath, auth.GoogleStartPath + "?purpose=login"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			p := oidctest.NewProvider(t)
			handler, _ := newTestService(t, withGoogleIssuer(p.URL))

			resp := doGetWithHeaders(t, handler, path, map[string]string{"Sec-Fetch-Site": "cross-site"}) //nolint:bodyclose // doGetWithHeaders closes the body itself before returning.
			if resp.StatusCode != http.StatusFound {
				t.Fatalf("status = %d, want %d (a login start is reachable from anywhere)", resp.StatusCode, http.StatusFound)
			}
			if loc := resp.Header.Get("Location"); !strings.HasPrefix(loc, p.URL) {
				t.Errorf("Location = %q, want the provider's authorize endpoint", loc)
			}
			if extractCookie(resp, auth.OAuthTxCookieName) == nil {
				t.Error("no __Host-oauth-tx cookie set on a successful login start")
			}
		})
	}
}

func TestStartGET_LoginNextValidation(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	cases := []struct {
		name string
		next string
		want string
	}{
		{name: "absent", want: "/app/resumes"},
		{name: "relative path", next: "/oauth/authorize?x=1", want: "/oauth/authorize?x=1"},
		{name: "network path", next: "//evil.example", want: "/app/resumes"},
		{name: "absolute URL", next: "https://evil.example", want: "/app/resumes"},
		{name: "javascript scheme", next: "javascript:alert(1)", want: "/app/resumes"},
		{name: "no leading slash", next: "oauth/authorize", want: "/app/resumes"},
		{name: "backslash", next: `/\\evil.example`, want: "/app/resumes"},
		{name: "2048 bytes", next: "/" + strings.Repeat("a", 2047), want: "/" + strings.Repeat("a", 2047)},
		{name: "2049 bytes", next: "/" + strings.Repeat("a", 2048), want: "/app/resumes"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := auth.GoogleStartPath
			if tc.next != "" {
				path += "?next=" + url.QueryEscape(tc.next)
			}
			resp := doGet(t, handler, path) //nolint:bodyclose // doGet closes the body itself before returning.
			if resp.StatusCode != http.StatusFound {
				t.Fatalf("GET start status = %d, want %d", resp.StatusCode, http.StatusFound)
			}
			cookie := extractCookie(resp, auth.OAuthTxCookieName)
			if cookie == nil {
				t.Fatal("start response missing __Host-oauth-tx cookie")
			}
			tx, err := auth.NewTransactionStore(q).Consume(context.Background(), cookie.Value, auth.ProviderGoogle)
			if err != nil {
				t.Fatalf("Consume() error = %v", err)
			}
			if tx.ReturnPath != tc.want {
				t.Errorf("ReturnPath = %q, want %q", tx.ReturnPath, tc.want)
			}
		})
	}
}

func TestStart_RateLimited(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL), withStartRateLimit(3, time.Minute))

	for i := range 3 {
		resp := doGet(t, handler, auth.GoogleStartPath) //nolint:bodyclose // doGet closes the body itself before returning.
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("start #%d status = %d, want %d (still inside the budget)", i+1, resp.StatusCode, http.StatusFound)
		}
	}

	resp := doGet(t, handler, auth.GoogleStartPath) //nolint:bodyclose // doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("start #4 status = %d, want %d (budget exhausted)", resp.StatusCode, http.StatusTooManyRequests)
	}
	if got := decodeErrorCode(t, resp); got != "rate_limited" {
		t.Errorf("error.code = %q, want %q", got, "rate_limited")
	}
	retryAfter := resp.Header.Get("Retry-After")
	if secs, err := strconv.Atoi(retryAfter); err != nil || secs < 1 {
		t.Errorf("Retry-After = %q, want a whole-second count of at least 1", retryAfter)
	}
}

func TestStart_RateLimitKeyedOnAccountAndIP(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withStartRateLimit(2, time.Minute))

	rawA, sessA := issueTestSession(t, q, createTestUser(t, q))
	rawB, sessB := issueTestSession(t, q, createTestUser(t, q))

	for i := range 2 {
		resp := doStartPOST(t, handler, auth.GoogleStartPath+"?purpose=link", sessA, rawA) //nolint:bodyclose // doStartPOST -> doJSON closes the body itself before returning.
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("account A start #%d status = %d, want %d", i+1, resp.StatusCode, http.StatusOK)
		}
	}
	if resp := doStartPOST(t, handler, auth.GoogleStartPath+"?purpose=link", sessA, rawA); resp.StatusCode != http.StatusTooManyRequests { //nolint:bodyclose // doStartPOST -> doJSON closes the body itself before returning.
		t.Fatalf("account A start #3 status = %d, want %d (its own budget is spent)", resp.StatusCode, http.StatusTooManyRequests)
	}

	if resp := doStartPOST(t, handler, auth.GoogleStartPath+"?purpose=link", sessB, rawB); resp.StatusCode != http.StatusOK { //nolint:bodyclose // doStartPOST -> doJSON closes the body itself before returning.
		t.Errorf("account B start status = %d, want %d -- account B shares account A's address but must not share its budget", resp.StatusCode, http.StatusOK)
	}
}

func TestStart_ReapsExpiredOAuthTransactions(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	pool := newRowInspectorPool(t)
	ctx := context.Background()

	expiredStore := auth.NewTransactionStoreForTest(q, func() time.Time { return time.Now().Add(-24 * time.Hour) })
	expiredHandle, _, err := expiredStore.Begin(ctx, auth.ProviderGoogle, auth.PurposeLogin, uuid.Nil, testPublicOrigin+auth.GoogleCallbackPath)
	if err != nil {
		t.Fatalf("Begin() expired transaction error = %v", err)
	}
	liveHandle, _, err := auth.NewTransactionStore(q).Begin(ctx, auth.ProviderGoogle, auth.PurposeLogin, uuid.Nil, testPublicOrigin+auth.GoogleCallbackPath)
	if err != nil {
		t.Fatalf("Begin() live transaction error = %v", err)
	}

	resp := doGet(t, handler, auth.GoogleStartPath) //nolint:bodyclose // doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("start status = %d, want %d", resp.StatusCode, http.StatusFound)
	}

	if _, count := rowState(ctx, t, pool, expiredHandle); count != 0 {
		t.Errorf("expired transaction row count after a start = %d, want 0 (the request path must reap it)", count)
	}
	if _, count := rowState(ctx, t, pool, liveHandle); count != 1 {
		t.Errorf("unexpired transaction row count after a start = %d, want 1 (the reaper must never touch a live transaction)", count)
	}
}

func TestStart_RejectionsAreLogged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		wantReason string
		exercise   func(t *testing.T, handler http.Handler, q *store.Queries)
	}{
		{
			name:       "link start with no session",
			wantReason: "start_session_required",
			exercise: func(t *testing.T, handler http.Handler, _ *store.Queries) {
				t.Helper()
				doJSON(t, handler, http.MethodPost, auth.GoogleStartPath+"?purpose=link", testPublicOrigin, "", "") //nolint:bodyclose // doJSON closes the body itself before returning.
			},
		},
		{
			name:       "link start failing the CSRF chain",
			wantReason: "start_csrf_rejected",
			exercise: func(t *testing.T, handler http.Handler, q *store.Queries) {
				t.Helper()
				raw, _ := issueTestSession(t, q, createTestUser(t, q))
				doJSON(t, handler, http.MethodPost, auth.GoogleStartPath+"?purpose=link", "https://evil.example", "token", "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
			},
		},
		{
			name:       "link start with a stale reauth window",
			wantReason: "start_reauth_required",
			exercise: func(t *testing.T, handler http.Handler, q *store.Queries) {
				t.Helper()
				raw, sess := issueTestSession(t, q, createTestUser(t, q))
				forceReauthenticatedAtStale(t, sess.ID)
				doStartPOST(t, handler, auth.GoogleStartPath+"?purpose=link", sess, raw) //nolint:bodyclose // doStartPOST -> doJSON closes the body itself before returning.
			},
		},
		{
			name:       "link purpose attempted over GET",
			wantReason: "start_method_not_allowed",
			exercise: func(t *testing.T, handler http.Handler, _ *store.Queries) {
				t.Helper()
				doGet(t, handler, auth.GoogleStartPath+"?purpose=link") //nolint:bodyclose // doGet closes the body itself before returning.
			},
		},
		{
			name:       "unsupported purpose over POST",
			wantReason: "start_purpose_unsupported",
			exercise: func(t *testing.T, handler http.Handler, q *store.Queries) {
				t.Helper()
				raw, sess := issueTestSession(t, q, createTestUser(t, q))
				doStartPOST(t, handler, auth.GoogleStartPath+"?purpose=login", sess, raw) //nolint:bodyclose // doStartPOST -> doJSON closes the body itself before returning.
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := oidctest.NewProvider(t)
			logger, logBuf := newCapturingLogger()
			handler, q := newTestService(t, withGoogleIssuer(p.URL), withLogger(logger))

			tc.exercise(t, handler, q)

			logged := logBuf.String()
			if !strings.Contains(logged, `"reason":"`+tc.wantReason+`"`) {
				t.Errorf("log record = %q, want a %q reason attribute", logged, tc.wantReason)
			}
			if !strings.Contains(logged, `"provider":"google"`) {
				t.Errorf("log record = %q, want a provider attribute -- one shared funnel serves all three providers", logged)
			}
		})
	}
}

func TestStart_RateLimitRejectionIsLogged(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	logger, logBuf := newCapturingLogger()
	handler, _ := newTestService(t, withGoogleIssuer(p.URL), withLogger(logger), withStartRateLimit(1, time.Minute))

	doGet(t, handler, auth.GoogleStartPath)         //nolint:bodyclose // doGet closes the body itself before returning.
	resp := doGet(t, handler, auth.GoogleStartPath) //nolint:bodyclose // doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("second start status = %d, want %d", resp.StatusCode, http.StatusTooManyRequests)
	}

	if logged := logBuf.String(); !strings.Contains(logged, `"reason":"start_rate_limited"`) {
		t.Errorf("log record = %q, want a start_rate_limited reason attribute", logged)
	}
}

func TestStartPOST_Link_EnrolledAccountNeedsFactorProof(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	userID := createTestUser(t, q)
	enrollForTest(t, q, userID)
	raw, sess := issueTestSession(t, q, userID)
	inspector := newRowInspectorPool(t)

	resp := doStartPOST(t, handler, auth.GoogleStartPath+"?purpose=link", sess, raw) //nolint:bodyclose // doStartPOST -> doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusForbidden || decodeErrorCode(t, resp) != "reauth_required" {
		t.Fatalf("enrolled link start without factor proof = %d, want 403 reauth_required", resp.StatusCode)
	}
	if n := oauthTransactionCountForLinkingUser(t.Context(), t, inspector, userID); n != 0 {
		t.Fatalf("rejected link start created %d transactions, want 0", n)
	}

	resp = doStartPOST(t, handler, auth.GoogleStartPath+"?purpose=reauth", sess, raw) //nolint:bodyclose // doStartPOST -> doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enrolled reauth start = %d, want 200", resp.StatusCode)
	}

	fresh := time.Now()
	setFactorProofForTest(t, sess.ID, &fresh)
	resp = doStartPOST(t, handler, auth.GoogleStartPath+"?purpose=link", sess, raw) //nolint:bodyclose // doStartPOST -> doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enrolled link start with both proofs = %d, want 200", resp.StatusCode)
	}
}
