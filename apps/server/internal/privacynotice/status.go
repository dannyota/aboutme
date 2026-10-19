package privacynotice

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// Report holds an internal snapshot. JSON exposes only the closed aggregates.
type Report struct {
	Campaign store.PrivacyNoticeCampaign
	Totals   store.PrivacyNoticeTotalsRow
	Code     int
	result   string
}

// Status reads aggregate evidence in one consistent read-only snapshot.
func (s *Service) Status(ctx context.Context) (result Report, resultErr error) {
	return s.aggregate(ctx, true)
}

func (s *Service) aggregate(ctx context.Context, checkHost bool) (result Report, resultErr error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Report{Code: 1}, ErrState
	}
	defer func() {
		if rollbackErr := rollback(ctx, tx); rollbackErr != nil {
			result.Code = 1
			resultErr = rollbackErr
		}
	}()
	if boundErr := boundTransaction(ctx, tx); boundErr != nil {
		return Report{Code: 1}, boundErr
	}
	report, err := readReport(ctx, store.New(tx))
	if err != nil {
		return report, err
	}
	if checkHost && !s.hostStateValid(report) {
		report.Code = 1
		return report, ErrState
	}
	if !validCampaignTimes(report.Campaign, s.now()) {
		return Report{Code: 1}, ErrState
	}
	if tx.Commit(ctx) != nil {
		report.Code = 1
		return report, ErrState
	}
	return report, nil
}

func readReport(ctx context.Context, q *store.Queries) (Report, error) {
	report := Report{Code: 1}
	campaign, err := q.GetPrivacyNoticeCampaign(ctx)
	if err != nil || !validCampaign(campaign) {
		return report, ErrState
	}
	report.Campaign = campaign
	totals, err := q.PrivacyNoticeTotals(ctx)
	if err != nil {
		return report, ErrState
	}
	report.Totals = totals
	if totals.Actual > campaign.ExpectedCount {
		return report, ErrState
	}
	if campaign.State == "aborted" {
		proof, proofErr := q.PrivacyNoticeAbortProof(ctx)
		if proofErr != nil || proof.Actual != campaign.ExpectedCount || proof.Invalid != 0 || (campaign.AbortedAt != nil && (proof.Remaining != 0 || proof.LiveOrOrphan != 0)) {
			return report, ErrState
		}
		return report, nil
	}
	if campaign.State == "retired" {
		if totals.Inconsistent != totals.OrphanCompleted || totals.Unqueued != 0 || totals.Queued != 0 {
			return report, ErrState
		}
		return report, nil
	}
	if totals.Inconsistent != 0 {
		return report, ErrState
	}
	if totals.Failed != 0 {
		return report, nil
	}
	if campaign.State == "capturing" {
		report.Code = 2
		return report, nil
	}
	if totals.Actual != campaign.ExpectedCount {
		return report, ErrState
	}
	if totals.Cancelled != 0 { //nolint:misspell // Generated store member follows the fixed database spelling.
		return report, ErrState
	}
	if campaign.State == "activated" && (campaign.AcceptedCount == nil || campaign.DeletedCount == nil || *campaign.AcceptedCount != totals.Accepted || *campaign.DeletedCount != totals.Deleted || (totals.Accepted > 0 && (campaign.LastAcceptedAt == nil || !campaign.LastAcceptedAt.Equal(totals.LastAcceptedAt)))) {
		return report, ErrState
	}
	if totals.Accepted+totals.Deleted == campaign.ExpectedCount && totals.Unqueued == 0 && totals.Queued == 0 {
		report.Code = 0
	} else {
		report.Code = 2
	}
	return report, nil
}

// Activate records completion or rechecks retained evidence for receipt recovery.
func (s *Service) Activate(ctx context.Context) (result Report, resultErr error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Report{Code: 1}, ErrState
	}
	defer func() {
		if rollbackErr := rollback(ctx, tx); rollbackErr != nil {
			result.Code = 1
			resultErr = rollbackErr
		}
	}()
	if boundErr := boundTransaction(ctx, tx); boundErr != nil {
		return Report{Code: 1}, boundErr
	}
	q := store.New(tx)
	if q.LockPrivacyNoticeCommand(ctx) != nil {
		return Report{Code: 1}, ErrState
	}
	report, err := readReport(ctx, q)
	if err != nil || !s.hostStateValid(report) {
		report.Code = 1
		return report, ErrState
	}
	if report.Code != 0 {
		return report, nil
	}
	if report.Campaign.State == "sealed" {
		var lastAcceptedAt *time.Time
		if report.Totals.Accepted > 0 {
			lastAcceptedAt = &report.Totals.LastAcceptedAt
		}
		count, activateErr := q.ActivatePrivacyNoticeCampaign(ctx, store.ActivatePrivacyNoticeCampaignParams{
			AcceptedCount: report.Totals.Accepted, DeletedCount: report.Totals.Deleted, LastAcceptedAt: lastAcceptedAt, ContentSha256: contentDigest(),
		})
		if activateErr != nil || count != 1 {
			report.Code = 1
			return report, ErrState
		}
		report, err = readReport(ctx, q)
		if err != nil {
			return report, err
		}
		if !validCampaignTimes(report.Campaign, s.now()) {
			return Report{Code: 1}, ErrState
		}
	} else if report.Campaign.State != "activated" {
		report.Code = 1
		return report, ErrState
	}
	if tx.Commit(ctx) != nil {
		report.Code = 1
		return report, ErrState
	}
	return s.aggregate(ctx, false)
}
