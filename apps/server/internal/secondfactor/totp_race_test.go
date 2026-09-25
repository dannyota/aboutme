package secondfactor

// Ordered races between TOTP replacement or removal and a code use, a
// recovery-code use, or session rotation. Each case forces one of the two
// possible orders with a real PostgreSQL user-row lock, then checks that the
// order has one winner and leaves no partial state
// (docs/design/totp-second-factor-contract.md#removal-recovery-and-races;
// AC-AUTH-027).

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// holdUserLock opens a transaction that holds the user's row lock and returns
// it with its backend PID. Every TOTP mutation, pending verification, and
// rotation successor takes this lock first, so the calls queue behind it.
func (h *harness) holdUserLock(userID uuid.UUID) (pgx.Tx, int32) {
	h.t.Helper()
	ctx := testContext(h.t)
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		h.t.Fatalf("begin user lock: %v", err)
	}
	var pid int32
	if err = tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		h.rollbackHolder(tx)
		h.t.Fatalf("read holder pid: %v", err)
	}
	if _, err = tx.Exec(ctx, "SELECT id FROM users WHERE id = $1 FOR UPDATE", userID); err != nil {
		h.rollbackHolder(tx)
		h.t.Fatalf("lock user: %v", err)
	}
	return tx, pid
}

func (h *harness) rollbackHolder(tx pgx.Tx) {
	h.t.Helper()
	if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		h.t.Errorf("rollback user lock: %v", err)
	}
}

// waitForLockQueue waits until want backends wait, directly or through
// another waiter, on the holder. It observes PostgreSQL's lock graph, so the
// order never depends on a timing guess; the deadline only bounds a failure.
func (h *harness) waitForLockQueue(holderPID int32, want int) {
	h.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		var queued int
		err := h.pool.QueryRow(context.Background(), `
			WITH RECURSIVE queue(pid) AS (
				SELECT $1::int
				UNION
				SELECT activity.pid
				FROM pg_stat_activity AS activity
				JOIN queue ON queue.pid = ANY(pg_blocking_pids(activity.pid))
			)
			SELECT count(*) - 1 FROM queue
		`, holderPID).Scan(&queued)
		if err != nil {
			h.t.Fatalf("observe lock queue: %v", err)
		}
		if queued >= want {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("lock queue behind holder = %d, want %d", queued, want)
		}
		<-ticker.C
	}
}

// runInLockOrder starts first, waits until it queues on the held user lock,
// then starts second and waits until it queues behind first. Releasing the
// lock lets first take the user row, commit, and only then lets second take
// it, while second's reads before its own lock already ran against the state
// before first committed.
func (h *harness) runInLockOrder(userID uuid.UUID, first, second func() error) (firstErr, secondErr error) {
	h.t.Helper()
	holder, pid := h.holdUserLock(userID)
	defer h.rollbackHolder(holder)
	results := [2]chan error{make(chan error, 1), make(chan error, 1)}
	go func() { results[0] <- first() }()
	h.waitForLockQueue(pid, 1)
	go func() { results[1] <- second() }()
	h.waitForLockQueue(pid, 2)
	if err := holder.Commit(testContext(h.t)); err != nil {
		h.t.Fatalf("release user lock: %v", err)
	}
	timeout := time.After(45 * time.Second)
	for i := range results {
		select {
		case err := <-results[i]:
			if i == 0 {
				firstErr = err
			} else {
				secondErr = err
			}
		case <-timeout:
			h.t.Fatal("ordered race did not finish before the timeout")
		}
	}
	return firstErr, secondErr
}

// totpRaceFixture is one account with an active first-factor TOTP credential,
// its ten recovery codes, and the rotated current session.
type totpRaceFixture struct {
	user          store.User
	secret        TOTPSecret
	recoveryCodes []string
	current       auth.SessionIssue
	epoch         int64
}

func (h *harness) newTOTPRaceFixture(now time.Time) totpRaceFixture {
	h.t.Helper()
	acct := h.newAccount()
	secret, enrolled, err := h.startAndCompleteTOTP(acct.sess, now)
	if err != nil {
		h.t.Fatalf("enroll: %v", err)
	}
	if len(enrolled.RecoveryCodes) != 10 {
		h.t.Fatalf("first enrollment returned %d recovery codes, want 10", len(enrolled.RecoveryCodes))
	}
	return totpRaceFixture{
		user: acct.user, secret: secret, recoveryCodes: enrolled.RecoveryCodes,
		current: enrolled.Session, epoch: h.epoch(acct.user.ID),
	}
}

// totpChange is a prepared replacement or removal on the fixture's current
// session. Preparation, such as starting the replacement enrollment, runs
// before the race; call runs inside it.
type totpChange struct {
	name  string
	final bool
	call  func() (auth.SessionIssue, error)
}

func (h *harness) prepareTOTPChange(ctx context.Context, f totpRaceFixture, remove bool, now time.Time) totpChange {
	h.t.Helper()
	sess := f.current.Session
	if remove {
		return totpChange{name: "removal", final: true, call: func() (auth.SessionIssue, error) {
			return h.svc.RemoveTOTP(ctx, sess)
		}}
	}
	start, err := h.svc.StartTOTPEnrollment(ctx, sess)
	if err != nil {
		h.t.Fatalf("start replacement: %v", err)
	}
	proof, err := h.svc.DecodeTOTPEnrollmentCompletion(totpEnrollmentBody(start.EnrollmentID, totpCodeAt(parseTOTPSecret(h.t, start.Secret), now)))
	if err != nil {
		h.t.Fatalf("decode replacement: %v", err)
	}
	return totpChange{name: "replacement", call: func() (auth.SessionIssue, error) {
		completed, completeErr := h.svc.CompleteTOTPEnrollment(ctx, sess, proof)
		return completed.Session, completeErr
	}}
}

// requireOnlyLiveSession checks that the change's replacement session is the
// account's one unrevoked session and carries the advanced epoch.
func (h *harness) requireOnlyLiveSession(f totpRaceFixture, replacement auth.SessionIssue) {
	h.t.Helper()
	if got := h.epoch(f.user.ID); got != f.epoch+1 {
		h.t.Fatalf("epoch = %d, want %d: the change advances it exactly once", got, f.epoch+1)
	}
	live := h.count("SELECT count(*) FROM sessions WHERE user_id = $1 AND revoked_at IS NULL", f.user.ID)
	replacementLive := h.count("SELECT count(*) FROM sessions WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL", replacement.Session.ID, f.user.ID)
	if live != 1 || replacementLive != 1 {
		h.t.Fatalf("live sessions = %d (replacement live: %d), want only the change's replacement", live, replacementLive)
	}
	if replacement.Session.AuthEpoch != f.epoch+1 {
		h.t.Fatalf("replacement session epoch = %d, want %d", replacement.Session.AuthEpoch, f.epoch+1)
	}
}

// TestTOTPChangeRacesCodeAndRecoveryUse forces both orders of a TOTP
// replacement or removal against a login that uses a TOTP code or a recovery
// code. Whichever commits first wins: a use that commits first issues a
// session that the change then revokes, and a change that commits first
// leaves the pending login dead with nothing consumed or counted
// (docs/design/totp-second-factor-contract.md#removal-recovery-and-races;
// AC-AUTH-027).
func TestTOTPChangeRacesCodeAndRecoveryUse(t *testing.T) {
	for _, useRecovery := range []bool{false, true} {
		for _, remove := range []bool{false, true} {
			for _, changeFirst := range []bool{false, true} {
				name := map[bool]string{false: "totp code", true: "recovery code"}[useRecovery] + "/" +
					map[bool]string{false: "replacement", true: "removal"}[remove] + "/" +
					map[bool]string{false: "use first", true: "change first"}[changeFirst]
				t.Run(name, func(t *testing.T) {
					runTOTPChangeUseRace(t, useRecovery, remove, changeFirst)
				})
			}
		}
	}
}

func runTOTPChangeUseRace(t *testing.T, useRecovery, remove, changeFirst bool) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	f := h.newTOTPRaceFixture(clock.Now())
	clock.Advance(totpPeriodSeconds * time.Second)
	ctx := testContext(t)

	pending := h.newPending(f.user.ID, nil)
	var (
		credential auth.SecondFactorCredential
		err        error
	)
	if useRecovery {
		credential, err = h.svc.DecodeRecoveryCode(f.recoveryCodes[0])
	} else {
		credential, err = h.svc.DecodeTOTPCode(totpCodeBody(totpCodeAt(f.secret, clock.Now())))
	}
	if err != nil {
		t.Fatalf("decode credential: %v", err)
	}
	change := h.prepareTOTPChange(ctx, f, remove, clock.Now())

	var (
		login       *auth.SessionIssue
		replacement auth.SessionIssue
	)
	use := func() error {
		var useErr error
		login, useErr = h.svc.CompletePending(ctx, pending.RawToken, nil, credential, auth.SecondFactorClient{UserAgent: "test-agent", IP: "203.0.113.7"})
		return useErr
	}
	mutate := func() error {
		var changeErr error
		replacement, changeErr = change.call()
		return changeErr
	}
	var useErr, changeErr error
	if changeFirst {
		changeErr, useErr = h.runInLockOrder(f.user.ID, mutate, use)
	} else {
		useErr, changeErr = h.runInLockOrder(f.user.ID, use, mutate)
	}
	if changeErr != nil {
		t.Fatalf("%s error = %v, want success in either order", change.name, changeErr)
	}
	h.requireOnlyLiveSession(f, replacement)

	wantCodes := 10
	if changeFirst {
		// A pending login bound to the old epoch completes nothing. After a
		// final removal the TOTP check finds no credential first, which the
		// contract answers with factor_not_found; every other case is the
		// dead pending row.
		wantErr := auth.ErrPendingAuthenticationRequired
		if remove && !useRecovery {
			wantErr = auth.ErrSecondFactorNotFound
		}
		if !errors.Is(useErr, wantErr) {
			t.Fatalf("use after the change error = %v, want %v", useErr, wantErr)
		}
		if login != nil {
			t.Fatal("use after the change issued a session")
		}
		if n := h.pendingFailedAttempts(pending.Pending.ID); n != 0 {
			t.Fatalf("pending failed_attempts = %d, want 0: a dead pending row counts no failure", n)
		}
		if n := h.count("SELECT count(*) FROM pending_authentications WHERE id = $1 AND consumed_at IS NULL", pending.Pending.ID); n != 1 {
			t.Fatal("use after the change consumed the pending row")
		}
	} else {
		if useErr != nil {
			t.Fatalf("use before the change error = %v, want success", useErr)
		}
		if login == nil || login.Session.AuthEpoch != f.epoch {
			t.Fatalf("use before the change issued %+v, want a session at epoch %d", login, f.epoch)
		}
		if _, _, authErr := h.sessions.Authenticate(ctx, login.RawToken); !errors.Is(authErr, auth.ErrSessionInvalid) {
			t.Fatalf("login session after the change authenticate error = %v, want ErrSessionInvalid", authErr)
		}
		if useRecovery {
			wantCodes = 9
		}
	}
	if remove {
		wantCodes = 0
	}
	if n := h.count("SELECT count(*) FROM second_factor_recovery_codes WHERE user_id = $1", f.user.ID); n != wantCodes {
		t.Fatalf("recovery codes = %d, want %d", n, wantCodes)
	}
	wantCredentials, wantPolicies := 1, 1
	if remove {
		wantCredentials, wantPolicies = 0, 0
	}
	if n := h.count("SELECT count(*) FROM totp_credentials WHERE user_id = $1", f.user.ID); n != wantCredentials {
		t.Fatalf("totp credentials = %d, want %d", n, wantCredentials)
	}
	if n := h.count("SELECT count(*) FROM second_factor_policies WHERE user_id = $1", f.user.ID); n != wantPolicies {
		t.Fatalf("policies = %d, want %d", n, wantPolicies)
	}
	if n := h.count("SELECT count(*) FROM totp_enrollments WHERE user_id = $1", f.user.ID); n != 0 {
		t.Fatalf("totp enrollments = %d, want 0 after the change", n)
	}
	if !remove {
		if row := h.totpCredentialRow(f.user.ID); row.failedAttempts != 0 || row.cooldownUntil != nil || row.lastFailedAt != nil {
			t.Fatalf("credential failure budget = %+v, want reset and untouched by the race", row)
		}
	}
}

// TestTOTPChangeRacesSessionRotation forces both orders of a TOTP replacement
// or removal against the 24-hour rotation of the same session. A rotation
// that commits first mints a successor at the old epoch that the change then
// revokes; a change that commits first advances the epoch, so the rotation
// mints nothing. Either way the change's replacement is the only live session
// (docs/design/second-factor-authentication.md#assurance-boundary;
// docs/adr/0015-session-rotation-delivery.md; AC-AUTH-027).
func TestTOTPChangeRacesSessionRotation(t *testing.T) {
	for _, remove := range []bool{false, true} {
		for _, changeFirst := range []bool{false, true} {
			name := map[bool]string{false: "replacement", true: "removal"}[remove] + "/" +
				map[bool]string{false: "rotation first", true: "change first"}[changeFirst]
			t.Run(name, func(t *testing.T) {
				runTOTPChangeRotationRace(t, remove, changeFirst)
			})
		}
	}
}

func runTOTPChangeRotationRace(t *testing.T, remove, changeFirst bool) {
	clock := newTOTPTestClock()
	h := newTOTPHarness(t, clock, nil)
	f := h.newTOTPRaceFixture(clock.Now())
	ctx := testContext(t)
	change := h.prepareTOTPChange(ctx, f, remove, clock.Now())
	// Age the session past the rotation threshold so its next authentication
	// rotates it. Primary and factor proof times stay recent, so the change
	// still passes its recent-reauthentication check.
	if _, err := h.pool.Exec(ctx, "UPDATE sessions SET created_at = created_at - interval '25 hours' WHERE id = $1", f.current.Session.ID); err != nil {
		t.Fatalf("age session: %v", err)
	}

	var (
		successorToken string
		replacement    auth.SessionIssue
	)
	rotate := func() error {
		var rotateErr error
		_, successorToken, rotateErr = h.sessions.Authenticate(ctx, f.current.RawToken)
		return rotateErr
	}
	mutate := func() error {
		var changeErr error
		replacement, changeErr = change.call()
		return changeErr
	}
	var rotateErr, changeErr error
	if changeFirst {
		changeErr, rotateErr = h.runInLockOrder(f.user.ID, mutate, rotate)
	} else {
		rotateErr, changeErr = h.runInLockOrder(f.user.ID, rotate, mutate)
	}
	if changeErr != nil {
		t.Fatalf("%s error = %v, want success in either order", change.name, changeErr)
	}
	h.requireOnlyLiveSession(f, replacement)

	successors := h.count("SELECT count(*) FROM sessions WHERE rotated_from = $1", f.current.Session.ID)
	if changeFirst {
		if !errors.Is(rotateErr, auth.ErrSessionInvalid) || successorToken != "" {
			t.Fatalf("rotation after the change = (%q, %v), want no successor and ErrSessionInvalid", successorToken, rotateErr)
		}
		if successors != 0 {
			t.Fatalf("rotation successors = %d, want 0 after the epoch advanced", successors)
		}
	} else {
		if rotateErr != nil || successorToken == "" {
			t.Fatalf("rotation before the change = (%q, %v), want a successor", successorToken, rotateErr)
		}
		if successors != 1 {
			t.Fatalf("rotation successors = %d, want 1", successors)
		}
		if _, _, authErr := h.sessions.Authenticate(ctx, successorToken); !errors.Is(authErr, auth.ErrSessionInvalid) {
			t.Fatalf("successor after the change authenticate error = %v, want ErrSessionInvalid", authErr)
		}
	}
	if _, _, authErr := h.sessions.Authenticate(ctx, f.current.RawToken); !errors.Is(authErr, auth.ErrSessionInvalid) {
		t.Fatalf("pre-change session authenticate error = %v, want ErrSessionInvalid", authErr)
	}
}
