package secondfactor

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

func TestTOTPReencryptBatchLimit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		rowsProcessed int64
		want          int32
	}{
		{rowsProcessed: 0, want: 200},
		{rowsProcessed: 9_800, want: 200},
		{rowsProcessed: 9_900, want: 100},
		{rowsProcessed: 9_999, want: 1},
		{rowsProcessed: 10_000, want: 0},
	}
	for _, tc := range cases {
		if got := totpReencryptBatchLimit(tc.rowsProcessed, totpReencryptBatchRows); got != tc.want {
			t.Errorf("totpReencryptBatchLimit(%d, %d) = %d, want %d", tc.rowsProcessed, totpReencryptBatchRows, got, tc.want)
		}
	}
}

func newTOTPRotationPool(t *testing.T) *store.Pool {
	t.Helper()
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	return pool
}

func totpRotationContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func newTOTPRotationUser(ctx context.Context, t *testing.T, q *store.Queries) uuid.UUID {
	t.Helper()
	u, err := q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.com", Name: "Test User"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return u.ID
}

func newTOTPRotationSession(ctx context.Context, t *testing.T, q *store.Queries, userID uuid.UUID, now time.Time) uuid.UUID {
	t.Helper()
	sess, err := q.CreateSession(ctx, store.CreateSessionParams{
		UserID: userID, TokenHash: totpRotationRandomBytes(t, 32), CSRFSecret: totpRotationRandomBytes(t, 32),
		CreatedAt: now, LastSeenAt: now, ReauthenticatedAt: now, AbsoluteExpiresAt: now.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return sess.ID
}

func totpRotationNow() time.Time {
	return time.Now().UTC()
}

func totpRotationRandomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	return b
}

const totpRotationBogusKeyID = "tk1_qqqqqqqqqqqqqqqqqqqqqq"

func newTOTPRotationRings(t *testing.T) (oldRing, ring *TOTPKeyRing) {
	t.Helper()
	oldRing, err := NewTOTPKeyRing(testTOTPKeySeqText, "", rand.Reader)
	if err != nil {
		t.Fatalf("NewTOTPKeyRing(old) error = %v", err)
	}
	ring, err = NewTOTPKeyRing(testTOTPKeyZeroText, testTOTPKeySeqText, rand.Reader)
	if err != nil {
		t.Fatalf("NewTOTPKeyRing(active+previous) error = %v", err)
	}
	return oldRing, ring
}

func newTOTPRotationRunner(t *testing.T, pool *store.Pool, ring *TOTPKeyRing, now func() time.Time) *TOTPRotationRunner {
	t.Helper()
	signal := NewTOTPUnavailableSignal(now, nil)
	runner, err := NewTOTPRotationRunner(TOTPRotationConfig{Pool: pool, Ring: ring, Now: now, Signal: signal})
	if err != nil {
		t.Fatalf("NewTOTPRotationRunner() error = %v", err)
	}
	return runner
}

func containsTOTPReencryptFailure(failures []TOTPReencryptFailure, kind TOTPRecordKind, id uuid.UUID) bool {
	for _, f := range failures {
		if f.Kind == kind && f.ID == id {
			return true
		}
	}
	return false
}

func TestTOTPRotationRunner_ReencryptsCredentialUnderFreshNonceSameID(t *testing.T) {
	pool := newTOTPRotationPool(t)
	ctx := totpRotationContext(t)
	q := store.New(pool)
	now := totpRotationNow()

	oldRing, ring := newTOTPRotationRings(t)
	userID := newTOTPRotationUser(ctx, t, q)
	rowID := uuid.Must(uuid.NewV7())
	secret, err := GenerateTOTPSecret(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateTOTPSecret() error = %v", err)
	}
	sealed, err := oldRing.Seal(TOTPRecordKindCredential, userID, rowID, secret)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if _, err = q.InstallTOTPCredential(ctx, store.InstallTOTPCredentialParams{
		ID: rowID, UserID: userID, KeyID: sealed.KeyID, Nonce: sealed.Nonce[:], Ciphertext: sealed.Ciphertext,
		LastUsedStep: 0, CreatedAt: now,
	}); err != nil {
		t.Fatalf("InstallTOTPCredential: %v", err)
	}

	runner := newTOTPRotationRunner(t, pool, ring, func() time.Time { return now })
	result, err := runner.Run(ctx)
	requireOwnTOTPRowRotated(t, result, err, TOTPRecordKindCredential, rowID)
	if result.CredentialsReencrypted < 1 {
		t.Errorf("CredentialsReencrypted = %d, want at least 1", result.CredentialsReencrypted)
	}

	row, err := q.GetTOTPCredentialForUpdate(ctx, userID)
	if err != nil {
		t.Fatalf("GetTOTPCredentialForUpdate: %v", err)
	}
	if row.ID != rowID {
		t.Errorf("row ID changed: got %s, want %s", row.ID, rowID)
	}
	if row.KeyID != ring.ActiveKeyID() {
		t.Errorf("KeyID = %s, want the active key %s", row.KeyID, ring.ActiveKeyID())
	}
	if bytes.Equal(row.Nonce, sealed.Nonce[:]) {
		t.Error("nonce did not change across re-encryption, want a fresh nonce")
	}
	reopened := SealedTOTPSecret{KeyID: row.KeyID, Ciphertext: row.Ciphertext, Version: int(row.FormatVersion)}
	copy(reopened.Nonce[:], row.Nonce)
	gotSecret, err := ring.Open(TOTPRecordKindCredential, userID, rowID, reopened)
	if err != nil {
		t.Fatalf("Open() the re-encrypted row error = %v", err)
	}
	if gotSecret != secret {
		t.Error("secret changed across re-encryption, want it unchanged")
	}
}

func TestTOTPRotationRunner_ReencryptsUnexpiredEnrollment(t *testing.T) {
	pool := newTOTPRotationPool(t)
	ctx := totpRotationContext(t)
	q := store.New(pool)
	now := totpRotationNow()

	oldRing, ring := newTOTPRotationRings(t)
	userID := newTOTPRotationUser(ctx, t, q)
	sessionID := newTOTPRotationSession(ctx, t, q, userID, now)
	rowID := uuid.Must(uuid.NewV7())
	secret, err := GenerateTOTPSecret(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateTOTPSecret() error = %v", err)
	}
	sealed, err := oldRing.Seal(TOTPRecordKindEnrollment, userID, rowID, secret)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	tokenDigest := totpRotationRandomBytes(t, 32)
	if _, err = q.CreateTOTPEnrollment(ctx, store.CreateTOTPEnrollmentParams{
		ID: rowID, TokenDigest: tokenDigest, UserID: userID, SessionID: sessionID,
		AuthEpoch: 0, Issuer: "aboutme.vn", KeyID: sealed.KeyID, Nonce: sealed.Nonce[:], Ciphertext: sealed.Ciphertext,
		CreatedAt: now, ExpiresAt: now.Add(10 * time.Minute),
	}); err != nil {
		t.Fatalf("CreateTOTPEnrollment: %v", err)
	}

	runner := newTOTPRotationRunner(t, pool, ring, func() time.Time { return now })
	result, err := runner.Run(ctx)
	requireOwnTOTPRowRotated(t, result, err, TOTPRecordKindEnrollment, rowID)
	if result.EnrollmentsReencrypted < 1 {
		t.Errorf("EnrollmentsReencrypted = %d, want at least 1", result.EnrollmentsReencrypted)
	}
	if result.EnrollmentsExpiredDeleted != 0 {
		t.Errorf("EnrollmentsExpiredDeleted = %d, want 0 for an unexpired row", result.EnrollmentsExpiredDeleted)
	}

	row, err := q.GetTOTPEnrollmentForUpdate(ctx, tokenDigest)
	if err != nil {
		t.Fatalf("GetTOTPEnrollmentForUpdate: %v", err)
	}
	if row.ID != rowID {
		t.Errorf("row ID changed: got %s, want %s", row.ID, rowID)
	}
	if row.KeyID != ring.ActiveKeyID() {
		t.Errorf("KeyID = %s, want the active key %s", row.KeyID, ring.ActiveKeyID())
	}
	if bytes.Equal(row.Nonce, sealed.Nonce[:]) {
		t.Error("nonce did not change across re-encryption, want a fresh nonce")
	}
	reopened := SealedTOTPSecret{KeyID: row.KeyID, Ciphertext: row.Ciphertext, Version: int(row.FormatVersion)}
	copy(reopened.Nonce[:], row.Nonce)
	gotSecret, err := ring.Open(TOTPRecordKindEnrollment, userID, rowID, reopened)
	if err != nil {
		t.Fatalf("Open() the re-encrypted row error = %v", err)
	}
	if gotSecret != secret {
		t.Error("secret changed across re-encryption, want it unchanged")
	}
}

func TestTOTPRotationRunner_DeletesExpiredEnrollmentWithoutDecrypting(t *testing.T) {
	pool := newTOTPRotationPool(t)
	ctx := totpRotationContext(t)
	q := store.New(pool)
	now := totpRotationNow()
	created := now.Add(-11 * time.Minute)

	_, ring := newTOTPRotationRings(t)
	userID := newTOTPRotationUser(ctx, t, q)
	sessionID := newTOTPRotationSession(ctx, t, q, userID, created)
	rowID := uuid.Must(uuid.NewV7())
	tokenDigest := totpRotationRandomBytes(t, 32)
	if _, err := q.CreateTOTPEnrollment(ctx, store.CreateTOTPEnrollmentParams{
		ID: rowID, TokenDigest: tokenDigest, UserID: userID, SessionID: sessionID,
		AuthEpoch: 0, Issuer: "aboutme.vn", KeyID: testTOTPKeySeqID,
		Nonce: totpRotationRandomBytes(t, 12), Ciphertext: totpRotationRandomBytes(t, 36),
		CreatedAt: created, ExpiresAt: created.Add(10 * time.Minute),
	}); err != nil {
		t.Fatalf("CreateTOTPEnrollment: %v", err)
	}

	runner := newTOTPRotationRunner(t, pool, ring, func() time.Time { return now })
	result, err := runner.Run(ctx)
	requireOwnTOTPRowRotated(t, result, err, TOTPRecordKindEnrollment, rowID)
	if result.EnrollmentsExpiredDeleted < 1 {
		t.Errorf("EnrollmentsExpiredDeleted = %d, want at least 1", result.EnrollmentsExpiredDeleted)
	}
	if containsTOTPReencryptFailure(result.DecryptFailures, TOTPRecordKindEnrollment, rowID) {
		t.Error("the expired row was decrypted (recorded as a decrypt failure), want it deleted unopened")
	}
	if _, err = q.GetTOTPEnrollmentForUpdate(ctx, tokenDigest); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetTOTPEnrollmentForUpdate() error = %v, want pgx.ErrNoRows (the row gone)", err)
	}
}

func TestTOTPRotationRunner_DecryptFailureIsCountedRunContinuesButFails(t *testing.T) {
	pool := newTOTPRotationPool(t)
	ctx := totpRotationContext(t)
	q := store.New(pool)
	now := totpRotationNow()

	oldRing, ring := newTOTPRotationRings(t)

	goodUserID := newTOTPRotationUser(ctx, t, q)
	goodRowID := uuid.Must(uuid.NewV7())
	secret, err := GenerateTOTPSecret(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateTOTPSecret() error = %v", err)
	}
	goodSealed, err := oldRing.Seal(TOTPRecordKindCredential, goodUserID, goodRowID, secret)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if _, err = q.InstallTOTPCredential(ctx, store.InstallTOTPCredentialParams{
		ID: goodRowID, UserID: goodUserID, KeyID: goodSealed.KeyID, Nonce: goodSealed.Nonce[:], Ciphertext: goodSealed.Ciphertext,
		LastUsedStep: 0, CreatedAt: now,
	}); err != nil {
		t.Fatalf("InstallTOTPCredential(good): %v", err)
	}

	badUserID := newTOTPRotationUser(ctx, t, q)
	badRowID := uuid.Must(uuid.NewV7())
	if _, err = q.InstallTOTPCredential(ctx, store.InstallTOTPCredentialParams{
		ID: badRowID, UserID: badUserID, KeyID: totpRotationBogusKeyID,
		Nonce: totpRotationRandomBytes(t, 12), Ciphertext: totpRotationRandomBytes(t, 36),
		LastUsedStep: 0, CreatedAt: now,
	}); err != nil {
		t.Fatalf("InstallTOTPCredential(bad): %v", err)
	}

	runner := newTOTPRotationRunner(t, pool, ring, func() time.Time { return now })
	result, err := runner.Run(ctx)
	if !errors.Is(err, ErrTOTPReencryptFailed) {
		t.Fatalf("Run() error = %v, want ErrTOTPReencryptFailed", err)
	}
	if !containsTOTPReencryptFailure(result.DecryptFailures, TOTPRecordKindCredential, badRowID) {
		t.Errorf("DecryptFailures = %+v, want it to contain the bad row %s", result.DecryptFailures, badRowID)
	}
	if result.CredentialsReencrypted < 1 {
		t.Errorf("CredentialsReencrypted = %d, want at least 1 (the good row still processed)", result.CredentialsReencrypted)
	}

	goodRow, err := q.GetTOTPCredentialForUpdate(ctx, goodUserID)
	if err != nil {
		t.Fatalf("GetTOTPCredentialForUpdate(good): %v", err)
	}
	if goodRow.KeyID != ring.ActiveKeyID() {
		t.Errorf("good row KeyID = %s, want the active key %s (run must continue past the bad row)", goodRow.KeyID, ring.ActiveKeyID())
	}

	badRow, err := q.GetTOTPCredentialForUpdate(ctx, badUserID)
	if err != nil {
		t.Fatalf("GetTOTPCredentialForUpdate(bad): %v", err)
	}
	if badRow.KeyID != totpRotationBogusKeyID {
		t.Errorf("bad row KeyID = %s, want it left unchanged at %s", badRow.KeyID, totpRotationBogusKeyID)
	}
}

func TestTOTPRotationRunner_PagesPastDecryptFailuresWithoutRepeats(t *testing.T) {
	pool := newTOTPRotationPool(t)
	ctx := totpRotationContext(t)
	q := store.New(pool)
	now := totpRotationNow()

	oldRing, ring := newTOTPRotationRings(t)

	badIDs := make([]uuid.UUID, 0, 3)
	for i := 0; i < 3; i++ {
		userID := newTOTPRotationUser(ctx, t, q)
		rowID := uuid.Must(uuid.NewV7())
		if _, err := q.InstallTOTPCredential(ctx, store.InstallTOTPCredentialParams{
			ID: rowID, UserID: userID, KeyID: totpRotationBogusKeyID,
			Nonce: totpRotationRandomBytes(t, 12), Ciphertext: totpRotationRandomBytes(t, 36),
			LastUsedStep: 0, CreatedAt: now,
		}); err != nil {
			t.Fatalf("InstallTOTPCredential(bad %d): %v", i, err)
		}
		badIDs = append(badIDs, rowID)
	}

	goodUsers := make([]uuid.UUID, 0, 2)
	for i := 0; i < 2; i++ {
		userID := newTOTPRotationUser(ctx, t, q)
		rowID := uuid.Must(uuid.NewV7())
		secret, err := GenerateTOTPSecret(rand.Reader)
		if err != nil {
			t.Fatalf("GenerateTOTPSecret() error = %v", err)
		}
		sealed, err := oldRing.Seal(TOTPRecordKindCredential, userID, rowID, secret)
		if err != nil {
			t.Fatalf("Seal() error = %v", err)
		}
		if _, err = q.InstallTOTPCredential(ctx, store.InstallTOTPCredentialParams{
			ID: rowID, UserID: userID, KeyID: sealed.KeyID, Nonce: sealed.Nonce[:], Ciphertext: sealed.Ciphertext,
			LastUsedStep: 0, CreatedAt: now,
		}); err != nil {
			t.Fatalf("InstallTOTPCredential(good %d): %v", i, err)
		}
		goodUsers = append(goodUsers, userID)
	}

	runner := newTOTPRotationRunner(t, pool, ring, func() time.Time { return now })
	runner.batchRows = 2

	result, err := runner.Run(ctx)
	if !errors.Is(err, ErrTOTPReencryptFailed) {
		t.Fatalf("Run() error = %v, want ErrTOTPReencryptFailed", err)
	}

	seen := make(map[uuid.UUID]int, len(result.DecryptFailures))
	for _, f := range result.DecryptFailures {
		seen[f.ID]++
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("row %s appears %d times in DecryptFailures, want at most once (paging repeated a row)", id, count)
		}
	}
	for _, id := range badIDs {
		if seen[id] != 1 {
			t.Errorf("bad row %s appears %d times in DecryptFailures, want exactly 1", id, seen[id])
		}
	}

	for i, userID := range goodUsers {
		row, err := q.GetTOTPCredentialForUpdate(ctx, userID)
		if err != nil {
			t.Fatalf("GetTOTPCredentialForUpdate(good %d): %v", i, err)
		}
		if row.KeyID != ring.ActiveKeyID() {
			t.Errorf("good row %d KeyID = %s, want the active key %s (paging must still reach every row)", i, row.KeyID, ring.ActiveKeyID())
		}
	}
}

type totpRotationStepClock struct {
	mu   sync.Mutex
	next time.Time
	step time.Duration
}

func (c *totpRotationStepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.next
	c.next = c.next.Add(c.step)
	return now
}

func TestTOTPRotationRunner_TimeBudgetStopsEarlyAndReportsLimitReached(t *testing.T) {
	pool := newTOTPRotationPool(t)
	ctx := totpRotationContext(t)
	q := store.New(pool)
	start := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)

	oldRing, ring := newTOTPRotationRings(t)
	userID := newTOTPRotationUser(ctx, t, q)
	rowID := uuid.Must(uuid.NewV7())
	secret, err := GenerateTOTPSecret(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateTOTPSecret() error = %v", err)
	}
	sealed, err := oldRing.Seal(TOTPRecordKindCredential, userID, rowID, secret)
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	if _, err = q.InstallTOTPCredential(ctx, store.InstallTOTPCredentialParams{
		ID: rowID, UserID: userID, KeyID: sealed.KeyID, Nonce: sealed.Nonce[:], Ciphertext: sealed.Ciphertext,
		LastUsedStep: 0, CreatedAt: start,
	}); err != nil {
		t.Fatalf("InstallTOTPCredential: %v", err)
	}

	clock := &totpRotationStepClock{next: start, step: totpReencryptMaxDuration + time.Minute}
	runner := newTOTPRotationRunner(t, pool, ring, clock.Now)
	result, err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("Run() error = %v, want nil: an exhausted time budget is not a failure", err)
	}
	if !result.LimitReached {
		t.Error("LimitReached = false, want true")
	}
	if result.Batches != 0 {
		t.Errorf("Batches = %d, want 0: the deadline check must trip before the first batch", result.Batches)
	}
	if result.CredentialsReencrypted != 0 {
		t.Errorf("CredentialsReencrypted = %d, want 0", result.CredentialsReencrypted)
	}

	row, err := q.GetTOTPCredentialForUpdate(ctx, userID)
	if err != nil {
		t.Fatalf("GetTOTPCredentialForUpdate: %v", err)
	}
	if row.KeyID != sealed.KeyID {
		t.Errorf("row was rewritten despite the exhausted time budget: KeyID = %s, want unchanged %s", row.KeyID, sealed.KeyID)
	}
}

func requireOwnTOTPRowRotated(t *testing.T, result TOTPReencryptResult, err error, kind TOTPRecordKind, rowID uuid.UUID) {
	t.Helper()
	if err != nil && !errors.Is(err, ErrTOTPReencryptFailed) {
		t.Fatalf("Run() error = %v", err)
	}
	if containsTOTPReencryptFailure(result.DecryptFailures, kind, rowID) {
		t.Fatalf("Run() could not decrypt this test's %s row", kind)
	}
}
