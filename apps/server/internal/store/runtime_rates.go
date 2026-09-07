package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RuntimeRateDecisionResult is the owned, validated result of one fixed token
// or slug admission. Partition is present only for a private bucket and holds
// a copied value that no driver buffer can change.
type RuntimeRateDecisionResult struct {
	Allowed           bool
	RetryAfterSeconds int32
	BucketKind        string
	Partition         *int16
	EffectiveAt       time.Time
}

// RuntimePasswordFailureResult is the owned, validated result of one fixed
// password-failure state or record operation. Partition is absent for an
// overflow bucket and for the state result of an unallocated routable key.
type RuntimePasswordFailureResult struct {
	Exhausted         bool
	RetryAfterSeconds int32
	BucketKind        string
	Partition         *int16
	EffectiveAt       time.Time
	Allocated         bool
	ActivityRefreshed bool
}

// RuntimePasswordClearResult is the owned, validated result of one fixed
// password-failure clear. The kind is always private, and Partition is present
// only for a cleared bucket.
type RuntimePasswordClearResult struct {
	Cleared    bool
	BucketKind string
	Partition  *int16
}

// RuntimeFailedGrantReserveResult is the owned, validated result of one fixed
// P22 failed-grant reservation. A denial deliberately discloses no bucket
// identity, so BucketKind and Partition are both absent.
type RuntimeFailedGrantReserveResult struct {
	Allowed           bool
	RetryAfterSeconds int32
	BucketKind        *string
	Partition         *int16
	Replayed          bool
}

// RuntimeAdmissionAttemptFinishResult is the owned, validated result of one
// fixed P22 finish. StoredOutcome is absent only for absent_noop.
type RuntimeAdmissionAttemptFinishResult struct {
	Resolution    string
	StoredOutcome *string
}

// RuntimeRateTransport executes the seven fixed shared-rate admission
// operations. Each call runs one generated function inside the write barrier
// and returns a decoded result only after the transaction finished and
// committed.
type RuntimeRateTransport interface {
	AdmitToken(context.Context, string, [32]byte) (RuntimeRateDecisionResult, error)
	FailureState(context.Context, [32]byte) (RuntimePasswordFailureResult, error)
	RecordFailure(context.Context, [32]byte) (RuntimePasswordFailureResult, error)
	ClearFailureSuccess(context.Context, [32]byte) (RuntimePasswordClearResult, error)
	AdmitChangedSlug(context.Context, [32]byte) (RuntimeRateDecisionResult, error)
	ReserveFailedGrant(context.Context, uuid.UUID, uuid.UUID) (RuntimeFailedGrantReserveResult, error)
	FinishAttempt(context.Context, uuid.UUID, string) (RuntimeAdmissionAttemptFinishResult, error)
}

// runtimeRateDecisionProjection is the shared generated result shape of the
// two fixed decision queries.
type runtimeRateDecisionProjection = RuntimeAdmitTokenRateRow

// runtimeRatePasswordProjection is the shared generated result shape of the
// two fixed password-failure queries.
type runtimeRatePasswordProjection = RuntimePasswordFailureStateRow

type runtimeRateTransport struct{ runner WriteTxRunner }

// NewRuntimeRateTransport constructs a rate transport backed by pool.
func NewRuntimeRateTransport(pool *pgxpool.Pool) RuntimeRateTransport {
	return &runtimeRateTransport{runner: NewWriteTxRunner(pool)}
}

var (
	runtimeRateBucketKinds = map[string]bool{"private": true, "overflow": true}
	runtimeRateOutcomes    = map[string]bool{"failure": true, "neutral": true, "success": true}
	runtimeRateResolutions = map[string]bool{"caller_finished": true, "caller_replay": true, "system_noop": true, "absent_noop": true}
)

// AdmitToken consumes one whole token of the fixed token-bucket policy.
func (s *runtimeRateTransport) AdmitToken(ctx context.Context, policy string, digest [32]byte) (RuntimeRateDecisionResult, error) {
	if err := validateRateCall(ctx); err != nil {
		return RuntimeRateDecisionResult{}, err
	}
	if policy == "" {
		return RuntimeRateDecisionResult{}, errors.New("store: invalid rate policy")
	}
	return runRateOperation(ctx, s.writeRunner(), func(q *Queries) (runtimeRateDecisionProjection, error) {
		return q.RuntimeAdmitTokenRate(ctx, RuntimeAdmitTokenRateParams{PolicyID: policy, KeyDigest: digest[:]})
	}, decodeRuntimeRateDecision)
}

// FailureState reads the fixed password-failure state of one key without
// allocating a bucket or changing committed debt.
func (s *runtimeRateTransport) FailureState(ctx context.Context, digest [32]byte) (RuntimePasswordFailureResult, error) {
	if err := validateRateCall(ctx); err != nil {
		return RuntimePasswordFailureResult{}, err
	}
	return runRateOperation(ctx, s.writeRunner(), func(q *Queries) (runtimeRatePasswordProjection, error) {
		return q.RuntimePasswordFailureState(ctx, digest[:])
	}, func(p runtimeRatePasswordProjection) (RuntimePasswordFailureResult, error) {
		return decodeRuntimeRatePassword(p, false)
	})
}

// RecordFailure records one password failure against the selected bucket.
func (s *runtimeRateTransport) RecordFailure(ctx context.Context, digest [32]byte) (RuntimePasswordFailureResult, error) {
	if err := validateRateCall(ctx); err != nil {
		return RuntimePasswordFailureResult{}, err
	}
	return runRateOperation(ctx, s.writeRunner(), func(q *Queries) (runtimeRatePasswordProjection, error) {
		row, err := q.RuntimeRecordPasswordFailure(ctx, digest[:])
		return runtimeRatePasswordProjection(row), err
	}, func(p runtimeRatePasswordProjection) (RuntimePasswordFailureResult, error) {
		return decodeRuntimeRatePassword(p, true)
	})
}

// ClearFailureSuccess deletes one existing private password-failure bucket.
func (s *runtimeRateTransport) ClearFailureSuccess(ctx context.Context, digest [32]byte) (RuntimePasswordClearResult, error) {
	if err := validateRateCall(ctx); err != nil {
		return RuntimePasswordClearResult{}, err
	}
	return runRateOperation(ctx, s.writeRunner(), func(q *Queries) (RuntimeClearPasswordFailureRow, error) {
		return q.RuntimeClearPasswordFailure(ctx, digest[:])
	}, decodeRuntimeRateClear)
}

// AdmitChangedSlug charges the fixed rolling slug-change policy.
func (s *runtimeRateTransport) AdmitChangedSlug(ctx context.Context, digest [32]byte) (RuntimeRateDecisionResult, error) {
	if err := validateRateCall(ctx); err != nil {
		return RuntimeRateDecisionResult{}, err
	}
	return runRateOperation(ctx, s.writeRunner(), func(q *Queries) (runtimeRateDecisionProjection, error) {
		row, err := q.RuntimeAdmitSlugChange(ctx, digest[:])
		return runtimeRateDecisionProjection(row), err
	}, decodeRuntimeRateDecision)
}

// ReserveFailedGrant reserves or exactly replays one P22 failed-grant slot.
func (s *runtimeRateTransport) ReserveFailedGrant(ctx context.Context, attemptID, clientID uuid.UUID) (RuntimeFailedGrantReserveResult, error) {
	if err := validateRateCall(ctx); err != nil {
		return RuntimeFailedGrantReserveResult{}, err
	}
	if attemptID == uuid.Nil || clientID == uuid.Nil {
		return RuntimeFailedGrantReserveResult{}, errors.New("store: invalid admission attempt identity")
	}
	return runRateOperation(ctx, s.writeRunner(), func(q *Queries) (RuntimeReserveOAuthFailedGrantRow, error) {
		return q.RuntimeReserveOAuthFailedGrant(ctx, RuntimeReserveOAuthFailedGrantParams{AttemptID: attemptID, ClientID: clientID})
	}, decodeRuntimeRateReserve)
}

// FinishAttempt resolves one reserved P22 attempt with the caller outcome.
func (s *runtimeRateTransport) FinishAttempt(ctx context.Context, attemptID uuid.UUID, outcome string) (RuntimeAdmissionAttemptFinishResult, error) {
	if err := validateRateCall(ctx); err != nil {
		return RuntimeAdmissionAttemptFinishResult{}, err
	}
	if attemptID == uuid.Nil {
		return RuntimeAdmissionAttemptFinishResult{}, errors.New("store: invalid admission attempt identity")
	}
	if !runtimeRateOutcomes[outcome] {
		return RuntimeAdmissionAttemptFinishResult{}, errors.New("store: invalid admission attempt outcome")
	}
	return runRateOperation(ctx, s.writeRunner(), func(q *Queries) (RuntimeFinishAdmissionAttemptRow, error) {
		return q.RuntimeFinishAdmissionAttempt(ctx, RuntimeFinishAdmissionAttemptParams{AttemptID: attemptID, Outcome: outcome})
	}, func(p RuntimeFinishAdmissionAttemptRow) (RuntimeAdmissionAttemptFinishResult, error) {
		return decodeRuntimeRateFinish(p, outcome)
	})
}

// writeRunner returns the configured runner, or nil when the transport was
// built without one.
func (s *runtimeRateTransport) writeRunner() WriteTxRunner {
	if s == nil {
		return nil
	}
	return s.runner
}

// runRateOperation executes one generated rate function inside the write
// runner. The row is validated and copied before the callback returns, so an
// invalid result rolls the transaction back, and no value escapes unless
// finish and commit both succeed.
func runRateOperation[P, R any](ctx context.Context, runner WriteTxRunner, call func(*Queries) (P, error), decode func(P) (R, error)) (R, error) {
	var zero R
	if runner == nil {
		return zero, errors.New("store: nil rate transport runner")
	}
	var decoded R
	err := runner.ExecWrite(ctx, func(q *Queries) error {
		projected, err := call(q)
		if err != nil {
			return err
		}
		decoded, err = decode(projected)
		return err
	})
	if err != nil {
		return zero, err
	}
	return decoded, nil
}

func validateRateCall(ctx context.Context) error {
	if ctx == nil {
		return errors.New("store: nil rate context")
	}
	return nil
}

// decodeRateBucket validates one bucket kind against its nullable partition
// and returns an owned partition copy. A private bucket carries partition 1 or
// 2 unless allowAbsent covers the fixed no-row state result; an overflow
// bucket never carries a partition.
func decodeRateBucket(kind string, value int16, present, allowAbsent bool) (*int16, error) {
	if !runtimeRateBucketKinds[kind] {
		return nil, errors.New("store: invalid rate bucket kind")
	}
	if kind == "overflow" {
		if present {
			return nil, errors.New("store: invalid overflow rate partition")
		}
		return nil, nil
	}
	if !present {
		if allowAbsent {
			return nil, nil
		}
		return nil, errors.New("store: missing private rate partition")
	}
	if value != 1 && value != 2 {
		return nil, errors.New("store: invalid private rate partition")
	}
	partition := value
	return &partition, nil
}

func decodeRuntimeRateDecision(p runtimeRateDecisionProjection) (RuntimeRateDecisionResult, error) {
	if p.RetryAfterSeconds < 0 || p.Allowed != (p.RetryAfterSeconds == 0) || p.EffectiveAt.IsZero() {
		return RuntimeRateDecisionResult{}, errors.New("store: invalid rate decision result")
	}
	partition, err := decodeRateBucket(p.BucketKind, p.PartitionValue, p.PartitionPresent, false)
	if err != nil {
		return RuntimeRateDecisionResult{}, err
	}
	return RuntimeRateDecisionResult{Allowed: p.Allowed, RetryAfterSeconds: p.RetryAfterSeconds, BucketKind: p.BucketKind, Partition: partition, EffectiveAt: p.EffectiveAt}, nil
}

// decodeRuntimeRatePassword validates the fixed password-failure matrix.
// recording selects the stricter function 3 rules: one bucket is always
// selected and refreshed, and only a newly allocated private bucket sets
// Allocated. The function 2 state result never allocates, and its private
// result may carry no partition when the key has no row yet.
func decodeRuntimeRatePassword(p runtimeRatePasswordProjection, recording bool) (RuntimePasswordFailureResult, error) {
	if p.RetryAfterSeconds < 0 || p.Exhausted != (p.RetryAfterSeconds > 0) || p.EffectiveAt.IsZero() {
		return RuntimePasswordFailureResult{}, errors.New("store: invalid password failure result")
	}
	partition, err := decodeRateBucket(p.BucketKind, p.PartitionValue, p.PartitionPresent, !recording)
	if err != nil {
		return RuntimePasswordFailureResult{}, err
	}
	unallocated := p.BucketKind == "private" && partition == nil
	if recording {
		if !p.ActivityRefreshed || (p.Allocated && p.BucketKind != "private") {
			return RuntimePasswordFailureResult{}, errors.New("store: invalid recorded password failure result")
		}
	} else {
		if p.Allocated || p.ActivityRefreshed == unallocated || (unallocated && p.Exhausted) {
			return RuntimePasswordFailureResult{}, errors.New("store: invalid password failure state result")
		}
	}
	return RuntimePasswordFailureResult{Exhausted: p.Exhausted, RetryAfterSeconds: p.RetryAfterSeconds, BucketKind: p.BucketKind, Partition: partition, EffectiveAt: p.EffectiveAt, Allocated: p.Allocated, ActivityRefreshed: p.ActivityRefreshed}, nil
}

func decodeRuntimeRateClear(p RuntimeClearPasswordFailureRow) (RuntimePasswordClearResult, error) {
	if p.BucketKind != "private" || p.Cleared != p.PartitionPresent {
		return RuntimePasswordClearResult{}, errors.New("store: invalid password clear result")
	}
	partition, err := decodeRateBucket(p.BucketKind, p.PartitionValue, p.PartitionPresent, true)
	if err != nil {
		return RuntimePasswordClearResult{}, err
	}
	return RuntimePasswordClearResult{Cleared: p.Cleared, BucketKind: p.BucketKind, Partition: partition}, nil
}

func decodeRuntimeRateReserve(p RuntimeReserveOAuthFailedGrantRow) (RuntimeFailedGrantReserveResult, error) {
	if p.RetryAfterSeconds < 0 || p.Allowed != (p.RetryAfterSeconds == 0) {
		return RuntimeFailedGrantReserveResult{}, errors.New("store: invalid failed grant reserve result")
	}
	if !p.Allowed {
		if p.BucketKindPresent || p.PartitionPresent || p.Replayed {
			return RuntimeFailedGrantReserveResult{}, errors.New("store: invalid denied failed grant result")
		}
		return RuntimeFailedGrantReserveResult{RetryAfterSeconds: p.RetryAfterSeconds}, nil
	}
	if !p.BucketKindPresent {
		return RuntimeFailedGrantReserveResult{}, errors.New("store: missing admitted failed grant bucket")
	}
	partition, err := decodeRateBucket(p.BucketKindValue, p.PartitionValue, p.PartitionPresent, false)
	if err != nil {
		return RuntimeFailedGrantReserveResult{}, err
	}
	kind := p.BucketKindValue
	return RuntimeFailedGrantReserveResult{Allowed: true, BucketKind: &kind, Partition: partition, Replayed: p.Replayed}, nil
}

// decodeRuntimeRateFinish validates the fixed P22 finish matrix against the
// requested outcome: a caller resolution stores exactly that outcome, a system
// resolution stores neutral, and only absent_noop stores none.
func decodeRuntimeRateFinish(p RuntimeFinishAdmissionAttemptRow, requested string) (RuntimeAdmissionAttemptFinishResult, error) {
	if !runtimeRateResolutions[p.Resolution] {
		return RuntimeAdmissionAttemptFinishResult{}, errors.New("store: invalid admission attempt resolution")
	}
	if p.Resolution == "absent_noop" {
		if p.StoredOutcomePresent {
			return RuntimeAdmissionAttemptFinishResult{}, errors.New("store: invalid absent admission attempt result")
		}
		return RuntimeAdmissionAttemptFinishResult{Resolution: p.Resolution}, nil
	}
	expected := requested
	if p.Resolution == "system_noop" {
		expected = "neutral"
	}
	if !p.StoredOutcomePresent || !runtimeRateOutcomes[p.StoredOutcomeValue] || p.StoredOutcomeValue != expected {
		return RuntimeAdmissionAttemptFinishResult{}, errors.New("store: invalid stored admission attempt outcome")
	}
	outcome := p.StoredOutcomeValue
	return RuntimeAdmissionAttemptFinishResult{Resolution: p.Resolution, StoredOutcome: &outcome}, nil
}
