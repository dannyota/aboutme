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

	if !leased.LeaseExpiresAt.After(now) {
		return nil
	}
	failAuthentication := func() error {
		w.logger.Warn("authmail: decrypt failed", "job", leased.ID)
		if terminalErr := w.markTerminal(ctx, qtx, leased.ID, now); terminalErr != nil {
			return terminalErr
		}
		return tx.Commit(ctx)
	}
	if !sameClaimedAuthority(job, leased) {
		return failAuthentication()
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

	payload, err := w.ring.Open(leased.ID, Kind(leased.Kind), scopeFromJob(leased), sealedFromJob(leased))
	if err != nil {
		return failAuthentication()
	}

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
		result.Outcome = SendTemporaryFailure
	}

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

func (w *Worker) markTerminal(ctx context.Context, qtx *store.Queries, jobID uuid.UUID, now time.Time) error {
	_, err := qtx.MarkAuthEmailJobTerminal(ctx, store.MarkAuthEmailJobTerminalParams{TerminalAt: now, ID: jobID})
	return err
}

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

type scopeState struct {
	tokenDigest []byte
}

func (s *scopeState) tokenMatches(jobDigest []byte) bool {
	if s.tokenDigest == nil {
		return true
	}
	return bytes.Equal(s.tokenDigest, jobDigest)
}

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

func sameClaimedAuthority(claimed, locked store.AuthEmailJob) bool {
	if claimed.Kind != locked.Kind || (claimed.TokenDigest == nil) != (locked.TokenDigest == nil) ||
		!bytes.Equal(claimed.TokenDigest, locked.TokenDigest) {
		return false
	}
	for _, ids := range [][2]*uuid.UUID{
		{claimed.UserID, locked.UserID},
		{claimed.RegistrationID, locked.RegistrationID},
		{claimed.ResetTokenID, locked.ResetTokenID},
	} {
		if ids[0] == nil || ids[1] == nil {
			if ids[0] != ids[1] {
				return false
			}
		} else if *ids[0] != *ids[1] {
			return false
		}
	}
	return true
}
