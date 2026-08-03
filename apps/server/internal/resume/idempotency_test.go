// idempotency_test.go exercises resume.IdempotencyStore against a live
// Postgres database (task-7-brief.md, D11): first-run, replay, rejected
// key reuse, mutate-error rollback, expiry-as-absence (Step 1); deterministic
// same-key contention around the real saveDocumentTx CAS core (Step 2); and
// composition with resume.Store's real cap-checked createTx across multiple
// keys, including cap rejection surfacing from inside mutate (Step 2b). Every
// DB-backed test here goes through the same
// internal/testutil helper internal/auth, internal/user, internal/store,
// and resume's own store_test.go use, so it never depends on another
// package's test binary having applied migrations first. Task 9 retains the
// independent blind N-caller adversarial suite.
package resume_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
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

func createIdempotencyRecord(ctx context.Context, t *testing.T, q *store.Queries,
	userID uuid.UUID, route string, key uuid.UUID, hash [32]byte, expiresAt time.Time,
) {
	t.Helper()
	if err := q.CreateIdempotencyRecord(ctx, store.CreateIdempotencyRecordParams{
		UserID:         userID,
		Route:          route,
		IdempotencyKey: key,
		RequestHash:    hash[:],
		ResponseStatus: 200,
		ResponseBody:   json.RawMessage(`{"seeded":true}`),
		ExpiresAt:      expiresAt,
	}); err != nil {
		t.Fatalf("CreateIdempotencyRecord() error = %v", err)
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
	_, replayed, err := idem.Execute(ctx, userID, targetRoute, targetKey, otherHash,
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
	_, replayed, err := idem.Execute(ctx, userID, idempotencyTestRoute, failedKey, failedHash,
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

// TestIdempotencyStore_Execute_ConcurrentSaveDocumentConverges covers the
// regression where two same-key Execute calls both reached a real CAS mutation
// before either inserted its idempotency row. The loser could then surface
// RevisionMismatch from saveDocumentTx and never reach the unique insert whose
// conflict path was supposed to converge it on the winner. The channels below
// hold the winning transaction open after its CAS write and start the contender
// while that write is uncommitted; no sleeps or scheduler timing assumptions
// are involved.
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
			userID := createTestUser(t, q)
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
				resp, replayed, executeErr := winnerStore.Execute(ctx, userID, idempotencyTestRoute, key, winnerHash,
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
			// deterministic way for the test to distinguish the pre-fix path
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
				resp, replayed, executeErr := contenderStore.Execute(ctx, userID, idempotencyTestRoute, key, contenderHash,
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
				// Before serialization existed, waiting here forced the old CAS
				// loser path deterministically: it entered mutate and then blocked
				// on the winner's uncommitted resume-row update. With the required
				// owner lock, the contender cannot reach this callback at all.
				<-contenderEnteredMutate
			}
			close(releaseWinner)

			gotWinner := <-winnerResult
			gotContender := <-contenderResult
			if gotWinner.err != nil || gotWinner.replayed {
				t.Errorf("winner Execute() = {replayed:%v err:%v}, want {false nil}", gotWinner.replayed, gotWinner.err)
			}
			if gotWinner.resp.Status != ready.resp.Status || !bytes.Equal(gotWinner.resp.Body, ready.resp.Body) {
				t.Errorf("winner Execute() response = %+v, want callback response %+v", gotWinner.resp, ready.resp)
			}
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
