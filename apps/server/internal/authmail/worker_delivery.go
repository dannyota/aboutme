package authmail

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/store"
)

// sendJob authorizes and delivers one claimed job. It opens one transaction,
// locks the scoped registration/reset token/user first and the exact leased job
// second, decrypts and confirms current authority and lease ownership, then
// holds both locks through the at-most-ten-second sender call and finalizes
// sent/terminal/requeue state before commit. A stale or unauthorized job
// becomes terminal without any send. Cancellation rolls the transaction back
// and leaves the job leased for the requeue query to recover.
func (w *Worker) sendJob(ctx context.Context, job store.AuthEmailJob) error {
	if w.hooks.beforeAuth != nil {
		w.hooks.beforeAuth(job.ID)
	}
	if Kind(job.Kind) == KindPrivacyNotice {
		return w.sendNoticeJob(ctx, job)
	}

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			w.logger.Warn("authmail: rollback transaction failed")
		}
	}()
	qtx := w.queries.WithTx(tx)
	now := w.clock()

	// Lock the scope row first (scope-before-job order). A missing scope
	// means the owning registration/reset token/user was replaced or deleted;
	// the job may have been deleted by cascade, so terminate it if it still
	// exists and otherwise commit a no-op.
	scope, err := w.lockScope(ctx, qtx, job)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if _, merr := qtx.MarkAuthEmailJobTerminal(ctx, store.MarkAuthEmailJobTerminalParams{
				TerminalAt: now,
				ID:         job.ID,
			}); merr != nil {
				return merr
			}
			return tx.Commit(ctx)
		}
		return err
	}

	// Re-lock the exact leased job by this worker. ErrNoRows means the lease was
	// reassigned to another worker or the job is gone: do not send and do not
	// finalize.
	leased, err := qtx.GetLeasedAuthEmailJobForUpdate(ctx, store.GetLeasedAuthEmailJobForUpdateParams{
		ID:         job.ID,
		LeaseOwner: w.id.String(),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}

	// Current authority: the lease must still be live, the job must not be
	// expired, and the stored token digest must match the scope row. An expired
	// or stale job becomes terminal, and that terminal state is committed here:
	// the deferred rollback must not undo it.
	if !leased.LeaseExpiresAt.After(now) {
		return nil
	}
	if !leased.ExpiresAt.After(now) {
		if terminalErr := w.markTerminal(ctx, qtx, leased.ID, now); terminalErr != nil {
			return terminalErr
		}
		return tx.Commit(ctx)
	}
	if scope != nil && !scope.tokenMatches(leased.TokenDigest) {
		if terminalErr := w.markTerminal(ctx, qtx, leased.ID, now); terminalErr != nil {
			return terminalErr
		}
		return tx.Commit(ctx)
	}

	// Authenticate the leased row's identity, scope, and token authority before
	// sending. A failed open commits terminal state without exposing plaintext.
	payload, err := w.ring.Open(leased.ID, Kind(leased.Kind), scopeFromJob(leased), sealedFromJob(leased))
	if err != nil {
		w.logger.Warn("authmail: decrypt failed", "job", leased.ID)
		if terminalErr := w.markTerminal(ctx, qtx, leased.ID, now); terminalErr != nil {
			return terminalErr
		}
		return tx.Commit(ctx)
	}

	// Send under a ten-second deadline while both locks are held.
	if w.hooks.beforeSend != nil {
		w.hooks.beforeSend(leased.ID)
	}
	sendCtx, cancel := context.WithTimeout(ctx, sendDeadline)
	result, sendErr := w.sender.Send(sendCtx, buildMessage(Kind(leased.Kind), payload, w.footerNote))
	cancel()
	if w.hooks.afterSend != nil {
		w.hooks.afterSend(leased.ID, result.Outcome)
	}
	if sendErr != nil {
		// An unexpected sender error is ambiguous and therefore temporary; a
		// duplicate delivery is harmless under token single use.
		result.Outcome = SendTemporaryFailure
	}

	// Finalize within the same transaction.
	switch result.Outcome {
	case SendAccepted:
		_, err = qtx.MarkAuthEmailJobSent(ctx, store.MarkAuthEmailJobSentParams{SentAt: now, ID: leased.ID})
	case SendPermanentFailure:
		err = w.markTerminal(ctx, qtx, leased.ID, now)
	case SendTemporaryFailure:
		err = w.requeueOrTerminal(ctx, qtx, leased, now)
	default:
		err = ErrWorker
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// markTerminal records the exact terminal state in the current transaction.
func (w *Worker) markTerminal(ctx context.Context, qtx *store.Queries, jobID uuid.UUID, now time.Time) error {
	_, err := qtx.MarkAuthEmailJobTerminal(ctx, store.MarkAuthEmailJobTerminalParams{TerminalAt: now, ID: jobID})
	return err
}

// requeueOrTerminal applies the temporary-failure policy: failed attempt 8
// or a next attempt at/after expiry marks terminal; otherwise the job returns
// to pending with a full-jitter backoff capped at min(30s*2^(attempt-1), 1h).
func (w *Worker) requeueOrTerminal(ctx context.Context, qtx *store.Queries, job store.AuthEmailJob, now time.Time) error {
	if job.Attempts >= maxAttempts {
		return w.markTerminal(ctx, qtx, job.ID, now)
	}
	next := now.Add(w.jitter(backoffCap(job.Attempts)))
	if !next.Before(job.ExpiresAt) {
		return w.markTerminal(ctx, qtx, job.ID, now)
	}
	_, err := qtx.RequeueAuthEmailJob(ctx, store.RequeueAuthEmailJobParams{NextAttemptAt: next, ID: job.ID})
	return err
}

// lockScope locks the single scope row named by the job's kind in the current
// transaction and returns the authority state (the scope row's token digest for
// verify/reset, nil for password_changed). A missing row surfaces as
// pgx.ErrNoRows.
func (w *Worker) lockScope(ctx context.Context, qtx *store.Queries, job store.AuthEmailJob) (*scopeState, error) {
	switch Kind(job.Kind) {
	case KindVerify:
		if job.RegistrationID == nil {
			return nil, ErrScope
		}
		reg, err := qtx.GetPasswordRegistrationForUpdate(ctx, *job.RegistrationID)
		if err != nil {
			return nil, err
		}
		return &scopeState{tokenDigest: reg.TokenDigest}, nil
	case KindReset:
		if job.ResetTokenID == nil {
			return nil, ErrScope
		}
		tok, err := qtx.GetPasswordResetTokenForUpdate(ctx, *job.ResetTokenID)
		if err != nil {
			return nil, err
		}
		return &scopeState{tokenDigest: tok.TokenDigest}, nil
	case KindPasswordChanged, KindSecondFactorEnabled, KindPasskeyAdded,
		KindPasskeyRemoved, KindSecondFactorDisabled, KindRecoveryCodesRegenerated,
		KindRecoveryCodeUsed, KindSecondFactorAttemptsExhausted,
		KindTOTPAdded, KindTOTPReplaced, KindTOTPRemoved:
		if job.UserID == nil {
			return nil, ErrScope
		}
		if _, err := qtx.GetUserForUpdate(ctx, *job.UserID); err != nil {
			return nil, err
		}
		return &scopeState{}, nil
	default:
		return nil, ErrInvalidKind
	}
}

// scopeState is the authority the send handoff checks against the job.
type scopeState struct {
	tokenDigest []byte
}

// tokenMatches reports whether the job's stored digest is still the scope
// row's current digest. User-scoped jobs have no token authority, so any digest
// (including nil) matches.
func (s *scopeState) tokenMatches(jobDigest []byte) bool {
	if s.tokenDigest == nil {
		return true
	}
	return bytes.Equal(s.tokenDigest, jobDigest)
}

// sealedFromJob converts the stored job columns into a Sealed blob.
func sealedFromJob(job store.AuthEmailJob) Sealed {
	var nonce [12]byte
	copy(nonce[:], job.Nonce)
	var keyID string
	if job.KeyID != nil {
		keyID = *job.KeyID
	}
	return Sealed{KeyID: keyID, Nonce: nonce, Ciphertext: job.Ciphertext}
}

func scopeFromJob(job store.AuthEmailJob) JobScope {
	return JobScope{UserID: job.UserID, RegistrationID: job.RegistrationID,
		ResetTokenID: job.ResetTokenID, TokenDigest: job.TokenDigest}
}
