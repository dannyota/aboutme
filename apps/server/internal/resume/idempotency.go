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

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// IdempotencyTTL is how long an idempotency record remains valid after
// Execute writes it. Request-path cleanup (each Execute's own bounded
// preflight) enforces the bound for active users; the hourly
// idempotency-expiry-sweep command is the retention guarantee for inactive
// ones (docs/design/operations.md).
const IdempotencyTTL = 24 * time.Hour

// Retention bounds from docs/design/budgets.md and ADR 0016. The cleanup
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
// See Execute's doc comment for the callback contract and
// docs/adr/0016-transactional-idempotency.md.
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
