package privacynotice

import (
	"context"
	"errors"

	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// ErrQueueFull reports the fixed notice backlog bound without mutation.
var ErrQueueFull = errors.New("privacy notice: queue_full")

// Enqueue seals at most 100 live-account payloads under the campaign lock.
func (s *Service) Enqueue(ctx context.Context) (resultErr error) {
	if s.outbox == nil {
		return ErrState
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ErrState
	}
	defer func() {
		if rollbackErr := rollback(ctx, tx); rollbackErr != nil {
			resultErr = rollbackErr
		}
	}()
	if boundErr := boundTransaction(ctx, tx); boundErr != nil {
		return boundErr
	}
	q := store.New(tx)
	if q.LockPrivacyNoticeCommand(ctx) != nil || !s.AuthorizeSend(ctx, q) {
		return ErrState
	}
	campaign, err := q.GetPrivacyNoticeCampaign(ctx)
	if err != nil || !validCampaign(campaign) || campaign.State != "sealed" {
		return ErrState
	}
	backlog, err := q.CountLivePrivacyNoticeJobs(ctx)
	if err != nil || backlog > 100 {
		return ErrState
	}
	if backlog == 100 {
		return ErrQueueFull
	}
	recipients, err := q.ListUnqueuedPrivacyNoticeRecipients(ctx)
	if err != nil {
		return ErrState
	}
	if int64(len(recipients)) > 100-backlog {
		recipients = recipients[:100-backlog]
	}
	for _, initial := range recipients {
		if initial.UserID == nil {
			return ErrState
		}
		user, userErr := q.GetUserForUpdate(ctx, *initial.UserID)
		if userErr != nil {
			return ErrState
		}
		recipient, recipientErr := q.GetPrivacyNoticeRecipientForUpdate(ctx, initial.JobID)
		if recipientErr != nil || recipient.State != "unqueued" || recipient.UserID == nil || *recipient.UserID != user.ID {
			return ErrState
		}
		if q.LockPrivacyNoticeJob(ctx, recipient.JobID) != nil {
			return ErrState
		}
		now := s.now()
		if enqueueErr := s.outbox.EnqueueTx(ctx, q, authmail.EnqueueRequest{
			JobID: recipient.JobID, Kind: authmail.KindPrivacyNotice, UserID: &user.ID,
			Payload: authmail.Payload{Version: 3, To: user.Email, UserID: user.ID.String(), NoticeID: authmail.PrivacyNoticeID, Generation: recipient.Generation},
		}); enqueueErr != nil {
			return ErrState
		}
		count, queueErr := q.QueuePrivacyNoticeRecipient(ctx, store.QueuePrivacyNoticeRecipientParams{ID: recipient.ID, Generation: recipient.Generation, QueuedAt: now, ContentSha256: contentDigest()})
		if queueErr != nil || count != 1 {
			return ErrState
		}
	}
	if !s.AuthorizeSend(ctx, q) {
		return ErrState
	}
	if tx.Commit(ctx) != nil {
		return ErrState
	}
	return nil
}
