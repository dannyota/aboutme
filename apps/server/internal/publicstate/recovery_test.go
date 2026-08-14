package publicstate

import (
	"context"
	"errors"
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
	err := transition.Recover(context.Background(), resolver)
	var unresolved *RecoveryUnresolvedError
	if !errors.As(err, &unresolved) || !errors.Is(err, cause) {
		t.Fatalf("Recover() error = %v, want RecoveryUnresolvedError wrapping %v", err, cause)
	}
	if _, err := coordinator.AcquireResume(context.Background(), id, 7, RepresentationJSON); !errors.Is(err, ErrAdmissionClosed) {
		t.Fatalf("AcquireResume(unresolved) error = %v, want ErrAdmissionClosed", err)
	}
	if err := coordinator.Ready(); err == nil {
		t.Fatal("Ready() error = nil, want unresolved recovery failure")
	}
	if err := transition.Recover(context.Background(), recoveryResolverFunc(func(context.Context) (RecoveryProof, error) {
		return RecoveryProof{
			Disposition: RecoveryNotCommitted,
			State:       CommittedState{ResumeRevisions: map[uuid.UUID]int64{id: 7}},
		}, nil
	})); err != nil {
		t.Fatalf("Recover(definite non-commit) error = %v", err)
	}
	if err := coordinator.Ready(); err != nil {
		t.Fatalf("Ready() after proof error = %v", err)
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

func TestRecoveryErrorIsStableWithoutSecrets(t *testing.T) {
	t.Parallel()

	deadline := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := (&DrainTimeoutError{Deadline: deadline}).Error(); got == "" {
		t.Fatal("DrainTimeoutError.Error() returned an empty error")
	}
}
