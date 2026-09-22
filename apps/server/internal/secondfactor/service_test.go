package secondfactor

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// harness wires the real service over the migrated test database. Every
// account it creates is new, so tests only read their own rows.
type harness struct {
	t        *testing.T
	pool     *store.Pool
	q        *store.Queries
	pending  *auth.PendingAuthenticationManager
	sessions *auth.SessionManager
	ring     *authmail.KeyRing
	opts     Options
	svc      *Service
}

type testAccount struct {
	user store.User
	sess store.Session
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func newHarness(t *testing.T, mutate func(*Options)) *harness {
	t.Helper()
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	h := &harness{t: t, pool: pool, q: store.New(pool)}
	h.pending = auth.NewPendingAuthenticationManager(pool, nil)
	h.sessions = auth.NewSessionManagerWithPool(pool)
	h.ring = newTestRing(t, rand.Reader)
	outbox, err := authmail.NewOutbox(h.ring, time.Now)
	if err != nil {
		t.Fatalf("NewOutbox() error = %v", err)
	}
	h.opts = Options{
		Pool: pool, Pending: h.pending, Sessions: h.sessions, Outbox: outbox, RelyingParty: testRelyingParty(t),
		EnrollmentEnabled: true, Clock: time.Now, Entropy: rand.Reader,
	}
	if mutate != nil {
		mutate(&h.opts)
	}
	h.svc = h.service(h.opts)
	return h
}

func (h *harness) service(opts Options) *Service {
	h.t.Helper()
	svc, err := New(opts)
	if err != nil {
		h.t.Fatalf("New() error = %v", err)
	}
	return svc
}

func newTestRing(t *testing.T, nonce io.Reader) *authmail.KeyRing {
	t.Helper()
	var key [32]byte
	ring, err := authmail.NewKeyRing("k1", map[string][32]byte{"k1": key}, nonce)
	if err != nil {
		t.Fatalf("NewKeyRing() error = %v", err)
	}
	return ring
}

func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func (h *harness) newAccount() testAccount {
	h.t.Helper()
	ctx := testContext(h.t)
	user, err := h.q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.com", Name: "Test User"})
	if err != nil {
		h.t.Fatalf("create user: %v", err)
	}
	return testAccount{user: user, sess: h.newSession(user.ID)}
}

func (h *harness) newSession(userID uuid.UUID) store.Session {
	h.t.Helper()
	_, sess, err := h.sessions.Issue(testContext(h.t), userID, "test-agent", "203.0.113.7")
	if err != nil {
		h.t.Fatalf("issue session: %v", err)
	}
	return sess
}

// publicKeyFields reads the challenge and user handle from options.
func publicKeyFields(t *testing.T, options auth.SecondFactorOptions) (challenge, handle []byte) {
	t.Helper()
	var pk struct {
		Challenge string `json:"challenge"`
		User      struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(options.PublicKey, &pk); err != nil {
		t.Fatalf("decode options: %v", err)
	}
	challenge, err := b64.DecodeString(pk.Challenge)
	if err != nil {
		t.Fatalf("decode challenge: %v", err)
	}
	if pk.User.ID != "" {
		if handle, err = b64.DecodeString(pk.User.ID); err != nil {
			t.Fatalf("decode user handle: %v", err)
		}
	}
	return challenge, handle
}

func (h *harness) registrationCredential(sess store.Session, a *testAuthenticator, mutate func(*registrationSpec)) auth.SecondFactorCredential {
	h.t.Helper()
	options, err := h.svc.StartPasskeyRegistration(testContext(h.t), sess)
	if err != nil {
		h.t.Fatalf("StartPasskeyRegistration() error = %v", err)
	}
	challenge, _ := publicKeyFields(h.t, options)
	credential, err := h.svc.DecodePasskeyRegistration(a.registrationBody(options.CeremonyID, challenge, mutate))
	if err != nil {
		h.t.Fatalf("DecodePasskeyRegistration() error = %v", err)
	}
	return credential
}

func (h *harness) register(sess store.Session, a *testAuthenticator, mutate func(*registrationSpec)) auth.SecondFactorRegistration {
	h.t.Helper()
	reg, err := h.svc.CompletePasskeyRegistration(testContext(h.t), sess, h.registrationCredential(sess, a, mutate))
	if err != nil {
		h.t.Fatalf("CompletePasskeyRegistration() error = %v", err)
	}
	return reg
}

func (h *harness) newPending(userID uuid.UUID, sessionID *uuid.UUID) auth.PendingAuthenticationIssue {
	h.t.Helper()
	purpose := auth.PendingAuthenticationPurposeLogin
	if sessionID != nil {
		purpose = auth.PendingAuthenticationPurposeReauth
	}
	issued, err := h.pending.Create(testContext(h.t), auth.PendingAuthenticationRequest{
		UserID: userID, Purpose: purpose, PrimaryVerifiedAt: time.Now(), ReturnPath: "/app/resumes", SessionID: sessionID,
	})
	if err != nil {
		h.t.Fatalf("create pending: %v", err)
	}
	return issued
}

func (h *harness) assertionCredential(pendingToken string, sessionID *uuid.UUID, a *testAuthenticator, counter uint32, mutate func(*assertionSpec)) auth.SecondFactorCredential {
	h.t.Helper()
	options, err := h.svc.StartPasskeyAssertion(testContext(h.t), pendingToken, sessionID)
	if err != nil {
		h.t.Fatalf("StartPasskeyAssertion() error = %v", err)
	}
	challenge, _ := publicKeyFields(h.t, options)
	credential, err := h.svc.DecodePasskeyAssertion(a.assertionBody(options.CeremonyID, challenge, counter, mutate))
	if err != nil {
		h.t.Fatalf("DecodePasskeyAssertion() error = %v", err)
	}
	return credential
}

func (h *harness) complete(pendingToken string, sessionID *uuid.UUID, credential auth.SecondFactorCredential) (*auth.SessionIssue, error) {
	return h.svc.CompletePending(testContext(h.t), pendingToken, sessionID, credential, auth.SecondFactorClient{UserAgent: "test-agent", IP: "203.0.113.7"})
}

func (h *harness) count(query string, args ...any) int {
	h.t.Helper()
	var n int
	if err := h.pool.QueryRow(testContext(h.t), query, args...).Scan(&n); err != nil {
		h.t.Fatalf("count %q: %v", query, err)
	}
	return n
}

func (h *harness) mails(userID uuid.UUID, kind authmail.Kind) int {
	return h.count("SELECT count(*) FROM auth_email_jobs WHERE user_id = $1 AND kind = $2", userID, string(kind))
}

func (h *harness) epoch(userID uuid.UUID) int64 {
	h.t.Helper()
	var epoch int64
	if err := h.pool.QueryRow(testContext(h.t), "SELECT auth_epoch FROM users WHERE id = $1", userID).Scan(&epoch); err != nil {
		h.t.Fatalf("read epoch: %v", err)
	}
	return epoch
}

// runConcurrently starts every call behind one barrier and fails the test if
// they do not all return within the timeout.
func runConcurrently(t *testing.T, calls ...func() error) []error {
	t.Helper()
	start := make(chan struct{})
	results := make([]error, len(calls))
	var wg sync.WaitGroup
	for i, call := range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i] = call()
		}()
	}
	close(start)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(45 * time.Second):
		t.Fatal("concurrent calls did not finish before the timeout")
	}
	return results
}

func TestFirstRegistration_CreatesPolicyCredentialCodesAndRotatesSession(t *testing.T) {
	h := newHarness(t, nil)
	acct := h.newAccount()
	a := newTestAuthenticator(t)
	reg := h.register(acct.sess, a, nil)

	if len(reg.RecoveryCodes) != recoveryCodeCount {
		t.Fatalf("first enrollment returned %d recovery codes, want %d", len(reg.RecoveryCodes), recoveryCodeCount)
	}
	for _, code := range reg.RecoveryCodes {
		value, err := parseRecoveryCode(code)
		if err != nil {
			t.Fatalf("returned code %q does not parse: %v", code, err)
		}
		if n := h.count("SELECT count(*) FROM second_factor_recovery_codes WHERE user_id = $1 AND code_digest = $2", acct.user.ID, recoveryCodeDigest(acct.user.ID, value)); n != 1 {
			t.Fatalf("stored digests matching one returned code = %d, want 1", n)
		}
	}
	replacement := reg.Session.Session
	if replacement.AuthEpoch != 1 || h.epoch(acct.user.ID) != 1 {
		t.Fatalf("epoch after first enrollment: session %d, user %d; want 1", replacement.AuthEpoch, h.epoch(acct.user.ID))
	}
	if replacement.SecondFactorVerifiedAt == nil || !replacement.CreatedAt.Equal(acct.sess.CreatedAt) ||
		!replacement.ReauthenticatedAt.Equal(acct.sess.ReauthenticatedAt) || !replacement.AbsoluteExpiresAt.Equal(acct.sess.AbsoluteExpiresAt) {
		t.Fatalf("replacement session = %+v; want copied windows and a factor proof", replacement)
	}
	if n := h.count("SELECT count(*) FROM sessions WHERE id = $1 AND revoked_at IS NOT NULL", acct.sess.ID); n != 1 {
		t.Fatal("first enrollment left the prior session live")
	}
	if n := h.count("SELECT count(*) FROM second_factor_policies WHERE user_id = $1", acct.user.ID); n != 1 {
		t.Fatalf("policies = %d, want 1", n)
	}
	if n := h.count("SELECT count(*) FROM webauthn_credentials WHERE user_id = $1 AND credential_id = $2 AND transports = ARRAY['hybrid','internal']::text[]", acct.user.ID, a.credentialID); n != 1 {
		t.Fatalf("stored passkeys with canonical transports = %d, want 1", n)
	}
	if n := h.mails(acct.user.ID, authmail.KindSecondFactorEnabled); n != 1 {
		t.Fatalf("second_factor_enabled mails = %d, want 1", n)
	}
	state, err := h.svc.State(testContext(t), acct.user.ID)
	if err != nil || !state.Enabled || len(state.Passkeys) != 1 || state.Passkeys[0].ID != reg.Passkey.ID || state.RecoveryCodesRemaining != 10 {
		t.Fatalf("State() = %+v, %v", state, err)
	}
}

func TestLaterRegistration_ReusesHandleWithoutRecoveryCodes(t *testing.T) {
	h := newHarness(t, nil)
	acct := h.newAccount()
	first := h.register(acct.sess, newTestAuthenticator(t), nil)
	sess := first.Session.Session
	options, err := h.svc.StartPasskeyRegistration(testContext(t), sess)
	if err != nil {
		t.Fatalf("StartPasskeyRegistration() error = %v", err)
	}
	challenge, handle := publicKeyFields(t, options)
	var stored []byte
	if err = h.pool.QueryRow(testContext(t), "SELECT webauthn_user_handle FROM second_factor_policies WHERE user_id = $1", acct.user.ID).Scan(&stored); err != nil {
		t.Fatalf("read policy handle: %v", err)
	}
	if string(handle) != string(stored) {
		t.Fatal("later registration options did not reuse the stored user handle")
	}
	if n := h.count("SELECT count(*) FROM webauthn_ceremonies WHERE session_id = $1 AND proposed_user_handle IS NOT NULL", sess.ID); n != 0 {
		t.Fatalf("later registration stored %d proposed handles", n)
	}
	b := newTestAuthenticator(t)
	credential, err := h.svc.DecodePasskeyRegistration(b.registrationBody(options.CeremonyID, challenge, nil))
	if err != nil {
		t.Fatalf("DecodePasskeyRegistration() error = %v", err)
	}
	later, err := h.svc.CompletePasskeyRegistration(testContext(t), sess, credential)
	if err != nil {
		t.Fatalf("CompletePasskeyRegistration(later) error = %v", err)
	}
	if later.RecoveryCodes != nil || h.count("SELECT count(*) FROM second_factor_recovery_codes WHERE user_id = $1", acct.user.ID) != 10 {
		t.Fatal("later enrollment returned or replaced recovery codes")
	}
	if later.Session.Session.SecondFactorVerifiedAt == nil || !later.Session.Session.SecondFactorVerifiedAt.Equal(*sess.SecondFactorVerifiedAt) {
		t.Fatal("later enrollment did not copy the prior factor proof")
	}
	if h.mails(acct.user.ID, authmail.KindPasskeyAdded) != 1 || h.mails(acct.user.ID, authmail.KindSecondFactorEnabled) != 1 {
		t.Fatal("later enrollment did not enqueue exactly one passkey_added mail")
	}
}

// TestCompetingFirstCompletions_OneWinner races two first-enrollment
// ceremonies from two sessions of one account.
func TestCompetingFirstCompletions_OneWinner(t *testing.T) {
	h := newHarness(t, nil)
	acct := h.newAccount()
	second := h.newSession(acct.user.ID)
	sessions := []store.Session{acct.sess, second}
	credentials := []auth.SecondFactorCredential{
		h.registrationCredential(acct.sess, newTestAuthenticator(t), nil),
		h.registrationCredential(second, newTestAuthenticator(t), nil),
	}
	calls := make([]func() error, 0, 2)
	for i := range sessions {
		calls = append(calls, func() error {
			_, err := h.svc.CompletePasskeyRegistration(testContext(t), sessions[i], credentials[i])
			return err
		})
	}
	results := runConcurrently(t, calls...)
	wins := 0
	for _, err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, auth.ErrSessionInvalid), errors.Is(err, auth.ErrSecondFactorChallengeInvalid):
		default:
			t.Fatalf("losing completion error = %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("winners = %d, want 1", wins)
	}
	if h.count("SELECT count(*) FROM second_factor_policies WHERE user_id = $1", acct.user.ID) != 1 ||
		h.count("SELECT count(*) FROM webauthn_credentials WHERE user_id = $1", acct.user.ID) != 1 ||
		h.count("SELECT count(*) FROM second_factor_recovery_codes WHERE user_id = $1", acct.user.ID) != 10 ||
		h.epoch(acct.user.ID) != 1 || h.mails(acct.user.ID, authmail.KindSecondFactorEnabled) != 1 {
		t.Fatal("competing first completions left more than one enrollment")
	}
}

func TestRegistrationWithProposedHandleAfterPolicy_ChallengeInvalid(t *testing.T) {
	h := newHarness(t, nil)
	acct := h.newAccount()
	sess := h.register(acct.sess, newTestAuthenticator(t), nil).Session.Session
	token, challenge, proposed := make([]byte, 32), make([]byte, 32), make([]byte, 32)
	for _, b := range [][]byte{token, challenge, proposed} {
		if _, err := rand.Read(b); err != nil {
			t.Fatalf("random: %v", err)
		}
	}
	tokenDigest, challengeDigest := sha256.Sum256(token), sha256.Sum256(challenge)
	now := time.Now()
	if _, err := h.q.CreateWebAuthnCeremony(testContext(t), store.CreateWebAuthnCeremonyParams{
		TokenDigest: tokenDigest[:], ChallengeDigest: challengeDigest[:], UserID: acct.user.ID, Purpose: purposeRegistration,
		AuthEpoch: 1, SessionID: &sess.ID, ProposedUserHandle: proposed, CreatedAt: now, ExpiresAt: now.Add(ceremonyLifetime),
	}); err != nil {
		t.Fatalf("create ceremony: %v", err)
	}
	credential, err := h.svc.DecodePasskeyRegistration(newTestAuthenticator(t).registrationBody(b64.EncodeToString(token), challenge, nil))
	if err != nil {
		t.Fatalf("DecodePasskeyRegistration() error = %v", err)
	}
	if _, err = h.svc.CompletePasskeyRegistration(testContext(t), sess, credential); !errors.Is(err, auth.ErrSecondFactorChallengeInvalid) {
		t.Fatalf("completion with a proposed handle after the policy error = %v, want ErrSecondFactorChallengeInvalid", err)
	}
	if h.count("SELECT count(*) FROM webauthn_ceremonies WHERE token_digest = $1 AND consumed_at IS NOT NULL", tokenDigest[:]) != 1 {
		t.Fatal("rejected ceremony was not consumed")
	}
	if h.count("SELECT count(*) FROM webauthn_credentials WHERE user_id = $1", acct.user.ID) != 1 || h.epoch(acct.user.ID) != 1 {
		t.Fatal("rejected ceremony stored a credential or advanced the epoch")
	}
}

func TestDisabledEnrollment_OptionsCreateNothingAndCompletionConsumes(t *testing.T) {
	h := newHarness(t, nil)
	acct := h.newAccount()
	credential := h.registrationCredential(acct.sess, newTestAuthenticator(t), nil)
	disabledOpts := h.opts
	disabledOpts.EnrollmentEnabled = false
	disabled := h.service(disabledOpts)
	if _, err := disabled.StartPasskeyRegistration(testContext(t), acct.sess); !errors.Is(err, auth.ErrSecondFactorEnrollmentDisabled) {
		t.Fatalf("disabled StartPasskeyRegistration() error = %v", err)
	}
	if n := h.count("SELECT count(*) FROM webauthn_ceremonies WHERE user_id = $1", acct.user.ID); n != 1 {
		t.Fatalf("ceremonies after disabled options = %d, want only the enabled one", n)
	}
	// A stale primary proof must not keep a disabled completion from
	// consuming its ceremony.
	if _, err := h.pool.Exec(testContext(t), "UPDATE sessions SET reauthenticated_at = reauthenticated_at - interval '16 minutes' WHERE id = $1", acct.sess.ID); err != nil {
		t.Fatalf("age session: %v", err)
	}
	if _, err := disabled.CompletePasskeyRegistration(testContext(t), acct.sess, credential); !errors.Is(err, auth.ErrSecondFactorEnrollmentDisabled) {
		t.Fatalf("disabled completion error = %v, want ErrSecondFactorEnrollmentDisabled", err)
	}
	if h.count("SELECT count(*) FROM webauthn_ceremonies WHERE user_id = $1 AND consumed_at IS NULL", acct.user.ID) != 0 {
		t.Fatal("disabled completion left its ceremony live")
	}
	if h.count("SELECT count(*) FROM webauthn_credentials WHERE user_id = $1", acct.user.ID) != 0 ||
		h.count("SELECT count(*) FROM second_factor_policies WHERE user_id = $1", acct.user.ID) != 0 || h.epoch(acct.user.ID) != 0 {
		t.Fatal("disabled completion stored a factor")
	}
}

func TestRegistrationMailFailure_RollsBackEveryWrite(t *testing.T) {
	h := newHarness(t, nil)
	failing, err := authmail.NewOutbox(newTestRing(t, failingReader{}), time.Now)
	if err != nil {
		t.Fatalf("NewOutbox() error = %v", err)
	}
	opts := h.opts
	opts.Outbox = failing
	h.svc = h.service(opts)
	acct := h.newAccount()
	credential := h.registrationCredential(acct.sess, newTestAuthenticator(t), nil)
	_, err = h.svc.CompletePasskeyRegistration(testContext(t), acct.sess, credential)
	if err == nil || errors.Is(err, auth.ErrSecondFactorVerificationFailed) || errors.Is(err, auth.ErrSecondFactorChallengeInvalid) {
		t.Fatalf("completion with a failing outbox error = %v, want an internal error", err)
	}
	if h.count("SELECT count(*) FROM second_factor_policies WHERE user_id = $1", acct.user.ID) != 0 ||
		h.count("SELECT count(*) FROM webauthn_credentials WHERE user_id = $1", acct.user.ID) != 0 ||
		h.count("SELECT count(*) FROM second_factor_recovery_codes WHERE user_id = $1", acct.user.ID) != 0 ||
		h.count("SELECT count(*) FROM webauthn_ceremonies WHERE user_id = $1 AND consumed_at IS NOT NULL", acct.user.ID) != 0 ||
		h.count("SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NOT NULL", acct.user.ID) != 0 ||
		h.epoch(acct.user.ID) != 0 {
		t.Fatal("a failed notification left enrollment writes behind")
	}
}

func TestManagementRequiresRecentReauth(t *testing.T) {
	h := newHarness(t, nil)
	acct := h.newAccount()
	if _, err := h.pool.Exec(testContext(t), "UPDATE sessions SET reauthenticated_at = reauthenticated_at - interval '16 minutes' WHERE id = $1", acct.sess.ID); err != nil {
		t.Fatalf("age session: %v", err)
	}
	if _, err := h.svc.StartPasskeyRegistration(testContext(t), acct.sess); !errors.Is(err, auth.ErrReauthRequired) {
		t.Fatalf("StartPasskeyRegistration(stale reauth) error = %v, want ErrReauthRequired", err)
	}
}

func TestAssertion_LoginIssuesSessionWithBothProofs(t *testing.T) {
	h := newHarness(t, nil)
	acct := h.newAccount()
	a := newTestAuthenticator(t)
	reg := h.register(acct.sess, a, nil)
	pending := h.newPending(acct.user.ID, nil)
	var handle []byte
	if err := h.pool.QueryRow(testContext(t), "SELECT webauthn_user_handle FROM second_factor_policies WHERE user_id = $1", acct.user.ID).Scan(&handle); err != nil {
		t.Fatalf("read handle: %v", err)
	}
	issue, err := h.complete(pending.RawToken, nil, h.assertionCredential(pending.RawToken, nil, a, 1, func(s *assertionSpec) { s.userHandle = handle }))
	if err != nil || issue == nil {
		t.Fatalf("CompletePending(login) = %v, %v", issue, err)
	}
	if issue.Session.SecondFactorVerifiedAt == nil || !issue.Session.SecondFactorVerifiedAt.Equal(issue.Session.ReauthenticatedAt) || issue.Session.AuthEpoch != 1 {
		t.Fatalf("login session = %+v; want both proofs at completion and the current epoch", issue.Session)
	}
	if h.count("SELECT count(*) FROM webauthn_credentials WHERE id = $1 AND sign_count = 1 AND last_used_at IS NOT NULL", reg.Passkey.ID) != 1 {
		t.Fatal("successful assertion did not advance the counter and last use")
	}
	if h.count("SELECT count(*) FROM pending_authentications WHERE id = $1 AND consumed_at IS NOT NULL", pending.Pending.ID) != 1 {
		t.Fatal("successful assertion left the pending row live")
	}
}

func TestAssertion_ReauthRefreshesOnlyTheBoundSession(t *testing.T) {
	h := newHarness(t, nil)
	acct := h.newAccount()
	a := newTestAuthenticator(t)
	sess := h.register(acct.sess, a, nil).Session.Session
	pending := h.newPending(acct.user.ID, &sess.ID)
	issue, err := h.complete(pending.RawToken, &sess.ID, h.assertionCredential(pending.RawToken, &sess.ID, a, 1, nil))
	if err != nil || issue != nil {
		t.Fatalf("CompletePending(reauth) = %v, %v; want no new session", issue, err)
	}
	if h.count("SELECT count(*) FROM sessions WHERE id = $1 AND second_factor_verified_at > $2 AND reauthenticated_at = second_factor_verified_at", sess.ID, *sess.SecondFactorVerifiedAt) != 1 {
		t.Fatal("reauth completion did not refresh both proofs on the bound session")
	}
	if h.count("SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL", acct.user.ID) != 1 {
		t.Fatal("reauth completion issued another session")
	}
}

func TestAssertion_ReplayAndConcurrentCompletionFailClosed(t *testing.T) {
	h := newHarness(t, nil)
	acct := h.newAccount()
	a := newTestAuthenticator(t)
	h.register(acct.sess, a, nil)
	first := h.newPending(acct.user.ID, nil)
	credential := h.assertionCredential(first.RawToken, nil, a, 1, nil)
	if _, err := h.complete(first.RawToken, nil, credential); err != nil {
		t.Fatalf("first completion error = %v", err)
	}
	if _, err := h.complete(first.RawToken, nil, credential); !errors.Is(err, auth.ErrPendingAuthenticationRequired) {
		t.Fatalf("replay on the consumed pending row error = %v", err)
	}
	second := h.newPending(acct.user.ID, nil)
	if _, err := h.complete(second.RawToken, nil, credential); !errors.Is(err, auth.ErrSecondFactorChallengeInvalid) {
		t.Fatalf("replay on another pending row error = %v, want ErrSecondFactorChallengeInvalid", err)
	}
	if h.count("SELECT failed_attempts FROM pending_authentications WHERE id = $1", second.Pending.ID) != 0 {
		t.Fatal("a foreign ceremony counted as a failed attempt")
	}
	third := h.newPending(acct.user.ID, nil)
	racing := h.assertionCredential(third.RawToken, nil, a, 2, nil)
	complete := func() error {
		_, err := h.complete(third.RawToken, nil, racing)
		return err
	}
	wins := 0
	for _, err := range runConcurrently(t, complete, complete) {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, auth.ErrPendingAuthenticationRequired), errors.Is(err, auth.ErrSecondFactorChallengeInvalid):
		default:
			t.Fatalf("losing concurrent completion error = %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("concurrent completion winners = %d, want 1", wins)
	}
}

// TestFailedAssertion_CommitsOnlyFailureWrites checks the pending rule: a
// failed verification commits the ceremony consumption, the counter event,
// and the counted attempt, and never the credential counter or a session.
func TestFailedAssertion_CommitsOnlyFailureWrites(t *testing.T) {
	h := newHarness(t, nil)
	acct := h.newAccount()
	a := newTestAuthenticator(t)
	reg := h.register(acct.sess, a, func(s *registrationSpec) { s.counter = 5 })
	pending := h.newPending(acct.user.ID, nil)
	sessionsBefore := h.count("SELECT count(*) FROM sessions WHERE user_id = $1", acct.user.ID)

	_, err := h.complete(pending.RawToken, nil, h.assertionCredential(pending.RawToken, nil, a, 5, nil))
	var failure *auth.PendingVerificationFailure
	if !errors.As(err, &failure) || failure.Exhausted {
		t.Fatalf("non-increasing counter error = %v, want a counted verification failure", err)
	}
	if h.count("SELECT count(*) FROM authentication_security_events WHERE user_id = $1 AND passkey_id = $2 AND stored_counter = 5 AND received_counter = 5", acct.user.ID, reg.Passkey.ID) != 1 {
		t.Fatal("non-increasing counter did not record one security event")
	}
	_, err = h.complete(pending.RawToken, nil, h.assertionCredential(pending.RawToken, nil, a, 6, func(s *assertionSpec) { s.badSig = true }))
	if !errors.As(err, &failure) {
		t.Fatalf("bad signature error = %v, want a counted verification failure", err)
	}
	if h.count("SELECT count(*) FROM webauthn_credentials WHERE id = $1 AND sign_count = 5 AND last_used_at IS NULL", reg.Passkey.ID) != 1 {
		t.Fatal("a failed assertion wrote the credential counter")
	}
	if h.count("SELECT count(*) FROM sessions WHERE user_id = $1", acct.user.ID) != sessionsBefore {
		t.Fatal("a failed assertion wrote a session")
	}
	if h.count("SELECT failed_attempts FROM pending_authentications WHERE id = $1 AND consumed_at IS NULL", pending.Pending.ID) != 2 {
		t.Fatal("failed assertions were not counted on the live pending row")
	}
	if h.count("SELECT count(*) FROM webauthn_ceremonies WHERE pending_authentication_id = $1 AND consumed_at IS NOT NULL", pending.Pending.ID) != 2 {
		t.Fatal("failed assertions did not consume their ceremonies")
	}
	if h.count("SELECT count(*) FROM authentication_security_events WHERE user_id = $1", acct.user.ID) != 1 {
		t.Fatal("a bad signature recorded a counter event")
	}
}

func TestCounterEventFailure_RollsBackEveryWrite(t *testing.T) {
	h := newHarness(t, nil)
	acct := h.newAccount()
	a := newTestAuthenticator(t)
	reg := h.register(acct.sess, a, func(s *registrationSpec) { s.counter = 5 })
	pending := h.newPending(acct.user.ID, nil)
	credential := h.assertionCredential(pending.RawToken, nil, a, 4, nil)
	h.svc.counterEventProbe = func() error { return errors.New("injected event failure") }
	_, err := h.complete(pending.RawToken, nil, credential)
	var failure *auth.PendingVerificationFailure
	if err == nil || errors.As(err, &failure) {
		t.Fatalf("event insert failure error = %v, want an internal error", err)
	}
	if h.count("SELECT count(*) FROM webauthn_ceremonies WHERE pending_authentication_id = $1 AND consumed_at IS NOT NULL", pending.Pending.ID) != 0 ||
		h.count("SELECT failed_attempts FROM pending_authentications WHERE id = $1", pending.Pending.ID) != 0 ||
		h.count("SELECT count(*) FROM authentication_security_events WHERE user_id = $1", acct.user.ID) != 0 ||
		h.count("SELECT count(*) FROM webauthn_credentials WHERE id = $1 AND sign_count = 5", reg.Passkey.ID) != 1 {
		t.Fatal("a failed counter event left writes behind")
	}
}

func TestFifthFailedAttempt_ExhaustsAndNotifies(t *testing.T) {
	h := newHarness(t, nil)
	acct := h.newAccount()
	a := newTestAuthenticator(t)
	h.register(acct.sess, a, nil)
	pending := h.newPending(acct.user.ID, nil)
	var failure *auth.PendingVerificationFailure
	for attempt := 1; attempt <= 5; attempt++ {
		_, err := h.complete(pending.RawToken, nil, h.assertionCredential(pending.RawToken, nil, a, uint32(attempt), func(s *assertionSpec) { s.badSig = true }))
		if !errors.As(err, &failure) || failure.Exhausted != (attempt == 5) {
			t.Fatalf("attempt %d error = %v", attempt, err)
		}
	}
	if h.mails(acct.user.ID, authmail.KindSecondFactorAttemptsExhausted) != 1 {
		t.Fatal("exhaustion did not enqueue one notification")
	}
	if _, err := h.svc.StartPasskeyAssertion(testContext(t), pending.RawToken, nil); !errors.Is(err, auth.ErrPendingAuthenticationRequired) {
		t.Fatalf("options after exhaustion error = %v", err)
	}
}

func fakeOtherFactor(context.Context, *store.Queries, uuid.UUID) (int, error) { return 1, nil }

// TestFinalPasskeyRemoval_KeepsPolicyWhileAnotherFactorRemains uses a fake
// second counter to prove removal counts every factor type.
func TestFinalPasskeyRemoval_KeepsPolicyWhileAnotherFactorRemains(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.Counters = []FactorCounter{PasskeyCounter, fakeOtherFactor} })
	acct := h.newAccount()
	reg := h.register(acct.sess, newTestAuthenticator(t), nil)
	sess := reg.Session.Session
	issue, err := h.svc.RemovePasskey(testContext(t), sess, reg.Passkey.ID)
	if err != nil {
		t.Fatalf("RemovePasskey() error = %v", err)
	}
	if h.count("SELECT count(*) FROM second_factor_policies WHERE user_id = $1", acct.user.ID) != 1 ||
		h.count("SELECT count(*) FROM second_factor_recovery_codes WHERE user_id = $1", acct.user.ID) != 10 {
		t.Fatal("removing the last passkey while another factor remains deleted the policy or codes")
	}
	if issue.Session.SecondFactorVerifiedAt == nil || !issue.Session.SecondFactorVerifiedAt.Equal(*sess.SecondFactorVerifiedAt) {
		t.Fatal("non-final removal did not keep the factor proof")
	}
	if h.mails(acct.user.ID, authmail.KindPasskeyRemoved) != 1 || h.mails(acct.user.ID, authmail.KindSecondFactorDisabled) != 0 {
		t.Fatal("non-final removal did not send only passkey_removed")
	}
	methods, err := h.svc.PendingMethods(testContext(t), acct.user.ID)
	if err != nil || len(methods) != 1 || methods[0] != auth.SecondFactorMethodRecovery {
		t.Fatalf("PendingMethods() = %v, %v; want recovery only", methods, err)
	}
	pending := h.newPending(acct.user.ID, nil)
	if _, err = h.svc.StartPasskeyAssertion(testContext(t), pending.RawToken, nil); !errors.Is(err, auth.ErrSecondFactorNotFound) {
		t.Fatalf("assertion options without a passkey error = %v, want ErrSecondFactorNotFound", err)
	}
	if h.count("SELECT count(*) FROM webauthn_ceremonies WHERE pending_authentication_id = $1", pending.Pending.ID) != 0 ||
		h.count("SELECT count(*) FROM pending_authentications WHERE id = $1 AND failed_attempts = 0 AND consumed_at IS NULL", pending.Pending.ID) != 1 {
		t.Fatal("assertion options without a passkey created a ceremony or changed the pending row")
	}
}

func TestFinalPasskeyRemoval_DisablesEnforcement(t *testing.T) {
	h := newHarness(t, nil)
	acct := h.newAccount()
	reg := h.register(acct.sess, newTestAuthenticator(t), nil)
	issue, err := h.svc.RemovePasskey(testContext(t), reg.Session.Session, reg.Passkey.ID)
	if err != nil {
		t.Fatalf("RemovePasskey() error = %v", err)
	}
	if issue.Session.SecondFactorVerifiedAt != nil || issue.Session.AuthEpoch != 2 {
		t.Fatalf("final removal session = %+v; want a cleared factor proof at epoch 2", issue.Session)
	}
	state, err := h.svc.State(testContext(t), acct.user.ID)
	if err != nil || state.Enabled || len(state.Passkeys) != 0 || state.RecoveryCodesRemaining != 0 {
		t.Fatalf("State() after final removal = %+v, %v", state, err)
	}
	if h.mails(acct.user.ID, authmail.KindSecondFactorDisabled) != 1 || h.mails(acct.user.ID, authmail.KindPasskeyRemoved) != 0 {
		t.Fatal("final removal did not send only second_factor_disabled")
	}
}

func TestRemovePasskey_ForeignAndMissingAreNotFound(t *testing.T) {
	h := newHarness(t, nil)
	owner := h.newAccount()
	other := h.newAccount()
	ownerSess := h.register(owner.sess, newTestAuthenticator(t), nil).Session.Session
	foreign := h.register(other.sess, newTestAuthenticator(t), nil)
	for _, id := range []uuid.UUID{foreign.Passkey.ID, uuid.New()} {
		if _, err := h.svc.RemovePasskey(testContext(t), ownerSess, id); !errors.Is(err, auth.ErrSecondFactorNotFound) {
			t.Fatalf("RemovePasskey(%v) error = %v, want ErrSecondFactorNotFound", id, err)
		}
	}
	if h.count("SELECT count(*) FROM webauthn_credentials WHERE id = $1", foreign.Passkey.ID) != 1 || h.epoch(owner.user.ID) != 1 {
		t.Fatal("a rejected removal changed state")
	}
}

// liveAgentAuthority creates one live grant with one refresh token family.
func (h *harness) liveAgentAuthority(userID uuid.UUID) (grantID, tokenID uuid.UUID) {
	h.t.Helper()
	ctx := testContext(h.t)
	created := time.Now().Add(-time.Minute)
	client, err := h.q.CreateOAuthClient(ctx, store.CreateOAuthClientParams{ClientName: "Agent", RedirectURIs: json.RawMessage(`["https://agent.example/callback"]`), CreatedAt: created})
	if err != nil {
		h.t.Fatalf("create client: %v", err)
	}
	grant, err := h.q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: userID, ClientID: client.ID, Scopes: "resumes:read", CreatedAt: created, AuthEpoch: h.epoch(userID)})
	if err != nil {
		h.t.Fatalf("create grant: %v", err)
	}
	digest := make([]byte, 32)
	if _, err = rand.Read(digest); err != nil {
		h.t.Fatalf("random: %v", err)
	}
	token, err := h.q.CreateOAuthToken(ctx, store.CreateOAuthTokenParams{
		TokenDigest: digest, Kind: "refresh", FamilyID: uuid.New(), ClientID: client.ID, UserID: userID, GrantID: grant.ID,
		CreatedAt: created, ExpiresAt: created.Add(24 * time.Hour), FamilyExpiresAt: created.Add(24 * time.Hour),
	})
	if err != nil {
		h.t.Fatalf("create token: %v", err)
	}
	return grant.ID, token.ID
}

func (h *harness) requireAgentAuthorityRevoked(step string, grantID, tokenID uuid.UUID) {
	h.t.Helper()
	if h.count("SELECT count(*) FROM oauth_grants WHERE id = $1 AND revoked_at IS NOT NULL", grantID) != 1 ||
		h.count("SELECT count(*) FROM oauth_tokens WHERE id = $1 AND revoked_at IS NOT NULL", tokenID) != 1 {
		h.t.Fatalf("%s left a live agent grant or token family", step)
	}
}

func TestFactorMutations_RevokeAgentAuthority(t *testing.T) {
	h := newHarness(t, nil)
	acct := h.newAccount()

	grant, token := h.liveAgentAuthority(acct.user.ID)
	first := h.register(acct.sess, newTestAuthenticator(t), nil)
	h.requireAgentAuthorityRevoked("first enrollment", grant, token)

	grant, token = h.liveAgentAuthority(acct.user.ID)
	second := h.register(first.Session.Session, newTestAuthenticator(t), nil)
	h.requireAgentAuthorityRevoked("passkey addition", grant, token)

	grant, token = h.liveAgentAuthority(acct.user.ID)
	codes, err := h.svc.RegenerateRecoveryCodes(testContext(t), second.Session.Session)
	if err != nil {
		t.Fatalf("RegenerateRecoveryCodes() error = %v", err)
	}
	h.requireAgentAuthorityRevoked("recovery-code regeneration", grant, token)

	grant, token = h.liveAgentAuthority(acct.user.ID)
	removed, err := h.svc.RemovePasskey(testContext(t), codes.Session.Session, first.Passkey.ID)
	if err != nil {
		t.Fatalf("RemovePasskey(non-final) error = %v", err)
	}
	h.requireAgentAuthorityRevoked("passkey removal", grant, token)

	grant, token = h.liveAgentAuthority(acct.user.ID)
	if _, err = h.svc.RemovePasskey(testContext(t), removed.Session, second.Passkey.ID); err != nil {
		t.Fatalf("RemovePasskey(final) error = %v", err)
	}
	h.requireAgentAuthorityRevoked("final disablement", grant, token)
	if h.epoch(acct.user.ID) != 5 {
		t.Fatalf("epoch after five factor mutations = %d, want 5", h.epoch(acct.user.ID))
	}
}

func TestActiveFactorCount_SumsEveryCounter(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.Counters = []FactorCounter{PasskeyCounter, fakeOtherFactor} })
	acct := h.newAccount()
	h.register(acct.sess, newTestAuthenticator(t), nil)
	n, err := h.svc.ActiveFactorCount(testContext(t), h.q, acct.user.ID)
	if err != nil || n != 2 {
		t.Fatalf("ActiveFactorCount() = %d, %v; want 2", n, err)
	}
}

// TestState_NeverTornDuringFirstEnrollment reads the state while a first
// enrollment commits and requires every read to be one consistent snapshot:
// never disabled while a passkey or recovery code is visible.
func TestState_NeverTornDuringFirstEnrollment(t *testing.T) {
	h := newHarness(t, nil)
	acct := h.newAccount()
	credential := h.registrationCredential(acct.sess, newTestAuthenticator(t), nil)
	var enrolled atomic.Bool
	const maxReads = 500
	results := runConcurrently(t,
		func() error {
			defer enrolled.Store(true)
			_, err := h.svc.CompletePasskeyRegistration(testContext(t), acct.sess, credential)
			return err
		},
		func() error {
			for i := 0; i < maxReads && !enrolled.Load(); i++ {
				state, err := h.svc.State(testContext(t), acct.user.ID)
				if err != nil {
					return err
				}
				if !state.Enabled && (len(state.Passkeys) > 0 || state.RecoveryCodesRemaining > 0) {
					return errors.New("state read was torn: disabled with a visible factor")
				}
				if state.Enabled && (len(state.Passkeys) != 1 || state.RecoveryCodesRemaining != recoveryCodeCount) {
					return errors.New("state read was torn: enabled without the whole enrollment")
				}
			}
			return nil
		},
	)
	for _, err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	state, err := h.svc.State(testContext(t), acct.user.ID)
	if err != nil || !state.Enabled || len(state.Passkeys) != 1 || state.RecoveryCodesRemaining != recoveryCodeCount {
		t.Fatalf("State() after enrollment = %+v, %v", state, err)
	}
}
