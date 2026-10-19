package privacynotice

import (
	"context"
	"errors"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func (s *Service) reviewed(ctx context.Context, operation func(*store.Queries, store.PrivacyNoticeCampaign) error) (resultErr error) {
	if !s.recoveryFenced() {
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
	if q.LockPrivacyNoticeCommand(ctx) != nil {
		return ErrState
	}
	campaign, err := q.GetPrivacyNoticeCampaign(ctx)
	if err != nil || !validCampaign(campaign) || campaign.SealedAt == nil || !validCampaignTimes(campaign, s.now()) {
		return ErrState
	}
	if operationErr := operation(q, campaign); operationErr != nil {
		return operationErr
	}
	if !s.recoveryFenced() || tx.Commit(ctx) != nil {
		return ErrState
	}
	return nil
}

func lockMember(ctx context.Context, q *store.Queries, initial store.PrivacyNoticeRecipient) (store.PrivacyNoticeRecipient, *store.User, *store.AuthEmailJob, error) {
	var user *store.User
	if initial.UserID != nil {
		row, userErr := q.GetUserForUpdate(ctx, *initial.UserID)
		if userErr != nil {
			return initial, nil, nil, ErrState
		}
		user = &row
	}
	recipient, err := q.GetPrivacyNoticeRecipientForUpdate(ctx, initial.JobID)
	if err != nil || recipient.ID != initial.ID || !sameUser(recipient.UserID, initial.UserID) {
		return initial, nil, nil, ErrState
	}
	job, err := q.GetPrivacyNoticeJobForUpdate(ctx, recipient.JobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return recipient, user, nil, nil
	}
	if err != nil || job.Kind != string(authmail.KindPrivacyNotice) || job.UserID == nil || !sameUser(job.UserID, recipient.UserID) {
		return initial, nil, nil, ErrState
	}
	return recipient, user, &job, nil
}

func sameUser(left, right *uuid.UUID) bool {
	return (left == nil && right == nil) || (left != nil && right != nil && *left == *right)
}

func failureReason(reason string) bool {
	switch reason {
	case "expired", "attempts_exhausted", "provider_rejected", "payload_invalid", "job_missing", "delivery_failed":
		return true
	}
	return false
}

// Recover is only for independently reviewed production operations after the
// host stops workers and revokes receipts. It is not a routine command.
func (s *Service) Recover(ctx context.Context, reason string) error {
	if !failureReason(reason) || s.outbox == nil {
		return ErrState
	}
	return s.reviewed(ctx, func(q *store.Queries, campaign store.PrivacyNoticeCampaign) error {
		if campaign.State != "sealed" {
			return ErrState
		}
		backlog, backlogErr := q.CountLivePrivacyNoticeJobs(ctx)
		if backlogErr != nil || backlog > 100 {
			return ErrState
		}
		if backlog == 100 {
			return ErrQueueFull
		}
		recipients, listErr := q.ListFailedPrivacyNoticeRecipients(ctx, &reason)
		if listErr != nil {
			return ErrState
		}
		if int64(len(recipients)) > 100-backlog {
			recipients = recipients[:100-backlog]
		}
		for _, initial := range recipients {
			recipient, user, job, lockErr := lockMember(ctx, q, initial)
			if lockErr != nil || user == nil || recipient.State != "failed" || recipient.FailureCode == nil || *recipient.FailureCode != reason || recipient.Generation == math.MaxInt32 {
				return ErrState
			}
			if job != nil {
				if job.State != "terminal" && job.State != "sent" {
					return ErrState
				}
				removed, deleteErr := q.DeleteRecoveredPrivacyNoticeJob(ctx, recipient.JobID)
				if deleteErr != nil || removed != 1 {
					return ErrState
				}
			}
			if enqueueErr := s.outbox.EnqueueTx(ctx, q, authmail.EnqueueRequest{JobID: recipient.JobID, Kind: authmail.KindPrivacyNotice, UserID: &user.ID, Payload: authmail.Payload{Version: 3, To: user.Email, UserID: user.ID.String(), NoticeID: authmail.PrivacyNoticeID, Generation: recipient.Generation + 1}}); enqueueErr != nil {
				return ErrState
			}
			count, updateErr := q.RecoverPrivacyNoticeRecipient(ctx, store.RecoverPrivacyNoticeRecipientParams{ID: recipient.ID, QueuedAt: s.now(), Generation: recipient.Generation, FailureCode: reason, ContentSha256: contentDigest()})
			if updateErr != nil || count != 1 {
				return ErrState
			}
			count, updateErr = q.CountPrivacyNoticeRecovery(ctx, store.CountPrivacyNoticeRecoveryParams{Reason: reason, ContentSha256: contentDigest()})
			if updateErr != nil || count != 1 {
				return ErrState
			}
		}
		return nil
	})
}

// RecordLateFailure requires revoked host authority and a known provider
// failure. It never infers delivery from the absence of feedback.
func (s *Service) RecordLateFailure(ctx context.Context, jobID uuid.UUID) error {
	return s.reviewed(ctx, func(q *store.Queries, campaign store.PrivacyNoticeCampaign) error {
		if campaign.State != "sealed" && campaign.State != "activated" {
			return ErrState
		}
		initial, readErr := q.GetPrivacyNoticeRecipient(ctx, jobID)
		if readErr != nil {
			return ErrState
		}
		recipient, _, job, lockErr := lockMember(ctx, q, initial)
		if lockErr != nil || recipient.State != "accepted" || (job != nil && job.State != "sent") {
			return ErrState
		}
		now, timeErr := q.PrivacyNoticeDatabaseTime(ctx)
		if timeErr != nil {
			return ErrState
		}
		count, updateErr := q.RecordPrivacyNoticeLateFailure(ctx, store.RecordPrivacyNoticeLateFailureParams{JobID: jobID, At: now, ContentSha256: contentDigest()})
		if updateErr != nil || count != 1 {
			return ErrState
		}
		if campaign.State == "activated" {
			count, updateErr = q.ReopenPrivacyNoticeCampaign(ctx, contentDigest())
			if updateErr != nil || count != 1 {
				return ErrState
			}
		}
		return nil
	})
}
