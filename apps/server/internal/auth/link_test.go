package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/auth/oidctest"
	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

func doGetWithHeaders(t *testing.T, handler http.Handler, path string, headers map[string]string, cookies ...*http.Cookie) *http.Response {
	t.Helper()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	resp := rec.Result()
	if err := resp.Body.Close(); err != nil {
		t.Errorf("close response body: %v", err)
	}
	return resp
}

func TestStart_PurposeLogin_UnaffectedBySameSiteEnforcement(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL))

	resp := doGetWithHeaders(t, handler, auth.GoogleStartPath, map[string]string{ //nolint:bodyclose // doGetWithHeaders closes the body itself before returning.
		"Sec-Fetch-Site": "cross-site",
		"Origin":         "https://evil.example",
	})
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("purpose=login start with cross-site signals status = %d, want %d (cross-site start checks must not apply to purpose=login)", resp.StatusCode, http.StatusFound)
	}
	if txc := extractCookie(resp, auth.OAuthTxCookieName); txc == nil {
		t.Error("purpose=login start did not set __Host-oauth-tx despite succeeding")
	}
}

func TestPurposeReauth_RejectsUnclaimedIdentity_NoIdentityCreatedNoReauthBump(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	pool := newRowInspectorPool(t)
	ctx := context.Background()

	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID)
	before := sessionRowReauthenticatedAt(ctx, t, pool, sess.ID)

	txCookie, tx := beginGoogleTransaction(t, q, auth.PurposeReauth, userID)

	subject := uniqueSubject(t)
	code := "code-reauth-unclaimed-" + uuid.NewString()
	p.RegisterCode(code, oidctest.Claims{
		Subject:       subject,
		Email:         uniqueEmail(t),
		EmailVerified: ptrTrue(),
		Nonce:         tx.Nonce,
	})

	resp := doGet(t, handler, auth.GoogleCallbackPath+"?code="+code+"&state="+tx.State, txCookie, sessionRequestCookie(raw)) //nolint:bodyclose // doGet closes the body itself before returning.

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	loc := resp.Header.Get("Location")
	if got := mustQueryParam(t, loc, "error"); got != "auth_failed" {
		t.Errorf("error param = %q, want %q (no auto-link via reauth -- the generic, no-oracle rejection, not identity_already_linked -- nobody else claims this identity either)", got, "auth_failed")
	}
	assertRedirectPath(t, loc, wantSettingsSessionsPath)
	if sc := extractCookie(resp, auth.SessionCookieName); sc != nil && sc.Value != raw {
		t.Errorf("response set a NEW %s cookie (value=%q) distinct from the caller's own, want none or the same rotated value only", auth.SessionCookieName, sc.Value)
	}

	assertNoIdentityForProvider(t, q, auth.ProviderGoogle, subject)

	after := sessionRowReauthenticatedAt(ctx, t, pool, sess.ID)
	if !after.Equal(before) {
		t.Errorf("sessions.reauthenticated_at changed after a purpose=reauth attempt against an UNCLAIMED identity (before=%v after=%v), want unchanged -- reauth must never silently link", before, after)
	}
}

func TestGitHubCallback_LinkPurpose_AttachesUnclaimedIdentityToLinkingUser(t *testing.T) {
	t.Parallel()

	gi := newGitHubIdentity(t, uniqueEmail(t))
	handler, q := newTestService(t, withGitHubEndpoint(gi.stub.URL))
	ctx := context.Background()

	linkingUserID := createTestUser(t, q)
	raw, _ := issueTestSession(t, q, linkingUserID)
	txStore := auth.NewTransactionStore(q)
	handle, tx, err := txStore.Begin(ctx, auth.ProviderGitHub, auth.PurposeLink, linkingUserID, testPublicOrigin+auth.GitHubCallbackPath)
	if err != nil {
		t.Fatalf("TransactionStore.Begin() error = %v", err)
	}
	txCookie := requestCookie(auth.OAuthTxCookieName, handle)

	resp := doGitHubCallback(t, handler, gi.code, tx.State, txCookie, sessionRequestCookie(raw)) //nolint:bodyclose // doGitHubCallback -> doGet closes the body itself before returning.

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d (redirect on a successful link)", resp.StatusCode, http.StatusFound)
	}
	wantLocation := testPublicOrigin + wantSettingsSessionsPath
	if got := resp.Header.Get("Location"); got != wantLocation {
		t.Errorf("callback Location = %q, want %q", got, wantLocation)
	}
	if sc := extractCookie(resp, auth.SessionCookieName); sc != nil {
		t.Errorf("callback set a %s cookie (value=%q) on a link success, want none -- the caller already has one", auth.SessionCookieName, sc.Value)
	}

	identity, err := q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
		Provider:       string(auth.ProviderGitHub),
		ProviderUserID: gi.subject,
	})
	if err != nil {
		t.Fatalf("GetIdentityByProviderSubject(github, %q) error = %v, want a created identity row", gi.subject, err)
	}
	if identity.UserID != linkingUserID {
		t.Errorf("identity.UserID = %v, want %v (the linking user -- a link flow must never create a new user)", identity.UserID, linkingUserID)
	}
}

func TestStartPOST_PurposeLinkOrReauth_NoSession_401NothingAnnounced(t *testing.T) {
	t.Parallel()

	for _, purpose := range []auth.Purpose{auth.PurposeLink, auth.PurposeReauth} {
		t.Run(string(purpose), func(t *testing.T) {
			t.Parallel()

			p := oidctest.NewProvider(t)
			handler, _ := newTestService(t, withGoogleIssuer(p.URL))

			resp := doJSON(t, handler, http.MethodPost, auth.GoogleStartPath+"?purpose="+string(purpose), //nolint:bodyclose // doJSON closes the body itself before returning.
				testPublicOrigin, "any-token", "")

			if resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d (RequireSession's own rejection)", resp.StatusCode, http.StatusUnauthorized)
			}
			if got := resp.Header.Get("Location"); got != "" {
				t.Errorf("Location = %q, want none -- a rejected start must not hand the caller a URL that announces the attempt", got)
			}
			if got := decodeErrorCode(t, resp); got != "session_required" {
				t.Errorf("error.code = %q, want %q", got, "session_required")
			}
			if sc := extractCookie(resp, auth.SessionCookieName); sc == nil {
				t.Error("response did not clear the __Host-session cookie on a missing/invalid-session rejection, want it cleared (RequireSession's own hygiene)")
			} else if sc.MaxAge >= 0 || sc.Value != "" {
				t.Errorf("__Host-session cookie = %+v, want cleared (empty value, negative Max-Age)", sc)
			}
			if tx := extractCookie(resp, auth.OAuthTxCookieName); tx != nil && tx.MaxAge >= 0 {
				t.Error("a LIVE __Host-oauth-tx cookie was set despite the missing-session rejection, want none -- no transaction row may exist before this gate passes")
			}
		})
	}
}

func TestGoogleCallback_LinkPurpose_RejectedAfterLogoutEverywhere(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	ctx := context.Background()

	linkingUserID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, linkingUserID)

	txCookie, tx := beginGoogleTransaction(t, q, auth.PurposeLink, linkingUserID)

	revokeResp := doJSON(t, handler, http.MethodDelete, auth.SessionsPath, testPublicOrigin, csrfTokenFor(sess), "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if revokeResp.StatusCode != http.StatusNoContent {
		t.Fatalf("DELETE %s status = %d, want %d (setup: logout-everywhere must itself succeed)", auth.SessionsPath, revokeResp.StatusCode, http.StatusNoContent)
	}

	subject := uniqueSubject(t)
	code := "code-link-after-logout-everywhere-" + uuid.NewString()
	p.RegisterCode(code, oidctest.Claims{
		Subject:       subject,
		Email:         uniqueEmail(t),
		EmailVerified: ptrTrue(),
		Nonce:         tx.Nonce,
	})

	resp := doGet(t, handler, auth.GoogleCallbackPath+"?code="+code+"&state="+tx.State, txCookie, sessionRequestCookie(raw)) //nolint:bodyclose // doGet closes the body itself before returning.

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	loc := resp.Header.Get("Location")
	if got := mustQueryParam(t, loc, "error"); got != "auth_failed" {
		t.Errorf("error param = %q, want %q (generic no-oracle rejection -- the completing request's session no longer authenticates)", got, "auth_failed")
	}
	assertRedirectPath(t, loc, wantSettingsSessionsPath)

	if sc := extractCookie(resp, auth.SessionCookieName); sc != nil {
		t.Errorf("response set a %s cookie (value=%q) on a rejected post-logout-everywhere link attempt, want none", auth.SessionCookieName, sc.Value)
	}

	assertNoIdentityForProvider(t, q, auth.ProviderGoogle, subject)

	sm := auth.NewSessionManager(q)
	if _, _, err := sm.Authenticate(ctx, raw); err == nil {
		t.Error("the revoked session token still authenticates after the rejected link attempt, want it to stay dead")
	}
}

func TestGoogleCallback_LinkOrReauth_RotatedRequest_MismatchStillCarriesSuccessorCookie(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	pool := newTestPool(t)
	q := store.New(pool)
	svc, err := auth.NewServiceForTest(testLogger(), config.Config{
		PublicOrigin:       testPublicOrigin,
		ProviderLogin:      config.ProviderLogin{Google: true, GitHub: true, LinkedIn: true},
		GoogleClientID:     oidctest.DefaultClientID,
		GoogleClientSecret: "test-google-client-secret",
	}, pool, p.URL, "", "")
	if err != nil {
		t.Fatalf("NewServiceForTest() error = %v", err)
	}
	clk := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerForTest(q, clk.Now)
	auth.SetSessionManagerForTest(svc, sm)
	handler := api.New(testLogger(), noopPinger{}, api.Options{}, nil, svc.RegisterRoutes)
	ctx := context.Background()

	linkingUserID := createTestUser(t, q)
	currentBrowserUserID := createTestUser(t, q)
	rawOld, _, err := sm.Issue(ctx, currentBrowserUserID, "ua", "203.0.113.44")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	clk.Advance(25 * time.Hour)

	txStore := auth.NewTransactionStore(q)
	handle, tx, err := txStore.Begin(ctx, auth.ProviderGoogle, auth.PurposeLink, linkingUserID, testPublicOrigin+auth.GoogleCallbackPath)
	if err != nil {
		t.Fatalf("TransactionStore.Begin() error = %v", err)
	}
	txCookie := requestCookie(auth.OAuthTxCookieName, handle)

	subject := uniqueSubject(t)
	code := "code-link-rotate-mismatch-" + uuid.NewString()
	p.RegisterCode(code, oidctest.Claims{
		Subject:       subject,
		Email:         uniqueEmail(t),
		EmailVerified: ptrTrue(),
		Nonce:         tx.Nonce,
	})

	resp := doGet(t, handler, auth.GoogleCallbackPath+"?code="+code+"&state="+tx.State, txCookie, sessionRequestCookie(rawOld)) //nolint:bodyclose // doGet closes the body itself before returning.

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	loc := resp.Header.Get("Location")
	if got := mustQueryParam(t, loc, "error"); got != "auth_failed" {
		t.Errorf("error param = %q, want %q (the completing session authenticates a DIFFERENT user than tx.LinkingUserID)", got, "auth_failed")
	}

	successor := extractCookie(resp, auth.SessionCookieName)
	if successor == nil {
		t.Fatal("a rejected link callback with a >24h-old session did not carry a successor Set-Cookie -- the rotation write already happened in the database and must not be silently discarded on a mismatch")
	}
	if successor.Value == "" || successor.Value == rawOld {
		t.Errorf("successor cookie value = %q, want a new, non-empty token distinct from the original", successor.Value)
	}
	if !successor.Secure || !successor.HttpOnly {
		t.Errorf("successor cookie Secure=%v HttpOnly=%v, want both true", successor.Secure, successor.HttpOnly)
	}

	again, _, err := sm.Authenticate(ctx, successor.Value)
	if err != nil {
		t.Fatalf("Authenticate(successor) error = %v, want the browser's rotated credential to keep working", err)
	}
	if again.UserID != currentBrowserUserID {
		t.Errorf("successor session UserID = %v, want %v", again.UserID, currentBrowserUserID)
	}

	assertNoIdentityForProvider(t, q, auth.ProviderGoogle, subject)
}
