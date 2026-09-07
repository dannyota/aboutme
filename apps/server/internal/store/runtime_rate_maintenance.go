package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// runtimeRateCleanupPageLimit is the fixed upper bound both cleanup operations
// accept for one page.
const runtimeRateCleanupPageLimit = 256

// RuntimeRateCleanupResult is the owned, validated result of one bounded rate
// bucket cleanup page. PolicyIdle holds only through the committing
// transaction and is never permission to erase debt.
type RuntimeRateCleanupResult struct {
	DeletedCount int32
	EffectiveAt  time.Time
	PolicyIdle   bool
}

// RuntimeAdmissionReceiptCleanupResult is the owned, validated result of one
// bounded terminal admission-receipt cleanup page.
type RuntimeAdmissionReceiptCleanupResult struct {
	DeletedCount int32
}

// RuntimeRateMaintenanceStore exposes the two bounded maintenance cleanup
// operations. It shares no admission entry point with the rate transport.
type RuntimeRateMaintenanceStore interface {
	CleanupRateBuckets(context.Context, string, int32) (RuntimeRateCleanupResult, error)
	CleanupAdmissionAttemptReceipts(context.Context, int32) (RuntimeAdmissionReceiptCleanupResult, error)
}

type runtimeRateMaintenanceStore struct{ runner WriteTxRunner }

// NewRuntimeRateMaintenanceStore constructs a maintenance store backed by pool.
func NewRuntimeRateMaintenanceStore(pool *pgxpool.Pool) RuntimeRateMaintenanceStore {
	return &runtimeRateMaintenanceStore{runner: NewWriteTxRunner(pool)}
}

// CleanupRateBuckets deletes at most pageSize eligible buckets of one policy
// and returns the committed count, sampled time and locked idle predicate.
func (s *runtimeRateMaintenanceStore) CleanupRateBuckets(ctx context.Context, policy string, pageSize int32) (RuntimeRateCleanupResult, error) {
	if err := validateRateCleanupCall(ctx, pageSize); err != nil {
		return RuntimeRateCleanupResult{}, err
	}
	if policy == "" {
		return RuntimeRateCleanupResult{}, errors.New("store: invalid rate policy")
	}
	return runRateOperation(ctx, s.writeRunner(), func(q *Queries) (RuntimeCleanupRateBucketsRow, error) {
		return q.RuntimeCleanupRateBuckets(ctx, RuntimeCleanupRateBucketsParams{PolicyID: policy, PageSize: pageSize})
	}, func(p RuntimeCleanupRateBucketsRow) (RuntimeRateCleanupResult, error) {
		if err := validateRateCleanupCount(p.DeletedCount, pageSize); err != nil {
			return RuntimeRateCleanupResult{}, err
		}
		if p.EffectiveAt.IsZero() {
			return RuntimeRateCleanupResult{}, errors.New("store: invalid rate cleanup time")
		}
		return RuntimeRateCleanupResult(p), nil
	})
}

// CleanupAdmissionAttemptReceipts deletes at most pageSize terminal admission
// receipts past the fixed retention and returns the committed count.
func (s *runtimeRateMaintenanceStore) CleanupAdmissionAttemptReceipts(ctx context.Context, pageSize int32) (RuntimeAdmissionReceiptCleanupResult, error) {
	if err := validateRateCleanupCall(ctx, pageSize); err != nil {
		return RuntimeAdmissionReceiptCleanupResult{}, err
	}
	return runRateOperation(ctx, s.writeRunner(), func(q *Queries) (int32, error) {
		return q.RuntimeCleanupAdmissionAttemptReceipts(ctx, pageSize)
	}, func(deleted int32) (RuntimeAdmissionReceiptCleanupResult, error) {
		if err := validateRateCleanupCount(deleted, pageSize); err != nil {
			return RuntimeAdmissionReceiptCleanupResult{}, err
		}
		return RuntimeAdmissionReceiptCleanupResult{DeletedCount: deleted}, nil
	})
}

// writeRunner returns the configured runner, or nil when the store was built
// without one.
func (s *runtimeRateMaintenanceStore) writeRunner() WriteTxRunner {
	if s == nil {
		return nil
	}
	return s.runner
}

func validateRateCleanupCall(ctx context.Context, pageSize int32) error {
	if err := validateRateCall(ctx); err != nil {
		return err
	}
	if pageSize < 1 || pageSize > runtimeRateCleanupPageLimit {
		return errors.New("store: invalid rate cleanup page size")
	}
	return nil
}

func validateRateCleanupCount(deleted, pageSize int32) error {
	if deleted < 0 || deleted > pageSize {
		return errors.New("store: invalid rate cleanup count")
	}
	return nil
}
