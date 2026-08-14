package publicstate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCoordinatorAcquireExactRevision(t *testing.T) {
	t.Parallel()

	coordinator, err := NewCoordinator(CoordinatorConfig{
		DiscoveryGeneration: 41,
		Now:                 func() time.Time { return time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}

	id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	lease, err := coordinator.AcquireResume(context.Background(), id, 7, RepresentationJSON)
	if err != nil {
		t.Fatalf("AcquireResume() error = %v", err)
	}
	lease.Release()

	_, err = coordinator.AcquireResume(context.Background(), id, 6, RepresentationJSON)
	var mismatch *GenerationMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("AcquireResume() error = %v, want GenerationMismatchError", err)
	}
	if mismatch.Expected != 6 || mismatch.Actual != 7 {
		t.Fatalf("GenerationMismatchError = %+v, want Expected: 6, Actual: 7", mismatch)
	}
}

func TestCoordinatorStartsWithExactPositiveDiscoveryGeneration(t *testing.T) {
	t.Parallel()

	if _, err := NewCoordinator(CoordinatorConfig{DiscoveryGeneration: 0}); err == nil {
		t.Fatal("NewCoordinator(zero generation) error = nil, want error")
	}
	coordinator := newTestCoordinator(t, 41)
	lease, err := coordinator.AcquireDiscovery(context.Background(), 41, RepresentationSitemap)
	if err != nil {
		t.Fatalf("AcquireDiscovery() error = %v", err)
	}
	lease.Release()
	if _, err := coordinator.AcquireDiscovery(context.Background(), 40, RepresentationSitemap); err == nil {
		t.Fatal("AcquireDiscovery(stale generation) error = nil, want mismatch")
	}
}

func TestCoordinatorWaitsForSingleTransitionOwner(t *testing.T) {
	t.Parallel()

	coordinator := newTestCoordinator(t, 41)
	id := uuid.MustParse("00000000-0000-0000-0000-000000000011")
	first, err := coordinator.Begin(context.Background(), Plan{Resumes: []ResumeTarget{{
		ID: id, ExpectedRevision: 7, Class: NonDraining,
	}}})
	if err != nil {
		t.Fatalf("first Begin() error = %v", err)
	}
	secondDone := make(chan *Transition, 1)
	secondErr := make(chan error, 1)
	go func() {
		transition, beginErr := coordinator.Begin(context.Background(), Plan{Resumes: []ResumeTarget{{
			ID: id, ExpectedRevision: 7, Class: NonDraining,
		}}})
		if beginErr != nil {
			secondErr <- beginErr
			return
		}
		secondDone <- transition
	}()
	if err := first.Rollback(); err != nil {
		t.Fatalf("first Rollback() error = %v", err)
	}
	select {
	case err := <-secondErr:
		t.Fatalf("second Begin() error = %v", err)
	case second := <-secondDone:
		if err := second.Rollback(); err != nil {
			t.Fatalf("second Rollback() error = %v", err)
		}
	}
}

func TestBeginDuplicateResumeDoesNotRetainTransitionOwner(t *testing.T) {
	t.Parallel()

	coordinator := newTestCoordinator(t, 41)
	id := uuid.MustParse("00000000-0000-0000-0000-000000000016")
	_, err := coordinator.Begin(context.Background(), Plan{Resumes: []ResumeTarget{
		{ID: id, ExpectedRevision: 7, Class: NonDraining},
		{ID: id, ExpectedRevision: 7, Class: NonDraining},
	}})
	if err == nil {
		t.Fatal("Begin(duplicate resume) error = nil, want error")
	}

	coordinator.mu.Lock()
	fence := coordinator.resumes[id]
	coordinator.mu.Unlock()
	fence.mu.Lock()
	retainedOwner := fence.owner
	fence.mu.Unlock()
	if retainedOwner {
		t.Fatal("Begin(duplicate resume) retained transition ownership")
	}

	transition, err := coordinator.Begin(context.Background(), Plan{Resumes: []ResumeTarget{{
		ID: id, ExpectedRevision: 7, Class: NonDraining,
	}}})
	if err != nil {
		t.Fatalf("Begin(valid resume) error = %v", err)
	}
	if err := transition.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
}

func newTestCoordinator(t *testing.T, discoveryGeneration int64) *Coordinator {
	t.Helper()
	coordinator, err := NewCoordinator(CoordinatorConfig{
		DiscoveryGeneration: discoveryGeneration,
		Now:                 time.Now,
	})
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	return coordinator
}
