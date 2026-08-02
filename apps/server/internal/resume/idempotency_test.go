// idempotency_test.go exercises resume.IdempotencyStore against a live
// Postgres database (task-7-brief.md, D11): first-run, replay, rejected
// key reuse, mutate-error rollback, expiry-as-absence (Step 1); the
// concurrent-duplicate rollback arm, proven by forcing the loser's own
// resume insert to disappear rather than assuming it (Step 2); and
// composition with resume.Store's real cap-checked createTx across
// multiple keys, including cap rejection surfacing from inside mutate
// (Step 2b). Every DB-backed test here goes through the same
// internal/testutil helper internal/auth, internal/user, internal/store,
// and resume's own store_test.go use, so it never depends on another
// package's test binary having applied migrations first. The true
// concurrent race (N callers, same key) is deliberately NOT tested here --
// it belongs to Task 9's independent blind adversarial suite.
package resume_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// idempotencyTestRoute is a placeholder route string -- P2B mints the real
// route names; IdempotencyStore treats it as an opaque part of the unique
// key, so any stable string exercises the same logic.
const idempotencyTestRoute = "PUT /api/v1/resumes/{id}"

// newIntegrationIdempotencyStore returns a resume.IdempotencyStore driven by
// now instead of the real wall clock (so expiry is proven by advancing an
// injected clock, never a real sleep), plus the resume.Store sharing the
// SAME connection pool -- Step 2/2b's tests need it so their mutate
// closures can call (*resume.Store).CreateTxForTest directly, proving
// Execute composes with Task 6's real cap-checked create core rather than
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

// --- Step 1: sequential semantics ---

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

	got, replayed, err := idem.Execute(ctx, userID, idempotencyTestRoute, key, hash, mutate)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if replayed {
		t.Errorf("Execute() replayed = true, want false (first use of a fresh key)")
	}
	if got.Status != wantResp.Status || !bytes.Equal(got.Body, wantResp.Body) {
		t.Errorf("Execute() = %+v, want %+v", got, wantResp)
	}
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

	first, _, err := idem.Execute(ctx, userID, idempotencyTestRoute, key, hash, mutate)
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	if first.Status != firstResp.Status || !bytes.Equal(first.Body, firstResp.Body) {
		t.Errorf("first Execute() = %+v, want %+v (a fresh run returns mutate's own value, untouched)", first, firstResp)
	}
	firstRow, err := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
		UserID: userID, Route: idempotencyTestRoute, IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("GetIdempotencyRecord() after first call error = %v", err)
	}

	second, replayed, err := idem.Execute(ctx, userID, idempotencyTestRoute, key, hash, mutate)
	if err != nil {
		t.Fatalf("second Execute() error = %v", err)
	}
	if !replayed {
		t.Errorf("second Execute() replayed = false, want true")
	}
	// "byte-identical" means identical to what is actually STORED -- not to
	// the pre-storage Go literal the first call returned directly without
	// a DB round trip (jsonb re-serializes on write, e.g. adding a space
	// after ":"). second's bytes must match firstRow's stored bytes
	// exactly: both are reads of the exact same persisted row.
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

	if _, _, err := idem.Execute(ctx, userID, idempotencyTestRoute, key, firstHash,
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
	_, replayed, err := idem.Execute(ctx, userID, idempotencyTestRoute, key, otherHash,
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

func TestIdempotencyStore_Execute_MutateError_RollsBackWriteAndPersistsNothing(t *testing.T) {
	t.Parallel()
	idem, rs, q, _, ctx := newIntegrationIdempotencyStore(t, testutil.NewClockAtEpoch().Now)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	key := uuid.New()
	hash := sha256.Sum256([]byte("body that fails"))
	errMutateFailed := errors.New("test: mutate failed after writing")

	var writtenID uuid.UUID
	_, replayed, err := idem.Execute(ctx, userID, idempotencyTestRoute, key, hash,
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

func TestIdempotencyStore_Execute_ExpiredRecord_TreatedAsFreshAndReplaced(t *testing.T) {
	t.Parallel()
	clock := testutil.NewClockAtEpoch()
	idem, _, q, _, ctx := newIntegrationIdempotencyStore(t, clock.Now)
	userID := createTestUser(t, q)

	key := uuid.New()
	hash := sha256.Sum256([]byte("first request"))
	firstResp := resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"n":1}`)}

	if _, _, err := idem.Execute(ctx, userID, idempotencyTestRoute, key, hash,
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
	got, replayed, err := idem.Execute(ctx, userID, idempotencyTestRoute, key, hash,
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
	if got.Status != secondResp.Status || !bytes.Equal(got.Body, secondResp.Body) {
		t.Errorf("Execute() past TTL = %+v, want the NEW response %+v", got, secondResp)
	}

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
}

// --- Step 2: rollback arm, forced deterministically ---

// TestIdempotencyStore_Execute_ConflictRollsBackMutateAndReplaysWinner forces
// the D11 unique-violation rollback arm WITHOUT true concurrency (no
// goroutines -- the genuine concurrent race is Task 9's): mutate itself, in
// the middle of Execute's still-open transaction, uses a SEPARATE
// connection from the same pool to commit a competing idempotency record
// for the SAME (userID, route, key) before returning. When Execute's own
// transaction then tries to insert its own record, it hits the unique
// index, and the WHOLE transaction -- including mutate's own resume insert,
// performed through the supplied qtx -- rolls back. Execute must then
// re-read the committed (winning) record and replay it, never the loser's
// own in-memory response.
func TestIdempotencyStore_Execute_ConflictRollsBackMutateAndReplaysWinner(t *testing.T) {
	t.Parallel()
	idem, rs, q, pool, ctx := newIntegrationIdempotencyStore(t, testutil.NewClockAtEpoch().Now)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	key := uuid.New()
	hash := sha256.Sum256([]byte("shared request body"))
	winnerResp := resume.StoredResponse{Status: 201, Body: json.RawMessage(`{"winner":true}`)}
	loserResp := resume.StoredResponse{Status: 201, Body: json.RawMessage(`{"loser":true}`)}

	var loserID uuid.UUID
	calls := 0
	mutate := func(qtx *store.Queries) (resume.StoredResponse, error) {
		calls++

		// Simulate a competing Execute call that already committed, using a
		// SEPARATE connection from the same pool -- not qtx, and not a
		// goroutine: this statement runs to completion (and commits) before
		// control returns to Execute, deterministically. This must happen
		// BEFORE this mutate's own createTx call below: createTx takes
		// LockUserForResumeWrite (SELECT ... FOR UPDATE on the users row),
		// which this transaction would then hold for its own remaining
		// duration -- and idempotency_records.user_id is a foreign key to
		// that same row, so a competing insert issued AFTER the lock is
		// held would block on it forever (this is one single goroutine,
		// nothing will ever release it). Seeding first, before the lock is
		// taken, avoids that self-deadlock entirely.
		competing := store.New(pool)
		if err := competing.CreateIdempotencyRecord(ctx, store.CreateIdempotencyRecordParams{
			UserID:         userID,
			Route:          idempotencyTestRoute,
			IdempotencyKey: key,
			RequestHash:    hash[:],
			ResponseStatus: int32(winnerResp.Status),
			ResponseBody:   winnerResp.Body,
			ExpiresAt:      testutil.Epoch.Add(resume.IdempotencyTTL),
		}); err != nil {
			return resume.StoredResponse{}, fmt.Errorf("pre-seed winner record: %w", err)
		}

		created, err := rs.CreateTxForTest(ctx, qtx, userID, "Loser", doc)
		if err != nil {
			return resume.StoredResponse{}, fmt.Errorf("loser CreateTxForTest(): %w", err)
		}
		loserID = created.ID

		return loserResp, nil
	}

	got, replayed, err := idem.Execute(ctx, userID, idempotencyTestRoute, key, hash, mutate)
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil (should replay the winner)", err)
	}
	if !replayed {
		t.Errorf("Execute() replayed = false, want true")
	}
	if got.Status != winnerResp.Status {
		t.Errorf("Execute() Status = %d, want the WINNER's %d (not the loser's own)", got.Status, winnerResp.Status)
	}
	assertJSONEqual(t, got.Body, winnerResp.Body)
	if calls != 1 {
		t.Errorf("mutate called %d times, want exactly 1", calls)
	}

	// The loser's resume insert must be rolled back along with the rest of
	// its transaction: proven by re-reading, not assumed.
	if _, getErr := rs.Get(ctx, userID, loserID); !errors.Is(getErr, resume.ErrNotFound) {
		t.Errorf("Get(loserID) after conflict = %v, want resume.ErrNotFound (the loser's insert must not survive the rollback)", getErr)
	}
}

// --- Step 2b: composition across multiple keys, including cap rejection ---

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
		if _, replayed, err := idem.Execute(ctx, userID, idempotencyTestRoute, key, hash, mutate); err != nil {
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
	_, replayed, err := idem.Execute(ctx, userID, idempotencyTestRoute, fourthKey, fourthHash, mutate)
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
