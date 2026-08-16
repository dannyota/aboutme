package auth_test

// Password-service adversarial cases: token expiry/replacement boundaries,
// rehash CAS, corrupt-hash no-oracle behavior, breach unavailability, the login
// failure limiter, and secret-free diagnostics.

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	resp, body := e.request(t, http.MethodPost, auth.PasswordVerifyPath, jsonBody(t, map[string]string{"token": token.Raw}))
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
	resp, body := e.request(t, http.MethodPost, auth.PasswordResetPath, jsonBody(t, map[string]string{
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
		resp, body := e.request(t, http.MethodPost, auth.PasswordRegisterPath, jsonBody(t, map[string]string{
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
		resp, _ := e.request(t, http.MethodPost, auth.PasswordForgotPath, jsonBody(t, map[string]string{"email": email}))
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
	if _, err := e.q.UpsertPasswordCredential(context.Background(), store.UpsertPasswordCredentialParams{
		UserID: userID, EncodedHash: []byte(weakEnc), CreatedAt: now, ChangedAt: now,
	}); err != nil {
		t.Fatalf("upsert weak credential: %v", err)
	}

	email := e.userEmail(t, userID)
	if _, err := e.svc.LoginForTest(context.Background(), email, testPassword, "ua", raceIP); err != nil {
		t.Fatalf("login error = %v", err)
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
	resp, body := e.request(t, http.MethodPost, auth.PasswordLoginPath, jsonBody(t, map[string]string{
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

	req := httptest.NewRequest(http.MethodPost, auth.PasswordRegisterPath, strings.NewReader(jsonBody(t, map[string]string{
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
	resp.Body.Close()
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
		resp, body := e.request(t, http.MethodPost, auth.PasswordLoginPath, jsonBody(t, map[string]string{
			"email": email, "password": testPassword,
		}))
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("failure #%d status = %d, want 401 (body=%s)", i, resp.StatusCode, body)
		}
		assertErrorCode(t, body, "authentication_failed")
	}
	resp, body := e.request(t, http.MethodPost, auth.PasswordLoginPath, jsonBody(t, map[string]string{
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

	resp, body := e.request(t, http.MethodPost, auth.PasswordLoginPath, jsonBody(t, map[string]string{
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

	_, regBody := e.request(t, http.MethodPost, auth.PasswordRegisterPath, jsonBody(t, map[string]string{
		"name": "Ada", "email": email, "password": secretPassword,
	}))
	for _, sentinel := range []string{email, secretPassword} {
		if strings.Contains(string(regBody), sentinel) {
			t.Errorf("register response leaked %q: %s", sentinel, regBody)
		}
	}
}
