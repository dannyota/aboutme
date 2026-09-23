package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

var totpStoreNow = time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)

func totpKeyID(marker byte) string {
	body := make([]byte, 22)
	for i := range body {
		body[i] = marker
	}
	return "tk1_" + string(body)
}

func totpNonce(marker byte) []byte {
	b := make([]byte, 12)
	b[0] = marker
	return b
}

func totpCiphertext(marker byte) []byte {
	b := make([]byte, 36)
	b[0] = marker
	return b
}

func newTOTPSession(ctx context.Context, t *testing.T, q *store.Queries, userID uuid.UUID, now time.Time) store.Session {
	t.Helper()
	session, err := q.CreateSession(ctx, store.CreateSessionParams{
		UserID: userID, TokenHash: uniqueTokenDigest(), CSRFSecret: uniqueTokenDigest(),
		CreatedAt: now, LastSeenAt: now, ReauthenticatedAt: now, AbsoluteExpiresAt: now.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return session
}

func installTOTPCredential(ctx context.Context, t *testing.T, q *store.Queries, userID uuid.UUID, keyID string, step int64, now time.Time) store.TotpCredential {
	t.Helper()
	cred, err := q.InstallTOTPCredential(ctx, store.InstallTOTPCredentialParams{
		ID: uuid.Must(uuid.NewV7()), UserID: userID, KeyID: keyID,
		Nonce: totpNonce(1), Ciphertext: totpCiphertext(1), LastUsedStep: step, CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("InstallTOTPCredential: %v", err)
	}
	if cred.LastFailedAt != nil {
		t.Fatalf("installed credential last_failed_at = %v, want null", cred.LastFailedAt)
	}
	return cred
}

func createTOTPEnrollment(ctx context.Context, t *testing.T, q *store.Queries, userID, sessionID uuid.UUID, keyID string, now time.Time) store.TotpEnrollment {
	t.Helper()
	enrollment, err := q.CreateTOTPEnrollment(ctx, store.CreateTOTPEnrollmentParams{
		ID: uuid.Must(uuid.NewV7()), TokenDigest: uniqueTokenDigest(), UserID: userID, SessionID: sessionID,
		AuthEpoch: 0, Issuer: "aboutme.vn", KeyID: keyID, Nonce: totpNonce(2), Ciphertext: totpCiphertext(2),
		CreatedAt: now, ExpiresAt: now.Add(10 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateTOTPEnrollment: %v", err)
	}
	return enrollment
}

func TestTOTPStore_CredentialCascadesFromUser(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	installTOTPCredential(ctx, t, q, userID, totpKeyID('a'), 0, totpStoreNow)

	if _, err := q.GetUserForUpdate(ctx, userID); err != nil {
		t.Fatalf("precondition GetUserForUpdate: %v", err)
	}
	if _, err := q.DeleteAccountUser(ctx, userID); err != nil {
		t.Fatalf("DeleteAccountUser: %v", err)
	}
	if _, err := q.GetTOTPCredentialForUpdate(ctx, userID); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("credential after user delete error = %v, want pgx.ErrNoRows", err)
	}
}

// TestTOTPStore_EnrollmentSurvivesSessionRevocationButNotSessionDeletion
// isolates the session_id foreign key from the user_id one: revocation is a
// soft flag the row survives, and only physically deleting the session row
// removes the enrollment through that specific cascade, while the account
// and its user row are untouched
// (docs/design/totp-second-factor-contract.md#postgresql-shape-and-bounds).
func TestTOTPStore_EnrollmentSurvivesSessionRevocationButNotSessionDeletion(t *testing.T) {
	ctx, _, tx, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	session := newTOTPSession(ctx, t, q, userID, totpStoreNow)
	enrollment := createTOTPEnrollment(ctx, t, q, userID, session.ID, totpKeyID('a'), totpStoreNow)

	revokedAt := totpStoreNow
	if err := q.RevokeSession(ctx, store.RevokeSessionParams{ID: session.ID, RevokedAt: &revokedAt}); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	if _, err := q.GetTOTPEnrollmentForUpdate(ctx, enrollment.TokenDigest); err != nil {
		t.Fatalf("enrollment after session revoke: %v", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, session.ID); err != nil {
		t.Fatalf("delete session row: %v", err)
	}
	if _, err := q.GetTOTPEnrollmentForUpdate(ctx, enrollment.TokenDigest); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("enrollment after session row delete error = %v, want pgx.ErrNoRows", err)
	}
	if _, err := q.GetUserForUpdate(ctx, userID); err != nil {
		t.Errorf("user after session row delete: %v, want the account untouched", err)
	}
}

func TestTOTPStore_EnrollmentCascadesFromUser(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	session := newTOTPSession(ctx, t, q, userID, totpStoreNow)
	enrollment := createTOTPEnrollment(ctx, t, q, userID, session.ID, totpKeyID('a'), totpStoreNow)

	if _, err := q.DeleteAccountUser(ctx, userID); err != nil {
		t.Fatalf("DeleteAccountUser: %v", err)
	}
	if _, err := q.GetTOTPEnrollmentForUpdate(ctx, enrollment.TokenDigest); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("enrollment after user delete error = %v, want pgx.ErrNoRows", err)
	}
}

func TestTOTPStore_EnrollmentHasOnlyOneRowPerAccount(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	session := newTOTPSession(ctx, t, q, userID, totpStoreNow)
	createTOTPEnrollment(ctx, t, q, userID, session.ID, totpKeyID('a'), totpStoreNow)

	_, err := q.CreateTOTPEnrollment(ctx, store.CreateTOTPEnrollmentParams{
		ID: uuid.Must(uuid.NewV7()), TokenDigest: uniqueTokenDigest(), UserID: userID, SessionID: session.ID,
		AuthEpoch: 0, Issuer: "aboutme.vn", KeyID: totpKeyID('b'), Nonce: totpNonce(3), Ciphertext: totpCiphertext(3),
		CreatedAt: totpStoreNow, ExpiresAt: totpStoreNow.Add(10 * time.Minute),
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("second CreateTOTPEnrollment error = %v, want a unique_violation (23505) on user_id", err)
	}
}

func TestTOTPStore_StartSupersedesAnyExistingEnrollment(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	session := newTOTPSession(ctx, t, q, userID, totpStoreNow)
	original := createTOTPEnrollment(ctx, t, q, userID, session.ID, totpKeyID('a'), totpStoreNow)

	deleted, err := q.DeleteTOTPEnrollmentForUser(ctx, userID)
	if err != nil {
		t.Fatalf("DeleteTOTPEnrollmentForUser: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("DeleteTOTPEnrollmentForUser rows = %d, want 1", deleted)
	}
	replacement := createTOTPEnrollment(ctx, t, q, userID, session.ID, totpKeyID('b'), totpStoreNow.Add(time.Second))

	if _, lookupErr := q.GetTOTPEnrollmentForUpdate(ctx, original.TokenDigest); !errors.Is(lookupErr, pgx.ErrNoRows) {
		t.Errorf("superseded enrollment lookup error = %v, want pgx.ErrNoRows", lookupErr)
	}
	locked, err := q.GetTOTPEnrollmentForUpdate(ctx, replacement.TokenDigest)
	if err != nil {
		t.Fatalf("GetTOTPEnrollmentForUpdate(replacement): %v", err)
	}
	if locked.ID != replacement.ID {
		t.Errorf("locked enrollment id = %s, want %s", locked.ID, replacement.ID)
	}
}

func TestTOTPStore_EnrollmentClaimHasOneConcurrentWinner(t *testing.T) {
	ctx, pool, _, _ := newOAuthStoreTx(t)
	seed := store.New(pool)
	userID := newOAuthStoreUser(ctx, t, seed)
	session := newTOTPSession(ctx, t, seed, userID, totpStoreNow)
	enrollment := createTOTPEnrollment(ctx, t, seed, userID, session.ID, totpKeyID('a'), totpStoreNow)
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Errorf("cleanup user: %v", err)
		}
	})

	txA, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin(A): %v", err)
	}
	t.Cleanup(func() { rollbackOAuthStoreTx(t, txA) })
	txB, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin(B): %v", err)
	}
	t.Cleanup(func() { rollbackOAuthStoreTx(t, txB) })
	var pidB int32
	if pidErr := txB.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pidB); pidErr != nil {
		t.Fatalf("B backend PID: %v", pidErr)
	}

	claim := func(tx pgx.Tx) (store.TotpEnrollment, error) {
		return store.New(tx).GetTOTPEnrollmentForUpdate(ctx, enrollment.TokenDigest)
	}
	lockedA, err := claim(txA)
	if err != nil {
		t.Fatalf("A claim: %v", err)
	}
	if _, err := store.New(txA).DeleteTOTPEnrollmentByID(ctx, lockedA.ID); err != nil {
		t.Fatalf("A delete: %v", err)
	}
	type outcome struct {
		err error
	}
	outcomeB := make(chan outcome, 1)
	go func() {
		_, err := claim(txB)
		outcomeB <- outcome{err: err}
	}()
	waitForBlockedBackend(ctx, t, pool, pidB)
	if err := txA.Commit(ctx); err != nil {
		t.Fatalf("A commit: %v", err)
	}
	if got := <-outcomeB; !errors.Is(got.err, pgx.ErrNoRows) {
		t.Errorf("B claim error = %v, want pgx.ErrNoRows", got.err)
	}
}

func TestTOTPStore_ConcurrentStepAdvanceHasOneWinner(t *testing.T) {
	ctx, pool, _, _ := newOAuthStoreTx(t)
	seed := store.New(pool)
	userID := newOAuthStoreUser(ctx, t, seed)
	installTOTPCredential(ctx, t, seed, userID, totpKeyID('a'), 0, totpStoreNow)
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Errorf("cleanup user: %v", err)
		}
	})

	txA, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin(A): %v", err)
	}
	t.Cleanup(func() { rollbackOAuthStoreTx(t, txA) })
	txB, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin(B): %v", err)
	}
	t.Cleanup(func() { rollbackOAuthStoreTx(t, txB) })
	var pidB int32
	if pidErr := txB.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pidB); pidErr != nil {
		t.Fatalf("B backend PID: %v", pidErr)
	}

	lock := func(tx pgx.Tx) (store.TotpCredential, error) {
		return store.New(tx).GetTOTPCredentialForUpdate(ctx, userID)
	}
	advance := func(tx pgx.Tx, step int64) (store.TotpCredential, error) {
		return store.New(tx).AdvanceTOTPCredentialStep(ctx, store.AdvanceTOTPCredentialStepParams{
			UserID: userID, Step: step, Now: totpStoreNow.Add(30 * time.Second),
		})
	}
	if _, err := lock(txA); err != nil {
		t.Fatalf("A lock: %v", err)
	}
	if _, err := advance(txA, 1); err != nil {
		t.Fatalf("A advance: %v", err)
	}
	type outcome struct {
		err error
	}
	outcomeB := make(chan outcome, 1)
	go func() {
		if _, err := lock(txB); err != nil {
			outcomeB <- outcome{err: err}
			return
		}
		_, err := advance(txB, 1)
		outcomeB <- outcome{err: err}
	}()
	waitForBlockedBackend(ctx, t, pool, pidB)
	if err := txA.Commit(ctx); err != nil {
		t.Fatalf("A commit: %v", err)
	}
	// B's lock acquires only after A commits, so B observes last_used_step = 1
	// already and its own advance to the same step 1 is a replay: it must
	// affect zero rows because the step is not greater than the stored one.
	if got := <-outcomeB; !errors.Is(got.err, pgx.ErrNoRows) {
		t.Errorf("B replayed advance error = %v, want pgx.ErrNoRows", got.err)
	}
}

func TestTOTPStore_RemovalHasOneConcurrentWinner(t *testing.T) {
	ctx, pool, _, _ := newOAuthStoreTx(t)
	seed := store.New(pool)
	userID := newOAuthStoreUser(ctx, t, seed)
	installTOTPCredential(ctx, t, seed, userID, totpKeyID('a'), 0, totpStoreNow)
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Errorf("cleanup user: %v", err)
		}
	})

	txA, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin(A): %v", err)
	}
	t.Cleanup(func() { rollbackOAuthStoreTx(t, txA) })
	txB, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin(B): %v", err)
	}
	t.Cleanup(func() { rollbackOAuthStoreTx(t, txB) })
	var pidB int32
	if pidErr := txB.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pidB); pidErr != nil {
		t.Fatalf("B backend PID: %v", pidErr)
	}
	remove := func(tx pgx.Tx) (store.TotpCredential, error) {
		return store.New(tx).DeleteTOTPCredentialForUser(ctx, userID)
	}
	if _, err := remove(txA); err != nil {
		t.Fatalf("A remove: %v", err)
	}
	type outcome struct {
		err error
	}
	outcomeB := make(chan outcome, 1)
	go func() {
		_, err := remove(txB)
		outcomeB <- outcome{err: err}
	}()
	waitForBlockedBackend(ctx, t, pool, pidB)
	if err := txA.Commit(ctx); err != nil {
		t.Fatalf("A commit: %v", err)
	}
	if got := <-outcomeB; !errors.Is(got.err, pgx.ErrNoRows) {
		t.Errorf("B remove error = %v, want pgx.ErrNoRows", got.err)
	}
}

func TestTOTPStore_FailureBudgetDoublesEveryFifthFailureAndSaturates(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	installTOTPCredential(ctx, t, q, userID, totpKeyID('a'), 0, totpStoreNow)

	fail := func(at time.Time) store.TotpCredential {
		t.Helper()
		cred, err := q.RecordTOTPCredentialFailure(ctx, store.RecordTOTPCredentialFailureParams{UserID: userID, Now: at})
		if err != nil {
			t.Fatalf("RecordTOTPCredentialFailure: %v", err)
		}
		return cred
	}

	var cred store.TotpCredential
	for i := 1; i <= 4; i++ {
		at := totpStoreNow.Add(time.Duration(i) * time.Second)
		cred = fail(at)
		if cred.FailedAttempts != int32(i) || cred.CooldownUntil != nil {
			t.Fatalf("failure %d = %+v, want no cool-down yet", i, cred)
		}
		if cred.LastFailedAt == nil || !cred.LastFailedAt.Equal(at) {
			t.Fatalf("failure %d last_failed_at = %v, want %v", i, cred.LastFailedAt, at)
		}
	}
	fifthAt := totpStoreNow.Add(5 * time.Second)
	cred = fail(fifthAt)
	if cred.FailedAttempts != 5 || cred.CooldownUntil == nil || !cred.CooldownUntil.Equal(fifthAt.Add(15*time.Minute)) {
		t.Fatalf("fifth failure = %+v, want failed_attempts 5 and a 15-minute cool-down", cred)
	}
	if cred.LastFailedAt == nil || !cred.LastFailedAt.Equal(fifthAt) {
		t.Fatalf("fifth failure last_failed_at = %v, want %v", cred.LastFailedAt, fifthAt)
	}

	// The cool-down guard blocks a sixth failure recorded before it elapses.
	if _, err := q.RecordTOTPCredentialFailure(ctx, store.RecordTOTPCredentialFailureParams{
		UserID: userID, Now: fifthAt.Add(time.Second),
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("failure during cool-down error = %v, want pgx.ErrNoRows", err)
	}

	// After the cool-down elapses, failures 6 through 9 accrue with no new
	// cool-down, and the tenth doubles it to 30 minutes.
	afterFirstCooldown := cred.CooldownUntil.Add(time.Second)
	for i := 6; i <= 9; i++ {
		cred = fail(afterFirstCooldown.Add(time.Duration(i) * time.Second))
		if cred.FailedAttempts != int32(i) {
			t.Fatalf("failure %d failed_attempts = %d, want %d", i, cred.FailedAttempts, i)
		}
	}
	tenthAt := afterFirstCooldown.Add(10 * time.Second)
	cred = fail(tenthAt)
	if cred.FailedAttempts != 10 || cred.CooldownUntil == nil || !cred.CooldownUntil.Equal(tenthAt.Add(30*time.Minute)) {
		t.Fatalf("tenth failure = %+v, want failed_attempts 10 and a 30-minute cool-down", cred)
	}
	if cred.LastFailedAt == nil || !cred.LastFailedAt.Equal(tenthAt) {
		t.Fatalf("tenth failure last_failed_at = %v, want %v", cred.LastFailedAt, tenthAt)
	}
}

func TestTOTPStore_FailureBudgetCapsCooldownAtTwentyFourHours(t *testing.T) {
	ctx, _, tx, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	installTOTPCredential(ctx, t, q, userID, totpKeyID('a'), 0, totpStoreNow)

	// Seed failed_attempts to 999 directly: the doubling formula's cap is
	// arithmetic on whatever count the row holds, not on the path that
	// produced it, and 199 real failure-and-wait cycles would only restate
	// the same doubling TestTOTPStore_FailureBudgetDoublesEveryFifthFailureAndSaturates
	// already proves.
	if _, err := tx.Exec(ctx, `UPDATE totp_credentials SET failed_attempts = 999 WHERE user_id = $1`, userID); err != nil {
		t.Fatalf("seed failed_attempts: %v", err)
	}

	at := totpStoreNow.Add(time.Hour)
	cred, err := q.RecordTOTPCredentialFailure(ctx, store.RecordTOTPCredentialFailureParams{UserID: userID, Now: at})
	if err != nil {
		t.Fatalf("RecordTOTPCredentialFailure(1000th): %v", err)
	}
	if cred.FailedAttempts != 1000 || cred.CooldownUntil == nil || !cred.CooldownUntil.Equal(at.Add(24*time.Hour)) {
		t.Fatalf("1000th failure = %+v, want failed_attempts 1000 and a 24-hour cool-down", cred)
	}
	if cred.LastFailedAt == nil || !cred.LastFailedAt.Equal(at) {
		t.Fatalf("1000th failure last_failed_at = %v, want %v", cred.LastFailedAt, at)
	}

	// At the ceiling, the counter cannot exceed 1,000 and every further
	// failure keeps resetting the 24-hour cool-down.
	after := cred.CooldownUntil.Add(time.Second)
	cred, err = q.RecordTOTPCredentialFailure(ctx, store.RecordTOTPCredentialFailureParams{UserID: userID, Now: after})
	if err != nil {
		t.Fatalf("RecordTOTPCredentialFailure(over ceiling): %v", err)
	}
	if cred.FailedAttempts != 1000 || cred.CooldownUntil == nil || !cred.CooldownUntil.Equal(after.Add(24*time.Hour)) {
		t.Fatalf("failure past the ceiling = %+v, want failed_attempts held at 1000 and a fresh 24-hour cool-down", cred)
	}
}

func TestTOTPStore_ResetClearsFailureBudget(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	installTOTPCredential(ctx, t, q, userID, totpKeyID('a'), 0, totpStoreNow)
	for i := 1; i <= 5; i++ {
		if _, err := q.RecordTOTPCredentialFailure(ctx, store.RecordTOTPCredentialFailureParams{
			UserID: userID, Now: totpStoreNow.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("RecordTOTPCredentialFailure(%d): %v", i, err)
		}
	}
	reset, err := q.ResetTOTPCredentialFailureBudget(ctx, store.ResetTOTPCredentialFailureBudgetParams{
		UserID: userID, Now: totpStoreNow.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("ResetTOTPCredentialFailureBudget: %v", err)
	}
	if reset.FailedAttempts != 0 || reset.CooldownUntil != nil || reset.LastFailedAt != nil {
		t.Errorf("reset credential = %+v, want zero failure budget and null last_failed_at", reset)
	}
}

func TestTOTPStore_ReplacementPreservesIdentityAndResetsFailureBudget(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	original := installTOTPCredential(ctx, t, q, userID, totpKeyID('a'), 3, totpStoreNow)
	for i := 1; i <= 5; i++ {
		if _, err := q.RecordTOTPCredentialFailure(ctx, store.RecordTOTPCredentialFailureParams{
			UserID: userID, Now: totpStoreNow.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("RecordTOTPCredentialFailure(%d): %v", i, err)
		}
	}

	replaced, err := q.ReplaceTOTPCredential(ctx, store.ReplaceTOTPCredentialParams{
		UserID: userID, KeyID: totpKeyID('b'), Nonce: totpNonce(9), Ciphertext: totpCiphertext(9),
		LastUsedStep: 0, UpdatedAt: totpStoreNow.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("ReplaceTOTPCredential: %v", err)
	}
	if replaced.ID != original.ID || !replaced.CreatedAt.Equal(original.CreatedAt) {
		t.Errorf("replaced identity = %+v, want id %s and created_at %s preserved", replaced, original.ID, original.CreatedAt)
	}
	if replaced.KeyID != totpKeyID('b') || replaced.LastUsedStep != 0 {
		t.Errorf("replaced key material = %+v, want the new key and reset step", replaced)
	}
	if replaced.FailedAttempts != 0 || replaced.CooldownUntil != nil || replaced.LastFailedAt != nil {
		t.Errorf("replaced failure budget = %+v, want zero and null last_failed_at", replaced)
	}
}

func TestTOTPStore_CleanupExpiredEnrollmentsIsBoundedAndSkipsLiveRows(t *testing.T) {
	// Cleanup compares against the database clock, so these rows are placed
	// relative to the real clock rather than the fixed test instant.
	ctx, _, _, q := newOAuthStoreTx(t)
	realNow := time.Now().UTC()
	liveUser := newOAuthStoreUser(ctx, t, q)
	liveSession := newTOTPSession(ctx, t, q, liveUser, realNow)
	live := createTOTPEnrollment(ctx, t, q, liveUser, liveSession.ID, totpKeyID('a'), realNow)

	var expired []store.TotpEnrollment
	for i := 0; i < 2; i++ {
		expiredUser := newOAuthStoreUser(ctx, t, q)
		expiredSession := newTOTPSession(ctx, t, q, expiredUser, realNow.Add(-time.Hour))
		expired = append(expired, createTOTPEnrollment(ctx, t, q, expiredUser, expiredSession.ID, totpKeyID('a'), realNow.Add(-time.Hour)))
	}

	deleted, err := q.CleanupExpiredTOTPEnrollments(ctx, 1)
	if err != nil {
		t.Fatalf("CleanupExpiredTOTPEnrollments(1): %v", err)
	}
	if deleted != 1 {
		t.Fatalf("CleanupExpiredTOTPEnrollments(1) rows = %d, want 1", deleted)
	}

	deleted, err = q.CleanupExpiredTOTPEnrollments(ctx, 200)
	if err != nil {
		t.Fatalf("CleanupExpiredTOTPEnrollments(200): %v", err)
	}
	// Other suites share the database, so the second sweep may also remove
	// their expired rows. The lookups below check this test's rows exactly.
	if deleted < 1 {
		t.Fatalf("CleanupExpiredTOTPEnrollments(200) remaining rows = %d, want at least 1", deleted)
	}

	for _, row := range expired {
		if _, err := q.GetTOTPEnrollmentForUpdate(ctx, row.TokenDigest); !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("expired enrollment %s survived cleanup, error = %v", row.ID, err)
		}
	}
	if _, err := q.GetTOTPEnrollmentForUpdate(ctx, live.TokenDigest); err != nil {
		t.Errorf("live enrollment removed by cleanup: %v", err)
	}
}

func TestTOTPStore_ReencryptionBatchIsBoundedAndCompareBeforeUpdate(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	staleA := installTOTPCredential(ctx, t, q, newOAuthStoreUser(ctx, t, q), totpKeyID('o'), 0, totpStoreNow)
	installTOTPCredential(ctx, t, q, newOAuthStoreUser(ctx, t, q), totpKeyID('o'), 0, totpStoreNow)
	installTOTPCredential(ctx, t, q, newOAuthStoreUser(ctx, t, q), totpKeyID('n'), 0, totpStoreNow)

	batch, err := q.ListTOTPCredentialsOffActiveKey(ctx, store.ListTOTPCredentialsOffActiveKeyParams{
		ActiveKeyID: totpKeyID('n'), LimitRows: 200,
	})
	if err != nil {
		t.Fatalf("ListTOTPCredentialsOffActiveKey: %v", err)
	}
	if len(batch) != 2 {
		t.Fatalf("batch off active key = %d rows, want 2", len(batch))
	}

	before, err := q.CountTOTPCredentialsOffActiveKey(ctx, totpKeyID('n'))
	if err != nil {
		t.Fatalf("CountTOTPCredentialsOffActiveKey(before): %v", err)
	}
	if before != 2 {
		t.Fatalf("count off active key(before) = %d, want 2", before)
	}

	rows, err := q.ReencryptTOTPCredential(ctx, store.ReencryptTOTPCredentialParams{
		ID: staleA.ID, OldKeyID: totpKeyID('o'), NewKeyID: totpKeyID('n'),
		Nonce: totpNonce(5), Ciphertext: totpCiphertext(5), Now: totpStoreNow.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("ReencryptTOTPCredential: %v", err)
	}
	if rows != 1 {
		t.Fatalf("ReencryptTOTPCredential rows = %d, want 1", rows)
	}

	// A second attempt with the same stale old_key_id compares before update
	// and affects zero rows: the row is no longer sealed under that key.
	rows, err = q.ReencryptTOTPCredential(ctx, store.ReencryptTOTPCredentialParams{
		ID: staleA.ID, OldKeyID: totpKeyID('o'), NewKeyID: totpKeyID('n'),
		Nonce: totpNonce(6), Ciphertext: totpCiphertext(6), Now: totpStoreNow.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("ReencryptTOTPCredential(stale): %v", err)
	}
	if rows != 0 {
		t.Fatalf("ReencryptTOTPCredential(stale) rows = %d, want 0", rows)
	}

	after, err := q.CountTOTPCredentialsOffActiveKey(ctx, totpKeyID('n'))
	if err != nil {
		t.Fatalf("CountTOTPCredentialsOffActiveKey(after): %v", err)
	}
	if after != 1 {
		t.Fatalf("count off active key(after) = %d, want 1 (the row not yet re-encrypted)", after)
	}
}

func TestTOTPStore_EnrollmentReencryptionBatchAndCompareBeforeUpdate(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	staleUser := newOAuthStoreUser(ctx, t, q)
	staleSession := newTOTPSession(ctx, t, q, staleUser, totpStoreNow)
	stale := createTOTPEnrollment(ctx, t, q, staleUser, staleSession.ID, totpKeyID('o'), totpStoreNow)

	freshUser := newOAuthStoreUser(ctx, t, q)
	freshSession := newTOTPSession(ctx, t, q, freshUser, totpStoreNow)
	createTOTPEnrollment(ctx, t, q, freshUser, freshSession.ID, totpKeyID('n'), totpStoreNow)

	batch, err := q.ListTOTPEnrollmentsOffActiveKey(ctx, store.ListTOTPEnrollmentsOffActiveKeyParams{
		ActiveKeyID: totpKeyID('n'), LimitRows: 200,
	})
	if err != nil {
		t.Fatalf("ListTOTPEnrollmentsOffActiveKey: %v", err)
	}
	if len(batch) != 1 || batch[0].ID != stale.ID {
		t.Fatalf("batch off active key = %+v, want exactly the stale-keyed enrollment", batch)
	}

	rows, err := q.ReencryptTOTPEnrollment(ctx, store.ReencryptTOTPEnrollmentParams{
		ID: stale.ID, OldKeyID: totpKeyID('o'), NewKeyID: totpKeyID('n'),
		Nonce: totpNonce(7), Ciphertext: totpCiphertext(7),
	})
	if err != nil {
		t.Fatalf("ReencryptTOTPEnrollment: %v", err)
	}
	if rows != 1 {
		t.Fatalf("ReencryptTOTPEnrollment rows = %d, want 1", rows)
	}

	// The stale old_key_id no longer matches, so a repeat affects zero rows.
	rows, err = q.ReencryptTOTPEnrollment(ctx, store.ReencryptTOTPEnrollmentParams{
		ID: stale.ID, OldKeyID: totpKeyID('o'), NewKeyID: totpKeyID('n'),
		Nonce: totpNonce(8), Ciphertext: totpCiphertext(8),
	})
	if err != nil {
		t.Fatalf("ReencryptTOTPEnrollment(stale): %v", err)
	}
	if rows != 0 {
		t.Fatalf("ReencryptTOTPEnrollment(stale) rows = %d, want 0", rows)
	}

	after, err := q.CountTOTPEnrollmentsOffActiveKey(ctx, totpKeyID('n'))
	if err != nil {
		t.Fatalf("CountTOTPEnrollmentsOffActiveKey: %v", err)
	}
	if after != 0 {
		t.Fatalf("count off active key(after) = %d, want 0", after)
	}
}

func TestTOTPStore_KeyHealthListsAtMostThreeDistinctIDs(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)

	userA := newOAuthStoreUser(ctx, t, q)
	installTOTPCredential(ctx, t, q, userA, totpKeyID('a'), 0, totpStoreNow)
	userB := newOAuthStoreUser(ctx, t, q)
	installTOTPCredential(ctx, t, q, userB, totpKeyID('b'), 0, totpStoreNow)

	ids, err := q.ListTOTPActiveKeyIDs(ctx, totpStoreNow)
	if err != nil {
		t.Fatalf("ListTOTPActiveKeyIDs(healthy ring): %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("ListTOTPActiveKeyIDs(healthy ring) = %v, want exactly 2 distinct IDs", ids)
	}

	userC := newOAuthStoreUser(ctx, t, q)
	installTOTPCredential(ctx, t, q, userC, totpKeyID('c'), 0, totpStoreNow)
	userD := newOAuthStoreUser(ctx, t, q)
	installTOTPCredential(ctx, t, q, userD, totpKeyID('d'), 0, totpStoreNow)

	ids, err = q.ListTOTPActiveKeyIDs(ctx, totpStoreNow)
	if err != nil {
		t.Fatalf("ListTOTPActiveKeyIDs(overflow): %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("ListTOTPActiveKeyIDs(overflow) = %v, want exactly 3 (bounded), not all 4 distinct IDs", ids)
	}
}

func TestTOTPStore_MailWindowClaimIsConditional(t *testing.T) {
	ctx, _, _, q := newOAuthStoreTx(t)
	userID := newOAuthStoreUser(ctx, t, q)
	handle := make([]byte, 32)
	handle[0] = 1
	if _, err := q.CreateSecondFactorPolicy(ctx, store.CreateSecondFactorPolicyParams{
		UserID: userID, WebauthnUserHandle: handle, EnabledAt: totpStoreNow,
	}); err != nil {
		t.Fatalf("CreateSecondFactorPolicy: %v", err)
	}

	claimed, err := q.ClaimSecondFactorAttemptMailWindow(ctx, store.ClaimSecondFactorAttemptMailWindowParams{
		UserID: userID, Now: totpStoreNow,
	})
	if err != nil {
		t.Fatalf("first ClaimSecondFactorAttemptMailWindow: %v", err)
	}
	if claimed.AttemptMailAt == nil || !claimed.AttemptMailAt.Equal(totpStoreNow) {
		t.Errorf("claimed policy = %+v, want attempt_mail_at %s", claimed, totpStoreNow)
	}

	if _, suppressedErr := q.ClaimSecondFactorAttemptMailWindow(ctx, store.ClaimSecondFactorAttemptMailWindowParams{
		UserID: userID, Now: totpStoreNow.Add(59 * time.Minute),
	}); !errors.Is(suppressedErr, pgx.ErrNoRows) {
		t.Errorf("claim inside the hour error = %v, want pgx.ErrNoRows", suppressedErr)
	}

	reclaimed, err := q.ClaimSecondFactorAttemptMailWindow(ctx, store.ClaimSecondFactorAttemptMailWindowParams{
		UserID: userID, Now: totpStoreNow.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("claim exactly one hour later: %v", err)
	}
	if reclaimed.AttemptMailAt == nil || !reclaimed.AttemptMailAt.Equal(totpStoreNow.Add(time.Hour)) {
		t.Errorf("reclaimed policy = %+v, want attempt_mail_at %s", reclaimed, totpStoreNow.Add(time.Hour))
	}
}
