package secondfactor

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// testTOTPKeyRing returns a ring with one active key, drawn from the fixed
// keys totp_crypto_test.go already establishes.
func testTOTPKeyRing(t *testing.T) *TOTPKeyRing {
	t.Helper()
	ring, err := NewTOTPKeyRing(testTOTPKeySeqText, "", rand.Reader)
	if err != nil {
		t.Fatalf("NewTOTPKeyRing() error = %v", err)
	}
	return ring
}

// newTOTPTestClock starts at the wall clock, because pending authentication,
// sessions, and the database's own expiry checks run on real time; tests
// then advance it for cool-down and reset windows.
func newTOTPTestClock() *testutil.Clock {
	return testutil.NewClock(time.Now().UTC().Truncate(time.Second))
}

// newTOTPHarness builds a harness with TOTP enrollment enabled over an
// injectable clock, so cool-down and reset-window tests need no real wait.
func newTOTPHarness(t *testing.T, clock *testutil.Clock, mutate func(*Options)) *harness {
	t.Helper()
	return newHarness(t, func(o *Options) {
		outbox, err := authmail.NewOutbox(newTestRing(t, rand.Reader), clock.Now)
		if err != nil {
			t.Fatalf("NewOutbox() error = %v", err)
		}
		o.Outbox = outbox
		o.Clock = clock.Now
		o.TOTPKeyRing = testTOTPKeyRing(t)
		o.TOTPEnrollmentEnabled = true
		o.TOTPIssuer = "aboutme.test"
		if mutate != nil {
			mutate(o)
		}
	})
}

// parseTOTPSecret decodes the grouped Base32 display StartTOTPEnrollment
// returns back into raw secret bytes, exactly as a real authenticator app
// would after scanning the QR code.
func parseTOTPSecret(t *testing.T, grouped string) TOTPSecret {
	t.Helper()
	compact := strings.ReplaceAll(grouped, " ", "")
	raw, err := totpBase32.DecodeString(compact)
	if err != nil || len(raw) != totpSecretBytes {
		t.Fatalf("parse totp secret %q: %v", grouped, err)
	}
	var secret TOTPSecret
	copy(secret[:], raw)
	return secret
}

// totpCodeAt computes the six-digit code for secret at the step containing
// at, the same computation VerifyTOTPCode uses internally.
func totpCodeAt(secret TOTPSecret, at time.Time) string {
	return totpCode(secret, uint64(at.Unix())/totpPeriodSeconds)
}

func totpEnrollmentBody(enrollmentID, code string) []byte {
	return []byte(fmt.Sprintf(`{"enrollmentId":%q,"code":%q}`, enrollmentID, code))
}

func totpCodeBody(code string) []byte {
	return []byte(fmt.Sprintf(`{"code":%q}`, code))
}

// startAndCompleteTOTP starts and completes TOTP enrollment or replacement
// for sess with a fresh secret, computing its proof code at now. It returns
// the plaintext secret so a later pending-verification test can compute
// further codes without decrypting storage.
func (h *harness) startAndCompleteTOTP(sess store.Session, now time.Time) (TOTPSecret, auth.TOTPEnrollmentComplete, error) {
	h.t.Helper()
	start, err := h.svc.StartTOTPEnrollment(testContext(h.t), sess)
	if err != nil {
		h.t.Fatalf("StartTOTPEnrollment() error = %v", err)
	}
	secret := parseTOTPSecret(h.t, start.Secret)
	credential, err := h.svc.DecodeTOTPEnrollmentCompletion(totpEnrollmentBody(start.EnrollmentID, totpCodeAt(secret, now)))
	if err != nil {
		h.t.Fatalf("DecodeTOTPEnrollmentCompletion() error = %v", err)
	}
	result, err := h.svc.CompleteTOTPEnrollment(testContext(h.t), sess, credential)
	return secret, result, err
}

// totpCredentialRow is a direct read of the stored row's non-secret columns,
// for assertions the service's own API deliberately never exposes.
type totpCredentialRow struct {
	id             uuid.UUID
	keyID          string
	failedAttempts int32
	cooldownUntil  *time.Time
	lastFailedAt   *time.Time
}

func (h *harness) totpCredentialRow(userID uuid.UUID) totpCredentialRow {
	h.t.Helper()
	var row totpCredentialRow
	err := h.pool.QueryRow(testContext(h.t),
		"SELECT id, key_id, failed_attempts, cooldown_until, last_failed_at FROM totp_credentials WHERE user_id = $1", userID,
	).Scan(&row.id, &row.keyID, &row.failedAttempts, &row.cooldownUntil, &row.lastFailedAt)
	if err != nil {
		h.t.Fatalf("read totp credential: %v", err)
	}
	return row
}

// timePtrEqual compares two nullable timestamps by value: two nil pointers
// are equal, and two non-nil pointers are equal exactly when the instants
// they name are equal, regardless of pointer identity.
func timePtrEqual(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func (h *harness) policyAttemptMailAt(userID uuid.UUID) *time.Time {
	h.t.Helper()
	var at *time.Time
	if err := h.pool.QueryRow(testContext(h.t), "SELECT attempt_mail_at FROM second_factor_policies WHERE user_id = $1", userID).Scan(&at); err != nil {
		h.t.Fatalf("read attempt_mail_at: %v", err)
	}
	return at
}

func (h *harness) pendingFailedAttempts(pendingID uuid.UUID) int32 {
	h.t.Helper()
	var n int32
	if err := h.pool.QueryRow(testContext(h.t), "SELECT failed_attempts FROM pending_authentications WHERE id = $1", pendingID).Scan(&n); err != nil {
		h.t.Fatalf("read pending failed_attempts: %v", err)
	}
	return n
}

// completeTOTPPendingCode drives one pending TOTP verification with code
// against pending, using the fixed test client.
func (h *harness) completeTOTPPendingCode(pendingToken string, sessionID *uuid.UUID, code string) (*auth.SessionIssue, error) {
	h.t.Helper()
	credential, err := h.svc.DecodeTOTPCode(totpCodeBody(code))
	if err != nil {
		h.t.Fatalf("DecodeTOTPCode() error = %v", err)
	}
	return h.svc.CompletePending(testContext(h.t), pendingToken, sessionID, credential, auth.SecondFactorClient{UserAgent: "test-agent", IP: "203.0.113.7"})
}

func TestTOTPStartEnrollment_SupersedesPriorAndChangesNoActiveState(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()

	first, err := h.svc.StartTOTPEnrollment(testContext(t), acct.sess)
	if err != nil {
		t.Fatalf("StartTOTPEnrollment() error = %v", err)
	}
	second, err := h.svc.StartTOTPEnrollment(testContext(t), acct.sess)
	if err != nil {
		t.Fatalf("StartTOTPEnrollment() second error = %v", err)
	}
	if first.EnrollmentID == second.EnrollmentID {
		t.Fatal("second start reused the first enrollment ID")
	}
	if n := h.count("SELECT count(*) FROM totp_enrollments WHERE user_id = $1", acct.user.ID); n != 1 {
		t.Fatalf("live enrollments = %d, want exactly 1 after supersession", n)
	}
	// The superseded enrollment ID is unusable.
	credential, err := h.svc.DecodeTOTPEnrollmentCompletion(totpEnrollmentBody(first.EnrollmentID, totpCodeAt(parseTOTPSecret(t, first.Secret), clock.Now())))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, err = h.svc.CompleteTOTPEnrollment(testContext(t), acct.sess, credential); !errors.Is(err, auth.ErrTOTPEnrollmentInvalid) {
		t.Fatalf("complete superseded enrollment error = %v, want ErrTOTPEnrollmentInvalid", err)
	}
	if n := h.count("SELECT count(*) FROM totp_credentials WHERE user_id = $1", acct.user.ID); n != 0 {
		t.Fatalf("credentials after start = %d, want 0: starting enrollment changes no active state", n)
	}
	if h.epoch(acct.user.ID) != 0 {
		t.Fatal("starting enrollment advanced the epoch")
	}
}

func TestTOTPCompleteEnrollment_FirstFactorCreatesPolicyAndSendsSecondFactorEnabled(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()

	_, result, err := h.startAndCompleteTOTP(acct.sess, clock.Now())
	if err != nil {
		t.Fatalf("startAndCompleteTOTP() error = %v", err)
	}
	if len(result.RecoveryCodes) != recoveryCodeCount {
		t.Fatalf("first completion recovery codes = %d, want %d", len(result.RecoveryCodes), recoveryCodeCount)
	}
	if result.Session.RawToken == "" {
		t.Fatal("first completion returned no replacement session")
	}
	if h.mails(acct.user.ID, authmail.KindSecondFactorEnabled) != 1 {
		t.Fatal("first completion did not send second_factor_enabled")
	}
	if h.epoch(acct.user.ID) != 1 {
		t.Fatal("first completion did not advance the epoch")
	}
	enabled, err := h.svc.TOTPState(testContext(t), acct.user.ID)
	if err != nil || !enabled {
		t.Fatalf("TOTPState() = %v, %v; want true", enabled, err)
	}
	if n := h.count("SELECT count(*) FROM totp_enrollments WHERE user_id = $1", acct.user.ID); n != 0 {
		t.Fatalf("enrollment rows after completion = %d, want 0", n)
	}
}

func TestTOTPCompleteEnrollment_AddedToPasskeyEnrolledAccountSendsTOTPAdded(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	reg := h.register(acct.sess, newTestAuthenticator(t), nil)

	_, result, err := h.startAndCompleteTOTP(reg.Session.Session, clock.Now())
	if err != nil {
		t.Fatalf("startAndCompleteTOTP() error = %v", err)
	}
	if result.RecoveryCodes != nil {
		t.Fatalf("adding totp to an enrolled account returned recovery codes = %v, want none", result.RecoveryCodes)
	}
	if h.mails(acct.user.ID, authmail.KindTOTPAdded) != 1 {
		t.Fatal("adding totp did not send totp_added")
	}
	// The passkey registration above already sent the account's one
	// second_factor_enabled mail as its first factor; adding totp as a
	// second factor must not send a second one.
	if h.mails(acct.user.ID, authmail.KindSecondFactorEnabled) != 1 {
		t.Fatal("adding totp to an already-enrolled account sent an extra second_factor_enabled")
	}
}

func TestTOTPCompleteEnrollment_ReplacementPreservesIdentitySendsTOTPReplacedAndResetsBudget(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	_, first, err := h.startAndCompleteTOTP(acct.sess, clock.Now())
	if err != nil {
		t.Fatalf("first startAndCompleteTOTP() error = %v", err)
	}
	original := h.totpCredentialRow(acct.user.ID)

	// Drive one failure so replacement's budget reset is observable.
	pending := h.newPending(acct.user.ID, nil)
	if _, err = h.completeTOTPPendingCode(pending.RawToken, nil, "000000"); err == nil {
		t.Fatal("wrong code unexpectedly succeeded")
	}
	if h.totpCredentialRow(acct.user.ID).failedAttempts != 1 {
		t.Fatal("primed failure did not record")
	}

	_, result, err := h.startAndCompleteTOTP(first.Session.Session, clock.Now())
	if err != nil {
		t.Fatalf("replacement startAndCompleteTOTP() error = %v", err)
	}
	if result.RecoveryCodes != nil {
		t.Fatal("replacement returned recovery codes, want none")
	}
	if h.mails(acct.user.ID, authmail.KindTOTPReplaced) != 1 {
		t.Fatal("replacement did not send totp_replaced")
	}
	replaced := h.totpCredentialRow(acct.user.ID)
	if replaced.id != original.id {
		t.Fatalf("replaced credential id = %s, want the original id %s preserved", replaced.id, original.id)
	}
	if replaced.failedAttempts != 0 || replaced.cooldownUntil != nil || replaced.lastFailedAt != nil {
		t.Fatalf("replaced credential budget = %+v, want fully reset", replaced)
	}
}

func TestTOTPCompleteEnrollment_InvalidCodeIsVerificationFailedAndPreservesEnrollment(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	start, err := h.svc.StartTOTPEnrollment(testContext(t), acct.sess)
	if err != nil {
		t.Fatalf("StartTOTPEnrollment() error = %v", err)
	}
	credential, err := h.svc.DecodeTOTPEnrollmentCompletion(totpEnrollmentBody(start.EnrollmentID, "000000"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, err = h.svc.CompleteTOTPEnrollment(testContext(t), acct.sess, credential); !errors.Is(err, auth.ErrSecondFactorVerificationFailed) {
		t.Fatalf("invalid code error = %v, want ErrSecondFactorVerificationFailed", err)
	}
	if n := h.count("SELECT count(*) FROM totp_enrollments WHERE user_id = $1", acct.user.ID); n != 1 {
		t.Fatalf("live enrollments after a wrong code = %d, want 1: the row must survive for a retry", n)
	}
	// A subsequent correct code still completes the same enrollment.
	secret := parseTOTPSecret(t, start.Secret)
	good, err := h.svc.DecodeTOTPEnrollmentCompletion(totpEnrollmentBody(start.EnrollmentID, totpCodeAt(secret, clock.Now())))
	if err != nil {
		t.Fatalf("decode good code: %v", err)
	}
	if _, err = h.svc.CompleteTOTPEnrollment(testContext(t), acct.sess, good); err != nil {
		t.Fatalf("retry with the correct code: %v", err)
	}
}

func TestTOTPCompleteEnrollment_MismatchedBindingIsEnrollmentInvalid(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	start, err := h.svc.StartTOTPEnrollment(testContext(t), acct.sess)
	if err != nil {
		t.Fatalf("StartTOTPEnrollment() error = %v", err)
	}
	secret := parseTOTPSecret(t, start.Secret)
	code := totpCodeAt(secret, clock.Now())

	unknown, err := h.svc.DecodeTOTPEnrollmentCompletion(totpEnrollmentBody(strings.Repeat("A", 43), code))
	if err != nil {
		t.Fatalf("decode unknown: %v", err)
	}
	if _, err = h.svc.CompleteTOTPEnrollment(testContext(t), acct.sess, unknown); !errors.Is(err, auth.ErrTOTPEnrollmentInvalid) {
		t.Fatalf("unknown enrollment id error = %v, want ErrTOTPEnrollmentInvalid", err)
	}

	other := h.newAccount()
	foreignCredential, err := h.svc.DecodeTOTPEnrollmentCompletion(totpEnrollmentBody(start.EnrollmentID, code))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, err = h.svc.CompleteTOTPEnrollment(testContext(t), other.sess, foreignCredential); !errors.Is(err, auth.ErrTOTPEnrollmentInvalid) {
		t.Fatalf("wrong account session error = %v, want ErrTOTPEnrollmentInvalid", err)
	}

	clock.Advance(totpEnrollmentLifetime + time.Second)
	expiredCredential, err := h.svc.DecodeTOTPEnrollmentCompletion(totpEnrollmentBody(start.EnrollmentID, code))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, err = h.svc.CompleteTOTPEnrollment(testContext(t), acct.sess, expiredCredential); !errors.Is(err, auth.ErrTOTPEnrollmentInvalid) {
		t.Fatalf("expired enrollment error = %v, want ErrTOTPEnrollmentInvalid", err)
	}
}

func TestTOTPCompleteEnrollment_DisabledConsumesEnrollmentAndReturnsDisabled(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	start, err := h.svc.StartTOTPEnrollment(testContext(t), acct.sess)
	if err != nil {
		t.Fatalf("StartTOTPEnrollment() error = %v", err)
	}
	disabledOpts := h.opts
	disabledOpts.TOTPEnrollmentEnabled = false
	disabled := h.service(disabledOpts)

	secret := parseTOTPSecret(t, start.Secret)
	credential, err := disabled.DecodeTOTPEnrollmentCompletion(totpEnrollmentBody(start.EnrollmentID, totpCodeAt(secret, clock.Now())))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, err = disabled.CompleteTOTPEnrollment(testContext(t), acct.sess, credential); !errors.Is(err, auth.ErrSecondFactorEnrollmentDisabled) {
		t.Fatalf("disabled completion error = %v, want ErrSecondFactorEnrollmentDisabled", err)
	}
	if n := h.count("SELECT count(*) FROM totp_enrollments WHERE user_id = $1", acct.user.ID); n != 0 {
		t.Fatalf("enrollments after disabled completion = %d, want 0: the matching row is consumed", n)
	}
	if n := h.count("SELECT count(*) FROM totp_credentials WHERE user_id = $1", acct.user.ID); n != 0 {
		t.Fatal("disabled completion installed a credential")
	}
	if h.epoch(acct.user.ID) != 0 {
		t.Fatal("disabled completion advanced the epoch")
	}
}

func TestTOTPCompleteEnrollment_ConcurrentCompletionHasOneWinner(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	start, err := h.svc.StartTOTPEnrollment(testContext(t), acct.sess)
	if err != nil {
		t.Fatalf("StartTOTPEnrollment() error = %v", err)
	}
	secret := parseTOTPSecret(t, start.Secret)
	code := totpCodeAt(secret, clock.Now())

	attempt := func() error {
		credential, decErr := h.svc.DecodeTOTPEnrollmentCompletion(totpEnrollmentBody(start.EnrollmentID, code))
		if decErr != nil {
			return decErr
		}
		_, completeErr := h.svc.CompleteTOTPEnrollment(testContext(t), acct.sess, credential)
		return completeErr
	}
	results := runConcurrently(t, attempt, attempt)
	// The winner's commit advances the epoch and revokes acct.sess, so the
	// loser can observe either an enrollment row already consumed by the
	// winner or, when it locks the account after the winner committed, its
	// own now-superseded session, exactly the two losing outcomes
	// TestCompetingFirstCompletions_OneWinner accepts for the same race
	// over one session.
	successes, losses := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, auth.ErrTOTPEnrollmentInvalid), errors.Is(err, auth.ErrSessionInvalid):
			losses++
		default:
			t.Fatalf("concurrent completion unexpected error: %v", err)
		}
	}
	if successes != 1 || losses != 1 {
		t.Fatalf("concurrent completions = %d success, %d loss; want exactly one winner", successes, losses)
	}
	if n := h.count("SELECT count(*) FROM totp_credentials WHERE user_id = $1", acct.user.ID); n != 1 {
		t.Fatalf("credentials after the race = %d, want exactly 1", n)
	}
}

func TestTOTPPendingLogin_ValidCodeIssuesSessionAndStepIsSingleUse(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	secret, _, err := h.startAndCompleteTOTP(acct.sess, clock.Now())
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}

	clock.Advance(totpPeriodSeconds * time.Second)
	code := totpCodeAt(secret, clock.Now())
	pending := h.newPending(acct.user.ID, nil)
	issue, err := h.completeTOTPPendingCode(pending.RawToken, nil, code)
	if err != nil {
		t.Fatalf("CompletePending() error = %v", err)
	}
	if issue == nil || issue.RawToken == "" {
		t.Fatal("login completion returned no session")
	}

	// The same code is single-use: a replay on a fresh pending row fails and
	// counts as one attempt.
	replay := h.newPending(acct.user.ID, nil)
	_, replayErr := h.completeTOTPPendingCode(replay.RawToken, nil, code)
	var failure *auth.PendingVerificationFailure
	if !errors.As(replayErr, &failure) {
		t.Fatalf("replay error = %v, want a counted PendingVerificationFailure", replayErr)
	}
	if h.totpCredentialRow(acct.user.ID).failedAttempts != 1 {
		t.Fatal("replay did not count as one credential failure")
	}
}

func TestTOTPPendingReauth_ValidCodeUpdatesBoundSession(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	secret, result, err := h.startAndCompleteTOTP(acct.sess, clock.Now())
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	sess := result.Session.Session

	clock.Advance(totpPeriodSeconds * time.Second)
	pending := h.newPending(acct.user.ID, &sess.ID)
	issue, err := h.completeTOTPPendingCode(pending.RawToken, &sess.ID, totpCodeAt(secret, clock.Now()))
	if err != nil {
		t.Fatalf("reauth CompletePending() error = %v", err)
	}
	if issue != nil {
		t.Fatal("reauth completion returned a new session, want the bound session updated in place")
	}
}

func TestTOTPPending_NoCredentialIsFactorNotFoundWithoutCountingFailure(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	pending := h.newPending(acct.user.ID, nil)
	_, err := h.completeTOTPPendingCode(pending.RawToken, nil, "123456")
	if !errors.Is(err, auth.ErrSecondFactorNotFound) {
		t.Fatalf("no-credential error = %v, want ErrSecondFactorNotFound", err)
	}
	if n := h.pendingFailedAttempts(pending.Pending.ID); n != 0 {
		t.Fatalf("pending failed_attempts = %d, want 0: no credential counts no failure", n)
	}
}

func TestTOTPPending_UnknownKeyFailsClosedWithoutCountingFailure(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	if _, _, err := h.startAndCompleteTOTP(acct.sess, clock.Now()); err != nil {
		t.Fatalf("enroll: %v", err)
	}

	strangerOpts := h.opts
	strangerRing, err := NewTOTPKeyRing(testTOTPKeyOnesText, "", rand.Reader)
	if err != nil {
		t.Fatalf("NewTOTPKeyRing() error = %v", err)
	}
	strangerOpts.TOTPKeyRing = strangerRing
	stranger := h.service(strangerOpts)

	clock.Advance(totpPeriodSeconds * time.Second)
	pending := h.newPending(acct.user.ID, nil)
	credential, err := stranger.DecodeTOTPCode(totpCodeBody("123456"))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	before := h.totpCredentialRow(acct.user.ID)
	_, completeErr := stranger.CompletePending(testContext(t), pending.RawToken, nil, credential, auth.SecondFactorClient{UserAgent: "test-agent", IP: "203.0.113.7"})
	if completeErr == nil {
		t.Fatal("verification under an unknown key succeeded, want a closed failure")
	}
	var failure *auth.PendingVerificationFailure
	if errors.As(completeErr, &failure) {
		t.Fatal("a key failure must not count as a pending attempt")
	}
	after := h.totpCredentialRow(acct.user.ID)
	if after.failedAttempts != before.failedAttempts || after.keyID != before.keyID {
		t.Fatalf("credential row changed on a key failure: before %+v, after %+v", before, after)
	}
}

func TestTOTPPending_LazyReencryptsPreviousKeyRowOnSuccess(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	secret, _, err := h.startAndCompleteTOTP(acct.sess, clock.Now())
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	sealedKeyID := h.totpCredentialRow(acct.user.ID).keyID

	rotatedRing, err := NewTOTPKeyRing(testTOTPKeyOnesText, testTOTPKeySeqText, rand.Reader)
	if err != nil {
		t.Fatalf("NewTOTPKeyRing(rotated) error = %v", err)
	}
	if rotatedRing.ActiveKeyID() == sealedKeyID {
		t.Fatal("fixture bug: the rotated ring's active key must differ from the sealed key")
	}
	rotatedOpts := h.opts
	rotatedOpts.TOTPKeyRing = rotatedRing
	rotated := h.service(rotatedOpts)

	clock.Advance(totpPeriodSeconds * time.Second)
	pending := h.newPending(acct.user.ID, nil)
	credential, err := rotated.DecodeTOTPCode(totpCodeBody(totpCodeAt(secret, clock.Now())))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, err = rotated.CompletePending(testContext(t), pending.RawToken, nil, credential, auth.SecondFactorClient{UserAgent: "test-agent", IP: "203.0.113.7"}); err != nil {
		t.Fatalf("CompletePending() under the rotated ring error = %v", err)
	}
	after := h.totpCredentialRow(acct.user.ID)
	if after.keyID != rotatedRing.ActiveKeyID() {
		t.Fatalf("credential key_id after verification = %q, want the rotated active key %q", after.keyID, rotatedRing.ActiveKeyID())
	}
	if after.keyID == sealedKeyID {
		t.Fatal("credential was not re-encrypted off the previous key")
	}
}

// TestTOTPPending_FailureBudgetEscalatesCapsMailAndPreservesOtherMethods
// proves the whole per-account failure budget: escalation to a cool-down,
// the one-per-account-per-hour exhaustion-mail cap shared with the pending
// exhaustion path, a live cool-down blocking even a valid code while
// leaving passkeys usable, and the 24-hour-since-last-failure reset rule
// (docs/design/totp-second-factor-contract.md#per-account-totp-failure-budget;
// AC-AUTH-028).
func TestTOTPPending_FailureBudgetEscalatesCapsMailAndPreservesOtherMethods(t *testing.T) {
	epoch := testutil.Epoch
	clock := testutil.NewClock(epoch)
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	authenticator := newTestAuthenticator(t)
	reg := h.register(acct.sess, authenticator, nil)
	secret, _, err := h.startAndCompleteTOTP(reg.Session.Session, clock.Now())
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}

	// Four failures on one pending row: no cool-down yet, the pending row is
	// not exhausted.
	pending := h.newPending(acct.user.ID, nil)
	for i := 1; i <= 4; i++ {
		if _, err = h.completeTOTPPendingCode(pending.RawToken, nil, "000000"); err == nil {
			t.Fatalf("failure %d unexpectedly succeeded", i)
		}
		row := h.totpCredentialRow(acct.user.ID)
		if row.failedAttempts != int32(i) || row.cooldownUntil != nil {
			t.Fatalf("failure %d credential row = %+v, want failed_attempts %d and no cool-down", i, row, i)
		}
	}

	// The fifth failure is also the pending row's fifth: both the credential
	// cool-down and the pending exhaustion fire from the same transaction,
	// and the shared cap must send exactly one exhaustion mail.
	_, err = h.completeTOTPPendingCode(pending.RawToken, nil, "000000")
	var failure *auth.PendingVerificationFailure
	if !errors.As(err, &failure) || !failure.Exhausted {
		t.Fatalf("fifth failure error = %v, want an exhausting PendingVerificationFailure", err)
	}
	row := h.totpCredentialRow(acct.user.ID)
	if row.failedAttempts != 5 || row.cooldownUntil == nil || !row.cooldownUntil.Equal(epoch.Add(15*time.Minute)) {
		t.Fatalf("fifth failure credential row = %+v, want failed_attempts 5 and a 15-minute cool-down", row)
	}
	if row.lastFailedAt == nil || !row.lastFailedAt.Equal(epoch) {
		t.Fatalf("fifth failure last_failed_at = %v, want %v", row.lastFailedAt, epoch)
	}
	if h.mails(acct.user.ID, authmail.KindSecondFactorAttemptsExhausted) != 1 {
		t.Fatal("credential cool-down and pending exhaustion sent more than one capped mail")
	}

	// A live cool-down blocks even a correct code and changes no row.
	fresh := h.newPending(acct.user.ID, nil)
	_, coolingErr := h.completeTOTPPendingCode(fresh.RawToken, nil, totpCodeAt(secret, clock.Now()))
	if coolingErr == nil || errors.As(coolingErr, &failure) {
		t.Fatalf("cooling-down attempt error = %v, want a rejection that is not a counted PendingVerificationFailure", coolingErr)
	}
	if unchanged := h.totpCredentialRow(acct.user.ID); unchanged.failedAttempts != row.failedAttempts ||
		!timePtrEqual(unchanged.cooldownUntil, row.cooldownUntil) || !timePtrEqual(unchanged.lastFailedAt, row.lastFailedAt) {
		t.Fatalf("credential row changed during a cool-down rejection: before %+v, after %+v", row, unchanged)
	}
	if n := h.pendingFailedAttempts(fresh.Pending.ID); n != 0 {
		t.Fatalf("fresh pending failed_attempts after a cool-down rejection = %d, want 0", n)
	}

	// A passkey still completes a fresh pending authentication during the
	// TOTP cool-down.
	passkeyPending := h.newPending(acct.user.ID, nil)
	passkeyCredential := h.assertionCredential(passkeyPending.RawToken, nil, authenticator, 1, nil)
	if _, err = h.complete(passkeyPending.RawToken, nil, passkeyCredential); err != nil {
		t.Fatalf("passkey completion during a totp cool-down: %v", err)
	}

	// After the cool-down elapses but before 24 hours since the last
	// failure, a valid code succeeds but keeps the escalation.
	clock.Set(epoch.Add(16 * time.Minute))
	afterCooldown := h.newPending(acct.user.ID, nil)
	if _, err = h.completeTOTPPendingCode(afterCooldown.RawToken, nil, totpCodeAt(secret, clock.Now())); err != nil {
		t.Fatalf("valid code after the cool-down elapsed: %v", err)
	}
	// resetTOTPBudgetIfDue is a no-op before the 24-hour window, so a valid
	// code neither clears nor extends the now-stale cooldown_until from the
	// fifth failure; it stays past, so it no longer blocks anything
	// (docs/design/totp-second-factor-contract.md#per-account-totp-failure-budget;
	// AC-AUTH-028).
	kept := h.totpCredentialRow(acct.user.ID)
	if kept.failedAttempts != 5 || !timePtrEqual(kept.cooldownUntil, row.cooldownUntil) || kept.lastFailedAt == nil || !kept.lastFailedAt.Equal(epoch) {
		t.Fatalf("credential row after an early valid code = %+v, want the escalation kept", kept)
	}

	// At least 24 hours after the last failure, a valid code resets the
	// budget.
	clock.Set(epoch.Add(24*time.Hour + time.Minute))
	resetPending := h.newPending(acct.user.ID, nil)
	if _, err = h.completeTOTPPendingCode(resetPending.RawToken, nil, totpCodeAt(secret, clock.Now())); err != nil {
		t.Fatalf("valid code after 24 hours: %v", err)
	}
	reset := h.totpCredentialRow(acct.user.ID)
	if reset.failedAttempts != 0 || reset.cooldownUntil != nil || reset.lastFailedAt != nil {
		t.Fatalf("credential row after a 24-hour-late valid code = %+v, want the budget reset", reset)
	}
}

// TestTOTPPending_MailClaimOnlyOnFailuresThatStartACooldown proves the
// attempt-mail window is claimed only by a failure that itself sets a fresh
// cool-down (failed_attempts a multiple of five), not by every failure that
// merely inherits a stale, already-elapsed cooldown_until from an earlier
// one (docs/design/totp-second-factor-contract.md#per-account-totp-failure-budget;
// AC-AUTH-028).
func TestTOTPPending_MailClaimOnlyOnFailuresThatStartACooldown(t *testing.T) {
	epoch := testutil.Epoch
	clock := testutil.NewClock(epoch)
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	if _, _, err := h.startAndCompleteTOTP(acct.sess, clock.Now()); err != nil {
		t.Fatalf("enroll: %v", err)
	}

	fail := func() {
		t.Helper()
		pending := h.newPending(acct.user.ID, nil)
		if _, err := h.completeTOTPPendingCode(pending.RawToken, nil, "000000"); err == nil {
			t.Fatal("wrong code unexpectedly succeeded")
		}
	}

	for i := 1; i <= 5; i++ {
		fail()
	}
	row := h.totpCredentialRow(acct.user.ID)
	if row.failedAttempts != 5 || row.cooldownUntil == nil || !row.cooldownUntil.Equal(epoch.Add(15*time.Minute)) {
		t.Fatalf("after 5 failures = %+v, want a 15-minute cool-down", row)
	}
	firstClaim := h.policyAttemptMailAt(acct.user.ID)
	if firstClaim == nil || !firstClaim.Equal(epoch) {
		t.Fatalf("attempt_mail_at after the first cool-down = %v, want %v", firstClaim, epoch)
	}
	if h.mails(acct.user.ID, authmail.KindSecondFactorAttemptsExhausted) != 1 {
		t.Fatal("want exactly one exhaustion mail after the first cool-down")
	}

	// Past the cool-down, the sixth failure inherits the stale
	// cooldown_until (6 is not a multiple of five) and must not re-claim
	// the mail window.
	clock.Set(epoch.Add(16 * time.Minute))
	fail()
	row = h.totpCredentialRow(acct.user.ID)
	if row.failedAttempts != 6 || row.cooldownUntil == nil || !row.cooldownUntil.Equal(epoch.Add(15*time.Minute)) {
		t.Fatalf("after the sixth failure = %+v, want the stale 15-minute cool-down kept", row)
	}
	if got := h.policyAttemptMailAt(acct.user.ID); !timePtrEqual(got, firstClaim) {
		t.Fatalf("attempt_mail_at after the sixth failure = %v, want unchanged %v", got, firstClaim)
	}

	// Once an hour has passed since the first claim, failures seven through
	// nine still claim nothing, and the tenth both starts a second
	// cool-down and re-claims the window.
	clock.Set(epoch.Add(61 * time.Minute))
	for i := 7; i <= 9; i++ {
		fail()
		if got := h.policyAttemptMailAt(acct.user.ID); !timePtrEqual(got, firstClaim) {
			t.Fatalf("attempt_mail_at after failure %d = %v, want unchanged %v", i, got, firstClaim)
		}
	}
	fail()
	row = h.totpCredentialRow(acct.user.ID)
	want := epoch.Add(61*time.Minute + 30*time.Minute)
	if row.failedAttempts != 10 || row.cooldownUntil == nil || !row.cooldownUntil.Equal(want) {
		t.Fatalf("after the tenth failure = %+v, want a fresh 30-minute cool-down at %v", row, want)
	}
	if got := h.policyAttemptMailAt(acct.user.ID); got == nil || !got.Equal(epoch.Add(61*time.Minute)) {
		t.Fatalf("attempt_mail_at after the tenth failure = %v, want %v", got, epoch.Add(61*time.Minute))
	}
	if h.mails(acct.user.ID, authmail.KindSecondFactorAttemptsExhausted) != 2 {
		t.Fatal("want exactly two exhaustion mails total after the second cool-down")
	}
}

func TestTOTPRemove_NonFinalWithPasskeyKeepsPolicyAndSendsTOTPRemoved(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	reg := h.register(acct.sess, newTestAuthenticator(t), nil)
	_, enrolled, err := h.startAndCompleteTOTP(reg.Session.Session, clock.Now())
	if err != nil {
		t.Fatalf("enroll totp: %v", err)
	}
	issue, err := h.svc.RemoveTOTP(testContext(t), enrolled.Session.Session)
	if err != nil {
		t.Fatalf("RemoveTOTP() error = %v", err)
	}
	if issue.RawToken == "" {
		t.Fatal("removal returned no replacement session")
	}
	if h.mails(acct.user.ID, authmail.KindTOTPRemoved) != 1 {
		t.Fatal("non-final removal did not send totp_removed")
	}
	if h.mails(acct.user.ID, authmail.KindSecondFactorDisabled) != 0 {
		t.Fatal("non-final removal sent second_factor_disabled")
	}
	if n := h.count("SELECT count(*) FROM second_factor_policies WHERE user_id = $1", acct.user.ID); n != 1 {
		t.Fatal("non-final removal deleted the policy")
	}
	if n := h.count("SELECT count(*) FROM second_factor_recovery_codes WHERE user_id = $1", acct.user.ID); n != recoveryCodeCount {
		t.Fatal("non-final removal deleted recovery codes")
	}
}

func TestTOTPRemove_FinalDisablesEnforcementAndDeletesRecoveryCodes(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	_, enrolled, err := h.startAndCompleteTOTP(acct.sess, clock.Now())
	if err != nil {
		t.Fatalf("enroll totp: %v", err)
	}
	if _, err := h.svc.RemoveTOTP(testContext(t), enrolled.Session.Session); err != nil {
		t.Fatalf("RemoveTOTP() error = %v", err)
	}
	if h.mails(acct.user.ID, authmail.KindSecondFactorDisabled) != 1 {
		t.Fatal("final removal did not send second_factor_disabled")
	}
	if n := h.count("SELECT count(*) FROM second_factor_policies WHERE user_id = $1", acct.user.ID); n != 0 {
		t.Fatal("final removal kept the policy")
	}
	if n := h.count("SELECT count(*) FROM second_factor_recovery_codes WHERE user_id = $1", acct.user.ID); n != 0 {
		t.Fatal("final removal kept recovery codes")
	}
}

func TestTOTPRemove_MissingCredentialIsFactorNotFound(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	reg := h.register(acct.sess, newTestAuthenticator(t), nil)
	if _, err := h.svc.RemoveTOTP(testContext(t), reg.Session.Session); !errors.Is(err, auth.ErrSecondFactorNotFound) {
		t.Fatalf("RemoveTOTP() without a credential error = %v, want ErrSecondFactorNotFound", err)
	}
}

// TestTOTPPendingMethods_FixedOrderPasskeyTOTPRecovery proves the pending
// method list follows the contract's fixed order and lists totp purely from
// credential existence, unaffected by an active cool-down
// ("Release surface"; AC-AUTH-028).
func TestTOTPPendingMethods_FixedOrderPasskeyTOTPRecovery(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	_, enrolled, err := h.startAndCompleteTOTP(acct.sess, clock.Now())
	if err != nil {
		t.Fatalf("enroll totp: %v", err)
	}
	h.register(enrolled.Session.Session, newTestAuthenticator(t), nil)

	methods, err := h.svc.PendingMethods(testContext(t), acct.user.ID)
	if err != nil {
		t.Fatalf("PendingMethods() error = %v", err)
	}
	want := []string{auth.SecondFactorMethodPasskey, auth.SecondFactorMethodTOTP, auth.SecondFactorMethodRecovery}
	if len(methods) != len(want) {
		t.Fatalf("PendingMethods() = %v, want %v", methods, want)
	}
	for i, m := range want {
		if methods[i] != m {
			t.Fatalf("PendingMethods() = %v, want %v", methods, want)
		}
	}

	// Driving the credential into cool-down does not remove totp from the
	// list; verification, not listing, is what enforces the cool-down.
	pending := h.newPending(acct.user.ID, nil)
	for i := 0; i < 5; i++ {
		if _, err = h.completeTOTPPendingCode(pending.RawToken, nil, "000000"); err == nil {
			t.Fatalf("failure %d unexpectedly succeeded", i+1)
		}
		if i < 4 {
			pending = h.newPending(acct.user.ID, nil)
		}
	}
	if h.totpCredentialRow(acct.user.ID).cooldownUntil == nil {
		t.Fatal("fixture bug: five failures did not start a cool-down")
	}
	methods, err = h.svc.PendingMethods(testContext(t), acct.user.ID)
	if err != nil || len(methods) != 3 || methods[1] != auth.SecondFactorMethodTOTP {
		t.Fatalf("PendingMethods() during a cool-down = %v, %v; want totp still listed", methods, err)
	}
}

// totpHandleScript intercepts every userHandleBytes-length entropy read once
// armed, returning its preset candidates in order, and always defers to real
// randomness otherwise. StartTOTPEnrollment's enrollment-token draw is also
// exactly userHandleBytes long, so a test arms the script only between
// Start and Complete, isolating interception to
// createTOTPPolicyHandle's candidate draws
// (docs/design/totp-second-factor-contract.md#enrollment-and-replacement-api).
type totpHandleScript struct {
	armed      bool
	candidates [][]byte
	drawn      int
}

func (s *totpHandleScript) Read(p []byte) (int, error) {
	if s.armed && len(p) == userHandleBytes && s.drawn < len(s.candidates) {
		copy(p, s.candidates[s.drawn])
		s.drawn++
		return len(p), nil
	}
	return rand.Read(p)
}

// totpHandleCandidate returns a userHandleBytes candidate that starts with a
// fresh random prefix and ends with marker: the prefix keeps candidates from
// this run from colliding with rows earlier runs committed to the shared test
// database, and the fixed marker keeps candidate1, candidate2, and candidate3
// distinct from each other within one run.
func totpHandleCandidate(t *testing.T, marker byte) []byte {
	t.Helper()
	candidate := make([]byte, userHandleBytes)
	if _, err := rand.Read(candidate[:len(candidate)-1]); err != nil {
		t.Fatalf("rand.Read() error = %v", err)
	}
	candidate[len(candidate)-1] = marker
	return candidate
}

// seedCollidingHandle installs an active TOTP-only policy for a fresh
// foreign account using handle, so a later INSERT of the same handle hits
// second_factor_policies' unique constraint.
func (h *harness) seedCollidingHandle(handle []byte, now time.Time) {
	h.t.Helper()
	foreign := h.newAccount()
	if _, err := h.q.CreateSecondFactorPolicy(testContext(h.t), store.CreateSecondFactorPolicyParams{
		UserID: foreign.user.ID, WebauthnUserHandle: handle, EnabledAt: now,
	}); err != nil {
		h.t.Fatalf("seed colliding policy: %v", err)
	}
}

// TestTOTPCompleteEnrollment_PolicyHandleRetriesUpToThreeCandidates proves
// the first-completion handle collision retry: a collision on candidate one
// retries onto candidate two and persists it, a later passkey registration
// reuses that exact handle, and three collisions in a row return the
// closed 503 with nothing created
// ("Enrollment and replacement API": "When no policy exists..."; AC-AUTH-027).
func TestTOTPCompleteEnrollment_PolicyHandleRetriesUpToThreeCandidates(t *testing.T) {
	t.Run("retries onto the second candidate and persists it", func(t *testing.T) {
		candidate1, candidate2 := totpHandleCandidate(t, 0xA1), totpHandleCandidate(t, 0xA2)
		clock := newTOTPTestClock()
		script := &totpHandleScript{candidates: [][]byte{candidate1, candidate2}}
		h := newTOTPHarness(t, clock, func(o *Options) { o.Entropy = script })
		h.seedCollidingHandle(candidate1, clock.Now())
		acct := h.newAccount()

		start, err := h.svc.StartTOTPEnrollment(testContext(t), acct.sess)
		if err != nil {
			t.Fatalf("StartTOTPEnrollment() error = %v", err)
		}
		secret := parseTOTPSecret(t, start.Secret)
		credential, err := h.svc.DecodeTOTPEnrollmentCompletion(totpEnrollmentBody(start.EnrollmentID, totpCodeAt(secret, clock.Now())))
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		script.armed = true
		result, err := h.svc.CompleteTOTPEnrollment(testContext(t), acct.sess, credential)
		if err != nil {
			t.Fatalf("CompleteTOTPEnrollment() error = %v", err)
		}
		if len(result.RecoveryCodes) != recoveryCodeCount {
			t.Fatalf("recovery codes = %d, want %d", len(result.RecoveryCodes), recoveryCodeCount)
		}
		var persisted []byte
		if readErr := h.pool.QueryRow(testContext(t), "SELECT webauthn_user_handle FROM second_factor_policies WHERE user_id = $1", acct.user.ID).Scan(&persisted); readErr != nil {
			t.Fatalf("read persisted handle: %v", readErr)
		}
		if !bytes.Equal(persisted, candidate2) {
			t.Fatalf("persisted handle = %x, want the second candidate %x", persisted, candidate2)
		}

		options, err := h.svc.StartPasskeyRegistration(testContext(t), result.Session.Session)
		if err != nil {
			t.Fatalf("StartPasskeyRegistration() error = %v", err)
		}
		_, reusedHandle := publicKeyFields(t, options)
		if !bytes.Equal(reusedHandle, candidate2) {
			t.Fatalf("passkey registration handle = %x, want the persisted TOTP handle %x", reusedHandle, candidate2)
		}
	})

	t.Run("three collisions create nothing", func(t *testing.T) {
		candidate1, candidate2, candidate3 := totpHandleCandidate(t, 0xB1), totpHandleCandidate(t, 0xB2), totpHandleCandidate(t, 0xB3)
		clock := newTOTPTestClock()
		script := &totpHandleScript{candidates: [][]byte{candidate1, candidate2, candidate3}}
		h := newTOTPHarness(t, clock, func(o *Options) { o.Entropy = script })
		h.seedCollidingHandle(candidate1, clock.Now())
		h.seedCollidingHandle(candidate2, clock.Now())
		h.seedCollidingHandle(candidate3, clock.Now())
		acct := h.newAccount()

		start, err := h.svc.StartTOTPEnrollment(testContext(t), acct.sess)
		if err != nil {
			t.Fatalf("StartTOTPEnrollment() error = %v", err)
		}
		secret := parseTOTPSecret(t, start.Secret)
		credential, err := h.svc.DecodeTOTPEnrollmentCompletion(totpEnrollmentBody(start.EnrollmentID, totpCodeAt(secret, clock.Now())))
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		script.armed = true
		if _, err = h.svc.CompleteTOTPEnrollment(testContext(t), acct.sess, credential); !errors.Is(err, errTOTPPolicyHandleExhausted) {
			t.Fatalf("three collisions error = %v, want errTOTPPolicyHandleExhausted", err)
		}
		if n := h.count("SELECT count(*) FROM totp_credentials WHERE user_id = $1", acct.user.ID); n != 0 {
			t.Fatalf("credentials after exhaustion = %d, want 0", n)
		}
		if n := h.count("SELECT count(*) FROM second_factor_policies WHERE user_id = $1", acct.user.ID); n != 0 {
			t.Fatalf("policies after exhaustion = %d, want 0", n)
		}
		if n := h.count("SELECT count(*) FROM second_factor_recovery_codes WHERE user_id = $1", acct.user.ID); n != 0 {
			t.Fatalf("recovery codes after exhaustion = %d, want 0", n)
		}
		if h.mails(acct.user.ID, authmail.KindSecondFactorEnabled) != 0 {
			t.Fatal("exhaustion sent second_factor_enabled")
		}
		if h.epoch(acct.user.ID) != 0 {
			t.Fatal("exhaustion advanced the epoch")
		}
		// Handle exhaustion is reported before the enrollment row is
		// deleted, and every policy attempt rolled back to its own
		// savepoint, so the transaction commits with no net change: the
		// enrollment row survives for a retry.
		if n := h.count("SELECT count(*) FROM totp_enrollments WHERE user_id = $1", acct.user.ID); n != 1 {
			t.Fatalf("enrollments after exhaustion = %d, want 1 (unconsumed)", n)
		}
	})
}

// TestTOTPCompleteEnrollment_MailFailureRollsBackEveryWrite mirrors
// TestRegistrationMailFailure_RollsBackEveryWrite for TOTP: a failed
// notification aborts the whole first-completion transaction.
func TestTOTPCompleteEnrollment_MailFailureRollsBackEveryWrite(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	start, err := h.svc.StartTOTPEnrollment(testContext(t), acct.sess)
	if err != nil {
		t.Fatalf("StartTOTPEnrollment() error = %v", err)
	}
	secret := parseTOTPSecret(t, start.Secret)
	credential, err := h.svc.DecodeTOTPEnrollmentCompletion(totpEnrollmentBody(start.EnrollmentID, totpCodeAt(secret, clock.Now())))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	failing, err := authmail.NewOutbox(newTestRing(t, failingReader{}), time.Now)
	if err != nil {
		t.Fatalf("NewOutbox() error = %v", err)
	}
	opts := h.opts
	opts.Outbox = failing
	failingSvc := h.service(opts)

	if _, err = failingSvc.CompleteTOTPEnrollment(testContext(t), acct.sess, credential); err == nil ||
		errors.Is(err, auth.ErrSecondFactorVerificationFailed) || errors.Is(err, auth.ErrTOTPEnrollmentInvalid) {
		t.Fatalf("completion with a failing outbox error = %v, want an internal error", err)
	}
	// The enrollment row's own delete happens inside the same transaction
	// that the outbox failure rolls back, so it survives for a retry, the
	// same way an unconsumed webauthn ceremony survives a failed passkey
	// registration notification.
	if h.count("SELECT count(*) FROM totp_credentials WHERE user_id = $1", acct.user.ID) != 0 ||
		h.count("SELECT count(*) FROM totp_enrollments WHERE user_id = $1", acct.user.ID) != 1 ||
		h.count("SELECT count(*) FROM second_factor_policies WHERE user_id = $1", acct.user.ID) != 0 ||
		h.count("SELECT count(*) FROM second_factor_recovery_codes WHERE user_id = $1", acct.user.ID) != 0 ||
		h.epoch(acct.user.ID) != 0 {
		t.Fatal("a failed notification left totp enrollment writes behind, or discarded the enrollment row it should have kept for a retry")
	}
}

// TestTOTPRemove_MailFailureRollsBackEveryWrite proves a failed removal
// notification leaves the credential, epoch, and session untouched.
func TestTOTPRemove_MailFailureRollsBackEveryWrite(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	_, enrolled, err := h.startAndCompleteTOTP(acct.sess, clock.Now())
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	before := h.totpCredentialRow(acct.user.ID)
	beforeEpoch := h.epoch(acct.user.ID)

	failing, err := authmail.NewOutbox(newTestRing(t, failingReader{}), time.Now)
	if err != nil {
		t.Fatalf("NewOutbox() error = %v", err)
	}
	opts := h.opts
	opts.Outbox = failing
	failingSvc := h.service(opts)

	if _, err = failingSvc.RemoveTOTP(testContext(t), enrolled.Session.Session); err == nil || errors.Is(err, auth.ErrSessionInvalid) {
		t.Fatalf("removal with a failing outbox error = %v, want an internal error", err)
	}
	after := h.totpCredentialRow(acct.user.ID)
	if after.id != before.id || after.failedAttempts != before.failedAttempts {
		t.Fatalf("credential row changed after a failed removal: before %+v, after %+v", before, after)
	}
	if h.epoch(acct.user.ID) != beforeEpoch {
		t.Fatal("a failed notification left the epoch advanced")
	}
	if n := h.count("SELECT count(*) FROM second_factor_policies WHERE user_id = $1", acct.user.ID); n != 1 {
		t.Fatalf("policies after a failed removal = %d, want 1 (unchanged)", n)
	}
}

func TestTOTPCounter_JoinsActiveFactorCount(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	if n, err := h.svc.ActiveFactorCount(testContext(t), h.q, acct.user.ID); err != nil || n != 0 {
		t.Fatalf("ActiveFactorCount() before enrollment = %d, %v; want 0", n, err)
	}
	if _, _, err := h.startAndCompleteTOTP(acct.sess, clock.Now()); err != nil {
		t.Fatalf("enroll: %v", err)
	}
	n, err := h.svc.ActiveFactorCount(testContext(t), h.q, acct.user.ID)
	if err != nil || n != 1 {
		t.Fatalf("ActiveFactorCount() after totp enrollment = %d, %v; want 1", n, err)
	}
}
