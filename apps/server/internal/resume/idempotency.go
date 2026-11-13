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

	"danny.vn/aboutme/apps/server/internal/store"
)

const IdempotencyTTL = 24 * time.Hour

const (
	idempotencyCleanupBatch       = 200
	maxRetainedIdempotencyRecords = 50_000
	maxRetainedIdempotencyBytes   = 1 << 30
)

const (
	storedHeaderLocation      = "Location"
	storedHeaderETag          = "ETag"
	storedHeaderSchemaVersion = "X-Resume-Schema-Version"
)

const (
	idempotencyRecordUniqueViolationCode = "23505"
	idempotencyRecordUniqueConstraint    = "idempotency_records_user_route_key_key"
)

type StoredResponse struct {
	Status  int
	Body    json.RawMessage
	Headers map[string]string
}

type CommitOutcome uint8

const (
	CommitNotAttempted CommitOutcome = iota
	CommitDefinitelyRolledBack
	CommitCommitted
	CommitUnknown
)

type ExecuteResult struct {
	Response StoredResponse
	Replayed bool
	Outcome  CommitOutcome
}

type RecheckDecision uint8

const (
	RecheckFresh RecheckDecision = iota
	RecheckReplay
	RecheckReuse
)

type RecheckResult struct {
	Decision RecheckDecision
	Response StoredResponse
}

var ErrIdempotencyKeyReuse = errors.New(
	"resume: idempotency key reused with a different request body")

var ErrIdempotencyCapacity = errors.New("resume: idempotency capacity exceeded")

type IdempotencyCapacityError struct {
	RetryAfterSeconds int64
}

func (e *IdempotencyCapacityError) Error() string {
	return fmt.Sprintf("resume: idempotency capacity exceeded (retry after %ds)", e.RetryAfterSeconds)
}

func (e *IdempotencyCapacityError) Unwrap() error { return ErrIdempotencyCapacity }

type IdempotencyStore struct {
	pool *store.Pool
	q    *store.Queries
	now  func() time.Time

	beginTx  func(ctx context.Context) (pgx.Tx, error)
	commitTx func(ctx context.Context, tx pgx.Tx) error

	afterRecheckLock func()
}

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
