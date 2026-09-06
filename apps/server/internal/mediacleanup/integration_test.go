package mediacleanup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

type fakeBackend struct {
	mu          sync.Mutex
	objects     []media.Object
	deleteErrs  map[string][]error
	deleteCalls []string
	active      int
	maxActive   int
	block       <-chan struct{}
	entered     chan<- string
	afterCall   func(string)
}

func (b *fakeBackend) Put(context.Context, string, string, io.Reader, int64) (media.PutOutcome, error) {
	return media.PutUnknown, errors.New("unused")
}

func (b *fakeBackend) Get(context.Context, string) (io.ReadCloser, string, error) {
	return nil, "", errors.New("unused")
}

func (b *fakeBackend) Delete(ctx context.Context, key string) error {
	b.mu.Lock()
	b.deleteCalls = append(b.deleteCalls, key)
	b.active++
	b.maxActive = max(b.maxActive, b.active)
	b.mu.Unlock()
	if b.entered != nil {
		select {
		case b.entered <- key:
		default:
		}
	}
	defer func() {
		b.mu.Lock()
		b.active--
		b.mu.Unlock()
	}()
	if b.block != nil {
		select {
		case <-b.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if b.afterCall != nil {
		b.afterCall(key)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	sequence := b.deleteErrs[key]
	var err error
	if len(sequence) > 0 {
		err = sequence[0]
		b.deleteErrs[key] = sequence[1:]
	}
	if err == nil {
		for index := range b.objects {
			if b.objects[index].Key == key {
				b.objects = append(b.objects[:index], b.objects[index+1:]...)
				break
			}
		}
	}
	return err
}

func (b *fakeBackend) ListPage(ctx context.Context, prefix, cursor string, limit int) ([]media.Object, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	start := 0
	for start < len(b.objects) && b.objects[start].Key <= cursor {
		start++
	}
	end := min(start+limit, len(b.objects))
	page := append([]media.Object(nil), b.objects[start:end]...)
	next := ""
	if end < len(b.objects) && len(page) > 0 {
		next = page[len(page)-1].Key
	}
	return page, next, nil
}

func (b *fakeBackend) calls() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.deleteCalls...)
}

type cleanupHarness struct {
	ctx  context.Context
	pool *store.Pool
	now  time.Time
}

func newCleanupHarness(t *testing.T) *cleanupHarness {
	t.Helper()
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	ctx := context.Background()
	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("store.NewPool(): %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM lifecycle_audit_events WHERE media_job_id IN (SELECT id FROM media_deletion_jobs WHERE object_key LIKE 'resumes/%/photo-8%')`); err != nil {
			t.Errorf("cleanup audit rows: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM media_deletion_jobs WHERE object_key LIKE 'resumes/%/photo-8%'`); err != nil {
			t.Errorf("cleanup media jobs: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, `DELETE FROM users WHERE email LIKE 'media-cleanup-%@example.test'`); err != nil {
			t.Errorf("cleanup users: %v", err)
		}
		if _, err := pool.Exec(cleanupCtx, `UPDATE privacy_sweep_state SET cursor = '' WHERE name = 'media-orphan-sweep'`); err != nil {
			t.Errorf("reset orphan cursor: %v", err)
		}
		pool.Close(cleanupCtx)
	})
	return &cleanupHarness{ctx: ctx, pool: pool, now: time.Date(2001, 9, 6, 12, 0, 0, 0, time.UTC)}
}

func cleanupKey(resumeID uuid.UUID, suffix byte) string {
	return "resumes/" + resumeID.String() + "/photo-8" + string(bytes.Repeat([]byte{suffix}, 31)) + ".jpg"
}

func (h *cleanupHarness) insertJob(t *testing.T, resumeID uuid.UUID, key string, enqueuedAt time.Time) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := h.pool.QueryRow(h.ctx, `
		INSERT INTO media_deletion_jobs (resume_id, object_key, enqueued_at, next_attempt_at)
		VALUES ($1, $2, $3, $3) RETURNING id`, resumeID, key, enqueuedAt).Scan(&id); err != nil {
		t.Fatalf("insert media job: %v", err)
	}
	return id
}

func (h *cleanupHarness) insertLiveResume(t *testing.T, resumeID uuid.UUID, key string) {
	t.Helper()
	userID := uuid.New()
	email := "media-cleanup-" + userID.String() + "@example.test"
	if _, err := h.pool.Exec(h.ctx, `INSERT INTO users (id, email, name) VALUES ($1, $2, 'Cleanup Test')`, userID, email); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := h.pool.Exec(h.ctx, `
		INSERT INTO resumes (id, user_id, title, schema_version, personal_details, content, customization)
		VALUES ($1, $2, 'Cleanup Test', 1, jsonb_build_object('photo', jsonb_build_object('key', $3::text)), '{}'::jsonb, '{}'::jsonb)`, resumeID, userID, key); err != nil {
		t.Fatalf("insert resume: %v", err)
	}
}

func testCleanupWorker(t *testing.T, h *cleanupHarness, backend media.Backend) *Worker {
	t.Helper()
	worker, err := New(Config{
		Pool: h.pool, Media: backend,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:    func() time.Time { return h.now },
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return worker
}

func TestDeleteDueCompletesExactJobsAndKeepsFailuresQueued(t *testing.T) {
	h := newCleanupHarness(t)
	deletedResume, absentResume, failedResume, liveResume := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	deletedKey := cleanupKey(deletedResume, '1')
	absentKey := cleanupKey(absentResume, '2')
	failedKey := cleanupKey(failedResume, '3')
	liveKey := cleanupKey(liveResume, '4')
	for _, item := range []struct {
		resumeID uuid.UUID
		key      string
	}{
		{deletedResume, deletedKey}, {absentResume, absentKey}, {failedResume, failedKey}, {liveResume, liveKey},
	} {
		h.insertJob(t, item.resumeID, item.key, h.now.Add(-overdueAge))
	}
	h.insertLiveResume(t, liveResume, liveKey)
	backendFailure := errors.New("backend detail must not escape")
	backend := &fakeBackend{deleteErrs: map[string][]error{
		absentKey: {media.ErrNotFound},
		failedKey: {backendFailure},
	}}

	result, err := testCleanupWorker(t, h, backend).DeleteDue(h.ctx)
	if err != nil {
		t.Fatalf("DeleteDue(): %v", err)
	}
	if result.Claimed != 4 || result.Succeeded != 2 || result.Deleted != 1 || result.Absent != 1 || result.Live != 1 || result.Failed != 2 || result.Backlog < 2 || result.Overdue != 2 || result.OldestAge != overdueAge {
		t.Fatalf("DeleteDue() result = %+v", result)
	}
	for _, call := range backend.calls() {
		if call == liveKey {
			t.Fatal("live object reached backend Delete")
		}
	}
	failedCalls := 0
	for _, call := range backend.calls() {
		if call == failedKey {
			failedCalls++
		}
	}
	if failedCalls != 1 {
		t.Fatalf("failed due job delete calls = %d, want one attempt in this run", failedCalls)
	}
	var completed, pending, overdueAudit, completedAudit int
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM media_deletion_jobs WHERE object_key = ANY($1::text[]) AND completed_at IS NOT NULL`, []string{deletedKey, absentKey}).Scan(&completed); err != nil {
		t.Fatalf("count completed jobs: %v", err)
	}
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM media_deletion_jobs WHERE object_key = ANY($1::text[]) AND completed_at IS NULL AND lease_id IS NULL AND next_attempt_at = $2`, []string{failedKey, liveKey}, h.now.Add(time.Minute)).Scan(&pending); err != nil {
		t.Fatalf("count pending jobs: %v", err)
	}
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM lifecycle_audit_events WHERE kind='media_deletion_overdue' AND media_job_id IN (SELECT id FROM media_deletion_jobs WHERE object_key = ANY($1::text[]))`, []string{deletedKey, absentKey, failedKey, liveKey}).Scan(&overdueAudit); err != nil {
		t.Fatalf("count overdue audit: %v", err)
	}
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM lifecycle_audit_events WHERE kind='media_deletion_completed' AND media_job_id IN (SELECT id FROM media_deletion_jobs WHERE object_key = ANY($1::text[]))`, []string{deletedKey, absentKey, failedKey, liveKey}).Scan(&completedAudit); err != nil {
		t.Fatalf("count completion audit: %v", err)
	}
	if completed != 2 || pending != 2 || overdueAudit != 4 || completedAudit != 2 {
		t.Fatalf("database state completed=%d pending=%d overdueAudit=%d completedAudit=%d", completed, pending, overdueAudit, completedAudit)
	}
}

func TestReconcileSkipsLiveQueuedAndYoungObjectsAndPersistsFailures(t *testing.T) {
	h := newCleanupHarness(t)
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	keys := []string{
		cleanupKey(ids[0], '5'), cleanupKey(ids[1], '6'), cleanupKey(ids[2], '7'),
		cleanupKey(ids[3], '8'), cleanupKey(ids[4], '9'),
	}
	h.insertLiveResume(t, ids[0], keys[0])
	h.insertJob(t, ids[1], keys[1], h.now.Add(-time.Hour))
	backend := &fakeBackend{
		objects: []media.Object{
			{Key: keys[0], UpdatedAt: h.now.Add(-orphanMinimumAge)},
			{Key: keys[1], UpdatedAt: h.now.Add(-orphanMinimumAge)},
			{Key: keys[2], UpdatedAt: h.now.Add(-orphanMinimumAge)},
			{Key: keys[3], UpdatedAt: h.now.Add(-orphanMinimumAge)},
			{Key: keys[4], UpdatedAt: h.now.Add(-orphanMinimumAge + time.Second)},
		},
		deleteErrs: map[string][]error{keys[3]: {errors.New("one"), errors.New("two"), errors.New("three")}},
	}
	sort.Slice(backend.objects, func(i, j int) bool { return backend.objects[i].Key < backend.objects[j].Key })

	worker := testCleanupWorker(t, h, backend)
	var delays []time.Duration
	worker.wait = func(_ context.Context, delay time.Duration) bool {
		delays = append(delays, delay)
		return true
	}
	result, reconcileErr := worker.Reconcile(h.ctx, false)
	if reconcileErr != nil {
		t.Fatalf("Reconcile(): %v", reconcileErr)
	}
	if result.Scanned != 5 || result.Candidates != 2 || result.Live != 1 || result.Queued != 1 || result.Enqueued != 2 || result.Succeeded != 1 || result.Deleted != 1 || result.Failed != 1 {
		t.Fatalf("Reconcile() result = %+v", result)
	}
	calls := backend.calls()
	called := make(map[string]bool, len(calls))
	for _, key := range calls {
		called[key] = true
	}
	if len(calls) != 4 || called[keys[0]] || called[keys[1]] || called[keys[4]] {
		t.Fatalf("backend delete calls = %v", calls)
	}
	if len(delays) != 2 || delays[0] != time.Second || delays[1] != 2*time.Second {
		t.Fatalf("orphan retry delays = %v, want [1s 2s]", delays)
	}
	var failedPending, completed, completedAudit int
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM media_deletion_jobs WHERE object_key=$1 AND completed_at IS NULL AND lease_id IS NULL`, keys[3]).Scan(&failedPending); err != nil {
		t.Fatalf("count failed orphan job: %v", err)
	}
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM media_deletion_jobs WHERE object_key=$1 AND outcome='deleted'`, keys[2]).Scan(&completed); err != nil {
		t.Fatalf("count completed orphan job: %v", err)
	}
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM lifecycle_audit_events WHERE kind='media_deletion_completed' AND media_job_id IN (SELECT id FROM media_deletion_jobs WHERE object_key=$1)`, keys[2]).Scan(&completedAudit); err != nil {
		t.Fatalf("count orphan completion audit: %v", err)
	}
	if failedPending != 1 || completed != 1 || completedAudit != 1 {
		t.Fatalf("orphan database state failed=%d completed=%d audit=%d", failedPending, completed, completedAudit)
	}
	repeated, repeatedErr := worker.Reconcile(h.ctx, false)
	if repeatedErr != nil || repeated.Deleted != 0 || repeated.Enqueued != 0 {
		t.Fatalf("repeated Reconcile() result=%+v err=%v", repeated, repeatedErr)
	}
}

func TestReconcileDryRunDoesNotMutateAnything(t *testing.T) {
	h := newCleanupHarness(t)
	priorID := uuid.MustParse("018cc251-f400-7000-8000-000000000001")
	resumeID := uuid.MustParse("018cc251-f400-7000-8000-000000000002")
	priorCursor := cleanupKey(priorID, '9')
	key := cleanupKey(resumeID, 'a')
	backend := &fakeBackend{objects: []media.Object{{Key: key, UpdatedAt: h.now.Add(-orphanMinimumAge)}}}
	if _, err := h.pool.Exec(h.ctx, `UPDATE privacy_sweep_state SET cursor=$1 WHERE name='media-orphan-sweep'`, priorCursor); err != nil {
		t.Fatalf("seed cursor: %v", err)
	}

	result, err := testCleanupWorker(t, h, backend).Reconcile(h.ctx, true)
	if err != nil {
		t.Fatalf("Reconcile(dry run): %v", err)
	}
	if result.Scanned != 1 || result.Candidates != 1 || result.Enqueued != 0 || result.Deleted != 0 || result.Failed != 0 || len(backend.calls()) != 0 {
		t.Fatalf("dry-run result=%+v calls=%v", result, backend.calls())
	}
	var jobs int
	var cursor string
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM media_deletion_jobs WHERE object_key=$1`, key).Scan(&jobs); err != nil {
		t.Fatalf("count dry-run jobs: %v", err)
	}
	if err := h.pool.QueryRow(h.ctx, `SELECT cursor FROM privacy_sweep_state WHERE name='media-orphan-sweep'`).Scan(&cursor); err != nil {
		t.Fatalf("read dry-run cursor: %v", err)
	}
	if jobs != 0 || cursor != priorCursor {
		t.Fatalf("dry run mutated jobs=%d cursor=%q", jobs, cursor)
	}
}

func TestReconcileLiveReferenceAfterJobCreationIsRequeuedWithoutIO(t *testing.T) {
	h := newCleanupHarness(t)
	ownerID := uuid.New()
	key := cleanupKey(ownerID, 'b')
	backend := &fakeBackend{objects: []media.Object{{Key: key, UpdatedAt: h.now.Add(-orphanMinimumAge)}}}
	worker := testCleanupWorker(t, h, backend)
	foreignID, userID := uuid.New(), uuid.New()
	email := "media-cleanup-" + userID.String() + "@example.test"
	var hookErr error
	worker.hooks.afterOrphanClaim = func(store.MediaDeletionJob) {
		_, hookErr = h.pool.Exec(h.ctx, `
			WITH inserted_user AS (
				INSERT INTO users (id, email, name) VALUES ($1, $2, 'Cleanup Test') RETURNING id
			)
			INSERT INTO resumes (id, user_id, title, schema_version, personal_details, content, customization)
			SELECT $3, id, 'Cleanup Test', 1, jsonb_build_object('photo', jsonb_build_object('key', $4::text)), '{}'::jsonb, '{}'::jsonb
			FROM inserted_user`, userID, email, foreignID, key)
	}
	result, err := worker.Reconcile(h.ctx, false)
	if hookErr != nil {
		t.Fatalf("install late live reference: %v", hookErr)
	}
	if err != nil || result.Enqueued != 1 || result.Live != 1 || result.Failed != 1 || len(backend.calls()) != 0 {
		t.Fatalf("late-live Reconcile() result=%+v calls=%v err=%v", result, backend.calls(), err)
	}
	var pending int
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM media_deletion_jobs WHERE object_key=$1 AND completed_at IS NULL AND lease_id IS NULL AND next_attempt_at=$2`, key, h.now.Add(time.Minute)).Scan(&pending); err != nil {
		t.Fatalf("read requeued late-live job: %v", err)
	}
	if pending != 1 {
		t.Fatalf("durable requeued late-live jobs = %d, want 1", pending)
	}
}

func TestDeleteDueClaimsOnlyReadySlotsRejectsOverlapAndJoinsCancellation(t *testing.T) {
	h := newCleanupHarness(t)
	keys := make([]string, 8)
	for i := range keys {
		resumeID := uuid.New()
		keys[i] = cleanupKey(resumeID, "01234567"[i])
		h.insertJob(t, resumeID, keys[i], h.now.Add(time.Duration(i)*time.Second-time.Hour))
	}
	block := make(chan struct{})
	entered := make(chan string, 8)
	backend := &fakeBackend{block: block, entered: entered}
	worker := testCleanupWorker(t, h, backend)
	ctx, cancel := context.WithCancel(h.ctx)
	t.Cleanup(cancel)
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := worker.DeleteDue(ctx)
		done <- outcome{result: result, err: err}
	}()
	for i := 0; i < workerCount; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("four deletion workers did not enter backend")
		}
	}
	var leased int
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM media_deletion_jobs WHERE object_key=ANY($1::text[]) AND lease_id IS NOT NULL`, keys).Scan(&leased); err != nil {
		t.Fatalf("count leased jobs: %v", err)
	}
	if leased != workerCount {
		t.Fatalf("leased jobs = %d, want %d ready worker slots", leased, workerCount)
	}
	rows, queryErr := h.pool.Query(h.ctx, `SELECT object_key FROM media_deletion_jobs WHERE object_key=ANY($1::text[]) AND lease_id IS NOT NULL`, keys)
	if queryErr != nil {
		t.Fatalf("read leased keys: %v", queryErr)
	}
	leasedKeys := make(map[string]bool, workerCount)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			t.Fatalf("scan leased key: %v", err)
		}
		leasedKeys[key] = true
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		rows.Close()
		t.Fatalf("read leased keys: %v", rowsErr)
	}
	rows.Close()
	for _, key := range keys[:workerCount] {
		if !leasedKeys[key] {
			t.Fatal("claim did not select the oldest due jobs first")
		}
	}
	overlap, err := testCleanupWorker(t, h, backend).DeleteDue(h.ctx)
	if err != nil || !overlap.Overlap || overlap.Claimed != 0 {
		t.Fatalf("overlapping DeleteDue() result=%+v err=%v", overlap, err)
	}
	cancel()
	select {
	case got := <-done:
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("canceled DeleteDue() error=%v result=%+v", got.err, got.result)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("DeleteDue did not join canceled object work")
	}
	backend.mu.Lock()
	active := backend.active
	maxActive := backend.maxActive
	backend.mu.Unlock()
	if active != 0 {
		t.Fatalf("active backend deletions after return = %d", active)
	}
	if maxActive != workerCount {
		t.Fatalf("maximum concurrent deletions = %d, want %d", maxActive, workerCount)
	}
}

func TestFailedAdvisoryUnlockDestroysLockedConnection(t *testing.T) {
	h := newCleanupHarness(t)
	worker := testCleanupWorker(t, h, &fakeBackend{})
	connection, acquired, acquireErr := worker.acquireRunLock(h.ctx, deletionLock)
	if acquireErr != nil || !acquired {
		t.Fatalf("acquire first lock acquired=%v err=%v", acquired, acquireErr)
	}
	worker.unlock = func(context.Context, *pgxpool.Conn, int32) (bool, error) {
		return false, errors.New("injected unlock failure")
	}
	if err := worker.releaseRunLock(h.ctx, connection, deletionLock); !errors.Is(err, ErrLockRelease) {
		t.Fatalf("releaseRunLock() error=%v, want ErrLockRelease", err)
	}
	worker.unlock = unlockRun

	replacement, acquired, acquireErr := worker.acquireRunLock(h.ctx, deletionLock)
	if acquireErr != nil || !acquired {
		t.Fatalf("lock remained stranded after failed unlock acquired=%v err=%v", acquired, acquireErr)
	}
	if err := worker.releaseRunLock(h.ctx, replacement, deletionLock); err != nil {
		t.Fatalf("release replacement lock: %v", err)
	}

	reportWorker := testCleanupWorker(t, h, &fakeBackend{})
	reportWorker.unlock = func(context.Context, *pgxpool.Conn, int32) (bool, error) {
		return false, errors.New("injected unlock failure")
	}
	result, err := reportWorker.DeleteDue(h.ctx)
	if !errors.Is(err, ErrLockRelease) || result.Failed != 1 {
		t.Fatalf("DeleteDue() hid unlock failure result=%+v err=%v", result, err)
	}
}

func TestDeleteDueLateResultCannotCompleteAnotherLease(t *testing.T) {
	h := newCleanupHarness(t)
	resumeID := uuid.New()
	key := cleanupKey(resumeID, 'c')
	jobID := h.insertJob(t, resumeID, key, h.now.Add(-time.Hour))
	block := make(chan struct{})
	entered := make(chan string, 1)
	backend := &fakeBackend{block: block, entered: entered}
	done := make(chan struct {
		result Result
		err    error
	}, 1)
	go func() {
		result, err := testCleanupWorker(t, h, backend).DeleteDue(h.ctx)
		done <- struct {
			result Result
			err    error
		}{result, err}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("delete did not start")
	}
	replacementLease := uuid.New()
	if _, err := h.pool.Exec(h.ctx, `UPDATE media_deletion_jobs SET lease_id=$2, lease_expires_at=$3 WHERE id=$1`, jobID, replacementLease, h.now.Add(time.Minute)); err != nil {
		t.Fatalf("replace lease: %v", err)
	}
	close(block)
	got := <-done
	if got.err != nil || got.result.Succeeded != 0 || got.result.Failed != 1 {
		t.Fatalf("late DeleteDue() result=%+v err=%v", got.result, got.err)
	}
	var completedAt *time.Time
	var leaseID *uuid.UUID
	if err := h.pool.QueryRow(h.ctx, `SELECT completed_at, lease_id FROM media_deletion_jobs WHERE id=$1`, jobID).Scan(&completedAt, &leaseID); err != nil {
		t.Fatalf("read late job: %v", err)
	}
	if completedAt != nil || leaseID == nil || *leaseID != replacementLease {
		t.Fatalf("late completion changed row completed=%v lease=%v", completedAt, leaseID)
	}
}

func TestDeleteDueReclaimsExpiredLease(t *testing.T) {
	h := newCleanupHarness(t)
	resumeID := uuid.New()
	key := cleanupKey(resumeID, 'a')
	jobID := h.insertJob(t, resumeID, key, h.now.Add(-time.Hour))
	if _, err := h.pool.Exec(h.ctx, `UPDATE media_deletion_jobs SET attempt_count=7, lease_id=$2, lease_expires_at=$3 WHERE id=$1`, jobID, uuid.New(), h.now); err != nil {
		t.Fatalf("seed expired lease: %v", err)
	}
	backend := &fakeBackend{deleteErrs: map[string][]error{key: {media.ErrNotFound}}}
	result, err := testCleanupWorker(t, h, backend).DeleteDue(h.ctx)
	if err != nil || result.Claimed != 1 || result.Absent != 1 {
		t.Fatalf("expired-lease DeleteDue() result=%+v err=%v", result, err)
	}
	var attempts int32
	if err := h.pool.QueryRow(h.ctx, `SELECT attempt_count FROM media_deletion_jobs WHERE id=$1`, jobID).Scan(&attempts); err != nil {
		t.Fatalf("read reclaimed job: %v", err)
	}
	if attempts != 8 {
		t.Fatalf("reclaimed attempt_count=%d, want 8", attempts)
	}
}

func TestDeleteDueAuditsOverdueWorkEvenWhenRetryIsNotDue(t *testing.T) {
	h := newCleanupHarness(t)
	resumeID := uuid.New()
	key := cleanupKey(resumeID, '4')
	jobID := h.insertJob(t, resumeID, key, h.now.Add(-overdueAge))
	if _, err := h.pool.Exec(h.ctx, `UPDATE media_deletion_jobs SET next_attempt_at=$2 WHERE id=$1`, jobID, h.now.Add(time.Hour)); err != nil {
		t.Fatalf("defer overdue retry: %v", err)
	}
	backend := &fakeBackend{}
	worker := testCleanupWorker(t, h, backend)
	for run := 1; run <= 2; run++ {
		result, err := worker.DeleteDue(h.ctx)
		if err != nil || result.Claimed != 0 || result.Overdue < 1 || len(backend.calls()) != 0 {
			t.Fatalf("overdue run %d result=%+v calls=%v err=%v", run, result, backend.calls(), err)
		}
	}
	var overdueAt *time.Time
	var audits int
	if err := h.pool.QueryRow(h.ctx, `SELECT overdue_at FROM media_deletion_jobs WHERE id=$1`, jobID).Scan(&overdueAt); err != nil {
		t.Fatalf("read overdue marker: %v", err)
	}
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM lifecycle_audit_events WHERE media_job_id=$1 AND kind='media_deletion_overdue'`, jobID).Scan(&audits); err != nil {
		t.Fatalf("count overdue audits: %v", err)
	}
	if overdueAt == nil || !overdueAt.Equal(h.now) || audits != 1 {
		t.Fatalf("overdue marker=%v audits=%d, want exact time and once-only audit", overdueAt, audits)
	}
}

func TestDeleteDueStopsAtRunCeiling(t *testing.T) {
	h := newCleanupHarness(t)
	resumeIDs := make([]uuid.UUID, deletionRunLimit+1)
	keys := make([]string, deletionRunLimit+1)
	for i := range keys {
		resumeIDs[i] = uuid.MustParse(fmt.Sprintf("028cc251-f400-7000-8000-%012x", i+1))
		keys[i] = cleanupKey(resumeIDs[i], '9')
	}
	if _, err := h.pool.Exec(h.ctx, `
		INSERT INTO media_deletion_jobs (resume_id, object_key, enqueued_at, next_attempt_at)
		SELECT input.resume_id, input.object_key, $3, $3
		FROM unnest($1::uuid[], $2::text[]) AS input(resume_id, object_key)`, resumeIDs, keys, h.now.Add(-time.Hour)); err != nil {
		t.Fatalf("bulk insert media jobs: %v", err)
	}
	result, err := testCleanupWorker(t, h, &fakeBackend{}).DeleteDue(h.ctx)
	if err != nil || result.Claimed != deletionRunLimit || result.Succeeded != deletionRunLimit || result.Backlog < 1 {
		t.Fatalf("run-ceiling DeleteDue() result=%+v err=%v", result, err)
	}
	var completed, pending int
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FILTER (WHERE completed_at IS NOT NULL), count(*) FILTER (WHERE completed_at IS NULL) FROM media_deletion_jobs WHERE object_key=ANY($1::text[])`, keys).Scan(&completed, &pending); err != nil {
		t.Fatalf("read run-ceiling jobs: %v", err)
	}
	if completed != deletionRunLimit || pending != 1 {
		t.Fatalf("run-ceiling database state completed=%d pending=%d", completed, pending)
	}
}

func TestDeleteDueAuditFailureRollsBackCompletionAndRecovers(t *testing.T) {
	h := newCleanupHarness(t)
	resumeID := uuid.New()
	key := cleanupKey(resumeID, 'd')
	jobID := h.insertJob(t, resumeID, key, h.now.Add(-time.Hour))
	functionName := "fail_media_audit_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	triggerName := functionName + "_trigger"
	ddl := fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $fn$
		BEGIN
			IF NEW.media_job_id = '%s'::uuid AND NEW.kind = 'media_deletion_completed' THEN
				RAISE EXCEPTION 'injected audit failure';
			END IF;
			RETURN NEW;
		END
		$fn$;
		CREATE TRIGGER %s BEFORE INSERT ON lifecycle_audit_events
		FOR EACH ROW EXECUTE FUNCTION %s()`, functionName, jobID, triggerName, functionName)
	if _, err := h.pool.Exec(h.ctx, ddl); err != nil {
		t.Fatalf("install audit failure trigger: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, cleanupErr := h.pool.Exec(cleanupCtx, fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON lifecycle_audit_events; DROP FUNCTION IF EXISTS %s()`, triggerName, functionName)); cleanupErr != nil {
			t.Errorf("cleanup audit failure trigger: %v", cleanupErr)
		}
	})
	backend := &fakeBackend{deleteErrs: map[string][]error{key: {nil, media.ErrNotFound}}}
	first, firstErr := testCleanupWorker(t, h, backend).DeleteDue(h.ctx)
	if firstErr != nil || first.Failed != 1 || first.Succeeded != 0 {
		t.Fatalf("first DeleteDue() result=%+v err=%v", first, firstErr)
	}
	var completedAt *time.Time
	if err := h.pool.QueryRow(h.ctx, `SELECT completed_at FROM media_deletion_jobs WHERE id=$1`, jobID).Scan(&completedAt); err != nil {
		t.Fatalf("read rolled-back completion: %v", err)
	}
	if completedAt != nil {
		t.Fatal("completion survived failed atomic audit insert")
	}
	if _, err := h.pool.Exec(h.ctx, fmt.Sprintf(`DROP TRIGGER %s ON lifecycle_audit_events; DROP FUNCTION %s()`, triggerName, functionName)); err != nil {
		t.Fatalf("remove audit failure trigger: %v", err)
	}
	h.now = h.now.Add(leaseDuration + time.Second)
	second, secondErr := testCleanupWorker(t, h, backend).DeleteDue(h.ctx)
	if secondErr != nil || second.Absent != 1 || second.Succeeded != 1 {
		t.Fatalf("recovery DeleteDue() result=%+v err=%v", second, secondErr)
	}
}

func TestDeleteDueDoesNotReclaimAbandonedLeaseWithinSameRun(t *testing.T) {
	h := newCleanupHarness(t)
	clock := testutil.NewClock(h.now)
	resumeID := uuid.New()
	key := cleanupKey(resumeID, '6')
	jobID := h.insertJob(t, resumeID, key, h.now.Add(-time.Hour))
	functionName := "fail_media_completion_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	triggerName := functionName + "_trigger"
	ddl := fmt.Sprintf(`
		CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $fn$
		BEGIN
			IF NEW.media_job_id = '%s'::uuid AND NEW.kind = 'media_deletion_completed' THEN
				RAISE EXCEPTION 'injected completion audit failure';
			END IF;
			RETURN NEW;
		END
		$fn$;
		CREATE TRIGGER %s BEFORE INSERT ON lifecycle_audit_events
		FOR EACH ROW EXECUTE FUNCTION %s()`, functionName, jobID, triggerName, functionName)
	if _, err := h.pool.Exec(h.ctx, ddl); err != nil {
		t.Fatalf("install completion failure trigger: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, cleanupErr := h.pool.Exec(cleanupCtx, fmt.Sprintf(`DROP TRIGGER IF EXISTS %s ON lifecycle_audit_events; DROP FUNCTION IF EXISTS %s()`, triggerName, functionName)); cleanupErr != nil {
			t.Errorf("cleanup completion failure trigger: %v", cleanupErr)
		}
	})
	var advance sync.Once
	backend := &fakeBackend{
		deleteErrs: map[string][]error{key: {nil, media.ErrNotFound}},
		afterCall: func(string) {
			advance.Do(func() { clock.Advance(leaseDuration + time.Second) })
		},
	}
	worker, newErr := New(Config{
		Pool: h.pool, Media: backend,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: clock.Now,
	})
	if newErr != nil {
		t.Fatalf("New(): %v", newErr)
	}
	first, firstErr := worker.DeleteDue(h.ctx)
	if firstErr != nil || first.Claimed != 1 || len(backend.calls()) != 1 {
		t.Fatalf("same-run recovery result=%+v calls=%d err=%v", first, len(backend.calls()), firstErr)
	}
	if _, err := h.pool.Exec(h.ctx, fmt.Sprintf(`DROP TRIGGER %s ON lifecycle_audit_events; DROP FUNCTION %s()`, triggerName, functionName)); err != nil {
		t.Fatalf("remove completion failure trigger: %v", err)
	}
	clock.Advance(leaseDuration + time.Second)
	second, secondErr := worker.DeleteDue(h.ctx)
	if secondErr != nil || second.Claimed != 1 || second.Absent != 1 {
		t.Fatalf("next-run recovery result=%+v err=%v", second, secondErr)
	}
}

func TestDeleteDueBlocksForeignLiveReference(t *testing.T) {
	h := newCleanupHarness(t)
	ownerID := uuid.New()
	foreignID := uuid.New()
	key := cleanupKey(ownerID, 'e')
	h.insertJob(t, ownerID, key, h.now.Add(-time.Hour))
	h.insertLiveResume(t, foreignID, key)
	backend := &fakeBackend{}
	result, err := testCleanupWorker(t, h, backend).DeleteDue(h.ctx)
	if err != nil || result.Live != 1 || result.Failed != 1 || len(backend.calls()) != 0 {
		t.Fatalf("foreign-reference DeleteDue() result=%+v calls=%v err=%v", result, backend.calls(), err)
	}
}

func TestDeleteDueDiagnosticsExcludeKeyAndBackendDetail(t *testing.T) {
	h := newCleanupHarness(t)
	resumeID := uuid.New()
	key := cleanupKey(resumeID, '5')
	h.insertJob(t, resumeID, key, h.now.Add(-time.Hour))
	backendDetail := "backend-response-private-detail"
	backend := &fakeBackend{deleteErrs: map[string][]error{key: {errors.New(backendDetail)}}}
	var logs bytes.Buffer
	worker, err := New(Config{
		Pool: h.pool, Media: backend,
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
		Now:    func() time.Time { return h.now },
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	result, err := worker.DeleteDue(h.ctx)
	if err != nil || result.Failed != 1 {
		t.Fatalf("DeleteDue() result=%+v err=%v", result, err)
	}
	output := logs.String()
	if strings.Contains(output, key) || strings.Contains(output, backendDetail) {
		t.Fatalf("diagnostic exposed private key or backend detail: %q", output)
	}
}

func TestReconcileRunCeilingPersistsAndResetsCursor(t *testing.T) {
	h := newCleanupHarness(t)
	objects := make([]media.Object, orphanRunLimit+1)
	for i := range objects {
		resumeID := uuid.MustParse(fmt.Sprintf("018cc251-f400-7000-8000-%012x", i+1))
		objects[i] = media.Object{Key: cleanupKey(resumeID, 'f'), UpdatedAt: h.now}
	}
	backend := &fakeBackend{objects: objects}
	worker := testCleanupWorker(t, h, backend)
	first, firstErr := worker.Reconcile(h.ctx, false)
	if firstErr != nil || first.Scanned != orphanRunLimit || first.Backlog != 1 {
		t.Fatalf("first Reconcile() result=%+v err=%v", first, firstErr)
	}
	var cursor string
	if err := h.pool.QueryRow(h.ctx, `SELECT cursor FROM privacy_sweep_state WHERE name='media-orphan-sweep'`).Scan(&cursor); err != nil {
		t.Fatalf("read saved cursor: %v", err)
	}
	if cursor != objects[orphanRunLimit-1].Key {
		t.Fatalf("saved cursor is not the run-ceiling object")
	}
	second, secondErr := worker.Reconcile(h.ctx, false)
	if secondErr != nil || second.Scanned != 1 || second.Backlog != 0 {
		t.Fatalf("second Reconcile() result=%+v err=%v", second, secondErr)
	}
	if err := h.pool.QueryRow(h.ctx, `SELECT cursor FROM privacy_sweep_state WHERE name='media-orphan-sweep'`).Scan(&cursor); err != nil {
		t.Fatalf("read reset cursor: %v", err)
	}
	if cursor != "" {
		t.Fatalf("cursor after exhaustion = %q, want reset", cursor)
	}
}

func TestReconcileRejectsInvalidPageBeforeMutation(t *testing.T) {
	h := newCleanupHarness(t)
	firstID, secondID := uuid.New(), uuid.New()
	firstKey, secondKey := cleanupKey(firstID, '1'), cleanupKey(secondID, '2')
	objects := []media.Object{
		{Key: firstKey, UpdatedAt: h.now.Add(-orphanMinimumAge)},
		{Key: secondKey, UpdatedAt: h.now.Add(-orphanMinimumAge)},
	}
	if objects[0].Key < objects[1].Key {
		objects[0], objects[1] = objects[1], objects[0]
	}
	backend := &fakeBackend{objects: objects}
	result, err := testCleanupWorker(t, h, backend).Reconcile(h.ctx, false)
	if !errors.Is(err, ErrBackend) || result.Enqueued != 0 || len(backend.calls()) != 0 {
		t.Fatalf("invalid-page Reconcile() result=%+v calls=%v err=%v", result, backend.calls(), err)
	}
	var jobs int
	if err := h.pool.QueryRow(h.ctx, `SELECT count(*) FROM media_deletion_jobs WHERE object_key=ANY($1::text[])`, []string{firstKey, secondKey}).Scan(&jobs); err != nil {
		t.Fatalf("count invalid-page jobs: %v", err)
	}
	if jobs != 0 {
		t.Fatalf("invalid listing created %d jobs", jobs)
	}
}

func TestReconcileRejectsInvalidStoredCursorBeforeBackendIO(t *testing.T) {
	h := newCleanupHarness(t)
	if _, err := h.pool.Exec(h.ctx, `UPDATE privacy_sweep_state SET cursor='../invalid' WHERE name='media-orphan-sweep'`); err != nil {
		t.Fatalf("seed invalid cursor: %v", err)
	}
	backend := &fakeBackend{}
	result, err := testCleanupWorker(t, h, backend).Reconcile(h.ctx, false)
	if !errors.Is(err, ErrBackend) || result.Scanned != 0 || len(backend.calls()) != 0 {
		t.Fatalf("invalid-cursor Reconcile() result=%+v calls=%v err=%v", result, backend.calls(), err)
	}
	var cursor string
	if err := h.pool.QueryRow(h.ctx, `SELECT cursor FROM privacy_sweep_state WHERE name='media-orphan-sweep'`).Scan(&cursor); err != nil {
		t.Fatalf("read rejected cursor: %v", err)
	}
	if cursor != "../invalid" {
		t.Fatalf("invalid cursor mutated to %q", cursor)
	}
}

func TestDeleteDueRemovesFilesystemObject(t *testing.T) {
	h := newCleanupHarness(t)
	backend, err := media.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("media.NewFS(): %v", err)
	}
	resumeID := uuid.New()
	key := cleanupKey(resumeID, '3')
	body := []byte("private photo")
	outcome, err := backend.Put(h.ctx, key, "image/jpeg", bytes.NewReader(body), int64(len(body)))
	if err != nil || outcome != media.PutCreated {
		t.Fatalf("filesystem Put() outcome=%v err=%v", outcome, err)
	}
	h.insertJob(t, resumeID, key, h.now.Add(-time.Hour))
	result, err := testCleanupWorker(t, h, backend).DeleteDue(h.ctx)
	if err != nil || result.Deleted != 1 || result.Succeeded != 1 {
		t.Fatalf("filesystem DeleteDue() result=%+v err=%v", result, err)
	}
	if _, _, err := backend.Get(h.ctx, key); !errors.Is(err, media.ErrNotFound) {
		t.Fatalf("filesystem Get() after cleanup error=%v, want ErrNotFound", err)
	}
}
