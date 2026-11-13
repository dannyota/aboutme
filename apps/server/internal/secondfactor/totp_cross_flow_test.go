package secondfactor

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/auth"
)

func (h *harness) sessionSecondFactorVerifiedAt(sessionID uuid.UUID) *time.Time {
	h.t.Helper()
	var at *time.Time
	if err := h.pool.QueryRow(testContext(h.t), "SELECT second_factor_verified_at FROM sessions WHERE id = $1", sessionID).Scan(&at); err != nil {
		h.t.Fatalf("read second_factor_verified_at: %v", err)
	}
	return at
}

func (h *harness) liveSessionCount(userID uuid.UUID) int {
	h.t.Helper()
	return h.count("SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL", userID)
}

func requirePendingVerificationFailure(t *testing.T, err error) {
	t.Helper()
	var failure *auth.PendingVerificationFailure
	if !errors.As(err, &failure) {
		t.Fatalf("error = %v, want a counted PendingVerificationFailure", err)
	}
	if !errors.Is(failure, auth.ErrSecondFactorVerificationFailed) {
		t.Fatalf("wrapped error = %v, want ErrSecondFactorVerificationFailed", failure.Unwrap())
	}
}

func TestTOTPCodeStep_EnrollmentProofStepRejectedAtLogin(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	secret, _, err := h.startAndCompleteTOTP(acct.sess, clock.Now())
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	code := totpCodeAt(secret, clock.Now())
	before := h.liveSessionCount(acct.user.ID)

	pending := h.newPending(acct.user.ID, nil)
	issue, err := h.completeTOTPPendingCode(pending.RawToken, nil, code)
	requirePendingVerificationFailure(t, err)
	if issue != nil {
		t.Fatal("rejected enrollment step issued a session")
	}
	if h.liveSessionCount(acct.user.ID) != before {
		t.Fatal("rejected enrollment step changed the live session count")
	}
	if h.pendingFailedAttempts(pending.Pending.ID) != 1 {
		t.Fatal("rejected enrollment step did not count one pending failure")
	}
	if h.totpCredentialRow(acct.user.ID).failedAttempts != 1 {
		t.Fatal("rejected enrollment step did not count one credential failure")
	}

	clock.Advance(totpPeriodSeconds * time.Second)
	fresh := h.newPending(acct.user.ID, nil)
	issue, err = h.completeTOTPPendingCode(fresh.RawToken, nil, totpCodeAt(secret, clock.Now()))
	if err != nil {
		t.Fatalf("control login with a fresh step failed: %v", err)
	}
	if issue == nil || issue.RawToken == "" {
		t.Fatal("control login with a fresh step returned no session")
	}
}

func TestTOTPCodeStep_EnrollmentProofStepRejectedAtReauth(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	secret, result, err := h.startAndCompleteTOTP(acct.sess, clock.Now())
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	sess := result.Session.Session
	code := totpCodeAt(secret, clock.Now())
	verifiedBefore := h.sessionSecondFactorVerifiedAt(sess.ID)

	pending := h.newPending(acct.user.ID, &sess.ID)
	issue, err := h.completeTOTPPendingCode(pending.RawToken, &sess.ID, code)
	requirePendingVerificationFailure(t, err)
	if issue != nil {
		t.Fatal("rejected enrollment step issued a session on reauth")
	}
	if !timePtrEqual(h.sessionSecondFactorVerifiedAt(sess.ID), verifiedBefore) {
		t.Fatal("rejected enrollment step changed second_factor_verified_at")
	}
	if h.pendingFailedAttempts(pending.Pending.ID) != 1 {
		t.Fatal("rejected enrollment step did not count one pending failure")
	}
	if h.totpCredentialRow(acct.user.ID).failedAttempts != 1 {
		t.Fatal("rejected enrollment step did not count one credential failure")
	}

	clock.Advance(totpPeriodSeconds * time.Second)
	fresh := h.newPending(acct.user.ID, &sess.ID)
	issue, err = h.completeTOTPPendingCode(fresh.RawToken, &sess.ID, totpCodeAt(secret, clock.Now()))
	if err != nil {
		t.Fatalf("control reauth with a fresh step failed: %v", err)
	}
	if issue != nil {
		t.Fatal("control reauth returned a new session, want the bound session updated in place")
	}
}

func TestTOTPCodeStep_LoginStepRejectedAtReauth(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	secret, _, err := h.startAndCompleteTOTP(acct.sess, clock.Now())
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}

	clock.Advance(totpPeriodSeconds * time.Second)
	loginCode := totpCodeAt(secret, clock.Now())
	loginPending := h.newPending(acct.user.ID, nil)
	loginIssue, err := h.completeTOTPPendingCode(loginPending.RawToken, nil, loginCode)
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if loginIssue == nil || loginIssue.RawToken == "" {
		t.Fatal("login returned no session")
	}
	loginSessionID := loginIssue.Session.ID
	verifiedBefore := h.sessionSecondFactorVerifiedAt(loginSessionID)

	reauthPending := h.newPending(acct.user.ID, &loginSessionID)
	reauthIssue, err := h.completeTOTPPendingCode(reauthPending.RawToken, &loginSessionID, loginCode)
	requirePendingVerificationFailure(t, err)
	if reauthIssue != nil {
		t.Fatal("rejected login step issued a session on reauth")
	}
	if !timePtrEqual(h.sessionSecondFactorVerifiedAt(loginSessionID), verifiedBefore) {
		t.Fatal("rejected login step changed second_factor_verified_at")
	}

	clock.Advance(totpPeriodSeconds * time.Second)
	fresh := h.newPending(acct.user.ID, &loginSessionID)
	freshIssue, err := h.completeTOTPPendingCode(fresh.RawToken, &loginSessionID, totpCodeAt(secret, clock.Now()))
	if err != nil {
		t.Fatalf("control reauth with a fresh step failed: %v", err)
	}
	if freshIssue != nil {
		t.Fatal("control reauth returned a new session, want the bound session updated in place")
	}
}

func TestTOTPCodeStep_ReauthStepRejectedAtLogin(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	secret, result, err := h.startAndCompleteTOTP(acct.sess, clock.Now())
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	sess := result.Session.Session

	clock.Advance(totpPeriodSeconds * time.Second)
	reauthCode := totpCodeAt(secret, clock.Now())
	reauthPending := h.newPending(acct.user.ID, &sess.ID)
	reauthIssue, err := h.completeTOTPPendingCode(reauthPending.RawToken, &sess.ID, reauthCode)
	if err != nil {
		t.Fatalf("reauth: %v", err)
	}
	if reauthIssue != nil {
		t.Fatal("reauth returned a new session, want the bound session updated in place")
	}

	loginPending := h.newPending(acct.user.ID, nil)
	loginIssue, err := h.completeTOTPPendingCode(loginPending.RawToken, nil, reauthCode)
	requirePendingVerificationFailure(t, err)
	if loginIssue != nil {
		t.Fatal("rejected reauth step issued a session on login")
	}
}

func TestTOTPCodeStep_ConcurrentLoginAndReauthHaveOneWinner(t *testing.T) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	acct := h.newAccount()
	secret, result, err := h.startAndCompleteTOTP(acct.sess, clock.Now())
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	sess := result.Session.Session

	clock.Advance(totpPeriodSeconds * time.Second)
	code := totpCodeAt(secret, clock.Now())
	loginPending := h.newPending(acct.user.ID, nil)
	reauthPending := h.newPending(acct.user.ID, &sess.ID)

	var loginErr, reauthErr error
	results := runConcurrently(t,
		func() error {
			_, loginErr = h.completeTOTPPendingCode(loginPending.RawToken, nil, code)
			return loginErr
		},
		func() error {
			_, reauthErr = h.completeTOTPPendingCode(reauthPending.RawToken, &sess.ID, code)
			return reauthErr
		},
	)

	successes, failures := 0, 0
	for _, raceErr := range results {
		if raceErr == nil {
			successes++
			continue
		}
		requirePendingVerificationFailure(t, raceErr)
		failures++
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("concurrent login and reauth = %d success, %d failure; want exactly one winner", successes, failures)
	}
	if h.totpCredentialRow(acct.user.ID).failedAttempts != 1 {
		t.Fatal("concurrent race did not count exactly one credential failure")
	}
}
