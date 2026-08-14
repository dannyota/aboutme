package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

var (
	_ store.PublicReadQueries     = (*store.Queries)(nil)
	_ store.PublicMutationQueries = (*store.Queries)(nil)
)

func newPublicStoreTx(t *testing.T) (context.Context, *pgxpool.Pool, pgx.Tx, *store.Queries) {
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

func createPublicStoreUser(ctx context.Context, t *testing.T, tx pgx.Tx) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id
	`, uuid.NewString()+"@example.com", "Public Store Test").Scan(&id); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

func createPublicStoreResume(
	ctx context.Context,
	t *testing.T,
	tx pgx.Tx,
	userID uuid.UUID,
	slug *string,
	live bool,
	seo bool,
) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO resumes (
			user_id, title, slug, live, download_enabled, seo_geo_enabled,
			schema_version, personal_details, content, customization
		) VALUES ($1, 'Public Store Resume', $2, $3, true, $4, 2,
			'{}'::jsonb, '{}'::jsonb, '{}'::jsonb)
		RETURNING id
	`, userID, slug, live, seo).Scan(&id); err != nil {
		t.Fatalf("insert resume: %v", err)
	}
	return id
}

func requireNoRows(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("error = %v, want pgx.ErrNoRows", err)
	}
}

func requireConstraint(t *testing.T, err error, name string) {
	t.Helper()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("error = %v, want PostgreSQL constraint %q", err, name)
	}
	if pgErr.ConstraintName != name {
		t.Fatalf("constraint = %q, want %q", pgErr.ConstraintName, name)
	}
}

func TestPublicStateGeneratedContract(t *testing.T) {
	ctx, _, _, queries := newPublicStoreTx(t)

	state, err := queries.GetPublicState(ctx)
	if err != nil {
		t.Fatalf("GetPublicState() error: %v", err)
	}
	if !state.Singleton || state.DiscoveryGeneration != 1 {
		t.Fatalf("GetPublicState() = %+v, want singleton generation 1", state)
	}
	locked, err := queries.LockPublicState(ctx)
	if err != nil {
		t.Fatalf("LockPublicState() error: %v", err)
	}
	if locked != state {
		t.Fatalf("LockPublicState() = %+v, want %+v", locked, state)
	}
	generation, err := queries.AdvanceDiscoveryGeneration(ctx)
	if err != nil {
		t.Fatalf("AdvanceDiscoveryGeneration() error: %v", err)
	}
	if generation != 2 {
		t.Fatalf("AdvanceDiscoveryGeneration() = %d, want 2", generation)
	}
}

func TestPublicReadUniformAbsence(t *testing.T) {
	ctx, _, tx, queries := newPublicStoreTx(t)
	ownerID := createPublicStoreUser(ctx, t, tx)
	otherID := createPublicStoreUser(ctx, t, tx)
	hiddenSlug := "hidden-resume"
	hiddenID := createPublicStoreResume(ctx, t, tx, ownerID, &hiddenSlug, false, false)
	liveSlug := "live-resume"
	liveID := createPublicStoreResume(ctx, t, tx, ownerID, &liveSlug, true, false)

	_, missingErr := queries.GetPublicResumeBySlug(ctx, "missing-resume")
	requireNoRows(t, missingErr)
	_, hiddenErr := queries.GetPublicResumeBySlug(ctx, hiddenSlug)
	requireNoRows(t, hiddenErr)
	live, err := queries.GetPublicResumeBySlug(ctx, liveSlug)
	if err != nil {
		t.Fatalf("GetPublicResumeBySlug(live) error: %v", err)
	}
	if live.ID != liveID {
		t.Fatalf("live resume ID = %s, want %s", live.ID, liveID)
	}

	owned, err := queries.GetPublicResumeByOwner(ctx, store.GetPublicResumeByOwnerParams{
		UserID: ownerID,
		ID:     hiddenID,
	})
	if err != nil {
		t.Fatalf("GetPublicResumeByOwner(owner) error: %v", err)
	}
	if owned.ID != hiddenID {
		t.Fatalf("owned resume ID = %s, want %s", owned.ID, hiddenID)
	}
	_, wrongOwnerErr := queries.GetPublicResumeByOwner(ctx, store.GetPublicResumeByOwnerParams{
		UserID: otherID,
		ID:     hiddenID,
	})
	requireNoRows(t, wrongOwnerErr)
	_, missingOwnerErr := queries.GetPublicResumeByOwner(ctx, store.GetPublicResumeByOwnerParams{
		UserID: ownerID,
		ID:     uuid.New(),
	})
	requireNoRows(t, missingOwnerErr)
}

func TestEligibleSlugsBytewise(t *testing.T) {
	ctx, _, tx, queries := newPublicStoreTx(t)
	baseline, err := queries.ListEligiblePublicSlugs(ctx)
	if err != nil {
		t.Fatalf("ListEligiblePublicSlugs(baseline) error: %v", err)
	}
	eligible := []string{"zeta", "a-00", "a000", "alpha", "a-0a"}
	for _, slug := range eligible {
		userID := createPublicStoreUser(ctx, t, tx)
		createPublicStoreResume(ctx, t, tx, userID, &slug, true, true)
	}
	for _, row := range []struct {
		slug *string
		live bool
		seo  bool
	}{
		{slug: stringPointer("private-resume"), live: false, seo: false},
		{slug: stringPointer("no-discovery"), live: true, seo: false},
		{slug: nil, live: false, seo: false},
	} {
		userID := createPublicStoreUser(ctx, t, tx)
		createPublicStoreResume(ctx, t, tx, userID, row.slug, row.live, row.seo)
	}

	want := append(append([]string(nil), baseline...), eligible...)
	sort.Strings(want)
	got, err := queries.ListEligiblePublicSlugs(ctx)
	if err != nil {
		t.Fatalf("ListEligiblePublicSlugs() error: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("eligible slug count = %d, want %d: %v", len(got), len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("eligible slugs = %v, want bytewise %v", got, want)
		}
	}
}

func stringPointer(value string) *string { return &value }

func TestSlugTombstoneExactBoundaryAndNoRefresh(t *testing.T) {
	ctx, _, tx, queries := newPublicStoreTx(t)
	userID := createPublicStoreUser(ctx, t, tx)
	releasedAt := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	tombstone, err := queries.InsertSlugTombstone(ctx, store.InsertSlugTombstoneParams{
		Slug:             "held-slug",
		ReleasedByUserID: &userID,
		ReleasedAt:       releasedAt,
	})
	if err != nil {
		t.Fatalf("InsertSlugTombstone() error: %v", err)
	}
	locked, err := queries.GetSlugTombstoneForUpdate(ctx, "held-slug")
	if err != nil {
		t.Fatalf("GetSlugTombstoneForUpdate() error: %v", err)
	}
	if locked.ID != tombstone.ID || locked.Slug != tombstone.Slug ||
		!locked.ReleasedAt.Equal(tombstone.ReleasedAt) ||
		!equalOptionalUUID(locked.ReleasedByUserID, tombstone.ReleasedByUserID) {
		t.Fatalf("locked tombstone = %+v, want %+v", locked, tombstone)
	}

	_, err = queries.ConsumeExpiredSlugTombstone(ctx, store.ConsumeExpiredSlugTombstoneParams{
		Slug:       "held-slug",
		ReusableAt: releasedAt.Add(180*24*time.Hour - time.Nanosecond),
	})
	requireNoRows(t, err)
	conflictTx, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("begin conflict savepoint: %v", err)
	}
	_, err = store.New(conflictTx).InsertSlugTombstone(ctx, store.InsertSlugTombstoneParams{
		Slug:             "held-slug",
		ReleasedByUserID: &userID,
		ReleasedAt:       releasedAt.Add(time.Hour),
	})
	requireConstraint(t, err, "slug_tombstones_slug_key")
	if rollbackErr := conflictTx.Rollback(ctx); rollbackErr != nil {
		t.Fatalf("rollback conflict savepoint: %v", rollbackErr)
	}
	unchanged, err := queries.GetSlugTombstoneForUpdate(ctx, "held-slug")
	if err != nil {
		t.Fatalf("GetSlugTombstoneForUpdate(after conflict) error: %v", err)
	}
	if !unchanged.ReleasedAt.Equal(releasedAt) {
		t.Fatalf("conflict refreshed released_at to %s, want %s", unchanged.ReleasedAt, releasedAt)
	}

	consumedID, err := queries.ConsumeExpiredSlugTombstone(ctx, store.ConsumeExpiredSlugTombstoneParams{
		Slug:       "held-slug",
		ReusableAt: releasedAt.Add(180 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("ConsumeExpiredSlugTombstone(exact boundary) error: %v", err)
	}
	if consumedID != tombstone.ID {
		t.Fatalf("consumed tombstone ID = %s, want %s", consumedID, tombstone.ID)
	}
}

func TestSlugLockUsesExactDomain(t *testing.T) {
	ctx, pool, tx, queries := newPublicStoreTx(t)
	if err := queries.LockSlugClaim(ctx, "locked-slug"); err != nil {
		t.Fatalf("LockSlugClaim() error: %v", err)
	}

	probe, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("probe Begin() error: %v", err)
	}
	t.Cleanup(func() { rollbackPublicTestTx(t, probe) })
	var sameAvailable bool
	if err := probe.QueryRow(ctx, `
		SELECT pg_try_advisory_xact_lock(
			hashtextextended('aboutme.slug.v1:' || $1::text, 0)
		)
	`, "locked-slug").Scan(&sameAvailable); err != nil {
		t.Fatalf("probe exact slug lock: %v", err)
	}
	if sameAvailable {
		t.Fatal("exact slug advisory lock remained available to a competing transaction")
	}
	var otherAvailable bool
	if err := probe.QueryRow(ctx, `
		SELECT pg_try_advisory_xact_lock(
			hashtextextended('aboutme.slug.v1:' || $1::text, 0)
		)
	`, "other-slug").Scan(&otherAvailable); err != nil {
		t.Fatalf("probe other slug lock: %v", err)
	}
	if !otherAvailable {
		t.Fatal("unrelated slug advisory lock was unavailable")
	}

	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("release exact slug lock: %v", err)
	}
}

func TestPublishRollbackRestoresClaimAndGeneration(t *testing.T) {
	ctx, _, tx, queries := newPublicStoreTx(t)
	userID := createPublicStoreUser(ctx, t, tx)
	resumeID := createPublicStoreResume(ctx, t, tx, userID, nil, false, false)
	releasedAt := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	tombstone, err := queries.InsertSlugTombstone(ctx, store.InsertSlugTombstoneParams{
		Slug:             "reclaim-slug",
		ReleasedByUserID: &userID,
		ReleasedAt:       releasedAt,
	})
	if err != nil {
		t.Fatalf("InsertSlugTombstone() error: %v", err)
	}

	mutationTx, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("begin publish transaction: %v", err)
	}
	mutation := store.New(mutationTx)
	if lockErr := mutation.LockSlugClaim(ctx, "reclaim-slug"); lockErr != nil {
		t.Fatalf("LockSlugClaim() error: %v", lockErr)
	}
	if _, consumeErr := mutation.ConsumeExpiredSlugTombstone(ctx, store.ConsumeExpiredSlugTombstoneParams{
		Slug:       "reclaim-slug",
		ReusableAt: releasedAt.Add(180 * 24 * time.Hour),
	}); consumeErr != nil {
		t.Fatalf("ConsumeExpiredSlugTombstone() error: %v", consumeErr)
	}
	updatedAt := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	claimedSlug := "reclaim-slug"
	updated, err := mutation.PublishResumeCAS(ctx, store.PublishResumeCASParams{
		ID:               resumeID,
		UserID:           userID,
		ExpectedRevision: 1,
		Slug:             &claimedSlug,
		Live:             true,
		DownloadEnabled:  true,
		SEOGeoEnabled:    true,
		UpdatedAt:        updatedAt,
	})
	if err != nil {
		t.Fatalf("PublishResumeCAS() error: %v", err)
	}
	if updated.Revision != 2 || updated.Slug == nil || *updated.Slug != claimedSlug || !updated.Live {
		t.Fatalf("published row = %+v, want claimed live revision 2", updated)
	}
	if _, lockStateErr := mutation.LockPublicState(ctx); lockStateErr != nil {
		t.Fatalf("LockPublicState() error: %v", lockStateErr)
	}
	if _, advanceErr := mutation.AdvanceDiscoveryGeneration(ctx); advanceErr != nil {
		t.Fatalf("AdvanceDiscoveryGeneration() error: %v", advanceErr)
	}
	proofKey := uuid.New()
	insertPublicProof(ctx, t, mutation, userID, proofKey, "publish")
	if rollbackErr := mutationTx.Rollback(ctx); rollbackErr != nil {
		t.Fatalf("rollback publish transaction: %v", rollbackErr)
	}

	assertPrivateResumeUnchanged(ctx, t, queries, userID, resumeID)
	assertPublicGeneration(ctx, t, queries, 1)
	if _, claimErr := queries.GetSlugClaim(ctx, claimedSlug); !errors.Is(claimErr, pgx.ErrNoRows) {
		t.Fatalf("GetSlugClaim() after rollback error = %v, want pgx.ErrNoRows", claimErr)
	}
	restored, err := queries.GetSlugTombstoneForUpdate(ctx, claimedSlug)
	if err != nil {
		t.Fatalf("GetSlugTombstoneForUpdate() after rollback error: %v", err)
	}
	if restored.ID != tombstone.ID || !restored.ReleasedAt.Equal(releasedAt) {
		t.Fatalf("restored tombstone = %+v, want ID %s released at %s", restored, tombstone.ID, releasedAt)
	}
	assertPublicProofCount(ctx, t, tx, userID, proofKey, 0)
}

func TestDeleteRollbackRestoresRowTombstoneAndJob(t *testing.T) {
	ctx, _, tx, queries := newPublicStoreTx(t)
	userID := createPublicStoreUser(ctx, t, tx)
	slug := "delete-slug"
	resumeID := createPublicStoreResume(ctx, t, tx, userID, &slug, true, true)
	before, err := queries.GetPublicResumeByOwner(ctx, store.GetPublicResumeByOwnerParams{
		UserID: userID,
		ID:     resumeID,
	})
	if err != nil {
		t.Fatalf("GetPublicResumeByOwner(before) error: %v", err)
	}

	mutationTx, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("begin delete transaction: %v", err)
	}
	mutation := store.New(mutationTx)
	if lockErr := mutation.LockSlugClaim(ctx, slug); lockErr != nil {
		t.Fatalf("LockSlugClaim() error: %v", lockErr)
	}
	if _, insertErr := mutation.InsertSlugTombstone(ctx, store.InsertSlugTombstoneParams{
		Slug:             slug,
		ReleasedByUserID: &userID,
		ReleasedAt:       time.Date(2026, time.July, 2, 0, 0, 0, 0, time.UTC),
	}); insertErr != nil {
		t.Fatalf("InsertSlugTombstone() error: %v", insertErr)
	}
	photoKey := "resumes/" + resumeID.String() + "/photo-0123456789abcdef0123456789abcdef.jpg"
	if _, enqueueErr := mutation.EnqueueMediaDeletionJob(ctx, store.EnqueueMediaDeletionJobParams{
		ResumeID:  resumeID,
		ObjectKey: photoKey,
	}); enqueueErr != nil {
		t.Fatalf("EnqueueMediaDeletionJob() error: %v", enqueueErr)
	}
	if _, lockStateErr := mutation.LockPublicState(ctx); lockStateErr != nil {
		t.Fatalf("LockPublicState() error: %v", lockStateErr)
	}
	if _, advanceErr := mutation.AdvanceDiscoveryGeneration(ctx); advanceErr != nil {
		t.Fatalf("AdvanceDiscoveryGeneration() error: %v", advanceErr)
	}
	deleted, err := mutation.DeleteResumePublicCAS(ctx, store.DeleteResumePublicCASParams{
		ID:               resumeID,
		UserID:           userID,
		ExpectedRevision: 1,
	})
	if err != nil {
		t.Fatalf("DeleteResumePublicCAS() error: %v", err)
	}
	if deleted.ID != resumeID || deleted.Slug == nil || *deleted.Slug != slug {
		t.Fatalf("deleted row = %+v, want resume %s slug %q", deleted, resumeID, slug)
	}
	proofKey := uuid.New()
	insertPublicProof(ctx, t, mutation, userID, proofKey, "delete")
	if rollbackErr := mutationTx.Rollback(ctx); rollbackErr != nil {
		t.Fatalf("rollback delete transaction: %v", rollbackErr)
	}

	after, err := queries.GetPublicResumeByOwner(ctx, store.GetPublicResumeByOwnerParams{
		UserID: userID,
		ID:     resumeID,
	})
	if err != nil {
		t.Fatalf("GetPublicResumeByOwner(after rollback) error: %v", err)
	}
	assertSamePublicResume(t, after, before)
	assertPublicGeneration(ctx, t, queries, 1)
	if _, err := queries.GetSlugTombstoneForUpdate(ctx, slug); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("GetSlugTombstoneForUpdate() after rollback error = %v, want pgx.ErrNoRows", err)
	}
	var jobCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM media_deletion_jobs WHERE object_key = $1`, photoKey).Scan(&jobCount); err != nil {
		t.Fatalf("count media deletion jobs: %v", err)
	}
	if jobCount != 0 {
		t.Fatalf("media deletion jobs after rollback = %d, want 0", jobCount)
	}
	assertPublicProofCount(ctx, t, tx, userID, proofKey, 0)
}

func TestPublicCASRejectsStaleRevision(t *testing.T) {
	ctx, _, tx, queries := newPublicStoreTx(t)
	userID := createPublicStoreUser(ctx, t, tx)
	resumeID := createPublicStoreResume(ctx, t, tx, userID, nil, false, false)
	slug := "stale-slug"
	_, publishErr := queries.PublishResumeCAS(ctx, store.PublishResumeCASParams{
		ID:               resumeID,
		UserID:           userID,
		ExpectedRevision: 2,
		Slug:             &slug,
		Live:             true,
		DownloadEnabled:  true,
		SEOGeoEnabled:    false,
		UpdatedAt:        time.Date(2026, time.July, 4, 0, 0, 0, 0, time.UTC),
	})
	requireNoRows(t, publishErr)
	_, deleteErr := queries.DeleteResumePublicCAS(ctx, store.DeleteResumePublicCASParams{
		ID:               resumeID,
		UserID:           userID,
		ExpectedRevision: 2,
	})
	requireNoRows(t, deleteErr)
	assertPrivateResumeUnchanged(ctx, t, queries, userID, resumeID)
}

func TestSlugCrossRenameLocksDoNotDeadlock(t *testing.T) {
	ctx, pool, firstTx, firstQueries := newPublicStoreTx(t)
	firstSlug := "cross-alpha"
	secondSlug := "cross-zeta"
	if err := firstQueries.LockSlugClaim(ctx, firstSlug); err != nil {
		t.Fatalf("first LockSlugClaim(%q) error: %v", firstSlug, err)
	}
	if err := firstQueries.LockSlugClaim(ctx, secondSlug); err != nil {
		t.Fatalf("first LockSlugClaim(%q) error: %v", secondSlug, err)
	}

	secondTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("second Begin() error: %v", err)
	}
	t.Cleanup(func() { rollbackPublicTestTx(t, secondTx) })
	var secondPID int32
	if err := secondTx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&secondPID); err != nil {
		t.Fatalf("second backend PID: %v", err)
	}
	locked := make(chan error, 1)
	go func() {
		secondQueries := store.New(secondTx)
		if err := secondQueries.LockSlugClaim(ctx, firstSlug); err != nil {
			locked <- err
			return
		}
		locked <- secondQueries.LockSlugClaim(ctx, secondSlug)
	}()
	waitForBlockedBackend(ctx, t, pool, secondPID)
	if err := firstTx.Rollback(ctx); err != nil {
		t.Fatalf("release first rename locks: %v", err)
	}
	select {
	case err := <-locked:
		if err != nil {
			t.Fatalf("second ordered lock sequence error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second ordered lock sequence did not finish after first transaction released")
	}
}

func TestSlugReclaimCollisionHasOneClaim(t *testing.T) {
	ctx, pool, outerTx, _ := newPublicStoreTx(t)
	if err := outerTx.Rollback(ctx); err != nil {
		t.Fatalf("release outer test transaction: %v", err)
	}
	setupTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("setup Begin() error: %v", err)
	}
	firstUser := createPublicStoreUser(ctx, t, setupTx)
	secondUser := createPublicStoreUser(ctx, t, setupTx)
	firstResume := createPublicStoreResume(ctx, t, setupTx, firstUser, nil, false, false)
	secondResume := createPublicStoreResume(ctx, t, setupTx, secondUser, nil, false, false)
	slug := "reclaim-" + uuid.NewString()[:8]
	releasedAt := time.Date(2025, time.January, 1, 0, 0, 0, 0, time.UTC)
	if _, insertErr := store.New(setupTx).InsertSlugTombstone(ctx, store.InsertSlugTombstoneParams{
		Slug:             slug,
		ReleasedByUserID: &firstUser,
		ReleasedAt:       releasedAt,
	}); insertErr != nil {
		t.Fatalf("setup InsertSlugTombstone() error: %v", insertErr)
	}
	if commitErr := setupTx.Commit(ctx); commitErr != nil {
		t.Fatalf("setup Commit() error: %v", commitErr)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, cleanupErr := pool.Exec(cleanupCtx, `DELETE FROM slug_tombstones WHERE slug = $1`, slug); cleanupErr != nil {
			t.Errorf("cleanup slug tombstone: %v", cleanupErr)
		}
		if _, cleanupErr := pool.Exec(cleanupCtx, `DELETE FROM users WHERE id IN ($1, $2)`, firstUser, secondUser); cleanupErr != nil {
			t.Errorf("cleanup users: %v", cleanupErr)
		}
	})

	firstTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("first reclaim Begin() error: %v", err)
	}
	firstQueries := store.New(firstTx)
	if lockErr := firstQueries.LockSlugClaim(ctx, slug); lockErr != nil {
		t.Fatalf("first LockSlugClaim() error: %v", lockErr)
	}
	if _, consumeErr := firstQueries.ConsumeExpiredSlugTombstone(ctx, store.ConsumeExpiredSlugTombstoneParams{
		Slug:       slug,
		ReusableAt: releasedAt.Add(180 * 24 * time.Hour),
	}); consumeErr != nil {
		t.Fatalf("first ConsumeExpiredSlugTombstone() error: %v", consumeErr)
	}
	if _, publishErr := firstQueries.PublishResumeCAS(ctx, store.PublishResumeCASParams{
		ID:               firstResume,
		UserID:           firstUser,
		ExpectedRevision: 1,
		Slug:             &slug,
		Live:             true,
		DownloadEnabled:  true,
		SEOGeoEnabled:    false,
		UpdatedAt:        time.Date(2026, time.July, 5, 0, 0, 0, 0, time.UTC),
	}); publishErr != nil {
		t.Fatalf("first PublishResumeCAS() error: %v", publishErr)
	}

	secondTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("second reclaim Begin() error: %v", err)
	}
	t.Cleanup(func() { rollbackPublicTestTx(t, secondTx) })
	var secondPID int32
	if pidErr := secondTx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&secondPID); pidErr != nil {
		t.Fatalf("second reclaim backend PID: %v", pidErr)
	}
	secondLocked := make(chan error, 1)
	go func() {
		secondLocked <- store.New(secondTx).LockSlugClaim(ctx, slug)
	}()
	waitForBlockedBackend(ctx, t, pool, secondPID)
	if commitErr := firstTx.Commit(ctx); commitErr != nil {
		t.Fatalf("first reclaim Commit() error: %v", commitErr)
	}
	select {
	case lockErr := <-secondLocked:
		if lockErr != nil {
			t.Fatalf("second LockSlugClaim() error: %v", lockErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second slug claimant did not resume")
	}
	secondQueries := store.New(secondTx)
	claimedBy, err := secondQueries.GetSlugClaim(ctx, slug)
	if err != nil {
		t.Fatalf("GetSlugClaim() after serialized reclaim error: %v", err)
	}
	if claimedBy != firstResume {
		t.Fatalf("slug claimed by %s, want first resume %s", claimedBy, firstResume)
	}
	secondRow, err := secondQueries.GetPublicResumeByOwner(ctx, store.GetPublicResumeByOwnerParams{
		UserID: secondUser,
		ID:     secondResume,
	})
	if err != nil {
		t.Fatalf("GetPublicResumeByOwner(second) error: %v", err)
	}
	if secondRow.Slug != nil || secondRow.Revision != 1 {
		t.Fatalf("losing resume = %+v, want unchanged unclaimed revision 1", secondRow)
	}
	if err := secondTx.Commit(ctx); err != nil {
		t.Fatalf("second reclaim Commit() error: %v", err)
	}
}

func waitForBlockedBackend(ctx context.Context, t *testing.T, pool *pgxpool.Pool, pid int32) {
	t.Helper()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := pool.QueryRow(ctx, `SELECT cardinality(pg_blocking_pids($1)) > 0`, pid).Scan(&blocked); err != nil {
			t.Fatalf("probe blocked backend: %v", err)
		}
		if blocked {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("backend %d did not block on the shared advisory lock", pid)
		case <-ticker.C:
		}
	}
}

func rollbackPublicTestTx(t *testing.T, tx pgx.Tx) {
	t.Helper()
	if rollbackErr := tx.Rollback(context.Background()); rollbackErr != nil &&
		!errors.Is(rollbackErr, pgx.ErrTxClosed) {
		t.Errorf("test transaction rollback: %v", rollbackErr)
	}
}

func insertPublicProof(
	ctx context.Context,
	t *testing.T,
	queries *store.Queries,
	userID uuid.UUID,
	key uuid.UUID,
	route string,
) {
	t.Helper()
	if _, err := queries.CreateIdempotencyRecord(ctx, store.CreateIdempotencyRecordParams{
		UserID:          userID,
		Route:           route,
		IdempotencyKey:  key,
		RequestHash:     make([]byte, 32),
		ResponseStatus:  200,
		ResponseBody:    json.RawMessage(`{"ok":true}`),
		ResponseHeaders: json.RawMessage(`{}`),
		ExpiresAt:       time.Date(2026, time.July, 3, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("CreateIdempotencyRecord() error: %v", err)
	}
}

func assertPrivateResumeUnchanged(
	ctx context.Context,
	t *testing.T,
	queries *store.Queries,
	userID uuid.UUID,
	resumeID uuid.UUID,
) {
	t.Helper()
	row, err := queries.GetPublicResumeByOwner(ctx, store.GetPublicResumeByOwnerParams{
		UserID: userID,
		ID:     resumeID,
	})
	if err != nil {
		t.Fatalf("GetPublicResumeByOwner() after rollback error: %v", err)
	}
	if row.Revision != 1 || row.Slug != nil || row.Live || row.SEOGeoEnabled {
		t.Fatalf("resume after rollback = %+v, want original private revision 1", row)
	}
}

func assertPublicGeneration(ctx context.Context, t *testing.T, queries *store.Queries, want int64) {
	t.Helper()
	state, err := queries.GetPublicState(ctx)
	if err != nil {
		t.Fatalf("GetPublicState() error: %v", err)
	}
	if state.DiscoveryGeneration != want {
		t.Fatalf("discovery generation = %d, want %d", state.DiscoveryGeneration, want)
	}
}

func assertPublicProofCount(
	ctx context.Context,
	t *testing.T,
	tx pgx.Tx,
	userID uuid.UUID,
	key uuid.UUID,
	want int,
) {
	t.Helper()
	var count int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM idempotency_records
		WHERE user_id = $1 AND idempotency_key = $2
	`, userID, key).Scan(&count); err != nil {
		t.Fatalf("count idempotency records: %v", err)
	}
	if count != want {
		t.Fatalf("idempotency record count = %d, want %d", count, want)
	}
}

func assertSamePublicResume(t *testing.T, got, want store.Resume) {
	t.Helper()
	if got.ID != want.ID || got.UserID != want.UserID || got.Title != want.Title ||
		!equalOptionalString(got.Slug, want.Slug) || got.Live != want.Live ||
		got.DownloadEnabled != want.DownloadEnabled || got.SEOGeoEnabled != want.SEOGeoEnabled ||
		got.SchemaVersion != want.SchemaVersion || got.Revision != want.Revision ||
		!equalOptionalString(got.Lng, want.Lng) ||
		string(got.PersonalDetails) != string(want.PersonalDetails) ||
		string(got.Content) != string(want.Content) ||
		string(got.Customization) != string(want.Customization) ||
		!got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("resume after rollback = %+v, want %+v", got, want)
	}
}

func equalOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func equalOptionalUUID(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
