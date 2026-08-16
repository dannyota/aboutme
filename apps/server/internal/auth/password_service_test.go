package auth_test

// Password service HTTP tests: the seven-route inventory, the closed route
// chain matrix, byte-equality responses, and the happy path for every operation
// against a live migrated database.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
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

// testPassword is a valid, already-NFC-normalized password for happy paths.
const testPassword = "correct horse battery staple"

// fastHashPolicy keeps Argon2id cheap in tests while staying within the parse
// budget the hasher enforces.
func fastHashPolicy() password.HashPolicy {
	return password.HashPolicy{
		Version:     19,
		MemoryKiB:   64,
		Iterations:  1,
		Parallelism: 1,
		SaltLen:     16,
		KeyLen:      32,
	}
}

// passwordEnv bundles a full production-shaped password service with its
// handler, query surface, and injectable clock.
type passwordEnv struct {
	handler http.Handler
	svc     *auth.PasswordService
	q       *store.Queries
	pool    *store.Pool
	clk     *testutil.Clock
	hasher  *password.Hasher
}

func newPasswordEnv(t *testing.T) *passwordEnv {
	t.Helper()

	pool := newTestPool(t)
	q := store.New(pool)
	clk := testutil.NewClockAtEpoch()

	sm := auth.NewSessionManagerWithPoolForTest(pool, clk.Now)
	hasher, err := password.NewHasher(fastHashPolicy(), rand.Reader, password.NewAdmission())
	if err != nil {
		t.Fatalf("NewHasher error = %v", err)
	}
	policy := password.NewPolicy(nil, nil)

	outbox := newTestOutbox(t, clk, rand.Reader)

	var emailKey [32]byte
	copy(emailKey[:], bytes.Repeat([]byte{0x01}, 32))
	limits, err := auth.NewPasswordRatePolicies(emailKey)
	if err != nil {
		t.Fatalf("NewPasswordRatePolicies error = %v", err)
	}

	svc, err := auth.NewPasswordService(auth.PasswordServiceOptions{
		Pool:         pool,
		Queries:      q,
		Sessions:     sm,
		Policy:       policy,
		Hasher:       hasher,
		Outbox:       outbox,
		Limits:       limits,
		PublicOrigin: testPublicOrigin,
		Clock:        clk.Now,
		Entropy:      rand.Reader,
	})
	if err != nil {
		t.Fatalf("NewPasswordService error = %v", err)
	}

	handler := api.New(testLogger(), noopPinger{}, api.Options{Clock: clk.Now}, nil, svc.RegisterRoutes)
	return &passwordEnv{handler: handler, svc: svc, q: q, pool: pool, clk: clk, hasher: hasher}
}

// passwordRequestOpt mutates a password request before it is served.
type passwordRequestOpt func(*http.Request)

func withOrigin(origin string) passwordRequestOpt {
	return func(r *http.Request) { r.Header.Set("Origin", origin) }
}

func withCSRF(token string) passwordRequestOpt {
	return func(r *http.Request) { r.Header.Set(auth.CSRFHeaderName, token) }
}

func withCookie(c *http.Cookie) passwordRequestOpt {
	return func(r *http.Request) { r.AddCookie(c) }
}

func withContentType(ct string) passwordRequestOpt {
	return func(r *http.Request) { r.Header.Set("Content-Type", ct) }
}

// request sends a JSON request through the full router chain and returns the
// raw body plus the response.
func (e *passwordEnv) request(t *testing.T, method, path, body string, opts ...passwordRequestOpt) (*http.Response, []byte) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Origin", testPublicOrigin)
	for _, o := range opts {
		o(req)
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	resp := rec.Result()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}
	return resp, raw
}

// jsonBody renders v as a request body, failing on any encode error.
func jsonBody(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return string(b)
}

func (e *passwordEnv) createUser(t *testing.T) uuid.UUID {
	t.Helper()
	user, err := e.q.CreateUser(context.Background(), store.CreateUserParams{
		Email: uuid.NewString() + "@example.com",
		Name:  "Test User",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user.ID
}

// setPassword stores a credential for rawPassword under userID using the same
// fast hasher the service verifies with.
func (e *passwordEnv) setPassword(t *testing.T, userID uuid.UUID, rawPassword string) {
	t.Helper()
	enc, err := e.hasher.Hash(context.Background(), rawPassword)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	now := e.clk.Now()
	if _, err := e.q.UpsertPasswordCredential(context.Background(), store.UpsertPasswordCredentialParams{
		UserID:      userID,
		EncodedHash: []byte(enc),
		CreatedAt:   now,
		ChangedAt:   now,
	}); err != nil {
		t.Fatalf("upsert credential: %v", err)
	}
}

func (e *passwordEnv) createSession(t *testing.T, userID uuid.UUID) (raw string, sess store.Session) {
	t.Helper()
	raw, sess, err := auth.NewSessionManagerWithPoolForTest(e.pool, e.clk.Now).Issue(context.Background(), userID, "test-agent/1.0", "203.0.113.60")
	if err != nil {
		t.Fatalf("issue session: %v", err)
	}
	return raw, sess
}

// createRegistration inserts a pending registration with a known raw token and
// returns the token and the registration row.
func (e *passwordEnv) createRegistration(t *testing.T, email, name, rawPassword string) (password.Token, store.PasswordRegistration) {
	t.Helper()
	token, err := password.NewToken(rand.Reader)
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	enc, err := e.hasher.Hash(context.Background(), rawPassword)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	now := e.clk.Now()
	reg, err := e.q.CreatePasswordRegistration(context.Background(), store.CreatePasswordRegistrationParams{
		Email:       email,
		Name:        name,
		EncodedHash: []byte(enc),
		TokenDigest: token.Digest[:],
		CreatedAt:   now,
		ExpiresAt:   now.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("create registration: %v", err)
	}
	return token, reg
}

// createResetToken inserts a pending reset token with a known raw token.
func (e *passwordEnv) createResetToken(t *testing.T, userID uuid.UUID) password.Token {
	t.Helper()
	token, err := password.NewToken(rand.Reader)
	if err != nil {
		t.Fatalf("new token: %v", err)
	}
	now := e.clk.Now()
	if _, err := e.q.CreatePasswordResetToken(context.Background(), store.CreatePasswordResetTokenParams{
		UserID:      userID,
		TokenDigest: token.Digest[:],
		CreatedAt:   now,
		ExpiresAt:   now.Add(30 * time.Minute),
	}); err != nil {
		t.Fatalf("create reset token: %v", err)
	}
	return token
}

func newEmail() string {
	return uuid.NewString() + "@example.com"
}

// newTestOutbox builds an outbox over a single key ring with the given nonce
// source. A failing nonce source makes EnqueueTx fail, which the outbox-failure
// race test uses to prove rollback.
func newTestOutbox(t *testing.T, clk *testutil.Clock, nonce io.Reader) *authmail.Outbox {
	t.Helper()
	var key [32]byte
	ring, err := authmail.NewKeyRing("k1", map[string][32]byte{"k1": key}, nonce)
	if err != nil {
		t.Fatalf("NewKeyRing error = %v", err)
	}
	outbox, err := authmail.NewOutbox(ring, clk.Now)
	if err != nil {
		t.Fatalf("NewOutbox error = %v", err)
	}
	return outbox
}

// errorReader is a nonce source that always fails, so Seal returns ErrNonce.
type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

// ---- inventory ----

func TestPasswordRoutes_Inventory(t *testing.T) {
	e := newPasswordEnv(t)

	paths := []struct {
		path   string
		method string
	}{
		{auth.PasswordRegisterPath, http.MethodPost},
		{auth.PasswordVerifyPath, http.MethodPost},
		{auth.PasswordLoginPath, http.MethodPost},
		{auth.PasswordForgotPath, http.MethodPost},
		{auth.PasswordResetPath, http.MethodPost},
		{auth.PasswordReauthPath, http.MethodPost},
		{auth.PasswordMePath, http.MethodPut},
	}
	for _, p := range paths {
		resp, body := e.request(t, p.method, p.path, "{}")
		// A registered route must not be a 404; a valid route responds with
		// something other than "no route".
		if resp.StatusCode == http.StatusNotFound {
			t.Errorf("%s %s returned 404: %s", p.method, p.path, body)
		}
	}
}

func TestPasswordRoutes_WrongMethod(t *testing.T) {
	e := newPasswordEnv(t)

	// GET is not allowed on any password route.
	for _, p := range []string{
		auth.PasswordRegisterPath,
		auth.PasswordVerifyPath,
		auth.PasswordLoginPath,
		auth.PasswordForgotPath,
		auth.PasswordResetPath,
		auth.PasswordReauthPath,
		auth.PasswordMePath,
	} {
		resp, body := e.request(t, http.MethodGet, p, "")
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("GET %s status = %d, want 405 (body=%s)", p, resp.StatusCode, body)
		}
	}
}

// ---- route chain matrix ----

func TestPasswordRoute_MediaTypeRejectedBeforeWork(t *testing.T) {
	e := newPasswordEnv(t)
	email := newEmail()

	resp, body := e.request(t, http.MethodPost, auth.PasswordRegisterPath,
		jsonBody(t, map[string]string{"name": "Ada", "email": email, "password": testPassword}),
		withContentType("text/plain"))
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415 (body=%s)", resp.StatusCode, body)
	}
	assertErrorCode(t, body, "media_type_unsupported")

	// No registration or job may be written for a rejected body.
	assertNoRegistrationForEmail(t, e.pool, email)
}

func TestPasswordRoute_BodyTooLargeRejected(t *testing.T) {
	e := newPasswordEnv(t)

	huge := `{"name":"` + strings.Repeat("x", 5000) + `"}`
	resp, body := e.request(t, http.MethodPost, auth.PasswordRegisterPath, huge)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413 (body=%s)", resp.StatusCode, body)
	}
	assertErrorCode(t, body, "body_too_large")
}

func TestPasswordRoute_MalformedJSONRejected(t *testing.T) {
	e := newPasswordEnv(t)
	email := newEmail()

	badBodies := []string{
		`{`,
		`{"name":"Ada",}`,
		`{"name":"Ada","name":"Bob"}`,
		`{"name":"Ada","email":"` + email + `","password":"correct horse battery staple","extra":1}`,
		`{"name":123}`,
		`{"password":null}`,
		`{"name":"Ada"} extra`,
	}
	for _, bad := range badBodies {
		resp, body := e.request(t, http.MethodPost, auth.PasswordRegisterPath, bad)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("body %q status = %d, want 400 (body=%s)", bad, resp.StatusCode, body)
		}
		assertErrorCode(t, body, "request_invalid")
	}
	assertNoRegistrationForEmail(t, e.pool, email)
}

func TestPasswordRoute_OriginRejectedAfterJSON(t *testing.T) {
	e := newPasswordEnv(t)

	// Missing Origin/Referer fails closed (403), and it runs after the strict
	// JSON chain: a request with BOTH a wrong origin and a malformed body
	// reports the JSON failure first.
	resp, body := e.request(t, http.MethodPost, auth.PasswordRegisterPath,
		`{"name":"Ada","email":"a@example.com","password":"correct horse battery staple"}`,
		withOrigin(""))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("missing-origin status = %d, want 403 (body=%s)", resp.StatusCode, body)
	}
	assertErrorCode(t, body, "csrf_rejected")

	resp, body = e.request(t, http.MethodPost, auth.PasswordRegisterPath,
		`{"name":"Ada","email":"a@example.com","password":"correct horse battery staple"`,
		withOrigin("https://evil.example"))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("malformed body + wrong origin status = %d, want 400 (body=%s)", resp.StatusCode, body)
	}
	assertErrorCode(t, body, "request_invalid")
}

// ---- byte equality ----

func TestPasswordRegisterForgot_ByteIdentical202(t *testing.T) {
	e := newPasswordEnv(t)

	register := jsonBody(t, map[string]string{
		"name": "Ada", "email": newEmail(), "password": testPassword,
	})
	_, regBody := e.request(t, http.MethodPost, auth.PasswordRegisterPath, register)
	wantAccepted := "{\"data\":{\"accepted\":true}}\n"
	if string(regBody) != wantAccepted {
		t.Errorf("register 202 body = %q, want %q", regBody, wantAccepted)
	}

	_, forgotBody := e.request(t, http.MethodPost, auth.PasswordForgotPath, jsonBody(t, map[string]string{"email": newEmail()}))
	if string(forgotBody) != wantAccepted {
		t.Errorf("forgot 202 body = %q, want %q", forgotBody, wantAccepted)
	}
	if !bytes.Equal(regBody, forgotBody) {
		t.Error("register and forgot 202 bodies differ, want byte-identical")
	}
}

func TestPasswordLogin_ByteIdentical401(t *testing.T) {
	e := newPasswordEnv(t)

	want := "{\"error\":{\"code\":\"authentication_failed\",\"message\":\"authentication failed\"}}\n"
	for _, email := range []string{newEmail(), newEmail(), newEmail()} {
		resp, body := e.request(t, http.MethodPost, auth.PasswordLoginPath,
			jsonBody(t, map[string]string{"email": email, "password": testPassword}))
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
		if string(body) != want {
			t.Errorf("login 401 body = %q, want %q", body, want)
		}
	}
}

// ---- register happy path ----

func TestPasswordRegister_HappyPath(t *testing.T) {
	e := newPasswordEnv(t)
	email := newEmail()

	resp, body := e.request(t, http.MethodPost, auth.PasswordRegisterPath, jsonBody(t, map[string]string{
		"name": "Ada Lovelace", "email": email, "password": testPassword,
	}))
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body=%s)", resp.StatusCode, body)
	}

	reg, err := e.q.GetPasswordRegistrationByEmailForUpdate(context.Background(), email)
	if err != nil {
		t.Fatalf("registration not created: %v", err)
	}
	if reg.Name != "Ada Lovelace" {
		t.Errorf("registration name = %q, want %q", reg.Name, "Ada Lovelace")
	}

	var kind string
	if err := e.pool.QueryRow(context.Background(),
		`SELECT kind FROM auth_email_jobs WHERE registration_id = $1`, reg.ID).Scan(&kind); err != nil {
		t.Fatalf("read verify job: %v", err)
	}
	if kind != "verify" {
		t.Errorf("job kind = %q, want %q", kind, "verify")
	}
}

// ---- verify happy path ----

func TestPasswordVerify_HappyPath(t *testing.T) {
	e := newPasswordEnv(t)
	email := newEmail()
	token, _ := e.createRegistration(t, email, "Ada", testPassword)

	resp, body := e.request(t, http.MethodPost, auth.PasswordVerifyPath, jsonBody(t, map[string]string{"token": token.Raw}))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body=%s)", resp.StatusCode, body)
	}

	user, err := e.q.GetUserByCanonicalEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("user not created: %v", err)
	}
	if _, err := e.q.GetPasswordCredential(context.Background(), user.ID); err != nil {
		t.Fatalf("credential not created: %v", err)
	}

	// The token must be single-use: the registration is gone.
	if _, err := e.q.GetPasswordRegistrationByDigest(context.Background(), token.Digest[:]); err == nil {
		t.Error("registration still present after verify, want consumed")
	}
}

// ---- login happy path ----

func TestPasswordLogin_HappyPath(t *testing.T) {
	e := newPasswordEnv(t)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	email := e.userEmail(t, userID)

	resp, body := e.request(t, http.MethodPost, auth.PasswordLoginPath, jsonBody(t, map[string]string{
		"email": email, "password": testPassword,
	}))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body=%s)", resp.StatusCode, body)
	}

	cookies := resp.Cookies()
	if len(cookies) != 1 || cookies[0].Name != auth.SessionCookieName {
		t.Fatalf("login cookies = %+v, want one __Host-session cookie", cookies)
	}
	if cookies[0].Secure != true || cookies[0].HttpOnly != true || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Errorf("session cookie attributes wrong: %+v", cookies[0])
	}
}

// userEmail reads the email of userID (creating a canonical-email account in the
// helper above would otherwise require threading the email).
func (e *passwordEnv) userEmail(t *testing.T, userID uuid.UUID) string {
	t.Helper()
	user, err := e.q.GetUserByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	return user.Email
}

// ---- helpers for assertions ----

func assertErrorCode(t *testing.T, body []byte, code string) {
	t.Helper()
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode error envelope %q: %v", body, err)
	}
	if env.Error.Code != code {
		t.Errorf("error.code = %q, want %q (body=%s)", env.Error.Code, code, body)
	}
	if env.Error.Message == "" {
		t.Error("error.message is empty")
	}
}

// assertNoRegistrationForEmail asserts no pending registration exists for a
// specific email, so a rejected request is proven not to have written one (the
// shared database holds registrations from other tests).
func assertNoRegistrationForEmail(t *testing.T, pool *store.Pool, email string) {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM password_registrations WHERE email = $1`, email).Scan(&n); err != nil {
		t.Fatalf("count registrations for %q: %v", email, err)
	}
	if n != 0 {
		t.Errorf("password_registrations rows for %q = %d, want 0", email, n)
	}
}

// countSessions returns the number of live sessions for userID.
func countSessions(t *testing.T, pool *store.Pool, userID uuid.UUID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL`, userID).Scan(&n); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return n
}

// ---- forgot happy path ----

func TestPasswordForgot_HappyPath(t *testing.T) {
	e := newPasswordEnv(t)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	email := e.userEmail(t, userID)

	resp, body := e.request(t, http.MethodPost, auth.PasswordForgotPath, jsonBody(t, map[string]string{"email": email}))
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body=%s)", resp.StatusCode, body)
	}

	// Exactly one reset token and one reset job now exist for the user.
	rt, err := e.q.GetPasswordResetTokenByUserForUpdate(context.Background(), userID)
	if err != nil {
		t.Fatalf("reset token not created: %v", err)
	}
	var kind string
	if err := e.pool.QueryRow(context.Background(),
		`SELECT kind FROM auth_email_jobs WHERE reset_token_id = $1`, rt.ID).Scan(&kind); err != nil {
		t.Fatalf("read reset job: %v", err)
	}
	if kind != "reset" {
		t.Errorf("job kind = %q, want %q", kind, "reset")
	}
}

func TestPasswordForgot_UnknownAndProviderOnly_NoOp(t *testing.T) {
	e := newPasswordEnv(t)

	// Unknown email: 202 and no reset token created.
	resp, _ := e.request(t, http.MethodPost, auth.PasswordForgotPath, jsonBody(t, map[string]string{"email": newEmail()}))
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("unknown forgot status = %d, want 202", resp.StatusCode)
	}

	// Provider-only account: 202 and no reset token created.
	userID := e.createUser(t)
	email := e.userEmail(t, userID)
	resp, _ = e.request(t, http.MethodPost, auth.PasswordForgotPath, jsonBody(t, map[string]string{"email": email}))
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("provider-only forgot status = %d, want 202", resp.StatusCode)
	}
	if _, err := e.q.GetPasswordResetTokenByUserForUpdate(context.Background(), userID); err == nil {
		t.Error("reset token created for a provider-only account, want none")
	}
}

// ---- reset happy path ----

func TestPasswordReset_HappyPath(t *testing.T) {
	e := newPasswordEnv(t)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	raw, _ := e.createSession(t, userID)
	token := e.createResetToken(t, userID)

	newPassword := "a completely different password"
	resp, body := e.request(t, http.MethodPost, auth.PasswordResetPath, jsonBody(t, map[string]string{
		"token": token.Raw, "password": newPassword,
	}))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body=%s)", resp.StatusCode, body)
	}

	// The reset token is single-use.
	if _, err := e.q.GetPasswordResetTokenByDigest(context.Background(), token.Digest[:]); err == nil {
		t.Error("reset token still present after reset, want consumed")
	}
	// Every session is revoked.
	if n := countSessions(t, e.pool, userID); n != 0 {
		t.Errorf("live sessions after reset = %d, want 0", n)
	}
	// The credential is replaced: new password works, old does not.
	email := e.userEmail(t, userID)
	if r, _ := e.request(t, http.MethodPost, auth.PasswordLoginPath, jsonBody(t, map[string]string{"email": email, "password": newPassword})); r.StatusCode != http.StatusNoContent {
		t.Errorf("login with new password status = %d, want 204", r.StatusCode)
	}
	if r, _ := e.request(t, http.MethodPost, auth.PasswordLoginPath, jsonBody(t, map[string]string{"email": email, "password": testPassword})); r.StatusCode != http.StatusUnauthorized {
		t.Errorf("login with old password status = %d, want 401", r.StatusCode)
	}
	_ = raw
}

// ---- reauth happy path ----

func TestPasswordReauth_HappyPath(t *testing.T) {
	e := newPasswordEnv(t)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	raw, sess := e.createSession(t, userID)

	e.clk.Advance(10 * time.Minute)
	resp, body := e.request(t, http.MethodPost, auth.PasswordReauthPath, jsonBody(t, map[string]string{"password": testPassword}),
		withCookie(sessionRequestCookie(raw)), withCSRF(csrfTokenFor(sess)))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body=%s)", resp.StatusCode, body)
	}

	// The current session's reauthenticated_at was refreshed to the new now.
	got, err := e.q.GetSessionByID(context.Background(), sess.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if !got.ReauthenticatedAt.Equal(e.clk.Now()) {
		t.Errorf("reauthenticated_at = %v, want %v", got.ReauthenticatedAt, e.clk.Now())
	}
}

func TestPasswordReauth_WrongPassword(t *testing.T) {
	e := newPasswordEnv(t)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	raw, sess := e.createSession(t, userID)

	resp, body := e.request(t, http.MethodPost, auth.PasswordReauthPath, jsonBody(t, map[string]string{"password": "totally wrong password"}),
		withCookie(sessionRequestCookie(raw)), withCSRF(csrfTokenFor(sess)))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body=%s)", resp.StatusCode, body)
	}
	assertErrorCode(t, body, "reauth_failed")
}

// ---- change happy path ----

func TestPasswordChange_HappyPath(t *testing.T) {
	e := newPasswordEnv(t)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	oldRaw, oldSess := e.createSession(t, userID)

	newPassword := "another brand new password"
	resp, body := e.request(t, http.MethodPut, auth.PasswordMePath, jsonBody(t, map[string]string{"password": newPassword}),
		withCookie(sessionRequestCookie(oldRaw)), withCSRF(csrfTokenFor(oldSess)))
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body=%s)", resp.StatusCode, body)
	}

	cookies := resp.Cookies()
	if len(cookies) != 1 || cookies[0].Name != auth.SessionCookieName {
		t.Fatalf("change cookies = %+v, want one fresh __Host-session cookie", cookies)
	}
	newRaw := cookies[0].Value
	if newRaw == oldRaw {
		t.Error("fresh session token equals the old one, want a new token")
	}

	// The old session is revoked; the new one authenticates.
	sm := auth.NewSessionManagerWithPoolForTest(e.pool, e.clk.Now)
	if _, _, err := sm.Authenticate(context.Background(), oldRaw); err == nil {
		t.Error("old session still authenticates after change, want revoked")
	}
	newSess, _, err := sm.Authenticate(context.Background(), newRaw)
	if err != nil {
		t.Fatalf("new session does not authenticate: %v", err)
	}
	// The fresh session preserves the old session's absolute expiry.
	if !newSess.AbsoluteExpiresAt.Equal(oldSess.AbsoluteExpiresAt) {
		t.Errorf("new session absolute expiry = %v, want %v", newSess.AbsoluteExpiresAt, oldSess.AbsoluteExpiresAt)
	}

	// The credential is replaced.
	email := e.userEmail(t, userID)
	if r, _ := e.request(t, http.MethodPost, auth.PasswordLoginPath, jsonBody(t, map[string]string{"email": email, "password": newPassword})); r.StatusCode != http.StatusNoContent {
		t.Errorf("login with new password status = %d, want 204", r.StatusCode)
	}
	if r, _ := e.request(t, http.MethodPost, auth.PasswordLoginPath, jsonBody(t, map[string]string{"email": email, "password": testPassword})); r.StatusCode != http.StatusUnauthorized {
		t.Errorf("login with old password status = %d, want 401", r.StatusCode)
	}
}

func TestPasswordChange_RequiresRecentReauth(t *testing.T) {
	e := newPasswordEnv(t)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	raw, sess := e.createSession(t, userID)

	// A stale reauthentication window is rejected before any write.
	e.clk.Advance(16 * time.Minute)
	resp, body := e.request(t, http.MethodPut, auth.PasswordMePath, jsonBody(t, map[string]string{"password": "another brand new password"}),
		withCookie(sessionRequestCookie(raw)), withCSRF(csrfTokenFor(sess)))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body=%s)", resp.StatusCode, body)
	}
	assertErrorCode(t, body, "reauth_required")
}

func TestPasswordChange_RequiresSessionAndCSRF(t *testing.T) {
	e := newPasswordEnv(t)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	raw, sess := e.createSession(t, userID)

	// No session cookie -> 401 authentication_required.
	resp, body := e.request(t, http.MethodPut, auth.PasswordMePath, jsonBody(t, map[string]string{"password": "x"}))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-session status = %d, want 401 (body=%s)", resp.StatusCode, body)
	}
	assertErrorCode(t, body, "authentication_required")

	// Session but wrong CSRF token -> 403 csrf_rejected.
	resp, body = e.request(t, http.MethodPut, auth.PasswordMePath, jsonBody(t, map[string]string{"password": "x"}),
		withCookie(sessionRequestCookie(raw)), withCSRF("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong-csrf status = %d, want 403 (body=%s)", resp.StatusCode, body)
	}
	assertErrorCode(t, body, "csrf_rejected")
	_ = sess
}
