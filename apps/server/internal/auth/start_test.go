package auth_test

// start_test.go covers P1.1 items 1 and 2 (docs/plans/phase-1-deferred.md)
// for the /api/v1/auth/{provider}/start surface:
//
//   - item 2: link/reauth start is a CSRF-protected POST returning the
//     authorize URL in the response envelope, replacing DD-C16's same-site
//     check on a GET. The plain login GET is untouched.
//   - item 1: auth-route rate limits (composite account+IP), opportunistic
//     reaping of expired oauth_transactions on the request path, and a
//     structured log record for every /start rejection.

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/auth/oidctest"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// startEnvelopeBody decodes POST /start's success envelope.
type startEnvelopeBody struct {
	Data struct {
		AuthorizeURL string `json:"authorizeUrl"`
	} `json:"data"`
}

// decodeStartEnvelope decodes resp's body as POST /start's success
// envelope, failing the test if it does not carry an authorize URL.
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

// doStartPOST issues the POST /start request a legitimate settings-page
// client would: exact Origin, the session's own CSRF token, no body.
func doStartPOST(t *testing.T, handler http.Handler, path string, sess store.Session, raw string) *http.Response {
	t.Helper()
	return doJSON(t, handler, http.MethodPost, path, testPublicOrigin, csrfTokenFor(sess), "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
}

// ============================================================================
// P1.1 item 2: link/reauth start is a CSRF-protected POST
// ============================================================================

// TestStartPOST_Link_ReturnsAuthorizeURL is item 2's happy path: an
// authenticated, CSRF-validated POST creates the link transaction and
// hands the caller the authorize URL to navigate to, instead of
// redirecting the browser itself.
func TestStartPOST_Link_ReturnsAuthorizeURL(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	pool := newRowInspectorPool(t)

	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID) // Issue sets reauthenticated_at = now, so the link gate passes.

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

// TestStartPOST_Reauth_NeedsNoRecentReauth proves purpose=reauth is still
// reachable from a session whose own reauth window has lapsed -- its
// entire point is to re-establish recency, so gating it on recency would
// be circular. DD-C16's same-site check was what made that safe on a GET;
// the CSRF chain is what makes it safe now.
func TestStartPOST_Reauth_NeedsNoRecentReauth(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID)
	forceReauthenticatedAtStale(t, sess.ID) // sessions_handlers_test.go: reauthenticated_at -> now-20m.

	resp := doStartPOST(t, handler, auth.GoogleStartPath+"?purpose=reauth", sess, raw) //nolint:bodyclose // doStartPOST -> doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d (purpose=reauth must not require a recent reauth)", resp.StatusCode, http.StatusOK)
	}
	decodeStartEnvelope(t, resp)
}

// TestStartPOST_Link_RejectsStaleReauth is DD-C11/DD-C17's recent-reauth
// gate, kept at /start (never deferred to /callback) so a stale caller
// never even creates a transaction row -- now answered with the JSON API's
// own 403 reauth_required rather than DD-C17's redirect, because a POST
// from the settings page is an API call, not a top-level navigation.
func TestStartPOST_Link_RejectsStaleReauth(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	pool := newRowInspectorPool(t)

	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID)
	forceReauthenticatedAtStale(t, sess.ID) // sessions_handlers_test.go: reauthenticated_at -> now-20m.

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

// TestStartPOST_RejectsWithoutCSRFOrSession is item 2's core claim: the
// link start now rides the existing CSRF chain, so every way a cross-site
// page could drive it -- no token, another session's token, a foreign
// Origin, no session at all -- is rejected by machinery already proven
// elsewhere, with no second authorization primitive of its own.
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

// TestStartPOST_RejectsUnsupportedPurpose pins the POST route's own closed
// purpose vocabulary: it exists for link and reauth, and nothing else. A
// login start is the GET's job, and silently treating an unrecognized
// value as one would return a login authorize URL to a caller that asked
// to link.
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

// TestStartGET_LinkAndReauthPurposesUnavailable proves the GET no longer
// serves link/reauth AT ALL -- for anyone, same-site or not. This is
// strictly stronger than DD-C16's same-site check it replaces: the
// same-origin row below is the exact request DD-C16 used to ADMIT, and it
// is now refused too, so the cross-site chain DD-C16 existed to break
// (forced reauth refresh, then forced link) cannot be assembled from GETs
// regardless of what headers a browser does or does not send.
func TestStartGET_LinkAndReauthPurposesUnavailable(t *testing.T) {
	t.Parallel()

	headerCases := []struct {
		name    string
		headers map[string]string
	}{
		{"same-origin (DD-C16 used to admit this)", map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": testPublicOrigin}},
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

// TestStartGET_LoginUnaffected is item 2's explicit carve-out: the plain
// login start stays exactly as it was -- a bare GET, reachable from
// anywhere (a bookmark, an email, another site's "continue with aboutme"
// button), with no session, CSRF, or same-site requirement of any kind.
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

// ============================================================================
// P1.1 item 1: rate limit, reaper, rejection logging
// ============================================================================

// TestStart_RateLimited proves the start routes carry their OWN limit,
// far tighter than the 300/min whole-server default they used to be
// bounded by alone -- an unauthenticated route that writes a database row
// per request needs one.
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

// TestStart_RateLimitKeyedOnAccountAndIP proves the limit is composite,
// not per-IP alone: two different accounts posting from the same address
// hold separate budgets, so one account exhausting its own can never lock
// another user out of linking.
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

// TestStart_ReapsExpiredOAuthTransactions proves the request-path reaper:
// an unauthenticated route that writes one row per request, with no
// scheduled cleanup job in existence yet (that is P8-priv's), must clear
// its own expired rows as it goes.
func TestStart_ReapsExpiredOAuthTransactions(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	pool := newRowInspectorPool(t)
	ctx := context.Background()

	// One long-expired transaction and one still well inside its TTL,
	// both created through the real TransactionStore with an injected
	// clock so they are exactly the rows /start itself would have left.
	expiredStore := auth.NewTransactionStoreForTest(q, func() time.Time { return time.Now().Add(-24 * time.Hour) })
	expiredHandle, _, err := expiredStore.Begin(ctx, auth.ProviderGoogle, auth.PurposeLogin, uuid.Nil, testPublicOrigin+auth.GoogleCallbackPath)
	if err != nil {
		t.Fatalf("Begin() expired transaction error = %v", err)
	}
	liveHandle, _, err := auth.NewTransactionStore(q).Begin(ctx, auth.ProviderGoogle, auth.PurposeLogin, uuid.Nil, testPublicOrigin+auth.GoogleCallbackPath)
	if err != nil {
		t.Fatalf("Begin() live transaction error = %v", err)
	}

	// No pre-check that the expired row is still present: this package's
	// tests run in parallel against one database, and a start from ANOTHER
	// test may legitimately reap it first -- that is the feature working,
	// not a broken setup. The assertion below stays sound either way,
	// because nothing else in the system deletes an oauth_transactions row:
	// if the reaper were broken, the row could only still be there.

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

// TestStart_RejectionsAreLogged closes the third half of item 1: every
// /start rejection now leaves a structured record naming the provider and
// a typed reason (reason.go's closed vocabulary). Before this, a /start
// rejection was invisible except as a bare status code in the access log
// -- an operator could see that link attempts were failing but never why.
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
				forceReauthenticatedAtStale(t, sess.ID)                                  // sessions_handlers_test.go: reauthenticated_at -> now-20m.
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

// TestStart_RateLimitRejectionIsLogged is the same obligation for the
// limiter's own rejection: a burst of starts against one key is exactly
// the signal an operator needs, and it must not be the one rejection
// class that stays silent.
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
