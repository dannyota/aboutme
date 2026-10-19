package authmail

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// NoticeAuthority reads host authority anew for each handoff. The worker never
// writes host receipts and treats a missing authority as disabled notices.
type NoticeAuthority interface {
	AuthorizeSend(context.Context, *store.Queries) bool
	ContentDigest() []byte
}

func (w *Worker) noticeAllowed(ctx context.Context, q *store.Queries) bool {
	if w.noticeAuthority == nil || !w.noticeAuthority.AuthorizeSend(ctx, q) {
		return false
	}
	campaign, err := q.GetPrivacyNoticeCampaign(ctx)
	return err == nil && campaign.ID == PrivacyNoticeID && campaign.State == "sealed" && bytes.Equal(campaign.ContentSha256, w.noticeAuthority.ContentDigest())
}

func (w *Worker) noticeManifestMatches(ctx context.Context, q *store.Queries) bool {
	if w.noticeAuthority == nil {
		return false
	}
	campaign, err := q.GetPrivacyNoticeCampaign(ctx)
	return err == nil && campaign.ID == PrivacyNoticeID && bytes.Equal(campaign.ContentSha256, w.noticeAuthority.ContentDigest())
}

func (w *Worker) sendNoticeJob(ctx context.Context, claimed store.AuthEmailJob) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return ErrWorker
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if rollbackErr := tx.Rollback(cleanup); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			w.logger.Warn("authmail: notice rollback failed")
		}
	}()
	q := w.queries.WithTx(tx)
	if !w.noticeAllowed(ctx, q) {
		return nil
	}
	// Read the ledger identity before acquiring locks so a reparented outbox
	// cannot make the worker lock a different account from the notice obligation.
	initial, err := q.GetPrivacyNoticeRecipient(ctx, claimed.ID)
	if err != nil || initial.UserID == nil {
		return ErrWorker
	}
	user, err := q.GetUserForUpdate(ctx, *initial.UserID)
	if err != nil {
		return ErrWorker
	}
	recipient, err := q.GetPrivacyNoticeRecipientForUpdate(ctx, claimed.ID)
	if err != nil || recipient.UserID == nil || *recipient.UserID != user.ID || recipient.State != "queued" {
		return ErrWorker
	}
	job, err := q.GetLeasedAuthEmailJobForUpdate(ctx, store.GetLeasedAuthEmailJobForUpdateParams{ID: claimed.ID, LeaseOwner: w.id.String()})
	if err != nil {
		return ErrWorker
	}
	now := w.clock()
	if job.LeaseExpiresAt == nil || !job.LeaseExpiresAt.After(now) {
		return nil
	}
	finishFailure := func(reason string) error {
		if failureErr := w.failNotice(ctx, q, recipient, reason, now); failureErr != nil {
			return failureErr
		}
		if tx.Commit(ctx) != nil {
			return ErrWorker
		}
		return nil
	}
	if !job.ExpiresAt.After(now) {
		return finishFailure("expired")
	}
	if job.Kind != string(KindPrivacyNotice) || job.UserID == nil || *job.UserID != user.ID || job.RegistrationID != nil || job.ResetTokenID != nil || job.TokenDigest != nil {
		return finishFailure("payload_invalid")
	}
	payload, err := w.ring.Open(job.ID, KindPrivacyNotice, sealedFromJob(job))
	if err != nil || payload.UserID != user.ID.String() || payload.To != user.Email || payload.NoticeID != PrivacyNoticeID || payload.Generation != recipient.Generation {
		return finishFailure("payload_invalid")
	}
	if w.hooks.beforeSend != nil {
		w.hooks.beforeSend(job.ID)
	}
	if !w.noticeAllowed(ctx, q) {
		return nil
	}
	sendCtx, cancel := context.WithTimeout(ctx, sendDeadline)
	result, sendErr := w.sender.Send(sendCtx, buildMessage(KindPrivacyNotice, payload, w.footerNote))
	cancel()
	if w.hooks.afterSend != nil {
		w.hooks.afterSend(job.ID, result.Outcome)
	}
	now = w.clock()
	if sendErr != nil {
		result.Outcome = SendTemporaryFailure
	}
	switch result.Outcome {
	case SendAccepted:
		count, updateErr := q.MarkAuthEmailJobSent(ctx, store.MarkAuthEmailJobSentParams{ID: job.ID, SentAt: now})
		if updateErr != nil || count != 1 {
			return ErrWorker
		}
		count, updateErr = q.AcceptPrivacyNoticeRecipient(ctx, store.AcceptPrivacyNoticeRecipientParams{JobID: job.ID, Generation: recipient.Generation, AcceptedAt: now, ContentSha256: w.noticeAuthority.ContentDigest()})
		if updateErr != nil || count != 1 {
			return ErrWorker
		}
	case SendPermanentFailure:
		return finishFailure("provider_rejected")
	case SendTemporaryFailure:
		if job.Attempts >= maxAttempts {
			return finishFailure("attempts_exhausted")
		}
		next := now.Add(w.jitter(backoffCap(job.Attempts)))
		if !next.Before(job.ExpiresAt) {
			return finishFailure("expired")
		}
		count, requeueErr := q.RequeueAuthEmailJob(ctx, store.RequeueAuthEmailJobParams{ID: job.ID, NextAttemptAt: next})
		if requeueErr != nil || count != 1 {
			return ErrWorker
		}
	default:
		return ErrWorker
	}
	if tx.Commit(ctx) != nil {
		return ErrWorker
	}
	return nil
}

func (w *Worker) failNotice(ctx context.Context, q *store.Queries, recipient store.PrivacyNoticeRecipient, reason string, now time.Time) error {
	if w.noticeAuthority == nil {
		return ErrWorker
	}
	count, err := q.MarkAuthEmailJobTerminal(ctx, store.MarkAuthEmailJobTerminalParams{ID: recipient.JobID, TerminalAt: now})
	if err != nil || count != 1 {
		return ErrWorker
	}
	count, err = q.FailPrivacyNoticeRecipient(ctx, store.FailPrivacyNoticeRecipientParams{JobID: recipient.JobID, Generation: recipient.Generation, FailedAt: now, FailureCode: reason, ContentSha256: w.noticeAuthority.ContentDigest()})
	if err != nil || count != 1 {
		return ErrWorker
	}
	return nil
}
