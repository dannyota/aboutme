// idempotency_test.go exercises resume.IdempotencyStore against a live
// Postgres database: first run, replay, rejected key reuse, mutation rollback,
// expiry as absence, deterministic same-key contention around the real CAS
// core, and composition with resume.Store's cap-checked createTx across multiple
// keys, including cap rejection surfacing from inside mutate. Every
// DB-backed test here goes through the same
// internal/testutil helper internal/auth, internal/user, internal/store,
// and resume's own store_test.go use, so it never depends on another
// package's test binary having applied migrations first.
package resume_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// idempotencyTestRoute is a placeholder route string. IdempotencyStore treats
// it as an opaque part of the unique key, so any stable string exercises the
// same logic.
const idempotencyTestRoute = "PUT /api/v1/resumes/{id}"

// newIntegrationIdempotencyStore returns a resume.IdempotencyStore driven by
// now instead of the real wall clock (so expiry is proven by advancing an
// injected clock, never a real sleep), plus the resume.Store sharing the
// same connection pool. Mutation closures can call
// (*resume.Store).CreateTxForTest directly, proving
// Execute composes with the real cap-checked create core rather than
// reimplementing it. Mirrors store_test.go's own newIntegrationStore, with
// an injectable clock added.
func newIntegrationIdempotencyStore(t *testing.T, now func() time.Time) (*resume.IdempotencyStore, *resume.Store, *store.Queries, *store.Pool, context.Context) {
	t.Helper()
	dsn := testutil.RequireMigratedTestDatabaseURL(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })

	idem := resume.NewIdempotencyStoreForTest(pool, now)
	rs := resume.NewStore(pool, docmigrate.NewIdentityProjector())
	return idem, rs, store.New(pool), pool, ctx
}

// assertJSONEqual compares got and want as JSON VALUES, not bytes: jsonb
// columns re-serialize on write (e.g. Postgres inserts a space after ":"
// and ","), so anything read back out of idempotency_records.response_body
// is never guaranteed byte-identical to the literal a test passed in,
// even though it is the exact same JSON value. Two reads of the SAME
// already-stored row, in contrast, ARE byte-identical -- callers proving
// that should keep using bytes.Equal against another DB read, not this.
func assertJSONEqual(t *testing.T, got, want json.RawMessage) {
	t.Helper()
	var gotVal, wantVal any
	if err := json.Unmarshal(got, &gotVal); err != nil {
		t.Fatalf("unmarshal got %s: %v", got, err)
	}
	if err := json.Unmarshal(want, &wantVal); err != nil {
		t.Fatalf("unmarshal want %s: %v", want, err)
	}
	if !reflect.DeepEqual(gotVal, wantVal) {
		t.Errorf("JSON mismatch: got %s, want %s", got, want)
	}
}

func createIdempotencyRecord(ctx context.Context, t *testing.T, q *store.Queries,
	userID uuid.UUID, route string, key uuid.UUID, hash [32]byte, expiresAt time.Time,
) {
	t.Helper()
	created, err := q.CreateIdempotencyRecord(ctx, store.CreateIdempotencyRecordParams{
		UserID:          userID,
		Route:           route,
		IdempotencyKey:  key,
		RequestHash:     hash[:],
		ResponseStatus:  200,
		ResponseBody:    json.RawMessage(`{"seeded":true}`),
		ResponseHeaders: json.RawMessage(`{}`),
		ExpiresAt:       expiresAt,
	})
	if err != nil {
		t.Fatalf("CreateIdempotencyRecord() error = %v", err)
	}
	if _, err := q.GetOrCreateIdempotencyUsageForUpdate(ctx, userID); err != nil {
		t.Fatalf("GetOrCreateIdempotencyUsageForUpdate() error = %v", err)
	}
	if _, err := q.TryReserveIdempotencyUsage(ctx, store.TryReserveIdempotencyUsageParams{
		UserID:      userID,
		RecordBytes: int64(created.StoredBytes),
		MaxRecords:  50_000,
		MaxBytes:    1 << 30,
	}); err != nil {
		t.Fatalf("TryReserveIdempotencyUsage() error = %v", err)
	}
}

func assertIdempotencyRecordAbsent(ctx context.Context, t *testing.T, q *store.Queries,
	userID uuid.UUID, route string, key uuid.UUID,
) {
	t.Helper()
	if _, err := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
		UserID: userID, Route: route, IdempotencyKey: key,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("GetIdempotencyRecord(%q, %v) error = %v, want pgx.ErrNoRows", route, key, err)
	}
}

type idempotencyExecuteResult struct {
	resp     resume.StoredResponse
	replayed bool
	err      error
}

type queryRowFailureTx struct {
	pgx.Tx
	queryFragment string
	err           error
}

func (tx *queryRowFailureTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	if strings.Contains(query, tx.queryFragment) {
		return queryRowFailure{err: tx.err}
	}
	return tx.Tx.QueryRow(ctx, query, args...)
}

type queryRowFailure struct {
	err error
}

func (row queryRowFailure) Scan(...any) error { return row.err }

type deadlineRecordingTx struct {
	pgx.Tx
	settings []string
}

func (tx *deadlineRecordingTx) Exec(ctx context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if strings.Contains(query, "idle_in_transaction_session_timeout") {
		for _, arg := range args {
			tx.settings = append(tx.settings, fmt.Sprint(arg))
		}
	}
	return tx.Tx.Exec(ctx, query, args...)
}

type deadlineBlockingTx struct {
	pgx.Tx
	blocked atomic.Bool
}

func (tx *deadlineBlockingTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	if strings.Contains(query, "-- name: UpdateResumeDocumentCAS") {
		tx.blocked.Store(true)
		return tx.Tx.QueryRow(ctx, "SELECT pg_sleep(10)")
	}
	return tx.Tx.QueryRow(ctx, query, args...)
}

// userRowLockedForResumeWrite reports whether another transaction already
// holds the users-row lock that serializes resume/idempotency writes. NOWAIT
// makes the observation deterministic: the probe either acquires and
// immediately releases the row lock, or PostgreSQL returns lock_not_available;
// it never waits for a scheduling-dependent interval.
func userRowLockedForResumeWrite(ctx context.Context, pool *store.Pool, userID uuid.UUID) (bool, error) {
	var got uuid.UUID
	err := pool.QueryRow(ctx,
		"SELECT id FROM users WHERE id = $1 FOR UPDATE NOWAIT", userID,
	).Scan(&got)
	if err == nil {
		if got != userID {
			return false, fmt.Errorf("owner-row lock probe returned id %v, want %v", got, userID)
		}
		return false, nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
		return true, nil
	}
	return false, fmt.Errorf("owner-row lock probe: %w", err)
}

// --- Sequential semantics ---

func TestIdempotencyStore_Execute_BindsPostgresTimeoutsToContextDeadline(t *testing.T) {
	_, _, q, pool, ctx := newIntegrationIdempotencyStore(t, time.Now)
	userID := createTestUser(t, q)
	var begins int
	var mutationTx *deadlineRecordingTx
	idem := resume.NewIdempotencyStoreWithHooksForTest(pool, time.Now,
		func(ctx context.Context) (pgx.Tx, error) {
			begins++
			tx, err := pool.Begin(ctx)
			if err != nil {
				return nil, err
			}
			if begins == 2 {
				mutationTx = &deadlineRecordingTx{Tx: tx}
				return mutationTx, nil
			}
			return tx, nil
		}, nil)
	deadlineContext, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result, err := idem.Execute(deadlineContext, userID, idempotencyTestRoute, uuid.New(),
		sha256.Sum256([]byte("deadline-bound transaction")),
		func(*store.Queries) (resume.StoredResponse, error) {
			return resume.StoredResponse{Status: http.StatusNoContent}, nil
		})
	if err != nil || result.Outcome != resume.CommitCommitted {
		t.Fatalf("Execute() = (%+v, %v), want committed", result, err)
	}
	if mutationTx == nil || len(mutationTx.settings) != 2 || mutationTx.settings[0] != mutationTx.settings[1] {
		t.Fatalf("transaction deadline settings = %#v, want equal statement and idle timeouts", mutationTx)
	}
	setting, parseErr := time.ParseDuration(mutationTx.settings[0])
	if parseErr != nil || setting <= 0 || setting > 10*time.Second {
		t.Fatalf("transaction timeout = %q (%v), want duration in (0,10s]", mutationTx.settings[0], parseErr)
	}
}

func TestIdempotencyStore_Execute_InFlightDeadlineRollsBackAndCannotCommitLate(t *testing.T) {
	_, rs, q, pool, ctx := newIntegrationIdempotencyStore(t, time.Now)
	userID := createTestUser(t, q)
	key := uuid.New()
	doc := validDocForTest(t)
	var begins int
	var mutationTx *deadlineBlockingTx
	idem := resume.NewIdempotencyStoreWithHooksForTest(pool, time.Now,
		func(ctx context.Context) (pgx.Tx, error) {
			begins++
			tx, err := pool.Begin(ctx)
			if err != nil {
				return nil, err
			}
			if begins == 2 {
				mutationTx = &deadlineBlockingTx{Tx: tx}
				return mutationTx, nil
			}
			return tx, nil
		}, nil)

	deadlineContext, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	var writtenID uuid.UUID
	result, err := idem.Execute(deadlineContext, userID, "POST candidate deadline", key,
		sha256.Sum256([]byte("candidate whose transaction crosses its deadline")),
		func(qtx *store.Queries) (resume.StoredResponse, error) {
			created, createErr := rs.CreateTxForTest(deadlineContext, qtx, userID, "must roll back", doc)
			if createErr != nil {
				return resume.StoredResponse{}, createErr
			}
			writtenID = created.ID
			changed := created.Doc
			changed.Customization.Font.BaseSizePx++
			_, saveErr := rs.SaveDocumentTxForTest(
				deadlineContext, qtx, userID, created.ID, changed, created.Revision,
			)
			return resume.StoredResponse{}, saveErr
		})
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Execute() error = %v, want deadline failure", err)
	}
	if mutationTx == nil || !mutationTx.blocked.Load() {
		t.Fatal("mutation did not reach the in-flight PostgreSQL statement")
	}
	if result.Outcome != resume.CommitDefinitelyRolledBack || result.Replayed {
		t.Fatalf("Execute() result = %+v, want definite rollback without replay", result)
	}
	if writtenID == uuid.Nil {
		t.Fatal("mutation did not write before crossing the deadline")
	}
	if _, getErr := rs.Get(ctx, userID, writtenID); !errors.Is(getErr, resume.ErrNotFound) {
		t.Fatalf("Get(writtenID) after deadline = %v, want no late committed reference", getErr)
	}
	assertIdempotencyRecordAbsent(ctx, t, q, userID, "POST candidate deadline", key)
}

func TestIdempotencyStore_Execute_FirstRun_RunsMutateAndStores(t *testing.T) {
	t.Parallel()
	idem, _, q, _, ctx := newIntegrationIdempotencyStore(t, testutil.NewClockAtEpoch().Now)
	userID := createTestUser(t, q)

	key := uuid.New()
	hash := sha256.Sum256([]byte("request body"))
	wantResp := resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"ok":true}`)}

	calls := 0
	mutate := func(qtx *store.Queries) (resume.StoredResponse, error) {
		calls++
		return wantResp, nil
	}

	got, replayed, err := idem.ExecuteForTest(ctx, userID, idempotencyTestRoute, key, hash, mutate)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if replayed {
		t.Errorf("Execute() replayed = true, want false (first use of a fresh key)")
	}
	if got.Status != wantResp.Status {
		t.Errorf("Execute() = %+v, want %+v", got, wantResp)
	}
	assertJSONEqual(t, got.Body, wantResp.Body)
	if calls != 1 {
		t.Errorf("mutate called %d times, want exactly 1", calls)
	}

	row, err := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
		UserID: userID, Route: idempotencyTestRoute, IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("GetIdempotencyRecord() error = %v", err)
	}
	if row.ResponseStatus != int32(wantResp.Status) {
		t.Errorf("stored record ResponseStatus = %d, want %d", row.ResponseStatus, wantResp.Status)
	}
	assertJSONEqual(t, row.ResponseBody, wantResp.Body)
	if !bytes.Equal(got.Body, row.ResponseBody) {
		t.Errorf("first response body = %s, want normalized stored bytes %s", got.Body, row.ResponseBody)
	}
	if !bytes.Equal(row.RequestHash, hash[:]) {
		t.Errorf("stored RequestHash = %x, want %x", row.RequestHash, hash)
	}
}

func TestIdempotencyStore_Execute_Replay_SameKeySameHash_SkipsMutate(t *testing.T) {
	t.Parallel()
	idem, _, q, _, ctx := newIntegrationIdempotencyStore(t, testutil.NewClockAtEpoch().Now)
	userID := createTestUser(t, q)

	key := uuid.New()
	hash := sha256.Sum256([]byte("request body"))
	firstResp := resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"n":1}`)}

	calls := 0
	mutate := func(qtx *store.Queries) (resume.StoredResponse, error) {
		calls++
		return firstResp, nil
	}

	first, _, err := idem.ExecuteForTest(ctx, userID, idempotencyTestRoute, key, hash, mutate)
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	firstRow, err := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
		UserID: userID, Route: idempotencyTestRoute, IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("GetIdempotencyRecord() after first call error = %v", err)
	}
	if first.Status != int(firstRow.ResponseStatus) || !bytes.Equal(first.Body, firstRow.ResponseBody) {
		t.Errorf("first Execute() = %+v, want normalized stored response {Status:%d Body:%s}", first, firstRow.ResponseStatus, firstRow.ResponseBody)
	}
	assertJSONEqual(t, first.Body, firstResp.Body)

	second, replayed, err := idem.ExecuteForTest(ctx, userID, idempotencyTestRoute, key, hash, mutate)
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	if !replayed {
		t.Errorf("second Execute() replayed = false, want true")
	}
	// First and replay both use PostgreSQL's normalized stored bytes.
	if second.Status != int(firstRow.ResponseStatus) || !bytes.Equal(second.Body, firstRow.ResponseBody) {
		t.Errorf("second Execute() = %+v, want the STORED response {Status:%d Body:%s}", second, firstRow.ResponseStatus, firstRow.ResponseBody)
	}
	if calls != 1 {
		t.Errorf("mutate called %d times across two Execute() calls, want exactly 1 (replay must not invoke it)", calls)
	}

	secondRow, err := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
		UserID: userID, Route: idempotencyTestRoute, IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("GetIdempotencyRecord() after second call error = %v", err)
	}
	if secondRow.ID != firstRow.ID || !secondRow.CreatedAt.Equal(firstRow.CreatedAt) {
		t.Errorf("record after replay = %+v, want the SAME row as after the first call %+v (no new row)", secondRow, firstRow)
	}
}

func TestIdempotencyStore_Execute_DifferentHash_RejectsReuse(t *testing.T) {
	t.Parallel()
	idem, _, q, _, ctx := newIntegrationIdempotencyStore(t, testutil.NewClockAtEpoch().Now)
	userID := createTestUser(t, q)

	key := uuid.New()
	firstHash := sha256.Sum256([]byte("original body"))
	otherHash := sha256.Sum256([]byte("a different body"))
	firstResp := resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"n":1}`)}

	if _, _, err := idem.ExecuteForTest(ctx, userID, idempotencyTestRoute, key, firstHash,
		func(qtx *store.Queries) (resume.StoredResponse, error) { return firstResp, nil },
	); err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	firstRow, err := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
		UserID: userID, Route: idempotencyTestRoute, IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("GetIdempotencyRecord() after first call error = %v", err)
	}

	calls := 0
	_, replayed, err := idem.ExecuteForTest(ctx, userID, idempotencyTestRoute, key, otherHash,
		func(qtx *store.Queries) (resume.StoredResponse, error) {
			calls++
			return resume.StoredResponse{Status: 999, Body: json.RawMessage(`{"should":"never persist"}`)}, nil
		},
	)
	if !errors.Is(err, resume.ErrIdempotencyKeyReuse) {
		t.Fatalf("Execute() with reused key/different hash error = %v, want resume.ErrIdempotencyKeyReuse", err)
	}
	if replayed {
		t.Errorf("Execute() replayed = true, want false")
	}
	if calls != 0 {
		t.Errorf("mutate called %d times, want 0 (a hash mismatch must not invoke it)", calls)
	}

	row, err := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
		UserID: userID, Route: idempotencyTestRoute, IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("GetIdempotencyRecord() after rejected reuse error = %v", err)
	}
	if row.ID != firstRow.ID || !row.CreatedAt.Equal(firstRow.CreatedAt) || !bytes.Equal(row.ResponseBody, firstRow.ResponseBody) {
		t.Errorf("record after rejected reuse = %+v, want the original untouched row %+v", row, firstRow)
	}
}

func TestIdempotencyStore_Execute_DifferentHash_ReapPersistsOnRejectedReuse(t *testing.T) {
	t.Parallel()
	clock := testutil.NewClockAtEpoch()
	idem, _, q, _, ctx := newIntegrationIdempotencyStore(t, clock.Now)
	userID := createTestUser(t, q)

	targetRoute := idempotencyTestRoute
	targetKey := uuid.New()
	firstHash := sha256.Sum256([]byte("original body"))
	createIdempotencyRecord(ctx, t, q, userID, targetRoute, targetKey, firstHash, clock.Now().Add(time.Hour))

	expiredRoute := "PATCH /api/v1/resumes/{id}/title"
	expiredKey := uuid.New()
	expiredHash := sha256.Sum256([]byte("expired unrelated request"))
	createIdempotencyRecord(ctx, t, q, userID, expiredRoute, expiredKey, expiredHash, clock.Now().Add(-time.Second))

	otherHash := sha256.Sum256([]byte("different body"))
	_, replayed, err := idem.ExecuteForTest(ctx, userID, targetRoute, targetKey, otherHash,
		func(qtx *store.Queries) (resume.StoredResponse, error) {
			t.Fatal("mutate called for a live key with a different request hash")
			return resume.StoredResponse{}, nil
		},
	)
	if !errors.Is(err, resume.ErrIdempotencyKeyReuse) {
		t.Fatalf("Execute() error = %v, want resume.ErrIdempotencyKeyReuse", err)
	}
	if replayed {
		t.Errorf("Execute() replayed = true, want false")
	}

	assertIdempotencyRecordAbsent(ctx, t, q, userID, expiredRoute, expiredKey)
	if _, getErr := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
		UserID: userID, Route: targetRoute, IdempotencyKey: targetKey,
	}); getErr != nil {
		t.Errorf("GetIdempotencyRecord(target) after rejected reuse error = %v, want target record preserved", getErr)
	}
}

func TestIdempotencyStore_Execute_MutateError_RollsBackWriteAndPersistsNothing(t *testing.T) {
	t.Parallel()
	idem, rs, q, _, ctx := newIntegrationIdempotencyStore(t, testutil.NewClockAtEpoch().Now)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	key := uuid.New()
	hash := sha256.Sum256([]byte("body that fails"))
	errMutateFailed := errors.New("test: mutate failed after writing")

	var writtenID uuid.UUID
	_, replayed, err := idem.ExecuteForTest(ctx, userID, idempotencyTestRoute, key, hash,
		func(qtx *store.Queries) (resume.StoredResponse, error) {
			created, createErr := rs.CreateTxForTest(ctx, qtx, userID, "Should Roll Back", doc)
			if createErr != nil {
				t.Fatalf("mutate's CreateTxForTest() error = %v", createErr)
			}
			writtenID = created.ID
			return resume.StoredResponse{}, errMutateFailed
		},
	)
	if !errors.Is(err, errMutateFailed) {
		t.Fatalf("Execute() error = %v, want errMutateFailed", err)
	}
	if replayed {
		t.Errorf("Execute() replayed = true, want false")
	}

	if _, getErr := rs.Get(ctx, userID, writtenID); !errors.Is(getErr, resume.ErrNotFound) {
		t.Errorf("Get(writtenID) after mutate error = %v, want resume.ErrNotFound (mutate's own write must roll back too)", getErr)
	}

	if _, getErr := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
		UserID: userID, Route: idempotencyTestRoute, IdempotencyKey: key,
	}); !errors.Is(getErr, pgx.ErrNoRows) {
		t.Errorf("GetIdempotencyRecord() after mutate error = %v, want pgx.ErrNoRows (nothing persisted)", getErr)
	}
}

func TestIdempotencyStore_Execute_MutateError_ReapPersistsWhileMutationRollsBack(t *testing.T) {
	t.Parallel()
	clock := testutil.NewClockAtEpoch()
	idem, rs, q, _, ctx := newIntegrationIdempotencyStore(t, clock.Now)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	expiredRoute := "PATCH /api/v1/resumes/{id}/personal-details"
	expiredKey := uuid.New()
	expiredHash := sha256.Sum256([]byte("expired unrelated request"))
	createIdempotencyRecord(ctx, t, q, userID, expiredRoute, expiredKey, expiredHash, clock.Now().Add(-time.Second))

	failedKey := uuid.New()
	failedHash := sha256.Sum256([]byte("request whose mutation fails"))
	errMutateFailed := errors.New("test: mutation failed after writing")
	var writtenID uuid.UUID
	_, replayed, err := idem.ExecuteForTest(ctx, userID, idempotencyTestRoute, failedKey, failedHash,
		func(qtx *store.Queries) (resume.StoredResponse, error) {
			created, createErr := rs.CreateTxForTest(ctx, qtx, userID, "Must Roll Back", doc)
			if createErr != nil {
				return resume.StoredResponse{}, createErr
			}
			writtenID = created.ID
			return resume.StoredResponse{}, errMutateFailed
		},
	)
	if !errors.Is(err, errMutateFailed) {
		t.Fatalf("Execute() error = %v, want errMutateFailed", err)
	}
	if replayed {
		t.Errorf("Execute() replayed = true, want false")
	}

	assertIdempotencyRecordAbsent(ctx, t, q, userID, expiredRoute, expiredKey)
	assertIdempotencyRecordAbsent(ctx, t, q, userID, idempotencyTestRoute, failedKey)
	if _, getErr := rs.Get(ctx, userID, writtenID); !errors.Is(getErr, resume.ErrNotFound) {
		t.Errorf("Get(writtenID) after mutate error = %v, want resume.ErrNotFound", getErr)
	}
}

func TestIdempotencyStore_Execute_ExpiredRecord_TreatedAsFreshAndReplaced(t *testing.T) {
	t.Parallel()
	clock := testutil.NewClockAtEpoch()
	idem, _, q, _, ctx := newIntegrationIdempotencyStore(t, clock.Now)
	userID := createTestUser(t, q)

	key := uuid.New()
	hash := sha256.Sum256([]byte("first request"))
	firstResp := resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"n":1}`)}

	if _, _, err := idem.ExecuteForTest(ctx, userID, idempotencyTestRoute, key, hash,
		func(qtx *store.Queries) (resume.StoredResponse, error) { return firstResp, nil },
	); err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	firstRow, err := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
		UserID: userID, Route: idempotencyTestRoute, IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("GetIdempotencyRecord() after first call error = %v", err)
	}

	// Advance the injected clock past IdempotencyTTL -- expiry is proven by
	// moving the fake clock forward, never by sleeping.
	clock.Advance(resume.IdempotencyTTL + time.Second)

	secondResp := resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"n":2}`)}
	calls := 0
	got, replayed, err := idem.ExecuteForTest(ctx, userID, idempotencyTestRoute, key, hash,
		func(qtx *store.Queries) (resume.StoredResponse, error) {
			calls++
			return secondResp, nil
		},
	)
	if err != nil {
		t.Fatalf("second Execute() (past TTL) error = %v", err)
	}
	if replayed {
		t.Errorf("Execute() past TTL replayed = true, want false (an expired record is treated as absent)")
	}
	if calls != 1 {
		t.Errorf("mutate called %d times, want exactly 1 (a fresh execution)", calls)
	}
	if got.Status != secondResp.Status {
		t.Errorf("Execute() past TTL = %+v, want status %d", got, secondResp.Status)
	}
	assertJSONEqual(t, got.Body, secondResp.Body)

	secondRow, err := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
		UserID: userID, Route: idempotencyTestRoute, IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("GetIdempotencyRecord() after second call error = %v", err)
	}
	if secondRow.ID == firstRow.ID {
		t.Errorf("record after expiry = same ID %v as before, want a REPLACED row", secondRow.ID)
	}
	assertJSONEqual(t, secondRow.ResponseBody, secondResp.Body)
	if !bytes.Equal(got.Body, secondRow.ResponseBody) {
		t.Errorf("fresh response body = %s, want normalized stored bytes %s", got.Body, secondRow.ResponseBody)
	}
}

// TestIdempotencyStore_Execute_ConcurrentSaveDocumentConverges proves a
// same-key contender waits for the winning CAS mutation and then replays or
// rejects from its stored result. The channels hold the winner open after its
// CAS write and start the contender while that write is uncommitted; no sleeps
// or scheduler timing assumptions are involved.
func TestIdempotencyStore_Execute_ConcurrentSaveDocumentConverges(t *testing.T) {
	tests := []struct {
		name                  string
		contenderUsesSameHash bool
	}{
		{name: "same hash replays winning stored response", contenderUsesSameHash: true},
		{name: "different hash rejects reuse without loser effects", contenderUsesSameHash: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			clock := testutil.NewClockAtEpoch()
			winnerStore, rs, q, pool, ctx := newIntegrationIdempotencyStore(t, clock.Now)
			userID := createTestUserWithContext(ctx, t, q)
			created, err := rs.Create(ctx, userID, "CAS idempotency contention", validDocForTest(t))
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}

			winnerDoc := validDocForTest(t)
			winnerDoc.PersonalDetails.FullName = strp("Idempotency Winner")
			contenderDoc := validDocForTest(t)
			contenderDoc.PersonalDetails.FullName = strp("Must Never Commit")

			key := uuid.New()
			winnerHash := sha256.Sum256([]byte("winning request body"))
			contenderHash := winnerHash
			if !tt.contenderUsesSameHash {
				contenderHash = sha256.Sum256([]byte("different request body"))
			}

			type mutationReady struct {
				resp resume.StoredResponse
				err  error
			}
			winnerReady := make(chan mutationReady, 1)
			releaseWinner := make(chan struct{})
			winnerResult := make(chan idempotencyExecuteResult, 1)
			var winnerCalls atomic.Int32
			go func() {
				resp, replayed, executeErr := winnerStore.ExecuteForTest(ctx, userID, idempotencyTestRoute, key, winnerHash,
					func(qtx *store.Queries) (resume.StoredResponse, error) {
						winnerCalls.Add(1)
						revision, saveErr := rs.SaveDocumentTxForTest(ctx, qtx, userID, created.ID, winnerDoc, created.Revision)
						stored := resume.StoredResponse{
							Status: 200,
							Body:   json.RawMessage(fmt.Sprintf(`{"revision":%d,"winner":true}`, revision)),
						}
						winnerReady <- mutationReady{resp: stored, err: saveErr}
						if saveErr != nil {
							return resume.StoredResponse{}, saveErr
						}
						<-releaseWinner
						return stored, nil
					},
				)
				winnerResult <- idempotencyExecuteResult{resp: resp, replayed: replayed, err: executeErr}
			}()

			ready := <-winnerReady
			if ready.err != nil {
				close(releaseWinner)
				got := <-winnerResult
				t.Fatalf("winner SaveDocumentTxForTest() error = %v; Execute result = %+v", ready.err, got)
			}

			// Execute must hold the same users-row lock Create uses before it
			// invokes mutate. This is both the serialization invariant and a
			// deterministic way for the test to observe the critical section
			// while still releasing every goroutine after an assertion failure.
			ownerLocked, probeErr := userRowLockedForResumeWrite(ctx, pool, userID)
			if probeErr != nil {
				close(releaseWinner)
				got := <-winnerResult
				t.Fatalf("owner-row lock probe error = %v; winner Execute result = %+v", probeErr, got)
			}
			if !ownerLocked {
				t.Error("Execute invoked mutate without first locking the user's row")
			}

			contenderStarted := make(chan struct{})
			contenderStore := resume.NewIdempotencyStoreForTest(pool, func() time.Time {
				close(contenderStarted)
				return clock.Now()
			})
			contenderEnteredMutate := make(chan struct{})
			contenderResult := make(chan idempotencyExecuteResult, 1)
			var contenderCalls atomic.Int32
			go func() {
				resp, replayed, executeErr := contenderStore.ExecuteForTest(ctx, userID, idempotencyTestRoute, key, contenderHash,
					func(qtx *store.Queries) (resume.StoredResponse, error) {
						contenderCalls.Add(1)
						close(contenderEnteredMutate)
						revision, saveErr := rs.SaveDocumentTxForTest(ctx, qtx, userID, created.ID, contenderDoc, created.Revision)
						return resume.StoredResponse{
							Status: 200,
							Body:   json.RawMessage(fmt.Sprintf(`{"revision":%d,"winner":false}`, revision)),
						}, saveErr
					},
				)
				contenderResult <- idempotencyExecuteResult{resp: resp, replayed: replayed, err: executeErr}
			}()

			<-contenderStarted
			if !ownerLocked {
				// Without the owner lock, wait until the contender enters mutate and
				// blocks on the winner's uncommitted resume-row update. With the lock,
				// the contender cannot reach this callback.
				<-contenderEnteredMutate
			}
			close(releaseWinner)

			gotWinner := <-winnerResult
			gotContender := <-contenderResult
			if gotWinner.err != nil || gotWinner.replayed {
				t.Errorf("winner Execute() = {replayed:%v err:%v}, want {false nil}", gotWinner.replayed, gotWinner.err)
			}
			if gotWinner.resp.Status != ready.resp.Status {
				t.Errorf("winner Execute() status = %d, want callback status %d", gotWinner.resp.Status, ready.resp.Status)
			}
			assertJSONEqual(t, gotWinner.resp.Body, ready.resp.Body)
			if winnerCalls.Load() != 1 {
				t.Errorf("winner mutate calls = %d, want 1", winnerCalls.Load())
			}
			if contenderCalls.Load() != 0 {
				t.Errorf("contender mutate calls = %d, want 0 (serialized contender must decide from winner's record)", contenderCalls.Load())
			}

			record, err := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
				UserID: userID, Route: idempotencyTestRoute, IdempotencyKey: key,
			})
			if err != nil {
				t.Fatalf("GetIdempotencyRecord() error = %v", err)
			}
			if !bytes.Equal(record.RequestHash, winnerHash[:]) {
				t.Errorf("stored request hash = %x, want winning hash %x", record.RequestHash, winnerHash)
			}
			assertJSONEqual(t, record.ResponseBody, ready.resp.Body)
			if !bytes.Equal(gotWinner.resp.Body, record.ResponseBody) {
				t.Errorf("winner response body = %s, want normalized stored bytes %s", gotWinner.resp.Body, record.ResponseBody)
			}

			if tt.contenderUsesSameHash {
				if gotContender.err != nil || !gotContender.replayed {
					t.Errorf("same-hash contender Execute() = {replayed:%v err:%v}, want {true nil}", gotContender.replayed, gotContender.err)
				}
				if gotContender.resp.Status != int(record.ResponseStatus) || !bytes.Equal(gotContender.resp.Body, record.ResponseBody) {
					t.Errorf("same-hash contender response = %+v, want stored winner {Status:%d Body:%s}", gotContender.resp, record.ResponseStatus, record.ResponseBody)
				}
			} else {
				if !errors.Is(gotContender.err, resume.ErrIdempotencyKeyReuse) || gotContender.replayed {
					t.Errorf("different-hash contender Execute() = {replayed:%v err:%v}, want {false ErrIdempotencyKeyReuse}", gotContender.replayed, gotContender.err)
				}
			}

			current, err := rs.Get(ctx, userID, created.ID)
			if err != nil {
				t.Fatalf("Get() after contention error = %v", err)
			}
			if current.Revision != created.Revision+1 {
				t.Errorf("revision after contention = %d, want %d (exactly one mutation)", current.Revision, created.Revision+1)
			}
			gotDoc, err := resume.AssembleCanonical(current.Doc)
			if err != nil {
				t.Fatalf("AssembleCanonical(current.Doc) error = %v", err)
			}
			wantDoc, err := resume.AssembleCanonical(winnerDoc)
			if err != nil {
				t.Fatalf("AssembleCanonical(winnerDoc) error = %v", err)
			}
			if !bytes.Equal(gotDoc, wantDoc) {
				t.Errorf("document after contention = %s, want winner %s", gotDoc, wantDoc)
			}
		})
	}
}

// --- Composition across multiple keys, including cap rejection ---

func TestIdempotencyStore_Execute_ComposesRealCapCheckAcrossKeys(t *testing.T) {
	t.Parallel()
	idem, rs, q, _, ctx := newIntegrationIdempotencyStore(t, testutil.NewClockAtEpoch().Now)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	for i := 0; i < 3; i++ {
		key := uuid.New()
		hash := sha256.Sum256([]byte(fmt.Sprintf("body-%d", i)))
		mutate := func(qtx *store.Queries) (resume.StoredResponse, error) {
			created, err := rs.CreateTxForTest(ctx, qtx, userID, fmt.Sprintf("Resume %d", i), doc)
			if err != nil {
				return resume.StoredResponse{}, err
			}
			body, marshalErr := json.Marshal(map[string]string{"id": created.ID.String()})
			if marshalErr != nil {
				return resume.StoredResponse{}, marshalErr
			}
			return resume.StoredResponse{Status: 201, Body: body}, nil
		}
		if _, replayed, err := idem.ExecuteForTest(ctx, userID, idempotencyTestRoute, key, hash, mutate); err != nil {
			t.Fatalf("Execute() #%d error = %v", i, err)
		} else if replayed {
			t.Errorf("Execute() #%d replayed = true, want false (first use of a fresh key)", i)
		}
	}

	list, err := rs.List(ctx, userID)
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("List() = %d resumes, want 3 (cap accounting across 3 distinct Execute() calls)", len(list))
	}

	// A 4th resume for this user must still be rejected by the REAL cap
	// check running inside mutate, inside IdempotencyStore's own
	// transaction -- and Execute must not insert an idempotency record for
	// the rejected key: nothing to replay for a rejected mutation.
	fourthKey := uuid.New()
	fourthHash := sha256.Sum256([]byte("body-4"))
	calls := 0
	mutate := func(qtx *store.Queries) (resume.StoredResponse, error) {
		calls++
		_, createErr := rs.CreateTxForTest(ctx, qtx, userID, "Resume 4 (over cap)", doc)
		return resume.StoredResponse{}, createErr
	}
	_, replayed, err := idem.ExecuteForTest(ctx, userID, idempotencyTestRoute, fourthKey, fourthHash, mutate)
	if !errors.Is(err, resume.ErrCapExceeded) {
		t.Fatalf("Execute() 4th error = %v, want resume.ErrCapExceeded", err)
	}
	if replayed {
		t.Errorf("Execute() 4th replayed = true, want false")
	}
	if calls != 1 {
		t.Errorf("mutate called %d times, want exactly 1 (it must still run once and fail, not be skipped)", calls)
	}

	if _, getErr := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
		UserID: userID, Route: idempotencyTestRoute, IdempotencyKey: fourthKey,
	}); !errors.Is(getErr, pgx.ErrNoRows) {
		t.Errorf("GetIdempotencyRecord(4th key) error = %v, want pgx.ErrNoRows (a rejected mutation must not leave a record)", getErr)
	}

	list, err = rs.List(ctx, userID)
	if err != nil {
		t.Fatalf("List() after rejected 4th error: %v", err)
	}
	if len(list) != 3 {
		t.Errorf("List() after rejected 4th = %d resumes, want 3 (still capped)", len(list))
	}
}

// --- P2B inspection and commit-outcome contract ---

func TestIdempotencyStore_Inspect_DecidesWithoutReservingOrCleaning(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	idem, _, q, pool, ctx := newIntegrationIdempotencyStore(t, clock.Now)
	userID := createTestUser(t, q)
	key := uuid.New()
	hash := sha256.Sum256([]byte("inspect body"))

	if got, replayed, err := idem.Inspect(ctx, userID, idempotencyTestRoute, key, hash); err != nil || replayed || got.Status != 0 {
		t.Fatalf("Inspect(absent) = (%+v, %t, %v), want zero, false, nil", got, replayed, err)
	}
	var usageRows int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM idempotency_usage WHERE user_id = $1`, userID).Scan(&usageRows); err != nil {
		t.Fatalf("count usage rows after absent Inspect: %v", err)
	}
	if usageRows != 0 {
		t.Errorf("usage rows after absent Inspect = %d, want 0", usageRows)
	}

	want := resume.StoredResponse{
		Status:  200,
		Body:    json.RawMessage(`{"z":1,"a":2}`),
		Headers: map[string]string{"ETag": `"2"`},
	}
	result, err := idem.Execute(ctx, userID, idempotencyTestRoute, key, hash,
		func(*store.Queries) (resume.StoredResponse, error) { return want, nil })
	if err != nil {
		t.Fatalf("Execute() seed error: %v", err)
	}
	if result.Replayed || result.Outcome != resume.CommitCommitted {
		t.Fatalf("Execute() seed result = %+v, want fresh committed", result)
	}

	got, replayed, err := idem.Inspect(ctx, userID, idempotencyTestRoute, key, hash)
	if err != nil || !replayed {
		t.Fatalf("Inspect(live same hash) = (%+v, %t, %v), want replay", got, replayed, err)
	}
	if got.Status != result.Response.Status || !bytes.Equal(got.Body, result.Response.Body) || !reflect.DeepEqual(got.Headers, result.Response.Headers) {
		t.Errorf("Inspect(live) = %+v, want stored Execute response %+v", got, result.Response)
	}

	otherHash := sha256.Sum256([]byte("different inspect body"))
	if _, replayed, err := idem.Inspect(ctx, userID, idempotencyTestRoute, key, otherHash); !errors.Is(err, resume.ErrIdempotencyKeyReuse) || replayed {
		t.Errorf("Inspect(changed hash) = (replayed=%t, err=%v), want false, ErrIdempotencyKeyReuse", replayed, err)
	}

	clock.Advance(resume.IdempotencyTTL + time.Second)
	if got, replayed, err := idem.Inspect(ctx, userID, idempotencyTestRoute, key, hash); err != nil || replayed || got.Status != 0 {
		t.Errorf("Inspect(expired) = (%+v, %t, %v), want zero, false, nil", got, replayed, err)
	}
	var retained int64
	if err := pool.QueryRow(ctx, `SELECT retained_records FROM idempotency_usage WHERE user_id = $1`, userID).Scan(&retained); err != nil {
		t.Fatalf("read usage after expired Inspect: %v", err)
	}
	if retained != 1 {
		t.Errorf("retained records after expired Inspect = %d, want 1 (Inspect does not clean)", retained)
	}
}

func TestIdempotencyStore_Inspect_TwoMissesStillConvergeAtExecute(t *testing.T) {
	idem, _, q, _, ctx := newIntegrationIdempotencyStore(t, testutil.NewClockAtEpoch().Now)
	userID := createTestUser(t, q)
	key := uuid.New()
	hash := sha256.Sum256([]byte("inspect race"))

	type inspectResult struct {
		replayed bool
		err      error
	}
	inspectResults := make(chan inspectResult, 2)
	startInspect := make(chan struct{})
	for range 2 {
		go func() {
			<-startInspect
			_, replayed, err := idem.Inspect(ctx, userID, idempotencyTestRoute, key, hash)
			inspectResults <- inspectResult{replayed: replayed, err: err}
		}()
	}
	close(startInspect)
	for range 2 {
		got := <-inspectResults
		if got.err != nil || got.replayed {
			t.Fatalf("concurrent Inspect = (replayed=%t, err=%v), want miss", got.replayed, got.err)
		}
	}

	type executeOutcome struct {
		result resume.ExecuteResult
		err    error
	}
	executeResults := make(chan executeOutcome, 2)
	startExecute := make(chan struct{})
	var calls atomic.Int32
	mutate := func(*store.Queries) (resume.StoredResponse, error) {
		calls.Add(1)
		return resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"winner":true}`)}, nil
	}
	for range 2 {
		go func() {
			<-startExecute
			result, err := idem.Execute(ctx, userID, idempotencyTestRoute, key, hash, mutate)
			executeResults <- executeOutcome{result: result, err: err}
		}()
	}
	close(startExecute)

	replays := 0
	var bodies [2]json.RawMessage
	for i := range 2 {
		got := <-executeResults
		if got.err != nil || got.result.Outcome != resume.CommitCommitted {
			t.Fatalf("Execute after concurrent misses = (%+v, %v), want committed", got.result, got.err)
		}
		if got.result.Replayed {
			replays++
		}
		bodies[i] = got.result.Response.Body
	}
	if calls.Load() != 1 || replays != 1 {
		t.Errorf("after two Inspect misses: callback calls=%d replays=%d, want 1 and 1", calls.Load(), replays)
	}
	if !bytes.Equal(bodies[0], bodies[1]) {
		t.Errorf("winner/replay bodies differ: %s vs %s", bodies[0], bodies[1])
	}
}

func TestIdempotencyStore_Execute_CommitOutcomeMatrix(t *testing.T) {
	_, rs, q, pool, ctx := newIntegrationIdempotencyStore(t, testutil.NewClockAtEpoch().Now)
	clock := testutil.NewClockAtEpoch()
	response := resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"ok":true}`)}

	assertPair := func(t *testing.T, result resume.ExecuteResult) {
		t.Helper()
		if result.Replayed && result.Outcome != resume.CommitCommitted {
			t.Fatalf("invalid ExecuteResult pair: Replayed=true with outcome %v", result.Outcome)
		}
	}

	t.Run("cleanup begin failure is not attempted", func(t *testing.T) {
		userID := createTestUserWithContext(ctx, t, q)
		injected := errors.New("begin cleanup failed")
		idem := resume.NewIdempotencyStoreWithHooksForTest(pool, clock.Now,
			func(context.Context) (pgx.Tx, error) { return nil, injected }, nil)
		var calls atomic.Int32
		result, err := idem.Execute(ctx, userID, idempotencyTestRoute, uuid.New(), sha256.Sum256([]byte("cleanup begin")),
			func(*store.Queries) (resume.StoredResponse, error) { calls.Add(1); return response, nil })
		if !errors.Is(err, injected) || result.Outcome != resume.CommitNotAttempted || calls.Load() != 0 {
			t.Errorf("Execute() = (%+v, %v), calls=%d; want CommitNotAttempted, injected error, 0 calls", result, err, calls.Load())
		}
		assertPair(t, result)
	})

	t.Run("cleanup query failure after begin is not attempted", func(t *testing.T) {
		userID := createTestUserWithContext(ctx, t, q)
		key := uuid.New()
		hash := sha256.Sum256([]byte("cleanup query failure seed"))
		createIdempotencyRecord(ctx, t, q, userID, "POST cleanup-query-failure-seed", key, hash,
			clock.Now().Add(-time.Hour))

		injected := errors.New("delete expired cleanup records failed")
		var begins atomic.Int32
		idem := resume.NewIdempotencyStoreWithHooksForTest(pool, clock.Now,
			func(ctx context.Context) (pgx.Tx, error) {
				tx, err := pool.Begin(ctx)
				if err != nil {
					return nil, err
				}
				begins.Add(1)
				return &queryRowFailureTx{
					Tx: tx, queryFragment: "-- name: DeleteExpiredIdempotencyRecordsForUser", err: injected,
				}, nil
			}, nil)
		var calls atomic.Int32
		result, err := idem.Execute(ctx, userID, idempotencyTestRoute, uuid.New(),
			sha256.Sum256([]byte("cleanup query failure")),
			func(*store.Queries) (resume.StoredResponse, error) {
				calls.Add(1)
				return response, nil
			})
		if !errors.Is(err, injected) || result.Outcome != resume.CommitNotAttempted ||
			result.Replayed || calls.Load() != 0 || begins.Load() != 1 {
			t.Errorf("Execute() = (%+v, %v), calls=%d begins=%d; want CommitNotAttempted, injected error, 0 calls, 1 begin",
				result, err, calls.Load(), begins.Load())
		}
		assertPair(t, result)
		if _, getErr := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
			UserID: userID, Route: "POST cleanup-query-failure-seed", IdempotencyKey: key,
		}); getErr != nil {
			t.Errorf("expired record after failed cleanup query: %v", getErr)
		}
		assertIdempotencyUsageMatchesRows(ctx, t, pool, userID)
		locked, err := userRowLockedForResumeWrite(ctx, pool, userID)
		if err != nil {
			t.Fatalf("probe owner lock after cleanup rollback: %v", err)
		}
		if locked {
			t.Error("cleanup transaction still holds the owner-row lock after Execute returned")
		}
	})

	t.Run("mutation begin failure is not attempted", func(t *testing.T) {
		userID := createTestUserWithContext(ctx, t, q)
		injected := errors.New("begin mutation failed")
		begins := 0
		idem := resume.NewIdempotencyStoreWithHooksForTest(pool, clock.Now,
			func(ctx context.Context) (pgx.Tx, error) {
				begins++
				if begins == 2 {
					return nil, injected
				}
				return pool.Begin(ctx)
			}, nil)
		var calls atomic.Int32
		result, err := idem.Execute(ctx, userID, idempotencyTestRoute, uuid.New(), sha256.Sum256([]byte("mutation begin")),
			func(*store.Queries) (resume.StoredResponse, error) { calls.Add(1); return response, nil })
		if !errors.Is(err, injected) || result.Outcome != resume.CommitNotAttempted || calls.Load() != 0 || begins != 2 {
			t.Errorf("Execute() = (%+v, %v), calls=%d begins=%d; want CommitNotAttempted, 0 calls, 2 begins", result, err, calls.Load(), begins)
		}
		assertPair(t, result)
	})

	for _, tc := range []struct {
		name        string
		commitErr   error
		wantOutcome resume.CommitOutcome
	}{
		{name: "server rollback at commit is definite", commitErr: pgx.ErrTxCommitRollback, wantOutcome: resume.CommitDefinitelyRolledBack},
		{name: "server pg error at commit is definite", commitErr: &pgconn.PgError{Code: "40001", Message: "serialization failure"}, wantOutcome: resume.CommitDefinitelyRolledBack},
		{name: "transaction resolution pg error is unknown", commitErr: &pgconn.PgError{Code: "08007", Message: "transaction resolution unknown"}, wantOutcome: resume.CommitUnknown},
		{name: "statement completion pg error is unknown", commitErr: &pgconn.PgError{Code: "40003", Message: "statement completion unknown"}, wantOutcome: resume.CommitUnknown},
		{name: "connection loss at commit is unknown", commitErr: errors.New("connection lost during commit"), wantOutcome: resume.CommitUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			userID := createTestUserWithContext(ctx, t, q)
			idem := resume.NewIdempotencyStoreWithHooksForTest(pool, clock.Now, nil,
				func(context.Context, pgx.Tx) error { return tc.commitErr })
			key := uuid.New()
			var createdID uuid.UUID
			result, err := idem.Execute(ctx, userID, idempotencyTestRoute, key, sha256.Sum256([]byte(tc.name)),
				func(qtx *store.Queries) (resume.StoredResponse, error) {
					created, createErr := rs.CreateTx(ctx, qtx, userID, tc.name, nil, validDocForTest(t))
					createdID = created.ID
					return response, createErr
				})
			if err == nil || result.Outcome != tc.wantOutcome || result.Replayed {
				t.Errorf("Execute() = (%+v, %v), want outcome %v with error and no replay", result, err, tc.wantOutcome)
			}
			assertPair(t, result)
			assertIdempotencyRecordAbsent(ctx, t, q, userID, idempotencyTestRoute, key)
			if createdID != uuid.Nil {
				if _, getErr := rs.Get(ctx, userID, createdID); !errors.Is(getErr, resume.ErrNotFound) {
					t.Errorf("Get(created after injected commit error) = %v, want ErrNotFound", getErr)
				}
			}
		})
	}

	t.Run("lost commit response after commit stays unknown and may persist", func(t *testing.T) {
		userID := createTestUserWithContext(ctx, t, q)
		injected := errors.New("connection lost after commit")
		idem := resume.NewIdempotencyStoreWithHooksForTest(pool, clock.Now, nil,
			func(ctx context.Context, tx pgx.Tx) error {
				if err := tx.Commit(ctx); err != nil {
					t.Fatalf("injected underlying commit: %v", err)
				}
				return injected
			})
		key := uuid.New()
		var createdID uuid.UUID
		result, err := idem.Execute(ctx, userID, "POST committed-then-lost", key,
			sha256.Sum256([]byte("committed then lost")),
			func(qtx *store.Queries) (resume.StoredResponse, error) {
				created, createErr := rs.CreateTx(ctx, qtx, userID, "committed then lost", nil, validDocForTest(t))
				createdID = created.ID
				return response, createErr
			})
		if !errors.Is(err, injected) || result.Outcome != resume.CommitUnknown || result.Replayed {
			t.Fatalf("Execute() = (%+v, %v), want non-replayed CommitUnknown with injected error", result, err)
		}
		assertPair(t, result)
		if _, getErr := rs.Get(ctx, userID, createdID); getErr != nil {
			t.Errorf("Get(committed mutation after lost response): %v", getErr)
		}
		if _, getErr := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
			UserID: userID, Route: "POST committed-then-lost", IdempotencyKey: key,
		}); getErr != nil {
			t.Errorf("GetIdempotencyRecord(committed after lost response): %v", getErr)
		}
	})

	t.Run("success and replay are committed", func(t *testing.T) {
		userID := createTestUserWithContext(ctx, t, q)
		idem := resume.NewIdempotencyStoreForTest(pool, clock.Now)
		key := uuid.New()
		hash := sha256.Sum256([]byte("success and replay"))
		first, err := idem.Execute(ctx, userID, idempotencyTestRoute, key, hash,
			func(*store.Queries) (resume.StoredResponse, error) { return response, nil })
		if err != nil || first.Replayed || first.Outcome != resume.CommitCommitted {
			t.Fatalf("first Execute() = (%+v, %v), want fresh committed", first, err)
		}
		second, err := idem.Execute(ctx, userID, idempotencyTestRoute, key, hash,
			func(*store.Queries) (resume.StoredResponse, error) {
				return resume.StoredResponse{}, errors.New("replay callback ran")
			})
		if err != nil || !second.Replayed || second.Outcome != resume.CommitCommitted {
			t.Fatalf("replay Execute() = (%+v, %v), want replayed committed", second, err)
		}
		assertPair(t, first)
		assertPair(t, second)
	})
}

// --- P2B bounded retention and capacity accounting ---

func seedIdempotencyRows(ctx context.Context, t *testing.T, pool *store.Pool,
	userID uuid.UUID, routePrefix string, count int, firstExpiry time.Time,
) {
	t.Helper()
	if count <= 0 {
		return
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO idempotency_records
		    (user_id, route, idempotency_key, request_hash,
		     response_status, response_body, response_headers, expires_at)
		SELECT $1, $2 || lpad(g::text, 6, '0'), uuidv7(),
		       decode(repeat('00', 32), 'hex'), 200,
		       '{"seeded":true}'::jsonb, '{}'::jsonb,
		       $3::timestamptz + g * interval '1 microsecond'
		FROM generate_series(0, $4::integer - 1) AS g`,
		userID, routePrefix, firstExpiry, count)
	if err != nil {
		t.Fatalf("seed %d idempotency rows for %s: %v", count, routePrefix, err)
	}
}

func refreshIdempotencyUsage(ctx context.Context, t *testing.T, pool *store.Pool, userID uuid.UUID) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO idempotency_usage (user_id, retained_records, stored_bytes)
		SELECT user_id, count(*)::bigint,
		       COALESCE(sum(octet_length(response_body::text) +
		                    octet_length(response_headers::text)), 0)::bigint
		FROM idempotency_records WHERE user_id = $1 GROUP BY user_id
		ON CONFLICT (user_id) DO UPDATE
		SET retained_records = EXCLUDED.retained_records,
		    stored_bytes = EXCLUDED.stored_bytes`, userID)
	if err != nil {
		t.Fatalf("refresh idempotency usage: %v", err)
	}
}

func assertIdempotencyUsageMatchesRows(ctx context.Context, t *testing.T, pool *store.Pool, userID uuid.UUID) {
	t.Helper()
	var gotRecords, gotBytes, wantRecords, wantBytes int64
	if err := pool.QueryRow(ctx, `
		SELECT retained_records, stored_bytes
		FROM idempotency_usage WHERE user_id = $1`, userID).Scan(&gotRecords, &gotBytes); err != nil {
		t.Fatalf("read idempotency usage: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*)::bigint,
		       COALESCE(sum(octet_length(response_body::text) +
		                    octet_length(response_headers::text)), 0)::bigint
		FROM idempotency_records WHERE user_id = $1`, userID).Scan(&wantRecords, &wantBytes); err != nil {
		t.Fatalf("recompute idempotency usage: %v", err)
	}
	if gotRecords != wantRecords || gotBytes != wantBytes {
		t.Errorf("usage counters = (%d records, %d bytes), rows = (%d records, %d bytes)",
			gotRecords, gotBytes, wantRecords, wantBytes)
	}
}

func TestIdempotencyStore_Execute_BoundedOldestFirstCleanup(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	idem, _, q, pool, ctx := newIntegrationIdempotencyStore(t, clock.Now)
	userID := createTestUser(t, q)
	neighborID := createTestUser(t, q)

	seedIdempotencyRows(ctx, t, pool, userID, "cleanup-expired-", 201, clock.Now().Add(-2*time.Hour))
	seedIdempotencyRows(ctx, t, pool, userID, "cleanup-live-", 1, clock.Now().Add(time.Hour))
	seedIdempotencyRows(ctx, t, pool, neighborID, "neighbor-expired-", 2, clock.Now().Add(-2*time.Hour))
	refreshIdempotencyUsage(ctx, t, pool, userID)
	refreshIdempotencyUsage(ctx, t, pool, neighborID)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin EXPLAIN transaction: %v", err)
	}
	defer tx.Rollback(context.Background()) //nolint:errcheck // cleanup after read-only plan evidence
	if _, seqscanErr := tx.Exec(ctx, `SET LOCAL enable_seqscan = off`); seqscanErr != nil {
		t.Fatalf("disable seqscan for plan evidence: %v", seqscanErr)
	}
	if _, bitmapErr := tx.Exec(ctx, `SET LOCAL enable_bitmapscan = off`); bitmapErr != nil {
		t.Fatalf("disable bitmap scan for ordered-index plan evidence: %v", bitmapErr)
	}
	rows, err := tx.Query(ctx, `
		EXPLAIN (COSTS OFF)
		SELECT id FROM idempotency_records
		WHERE user_id = $1 AND expires_at <= $2
		ORDER BY expires_at, id LIMIT 200 FOR UPDATE SKIP LOCKED`, userID, clock.Now())
	if err != nil {
		t.Fatalf("EXPLAIN cleanup selection: %v", err)
	}
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			rows.Close()
			t.Fatalf("scan cleanup plan: %v", err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		t.Fatalf("read cleanup plan: %v", err)
	}
	rows.Close()
	if !strings.Contains(plan.String(), "idempotency_records_user_expires_id_idx") {
		t.Errorf("bounded cleanup plan does not use composite index:\n%s", plan.String())
	}

	if _, err := idem.Execute(ctx, userID, "POST cleanup-first", uuid.New(), sha256.Sum256([]byte("cleanup first")),
		func(*store.Queries) (resume.StoredResponse, error) {
			return resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"ok":1}`)}, nil
		}); err != nil {
		t.Fatalf("first cleanup Execute(): %v", err)
	}

	var expiredCount int64
	var remainingRoute string
	if err := pool.QueryRow(ctx, `
		SELECT count(*)::bigint, min(route)
		FROM idempotency_records
		WHERE user_id = $1 AND route LIKE 'cleanup-expired-%'`, userID).Scan(&expiredCount, &remainingRoute); err != nil {
		t.Fatalf("read expired remainder: %v", err)
	}
	if expiredCount != 1 || remainingRoute != "cleanup-expired-000200" {
		t.Errorf("expired remainder = (%d, %q), want newest single row cleanup-expired-000200", expiredCount, remainingRoute)
	}
	var liveCount, neighborCount int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM idempotency_records WHERE user_id = $1 AND route LIKE 'cleanup-live-%'`, userID).Scan(&liveCount); err != nil {
		t.Fatalf("count live rows: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM idempotency_records WHERE user_id = $1 AND route LIKE 'neighbor-expired-%'`, neighborID).Scan(&neighborCount); err != nil {
		t.Fatalf("count neighbor rows: %v", err)
	}
	if liveCount != 1 || neighborCount != 2 {
		t.Errorf("live/neighbor rows after cleanup = %d/%d, want 1/2", liveCount, neighborCount)
	}
	assertIdempotencyUsageMatchesRows(ctx, t, pool, userID)
	assertIdempotencyUsageMatchesRows(ctx, t, pool, neighborID)

	if _, err := idem.Execute(ctx, userID, "POST cleanup-second", uuid.New(), sha256.Sum256([]byte("cleanup second")),
		func(*store.Queries) (resume.StoredResponse, error) {
			return resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"ok":2}`)}, nil
		}); err != nil {
		t.Fatalf("second cleanup Execute(): %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM idempotency_records WHERE user_id = $1 AND route LIKE 'cleanup-expired-%'`, userID).Scan(&expiredCount); err != nil {
		t.Fatalf("count final expired rows: %v", err)
	}
	if expiredCount != 0 {
		t.Errorf("expired rows after second cleanup = %d, want 0", expiredCount)
	}
	assertIdempotencyUsageMatchesRows(ctx, t, pool, userID)
}

func TestIdempotencyStore_Execute_ConcurrentCleanupStaysBounded(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	idem, _, q, pool, ctx := newIntegrationIdempotencyStore(t, clock.Now)
	userID := createTestUser(t, q)
	seedIdempotencyRows(ctx, t, pool, userID, "concurrent-expired-", 401, clock.Now().Add(-2*time.Hour))
	refreshIdempotencyUsage(ctx, t, pool, userID)

	type result struct {
		result resume.ExecuteResult
		err    error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	var callbacks atomic.Int32
	for i := range 2 {
		i := i
		go func() {
			<-start
			got, err := idem.Execute(ctx, userID, fmt.Sprintf("POST concurrent-cleanup-%d", i), uuid.New(),
				sha256.Sum256([]byte(fmt.Sprintf("cleanup-%d", i))),
				func(*store.Queries) (resume.StoredResponse, error) {
					callbacks.Add(1)
					return resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"ok":true}`)}, nil
				})
			results <- result{result: got, err: err}
		}()
	}
	close(start)
	for range 2 {
		got := <-results
		if got.err != nil || got.result.Outcome != resume.CommitCommitted {
			t.Fatalf("concurrent cleanup Execute() = (%+v, %v), want committed", got.result, got.err)
		}
	}
	if callbacks.Load() != 2 {
		t.Errorf("callbacks = %d, want 2 distinct-key mutations", callbacks.Load())
	}
	var remaining int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM idempotency_records WHERE user_id = $1 AND route LIKE 'concurrent-expired-%'`, userID).Scan(&remaining); err != nil {
		t.Fatalf("count concurrent cleanup remainder: %v", err)
	}
	if remaining != 1 {
		t.Errorf("expired rows after two concurrent cleanups = %d, want 1 (two bounded batches of 200)", remaining)
	}
	assertIdempotencyUsageMatchesRows(ctx, t, pool, userID)
}

func setIdempotencyUsage(ctx context.Context, t *testing.T, pool *store.Pool,
	userID uuid.UUID, records, storedBytes int64,
) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO idempotency_usage (user_id, retained_records, stored_bytes)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE
		SET retained_records = EXCLUDED.retained_records,
		    stored_bytes = EXCLUDED.stored_bytes`, userID, records, storedBytes)
	if err != nil {
		t.Fatalf("set idempotency usage: %v", err)
	}
}

func assertCapacityError(t *testing.T, err error, wantRetryAfter int64) {
	t.Helper()
	if !errors.Is(err, resume.ErrIdempotencyCapacity) {
		t.Fatalf("error = %v, want ErrIdempotencyCapacity", err)
	}
	var capacity *resume.IdempotencyCapacityError
	if !errors.As(err, &capacity) {
		t.Fatalf("error type = %T, want *IdempotencyCapacityError", err)
	}
	if capacity.RetryAfterSeconds != wantRetryAfter {
		t.Errorf("RetryAfterSeconds = %d, want %d", capacity.RetryAfterSeconds, wantRetryAfter)
	}
}

func TestIdempotencyStore_Execute_RetainedCapacityAndReplayAtCap(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	idem, rs, q, pool, ctx := newIntegrationIdempotencyStore(t, clock.Now)

	t.Run("record cap rejects before callback", func(t *testing.T) {
		userID := createTestUserWithContext(ctx, t, q)
		setIdempotencyUsage(ctx, t, pool, userID, 50_000, 0)
		var calls atomic.Int32
		result, err := idem.Execute(ctx, userID, "POST record-cap", uuid.New(), sha256.Sum256([]byte("record cap")),
			func(*store.Queries) (resume.StoredResponse, error) {
				calls.Add(1)
				return resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"no":true}`)}, nil
			})
		assertCapacityError(t, err, 1)
		if result.Outcome != resume.CommitDefinitelyRolledBack || result.Replayed || calls.Load() != 0 {
			t.Errorf("result=%+v calls=%d, want definite rollback, no replay, no callback", result, calls.Load())
		}
	})

	t.Run("record count exactly at boundary commits", func(t *testing.T) {
		userID := createTestUserWithContext(ctx, t, q)
		response := resume.StoredResponse{
			Status: 200,
			Body:   json.RawMessage(`{"recordBoundary":true}`),
		}
		normalized, err := q.NormalizeIdempotencyResponse(ctx, store.NormalizeIdempotencyResponseParams{
			ResponseBody: response.Body, ResponseHeaders: json.RawMessage(`{}`),
		})
		if err != nil {
			t.Fatalf("normalize record-boundary response: %v", err)
		}
		setIdempotencyUsage(ctx, t, pool, userID, 49_999, 0)

		var calls atomic.Int32
		key := uuid.New()
		result, err := idem.Execute(ctx, userID, "POST record-boundary", key,
			sha256.Sum256([]byte("record boundary")),
			func(*store.Queries) (resume.StoredResponse, error) {
				calls.Add(1)
				return response, nil
			})
		if err != nil || result.Outcome != resume.CommitCommitted || result.Replayed || calls.Load() != 1 {
			t.Fatalf("Execute(record boundary) = (%+v, %v), calls=%d; want fresh commit and 1 call",
				result, err, calls.Load())
		}
		var records, storedBytes int64
		if err := pool.QueryRow(ctx, `
			SELECT retained_records, stored_bytes
			FROM idempotency_usage WHERE user_id = $1`, userID).Scan(&records, &storedBytes); err != nil {
			t.Fatalf("read usage at record boundary: %v", err)
		}
		if records != 50_000 || storedBytes != int64(normalized.StoredBytes) {
			t.Errorf("usage at record boundary = (%d records, %d bytes), want (50,000 records, %d bytes)",
				records, storedBytes, normalized.StoredBytes)
		}
		if _, err := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
			UserID: userID, Route: "POST record-boundary", IdempotencyKey: key,
		}); err != nil {
			t.Errorf("committed record at exact record boundary: %v", err)
		}
	})

	t.Run("stored bytes exactly at boundary commit", func(t *testing.T) {
		userID := createTestUserWithContext(ctx, t, q)
		response := resume.StoredResponse{
			Status: 200,
			Body:   json.RawMessage(` { "z": 1, "a": 2 } `),
		}
		normalized, err := q.NormalizeIdempotencyResponse(ctx, store.NormalizeIdempotencyResponseParams{
			ResponseBody: response.Body, ResponseHeaders: json.RawMessage(`{}`),
		})
		if err != nil {
			t.Fatalf("normalize byte-boundary response: %v", err)
		}
		initialBytes := int64(1<<30) - int64(normalized.StoredBytes)
		setIdempotencyUsage(ctx, t, pool, userID, 0, initialBytes)

		var calls atomic.Int32
		key := uuid.New()
		result, err := idem.Execute(ctx, userID, "POST byte-boundary", key,
			sha256.Sum256([]byte("byte boundary")),
			func(*store.Queries) (resume.StoredResponse, error) {
				calls.Add(1)
				return response, nil
			})
		if err != nil || result.Outcome != resume.CommitCommitted || result.Replayed || calls.Load() != 1 {
			t.Fatalf("Execute(byte boundary) = (%+v, %v), calls=%d; want fresh commit and 1 call",
				result, err, calls.Load())
		}
		var records, storedBytes int64
		if queryErr := pool.QueryRow(ctx, `
			SELECT retained_records, stored_bytes
			FROM idempotency_usage WHERE user_id = $1`, userID).Scan(&records, &storedBytes); queryErr != nil {
			t.Fatalf("read usage at byte boundary: %v", queryErr)
		}
		if records != 1 || storedBytes != 1<<30 {
			t.Errorf("usage at byte boundary = (%d records, %d bytes), want (1 record, %d bytes)",
				records, storedBytes, int64(1<<30))
		}
		row, err := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
			UserID: userID, Route: "POST byte-boundary", IdempotencyKey: key,
		})
		if err != nil {
			t.Fatalf("read committed record at exact byte boundary: %v", err)
		}
		if !bytes.Equal(row.ResponseBody, normalized.ResponseBody) || int64(row.ResponseStatus) != int64(response.Status) {
			t.Errorf("stored byte-boundary response = (status %d, body %s), want (status %d, normalized body %s)",
				row.ResponseStatus, row.ResponseBody, response.Status, normalized.ResponseBody)
		}
	})

	t.Run("existing key replays at record cap", func(t *testing.T) {
		userID := createTestUserWithContext(ctx, t, q)
		key := uuid.New()
		hash := sha256.Sum256([]byte("existing at cap"))
		inserted, err := q.CreateIdempotencyRecord(ctx, store.CreateIdempotencyRecordParams{
			UserID: userID, Route: "POST replay-at-cap", IdempotencyKey: key,
			RequestHash: hash[:], ResponseStatus: 200,
			ResponseBody: json.RawMessage(`{"stored":true}`), ResponseHeaders: json.RawMessage(`{}`),
			ExpiresAt: clock.Now().Add(time.Hour),
		})
		if err != nil {
			t.Fatalf("seed replay-at-cap record: %v", err)
		}
		setIdempotencyUsage(ctx, t, pool, userID, 50_000, int64(inserted.StoredBytes))
		var calls atomic.Int32
		result, err := idem.Execute(ctx, userID, "POST replay-at-cap", key, hash,
			func(*store.Queries) (resume.StoredResponse, error) {
				calls.Add(1)
				return resume.StoredResponse{}, errors.New("replay callback ran")
			})
		if err != nil || !result.Replayed || result.Outcome != resume.CommitCommitted || calls.Load() != 0 {
			t.Errorf("Execute(replay at cap) = (%+v, %v), calls=%d; want replayed committed", result, err, calls.Load())
		}
		if !bytes.Equal(result.Response.Body, inserted.ResponseBody) {
			t.Errorf("replay body = %s, want stored %s", result.Response.Body, inserted.ResponseBody)
		}
	})

	t.Run("byte cap rolls mutation back", func(t *testing.T) {
		userID := createTestUserWithContext(ctx, t, q)
		setIdempotencyUsage(ctx, t, pool, userID, 0, 1<<30)
		key := uuid.New()
		var createdID uuid.UUID
		result, err := idem.Execute(ctx, userID, "POST byte-cap", key, sha256.Sum256([]byte("byte cap")),
			func(qtx *store.Queries) (resume.StoredResponse, error) {
				created, createErr := rs.CreateTx(ctx, qtx, userID, "must roll back", nil, validDocForTest(t))
				createdID = created.ID
				return resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"tooLarge":true}`)}, createErr
			})
		assertCapacityError(t, err, 1)
		if result.Outcome != resume.CommitDefinitelyRolledBack || createdID == uuid.Nil {
			t.Errorf("byte-cap result=%+v createdID=%v, want definite rollback after callback", result, createdID)
		}
		if _, getErr := rs.Get(ctx, userID, createdID); !errors.Is(getErr, resume.ErrNotFound) {
			t.Errorf("Get(byte-cap mutation) = %v, want ErrNotFound", getErr)
		}
		assertIdempotencyRecordAbsent(ctx, t, q, userID, "POST byte-cap", key)
		var records, storedBytes int64
		if err := pool.QueryRow(ctx, `SELECT retained_records, stored_bytes FROM idempotency_usage WHERE user_id = $1`, userID).Scan(&records, &storedBytes); err != nil {
			t.Fatalf("read usage after byte-cap rollback: %v", err)
		}
		if records != 0 || storedBytes != 1<<30 {
			t.Errorf("usage after byte-cap rollback = (%d, %d), want (0, %d)", records, storedBytes, int64(1<<30))
		}
	})

	t.Run("earliest live expiry rounds up", func(t *testing.T) {
		userID := createTestUserWithContext(ctx, t, q)
		key := uuid.New()
		hash := sha256.Sum256([]byte("retry rounding seed"))
		inserted, err := q.CreateIdempotencyRecord(ctx, store.CreateIdempotencyRecordParams{
			UserID: userID, Route: "POST retry-rounding-seed", IdempotencyKey: key,
			RequestHash: hash[:], ResponseStatus: 200,
			ResponseBody: json.RawMessage(`{"seed":true}`), ResponseHeaders: json.RawMessage(`{}`),
			ExpiresAt: clock.Now().Add(1500 * time.Millisecond),
		})
		if err != nil {
			t.Fatalf("seed retry-rounding record: %v", err)
		}
		setIdempotencyUsage(ctx, t, pool, userID, 50_000, int64(inserted.StoredBytes))
		result, err := idem.Execute(ctx, userID, "POST retry-rounding-new", uuid.New(), sha256.Sum256([]byte("retry rounding new")),
			func(*store.Queries) (resume.StoredResponse, error) {
				return resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"no":true}`)}, nil
			})
		assertCapacityError(t, err, 2)
		if result.Outcome != resume.CommitDefinitelyRolledBack {
			t.Errorf("retry-rounding outcome = %v, want definite rollback", result.Outcome)
		}
	})
}

func TestIdempotencyStore_Execute_ExpiredBacklogRemainsCountedAtCap(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	idem, _, q, pool, ctx := newIntegrationIdempotencyStore(t, clock.Now)
	userID := createTestUser(t, q)
	t.Cleanup(func() {
		if _, cleanupErr := pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID); cleanupErr != nil {
			t.Errorf("clean up capacity test user: %v", cleanupErr)
		}
	})

	// One bulk insert makes the physical retained-row count itself the cap
	// authority. Cleanup removes only 200, leaving 50,001 expired rows still
	// counted, so this request cannot admit a replacement record.
	seedIdempotencyRows(ctx, t, pool, userID, "capacity-expired-", 50_201, clock.Now().Add(-2*time.Hour))
	refreshIdempotencyUsage(ctx, t, pool, userID)
	var calls atomic.Int32
	result, err := idem.Execute(ctx, userID, "POST capacity-expired-new", uuid.New(), sha256.Sum256([]byte("capacity expired new")),
		func(*store.Queries) (resume.StoredResponse, error) {
			calls.Add(1)
			return resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"no":true}`)}, nil
		})
	assertCapacityError(t, err, 1)
	if result.Outcome != resume.CommitDefinitelyRolledBack || calls.Load() != 0 {
		t.Errorf("expired-backlog result=%+v calls=%d, want definite rollback before callback", result, calls.Load())
	}
	var remaining int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM idempotency_records WHERE user_id = $1`, userID).Scan(&remaining); err != nil {
		t.Fatalf("count expired backlog remainder: %v", err)
	}
	if remaining != 50_001 {
		t.Errorf("retained rows after one bounded cleanup = %d, want 50,001", remaining)
	}
	assertIdempotencyUsageMatchesRows(ctx, t, pool, userID)
}

func TestIdempotencyStore_Execute_ExpiredExactKeyRollbackForcesOneSecondRetry(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	idem, _, q, pool, ctx := newIntegrationIdempotencyStore(t, clock.Now)
	userID := createTestUser(t, q)

	seedIdempotencyRows(ctx, t, pool, userID, "capacity-older-expired-", 200, clock.Now().Add(-2*time.Hour))
	targetRoute := "POST capacity-expired-exact"
	targetKey := uuid.New()
	targetHash := sha256.Sum256([]byte("capacity expired exact"))
	if _, err := q.CreateIdempotencyRecord(ctx, store.CreateIdempotencyRecordParams{
		UserID: userID, Route: targetRoute, IdempotencyKey: targetKey,
		RequestHash: targetHash[:], ResponseStatus: 200,
		ResponseBody: json.RawMessage(`{"target":true}`), ResponseHeaders: json.RawMessage(`{}`),
		ExpiresAt: clock.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("seed expired exact-key record: %v", err)
	}
	liveHash := sha256.Sum256([]byte("capacity later live"))
	if _, err := q.CreateIdempotencyRecord(ctx, store.CreateIdempotencyRecordParams{
		UserID: userID, Route: "POST capacity-later-live", IdempotencyKey: uuid.New(),
		RequestHash: liveHash[:], ResponseStatus: 200,
		ResponseBody: json.RawMessage(`{"live":true}`), ResponseHeaders: json.RawMessage(`{}`),
		ExpiresAt: clock.Now().Add(1500 * time.Millisecond),
	}); err != nil {
		t.Fatalf("seed later live record: %v", err)
	}

	var physicalBytes int64
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(sum(octet_length(response_body::text) +
		                    octet_length(response_headers::text)), 0)::bigint
		FROM idempotency_records WHERE user_id = $1`, userID).Scan(&physicalBytes); err != nil {
		t.Fatalf("read seeded response bytes: %v", err)
	}
	// Synthetic retained-record pressure keeps this regression small. Its
	// expected counter transition is still exact: cleanup commits -200,
	// while the exact-key deletion and -1 release must both roll back.
	setIdempotencyUsage(ctx, t, pool, userID, 50_201, physicalBytes)

	var calls atomic.Int32
	result, err := idem.Execute(ctx, userID, targetRoute, targetKey, targetHash,
		func(*store.Queries) (resume.StoredResponse, error) {
			calls.Add(1)
			return resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"no":true}`)}, nil
		})
	assertCapacityError(t, err, 1)
	if result.Outcome != resume.CommitDefinitelyRolledBack || result.Replayed || calls.Load() != 0 {
		t.Errorf("result=%+v calls=%d, want definite rollback, no replay, no callback", result, calls.Load())
	}

	var olderRows int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM idempotency_records
		WHERE user_id = $1 AND route LIKE 'capacity-older-expired-%'`, userID).Scan(&olderRows); err != nil {
		t.Fatalf("count older expired rows: %v", err)
	}
	if olderRows != 0 {
		t.Errorf("older expired rows after committed cleanup = %d, want 0", olderRows)
	}
	if _, err := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
		UserID: userID, Route: targetRoute, IdempotencyKey: targetKey,
	}); err != nil {
		t.Errorf("expired exact-key record after capacity rollback: %v", err)
	}

	var retainedRecords, storedBytes, remainingBytes int64
	if err := pool.QueryRow(ctx, `
		SELECT retained_records, stored_bytes
		FROM idempotency_usage WHERE user_id = $1`, userID).Scan(&retainedRecords, &storedBytes); err != nil {
		t.Fatalf("read usage after exact-key rollback: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT COALESCE(sum(octet_length(response_body::text) +
		                    octet_length(response_headers::text)), 0)::bigint
		FROM idempotency_records WHERE user_id = $1`, userID).Scan(&remainingBytes); err != nil {
		t.Fatalf("recompute retained response bytes: %v", err)
	}
	if retainedRecords != 50_001 || storedBytes != remainingBytes {
		t.Errorf("usage after cleanup and rollback = (%d records, %d bytes), want (50,001 records, %d bytes)",
			retainedRecords, storedBytes, remainingBytes)
	}
}

func TestIdempotencyStore_Execute_CapacityRetryQueryFailureIsSurfaced(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	_, _, q, pool, ctx := newIntegrationIdempotencyStore(t, clock.Now)
	userID := createTestUser(t, q)
	setIdempotencyUsage(ctx, t, pool, userID, 50_000, 0)

	injected := errors.New("capacity retry query failed")
	var begins atomic.Int32
	idem := resume.NewIdempotencyStoreWithHooksForTest(pool, clock.Now,
		func(ctx context.Context) (pgx.Tx, error) {
			tx, err := pool.Begin(ctx)
			if err != nil {
				return nil, err
			}
			if begins.Add(1) == 2 {
				return &queryRowFailureTx{
					Tx: tx, queryFragment: "AS expired_backlog", err: injected,
				}, nil
			}
			return tx, nil
		}, nil)

	var calls atomic.Int32
	result, err := idem.Execute(ctx, userID, "POST capacity-query-failure", uuid.New(),
		sha256.Sum256([]byte("capacity query failure")),
		func(*store.Queries) (resume.StoredResponse, error) {
			calls.Add(1)
			return resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"no":true}`)}, nil
		})
	if !errors.Is(err, injected) {
		t.Fatalf("Execute() error = %v, want wrapped injected query error", err)
	}
	if errors.Is(err, resume.ErrIdempotencyCapacity) {
		t.Errorf("Execute() error = %v, must not fabricate ErrIdempotencyCapacity", err)
	}
	if result.Outcome != resume.CommitDefinitelyRolledBack || result.Replayed || calls.Load() != 0 {
		t.Errorf("result=%+v calls=%d, want definite rollback, no replay, no callback", result, calls.Load())
	}
}

func TestTryReserveIdempotencyUsage_ConcurrentContendersCannotOvershoot(t *testing.T) {
	_, _, q, pool, ctx := newIntegrationIdempotencyStore(t, testutil.NewClockAtEpoch().Now)
	userID := createTestUser(t, q)
	setIdempotencyUsage(ctx, t, pool, userID, 49_999, 0)

	type result struct{ err error }
	results := make(chan result, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			_, err := q.TryReserveIdempotencyUsage(ctx, store.TryReserveIdempotencyUsageParams{
				UserID: userID, RecordBytes: 1, MaxRecords: 50_000, MaxBytes: 1 << 30,
			})
			results <- result{err: err}
		}()
	}
	close(start)
	successes, rejected := 0, 0
	for range 2 {
		got := <-results
		switch {
		case got.err == nil:
			successes++
		case errors.Is(got.err, pgx.ErrNoRows):
			rejected++
		default:
			t.Fatalf("TryReserveIdempotencyUsage() error: %v", got.err)
		}
	}
	if successes != 1 || rejected != 1 {
		t.Errorf("concurrent reservations = %d successes/%d rejected, want 1/1", successes, rejected)
	}
	var records int64
	if err := pool.QueryRow(ctx, `SELECT retained_records FROM idempotency_usage WHERE user_id = $1`, userID).Scan(&records); err != nil {
		t.Fatalf("read retained count after concurrent reserve: %v", err)
	}
	if records != 50_000 {
		t.Errorf("retained count after concurrent reserve = %d, want 50,000", records)
	}
}

func TestTryReserveIdempotencyUsage_ConcurrentByteContendersCannotOvershoot(t *testing.T) {
	_, _, q, pool, ctx := newIntegrationIdempotencyStore(t, testutil.NewClockAtEpoch().Now)
	userID := createTestUser(t, q)
	setIdempotencyUsage(ctx, t, pool, userID, 0, (1<<30)-1)

	type result struct{ err error }
	results := make(chan result, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			_, err := q.TryReserveIdempotencyUsage(ctx, store.TryReserveIdempotencyUsageParams{
				UserID: userID, RecordBytes: 1, MaxRecords: 50_000, MaxBytes: 1 << 30,
			})
			results <- result{err: err}
		}()
	}
	close(start)
	successes, rejected := 0, 0
	for range 2 {
		got := <-results
		switch {
		case got.err == nil:
			successes++
		case errors.Is(got.err, pgx.ErrNoRows):
			rejected++
		default:
			t.Fatalf("TryReserveIdempotencyUsage() error: %v", got.err)
		}
	}
	if successes != 1 || rejected != 1 {
		t.Errorf("concurrent byte reservations = %d successes/%d rejected, want 1/1", successes, rejected)
	}
	var records, storedBytes int64
	if err := pool.QueryRow(ctx, `
		SELECT retained_records, stored_bytes
		FROM idempotency_usage WHERE user_id = $1`, userID).Scan(&records, &storedBytes); err != nil {
		t.Fatalf("read usage after concurrent byte reserve: %v", err)
	}
	if records != 1 || storedBytes != 1<<30 {
		t.Errorf("usage after concurrent byte reserve = (%d records, %d bytes), want (1 record, %d bytes)",
			records, storedBytes, int64(1<<30))
	}
}

func TestIdempotencyStore_Execute_CallbackRollbackLeavesUsageUnchanged(t *testing.T) {
	idem, _, q, pool, ctx := newIntegrationIdempotencyStore(t, testutil.NewClockAtEpoch().Now)
	userID := createTestUser(t, q)
	injected := errors.New("callback failed")
	result, err := idem.Execute(ctx, userID, "POST callback-rollback", uuid.New(), sha256.Sum256([]byte("callback rollback")),
		func(*store.Queries) (resume.StoredResponse, error) { return resume.StoredResponse{}, injected })
	if !errors.Is(err, injected) || result.Outcome != resume.CommitDefinitelyRolledBack {
		t.Fatalf("Execute(callback failure) = (%+v, %v), want definite rollback and injected error", result, err)
	}
	var usageRows int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM idempotency_usage WHERE user_id = $1`, userID).Scan(&usageRows); err != nil {
		t.Fatalf("count usage after callback rollback: %v", err)
	}
	if usageRows != 0 {
		t.Errorf("usage rows after callback rollback = %d, want 0", usageRows)
	}
}

// --- P2B normalized replay identity and stored-header policy ---

func TestIdempotencyStore_Execute_FirstAndReplayAreByteIdentical(t *testing.T) {
	idem, _, q, _, ctx := newIntegrationIdempotencyStore(t, testutil.NewClockAtEpoch().Now)
	userID := createTestUser(t, q)
	key := uuid.New()
	hash := sha256.Sum256([]byte("identity"))
	input := resume.StoredResponse{
		Status: 201,
		Body:   json.RawMessage(`{"longer":1,"a":2}`),
		Headers: map[string]string{
			"Location":                "/api/v1/resumes/example",
			"ETag":                    `"1"`,
			"X-Resume-Schema-Version": "1",
		},
	}
	first, err := idem.Execute(ctx, userID, "POST identity", key, hash,
		func(*store.Queries) (resume.StoredResponse, error) { return input, nil })
	if err != nil {
		t.Fatalf("first Execute(): %v", err)
	}
	replay, err := idem.Execute(ctx, userID, "POST identity", key, hash,
		func(*store.Queries) (resume.StoredResponse, error) {
			return resume.StoredResponse{}, errors.New("replay callback ran")
		})
	if err != nil {
		t.Fatalf("replay Execute(): %v", err)
	}
	if first.Replayed || !replay.Replayed || first.Outcome != resume.CommitCommitted || replay.Outcome != resume.CommitCommitted {
		t.Errorf("first/replay flags = (%+v, %+v), want fresh committed then replayed committed", first, replay)
	}
	if first.Response.Status != replay.Response.Status || !bytes.Equal(first.Response.Body, replay.Response.Body) ||
		!reflect.DeepEqual(first.Response.Headers, replay.Response.Headers) {
		t.Errorf("first/replay differ:\nfirst=%+v\nreplay=%+v", first.Response, replay.Response)
	}
	if bytes.Equal(first.Response.Body, input.Body) {
		t.Errorf("first body unexpectedly kept pre-storage bytes %s; test input must exercise jsonb normalization", input.Body)
	}
	row, err := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
		UserID: userID, Route: "POST identity", IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("read identity row: %v", err)
	}
	if !bytes.Equal(first.Response.Body, row.ResponseBody) {
		t.Errorf("first body = %s, stored body = %s", first.Response.Body, row.ResponseBody)
	}
}

func TestIdempotencyStore_Execute_BodylessSentinelNeverReachesCaller(t *testing.T) {
	idem, _, q, _, ctx := newIntegrationIdempotencyStore(t, testutil.NewClockAtEpoch().Now)
	userID := createTestUser(t, q)
	cases := []struct {
		operation string
		headers   map[string]string
	}{
		{operation: "DELETE resume", headers: nil},
		{operation: "DELETE entry", headers: map[string]string{
			"ETag": `"2"`, "X-Resume-Schema-Version": "1",
		}},
		{operation: "DELETE photo", headers: map[string]string{
			"ETag": `"2"`, "X-Resume-Schema-Version": "1",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.operation, func(t *testing.T) {
			key := uuid.New()
			hash := sha256.Sum256([]byte(tc.operation))
			input := resume.StoredResponse{Status: 204, Headers: tc.headers}
			first, err := idem.Execute(ctx, userID, tc.operation, key, hash,
				func(*store.Queries) (resume.StoredResponse, error) { return input, nil })
			if err != nil {
				t.Fatalf("first Execute(): %v", err)
			}
			replay, err := idem.Execute(ctx, userID, tc.operation, key, hash,
				func(*store.Queries) (resume.StoredResponse, error) {
					return resume.StoredResponse{}, errors.New("replay callback ran")
				})
			if err != nil {
				t.Fatalf("replay Execute(): %v", err)
			}
			if len(first.Response.Body) != 0 || len(replay.Response.Body) != 0 {
				t.Errorf("bodyless first/replay lengths = %d/%d, want 0/0", len(first.Response.Body), len(replay.Response.Body))
			}
			if !reflect.DeepEqual(first.Response.Headers, tc.headers) || !reflect.DeepEqual(replay.Response.Headers, tc.headers) {
				t.Errorf("bodyless first/replay headers = %#v / %#v, want exact %#v", first.Response.Headers, replay.Response.Headers, tc.headers)
			}
			row, err := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
				UserID: userID, Route: tc.operation, IdempotencyKey: key,
			})
			if err != nil {
				t.Fatalf("read bodyless row: %v", err)
			}
			if !bytes.Equal(bytes.TrimSpace(row.ResponseBody), []byte("null")) {
				t.Errorf("stored bodyless sentinel = %s, want null", row.ResponseBody)
			}
		})
	}
}

func TestIdempotencyStore_Execute_RejectsUnapprovedStoredHeader(t *testing.T) {
	idem, _, q, _, ctx := newIntegrationIdempotencyStore(t, testutil.NewClockAtEpoch().Now)
	userID := createTestUser(t, q)
	key := uuid.New()
	result, err := idem.Execute(ctx, userID, "POST bad-header", key, sha256.Sum256([]byte("bad header")),
		func(*store.Queries) (resume.StoredResponse, error) {
			return resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"ok":true}`), Headers: map[string]string{"Date": "never persist"}}, nil
		})
	if err == nil || result.Outcome != resume.CommitDefinitelyRolledBack {
		t.Fatalf("Execute(unapproved header) = (%+v, %v), want definite rollback error", result, err)
	}
	assertIdempotencyRecordAbsent(ctx, t, q, userID, "POST bad-header", key)
}

func TestIdempotencyStore_Inspect_FailsClosedOnCorruptStoredHeader(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	idem, _, q, pool, ctx := newIntegrationIdempotencyStore(t, clock.Now)
	userID := createTestUser(t, q)
	key := uuid.New()
	hash := sha256.Sum256([]byte("corrupt header"))
	inserted, err := q.CreateIdempotencyRecord(ctx, store.CreateIdempotencyRecordParams{
		UserID: userID, Route: "POST corrupt-header", IdempotencyKey: key,
		RequestHash: hash[:], ResponseStatus: 200,
		ResponseBody: json.RawMessage(`{"ok":true}`), ResponseHeaders: json.RawMessage(`{"Date":"stale"}`),
		ExpiresAt: clock.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("seed corrupt stored header: %v", err)
	}
	setIdempotencyUsage(ctx, t, pool, userID, 1, int64(inserted.StoredBytes))
	if got, replayed, err := idem.Inspect(ctx, userID, "POST corrupt-header", key, hash); err == nil || replayed || got.Status != 0 {
		t.Errorf("Inspect(corrupt stored header) = (%+v, %t, %v), want zero, false, error", got, replayed, err)
	}
}
