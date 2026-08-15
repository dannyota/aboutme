package resume

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// IdempotencyTTL is how long an idempotency record remains valid after
// Execute writes it. Request-path cleanup (each Execute's own bounded
// preflight) enforces the bound for active users; the P8 scheduled sweep is
// the retention guarantee for inactive ones.
const IdempotencyTTL = 24 * time.Hour

// Retention bounds from docs/plans/budgets.md and ADR 0016. The cleanup
// batch bounds one request's cleanup work; the two caps bound an account's
// physically retained records and their stored response bytes (body plus
// approved headers, by the canonical octet_length expression).
const (
	idempotencyCleanupBatch       = 200
	maxRetainedIdempotencyRecords = 50_000
	maxRetainedIdempotencyBytes   = 1 << 30 // 1 GiB
)

// The only response headers a stored response may carry (ADR 0016):
// deterministic, replay-safe values. Request-scoped headers such as Date
// and X-Request-ID are never persisted; Execute fails closed on any header
// outside this set.
const (
	storedHeaderLocation      = "Location"
	storedHeaderETag          = "ETag"
	storedHeaderSchemaVersion = "X-Resume-Schema-Version"
)

// idempotencyRecordUniqueViolationCode and idempotencyRecordUniqueConstraint
// identify the unique violation raised when a conflicting record reaches the
// insert despite Execute's user-row serialization. Checking both, not only the
// code, distinguishes this defensive backstop from any other unique
// constraint violation the table might one day gain.
const (
	idempotencyRecordUniqueViolationCode = "23505"
	idempotencyRecordUniqueConstraint    = "idempotency_records_user_route_key_key"
)

// StoredResponse is the result of one mutate call and the value persisted for
// later replay.
//
// Body: a fresh execution and every replay both carry PostgreSQL's
// normalized jsonb bytes as returned by the record insert, so first and
// replay are byte-identical even when mutate's own literal used a different
// key order or whitespace. Status 204 is bodyless: mutate must return an
// empty Body, the record stores the internal jsonb null sentinel, and both
// the first response and every replay carry zero body bytes — the four
// bytes `null` never reach a caller.
//
// Headers may contain only the approved deterministic response headers
// (Location, ETag, X-Resume-Schema-Version). An empty or nil map stores the
// empty JSON object.
type StoredResponse struct {
	Status  int
	Body    json.RawMessage
	Headers map[string]string
}

// CommitOutcome classifies what Execute knows about its mutation
// transaction's commit. Media compensation may delete a request's candidate
// object after CommitNotAttempted or CommitDefinitelyRolledBack, and after
// any Replayed=true result (the stored response proves another execution
// owns the referenced object). A non-replayed CommitCommitted result is the
// winner, and every CommitUnknown result is retained.
type CommitOutcome uint8

const (
	// CommitNotAttempted means Execute failed before its mutation transaction
	// began (including a bounded-cleanup failure).
	CommitNotAttempted CommitOutcome = iota
	// CommitDefinitelyRolledBack means the mutation transaction began and
	// definitely did not commit — callback failure, a rejected decision
	// (key reuse, capacity), or a commit the server itself rejected.
	CommitDefinitelyRolledBack
	// CommitCommitted means the mutation transaction committed, or the result
	// is a replay of an already committed record.
	CommitCommitted
	// CommitUnknown means connection loss or another indeterminate commit
	// result; the transaction may or may not have committed.
	CommitUnknown
)

// ExecuteResult is Execute's classified result, returned on every path
// including errors. Replayed=true only ever pairs with CommitCommitted.
type ExecuteResult struct {
	Response StoredResponse
	Replayed bool
	Outcome  CommitOutcome
}

// RecheckDecision is the read-only idempotency decision a transition makes
// after it owns its fence but before it closes public admission. Its values
// are a closed producer contract for resumeapi: fresh can proceed to CAS,
// replay returns Response, and reuse maps to the normal idempotency conflict.
type RecheckDecision uint8

const (
	// RecheckFresh permits the caller to continue with its normal mutation.
	RecheckFresh RecheckDecision = iota
	// RecheckReplay returns the response stored for the matching request.
	RecheckReplay
	// RecheckReuse rejects a key whose request fingerprint differs.
	RecheckReuse
)

// RecheckResult is the outcome of a read-only serialized idempotency probe.
// Response is populated only for RecheckReplay.
type RecheckResult struct {
	Decision RecheckDecision
	Response StoredResponse
}

// ErrIdempotencyKeyReuse is returned by Execute and Inspect when key has
// already been used, by this user for this operation, with a different
// request hash: the caller reused an Idempotency-Key for a logically
// different request.
var ErrIdempotencyKeyReuse = errors.New(
	"resume: idempotency key reused with a different request body")

// ErrIdempotencyCapacity is the sentinel for a new-key insert that would
// exceed the retained-record or stored-byte cap. Execute wraps it in
// *IdempotencyCapacityError, which carries the Retry-After value.
var ErrIdempotencyCapacity = errors.New("resume: idempotency capacity exceeded")

// IdempotencyCapacityError is the typed capacity rejection. The HTTP kernel
// maps it to 429 rate_limited with Retry-After: RetryAfterSeconds —
// one second while an expired retained row remains (the next mutation's
// bounded cleanup frees space), otherwise the rounded-up interval to the
// earliest retained expiry.
type IdempotencyCapacityError struct {
	RetryAfterSeconds int64
}

func (e *IdempotencyCapacityError) Error() string {
	return fmt.Sprintf("resume: idempotency capacity exceeded (retry after %ds)", e.RetryAfterSeconds)
}

// Unwrap makes errors.Is(err, ErrIdempotencyCapacity) hold.
func (e *IdempotencyCapacityError) Unwrap() error { return ErrIdempotencyCapacity }

// IdempotencyStore provides transactional idempotency with bounded
// retention. Execute serializes a user's contenders on the user-row lock
// that resume creation uses before it looks up the key or invokes mutate.
// See Execute's doc comment for the callback contract,
// docs/adr/0016-transactional-idempotency.md, and P2B decisions D15/D18.
type IdempotencyStore struct {
	pool *store.Pool
	q    *store.Queries
	now  func() time.Time

	// beginTx and commitTx are test seams for injecting begin/commit
	// failures (disconnect-at-commit classification). nil means the real
	// pool begin and tx commit.
	beginTx  func(ctx context.Context) (pgx.Tx, error)
	commitTx func(ctx context.Context, tx pgx.Tx) error

	// afterRecheckLock is test-only placement control. Production stores leave
	// it nil; it lets a live-DB test prove Recheck's user lock bounds its
	// read-only transaction without scheduling or sleeps.
	afterRecheckLock func()
}

// NewIdempotencyStore builds an IdempotencyStore backed by pool, using the
// real wall clock.
func NewIdempotencyStore(pool *store.Pool) *IdempotencyStore {
	return &IdempotencyStore{pool: pool, q: store.New(pool), now: time.Now}
}

func (s *IdempotencyStore) begin(ctx context.Context) (pgx.Tx, error) {
	if s.beginTx != nil {
		return s.beginTx(ctx)
	}
	return s.pool.Begin(ctx)
}

func (s *IdempotencyStore) commit(ctx context.Context, tx pgx.Tx) error {
	if s.commitTx != nil {
		return s.commitTx(ctx, tx)
	}
	return tx.Commit(ctx)
}

// Inspect returns an already committed replay or key-reuse decision without
// running a mutation and without reserving anything. An absent or expired
// record is fresh (false, nil error). Inspect is an optimization for
// external media preparation, not the concurrency authority: two fresh
// inspections may both miss, and Execute must still decide after the
// candidate object is written.
func (s *IdempotencyStore) Inspect(ctx context.Context, userID uuid.UUID,
	operation string, key uuid.UUID, requestHash [32]byte,
) (StoredResponse, bool, error) {
	row, err := s.q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
		UserID: userID, Route: operation, IdempotencyKey: key,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return StoredResponse{}, false, fmt.Errorf("resume: idempotency: inspect record: %w", err)
	}
	result, decisionErr := exactIdempotencyRecordDecision(row, err == nil, requestHash, s.now())
	if decisionErr != nil {
		return StoredResponse{}, false, decisionErr
	}
	if result.Decision == RecheckReuse {
		return StoredResponse{}, false, ErrIdempotencyKeyReuse
	}
	return result.Response, result.Decision == RecheckReplay, nil
}

// Recheck serializes one read-only idempotency decision with the normal user
// lock. It is the transition seam, not a mutation authority: it does not clean
// expired records, reserve usage, invoke a callback, or write a record.
// Execute remains the final post-fence decision and transaction owner.
func (s *IdempotencyStore) Recheck(ctx context.Context, userID uuid.UUID,
	operation string, key uuid.UUID, requestHash [32]byte,
) (RecheckResult, error) {
	tx, err := s.begin(ctx)
	if err != nil {
		return RecheckResult{}, fmt.Errorf("resume: idempotency: begin recheck transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				return
			}
		}
	}()
	qtx := s.q.WithTx(tx)

	if _, lockErr := qtx.LockUserForResumeWrite(ctx, userID); lockErr != nil {
		return RecheckResult{}, fmt.Errorf("resume: idempotency: recheck lock owner row: %w", lockErr)
	}
	if s.afterRecheckLock != nil {
		s.afterRecheckLock()
	}
	row, err := qtx.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
		UserID: userID, Route: operation, IdempotencyKey: key,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return RecheckResult{}, fmt.Errorf("resume: idempotency: recheck record: %w", err)
	}
	result, err := exactIdempotencyRecordDecision(row, err == nil, requestHash, s.now())
	if err != nil {
		return RecheckResult{}, err
	}
	if err := s.commit(ctx, tx); err != nil {
		return RecheckResult{}, fmt.Errorf("resume: idempotency: commit recheck transaction: %w", err)
	}
	committed = true
	return result, nil
}

// exactIdempotencyRecordDecision is the single retained-record decision shared
// by optimistic Inspect, serialized Recheck, and Execute's final decision.
// Expiry means fresh here; only Execute deletes expired records and releases
// their retained usage under its mutation transaction.
func exactIdempotencyRecordDecision(row store.IdempotencyRecord, found bool,
	requestHash [32]byte, now time.Time,
) (RecheckResult, error) {
	if !found || !row.ExpiresAt.After(now) {
		return RecheckResult{Decision: RecheckFresh}, nil
	}
	if !bytes.Equal(row.RequestHash, requestHash[:]) {
		return RecheckResult{Decision: RecheckReuse}, nil
	}
	response, err := storedResponseFromRecord(row.ResponseStatus, row.ResponseBody, row.ResponseHeaders)
	if err != nil {
		return RecheckResult{}, err
	}
	return RecheckResult{Decision: RecheckReplay, Response: response}, nil
}

// Execute ensures that only one transaction's database effects and response
// record commit per (userID, operation, key), with bounded retention. It
// runs two explicit transactions in the same user-first lock order:
//
//  1. Cleanup transaction: lock the user's row, delete at most
//     idempotencyCleanupBatch of the user's expired records —
//     deterministically oldest first by (expires_at, id) — release their
//     exact retained counters, and commit. A cleanup failure stops before
//     the mutation transaction (CommitNotAttempted). A later mutation
//     failure does not roll this committed cleanup back.
//  2. Mutation transaction: lock the same user's row, delete this exact key
//     if expired (releasing its counters), decide replay / key reuse /
//     capacity, invoke mutate, normalize the response through jsonb,
//     reserve usage within the 50,000-record / 1 GiB caps, insert the
//     record, and commit.
//
// mutate MUST perform every database write through the supplied qtx and
// MUST NOT perform non-transactional side effects (ADR 0016); external
// media effects follow ADR 0019's compensation rules using the returned
// CommitOutcome.
//
// Execute returns an ExecuteResult on every path. Failure before the
// mutation transaction begins is CommitNotAttempted; callback failure, a
// rejected decision, or a server-rejected commit is
// CommitDefinitelyRolledBack; a successful commit or replay is
// CommitCommitted; connection loss or another indeterminate commit result
// is CommitUnknown.
//
// The unique constraint and post-conflict re-read remain a final database
// backstop: if a conflicting record still wins the insert, the entire
// mutation transaction rolls back, and Execute replays (Replayed=true,
// CommitCommitted) or rejects from that committed record.
func (s *IdempotencyStore) Execute(ctx context.Context, userID uuid.UUID,
	operation string, key uuid.UUID, requestHash [32]byte,
	mutate func(qtx *store.Queries) (StoredResponse, error),
) (ExecuteResult, error) {
	now := s.now()

	if err := s.cleanupExpired(ctx, userID, now); err != nil {
		return ExecuteResult{Outcome: CommitNotAttempted}, err
	}

	tx, err := s.begin(ctx)
	if err != nil {
		return ExecuteResult{Outcome: CommitNotAttempted},
			fmt.Errorf("resume: idempotency: begin mutation transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			// WithoutCancel: the rollback must be attempted even when ctx
			// was canceled mid-flight. After a failed or ambiguous commit
			// this is a no-op on a closed transaction.
			if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				return
			}
		}
	}()
	qtx := s.q.WithTx(tx)

	rolledBack := func(err error) (ExecuteResult, error) {
		return ExecuteResult{Outcome: CommitDefinitelyRolledBack}, err
	}
	if timeoutErr := bindTransactionDeadline(ctx, tx); timeoutErr != nil {
		return rolledBack(timeoutErr)
	}

	if _, lockErr := qtx.LockUserForResumeWrite(ctx, userID); lockErr != nil {
		return rolledBack(fmt.Errorf("resume: idempotency: lock owner row: %w", lockErr))
	}

	del, delErr := qtx.DeleteExpiredIdempotencyRecordForKey(ctx, store.DeleteExpiredIdempotencyRecordForKeyParams{
		UserID: userID, Route: operation, IdempotencyKey: key, ExpiresAt: now,
	})
	if delErr != nil {
		return rolledBack(fmt.Errorf("resume: idempotency: delete expired record: %w", delErr))
	}
	if err := releaseUsage(ctx, qtx, userID, del.DeletedRecords, del.DeletedBytes); err != nil {
		return rolledBack(err)
	}

	existing, getErr := qtx.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
		UserID: userID, Route: operation, IdempotencyKey: key,
	})
	if getErr == nil {
		decision, decisionErr := exactIdempotencyRecordDecision(existing, true, requestHash, now)
		if decisionErr != nil {
			return rolledBack(decisionErr)
		}
		switch decision.Decision {
		case RecheckReuse:
			return rolledBack(ErrIdempotencyKeyReuse)
		case RecheckReplay:
			// The replayed record is already durable; this transaction wrote
			// nothing (a live record excludes the expired-key delete above), so
			// its own commit result cannot change the replay.
			if commitErr := s.commit(ctx, tx); commitErr == nil {
				committed = true
			}
			return ExecuteResult{Response: decision.Response, Replayed: true, Outcome: CommitCommitted}, nil
		case RecheckFresh:
			// Execute has already removed an expired exact key; retain this
			// branch as the helper's defensive full contract.
		}
	} else if !errors.Is(getErr, pgx.ErrNoRows) {
		return rolledBack(fmt.Errorf("resume: idempotency: check existing record: %w", getErr))
	}

	// Lock (creating if needed) the usage row and reject on the
	// record-count cap before running the callback at all: the mutation
	// never runs when no insert could be admitted.
	usage, usageErr := qtx.GetOrCreateIdempotencyUsageForUpdate(ctx, userID)
	if usageErr != nil {
		return rolledBack(fmt.Errorf("resume: idempotency: lock usage row: %w", usageErr))
	}
	if usage.RetainedRecords+1 > maxRetainedIdempotencyRecords {
		return rolledBack(s.capacityError(ctx, qtx, userID, now, del.DeletedRecords > 0))
	}

	stored, mutateErr := mutate(qtx)
	if mutateErr != nil {
		return rolledBack(mutateErr)
	}

	storageBody, bodyErr := bodyForStorage(stored)
	if bodyErr != nil {
		return rolledBack(bodyErr)
	}
	headersJSON, headersErr := encodeStoredHeaders(stored.Headers)
	if headersErr != nil {
		return rolledBack(headersErr)
	}

	norm, normErr := qtx.NormalizeIdempotencyResponse(ctx, store.NormalizeIdempotencyResponseParams{
		ResponseBody:    storageBody,
		ResponseHeaders: headersJSON,
	})
	if normErr != nil {
		return rolledBack(fmt.Errorf("resume: idempotency: normalize response: %w", normErr))
	}

	if _, reserveErr := qtx.TryReserveIdempotencyUsage(ctx, store.TryReserveIdempotencyUsageParams{
		UserID:      userID,
		RecordBytes: int64(norm.StoredBytes),
		MaxRecords:  maxRetainedIdempotencyRecords,
		MaxBytes:    maxRetainedIdempotencyBytes,
	}); reserveErr != nil {
		if errors.Is(reserveErr, pgx.ErrNoRows) {
			return rolledBack(s.capacityError(ctx, qtx, userID, now, del.DeletedRecords > 0))
		}
		return rolledBack(fmt.Errorf("resume: idempotency: reserve usage: %w", reserveErr))
	}

	ins, insErr := qtx.CreateIdempotencyRecord(ctx, store.CreateIdempotencyRecordParams{
		UserID:          userID,
		Route:           operation,
		IdempotencyKey:  key,
		RequestHash:     requestHash[:],
		ResponseStatus:  int32(stored.Status), //nolint:gosec // bodyForStorage bounds Status to 100..599.
		ResponseBody:    storageBody,
		ResponseHeaders: headersJSON,
		ExpiresAt:       now.Add(IdempotencyTTL),
	})
	if insErr != nil {
		if isIdempotencyKeyConflict(insErr) {
			// A conflicting record committed despite the user-row lock. Roll
			// this transaction back in full (including anything mutate wrote
			// through qtx), then decide replay vs. reuse from the committed
			// winner.
			if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				return rolledBack(fmt.Errorf("resume: idempotency: roll back insert conflict: %w", rollbackErr))
			}
			return s.afterInsertConflict(ctx, userID, operation, key, requestHash)
		}
		return rolledBack(fmt.Errorf("resume: idempotency: insert record: %w", insErr))
	}
	if int64(ins.StoredBytes) != int64(norm.StoredBytes) {
		// Defensive: the reservation and the insert use the one canonical
		// byte expression; a mismatch means the accounting would drift.
		return rolledBack(fmt.Errorf(
			"resume: idempotency: stored-byte accounting mismatch (reserved %d, inserted %d)",
			norm.StoredBytes, ins.StoredBytes))
	}

	resp, respErr := storedResponseFromRecord(int32(stored.Status), ins.ResponseBody, ins.ResponseHeaders) //nolint:gosec // bounded above
	if respErr != nil {
		return rolledBack(respErr)
	}

	if commitErr := s.commit(ctx, tx); commitErr != nil {
		var pgErr *pgconn.PgError
		if errors.As(commitErr, &pgErr) && commitOutcomeUnknownSQLState(pgErr.Code) {
			return ExecuteResult{Outcome: CommitUnknown},
				fmt.Errorf("resume: idempotency: commit outcome unknown: %w", commitErr)
		}
		if errors.Is(commitErr, pgx.ErrTxCommitRollback) || pgErr != nil {
			// The server processed the COMMIT and rejected it: the
			// transaction is definitely rolled back.
			return rolledBack(fmt.Errorf("resume: idempotency: commit rejected: %w", commitErr))
		}
		// Connection loss or another indeterminate result: the commit may
		// or may not have happened.
		return ExecuteResult{Outcome: CommitUnknown},
			fmt.Errorf("resume: idempotency: commit outcome unknown: %w", commitErr)
	}
	committed = true
	return ExecuteResult{Response: resp, Replayed: false, Outcome: CommitCommitted}, nil
}

func bindTransactionDeadline(ctx context.Context, tx pgx.Tx) error {
	deadline, ok := ctx.Deadline()
	if !ok {
		return nil
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return context.DeadlineExceeded
	}
	milliseconds := remaining.Milliseconds()
	if milliseconds < 1 {
		milliseconds = 1
	}
	timeout := fmt.Sprintf("%dms", milliseconds)
	if _, err := tx.Exec(ctx, `SELECT
		set_config('statement_timeout', $1, true),
		set_config('idle_in_transaction_session_timeout', $2, true)`, timeout, timeout); err != nil {
		return fmt.Errorf("resume: idempotency: bind transaction deadline: %w", err)
	}
	return nil
}

// cleanupExpired is Execute's first, separately committed transaction: the
// bounded, deterministic oldest-first cleanup of the calling user's expired
// records, with their exact counters released under the same user-first
// lock order the mutation transaction uses.
func (s *IdempotencyStore) cleanupExpired(ctx context.Context, userID uuid.UUID, now time.Time) error {
	tx, err := s.begin(ctx)
	if err != nil {
		return fmt.Errorf("resume: idempotency: begin cleanup transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				return
			}
		}
	}()
	qtx := s.q.WithTx(tx)

	if _, lockErr := qtx.LockUserForResumeWrite(ctx, userID); lockErr != nil {
		return fmt.Errorf("resume: idempotency: cleanup: lock owner row: %w", lockErr)
	}
	del, delErr := qtx.DeleteExpiredIdempotencyRecordsForUser(ctx, store.DeleteExpiredIdempotencyRecordsForUserParams{
		UserID:    userID,
		ExpiresAt: now,
		Limit:     idempotencyCleanupBatch,
	})
	if delErr != nil {
		return fmt.Errorf("resume: idempotency: cleanup: delete expired records: %w", delErr)
	}
	if err := releaseUsage(ctx, qtx, userID, del.DeletedRecords, del.DeletedBytes); err != nil {
		return err
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		return fmt.Errorf("resume: idempotency: cleanup: commit: %w", commitErr)
	}
	committed = true
	return nil
}

// releaseUsage releases exactly the counters of deletedRecords physically
// deleted rows. Zero deletions release nothing. A missing usage row while
// records were deleted is a broken accounting invariant and fails closed.
func releaseUsage(ctx context.Context, qtx *store.Queries, userID uuid.UUID, deletedRecords, deletedBytes int64) error {
	if deletedRecords == 0 {
		return nil
	}
	n, err := qtx.ReleaseIdempotencyUsage(ctx, store.ReleaseIdempotencyUsageParams{
		UserID:        userID,
		Records:       deletedRecords,
		ReleasedBytes: deletedBytes,
	})
	if err != nil {
		return fmt.Errorf("resume: idempotency: release usage counters: %w", err)
	}
	if n != 1 {
		return fmt.Errorf(
			"resume: idempotency: released counters for %d deleted records but no usage row exists", deletedRecords)
	}
	return nil
}

// capacityError builds the typed capacity rejection with its Retry-After
// value, read inside the caller's still-healthy transaction. An exact-key
// expiry deletion in this transaction will roll back with the capacity
// rejection, so that row remains expired retained backlog for the caller.
func (s *IdempotencyStore) capacityError(ctx context.Context, qtx *store.Queries,
	userID uuid.UUID, now time.Time, expiredExactKeyWillRollback bool,
) error {
	retryAfter := int64(1)
	if expiredExactKeyWillRollback {
		return &IdempotencyCapacityError{RetryAfterSeconds: retryAfter}
	}
	row, err := qtx.GetIdempotencyCapacityRetryAfter(ctx, store.GetIdempotencyCapacityRetryAfterParams{
		UserID: userID, Now: now,
	})
	if err != nil {
		return fmt.Errorf("resume: idempotency: read capacity retry-after: %w", err)
	}
	if !row.ExpiredBacklog {
		if until := row.EarliestExpiry.Sub(now); until > 0 {
			retryAfter = int64((until + time.Second - 1) / time.Second)
		}
	}
	// While expired backlog remains, retry in one second: the next
	// mutation's bounded cleanup frees space.
	return &IdempotencyCapacityError{RetryAfterSeconds: retryAfter}
}

// afterInsertConflict re-reads the committed winner after the defensive
// unique-violation backstop fired, outside the already rolled-back
// transaction.
func (s *IdempotencyStore) afterInsertConflict(ctx context.Context, userID uuid.UUID,
	operation string, key uuid.UUID, requestHash [32]byte,
) (ExecuteResult, error) {
	row, getErr := s.q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
		UserID: userID, Route: operation, IdempotencyKey: key,
	})
	if getErr != nil {
		return ExecuteResult{Outcome: CommitDefinitelyRolledBack},
			fmt.Errorf("resume: idempotency: re-read after conflict: %w", getErr)
	}
	if bytes.Equal(row.RequestHash, requestHash[:]) {
		resp, respErr := storedResponseFromRecord(row.ResponseStatus, row.ResponseBody, row.ResponseHeaders)
		if respErr != nil {
			return ExecuteResult{Outcome: CommitDefinitelyRolledBack}, respErr
		}
		return ExecuteResult{Response: resp, Replayed: true, Outcome: CommitCommitted}, nil
	}
	return ExecuteResult{Outcome: CommitDefinitelyRolledBack}, ErrIdempotencyKeyReuse
}

// bodyForStorage validates stored's status/body pairing and returns the
// jsonb value to persist: the body bytes for a non-204 response, or the
// internal jsonb null sentinel (exactly four bytes, counted as such by the
// capacity accounting) for bodyless 204 success.
func bodyForStorage(stored StoredResponse) (json.RawMessage, error) {
	if stored.Status < 100 || stored.Status > 599 {
		return nil, fmt.Errorf("resume: idempotency: invalid response status %d", stored.Status)
	}
	if stored.Status == 204 {
		if len(stored.Body) != 0 {
			return nil, fmt.Errorf("resume: idempotency: 204 response must have an empty body, got %d bytes", len(stored.Body))
		}
		return json.RawMessage(`null`), nil
	}
	if len(stored.Body) == 0 {
		return nil, fmt.Errorf("resume: idempotency: non-204 response requires a JSON body")
	}
	return stored.Body, nil
}

// storedResponseFromRecord converts a stored record's columns back into a
// StoredResponse, translating the 204 null-body sentinel to exactly zero
// body bytes.
func storedResponseFromRecord(status int32, body, headersJSON json.RawMessage) (StoredResponse, error) {
	headers, err := decodeStoredHeaders(headersJSON)
	if err != nil {
		return StoredResponse{}, err
	}
	resp := StoredResponse{Status: int(status), Headers: headers}
	if status == 204 {
		if !bytes.Equal(bytes.TrimSpace(body), []byte(`null`)) {
			return StoredResponse{}, fmt.Errorf("resume: idempotency: stored 204 record body is not the null sentinel")
		}
		return resp, nil // Body stays nil: zero bytes on the wire, always.
	}
	resp.Body = body
	return resp, nil
}

// encodeStoredHeaders validates the approved-header allowlist and encodes
// the map as the jsonb object to store. Empty and nil both encode as the
// empty object (two bytes, per the capacity accounting).
func encodeStoredHeaders(headers map[string]string) (json.RawMessage, error) {
	if len(headers) == 0 {
		return json.RawMessage(`{}`), nil
	}
	for name := range headers {
		if !isStoredHeaderName(name) {
			return nil, fmt.Errorf("resume: idempotency: header %q is not an approved stored response header", name)
		}
	}
	encoded, err := json.Marshal(headers)
	if err != nil {
		return nil, fmt.Errorf("resume: idempotency: encode stored headers: %w", err)
	}
	return encoded, nil
}

// decodeStoredHeaders parses a stored jsonb headers object back into a map;
// an empty object decodes to nil for symmetry with a response that never
// set headers.
func decodeStoredHeaders(headersJSON json.RawMessage) (map[string]string, error) {
	if len(headersJSON) == 0 {
		return nil, fmt.Errorf("resume: idempotency: stored headers value is empty")
	}
	var headers map[string]string
	if err := json.Unmarshal(headersJSON, &headers); err != nil {
		return nil, fmt.Errorf("resume: idempotency: decode stored headers: %w", err)
	}
	if len(headers) == 0 {
		return nil, nil
	}
	for name := range headers {
		if !isStoredHeaderName(name) {
			return nil, fmt.Errorf("resume: idempotency: stored header %q is not approved", name)
		}
	}
	return headers, nil
}

func isStoredHeaderName(name string) bool {
	switch name {
	case storedHeaderLocation, storedHeaderETag, storedHeaderSchemaVersion:
		return true
	default:
		return false
	}
}

// PostgreSQL names these SQLSTATEs as indeterminate outcomes. They remain
// unknown even though pgx can decode the server message as *pgconn.PgError.
func commitOutcomeUnknownSQLState(code string) bool {
	return code == "08007" || code == "40003"
}

// isIdempotencyKeyConflict reports whether err is exactly the unique-index
// violation on idempotency_records_user_route_key_key: the defensive signal
// that a record for this exact (userID, operation, key) committed between
// this call's presence check and insert despite its user-row serialization.
func isIdempotencyKeyConflict(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == idempotencyRecordUniqueViolationCode && pgErr.ConstraintName == idempotencyRecordUniqueConstraint
}
