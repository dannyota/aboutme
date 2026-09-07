package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RuntimeClaimReceiptGCResult contains committed receipt-cleanup counts.
type RuntimeClaimReceiptGCResult struct {
	DeletedClaimCount        int32
	DeletedScopeSummaryCount int32
}

// RuntimeClaimReceiptStore exposes bounded maintenance receipt cleanup. It
// shares no acquire or fencing entry point with the claim transport.
type RuntimeClaimReceiptStore interface {
	GCReleasedClaimReceipts(context.Context) (RuntimeClaimReceiptGCResult, error)
}

type runtimeClaimReceiptStore struct{ runner WriteTxRunner }

// NewRuntimeClaimReceiptStore constructs a receipt store backed by pool.
func NewRuntimeClaimReceiptStore(pool *pgxpool.Pool) RuntimeClaimReceiptStore {
	return &runtimeClaimReceiptStore{runner: NewWriteTxRunner(pool)}
}

// GCReleasedClaimReceipts deletes one fixed page of expired release receipts
// and returns the committed counts. Every runner error yields the zero result.
func (s *runtimeClaimReceiptStore) GCReleasedClaimReceipts(ctx context.Context) (RuntimeClaimReceiptGCResult, error) {
	if ctx == nil {
		return RuntimeClaimReceiptGCResult{}, errors.New("store: nil claim receipt context")
	}
	if s == nil || s.runner == nil {
		return RuntimeClaimReceiptGCResult{}, errors.New("store: nil claim receipt runner")
	}
	var result RuntimeClaimReceiptGCResult
	err := s.runner.ExecWrite(ctx, func(q *Queries) error {
		row, err := q.RuntimeGCReleasedClaimReceipts(ctx)
		if err != nil {
			return err
		}
		if row.DeletedClaimCount < 0 || row.DeletedScopeSummaryCount < 0 {
			return errors.New("store: invalid claim receipt counts")
		}
		result = RuntimeClaimReceiptGCResult(row)
		return nil
	})
	if err != nil {
		return RuntimeClaimReceiptGCResult{}, err
	}
	return result, nil
}
