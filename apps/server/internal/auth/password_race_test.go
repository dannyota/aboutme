package auth_test

// Deterministic password-service race proofs. Each race has two explicit
// orderings (or one lock-ordered ordering) with a deterministic pause via a
// test-only probe, never a sleep. See docs/plans/phase-pa/task-08-password-http-service.md.

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/password"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

const raceIP = "203.0.113.7"

// onceProbe returns a probe that pauses exactly once at the first invocation
// (the first operation to acquire the lock), so the second operation's probe
// invocation is a no-op and cannot deadlock.
func onceProbe(locked, release chan struct{}) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			close(locked)
			<-release
		})
	}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// TestPasswordLogin_ResetFence proves login session issuance serializes on the
// user lock with password reset's RevokeAllSessions.
func TestPasswordLogin_ResetFence(t *testing.T) {
	t.Run("login first, reset revokes", func(t *testing.T) {
		e := newPasswordEnv(t)
		userID := e.createUser(t)
		e.setPassword(t, userID, testPassword)
		email := e.userEmail(t, userID)
		token := e.createResetToken(t, userID)

		locked := make(chan struct{})
		release := make(chan struct{})
		auth.SetPasswordUserLockProbeForTest(e.svc, onceProbe(locked, release))

		loginDone := make(chan string, 1)
		go func() {
			raw, err := e.svc.LoginForTest(context.Background(), email, testPassword, "ua", raceIP)
			if err != nil {
				loginDone <- "ERR:" + err.Error()
				return
			}
			loginDone <- raw
		}()

		<-locked // login holds the user lock

		resetDone := make(chan error, 1)
		go func() {
			resetDone <- e.svc.ResetForTest(context.Background(), token.Raw, "a fresh password 123", raceIP)
		}()

		select {
		case err := <-resetDone:
			t.Fatalf("reset acquired the user lock while login held it (err=%v)", err)
		case <-time.After(200 * time.Millisecond):
		}

		close(release)
		raw := <-loginDone
		if len(raw) >= 4 && raw[:4] == "ERR:" {
			t.Fatalf("login error = %s", raw)
		}
		if err := <-resetDone; err != nil {
			t.Fatalf("reset error = %v", err)
		}

		sm := auth.NewSessionManagerWithPoolForTest(e.pool, e.clk.Now)
		if _, _, err := sm.Authenticate(context.Background(), raw); err == nil {
			t.Error("login session still authenticates after reset, want revoked")
		}
	})

	t.Run("reset first, login after", func(t *testing.T) {
		e := newPasswordEnv(t)
		userID := e.createUser(t)
		e.setPassword(t, userID, testPassword)
		email := e.userEmail(t, userID)

		// Hold the user lock as reset does, then revoke and commit.
		tx, err := e.pool.Begin(context.Background())
		if err != nil {
			t.Fatalf("begin reset tx: %v", err)
		}
		defer func() {
			if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
				t.Errorf("Rollback() error: %v", err)
			}
		}()
		qtx := e.q.WithTx(tx)
		if _, err := qtx.GetUserForUpdate(context.Background(), userID); err != nil {
			t.Fatalf("reset lock user: %v", err)
		}
		now := e.clk.Now()
		if _, err := qtx.RevokeAllSessions(context.Background(), store.RevokeAllSessionsParams{UserID: userID, RevokedAt: &now}); err != nil {
			t.Fatalf("reset revoke all: %v", err)
		}

		loginDone := make(chan string, 1)
		go func() {
			raw, err := e.svc.LoginForTest(context.Background(), email, testPassword, "ua", raceIP)
			if err != nil {
				loginDone <- "ERR:" + err.Error()
				return
			}
			loginDone <- raw
		}()

		select {
		case raw := <-loginDone:
			t.Fatalf("login acquired the user lock while reset held it (raw=%q)", raw)
		case <-time.After(200 * time.Millisecond):
		}

		if err := tx.Commit(context.Background()); err != nil {
			t.Fatalf("commit reset: %v", err)
		}

		raw := <-loginDone
		if len(raw) >= 4 && raw[:4] == "ERR:" {
			t.Fatalf("login error = %s", raw)
		}
		sm := auth.NewSessionManagerWithPoolForTest(e.pool, e.clk.Now)
		if _, _, err := sm.Authenticate(context.Background(), raw); err != nil {
			t.Errorf("post-reset login session does not authenticate, want valid")
		}
	})
}

// TestPasswordLogin_ChangeSnapshotRetry proves a login whose credential changed
// between snapshot and transaction re-verifies once against the new hash.
func TestPasswordLogin_ChangeSnapshotRetry(t *testing.T) {
	e := newPasswordEnv(t)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	email := e.userEmail(t, userID)

	preTx := make(chan struct{})
	release := make(chan struct{})
	auth.SetPasswordLoginPreTxProbeForTest(e.svc, onceProbe(preTx, release))

	loginDone := make(chan error, 1)
	go func() {
		_, err := e.svc.LoginForTest(context.Background(), email, testPassword, "ua", raceIP)
		loginDone <- err
	}()

	<-preTx // the snapshot was verified; the transaction has not started.

	// Replace the credential with a fresh encoding of the SAME password: the
	// snapshot hash no longer matches, so login must re-verify and still succeed.
	e.setPassword(t, userID, testPassword)

	close(release)
	if err := <-loginDone; err != nil {
		t.Fatalf("login after same-password rehash error = %v, want success (retry re-verify)", err)
	}
}

// TestPasswordReset_ChangeLockOrder proves reset and change serialize on the
// user lock: a change that starts while reset holds the lock re-reads its
// current session and finds it revoked.
func TestPasswordReset_ChangeLockOrder(t *testing.T) {
	e := newPasswordEnv(t)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	_, sess := e.createSession(t, userID)
	token := e.createResetToken(t, userID)

	locked := make(chan struct{})
	release := make(chan struct{})
	auth.SetPasswordUserLockProbeForTest(e.svc, onceProbe(locked, release))

	resetDone := make(chan error, 1)
	go func() {
		resetDone <- e.svc.ResetForTest(context.Background(), token.Raw, "a fresh password 123", raceIP)
	}()

	<-locked // reset holds the user lock

	changeDone := make(chan error, 1)
	go func() {
		_, err := e.svc.ChangeForTest(context.Background(), sess, "another fresh password", raceIP)
		changeDone <- err
	}()

	select {
	case err := <-changeDone:
		t.Fatalf("change acquired the user lock while reset held it (err=%v)", err)
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	if err := <-resetDone; err != nil {
		t.Fatalf("reset error = %v", err)
	}
	// Change proceeds after the reset and finds its current session revoked.
	if err := <-changeDone; err == nil {
		t.Error("change succeeded after a reset revoked its session, want failure")
	}
}

// TestPasswordReset_DuplicateToken proves the reset token is single-use under a
// user-lock race.
func TestPasswordReset_DuplicateToken(t *testing.T) {
	e := newPasswordEnv(t)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	token := e.createResetToken(t, userID)

	locked := make(chan struct{})
	release := make(chan struct{})
	auth.SetPasswordUserLockProbeForTest(e.svc, onceProbe(locked, release))

	first := make(chan error, 1)
	go func() {
		first <- e.svc.ResetForTest(context.Background(), token.Raw, "a fresh password 123", raceIP)
	}()
	<-locked

	second := make(chan error, 1)
	go func() {
		second <- e.svc.ResetForTest(context.Background(), token.Raw, "a second fresh password", raceIP)
	}()

	// Give the second reset a moment to reach its preflight and block on the
	// user lock, then release the first.
	time.Sleep(20 * time.Millisecond)
	close(release)

	if err := <-first; err != nil {
		t.Fatalf("first reset error = %v, want success", err)
	}
	if err := <-second; err == nil {
		t.Fatal("second reset with the same token succeeded, want token-invalid")
	}
}

// TestPasswordVerify_DuplicateToken proves the verify token is single-use.
func TestPasswordVerify_DuplicateToken(t *testing.T) {
	e := newPasswordEnv(t)
	token, _ := e.createRegistration(t, newEmail(), "Ada", testPassword)

	locked := make(chan struct{})
	release := make(chan struct{})
	auth.SetPasswordVerifyRegistrationLockProbeForTest(e.svc, onceProbe(locked, release))

	first := make(chan error, 1)
	go func() {
		first <- e.svc.VerifyForTest(context.Background(), token.Raw, raceIP)
	}()
	<-locked

	second := make(chan error, 1)
	go func() {
		second <- e.svc.VerifyForTest(context.Background(), token.Raw, raceIP)
	}()

	time.Sleep(20 * time.Millisecond)
	close(release)

	if err := <-first; err != nil {
		t.Fatalf("first verify error = %v, want success", err)
	}
	if err := <-second; err == nil {
		t.Fatal("second verify with the same token succeeded, want token-invalid")
	}
}

// TestPasswordVerify_ProviderSignupRace proves the two email-race orderings
// between password verify and provider signup.
func TestPasswordVerify_ProviderSignupRace(t *testing.T) {
	t.Run("provider wins, verify consumes without password", func(t *testing.T) {
		e := newPasswordEnv(t)
		email := newEmail()
		token, _ := e.createRegistration(t, email, "Ada", testPassword)

		locked := make(chan struct{})
		release := make(chan struct{})
		auth.SetPasswordVerifyRegistrationLockProbeForTest(e.svc, onceProbe(locked, release))

		verifyDone := make(chan error, 1)
		go func() {
			verifyDone <- e.svc.VerifyForTest(context.Background(), token.Raw, raceIP)
		}()
		<-locked // verify locked the registration, before the email-ownership check

		// Provider signup creates the user for the same email first.
		provider, err := e.q.CreateUser(context.Background(), store.CreateUserParams{Email: email, Name: "Provider"})
		if err != nil {
			t.Fatalf("provider CreateUser: %v", err)
		}

		close(release)
		if err := <-verifyDone; err != nil {
			t.Fatalf("verify error = %v, want success (consume without password)", err)
		}

		// The provider user owns the email and has no password credential.
		if _, err := e.q.GetPasswordCredential(context.Background(), provider.ID); err == nil {
			t.Error("provider user gained a password credential, want none")
		}
		if _, err := e.q.GetPasswordRegistrationByDigest(context.Background(), token.Digest[:]); err == nil {
			t.Error("registration still present, want consumed without password")
		}
	})

	t.Run("verify wins, provider signup collides", func(t *testing.T) {
		e := newPasswordEnv(t)
		email := newEmail()
		token, _ := e.createRegistration(t, email, "Ada", testPassword)

		if err := e.svc.VerifyForTest(context.Background(), token.Raw, raceIP); err != nil {
			t.Fatalf("verify error = %v", err)
		}

		// A later provider signup for the same email is rejected.
		if _, err := e.q.CreateUser(context.Background(), store.CreateUserParams{Email: email, Name: "Provider"}); !isUniqueViolation(err) {
			t.Errorf("provider CreateUser after verify error = %v, want unique violation", err)
		}
		user, err := e.q.GetUserByCanonicalEmail(context.Background(), email)
		if err != nil {
			t.Fatalf("password account missing: %v", err)
		}
		if _, err := e.q.GetPasswordCredential(context.Background(), user.ID); err != nil {
			t.Errorf("password account has no credential, want one")
		}
	})
}

// TestPasswordProvider_SubjectCollision proves a provider subject (identity)
// stays unique to one account even as a password account is created separately.
func TestPasswordProvider_SubjectCollision(t *testing.T) {
	e := newPasswordEnv(t)
	u1 := e.createUser(t)
	subject := "g-" + uuid.NewString()
	if _, err := e.q.CreateIdentity(context.Background(), store.CreateIdentityParams{
		UserID:         u1,
		Provider:       string(auth.ProviderGoogle),
		ProviderUserID: subject,
	}); err != nil {
		t.Fatalf("create provider identity: %v", err)
	}

	// A password account for a different email is created by verify.
	email2 := newEmail()
	token, _ := e.createRegistration(t, email2, "Ada", testPassword)
	if err := e.svc.VerifyForTest(context.Background(), token.Raw, raceIP); err != nil {
		t.Fatalf("verify error = %v", err)
	}
	u2, err := e.q.GetUserByCanonicalEmail(context.Background(), email2)
	if err != nil {
		t.Fatalf("password account missing: %v", err)
	}

	// The provider subject cannot be linked to the second account (cross-user
	// subject collision).
	if _, err := e.q.CreateIdentity(context.Background(), store.CreateIdentityParams{
		UserID:         u2.ID,
		Provider:       string(auth.ProviderGoogle),
		ProviderUserID: subject,
	}); !isUniqueViolation(err) {
		t.Errorf("cross-user subject link error = %v, want unique violation", err)
	}
}

// TestPasswordProviderIssue_ResetFence proves a password reset revokes a
// provider-issued session.
func TestPasswordProviderIssue_ResetFence(t *testing.T) {
	e := newPasswordEnv(t)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	token := e.createResetToken(t, userID)

	sm := auth.NewSessionManagerWithPoolForTest(e.pool, e.clk.Now)
	raw, _, err := sm.Issue(context.Background(), userID, "provider-agent", "203.0.113.9")
	if err != nil {
		t.Fatalf("provider issue error = %v", err)
	}

	if err := e.svc.ResetForTest(context.Background(), token.Raw, "a fresh password 123", raceIP); err != nil {
		t.Fatalf("reset error = %v", err)
	}

	if _, _, err := sm.Authenticate(context.Background(), raw); err == nil {
		t.Error("provider session still authenticates after reset, want revoked")
	}
}

// TestPasswordChange_LostResponseRevokesOldSession proves a lost change
// response still leaves the old session revoked and the credential replaced.
func TestPasswordChange_LostResponseRevokesOldSession(t *testing.T) {
	e := newPasswordEnv(t)
	userID := e.createUser(t)
	e.setPassword(t, userID, testPassword)
	oldRaw, oldSess := e.createSession(t, userID)

	newPassword := "another brand new password"
	newRaw, err := e.svc.ChangeForTest(context.Background(), oldSess, newPassword, raceIP)
	if err != nil {
		t.Fatalf("change error = %v", err)
	}

	// The response carrying newRaw is lost; the client only has oldRaw.
	sm := auth.NewSessionManagerWithPoolForTest(e.pool, e.clk.Now)
	if _, _, err := sm.Authenticate(context.Background(), oldRaw); err == nil {
		t.Error("old session still authenticates after a lost change response, want revoked")
	}
	if _, _, err := sm.Authenticate(context.Background(), newRaw); err != nil {
		t.Errorf("fresh session does not authenticate, want valid (even though unreachable)")
	}

	// The pre-change credential is gone; only the new password works.
	email := e.userEmail(t, userID)
	if _, err := e.svc.LoginForTest(context.Background(), email, newPassword, "ua", raceIP); err != nil {
		t.Errorf("login with new password error = %v, want success", err)
	}
	if _, err := e.svc.LoginForTest(context.Background(), email, testPassword, "ua", raceIP); err == nil {
		t.Error("login with old password succeeded, want failure")
	}
}

// TestPasswordOutboxFailure_RollsBackMutation proves an enqueue failure rolls
// back the credential change and session revocation.
func TestPasswordOutboxFailure_RollsBackMutation(t *testing.T) {
	pool := newTestPool(t)
	q := store.New(pool)
	clk := newTestClockAtEpoch()
	sm := auth.NewSessionManagerWithPoolForTest(pool, clk.Now)
	hasher, err := newTestHasher()
	if err != nil {
		t.Fatalf("NewHasher: %v", err)
	}
	outbox := newTestOutbox(t, clk, errorReader{}) // EnqueueTx will fail to seal

	svc, err := newPasswordServiceWithOutbox(pool, q, sm, hasher, outbox, clk)
	if err != nil {
		t.Fatalf("NewPasswordService: %v", err)
	}

	email := newEmail()
	userID := createUserWithEmail(t, q, email)
	setPasswordWith(t, q, hasher, clk, userID, testPassword)
	oldRaw, oldSess := issueSessionWith(t, pool, clk, userID)

	if _, err := svc.ChangeForTest(context.Background(), oldSess, "another brand new password", raceIP); err == nil {
		t.Fatal("change with a failing outbox succeeded, want failure")
	}

	// The credential and sessions were rolled back.
	if _, err := svc.LoginForTest(context.Background(), email, testPassword, "ua", raceIP); err != nil {
		t.Errorf("old password no longer works after rolled-back change: %v", err)
	}
	smAuth := auth.NewSessionManagerWithPoolForTest(pool, clk.Now)
	if _, _, err := smAuth.Authenticate(context.Background(), oldRaw); err != nil {
		t.Errorf("old session revoked after a rolled-back change, want still live")
	}
}

// newTestClockAtEpoch and the helpers below exist so the outbox-failure test can
// build a standalone service with a broken outbox without reusing passwordEnv.
func newTestClockAtEpoch() *testutil.Clock {
	return testutil.NewClockAtEpoch()
}

func newTestHasher() (*password.Hasher, error) {
	return password.NewHasher(fastHashPolicy(), rand.Reader, password.NewAdmission())
}

func newPasswordServiceWithOutbox(pool *store.Pool, q *store.Queries, sm *auth.SessionManager, hasher *password.Hasher, outbox *authmail.Outbox, clk *testutil.Clock) (*auth.PasswordService, error) {
	policy := password.NewPolicy(nil, nil)
	var emailKey [32]byte
	for i := range emailKey {
		emailKey[i] = 0x01
	}
	limits, err := auth.NewPasswordRatePolicies(emailKey)
	if err != nil {
		return nil, err
	}
	return auth.NewPasswordService(auth.PasswordServiceOptions{
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
}

func createUserWithEmail(t *testing.T, q *store.Queries, email string) uuid.UUID {
	t.Helper()
	user, err := q.CreateUser(context.Background(), store.CreateUserParams{Email: email, Name: "Outbox User"})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user.ID
}

func setPasswordWith(t *testing.T, q *store.Queries, hasher *password.Hasher, clk *testutil.Clock, userID uuid.UUID, raw string) {
	t.Helper()
	enc, err := hasher.Hash(context.Background(), raw)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	now := clk.Now()
	if _, err := q.UpsertPasswordCredential(context.Background(), store.UpsertPasswordCredentialParams{
		UserID: userID, EncodedHash: []byte(enc), CreatedAt: now, ChangedAt: now,
	}); err != nil {
		t.Fatalf("upsert credential: %v", err)
	}
}

func issueSessionWith(t *testing.T, pool *store.Pool, clk *testutil.Clock, userID uuid.UUID) (string, store.Session) {
	t.Helper()
	raw, sess, err := auth.NewSessionManagerWithPoolForTest(pool, clk.Now).Issue(context.Background(), userID, "ua", "203.0.113.60")
	if err != nil {
		t.Fatalf("issue session: %v", err)
	}
	return raw, sess
}
