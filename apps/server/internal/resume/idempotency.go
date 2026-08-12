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
// expired rows enforces this bound on the user's next request.
const IdempotencyTTL = 24 * time.Hour

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
// later replay. A fresh execution returns mutate's Body bytes unchanged. A
// replay returns Postgres's persisted jsonb representation: it is the same JSON
// value and is byte-identical to the stored row, but jsonb normalization means
// it need not be byte-identical to mutate's original Body.
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

// IdempotencyStore provides transactional idempotency. Execute serializes a
// user's contenders on the user-row lock that resume creation uses before it
// looks up the key or invokes mutate. A waiting contender replays a committed
// same-key winner, or rejects a different request hash, without invoking its
// callback. See Execute's doc comment for the callback contract and
// docs/adr/0016-transactional-idempotency.md.
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

// Execute ensures that only one transaction's database effects and response
// record commit per (userID, route, key). Same-user calls serialize before the
// key lookup and mutate, using the users-row lock already shared with Create.
// Once a winner commits, a waiting same-key contender skips mutate and decides
// replay versus key reuse from the stored record.
//
// mutate MUST still perform every database write through the supplied qtx and
// MUST NOT perform non-transactional side effects. Transaction errors,
// connection loss around commit, and a caller retry can roll back or re-enter
// the operation even though healthy same-key contenders are serialized. A write
// through the pool, network call, file write, or other external effect would not
// share the idempotency transaction's commit/rollback outcome.
//
// Flow:
//
//  1. In one committed preflight statement, reap this user's own expired
//     records. response_body holds user content, so this opportunistic delete
//     enforces IdempotencyTTL even when the later replay/reuse decision or
//     mutate returns an error. A reap failure aborts Execute before mutate.
//  2. In the mutation transaction, lock the user's row FOR UPDATE before any
//     key lookup or callback. This matches Create's lock order and serializes
//     contenders before a stale CAS or other mutation can fail ahead of the
//     idempotency decision.
//  3. Defensively delete the same key if it is expired (covering a row inserted
//     after the preflight), then look up a live record for (userID, route, key).
//     A matching hash means this is an ordinary replay (stored response
//     returned, mutate never invoked, nothing written). A different hash
//     means the key was reused for a different request
//     (ErrIdempotencyKeyReuse, mutate never invoked, nothing written). No
//     live record (never written, or reaped above as expired) means
//     this is treated as a fresh request: fall through to mutate.
//  4. Run mutate, then insert its result as the new record.
//
// The unique constraint and post-conflict re-read remain a final database
// backstop: if a conflicting record still wins the insert, the entire mutation
// transaction rolls back before Execute replays or rejects from that committed
// record.
func (s *IdempotencyStore) Execute(ctx context.Context, userID uuid.UUID,
	route string, key uuid.UUID, bodyHash [32]byte,
	mutate func(qtx *store.Queries) (StoredResponse, error),
) (resp StoredResponse, replayed bool, err error) {
	now := s.now()

	if _, reapErr := s.q.DeleteExpiredIdempotencyRecordsForUser(ctx, store.DeleteExpiredIdempotencyRecordsForUserParams{
		UserID:    userID,
		ExpiresAt: now,
	}); reapErr != nil {
		return StoredResponse{}, false, fmt.Errorf("resume: idempotency: reap expired records: %w", reapErr)
	}

	txErr := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)

		if _, lockErr := qtx.LockUserForResumeWrite(ctx, userID); lockErr != nil {
			return fmt.Errorf("resume: idempotency: lock owner row: %w", lockErr)
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
			// A conflicting record committed despite the user-row lock; the
			// transaction above has already rolled back in full, including
			// anything mutate wrote through qtx. Re-read the committed winner
			// outside that rolled-back transaction and decide replay vs. reuse.
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
// violation on idempotency_records_user_route_key_key: the defensive signal
// that a record for this exact (userID, route, key) committed between this
// call's presence check and insert despite its user-row serialization.
func isIdempotencyKeyConflict(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == idempotencyRecordUniqueViolationCode && pgErr.ConstraintName == idempotencyRecordUniqueConstraint
}
