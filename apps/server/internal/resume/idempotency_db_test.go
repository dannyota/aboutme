package resume_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// A wrong numeric ordering would make a transition consumer interpret a
// producer decision as a different action.
func TestIdempotencyRecheck_InterfaceAndDecisions(t *testing.T) {
	if resume.RecheckFresh != 0 || resume.RecheckReplay != 1 || resume.RecheckReuse != 2 {
		t.Fatalf("RecheckDecision values = (%d, %d, %d), want (0, 1, 2)",
			resume.RecheckFresh, resume.RecheckReplay, resume.RecheckReuse)
	}

	var _ interface {
		Recheck(context.Context, uuid.UUID, string, uuid.UUID, [32]byte) (resume.RecheckResult, error)
		Execute(context.Context, uuid.UUID, string, uuid.UUID, [32]byte, func(*store.Queries) (resume.StoredResponse, error)) (resume.ExecuteResult, error)
	} = (*resume.IdempotencyStore)(nil)
}

// Recheck must keep the owner lock only through its lookup and commit, then
// release it; retaining it would serialize a later mutation beyond the probe.
func TestIdempotencyRecheck_UserLockBoundsReadOnlyTransaction(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	_, _, q, pool, ctx := newIntegrationIdempotencyStore(t, clock.Now)
	userID := createTestUser(t, q)
	locked := make(chan struct{})
	release := make(chan struct{})
	idem := resume.NewIdempotencyStoreWithRecheckBlockerForTest(pool, clock.Now, func() {
		close(locked)
		<-release
	})
	type outcome struct {
		result resume.RecheckResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := idem.Recheck(ctx, userID, idempotencyTestRoute, uuid.New(), sha256.Sum256([]byte("lock boundary")))
		done <- outcome{result: result, err: err}
	}()
	<-locked

	held, err := userRowLockedForResumeWrite(ctx, pool, userID)
	if err != nil || !held {
		t.Fatalf("user lock during Recheck = (%t, %v), want true, nil", held, err)
	}
	close(release)
	got := <-done
	if got.err != nil || got.result.Decision != resume.RecheckFresh {
		t.Fatalf("Recheck after release = (%+v, %v), want fresh", got.result, got.err)
	}
	held, err = userRowLockedForResumeWrite(ctx, pool, userID)
	if err != nil || held {
		t.Fatalf("user lock after Recheck = (%t, %v), want false, nil", held, err)
	}
	assertRecheckNoWrite(t, ctx, pool, userID, 0, 0)
}

// Removing the retained-record decision or treating expiry as reuse would
// make a transition close unnecessarily or reject a fresh mutation.
func TestIdempotencyRecheck_DecidesWithoutWritesOrUsageMutation(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	idem, _, q, pool, ctx := newIntegrationIdempotencyStore(t, clock.Now)
	userID := createTestUser(t, q)
	key := uuid.New()
	hash := sha256.Sum256([]byte("recheck request"))

	assertRecheck := func(name string, want resume.RecheckDecision, wantBody []byte, got resume.RecheckResult, err error) {
		t.Helper()
		if err != nil || got.Decision != want {
			t.Fatalf("Recheck(%s) = (%+v, %v), want decision %d", name, got, err, want)
		}
		if !bytes.Equal(got.Response.Body, wantBody) {
			t.Errorf("Recheck(%s) body = %s, want %s", name, got.Response.Body, wantBody)
		}
	}

	result, err := idem.Recheck(ctx, userID, idempotencyTestRoute, key, hash)
	assertRecheck("absent", resume.RecheckFresh, nil, result, err)
	assertRecheckNoWrite(t, ctx, pool, userID, 0, 0)

	seed := resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"seeded":true}`)}
	committed, err := idem.Execute(ctx, userID, idempotencyTestRoute, key, hash,
		func(*store.Queries) (resume.StoredResponse, error) { return seed, nil })
	if err != nil {
		t.Fatalf("seed Execute: %v", err)
	}
	assertRecheckNoWrite(t, ctx, pool, userID, 1, 1)

	result, err = idem.Recheck(ctx, userID, idempotencyTestRoute, key, hash)
	assertRecheck("same hash", resume.RecheckReplay, committed.Response.Body, result, err)
	assertRecheckNoWrite(t, ctx, pool, userID, 1, 1)

	otherHash := sha256.Sum256([]byte("recheck changed request"))
	result, err = idem.Recheck(ctx, userID, idempotencyTestRoute, key, otherHash)
	assertRecheck("different hash", resume.RecheckReuse, nil, result, err)
	assertRecheckNoWrite(t, ctx, pool, userID, 1, 1)

	clock.Advance(resume.IdempotencyTTL + time.Second)
	result, err = idem.Recheck(ctx, userID, idempotencyTestRoute, key, hash)
	assertRecheck("expired", resume.RecheckFresh, nil, result, err)
	assertRecheckNoWrite(t, ctx, pool, userID, 1, 1)
}

// A transition's optimistic probe is not its final authority: a committed
// contender between probe and Execute must replay without invoking mutate.
func TestSameKeyContenderBetweenRecheckAndExecuteReplaysWithoutCallback(t *testing.T) {
	idem, _, q, _, ctx := newIntegrationIdempotencyStore(t, testutil.NewClockAtEpoch().Now)
	userID := createTestUser(t, q)
	key := uuid.New()
	hash := sha256.Sum256([]byte("same key contender"))

	preflight, err := idem.Recheck(ctx, userID, idempotencyTestRoute, key, hash)
	if err != nil || preflight.Decision != resume.RecheckFresh {
		t.Fatalf("preflight Recheck = (%+v, %v), want fresh", preflight, err)
	}
	winner, err := idem.Execute(ctx, userID, idempotencyTestRoute, key, hash,
		func(*store.Queries) (resume.StoredResponse, error) {
			return resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"winner":true}`)}, nil
		})
	if err != nil || winner.Replayed || winner.Outcome != resume.CommitCommitted {
		t.Fatalf("winner Execute = (%+v, %v), want fresh committed", winner, err)
	}

	var calls atomic.Int32
	result, err := idem.Execute(ctx, userID, idempotencyTestRoute, key, hash,
		func(*store.Queries) (resume.StoredResponse, error) {
			calls.Add(1)
			return resume.StoredResponse{}, errors.New("contender callback ran")
		})
	if err != nil || !result.Replayed || result.Outcome != resume.CommitCommitted || calls.Load() != 0 {
		t.Fatalf("contender Execute = (%+v, %v), calls=%d; want replayed committed, nil, 0", result, err, calls.Load())
	}
	if !bytes.Equal(result.Response.Body, winner.Response.Body) {
		t.Errorf("contender replay body = %s, want winner bytes %s", result.Response.Body, winner.Response.Body)
	}
}

// Removing Execute's final decision would let a key reuse between transition
// preflight and the callback run a second mutation.
func TestExecuteFinalRecheck_ReplayAndReuseSkipCallback(t *testing.T) {
	idem, _, q, _, ctx := newIntegrationIdempotencyStore(t, testutil.NewClockAtEpoch().Now)
	userID := createTestUser(t, q)
	key := uuid.New()
	hash := sha256.Sum256([]byte("final recheck"))
	seed, err := idem.Execute(ctx, userID, idempotencyTestRoute, key, hash,
		func(*store.Queries) (resume.StoredResponse, error) {
			return resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"seed":true}`)}, nil
		})
	if err != nil || seed.Replayed || seed.Outcome != resume.CommitCommitted {
		t.Fatalf("seed Execute = (%+v, %v), want fresh committed", seed, err)
	}

	var replayCalls atomic.Int32
	replay, err := idem.Execute(ctx, userID, idempotencyTestRoute, key, hash,
		func(*store.Queries) (resume.StoredResponse, error) {
			replayCalls.Add(1)
			return resume.StoredResponse{}, errors.New("replay callback ran")
		})
	if err != nil || !replay.Replayed || replay.Outcome != resume.CommitCommitted || replayCalls.Load() != 0 {
		t.Fatalf("replay Execute = (%+v, %v), calls=%d; want replayed committed, nil, 0", replay, err, replayCalls.Load())
	}

	var reuseCalls atomic.Int32
	_, err = idem.Execute(ctx, userID, idempotencyTestRoute, key, sha256.Sum256([]byte("reused key")),
		func(*store.Queries) (resume.StoredResponse, error) {
			reuseCalls.Add(1)
			return resume.StoredResponse{}, errors.New("reuse callback ran")
		})
	if !errors.Is(err, resume.ErrIdempotencyKeyReuse) || reuseCalls.Load() != 0 {
		t.Fatalf("reuse Execute = (%v), calls=%d; want ErrIdempotencyKeyReuse, 0", err, reuseCalls.Load())
	}
}

// Canceling the request after PostgreSQL has accepted COMMIT cannot prove that
// the mutation rolled back; callers must retain any external candidate.
func TestCommitOutcome_CancellationAfterCommitBeginsIsUnknown(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	_, _, q, pool, ctx := newIntegrationIdempotencyStore(t, clock.Now)
	userID := createTestUser(t, q)
	key := uuid.New()
	hash := sha256.Sum256([]byte("canceled after commit"))
	commitContext, cancel := context.WithCancel(ctx)
	defer cancel()
	idem := resume.NewIdempotencyStoreWithHooksForTest(pool, clock.Now, nil,
		func(_ context.Context, tx pgx.Tx) error {
			// Commit without the request context so this test can cancel the
			// request exactly after the server has accepted the transaction.
			// The returned cancellation is consequently an ambiguous outcome.
			if err := tx.Commit(context.WithoutCancel(commitContext)); err != nil {
				return err
			}
			cancel()
			return context.Canceled
		})
	result, err := idem.Execute(commitContext, userID, idempotencyTestRoute, key, hash,
		func(*store.Queries) (resume.StoredResponse, error) {
			return resume.StoredResponse{Status: 200, Body: json.RawMessage(`{"committed":true}`)}, nil
		})
	if !errors.Is(err, context.Canceled) || result.Outcome != resume.CommitUnknown || result.Replayed {
		t.Fatalf("Execute(canceled after commit) = (%+v, %v), want non-replayed CommitUnknown and context cancellation", result, err)
	}
	if _, err := q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
		UserID: userID, Route: idempotencyTestRoute, IdempotencyKey: key,
	}); err != nil {
		t.Errorf("committed record after canceled response: %v", err)
	}
}

func assertRecheckNoWrite(t *testing.T, ctx context.Context, pool *store.Pool, userID uuid.UUID, wantRecords, wantUsageRows int64) {
	t.Helper()
	var records, usageRows, retained int64
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM idempotency_records WHERE user_id = $1`, userID).Scan(&records); err != nil {
		t.Fatalf("count records: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*), COALESCE(max(retained_records), 0) FROM idempotency_usage WHERE user_id = $1`, userID).Scan(&usageRows, &retained); err != nil {
		t.Fatalf("read usage: %v", err)
	}
	if records != wantRecords || usageRows != wantUsageRows || retained != wantRecords {
		t.Errorf("after Recheck records/usageRows/retained = %d/%d/%d, want %d/%d/%d", records, usageRows, retained, wantRecords, wantUsageRows, wantRecords)
	}
}

// Recheck must release its user lock and surface cancellation or a read error;
// neither outcome may create a record or a usage row.
func TestIdempotencyRecheck_FailuresDoNotWrite(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	_, _, q, pool, ctx := newIntegrationIdempotencyStore(t, clock.Now)

	t.Run("canceled context", func(t *testing.T) {
		userID := createTestUser(t, q)
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		idem := resume.NewIdempotencyStoreForTest(pool, clock.Now)
		result, err := idem.Recheck(canceled, userID, idempotencyTestRoute, uuid.New(), sha256.Sum256([]byte("canceled")))
		if err == nil || result.Decision != resume.RecheckFresh {
			t.Fatalf("Recheck(canceled) = (%+v, %v), want fresh result plus error", result, err)
		}
		assertRecheckNoWrite(t, ctx, pool, userID, 0, 0)
	})

	t.Run("lock failure", func(t *testing.T) {
		userID := createTestUser(t, q)
		injected := errors.New("lock failed")
		idem := resume.NewIdempotencyStoreWithHooksForTest(pool, clock.Now,
			func(ctx context.Context) (pgx.Tx, error) {
				tx, err := pool.Begin(ctx)
				if err != nil {
					return nil, err
				}
				return &queryRowFailureTx{Tx: tx, queryFragment: "-- name: LockUserForResumeWrite", err: injected}, nil
			}, nil)
		_, err := idem.Recheck(ctx, userID, idempotencyTestRoute, uuid.New(), sha256.Sum256([]byte("lock")))
		if !errors.Is(err, injected) {
			t.Fatalf("Recheck(lock failure) error = %v, want injected", err)
		}
		assertRecheckNoWrite(t, ctx, pool, userID, 0, 0)
	})

	t.Run("lookup failure", func(t *testing.T) {
		userID := createTestUser(t, q)
		injected := errors.New("lookup failed")
		idem := resume.NewIdempotencyStoreWithHooksForTest(pool, clock.Now,
			func(ctx context.Context) (pgx.Tx, error) {
				tx, err := pool.Begin(ctx)
				if err != nil {
					return nil, err
				}
				return &queryRowFailureTx{Tx: tx, queryFragment: "-- name: GetIdempotencyRecord", err: injected}, nil
			}, nil)
		_, err := idem.Recheck(ctx, userID, idempotencyTestRoute, uuid.New(), sha256.Sum256([]byte("lookup")))
		if !errors.Is(err, injected) {
			t.Fatalf("Recheck(lookup failure) error = %v, want injected", err)
		}
		assertRecheckNoWrite(t, ctx, pool, userID, 0, 0)
	})

	t.Run("commit failure", func(t *testing.T) {
		userID := createTestUser(t, q)
		injected := errors.New("recheck commit failed")
		idem := resume.NewIdempotencyStoreWithHooksForTest(pool, clock.Now, nil,
			func(context.Context, pgx.Tx) error { return injected })
		_, err := idem.Recheck(ctx, userID, idempotencyTestRoute, uuid.New(), sha256.Sum256([]byte("commit")))
		if !errors.Is(err, injected) {
			t.Fatalf("Recheck(commit failure) error = %v, want injected", err)
		}
		assertRecheckNoWrite(t, ctx, pool, userID, 0, 0)
	})

	t.Run("malformed stored response", func(t *testing.T) {
		userID := createTestUser(t, q)
		key := uuid.New()
		hash := sha256.Sum256([]byte("malformed response"))
		inserted, err := q.CreateIdempotencyRecord(ctx, store.CreateIdempotencyRecordParams{
			UserID: userID, Route: idempotencyTestRoute, IdempotencyKey: key,
			RequestHash: hash[:], ResponseStatus: 200,
			ResponseBody: json.RawMessage(`{"ok":true}`), ResponseHeaders: json.RawMessage(`{"Date":"invalid"}`),
			ExpiresAt: clock.Now().Add(time.Hour),
		})
		if err != nil {
			t.Fatalf("seed malformed record: %v", err)
		}
		setIdempotencyUsage(ctx, t, pool, userID, 1, int64(inserted.StoredBytes))
		_, err = resume.NewIdempotencyStoreForTest(pool, clock.Now).Recheck(ctx, userID, idempotencyTestRoute, key, hash)
		if err == nil {
			t.Fatal("Recheck(malformed stored response) error = nil, want error")
		}
		assertRecheckNoWrite(t, ctx, pool, userID, 1, 1)
	})
}
