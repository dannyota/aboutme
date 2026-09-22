package auth_test

// Password-service adversarial cases: token expiry/replacement boundaries,
// rehash CAS, corrupt-hash no-oracle behavior, breach unavailability, the login
// failure limiter, and secret-free diagnostics.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/password"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// failingBreachChecker simulates an unreachable HIBP endpoint.
type failingBreachChecker struct{}

func (failingBreachChecker) Breached(context.Context, string) (bool, error) {
	return false, errors.New("hibp unavailable")
}

func TestPasswordVerify_ExpiredRegistration(t *testing.T) {
	e := newPasswordEnv(t)
	token, _ := e.createRegistration(t, newEmail(), "Ada", testPassword)

	e.clk.Advance(25 * time.Hour)
	resp, body := e.request(t, http.MethodPost, auth.PasswordVerifyPath, jsonBody(t, map[string]string{"token": token.Raw})) //nolint:bodyclose // request closes the body itself before returning.
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", resp.StatusCode, body)
	}
	assertErrorCode(t, body, "credential_token_invalid")
}

func TestPasswordReset_ExpiredToken(t *testing.T) {
	e := newPasswordEnv(t)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	token := e.createResetToken(t, userID)

	e.clk.Advance(31 * time.Minute)
	resp, body := e.request(t, http.MethodPost, auth.PasswordResetPath, jsonBody(t, map[string]string{ //nolint:bodyclose // request closes the body itself before returning.
		"token": token.Raw, "password": "a fresh password 123",
	}))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", resp.StatusCode, body)
	}
	assertErrorCode(t, body, "credential_token_invalid")
}

func TestPasswordRegister_ReplacesPriorRegistration(t *testing.T) {
	e := newPasswordEnv(t)
	email := newEmail()

	for i := 0; i < 2; i++ {
		resp, body := e.request(t, http.MethodPost, auth.PasswordRegisterPath, jsonBody(t, map[string]string{ //nolint:bodyclose // request closes the body itself before returning.
			"name": "Ada", "email": email, "password": testPassword,
		}))
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("register #%d status = %d, want 202 (body=%s)", i, resp.StatusCode, body)
		}
	}

	// Exactly one registration and one job remain: the second replaced the first.
	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM password_registrations WHERE email = $1`, email).Scan(&n); err != nil {
		t.Fatalf("count registrations: %v", err)
	}
	if n != 1 {
		t.Errorf("registrations for %q = %d, want 1 (replacement)", email, n)
	}
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM auth_email_jobs j JOIN password_registrations r ON j.registration_id = r.id WHERE r.email = $1`, email).Scan(&n); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	if n != 1 {
		t.Errorf("verify jobs for %q = %d, want 1", email, n)
	}
}

func TestPasswordForgot_ReplacesPriorResetToken(t *testing.T) {
	e := newPasswordEnv(t)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	email := e.userEmail(t, userID)

	for i := 0; i < 2; i++ {
		resp, _ := e.request(t, http.MethodPost, auth.PasswordForgotPath, jsonBody(t, map[string]string{"email": email})) //nolint:bodyclose // request closes the body itself before returning.
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("forgot #%d status = %d, want 202", i, resp.StatusCode)
		}
	}

	var n int
	if err := e.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM password_reset_tokens WHERE user_id = $1`, userID).Scan(&n); err != nil {
		t.Fatalf("count reset tokens: %v", err)
	}
	if n != 1 {
		t.Errorf("reset tokens = %d, want 1 (replacement)", n)
	}
}

func TestPasswordLogin_RehashCAS(t *testing.T) {
	e := newPasswordEnv(t)
	userID := e.createUser(t)

	// Store a weaker encoding; the service's hasher uses memory=64.
	weak, err := password.NewHasher(password.HashPolicy{
		Version: 19, MemoryKiB: 32, Iterations: 1, Parallelism: 1, SaltLen: 16, KeyLen: 32,
	}, rand.Reader, password.NewAdmission())
	if err != nil {
		t.Fatalf("NewHasher(weak): %v", err)
	}
	weakEnc, err := weak.Hash(context.Background(), testPassword)
	if err != nil {
		t.Fatalf("weak hash: %v", err)
	}
	now := e.clk.Now()
	if _, upsertErr := e.q.UpsertPasswordCredential(context.Background(), store.UpsertPasswordCredentialParams{
		UserID: userID, EncodedHash: []byte(weakEnc), CreatedAt: now, ChangedAt: now,
	}); upsertErr != nil {
		t.Fatalf("upsert weak credential: %v", upsertErr)
	}

	email := e.userEmail(t, userID)
	if _, loginErr := e.svc.LoginForTest(context.Background(), email, testPassword, "ua", raceIP); loginErr != nil {
		t.Fatalf("login error = %v", loginErr)
	}

	cred, err := e.q.GetPasswordCredential(context.Background(), userID)
	if err != nil {
		t.Fatalf("get credential: %v", err)
	}
	if !strings.Contains(string(cred.EncodedHash), "m=64") {
		t.Errorf("credential not rehashed to the strong policy: %q", cred.EncodedHash)
	}
	if strings.Contains(string(cred.EncodedHash), "m=32") {
		t.Errorf("credential still weak after login: %q", cred.EncodedHash)
	}
}

func TestPasswordLogin_CorruptHash_NoOracle(t *testing.T) {
	e := newPasswordEnv(t)
	userID := e.createUser(t)

	now := e.clk.Now()
	if _, err := e.q.UpsertPasswordCredential(context.Background(), store.UpsertPasswordCredentialParams{
		UserID: userID, EncodedHash: []byte("not-a-valid-phc"), CreatedAt: now, ChangedAt: now,
	}); err != nil {
		t.Fatalf("upsert corrupt credential: %v", err)
	}

	email := e.userEmail(t, userID)
	resp, body := e.request(t, http.MethodPost, auth.PasswordLoginPath, jsonBody(t, map[string]string{ //nolint:bodyclose // request closes the body itself before returning.
		"email": email, "password": testPassword,
	}))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body=%s)", resp.StatusCode, body)
	}
	assertErrorCode(t, body, "authentication_failed")
}

func TestPasswordRegister_HIBPUnavailable(t *testing.T) {
	pool := newTestPool(t)
	q := store.New(pool)
	clk := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerWithPoolForTest(pool, clk.Now)
	hasher, err := password.NewHasher(fastHashPolicy(), rand.Reader, password.NewAdmission())
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}
	policy := password.NewPolicy(nil, failingBreachChecker{})
	outbox := newTestOutbox(t, clk, rand.Reader)

	svc, err := newPasswordServiceWith(pool, q, sm, policy, hasher, outbox, clk)
	if err != nil {
		t.Fatalf("NewPasswordService: %v", err)
	}
	handler := api.New(testLogger(), noopPinger{}, api.Options{Clock: clk.Now}, nil, svc.RegisterRoutes)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, auth.PasswordRegisterPath, strings.NewReader(jsonBody(t, map[string]string{
		"name": "Ada", "email": newEmail(), "password": testPassword,
	})))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", testPublicOrigin)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	resp := rec.Result()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (body=%s)", resp.StatusCode, raw)
	}
	assertErrorCode(t, raw, "authentication_unavailable")
}

// newPasswordServiceWith builds a service with a caller-supplied policy, used by
// the breach-unavailability test. It mirrors newPasswordServiceWithOutbox.
func newPasswordServiceWith(pool *store.Pool, q *store.Queries, sm *auth.SessionManager, policy *password.Policy, hasher *password.Hasher, outbox *authmail.Outbox, clk *testutil.Clock) (*auth.PasswordService, error) {
	var emailKey [32]byte
	for i := range emailKey {
		emailKey[i] = 0x01
	}
	limits, err := auth.NewPasswordRatePolicies(emailKey)
	if err != nil {
		return nil, err
	}
	return auth.NewPasswordService(auth.PasswordServiceOptions{
		Pool: pool, Queries: q, Sessions: sm, Policy: policy, Hasher: hasher,
		Outbox: outbox, Limits: limits, PublicOrigin: testPublicOrigin,
		Clock: clk.Now, Entropy: rand.Reader,
	})
}

func TestPasswordLogin_FailureLimiter429(t *testing.T) {
	e := newPasswordEnv(t)
	email := newEmail()

	// Failures 1..9 return the identical 401; the tenth returns 429.
	for i := 0; i < 9; i++ {
		resp, body := e.request(t, http.MethodPost, auth.PasswordLoginPath, jsonBody(t, map[string]string{ //nolint:bodyclose // request closes the body itself before returning.
			"email": email, "password": testPassword,
		}))
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("failure #%d status = %d, want 401 (body=%s)", i, resp.StatusCode, body)
		}
		assertErrorCode(t, body, "authentication_failed")
	}
	resp, body := e.request(t, http.MethodPost, auth.PasswordLoginPath, jsonBody(t, map[string]string{ //nolint:bodyclose // request closes the body itself before returning.
		"email": email, "password": testPassword,
	}))
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("tenth failure status = %d, want 429 (body=%s)", resp.StatusCode, body)
	}
	assertErrorCode(t, body, "rate_limited")
	if resp.Header.Get("Retry-After") == "" {
		t.Error("429 login failure is missing Retry-After")
	}
}

func TestPasswordSecretLeak_AbsentFromResponses(t *testing.T) {
	e := newPasswordEnv(t)
	email := newEmail()
	secretPassword := "a very secret password"

	resp, body := e.request(t, http.MethodPost, auth.PasswordLoginPath, jsonBody(t, map[string]string{ //nolint:bodyclose // request closes the body itself before returning.
		"email": email, "password": secretPassword,
	}))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	for _, sentinel := range []string{email, secretPassword} {
		if strings.Contains(string(body), sentinel) {
			t.Errorf("login response leaked %q: %s", sentinel, body)
		}
	}

	_, regBody := e.request(t, http.MethodPost, auth.PasswordRegisterPath, jsonBody(t, map[string]string{ //nolint:bodyclose // request closes the body itself before returning.
		"name": "Ada", "email": email, "password": secretPassword,
	}))
	for _, sentinel := range []string{email, secretPassword} {
		if strings.Contains(string(regBody), sentinel) {
			t.Errorf("register response leaked %q: %s", sentinel, regBody)
		}
	}
}

// ---- enrolled accounts ----
//
// Password login and reauthentication for an enrolled account create a pending
// authentication instead of a session or proof update. Add or change and reset
// keep factor enforcement. See docs/design/second-factor-authentication.md.

// newRealClockPasswordEnv runs the password service on the wall clock, which
// the pending authentication manager also uses.
func newRealClockPasswordEnv(t *testing.T) *passwordEnv {
	t.Helper()
	return newPasswordEnvWith(t, func(o *auth.PasswordServiceOptions) {
		o.Clock = time.Now
		o.Sessions = auth.NewSessionManagerWithPool(o.Pool)
	})
}

// realSession issues a wall-clock session for userID.
func (e *passwordEnv) realSession(t *testing.T, userID uuid.UUID) (string, store.Session) {
	t.Helper()
	raw, sess, err := auth.NewSessionManagerWithPool(e.pool).Issue(t.Context(), userID, "test-agent/1.0", "203.0.113.60")
	if err != nil {
		t.Fatalf("issue session: %v", err)
	}
	return raw, sess
}

// assertSecondFactorRequired checks the fixed 202 body, a pending cookie, and
// no session cookie. It returns the pending cookie value.
func assertSecondFactorRequired(t *testing.T, resp *http.Response, body []byte) string {
	t.Helper()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body=%s)", resp.StatusCode, body)
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode 202 body %q: %v", body, err)
	}
	if len(env.Data) != 1 || env.Data["secondFactorRequired"] != true {
		t.Errorf("202 data = %v, want exactly secondFactorRequired=true", env.Data)
	}
	var pending string
	for _, c := range resp.Cookies() {
		switch c.Name {
		case auth.SessionCookieName:
			t.Errorf("enrolled response set %s, want no session", c.Name)
		case pendingCookieName:
			pending = c.Value
		}
	}
	if pending == "" {
		t.Fatal("enrolled response set no pending cookie")
	}
	return pending
}

func TestPasswordLogin_EnrolledAccount_PendingNotSession(t *testing.T) {
	e := newRealClockPasswordEnv(t)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	enrollForTest(t, e.q, userID)
	email := e.userEmail(t, userID)

	resp, body := e.request(t, http.MethodPost, auth.PasswordLoginPath, jsonBody(t, map[string]string{ //nolint:bodyclose // request closes the body itself before returning.
		"email": email, "password": testPassword, "next": "/app/settings",
	}))
	first := assertSecondFactorRequired(t, resp, body)
	row := pendingForCookie(t, e.q, first)
	if row.UserID != userID || row.Purpose != auth.PendingAuthenticationPurposeLogin || row.SessionID != nil || row.ReturnPath != "/app/settings" {
		t.Errorf("pending row = %+v, want an unbound login row with the validated next path", row)
	}
	if n := countSessions(t, e.pool, userID); n != 0 {
		t.Fatalf("enrolled login created %d sessions, want 0", n)
	}

	// An invalid next falls back to the default, and the new pending login
	// consumes the browser's previous one.
	resp, body = e.request(t, http.MethodPost, auth.PasswordLoginPath, jsonBody(t, map[string]string{ //nolint:bodyclose // request closes the body itself before returning.
		"email": email, "password": testPassword, "next": "https://evil.example/app",
	}), withCookie(requestCookie(pendingCookieName, first)))
	second := assertSecondFactorRequired(t, resp, body)
	if got := pendingForCookie(t, e.q, second).ReturnPath; got != "/app/resumes" {
		t.Errorf("pending return path for a foreign next = %q, want /app/resumes", got)
	}
	if pendingForCookie(t, e.q, first).ConsumedAt == nil {
		t.Error("previous pending login still live after a new primary login")
	}

	// A failed primary credential neither creates nor replaces a pending row.
	resp, body = e.request(t, http.MethodPost, auth.PasswordLoginPath, jsonBody(t, map[string]string{ //nolint:bodyclose // request closes the body itself before returning.
		"email": email, "password": "totally wrong password",
	}), withCookie(requestCookie(pendingCookieName, second)))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong password status = %d, want 401 (body=%s)", resp.StatusCode, body)
	}
	if n := pendingCountForUser(t, userID); n != 2 {
		t.Errorf("pending rows after a failed login = %d, want 2", n)
	}
	if pendingForCookie(t, e.q, second).ConsumedAt != nil {
		t.Error("a failed login consumed the live pending row")
	}
}

func TestPasswordLogin_UnenrolledClearsPendingCookie(t *testing.T) {
	e := newRealClockPasswordEnv(t)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)

	resp, body := e.request(t, http.MethodPost, auth.PasswordLoginPath, jsonBody(t, map[string]string{ //nolint:bodyclose // request closes the body itself before returning.
		"email": e.userEmail(t, userID), "password": testPassword, "next": "/app/settings",
	}), withCookie(requestCookie(pendingCookieName, "stale-pending")))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body=%s)", resp.StatusCode, body)
	}
	var session, pending *http.Cookie
	for _, c := range resp.Cookies() {
		switch c.Name {
		case auth.SessionCookieName:
			session = c
		case pendingCookieName:
			pending = c
		}
	}
	if session == nil || session.Value == "" {
		t.Error("unenrolled login set no session cookie")
	}
	if pending == nil || pending.MaxAge >= 0 {
		t.Errorf("unenrolled login pending cookie = %+v, want cleared", pending)
	}
	if n := pendingCountForUser(t, userID); n != 0 {
		t.Errorf("unenrolled login created %d pending rows, want 0", n)
	}
}

func TestPasswordReauth_EnrolledAccount_BindsSessionWithoutProofUpdate(t *testing.T) {
	e := newRealClockPasswordEnv(t)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	enrollForTest(t, e.q, userID)
	raw, sess := e.realSession(t, userID)

	resp, body := e.request(t, http.MethodPost, auth.PasswordReauthPath, jsonBody(t, map[string]string{"password": testPassword}), //nolint:bodyclose // request closes the body itself before returning.
		withCookie(sessionRequestCookie(raw)), withCSRF(csrfTokenFor(sess)))
	row := pendingForCookie(t, e.q, assertSecondFactorRequired(t, resp, body))
	if row.Purpose != auth.PendingAuthenticationPurposeReauth || row.SessionID == nil || *row.SessionID != sess.ID || row.ReturnPath != wantSettingsSessionsPath {
		t.Errorf("pending row = %+v, want reauth bound to session %s", row, sess.ID)
	}
	got, err := e.q.GetSessionByID(t.Context(), sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if !got.ReauthenticatedAt.Equal(sess.ReauthenticatedAt) || got.SecondFactorVerifiedAt != nil {
		t.Errorf("session proofs changed before factor completion: %+v", got)
	}
}

func TestPasswordReauth_StaleEpochRefreshesNothing(t *testing.T) {
	e := newRealClockPasswordEnv(t)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	_, sess := e.realSession(t, userID)
	if _, err := e.q.AdvanceUserAuthEpoch(t.Context(), userID); err != nil {
		t.Fatalf("advance epoch: %v", err)
	}

	if err := e.svc.ReauthForTest(t.Context(), sess, testPassword, raceIP); err == nil {
		t.Fatal("reauth of a stale-epoch session succeeded, want failure")
	}
	got, err := e.q.GetSessionByID(t.Context(), sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if !got.ReauthenticatedAt.Equal(sess.ReauthenticatedAt) {
		t.Error("stale-epoch reauth refreshed the primary proof")
	}
	if n := pendingCountForUser(t, userID); n != 0 {
		t.Errorf("stale-epoch reauth created %d pending rows, want 0", n)
	}
}

func TestPasswordChange_EnrolledAccountNeedsFactorProof(t *testing.T) {
	e := newRealClockPasswordEnv(t)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	enrollForTest(t, e.q, userID)
	raw, sess := e.realSession(t, userID)
	before, err := e.q.GetPasswordCredential(t.Context(), userID)
	if err != nil {
		t.Fatalf("get credential: %v", err)
	}

	resp, body := e.request(t, http.MethodPut, auth.PasswordMePath, jsonBody(t, map[string]string{"password": "another brand new password"}), //nolint:bodyclose // request closes the body itself before returning.
		withCookie(sessionRequestCookie(raw)), withCSRF(csrfTokenFor(sess)))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("change without factor proof status = %d, want 403 (body=%s)", resp.StatusCode, body)
	}
	assertErrorCode(t, body, "reauth_required")
	after, err := e.q.GetPasswordCredential(t.Context(), userID)
	if err != nil {
		t.Fatalf("get credential: %v", err)
	}
	if !bytes.Equal(after.EncodedHash, before.EncodedHash) || countSessions(t, e.pool, userID) != 1 {
		t.Fatal("a rejected change replaced the credential or revoked sessions")
	}

	fresh := time.Now()
	setFactorProofForTest(t, sess.ID, &fresh)
	resp, body = e.request(t, http.MethodPut, auth.PasswordMePath, jsonBody(t, map[string]string{"password": "another brand new password"}), //nolint:bodyclose // request closes the body itself before returning.
		withCookie(sessionRequestCookie(raw)), withCSRF(csrfTokenFor(sess)))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("change with both proofs status = %d, want 204 (body=%s)", resp.StatusCode, body)
	}
}

func TestPasswordChange_StaleEpochSnapshotRejected(t *testing.T) {
	e := newRealClockPasswordEnv(t)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	_, sess := e.realSession(t, userID)
	if _, err := e.q.AdvanceUserAuthEpoch(t.Context(), userID); err != nil {
		t.Fatalf("advance epoch: %v", err)
	}
	before, err := e.q.GetPasswordCredential(t.Context(), userID)
	if err != nil {
		t.Fatalf("get credential: %v", err)
	}
	if _, err = e.svc.ChangeForTest(t.Context(), sess, "another brand new password", raceIP); err == nil {
		t.Fatal("change with a stale-epoch session succeeded, want failure")
	}
	after, err := e.q.GetPasswordCredential(t.Context(), userID)
	if err != nil {
		t.Fatalf("get credential: %v", err)
	}
	if !bytes.Equal(after.EncodedHash, before.EncodedHash) {
		t.Error("a stale-epoch change replaced the credential")
	}
}

// TestPasswordReset_PreservesFactorEnforcement proves reset revokes sessions
// but keeps the factor policy, credentials, and recovery codes, so the next
// password login still stops at the pending boundary.
func TestPasswordReset_PreservesFactorEnforcement(t *testing.T) {
	e := newRealClockPasswordEnv(t)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	enrollForTest(t, e.q, userID)
	credentialID := make([]byte, 32)
	codeDigest := make([]byte, 32)
	for _, b := range [][]byte{credentialID, codeDigest} {
		if _, err := rand.Read(b); err != nil {
			t.Fatalf("random fixture: %v", err)
		}
	}
	if _, err := e.q.CreateWebAuthnCredential(t.Context(), store.CreateWebAuthnCredentialParams{
		UserID: userID, CredentialID: credentialID, PublicKey: []byte{1}, Transports: []string{}, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateWebAuthnCredential() error = %v", err)
	}
	if _, err := e.q.CreateSecondFactorRecoveryCode(t.Context(), store.CreateSecondFactorRecoveryCodeParams{
		UserID: userID, CodeDigest: codeDigest, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateSecondFactorRecoveryCode() error = %v", err)
	}
	e.realSession(t, userID)
	token, err := password.NewToken(rand.Reader)
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	issuedAt := time.Now()
	if _, err = e.q.CreatePasswordResetToken(t.Context(), store.CreatePasswordResetTokenParams{
		UserID: userID, TokenDigest: token.Digest[:], CreatedAt: issuedAt, ExpiresAt: issuedAt.Add(30 * time.Minute),
	}); err != nil {
		t.Fatalf("create reset token: %v", err)
	}

	newPassword := "a completely different password"
	resp, body := e.request(t, http.MethodPost, auth.PasswordResetPath, jsonBody(t, map[string]string{"token": token.Raw, "password": newPassword})) //nolint:bodyclose // request closes the body itself before returning.
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("reset status = %d, want 204 (body=%s)", resp.StatusCode, body)
	}
	if n := countSessions(t, e.pool, userID); n != 0 {
		t.Errorf("live sessions after reset = %d, want 0", n)
	}
	if _, readErr := e.q.GetSecondFactorPolicyForUpdate(t.Context(), userID); readErr != nil {
		t.Errorf("factor policy after reset: %v, want kept", readErr)
	}
	if creds, readErr := e.q.ListWebAuthnCredentialsForUser(t.Context(), userID); readErr != nil || len(creds) != 1 {
		t.Errorf("passkeys after reset = %d, %v; want 1", len(creds), readErr)
	}
	if n, readErr := e.q.CountSecondFactorRecoveryCodes(t.Context(), userID); readErr != nil || n != 1 {
		t.Errorf("recovery codes after reset = %d, %v; want 1", n, readErr)
	}

	resp, body = e.request(t, http.MethodPost, auth.PasswordLoginPath, jsonBody(t, map[string]string{ //nolint:bodyclose // request closes the body itself before returning.
		"email": e.userEmail(t, userID), "password": newPassword,
	}))
	assertSecondFactorRequired(t, resp, body)
	if n := countSessions(t, e.pool, userID); n != 0 {
		t.Errorf("login after reset created %d sessions, want 0", n)
	}
}

// servePasswordLoginAsync posts one password login on its own goroutine.
func servePasswordLoginAsync(ctx context.Context, handler http.Handler, email string) <-chan *httptest.ResponseRecorder {
	done := make(chan *httptest.ResponseRecorder, 1)
	body := `{"email":` + strconvQuote(email) + `,"password":` + strconvQuote(testPassword) + `}`
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, auth.PasswordLoginPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", testPublicOrigin)
	go func() {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		done <- rec
	}()
	return done
}

// strconvQuote renders s as a JSON string.
func strconvQuote(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

// TestPasswordLogin_FactorChangeUnderUserLock holds a factor change under the
// user lock while a login with a valid password waits for it. The login
// decides between session and pending from the committed factor state it
// reads under the lock, never from a snapshot taken before it.
func TestPasswordLogin_FactorChangeUnderUserLock(t *testing.T) {
	t.Run("first enrollment commits first, login gets pending", func(t *testing.T) {
		e := newRealClockPasswordEnv(t)
		userID := e.createUser(t)
		e.setPassword(t, userID, testPassword)
		email := e.userEmail(t, userID)

		holder, qtx, pid := beginUserLockHolder(t, e.pool, userID)
		handle := make([]byte, 32)
		if _, err := rand.Read(handle); err != nil {
			t.Fatalf("user handle: %v", err)
		}
		if _, err := qtx.CreateSecondFactorPolicy(t.Context(), store.CreateSecondFactorPolicyParams{UserID: userID, WebauthnUserHandle: handle, EnabledAt: time.Now()}); err != nil {
			t.Fatalf("create policy: %v", err)
		}
		if _, err := qtx.AdvanceUserAuthEpoch(t.Context(), userID); err != nil {
			t.Fatalf("advance epoch: %v", err)
		}

		done := servePasswordLoginAsync(t.Context(), e.handler, email)
		waitBlockedBy(t, pid)
		if err := holder.Commit(t.Context()); err != nil {
			t.Fatalf("commit enrollment: %v", err)
		}
		rec := awaitRecorder(t, done)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("login across first enrollment = %d (%s), want 202", rec.Code, rec.Body)
		}
		if n := countSessions(t, e.pool, userID); n != 0 {
			t.Errorf("login across first enrollment created %d sessions, want 0", n)
		}
	})

	t.Run("final removal commits first, login gets a current-epoch session", func(t *testing.T) {
		e := newRealClockPasswordEnv(t)
		userID := e.createUser(t)
		e.setPassword(t, userID, testPassword)
		enrollForTest(t, e.q, userID)
		email := e.userEmail(t, userID)

		holder, qtx, pid := beginUserLockHolder(t, e.pool, userID)
		if _, err := qtx.DeleteSecondFactorPolicyForUser(t.Context(), userID); err != nil {
			t.Fatalf("delete policy: %v", err)
		}
		if _, err := qtx.AdvanceUserAuthEpoch(t.Context(), userID); err != nil {
			t.Fatalf("advance epoch: %v", err)
		}

		done := servePasswordLoginAsync(t.Context(), e.handler, email)
		waitBlockedBy(t, pid)
		if err := holder.Commit(t.Context()); err != nil {
			t.Fatalf("commit removal: %v", err)
		}
		rec := awaitRecorder(t, done)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("login across final removal = %d (%s), want 204", rec.Code, rec.Body)
		}
		var raw string
		for _, c := range (&http.Response{Header: rec.Header()}).Cookies() {
			if c.Name == auth.SessionCookieName {
				raw = c.Value
			}
		}
		if _, _, err := auth.NewSessionManagerWithPool(e.pool).Authenticate(t.Context(), raw); err != nil {
			t.Errorf("session issued across final removal does not authenticate at the new epoch: %v", err)
		}
	})
}
