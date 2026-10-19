package privacynotice

import (
	"context"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// Abort is a reviewed production operation, not a routine command. The host
// must stop workers and changed ingestion and durably revoke both receipts.
func (s *Service) Abort(ctx context.Context) error {
	// The latch commits separately so an invalid cancellation batch cannot
	// reopen sending or erase an interrupted abort.
	if latchErr := s.reviewed(ctx, func(q *store.Queries, campaign store.PrivacyNoticeCampaign) error {
		if campaign.State == "aborted" {
			return nil
		}
		if campaign.State != "sealed" && campaign.State != "activated" {
			return ErrState
		}
		totals, readErr := q.PrivacyNoticeTotals(ctx)
		if readErr != nil || totals.Actual != campaign.ExpectedCount {
			return ErrState
		}
		count, updateErr := q.LatchPrivacyNoticeAbort(ctx, contentDigest())
		if updateErr != nil || count != 1 {
			return ErrState
		}
		return nil
	}); latchErr != nil {
		return latchErr
	}
	return s.reviewed(ctx, func(q *store.Queries, campaign store.PrivacyNoticeCampaign) error {
		if campaign.State != "aborted" {
			return ErrState
		}
		proof, proofErr := q.PrivacyNoticeAbortProof(ctx)
		if proofErr != nil || proof.Actual != campaign.ExpectedCount || proof.Invalid != 0 {
			return ErrState
		}
		if campaign.AbortedAt != nil {
			if proof.Remaining != 0 || proof.LiveOrOrphan != 0 {
				return ErrState
			}
			return nil
		}
		recipients, listErr := q.ListPrivacyNoticeAbortCandidates(ctx)
		if listErr != nil {
			return ErrState
		}
		now, timeErr := q.PrivacyNoticeDatabaseTime(ctx)
		if timeErr != nil {
			return ErrState
		}
		for _, initial := range recipients {
			recipient, user, job, lockErr := lockMember(ctx, q, initial)
			if lockErr != nil || user == nil {
				return ErrState
			}
			switch recipient.State {
			case "unqueued":
				if job != nil {
					return ErrState
				}
			case "queued":
				if job != nil {
					if job.State != "pending" && job.State != "leased" {
						return ErrState
					}
					count, terminalErr := q.MarkAuthEmailJobTerminal(ctx, store.MarkAuthEmailJobTerminalParams{ID: job.ID, TerminalAt: now})
					if terminalErr != nil || count != 1 {
						return ErrState
					}
				}
			default:
				return ErrState
			}
			count, cancelErr := q.CancelPrivacyNoticeRecipient(ctx, store.CancelPrivacyNoticeRecipientParams{ID: recipient.ID, At: now, ContentSha256: contentDigest()})
			if cancelErr != nil || count != 1 {
				return ErrState
			}
		}
		proof, proofErr = q.PrivacyNoticeAbortProof(ctx)
		if proofErr != nil || proof.Actual != campaign.ExpectedCount || proof.Invalid != 0 {
			return ErrState
		}
		if proof.Remaining == 0 {
			if proof.LiveOrOrphan != 0 {
				return ErrState
			}
			count, finishErr := q.CompletePrivacyNoticeAbort(ctx, store.CompletePrivacyNoticeAbortParams{At: now, ContentSha256: contentDigest()})
			if finishErr != nil || count != 1 {
				return ErrState
			}
		}
		return nil
	})
}
