package auth_test

// Second-factor HTTP adversarial cases over live pending and session rows:
// pending authority is separate from session authority, Origin and pending
// CSRF run before rate admission and work, failures clear the pending cookie
// only when the contract says so, and disabled enrollment stays uniform.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/secondfactor"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

type sfEnv struct {
	pool     *store.Pool
	userID   uuid.UUID
	pending  auth.PendingAuthenticationIssue
	service  *sfService
	mux      *http.ServeMux
	sessions *auth.SessionManager
}

func newSFEnv(t *testing.T, service *sfService) *sfEnv {
	t.Helper()
	pool := newTestPool(t)
	userID := createTestUser(t, store.New(pool))
	issued, err := auth.NewPendingAuthenticationManager(pool, nil).Create(t.Context(), auth.PendingAuthenticationRequest{
		UserID: userID, Purpose: auth.PendingAuthenticationPurposeLogin, PrimaryVerifiedAt: time.Now(), ReturnPath: "/app/resumes",
	})
	if err != nil {
		t.Fatalf("create pending: %v", err)
	}
	return &sfEnv{pool: pool, userID: userID, pending: issued, service: service, mux: sfMux(t, service, pool), sessions: auth.NewSessionManagerWithPool(pool)}
}

func (e *sfEnv) pendingCookie() *http.Cookie {
	return &http.Cookie{Name: "__Host-auth-pending", Value: e.pending.RawToken}
}

func (e *sfEnv) pendingCSRF() string {
	return base64.RawURLEncoding.EncodeToString(e.pending.Pending.CSRFSecret)
}

func (e *sfEnv) verify(origin, csrf string) sfRequest {
	return sfRequest{
		method: http.MethodPost, path: auth.SecondFactorPendingPasskeyPath, body: `{"ok":true}`,
		contentType: "application/json", origin: origin, csrf: csrf, cookies: []*http.Cookie{e.pendingCookie()},
	}
}

func (e *sfEnv) session(t *testing.T) (string, store.Session) {
	t.Helper()
	raw, sess, err := e.sessions.Issue(t.Context(), e.userID, "test-agent", "203.0.113.7")
	if err != nil {
		t.Fatalf("issue session: %v", err)
	}
	return raw, sess
}

func TestSecondFactorPending_OriginAndCSRFRunBeforeWork(t *testing.T) {
	e := newSFEnv(t, &sfService{enabled: true})
	for name, req := range map[string]sfRequest{
		"missing origin":     e.verify("", e.pendingCSRF()),
		"foreign origin":     e.verify("https://evil.example", e.pendingCSRF()),
		"missing csrf":       e.verify(sfOrigin, ""),
		"session csrf shape": e.verify(sfOrigin, base64.RawURLEncoding.EncodeToString(make([]byte, 32))),
		"padded csrf":        e.verify(sfOrigin, e.pendingCSRF()+"="),
	} {
		rec := sfServe(t, e.mux, req)
		if rec.Code != http.StatusForbidden || sfErrorCode(t, rec) != "csrf_rejected" {
			t.Errorf("%s = %d %s, want 403 csrf_rejected", name, rec.Code, rec.Body)
		}
	}
	if calls := e.service.workCalls(); len(calls) != 0 {
		t.Fatalf("CSRF failures reached service work: %v", calls)
	}
}

func TestSecondFactorPending_OutcomesAndCookies(t *testing.T) {
	service := &sfService{enabled: true, issue: &auth.SessionIssue{RawToken: "new-session"}}
	e := newSFEnv(t, service)
	rec := sfServe(t, e.mux, e.verify(sfOrigin, e.pendingCSRF()))
	if rec.Code != http.StatusNoContent || !sfSetsCookie(rec, "__Host-session", "new-session") || !sfClearsCookie(rec, "__Host-auth-pending") {
		t.Fatalf("successful completion = %d, cookies %v", rec.Code, rec.Header().Values("Set-Cookie"))
	}

	cases := []struct {
		name    string
		err     error
		code    int
		errCode string
		clears  bool
	}{
		{"counted failure", auth.FailPendingVerification(auth.ErrSecondFactorVerificationFailed, nil), 401, "verification_failed", false},
		{"exhausting failure", &auth.PendingVerificationFailure{Err: auth.ErrSecondFactorVerificationFailed, Exhausted: true}, 401, "verification_failed", true},
		{"stale pending", auth.ErrPendingAuthenticationRequired, 401, "authentication_required", true},
		{"foreign ceremony", auth.ErrSecondFactorChallengeInvalid, 400, "challenge_invalid", false},
		{"dependency", context.DeadlineExceeded, 503, "authentication_unavailable", false},
	}
	for _, tc := range cases {
		service.err, service.issue = tc.err, nil
		rec = sfServe(t, e.mux, e.verify(sfOrigin, e.pendingCSRF()))
		if rec.Code != tc.code || sfErrorCode(t, rec) != tc.errCode || sfClearsCookie(rec, "__Host-auth-pending") != tc.clears {
			t.Errorf("%s = %d %s, cookies %v", tc.name, rec.Code, rec.Body, rec.Header().Values("Set-Cookie"))
		}
	}

	service.err = auth.ErrSecondFactorNotFound
	rec = sfServe(t, e.mux, sfRequest{
		method: http.MethodPost, path: auth.SecondFactorPendingOptionsPath, body: `{}`, contentType: "application/json",
		origin: sfOrigin, csrf: e.pendingCSRF(), cookies: []*http.Cookie{e.pendingCookie()},
	})
	if rec.Code != http.StatusNotFound || sfErrorCode(t, rec) != "factor_not_found" || sfClearsCookie(rec, "__Host-auth-pending") {
		t.Fatalf("assertion options without a passkey = %d %s", rec.Code, rec.Body)
	}
}

func TestSecondFactorPending_RateAdmissionFollowsCSRF(t *testing.T) {
	service := &sfService{enabled: true, err: auth.ErrSecondFactorChallengeInvalid}
	e := newSFEnv(t, service)
	for i := 0; i < 10; i++ {
		if rec := sfServe(t, e.mux, e.verify(sfOrigin, e.pendingCSRF())); rec.Code != http.StatusBadRequest {
			t.Fatalf("admitted attempt %d = %d %s", i+1, rec.Code, rec.Body)
		}
	}
	before := len(service.workCalls())
	rec := sfServe(t, e.mux, e.verify(sfOrigin, e.pendingCSRF()))
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("eleventh attempt = %d %s, want 429 with Retry-After", rec.Code, rec.Body)
	}
	if len(service.workCalls()) != before {
		t.Fatal("a rate-limited attempt reached service work")
	}
}

func TestSecondFactorPendingStatus_ReadsOnlyThePendingRow(t *testing.T) {
	service := &sfService{enabled: true, methods: []string{auth.SecondFactorMethodPasskey, auth.SecondFactorMethodRecovery}}
	e := newSFEnv(t, service)
	rec := sfServe(t, e.mux, sfRequest{method: http.MethodGet, path: auth.SecondFactorPendingPath, cookies: []*http.Cookie{e.pendingCookie()}})
	var body struct {
		Data struct {
			Purpose    string   `json:"purpose"`
			Methods    []string `json:"methods"`
			ExpiresAt  string   `json:"expiresAt"`
			ReturnPath string   `json:"returnPath"`
			CSRFToken  string   `json:"csrfToken"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status = %d %s, %v", rec.Code, rec.Body, err)
	}
	if body.Data.Purpose != "login" || len(body.Data.Methods) != 2 || body.Data.ReturnPath != "/app/resumes" ||
		body.Data.CSRFToken != e.pendingCSRF() || len(body.Data.CSRFToken) != 43 || body.Data.ExpiresAt != e.pending.Pending.ExpiresAt.UTC().Format(time.RFC3339) {
		t.Fatalf("status body = %+v", body.Data)
	}
	service.methods = nil
	if rec = sfServe(t, e.mux, sfRequest{method: http.MethodGet, path: auth.SecondFactorPendingPath, cookies: []*http.Cookie{e.pendingCookie()}}); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status with no available method = %d, want 503", rec.Code)
	}
	raw, _ := e.session(t)
	if rec = sfServe(t, e.mux, sfRequest{method: http.MethodGet, path: auth.SecondFactorPendingPath, cookies: []*http.Cookie{{Name: "__Host-session", Value: raw}}}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status with only a session cookie = %d, want 401", rec.Code)
	}
}

func TestSecondFactorPendingReauth_RequiresTheBoundSession(t *testing.T) {
	service := &sfService{enabled: true, methods: []string{auth.SecondFactorMethodPasskey}}
	e := newSFEnv(t, service)
	raw, sess := e.session(t)
	reauth, err := auth.NewPendingAuthenticationManager(e.pool, nil).Create(t.Context(), auth.PendingAuthenticationRequest{
		UserID: e.userID, Purpose: auth.PendingAuthenticationPurposeReauth, PrimaryVerifiedAt: time.Now(),
		ReturnPath: "/app/settings/sessions", SessionID: &sess.ID,
	})
	if err != nil {
		t.Fatalf("create reauth pending: %v", err)
	}
	pendingCookie := &http.Cookie{Name: "__Host-auth-pending", Value: reauth.RawToken}
	otherRaw, _ := e.session(t)
	for name, cookies := range map[string][]*http.Cookie{
		"no session":      {pendingCookie},
		"another session": {pendingCookie, {Name: "__Host-session", Value: otherRaw}},
	} {
		if rec := sfServe(t, e.mux, sfRequest{method: http.MethodGet, path: auth.SecondFactorPendingPath, cookies: cookies}); rec.Code != http.StatusUnauthorized {
			t.Errorf("reauth status with %s = %d, want 401", name, rec.Code)
		}
	}
	rec := sfServe(t, e.mux, sfRequest{method: http.MethodGet, path: auth.SecondFactorPendingPath, cookies: []*http.Cookie{pendingCookie, {Name: "__Host-session", Value: raw}}})
	if rec.Code != http.StatusOK {
		t.Fatalf("reauth status with the bound session = %d %s", rec.Code, rec.Body)
	}
}

func TestSecondFactorAccountMutations_SessionCSRFAndResponses(t *testing.T) {
	service := &sfService{enabled: true, replacement: auth.SessionIssue{RawToken: "replacement"}}
	e := newSFEnv(t, service)
	raw, sess := e.session(t)
	sessionCookie := []*http.Cookie{{Name: "__Host-session", Value: raw}}
	csrf := base64.RawURLEncoding.EncodeToString(sess.CSRFSecret)
	remove := sfRequest{method: http.MethodDelete, path: auth.SecondFactorPasskeysPath + "/" + uuid.NewString(), origin: sfOrigin, csrf: csrf, cookies: sessionCookie}

	for name, req := range map[string]sfRequest{
		"missing csrf":   {method: remove.method, path: remove.path, origin: sfOrigin, cookies: sessionCookie},
		"foreign origin": {method: remove.method, path: remove.path, origin: "https://evil.example", csrf: csrf, cookies: sessionCookie},
		"pending csrf":   {method: remove.method, path: remove.path, origin: sfOrigin, csrf: e.pendingCSRF(), cookies: sessionCookie},
	} {
		if rec := sfServe(t, e.mux, req); rec.Code != http.StatusForbidden {
			t.Errorf("%s = %d, want 403", name, rec.Code)
		}
	}
	if calls := e.service.workCalls(); len(calls) != 0 {
		t.Fatalf("CSRF failures reached service work: %v", calls)
	}
	if rec := sfServe(t, e.mux, sfRequest{method: http.MethodDelete, path: auth.SecondFactorPasskeysPath + "/not-a-uuid", origin: sfOrigin, csrf: csrf, cookies: sessionCookie}); rec.Code != http.StatusNotFound || sfErrorCode(t, rec) != "factor_not_found" {
		t.Fatalf("malformed passkey ID = %d %s", rec.Code, rec.Body)
	}
	if rec := sfServe(t, e.mux, remove); rec.Code != http.StatusNoContent || !sfSetsCookie(rec, "__Host-session", "replacement") {
		t.Fatalf("removal = %d, cookies %v", rec.Code, rec.Header().Values("Set-Cookie"))
	}

	for _, tc := range []struct {
		err     error
		code    int
		errCode string
	}{
		{auth.ErrReauthRequired, 403, "reauth_required"},
		{auth.ErrSecondFactorNotFound, 404, "factor_not_found"},
		{auth.ErrSecondFactorLimitReached, 409, "passkey_limit_reached"},
		{auth.ErrSecondFactorVerificationFailed, 400, "verification_failed"},
		{auth.ErrSessionInvalid, 401, "authentication_required"},
	} {
		service.err = tc.err
		if rec := sfServe(t, e.mux, remove); rec.Code != tc.code || sfErrorCode(t, rec) != tc.errCode {
			t.Errorf("removal with %v = %d %s", tc.err, rec.Code, rec.Body)
		}
	}
	service.err = nil

	regenerate := sfRequest{method: http.MethodPost, path: auth.SecondFactorRecoveryCodesPath, origin: sfOrigin, csrf: csrf, cookies: sessionCookie}
	if rec := sfServe(t, e.mux, regenerate); rec.Code != http.StatusOK || rec.Body.String() != `{"data":{"recoveryCodes":["amr_new"]}}`+"\n" {
		t.Fatalf("regeneration = %d %q", rec.Code, rec.Body)
	}

	service.registration = auth.SecondFactorRegistration{
		Passkey: auth.SecondFactorPasskey{ID: uuid.MustParse("01900000-0000-7000-8000-000000000001"), CreatedAt: time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)},
		Session: auth.SessionIssue{RawToken: "enrolled"},
	}
	complete := sfRequest{method: http.MethodPost, path: auth.SecondFactorPasskeysPath, body: `{"ok":true}`, contentType: "application/json", origin: sfOrigin, csrf: csrf, cookies: sessionCookie}
	rec := sfServe(t, e.mux, complete)
	want := `{"data":{"passkey":{"id":"01900000-0000-7000-8000-000000000001","createdAt":"2026-09-20T09:00:00Z","lastUsedAt":null}}}` + "\n"
	if rec.Code != http.StatusCreated || rec.Body.String() != want || !sfSetsCookie(rec, "__Host-session", "enrolled") {
		t.Fatalf("later registration = %d %q", rec.Code, rec.Body)
	}

	service.state = auth.SecondFactorState{}
	rec = sfServe(t, e.mux, sfRequest{method: http.MethodGet, path: auth.SecondFactorStatePath, cookies: sessionCookie})
	if rec.Code != http.StatusOK || rec.Body.String() != `{"data":{"enabled":false,"passkeys":[],"recoveryCodesRemaining":0}}`+"\n" {
		t.Fatalf("unenrolled state = %d %q", rec.Code, rec.Body)
	}
}

func TestSecondFactorRegistrationComplete_DisabledConsumesThenNotFound(t *testing.T) {
	service := &sfService{err: auth.ErrSecondFactorEnrollmentDisabled}
	e := newSFEnv(t, service)
	raw, sess := e.session(t)
	req := sfRequest{
		method: http.MethodPost, path: auth.SecondFactorPasskeysPath, body: `{"ok":true}`, contentType: "application/json",
		origin: sfOrigin, csrf: base64.RawURLEncoding.EncodeToString(sess.CSRFSecret), cookies: []*http.Cookie{{Name: "__Host-session", Value: raw}},
	}
	rec := sfServe(t, e.mux, req)
	want := sfServe(t, api.NotFound(), req)
	if rec.Code != http.StatusNotFound || rec.Body.String() != want.Body.String() {
		t.Fatalf("disabled completion = %d %s", rec.Code, rec.Body)
	}
	if service.completeCalls != 1 {
		t.Fatalf("disabled completion reached the service %d times, want 1 to consume the ceremony", service.completeCalls)
	}
	csrf := req.csrf
	req.csrf = ""
	if rec = sfServe(t, e.mux, req); rec.Code != http.StatusNotFound || service.completeCalls != 1 {
		t.Fatal("disabled completion without CSRF consumed a ceremony")
	}
	req.csrf = csrf
	for i := 2; i <= 10; i++ {
		if rec = sfServe(t, e.mux, req); rec.Code != http.StatusNotFound || service.completeCalls != i {
			t.Fatalf("admitted disabled completion %d = %d, calls %d", i, rec.Code, service.completeCalls)
		}
	}
	rec = sfServe(t, e.mux, req)
	if rec.Code != http.StatusNotFound || rec.Body.String() != want.Body.String() || service.completeCalls != 10 {
		t.Fatalf("rate-limited disabled completion = %d %s, calls %d; want the uniform 404 and no service work", rec.Code, rec.Body, service.completeCalls)
	}
}

// TestSecondFactorDisabledCompletion_StaleReauthConsumesCeremony runs the real
// service: a completion from a session whose primary proof is stale still
// consumes its matching ceremony and answers as an unregistered route.
func TestSecondFactorDisabledCompletion_StaleReauthConsumesCeremony(t *testing.T) {
	pool := newTestPool(t)
	userID := createTestUser(t, store.New(pool))
	sessions := auth.NewSessionManagerWithPool(pool)
	pending := auth.NewPendingAuthenticationManager(pool, nil)
	ring, err := authmail.NewKeyRing("k1", map[string][32]byte{"k1": {}}, rand.Reader)
	if err != nil {
		t.Fatalf("NewKeyRing() error = %v", err)
	}
	outbox, err := authmail.NewOutbox(ring, time.Now)
	if err != nil {
		t.Fatalf("NewOutbox() error = %v", err)
	}
	rp, err := secondfactor.NewRelyingParty(sfOrigin)
	if err != nil {
		t.Fatalf("NewRelyingParty() error = %v", err)
	}
	opts := secondfactor.Options{
		Pool: pool, Pending: pending, Sessions: sessions, Outbox: outbox, RelyingParty: rp,
		EnrollmentEnabled: true, Clock: time.Now, Entropy: rand.Reader,
	}
	enabled, err := secondfactor.New(opts)
	if err != nil {
		t.Fatalf("New(enabled) error = %v", err)
	}
	opts.EnrollmentEnabled = false
	disabled, err := secondfactor.New(opts)
	if err != nil {
		t.Fatalf("New(disabled) error = %v", err)
	}
	raw, sess, err := sessions.Issue(t.Context(), userID, "test-agent", "203.0.113.7")
	if err != nil {
		t.Fatalf("issue session: %v", err)
	}
	options, err := enabled.StartPasskeyRegistration(t.Context(), sess)
	if err != nil {
		t.Fatalf("StartPasskeyRegistration() error = %v", err)
	}
	if _, err = pool.Exec(t.Context(), "UPDATE sessions SET reauthenticated_at = reauthenticated_at - interval '16 minutes' WHERE id = $1", sess.ID); err != nil {
		t.Fatalf("age session: %v", err)
	}
	handlers, err := auth.NewSecondFactorHandlers(auth.SecondFactorHandlerOptions{
		Service: disabled, Pending: pending, Sessions: sessions, Limits: auth.NewSecondFactorRatePolicies(),
		PublicOrigin: sfOrigin, Clock: time.Now,
	})
	if err != nil {
		t.Fatalf("NewSecondFactorHandlers() error = %v", err)
	}
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)
	credentialID := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	body, err := json.Marshal(map[string]any{
		"ceremonyId": options.CeremonyID,
		"credential": map[string]any{
			"id": credentialID, "rawId": credentialID, "type": "public-key",
			"response":               map[string]any{"clientDataJSON": "e30", "attestationObject": "oA", "transports": []string{}},
			"clientExtensionResults": map[string]any{},
		},
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := sfRequest{
		method: http.MethodPost, path: auth.SecondFactorPasskeysPath, body: string(body), contentType: "application/json",
		origin: sfOrigin, csrf: base64.RawURLEncoding.EncodeToString(sess.CSRFSecret), cookies: []*http.Cookie{{Name: "__Host-session", Value: raw}},
	}
	rec := sfServe(t, mux, req)
	want := sfServe(t, api.NotFound(), req)
	if rec.Code != http.StatusNotFound || rec.Body.String() != want.Body.String() {
		t.Fatalf("disabled completion with stale reauth = %d %s, want the unregistered 404", rec.Code, rec.Body)
	}
	var consumed, credentials int
	if err = pool.QueryRow(t.Context(), "SELECT count(*) FROM webauthn_ceremonies WHERE session_id = $1 AND consumed_at IS NOT NULL", sess.ID).Scan(&consumed); err != nil {
		t.Fatalf("count ceremonies: %v", err)
	}
	if err = pool.QueryRow(t.Context(), "SELECT count(*) FROM webauthn_credentials WHERE user_id = $1", userID).Scan(&credentials); err != nil {
		t.Fatalf("count credentials: %v", err)
	}
	if consumed != 1 || credentials != 0 {
		t.Fatalf("disabled completion consumed %d ceremonies and stored %d credentials; want 1 and 0", consumed, credentials)
	}
}
