package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/auth/oidctest"
	"danny.vn/aboutme/apps/server/internal/store"
)

func ptrFalse() *bool {
	b := false
	return &b
}

func generateOtherRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating test RSA key: %v", err)
	}
	return key
}

func doGetCaptureBody(t *testing.T, handler http.Handler, path string, cookies ...*http.Cookie) (*http.Response, string) {
	t.Helper()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	resp := rec.Result()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("read response body: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Errorf("close response body: %v", err)
	}
	return resp, string(body)
}

func beginGoogle(t *testing.T, handler http.Handler) (txCookie *http.Cookie, state, nonce string) {
	t.Helper()

	start := doGet(t, handler, auth.GoogleStartPath) //nolint:bodyclose // doGet (handlers_test.go) closes the body itself before returning.
	if start.StatusCode != http.StatusFound {
		t.Fatalf("GET %s status = %d, want %d (a redirect to the provider)", auth.GoogleStartPath, start.StatusCode, http.StatusFound)
	}

	txCookie = extractCookie(start, auth.OAuthTxCookieName)
	if txCookie == nil {
		t.Fatalf("GET %s did not set the %s cookie", auth.GoogleStartPath, auth.OAuthTxCookieName)
	}

	loc := start.Header.Get("Location")
	state = mustQueryParam(t, loc, "state")
	nonce = mustQueryParam(t, loc, "nonce")

	return txCookie, state, nonce
}

func doCallback(t *testing.T, handler http.Handler, code, state string, cookies ...*http.Cookie) *http.Response {
	t.Helper()

	path := auth.GoogleCallbackPath + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
	return doGet(t, handler, path, cookies...) //nolint:bodyclose // doGet (handlers_test.go) closes the body itself before returning.
}

func assertRejected(t *testing.T, resp *http.Response) (errorCode string) {
	t.Helper()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want %d (every /callback rejection is a redirect, never a raw JSON error or a 200)", resp.StatusCode, http.StatusFound)
	}

	loc := resp.Header.Get("Location")
	errorCode = mustQueryParam(t, loc, "error")
	assertRedirectPath(t, loc, "/login")

	if sc := extractCookie(resp, auth.SessionCookieName); sc != nil {
		t.Errorf("response set a %s cookie on a rejected callback (value=%q), want none -- a rejected callback must never authenticate the visitor", auth.SessionCookieName, sc.Value)
	}

	tx := extractCookie(resp, auth.OAuthTxCookieName)
	if tx == nil {
		t.Fatalf("response did not clear the %s cookie on a rejected callback (no Set-Cookie for it at all) -- every failure path must call ClearOAuthTxCookie", auth.OAuthTxCookieName)
	}
	if tx.MaxAge >= 0 {
		t.Errorf("%s cookie MaxAge = %d on a rejected callback, want negative (cleared, matching auth.ClearOAuthTxCookie)", auth.OAuthTxCookieName, tx.MaxAge)
	}
	if tx.Value != "" {
		t.Errorf("%s cookie Value = %q on a rejected callback, want empty (cleared)", auth.OAuthTxCookieName, tx.Value)
	}

	return errorCode
}

func assertNoIdentity(t *testing.T, q *store.Queries, providerUserID string) {
	t.Helper()

	_, err := q.GetIdentityByProviderSubject(context.Background(), store.GetIdentityByProviderSubjectParams{
		Provider:       string(auth.ProviderGoogle),
		ProviderUserID: providerUserID,
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetIdentityByProviderSubject(google, %q) error = %v, want pgx.ErrNoRows (no identity row may be created on a rejected callback)", providerUserID, err)
	}
}

func assertNoUser(t *testing.T, q *store.Queries, email string) {
	t.Helper()

	_, err := q.GetUserByEmail(context.Background(), email)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetUserByEmail(%q) error = %v, want pgx.ErrNoRows (no user row may be created on a rejected callback)", email, err)
	}
}

func uniqueSubject(t *testing.T) string {
	t.Helper()
	return "g-sub-" + uuid.NewString()
}

func uniqueEmail(t *testing.T) string {
	t.Helper()
	return uuid.NewString() + "@example.com"
}

func TestGoogleCallback_RejectsUnverifiedEmail(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginGoogle(t, handler)
	p.RegisterCode("code-unverified", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrFalse(),
		Nonce:         nonce,
	})

	resp := doCallback(t, handler, "code-unverified", state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	errorCode := assertRejected(t, resp)
	if errorCode != "email_not_verified" {
		t.Errorf("error code = %q, want %q (design spec §3: Google requires email_verified == true)", errorCode, "email_not_verified")
	}

	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

func TestGoogleCallback_RejectsWrongIssuer(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginGoogle(t, handler)
	p.RegisterCode("code-wrong-issuer", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
		Issuer:        "https://evil.example",
	})

	resp := doCallback(t, handler, "code-wrong-issuer", state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

func TestGoogleCallback_RejectsWrongAudience(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginGoogle(t, handler)
	p.RegisterCode("code-wrong-audience", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
		Audience:      "some-other-client-id",
	})

	resp := doCallback(t, handler, "code-wrong-audience", state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

func TestGoogleCallback_RejectsTamperedSignature(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginGoogle(t, handler)
	p.RegisterCode("code-tampered-signature", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
		SigningKey:    generateOtherRSAKey(t),
	})

	resp := doCallback(t, handler, "code-tampered-signature", state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

func TestGoogleCallback_RejectsNonceMismatch(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, state, _ := beginGoogle(t, handler)
	p.RegisterCode("code-nonce-mismatch", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         "wrong-nonce-does-not-match-the-real-transaction",
	})

	resp := doCallback(t, handler, "code-nonce-mismatch", state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

func TestGoogleCallback_RejectsMissingNonce(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, state, _ := beginGoogle(t, handler)
	p.RegisterCode("code-nonce-missing", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
	})

	resp := doCallback(t, handler, "code-nonce-missing", state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

func TestGoogleCallback_RejectsExpiredIDToken(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, state, nonce := beginGoogle(t, handler)
	p.RegisterCode("code-expired", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
		ExpiresAt:     time.Now().Add(-1 * time.Hour),
	})

	resp := doCallback(t, handler, "code-expired", state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

func TestGoogleCallback_RejectsStateMismatch(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	txCookie, _, nonce := beginGoogle(t, handler)
	p.RegisterCode("code-state-mismatch", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
	})

	resp := doCallback(t, handler, "code-state-mismatch", "attacker-supplied-wrong-state", txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

func TestGoogleCallback_RejectsMissingTxCookie(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	_, state, nonce := beginGoogle(t, handler)
	p.RegisterCode("code-missing-cookie", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
	})

	resp := doCallback(t, handler, "code-missing-cookie", state) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.

	assertRejected(t, resp)
	assertNoIdentity(t, q, subject)
	assertNoUser(t, q, email)
}

func TestGoogleCallback_OIDCFailures_NoOracleAcrossFailureModes(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL))

	otherKey := generateOtherRSAKey(t)

	cases := map[string]func(nonce string) oidctest.Claims{
		"wrong issuer": func(nonce string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: nonce, Issuer: "https://evil.example",
			}
		},
		"wrong audience": func(nonce string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: nonce, Audience: "some-other-client-id",
			}
		},
		"tampered signature": func(nonce string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: nonce, SigningKey: otherKey,
			}
		},
		"nonce mismatch": func(string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: "wrong-nonce-does-not-match-the-real-transaction",
			}
		},
		"expired id token": func(nonce string) oidctest.Claims {
			return oidctest.Claims{
				Subject: uniqueSubject(t), Email: uniqueEmail(t),
				EmailVerified: ptrTrue(), Nonce: nonce, ExpiresAt: time.Now().Add(-1 * time.Hour),
			}
		},
	}

	errorCodes := make(map[string]string, len(cases))
	bodies := make(map[string]string, len(cases))
	for name, buildClaims := range cases {
		code := "code-oracle-" + name

		txCookie, state, nonce := beginGoogle(t, handler)
		p.RegisterCode(code, buildClaims(nonce))

		path := auth.GoogleCallbackPath + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
		resp, body := doGetCaptureBody(t, handler, path, txCookie) //nolint:bodyclose // doGetCaptureBody closes the body itself before returning.

		errorCodes[name] = assertRejected(t, resp)
		bodies[name] = body
	}

	var firstName, firstCode, firstBody string
	for name, code := range errorCodes {
		if firstName == "" {
			firstName, firstCode, firstBody = name, code, bodies[name]
			continue
		}
		if code != firstCode {
			t.Errorf("error code for %q = %q, want %q (same as %q) -- distinct OIDC failure classes must not be distinguishable to the browser", name, code, firstCode, firstName)
		}
		if bodies[name] != firstBody {
			t.Errorf("response body for %q differs from %q's -- distinct OIDC failure classes must not produce distinguishable response bodies", name, firstName)
		}
	}

	if firstCode == "email_not_verified" {
		t.Error("OIDC verification failures reused the email_not_verified error code, want a distinct generic code -- these five are cryptographic/protocol failures, not the email_verified claim check, and conflating them would misreport the failure to the visitor")
	}

	forbidden := []string{"issuer", "audience", "signature", "nonce", "expired", "expire"}
	for name, body := range bodies {
		lower := strings.ToLower(body)
		for _, word := range forbidden {
			if strings.Contains(lower, word) {
				t.Errorf("%s: response body contains %q, want no OIDC-verification internals leaked to the browser (body=%q)", name, word, body)
			}
		}
	}
}

func createGoogleIdentityForTest(t *testing.T, q *store.Queries, userID uuid.UUID) string {
	t.Helper()
	subject := uniqueSubject(t)
	if _, err := q.CreateIdentity(t.Context(), store.CreateIdentityParams{
		UserID: userID, Provider: string(auth.ProviderGoogle), ProviderUserID: subject,
	}); err != nil {
		t.Fatalf("CreateIdentity() error = %v", err)
	}
	return subject
}

func registerGoogleCode(t *testing.T, p *oidctest.Provider, subject, nonce string) string {
	t.Helper()
	code := "code-enrolled-" + uuid.NewString()
	p.RegisterCode(code, oidctest.Claims{Subject: subject, Email: uniqueEmail(t), EmailVerified: ptrTrue(), Nonce: nonce})
	return code
}

func serveGetAsync(ctx context.Context, handler http.Handler, path string, cookies ...*http.Cookie) <-chan *httptest.ResponseRecorder {
	done := make(chan *httptest.ResponseRecorder, 1)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	go func() {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		done <- rec
	}()
	return done
}

func TestGoogleCallback_EnrolledLogin_PendingNotSession(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	userID := createTestUser(t, q)
	subject := createGoogleIdentityForTest(t, q, userID)
	enrollForTest(t, q, userID)

	start := doGet(t, handler, auth.GoogleStartPath+"?next="+url.QueryEscape("/app/settings")) //nolint:bodyclose // doGet closes the body itself before returning.
	txCookie := extractCookie(start, auth.OAuthTxCookieName)
	if start.StatusCode != http.StatusFound || txCookie == nil {
		t.Fatalf("start = %d, tx cookie %v", start.StatusCode, txCookie)
	}
	loc := start.Header.Get("Location")
	state, nonce := mustQueryParam(t, loc, "state"), mustQueryParam(t, loc, "nonce")
	code := registerGoogleCode(t, p, subject, nonce)

	resp := doCallback(t, handler, code, state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.
	raw := assertPendingRedirect(t, resp)
	pending := pendingForCookie(t, q, raw)
	if pending.UserID != userID || pending.Purpose != auth.PendingAuthenticationPurposeLogin || pending.SessionID != nil {
		t.Errorf("pending row = %+v, want an unbound login row for the account", pending)
	}
	if pending.ReturnPath != "/app/settings" {
		t.Errorf("pending return path = %q, want the transaction's validated path", pending.ReturnPath)
	}
	if n := unrevokedSessionCount(t, userID); n != 0 {
		t.Errorf("enrolled login created %d sessions, want 0", n)
	}

	replay := doCallback(t, handler, code, state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.
	if got := assertRejected(t, replay); got != "auth_failed" {
		t.Errorf("replayed callback error = %q, want auth_failed", got)
	}
	if extractCookie(replay, pendingCookieName) != nil {
		t.Error("replayed callback set a pending cookie")
	}
	if n := pendingCountForUser(t, userID); n != 1 {
		t.Errorf("pending rows after replay = %d, want 1", n)
	}
}

func TestGoogleCallback_UnenrolledLogin_ClearsPendingCookie(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	userID := createTestUser(t, q)
	subject := createGoogleIdentityForTest(t, q, userID)

	txCookie, state, nonce := beginGoogle(t, handler)
	code := registerGoogleCode(t, p, subject, nonce)
	resp := doCallback(t, handler, code, state, txCookie, requestCookie(pendingCookieName, "stale-pending")) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound || extractCookie(resp, auth.SessionCookieName) == nil {
		t.Fatalf("unenrolled login = %d without a session cookie", resp.StatusCode)
	}
	if cleared := extractCookie(resp, pendingCookieName); cleared == nil || cleared.MaxAge >= 0 {
		t.Errorf("unenrolled login pending cookie = %+v, want cleared", cleared)
	}
	if n := pendingCountForUser(t, userID); n != 0 {
		t.Errorf("unenrolled login created %d pending rows, want 0", n)
	}
}

func TestGoogleCallback_EnrolledReauth_BindsSessionWithoutProofUpdate(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	userID := createTestUser(t, q)
	subject := createGoogleIdentityForTest(t, q, userID)
	enrollForTest(t, q, userID)
	raw, sess := issueTestSession(t, q, userID)
	inspector := newRowInspectorPool(t)
	before := sessionRowReauthenticatedAt(t.Context(), t, inspector, sess.ID)

	txCookie, tx := beginGoogleTransaction(t, q, auth.PurposeReauth, userID)
	code := registerGoogleCode(t, p, subject, tx.Nonce)
	resp := doCallback(t, handler, code, tx.State, txCookie, sessionRequestCookie(raw)) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.
	pendingRaw := assertPendingRedirect(t, resp)

	pending := pendingForCookie(t, q, pendingRaw)
	if pending.Purpose != auth.PendingAuthenticationPurposeReauth || pending.SessionID == nil || *pending.SessionID != sess.ID {
		t.Errorf("pending row = %+v, want reauth bound to session %s", pending, sess.ID)
	}
	if pending.ReturnPath != wantSettingsSessionsPath {
		t.Errorf("pending return path = %q, want %q", pending.ReturnPath, wantSettingsSessionsPath)
	}
	if after := sessionRowReauthenticatedAt(t.Context(), t, inspector, sess.ID); !after.Equal(before) {
		t.Errorf("reauthenticated_at changed before factor completion: %v -> %v", before, after)
	}
	row, err := q.GetSessionByID(t.Context(), sess.ID)
	if err != nil {
		t.Fatalf("GetSessionByID() error = %v", err)
	}
	if row.SecondFactorVerifiedAt != nil || row.RevokedAt != nil {
		t.Errorf("session after enrolled reauth = %+v, want unchanged and live", row)
	}
}

func TestGoogleCallback_EnrolledLink_NeedsFactorProof(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	userID := createTestUser(t, q)
	enrollForTest(t, q, userID)
	raw, sess := issueTestSession(t, q, userID)

	subject := uniqueSubject(t)
	txCookie, tx := beginGoogleTransaction(t, q, auth.PurposeLink, userID)
	resp := doCallback(t, handler, registerGoogleCode(t, p, subject, tx.Nonce), tx.State, txCookie, sessionRequestCookie(raw)) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound || mustQueryParam(t, resp.Header.Get("Location"), "error") != "auth_failed" {
		t.Fatalf("enrolled link without factor proof = %d %q, want auth_failed", resp.StatusCode, resp.Header.Get("Location"))
	}
	assertNoIdentity(t, q, subject)

	fresh := time.Now()
	setFactorProofForTest(t, sess.ID, &fresh)
	txCookie, tx = beginGoogleTransaction(t, q, auth.PurposeLink, userID)
	resp = doCallback(t, handler, registerGoogleCode(t, p, subject, tx.Nonce), tx.State, txCookie, sessionRequestCookie(raw)) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("enrolled link with both proofs = %d, want 302", resp.StatusCode)
	}
	assertRedirectPath(t, resp.Header.Get("Location"), wantSettingsSessionsPath)
	identity, err := q.GetIdentityByProviderSubject(t.Context(), store.GetIdentityByProviderSubjectParams{Provider: string(auth.ProviderGoogle), ProviderUserID: subject})
	if err != nil || identity.UserID != userID {
		t.Fatalf("identity after authorized link = %+v, %v; want owned by the account", identity, err)
	}
}

func TestGoogleCallback_LinkRechecksEpochUnderUserLock(t *testing.T) {
	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	pool := newTestPool(t)
	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID)
	subject := uniqueSubject(t)
	txCookie, tx := beginGoogleTransaction(t, q, auth.PurposeLink, userID)
	code := registerGoogleCode(t, p, subject, tx.Nonce)

	holder, qtx, pid := beginUserLockHolder(t, pool, userID)
	if _, err := qtx.AdvanceUserAuthEpoch(t.Context(), userID); err != nil {
		t.Fatalf("advance epoch: %v", err)
	}
	now := time.Now()
	if _, err := auth.NewSessionManagerWithPool(pool).ReplaceAfterEpochChangeTx(t.Context(), qtx, userID, sess.ID, &now); err != nil {
		t.Fatalf("ReplaceAfterEpochChangeTx() error = %v", err)
	}

	path := auth.GoogleCallbackPath + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(tx.State)
	done := serveGetAsync(t.Context(), handler, path, txCookie, sessionRequestCookie(raw))
	waitBlockedBy(t, pid)
	if err := holder.Commit(t.Context()); err != nil {
		t.Fatalf("commit epoch change: %v", err)
	}
	rec := awaitRecorder(t, done)

	if rec.Code != http.StatusFound {
		t.Fatalf("link callback status = %d, want 302", rec.Code)
	}
	if got := mustQueryParam(t, rec.Header().Get("Location"), "error"); got != "auth_failed" {
		t.Errorf("link across an epoch change error = %q, want auth_failed", got)
	}
	assertNoIdentity(t, q, subject)
}
