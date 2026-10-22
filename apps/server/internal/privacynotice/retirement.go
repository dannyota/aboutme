package privacynotice

import (
	"context"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// Retire requires permanent collection disablement and revoked host receipts.
// Unresolved obligations must first complete the permanent abort operation.
func (s *Service) Retire(ctx context.Context) error {
	return s.reviewed(ctx, func(q *store.Queries, campaign store.PrivacyNoticeCampaign) error {
		if campaign.State == "retired" {
			return ErrState
		}
		if campaign.State == "aborted" {
			proof, proofErr := q.PrivacyNoticeAbortProof(ctx)
			if proofErr != nil || campaign.AbortedAt == nil || proof.Actual != campaign.ExpectedCount || proof.Remaining != 0 || proof.Invalid != 0 || proof.LiveOrOrphan != 0 {
				return ErrState
			}
		} else {
			report, readErr := readReport(ctx, q)
			if readErr != nil || report.Code != 0 {
				return ErrState
			}
		}
		now, timeErr := q.PrivacyNoticeDatabaseTime(ctx)
		if timeErr != nil {
			return ErrState
		}
		count, updateErr := q.RetirePrivacyNoticeCampaign(ctx, store.RetirePrivacyNoticeCampaignParams{At: now, ContentSha256: contentDigest()})
		if updateErr != nil || count != 1 {
			return ErrState
		}
		return nil
	})
}

// PurgeRetired removes at most 200 recipient rows after reviewed retirement.
func (s *Service) PurgeRetired(ctx context.Context) (int64, error) {
	var removed int64
	err := s.reviewed(ctx, func(q *store.Queries, campaign store.PrivacyNoticeCampaign) error {
		if campaign.State != "retired" || campaign.RetiredAt == nil {
			return ErrState
		}
		if _, readErr := readReport(ctx, q); readErr != nil {
			return ErrState
		}
		var purgeErr error
		removed, purgeErr = q.PurgeRetiredPrivacyNoticeRecipients(ctx, contentDigest())
		if purgeErr != nil {
			return ErrState
		}
		if _, finishErr := q.CompletePrivacyNoticePurge(ctx, contentDigest()); finishErr != nil {
			return ErrState
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return removed, nil
}
