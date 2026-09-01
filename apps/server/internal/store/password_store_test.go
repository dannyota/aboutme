// Phase PA task 1's live-database store tests: cascade behavior, SKIP LOCKED
// claim disjointness, stale-lease recovery, bounded cleanup, and live key-ID
// listing — all against the real generated query layer (store.New) so the
// sqlc contract and the D3 constraints are proven together.
package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

var _ store.PasswordQueries = (*store.Queries)(nil)

// newPasswordStoreTx returns a rolled-back transaction plus the underlying
// pool, matching newPublicStoreTx. Every query and raw SQL goes through the
// transaction so repeated runs never accumulate rows in the shared database.
func newPasswordStoreTx(t *testing.T) (context.Context, *pgxpool.Pool, pgx.Tx, *store.Queries) {
	t.Helper()
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error: %v", err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error: %v", err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("Rollback() error: %v", err)
		}
	})
	return ctx, pool, tx, store.New(tx)
}

// lockExistingAuthEmailJobs holds row locks on the shared queue until the
// test finishes, so SKIP LOCKED claims only fixtures inserted afterward.
func lockExistingAuthEmailJobs(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin queue blocker: %v", err)
	}
	cleanupCtx := context.WithoutCancel(ctx)
	t.Cleanup(func() {
		if rollbackErr := tx.Rollback(cleanupCtx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("Rollback queue blocker: %v", rollbackErr)
		}
	})

	rows, err := tx.Query(ctx, `SELECT id FROM auth_email_jobs FOR UPDATE`)
	if err != nil {
		t.Fatalf("lock existing auth email jobs: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan existing auth email job: %v", err)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate existing auth email jobs: %v", err)
	}
}

func newPasswordStoreUser(ctx context.Context, t *testing.T, q *store.Queries) uuid.UUID {
	t.Helper()
	u, err := q.CreateUser(ctx, store.CreateUserParams{
		Email: uuid.NewString() + "@example.com",
		Name:  "Password Store Test",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return u.ID
}

// newPendingVerifyJob creates a pending verify job that is immediately due
// (next_attempt_at == now), so claim tests observe it without a timing race.
func newPendingVerifyJob(ctx context.Context, t *testing.T, q *store.Queries, regID uuid.UUID) store.AuthEmailJob {
	t.Helper()
	now := time.Now().UTC()
	keyID := "k-test"
	job, err := q.CreateAuthEmailJob(ctx, store.CreateAuthEmailJobParams{
		ID:             uuid.New(),
		Kind:           "verify",
		State:          "pending",
		RegistrationID: &regID,
		TokenDigest:    uniqueTokenDigest(),
		KeyID:          &keyID,
		Nonce:          bytesOf(12, 'n'),
		Ciphertext:     bytesOf(64, 'c'),
		CreatedAt:      now,
		ExpiresAt:      now.Add(24 * time.Hour),
		NextAttemptAt:  &now,
	})
	if err != nil {
		t.Fatalf("CreateAuthEmailJob: %v", err)
	}
	return job
}

func newPasswordRegistration(ctx context.Context, t *testing.T, q *store.Queries) uuid.UUID {
	t.Helper()
	now := time.Now().UTC()
	reg, err := q.CreatePasswordRegistration(ctx, store.CreatePasswordRegistrationParams{
		Email:       uuid.NewString() + "@example.com",
		Name:        "Registration",
		EncodedHash: bytesOf(60, 'h'),
		TokenDigest: uniqueTokenDigest(),
		CreatedAt:   now,
		ExpiresAt:   now.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CreatePasswordRegistration: %v", err)
	}
	return reg.ID
}

func TestPasswordAuthRegistrationDeleteCascadesJobs(t *testing.T) {
	ctx, _, tx, q := newPasswordStoreTx(t)

	regID := newPasswordRegistration(ctx, t, q)
	newPendingVerifyJob(ctx, t, q, regID)

	if _, err := q.DeletePasswordRegistration(ctx, regID); err != nil {
		t.Fatalf("DeletePasswordRegistration: %v", err)
	}

	var n int
	if err := tx.QueryRow(ctx,
		`SELECT count(*) FROM auth_email_jobs WHERE registration_id = $1`, regID,
	).Scan(&n); err != nil {
		t.Fatalf("count jobs after cascade: %v", err)
	}
	if n != 0 {
		t.Errorf("auth_email_jobs for deleted registration = %d, want 0 (cascade)", n)
	}
}

func TestPasswordAuthUserDeleteCascades(t *testing.T) {
	ctx, _, tx, q := newPasswordStoreTx(t)
	userID := newPasswordStoreUser(ctx, t, q)
	now := time.Now().UTC()

	if _, err := q.UpsertPasswordCredential(ctx, store.UpsertPasswordCredentialParams{
		UserID:      userID,
		EncodedHash: bytesOf(60, 'h'),
		CreatedAt:   now,
		ChangedAt:   now,
	}); err != nil {
		t.Fatalf("UpsertPasswordCredential: %v", err)
	}
	if _, err := q.CreatePasswordResetToken(ctx, store.CreatePasswordResetTokenParams{
		UserID:      userID,
		TokenDigest: uniqueTokenDigest(),
		CreatedAt:   now,
		ExpiresAt:   now.Add(30 * time.Minute),
	}); err != nil {
		t.Fatalf("CreatePasswordResetToken: %v", err)
	}
	keyID := "k-test"
	next := now.Add(time.Minute)
	if _, err := q.CreateAuthEmailJob(ctx, store.CreateAuthEmailJobParams{
		ID:            uuid.New(),
		Kind:          "password_changed",
		State:         "pending",
		UserID:        &userID,
		KeyID:         &keyID,
		Nonce:         bytesOf(12, 'n'),
		Ciphertext:    bytesOf(64, 'c'),
		CreatedAt:     now,
		ExpiresAt:     now.Add(24 * time.Hour),
		NextAttemptAt: &next,
	}); err != nil {
		t.Fatalf("CreateAuthEmailJob: %v", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	for table, query := range map[string]string{
		"credential": `SELECT count(*) FROM password_credentials WHERE user_id = $1`,
		"reset":      `SELECT count(*) FROM password_reset_tokens WHERE user_id = $1`,
		"job":        `SELECT count(*) FROM auth_email_jobs WHERE user_id = $1`,
	} {
		var n int
		if err := tx.QueryRow(ctx, query, userID).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("%s rows after user delete = %d, want 0 (cascade)", table, n)
		}
	}
}

func TestPasswordAuthClaimIsDisjointUnderSkipLocked(t *testing.T) {
	ctx, pool, _, _ := newPasswordStoreTx(t)
	lockExistingAuthEmailJobs(ctx, t, pool)
	seed := store.New(pool) // committed so both claimers can see the jobs

	reg1 := newPasswordRegistration(ctx, t, seed)
	reg2 := newPasswordRegistration(ctx, t, seed)
	fixture1 := newPendingVerifyJob(ctx, t, seed, reg1)
	fixture2 := newPendingVerifyJob(ctx, t, seed, reg2)
	t.Cleanup(func() {
		if _, err := seed.DeletePasswordRegistration(context.Background(), reg1); err != nil {
			t.Errorf("cleanup delete registration: %v", err)
		}
		if _, err := seed.DeletePasswordRegistration(context.Background(), reg2); err != nil {
			t.Errorf("cleanup delete registration: %v", err)
		}
	})

	now := time.Now().UTC()
	tx1, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin tx1: %v", err)
	}
	defer func() {
		if rollbackErr := tx1.Rollback(context.Background()); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("Rollback() error: %v", rollbackErr)
		}
	}()
	tx2, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin tx2: %v", err)
	}
	defer func() {
		if rollbackErr := tx2.Rollback(context.Background()); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("Rollback() error: %v", rollbackErr)
		}
	}()

	claimed1, err := store.New(tx1).ClaimAuthEmailJobs(ctx, store.ClaimAuthEmailJobsParams{
		LeaseOwner:     "worker-1",
		LeaseExpiresAt: now.Add(30 * time.Second),
		Now:            now,
		LimitRows:      1,
	})
	if err != nil {
		t.Fatalf("ClaimAuthEmailJobs(tx1): %v", err)
	}
	claimed2, err := store.New(tx2).ClaimAuthEmailJobs(ctx, store.ClaimAuthEmailJobsParams{
		LeaseOwner:     "worker-2",
		LeaseExpiresAt: now.Add(30 * time.Second),
		Now:            now,
		LimitRows:      1,
	})
	if err != nil {
		t.Fatalf("ClaimAuthEmailJobs(tx2): %v", err)
	}

	if len(claimed1) != 1 || len(claimed2) != 1 {
		t.Fatalf("claimed counts = (%d, %d), want (1, 1)", len(claimed1), len(claimed2))
	}
	if claimed1[0].ID == claimed2[0].ID {
		t.Fatalf("both claimers returned the same job %s (SKIP LOCKED failed)", claimed1[0].ID)
	}
	fixtureIDs := map[uuid.UUID]struct{}{fixture1.ID: {}, fixture2.ID: {}}
	if _, ok := fixtureIDs[claimed1[0].ID]; !ok {
		t.Fatalf("tx1 claimed unrelated job %s, want one of the fixture jobs", claimed1[0].ID)
	}
	if _, ok := fixtureIDs[claimed2[0].ID]; !ok {
		t.Fatalf("tx2 claimed unrelated job %s, want one of the fixture jobs", claimed2[0].ID)
	}
}

func TestPasswordAuthStaleLeaseRequeueDecrementsAttempts(t *testing.T) {
	ctx, pool, tx, q := newPasswordStoreTx(t)
	lockExistingAuthEmailJobs(ctx, t, pool)

	regID := newPasswordRegistration(ctx, t, q)
	newPendingVerifyJob(ctx, t, q, regID)

	now := time.Now().UTC()
	claimed, err := q.ClaimAuthEmailJobs(ctx, store.ClaimAuthEmailJobsParams{
		LeaseOwner:     "worker",
		LeaseExpiresAt: now.Add(30 * time.Second),
		Now:            now,
		LimitRows:      1,
	})
	if err != nil {
		t.Fatalf("ClaimAuthEmailJobs: %v", err)
	}
	if len(claimed) != 1 || claimed[0].Attempts != 1 {
		t.Fatalf("claimed = %+v, want exactly one leased job with attempts 1", claimed)
	}

	if _, execErr := tx.Exec(ctx,
		`UPDATE auth_email_jobs SET lease_expires_at = $2 WHERE id = $1`,
		claimed[0].ID, now.Add(-time.Second),
	); execErr != nil {
		t.Fatalf("expire lease: %v", execErr)
	}
	requeued, err := q.RequeueExpiredAuthEmailLeases(ctx, store.RequeueExpiredAuthEmailLeasesParams{
		Now:       now,
		LimitRows: 1,
	})
	if err != nil {
		t.Fatalf("RequeueExpiredAuthEmailLeases: %v", err)
	}
	if requeued != 1 {
		t.Fatalf("RequeueExpiredAuthEmailLeases affected = %d, want 1", requeued)
	}

	var state string
	var attempts int32
	if err := tx.QueryRow(ctx,
		`SELECT state, attempts FROM auth_email_jobs WHERE id = $1`, claimed[0].ID,
	).Scan(&state, &attempts); err != nil {
		t.Fatalf("read requeued job: %v", err)
	}
	if state != "pending" || attempts != 0 {
		t.Fatalf("requeued job = (state=%s, attempts=%d), want (pending, 0)", state, attempts)
	}
}

func TestPasswordAuthCleanupIsBounded(t *testing.T) {
	ctx, _, _, q := newPasswordStoreTx(t)

	now := time.Now().UTC()
	expired := now.Add(-time.Hour)
	for i := 0; i < 5; i++ {
		if _, err := q.CreatePasswordRegistration(ctx, store.CreatePasswordRegistrationParams{
			Email:       uuid.NewString() + "@example.com",
			Name:        "Expired",
			EncodedHash: bytesOf(60, 'h'),
			TokenDigest: uniqueTokenDigest(),
			CreatedAt:   expired.Add(-24 * time.Hour),
			ExpiresAt:   expired,
		}); err != nil {
			t.Fatalf("CreatePasswordRegistration: %v", err)
		}
	}

	deleted, err := q.CleanupExpiredPasswordRegistrations(ctx, store.CleanupExpiredPasswordRegistrationsParams{
		Cutoff:    now,
		LimitRows: 2,
	})
	if err != nil {
		t.Fatalf("CleanupExpiredPasswordRegistrations: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("cleanup deleted %d, want 2 (limit respected)", deleted)
	}
}

func TestPasswordAuthListLiveKeyIDs(t *testing.T) {
	ctx, _, _, q := newPasswordStoreTx(t)

	reg1 := newPasswordRegistration(ctx, t, q)
	reg2 := newPasswordRegistration(ctx, t, q)

	mk := func(regID uuid.UUID, keyID string) {
		now := time.Now().UTC()
		if _, err := q.CreateAuthEmailJob(ctx, store.CreateAuthEmailJobParams{
			ID:             uuid.New(),
			Kind:           "verify",
			State:          "pending",
			RegistrationID: &regID,
			TokenDigest:    uniqueTokenDigest(),
			KeyID:          &keyID,
			Nonce:          bytesOf(12, 'n'),
			Ciphertext:     bytesOf(64, 'c'),
			CreatedAt:      now,
			ExpiresAt:      now.Add(24 * time.Hour),
			NextAttemptAt:  &now,
		}); err != nil {
			t.Fatalf("CreateAuthEmailJob: %v", err)
		}
	}
	mk(reg1, "k-shared")
	mk(reg2, "k-shared")
	mk(reg2, "k-other")

	keys, err := q.ListLiveAuthEmailJobKeyIDs(ctx, time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListLiveAuthEmailJobKeyIDs: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("live key IDs = %v, want exactly 2 distinct keys", keys)
	}
}

func bytesOf(n int, fill byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = fill
	}
	return b
}

// uniqueTokenDigest returns a fresh random 32-byte digest each call: the digest
// is globally unique in the schema, so a shared constant would collide across
// the parallel package tests that share the one migrated database.
func uniqueTokenDigest() []byte { return []byte(uuid.NewString() + uuid.NewString())[:32] }
