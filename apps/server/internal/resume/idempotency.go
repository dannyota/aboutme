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
// Execute writes it. There is no scheduled reaping job: response_body holds
// user content, so Execute's own opportunistic reap of the calling user's
// expired rows (D11 owner ruling) is what actually enforces this bound, on
// the next request that user makes -- not a background sweep.
const IdempotencyTTL = 24 * time.Hour // D11; flagged for review

// idempotencyRecordUniqueViolationCode and idempotencyRecordUniqueConstraint
// are the exact SQLSTATE and constraint name
// idempotency_records_user_route_key_key (sql/schema.sql's UNIQUE (user_id,
// route, idempotency_key)) raises when a concurrent Execute call for the
// same (userID, route, key) commits its own record first. Checking both --
// not the code alone -- distinguishes this from any other unique-constraint
// violation the table might one day gain.
const (
	idempotencyRecordUniqueViolationCode = "23505"
	idempotencyRecordUniqueConstraint    = "idempotency_records_user_route_key_key"
)

// StoredResponse is the persisted result of one mutate call: the status and
// body Execute's caller returns to its own caller, whether this Execute
// call actually ran mutate or is replaying a previous call's committed
// result.
type StoredResponse struct {
	Status int
	Body   json.RawMessage
}

// ErrIdempotencyKeyReuse is returned by Execute when key has already been
// used, by this user on this route, for a request whose body hash does not
// match the one stored: the caller reused an Idempotency-Key for a
// logically different request. Execute refuses this outright rather than
// either silently replaying the wrong response or running mutate a second
// time.
var ErrIdempotencyKeyReuse = errors.New(
	"resume: idempotency key reused with a different request body")

// IdempotencyStore is the transactional idempotency-record primitive
// implementing D11: it runs a caller-supplied mutation exactly once per
// (userID, route, idempotencyKey), replaying the stored response on a
// repeat and rejecting a reused key carrying a different request body. See
// Execute's own doc comment for the full flow, and this package's own doc
// comment (codec.go) for the forward contract this gives P2B/P4's
// csrf_rejected retry.
type IdempotencyStore struct {
	pool *store.Pool
	q    *store.Queries
	now  func() time.Time
}

// NewIdempotencyStore builds an IdempotencyStore backed by pool, using the
// real wall clock.
func NewIdempotencyStore(pool *store.Pool) *IdempotencyStore {
	return &IdempotencyStore{pool: pool, q: store.New(pool), now: time.Now}
}

// Execute runs mutate exactly once per (userID, route, key). mutate MUST
// perform every write through the supplied qtx -- the rollback arm below
// depends on it; a mutate that wrote through the pool instead would survive
// a rollback that is supposed to undo it.
//
// Flow, all in one transaction (D11):
//
//  1. Reap this user's own expired records (opportunistic: response_body
//     holds user content, so this -- not a job that doesn't exist yet -- is
//     what enforces IdempotencyTTL).
//  2. Delete the same (route, key) row too, specifically, if it is expired.
//  3. Look up a live record for (userID, route, key). If one exists: its
//     hash equal to bodyHash means this is an ordinary replay (stored
//     response returned, mutate never invoked, nothing written); its hash
//     different means the key was reused for a different request
//     (ErrIdempotencyKeyReuse, mutate never invoked, nothing written). No
//     live record (never written, or just reaped above as expired) means
//     this is treated as a fresh request: fall through to mutate.
//  4. Run mutate, then insert its result as the new record.
//
// A genuinely concurrent duplicate call can still race between this
// transaction's own step 3 and its own step 4's insert -- exactly the
// window a second caller's insert can land in. That insert then hits the
// unique index, and the ENTIRE transaction (including whatever mutate
// itself wrote through qtx) rolls back. Execute detects that unique
// violation, re-reads the now-committed winning record outside the rolled
// back transaction, and either replays it (hash equal) or returns
// ErrIdempotencyKeyReuse (hash different) -- in both cases with nothing
// left over from the losing attempt. The true N-caller race is exercised
// elsewhere (Task 9); this package only guarantees the outcome is correct
// however the race resolves.
func (s *IdempotencyStore) Execute(ctx context.Context, userID uuid.UUID,
	route string, key uuid.UUID, bodyHash [32]byte,
	mutate func(qtx *store.Queries) (StoredResponse, error),
) (resp StoredResponse, replayed bool, err error) {
	now := s.now()

	txErr := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)

		if _, reapErr := qtx.DeleteExpiredIdempotencyRecordsForUser(ctx, store.DeleteExpiredIdempotencyRecordsForUserParams{
			UserID:    userID,
			ExpiresAt: now,
		}); reapErr != nil {
			return fmt.Errorf("resume: idempotency: reap expired records: %w", reapErr)
		}

		if _, delErr := qtx.DeleteIdempotencyRecordIfExpired(ctx, store.DeleteIdempotencyRecordIfExpiredParams{
			UserID:         userID,
			Route:          route,
			IdempotencyKey: key,
			ExpiresAt:      now,
		}); delErr != nil {
			return fmt.Errorf("resume: idempotency: delete expired record: %w", delErr)
		}

		existing, getErr := qtx.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
			UserID:         userID,
			Route:          route,
			IdempotencyKey: key,
		})
		switch {
		case getErr == nil:
			if !bytes.Equal(existing.RequestHash, bodyHash[:]) {
				return ErrIdempotencyKeyReuse
			}
			resp = StoredResponse{Status: int(existing.ResponseStatus), Body: existing.ResponseBody}
			replayed = true
			return nil
		case errors.Is(getErr, pgx.ErrNoRows):
			// No live record: fall through and run mutate.
		default:
			return fmt.Errorf("resume: idempotency: check existing record: %w", getErr)
		}

		stored, mutateErr := mutate(qtx)
		if mutateErr != nil {
			return mutateErr
		}

		if createErr := qtx.CreateIdempotencyRecord(ctx, store.CreateIdempotencyRecordParams{
			UserID:         userID,
			Route:          route,
			IdempotencyKey: key,
			RequestHash:    bodyHash[:],
			ResponseStatus: int32(stored.Status), //nolint:gosec // stored.Status is a caller-supplied HTTP status code (100-599); it never approaches int32's range.
			ResponseBody:   stored.Body,
			ExpiresAt:      now.Add(IdempotencyTTL),
		}); createErr != nil {
			return fmt.Errorf("resume: idempotency: insert record: %w", createErr)
		}

		resp = stored
		replayed = false
		return nil
	})

	if txErr != nil {
		if isIdempotencyKeyConflict(txErr) {
			// A concurrent duplicate committed first; the transaction above
			// has already rolled back in full, including anything mutate
			// wrote through qtx. Re-read the committed winner outside that
			// rolled-back transaction and decide replay vs. reuse from it.
			row, getErr := s.q.GetIdempotencyRecord(ctx, store.GetIdempotencyRecordParams{
				UserID:         userID,
				Route:          route,
				IdempotencyKey: key,
			})
			if getErr != nil {
				return StoredResponse{}, false, fmt.Errorf("resume: idempotency: re-read after conflict: %w", getErr)
			}
			if bytes.Equal(row.RequestHash, bodyHash[:]) {
				return StoredResponse{Status: int(row.ResponseStatus), Body: row.ResponseBody}, true, nil
			}
			return StoredResponse{}, false, ErrIdempotencyKeyReuse
		}
		return StoredResponse{}, false, txErr
	}

	return resp, replayed, nil
}

// isIdempotencyKeyConflict reports whether err is exactly the unique-index
// violation on idempotency_records_user_route_key_key: the signal that a
// concurrent Execute call already committed a record for this exact
// (userID, route, key) between this call's own presence check and its own
// insert attempt.
func isIdempotencyKeyConflict(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == idempotencyRecordUniqueViolationCode && pgErr.ConstraintName == idempotencyRecordUniqueConstraint
}
