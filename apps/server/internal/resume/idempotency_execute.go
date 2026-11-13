package resume

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"danny.vn/aboutme/apps/server/internal/store"
)

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
			if commitErr := s.commit(ctx, tx); commitErr == nil {
				committed = true
			}
			return ExecuteResult{Response: decision.Response, Replayed: true, Outcome: CommitCommitted}, nil
		case RecheckFresh:
		}
	} else if !errors.Is(getErr, pgx.ErrNoRows) {
		return rolledBack(fmt.Errorf("resume: idempotency: check existing record: %w", getErr))
	}

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
			if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
				return rolledBack(fmt.Errorf("resume: idempotency: roll back insert conflict: %w", rollbackErr))
			}
			return s.afterInsertConflict(ctx, userID, operation, key, requestHash)
		}
		return rolledBack(fmt.Errorf("resume: idempotency: insert record: %w", insErr))
	}
	if int64(ins.StoredBytes) != int64(norm.StoredBytes) {
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
			return rolledBack(fmt.Errorf("resume: idempotency: commit rejected: %w", commitErr))
		}
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
	return &IdempotencyCapacityError{RetryAfterSeconds: retryAfter}
}

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
