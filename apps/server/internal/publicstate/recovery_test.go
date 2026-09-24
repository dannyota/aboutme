package publicstate

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDefiniteOutcomeOpensOnlyExactState(t *testing.T) {
	t.Parallel()

	coordinator := newTestCoordinator(t, 41)
	id := uuid.MustParse("00000000-0000-0000-0000-000000000008")
	lease, err := coordinator.AcquireResume(context.Background(), id, 7, RepresentationJSON)
	if err != nil {
		t.Fatalf("AcquireResume() error = %v", err)
	}
	lease.Release()
	generation := int64(41)
	transition := beginAndClose(t, coordinator, Plan{
		DiscoveryGeneration: &generation,
		Resumes:             []ResumeTarget{{ID: id, ExpectedRevision: 7, Class: NonDraining}},
	})
	if err := transition.Commit(CommittedState{ResumeRevisions: map[uuid.UUID]int64{id: 8}}); !errors.Is(err, errTransitionState) {
		t.Fatalf("Commit(incomplete proof) error = %v, want transition state error", err)
	}
	next := int64(42)
	if err := transition.Commit(CommittedState{
		DiscoveryGeneration: &next,
		ResumeRevisions:     map[uuid.UUID]int64{id: 8},
	}); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if _, err := coordinator.AcquireResume(context.Background(), id, 7, RepresentationJSON); err == nil {
		t.Fatal("AcquireResume(old) error = nil, want mismatch")
	}
	if _, err := coordinator.AcquireDiscovery(context.Background(), 41, RepresentationSitemap); err == nil {
		t.Fatal("AcquireDiscovery(old) error = nil, want mismatch")
	}
	if _, err := coordinator.AcquireResume(context.Background(), id, 8, RepresentationJSON); err != nil {
		t.Fatalf("AcquireResume(committed) error = %v", err)
	}
}

func TestAmbiguousEvidenceControlsAdmissionAndReadiness(t *testing.T) {
	t.Parallel()

	coordinator := newTestCoordinator(t, 41)
	id := uuid.MustParse("00000000-0000-0000-0000-000000000009")
	transition := beginAndClose(t, coordinator, Plan{Resumes: []ResumeTarget{{
		ID: id, ExpectedRevision: 7, Class: NonDraining,
	}}})
	cause := errors.New("database unavailable")
	resolver := recoveryResolverFunc(func(context.Context) (RecoveryProof, error) {
		return RecoveryProof{}, cause
	})
	recoverErr := transition.Recover(context.Background(), resolver)
	var unresolved *RecoveryUnresolvedError
	if !errors.As(recoverErr, &unresolved) || !errors.Is(recoverErr, cause) {
		t.Fatalf("Recover() error = %v, want RecoveryUnresolvedError wrapping %v", recoverErr, cause)
	}
	if _, acquireErr := coordinator.AcquireResume(context.Background(), id, 7, RepresentationJSON); !errors.Is(acquireErr, ErrAdmissionClosed) {
		t.Fatalf("AcquireResume(unresolved) error = %v, want ErrAdmissionClosed", acquireErr)
	}
	if readyErr := coordinator.Ready(); readyErr == nil {
		t.Fatal("Ready() error = nil, want unresolved recovery failure")
	}
	if retryErr := transition.Recover(context.Background(), recoveryResolverFunc(func(context.Context) (RecoveryProof, error) {
		return RecoveryProof{
			Disposition: RecoveryNotCommitted,
			State:       CommittedState{ResumeRevisions: map[uuid.UUID]int64{id: 7}},
		}, nil
	})); retryErr != nil {
		t.Fatalf("Recover(definite non-commit) error = %v", retryErr)
	}
	if readyErr := coordinator.Ready(); readyErr != nil {
		t.Fatalf("Ready() after proof error = %v", readyErr)
	}
	lease, err := coordinator.AcquireResume(context.Background(), id, 7, RepresentationJSON)
	if err != nil {
		t.Fatalf("AcquireResume(recovered) error = %v", err)
	}
	lease.Release()
}

func TestRecoveryRejectsMixedNonCommitProof(t *testing.T) {
	t.Parallel()

	coordinator := newTestCoordinator(t, 41)
	id := uuid.MustParse("00000000-0000-0000-0000-000000000012")
	transition := beginAndClose(t, coordinator, Plan{Resumes: []ResumeTarget{{
		ID: id, ExpectedRevision: 7, Class: NonDraining,
	}}})
	err := transition.Recover(context.Background(), recoveryResolverFunc(func(context.Context) (RecoveryProof, error) {
		return RecoveryProof{
			Disposition: RecoveryNotCommitted,
			State:       CommittedState{ResumeRevisions: map[uuid.UUID]int64{id: 8}},
		}, nil
	}))
	var unresolved *RecoveryUnresolvedError
	if !errors.As(err, &unresolved) {
		t.Fatalf("Recover(mixed non-commit proof) error = %v, want RecoveryUnresolvedError", err)
	}
	if err := coordinator.Ready(); err == nil {
		t.Fatal("Ready() after mixed proof error = nil, want unresolved recovery failure")
	}
}

func TestRecoveryRejectsProofForAnUnrelatedFence(t *testing.T) {
	t.Parallel()

	coordinator := newTestCoordinator(t, 41)
	id := uuid.MustParse("00000000-0000-0000-0000-000000000014")
	unrelated := uuid.MustParse("00000000-0000-0000-0000-000000000015")
	transition := beginAndClose(t, coordinator, Plan{Resumes: []ResumeTarget{{
		ID: id, ExpectedRevision: 7, Class: NonDraining,
	}}})
	err := transition.Recover(context.Background(), recoveryResolverFunc(func(context.Context) (RecoveryProof, error) {
		return RecoveryProof{
			Disposition: RecoveryCommitted,
			State: CommittedState{ResumeRevisions: map[uuid.UUID]int64{
				id: 8, unrelated: 12,
			}},
		}, nil
	}))
	var unresolved *RecoveryUnresolvedError
	if !errors.As(err, &unresolved) {
		t.Fatalf("Recover(unrelated proof) error = %v, want RecoveryUnresolvedError", err)
	}
	if err := coordinator.Ready(); err == nil {
		t.Fatal("Ready() after unrelated proof error = nil, want unresolved recovery failure")
	}
}

type recoveryResolverFunc func(context.Context) (RecoveryProof, error)

func (f recoveryResolverFunc) Resolve(ctx context.Context) (RecoveryProof, error) { return f(ctx) }

// TestCloseCancelsEveryLeaseBeforeReadingDrainClock pins the ordering inside
// Close that a canceled-cache-hit caller depends on for deterministic
// synchronization: every affected lease is canceled before Close reads the
// coordinator clock to arm the drain timer. A caller that only observes
// admission closing (ErrAdmissionClosed from AcquireResume) races that same
// cancel loop and cannot prove a specific already-admitted lease is canceled;
// only this clock read can, and only because of the order asserted here. If
// a future change reads the clock earlier, this test fails instead of the
// race silently reopening.
func TestCloseCancelsEveryLeaseBeforeReadingDrainClock(t *testing.T) {
	t.Parallel()

	var canceled atomic.Bool
	var clockCalls atomic.Int32
	var sawCanceledBeforeClock atomic.Bool
	fixedNow := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	coordinator, err := NewCoordinator(CoordinatorConfig{
		DiscoveryGeneration: 1,
		Now: func() time.Time {
			clockCalls.Add(1)
			if canceled.Load() {
				sawCanceledBeforeClock.Store(true)
			}
			return fixedNow
		},
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	id := uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	lease, err := coordinator.AcquireResume(context.Background(), id, 1, RepresentationPDF)
	if err != nil {
		t.Fatalf("AcquireResume() error = %v", err)
	}
	// The hook stands in for a handler that stops serving and releases its
	// lease as soon as it observes cancellation, the same reaction the
	// canceled-cache-hit path requires.
	if hookErr := lease.OnCancel(func() {
		canceled.Store(true)
		lease.Release()
	}); hookErr != nil {
		t.Fatalf("OnCancel() error = %v", hookErr)
	}
	transition, err := coordinator.Begin(context.Background(), Plan{Resumes: []ResumeTarget{{
		ID: id, ExpectedRevision: 1, Class: Revoking,
	}}})
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := transition.Close(context.Background(), fixedNow.Add(time.Second)); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if clockCalls.Load() == 0 {
		t.Fatal("Close never read the drain clock; this test no longer exercises the ordering it checks")
	}
	if !sawCanceledBeforeClock.Load() {
		t.Fatal("Close read the drain deadline clock before its cancel loop finished canceling every lease")
	}
	if err := transition.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
}

func TestRecoveryErrorIsStableWithoutSecrets(t *testing.T) {
	t.Parallel()

	deadline := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := (&DrainTimeoutError{Deadline: deadline}).Error(); got == "" {
		t.Fatal("DrainTimeoutError.Error() returned an empty error")
	}
}
