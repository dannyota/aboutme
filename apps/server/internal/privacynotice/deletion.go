package privacynotice

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// ResolveAccountDeletion runs after the caller locks the user. Recipient and
// job locks precede the user's cascading deletion, including cleaned-up jobs.
func ResolveAccountDeletion(ctx context.Context, q *store.Queries, user uuid.UUID, now time.Time) error {
	recipients, err := q.ListUserPrivacyNoticeRecipientsForUpdate(ctx, &user)
	if err != nil {
		return ErrState
	}
	if len(recipients) == 0 {
		return nil
	}
	campaign, err := q.GetPrivacyNoticeCampaign(ctx)
	if err != nil || !validCampaign(campaign) {
		return ErrState
	}
	for _, recipient := range recipients {
		if lockErr := q.LockPrivacyNoticeJob(ctx, recipient.JobID); lockErr != nil {
			return ErrState
		}
		count, updateErr := q.DeletePrivacyNoticeRecipientUser(ctx, store.DeletePrivacyNoticeRecipientUserParams{ID: recipient.ID, DeletedAt: now, ContentSha256: contentDigest()})
		if updateErr != nil || count != 1 {
			return ErrState
		}
	}
	return nil
}
