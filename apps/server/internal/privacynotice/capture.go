// Package privacynotice implements the fixed privacy notice cohort and ledger.
package privacynotice

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// ErrState hides database and authority details from command output.
var ErrState = errors.New("privacy notice: invalid state")

// Service owns the fixed cohort and its durable outcomes.
type Service struct {
	pool        *store.Pool
	outbox      *authmail.Outbox
	now         func() time.Time
	authorityFS fs.FS
}

// New permits a keyless service for every action except enqueue.
func New(pool *store.Pool, outbox *authmail.Outbox, now func() time.Time) (*Service, error) {
	if pool == nil || now == nil {
		return nil, ErrState
	}
	return &Service{pool: pool, outbox: outbox, now: now}, nil
}

// Prepare captures one bounded batch. The host keeps account writers stopped
// until the exact membership proof seals the cohort.
func (s *Service) Prepare(ctx context.Context) (resultErr error) {
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
	if lockErr := q.LockPrivacyNoticeCommand(ctx); lockErr != nil {
		return ErrState
	}
	campaign, err := q.GetPrivacyNoticeCampaign(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		campaign, err = q.CreatePrivacyNoticeCampaign(ctx, contentDigest())
	}
	if err != nil || !validCampaign(campaign) {
		return ErrState
	}
	if campaign.State == "sealed" {
		report, readErr := readReport(ctx, q)
		if readErr != nil || report.Code == 1 {
			return ErrState
		}
		return nil
	}
	if campaign.State != "capturing" {
		return ErrState
	}
	proof, err := q.PrivacyNoticeCaptureProof(ctx)
	if err != nil || proof.UserCount != campaign.ExpectedCount || !proof.AllRecipientsExist {
		return ErrState
	}
	users, err := q.ListPrivacyNoticeCaptureUsers(ctx, campaign.CaptureCursor)
	if err != nil {
		return ErrState
	}
	for _, user := range users {
		id, idErr := uuid.NewRandom()
		if idErr != nil {
			return ErrState
		}
		job, jobErr := uuid.NewRandom()
		if jobErr != nil {
			return ErrState
		}
		if insertErr := q.InsertPrivacyNoticeRecipient(ctx, store.InsertPrivacyNoticeRecipientParams{ID: id, UserID: &user, JobID: job}); insertErr != nil {
			return ErrState
		}
	}
	if len(users) > 0 {
		count, advanceErr := q.AdvancePrivacyNoticeCapture(ctx, store.AdvancePrivacyNoticeCaptureParams{CaptureCursor: users[len(users)-1], ContentSha256: contentDigest()})
		if advanceErr != nil || count != 1 {
			return ErrState
		}
	}
	proof, err = q.PrivacyNoticeCaptureProof(ctx)
	if err != nil || proof.UserCount != campaign.ExpectedCount || proof.RecipientCount > campaign.ExpectedCount || !proof.AllRecipientsExist {
		return ErrState
	}
	if len(users) == 0 && proof.RecipientCount < campaign.ExpectedCount {
		return ErrState
	}
	if proof.RecipientCount == campaign.ExpectedCount {
		if !proof.AllUsersCaptured {
			return ErrState
		}
		count, sealErr := q.SealPrivacyNoticeCampaign(ctx, contentDigest())
		if sealErr != nil || count != 1 {
			return ErrState
		}
	}
	if tx.Commit(ctx) != nil {
		return ErrState
	}
	return nil
}

func validCampaign(campaign store.PrivacyNoticeCampaign) bool {
	return campaign.ID == authmail.PrivacyNoticeID && bytes.Equal(campaign.ContentSha256, contentDigest())
}

func boundTransaction(ctx context.Context, tx pgx.Tx) error {
	if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout='10s'`); err != nil {
		return ErrState
	}
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout='2s'`); err != nil {
		return ErrState
	}
	return nil
}

func rollback(ctx context.Context, tx pgx.Tx) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if rollbackErr := tx.Rollback(cleanup); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
		return ErrState
	}
	return nil
}
