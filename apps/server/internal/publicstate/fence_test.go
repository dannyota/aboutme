package publicstate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNonDrainingCommitLetsOldLeaseFinishAndRejectsNewOldLease(t *testing.T) {
	t.Parallel()

	coordinator := newTestCoordinator(t, 41)
	id := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	oldLease, err := coordinator.AcquireResume(context.Background(), id, 7, RepresentationJSON)
	if err != nil {
		t.Fatalf("AcquireResume(old) error = %v", err)
	}

	transition, err := coordinator.Begin(context.Background(), Plan{Resumes: []ResumeTarget{{
		ID: id, ExpectedRevision: 7, Class: NonDraining,
	}}})
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if closeErr := transition.Close(context.Background(), time.Now().Add(time.Second)); closeErr != nil {
		t.Fatalf("Close() error = %v", closeErr)
	}
	if _, acquireErr := coordinator.AcquireResume(context.Background(), id, 7, RepresentationJSON); !errors.Is(acquireErr, ErrAdmissionClosed) {
		t.Fatalf("AcquireResume(closed old) error = %v, want ErrAdmissionClosed", acquireErr)
	}
	if commitErr := transition.Commit(CommittedState{ResumeRevisions: map[uuid.UUID]int64{id: 8}}); commitErr != nil {
		t.Fatalf("Commit() error = %v", commitErr)
	}
	if contextErr := oldLease.Context().Err(); contextErr != nil {
		t.Fatalf("old Lease Context() error = %v, want active", contextErr)
	}
	newLease, err := coordinator.AcquireResume(context.Background(), id, 8, RepresentationJSON)
	if err != nil {
		t.Fatalf("AcquireResume(new) error = %v", err)
	}
	newLease.Release()
	oldLease.Release()
}

func TestLaterRevocationDrainsRetainedGenerationSets(t *testing.T) {
	t.Parallel()

	coordinator := newTestCoordinator(t, 41)
	id := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	oldLease, err := coordinator.AcquireResume(context.Background(), id, 7, RepresentationJSON)
	if err != nil {
		t.Fatalf("AcquireResume(old) error = %v", err)
	}
	advance := beginAndClose(t, coordinator, Plan{Resumes: []ResumeTarget{{ID: id, ExpectedRevision: 7, Class: NonDraining}}})
	if commitErr := advance.Commit(CommittedState{ResumeRevisions: map[uuid.UUID]int64{id: 8}}); commitErr != nil {
		t.Fatalf("Commit(non-draining) error = %v", commitErr)
	}
	currentLease, err := coordinator.AcquireResume(context.Background(), id, 8, RepresentationHTML)
	if err != nil {
		t.Fatalf("AcquireResume(current) error = %v", err)
	}

	transition, err := coordinator.Begin(context.Background(), Plan{Resumes: []ResumeTarget{{ID: id, ExpectedRevision: 8, Class: Revoking}}})
	if err != nil {
		t.Fatalf("Begin(revoking) error = %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- transition.Close(context.Background(), time.Now().Add(time.Second)) }()
	waitCanceled(t, oldLease)
	waitCanceled(t, currentLease)
	oldLease.Release()
	currentLease.Release()
	if err := <-done; err != nil {
		t.Fatalf("Close(revoking) error = %v", err)
	}
	if err := transition.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
}

func TestDiscoveryChangeDrainsAllGlobalGenerations(t *testing.T) {
	t.Parallel()

	coordinator := newTestCoordinator(t, 41)
	lease, err := coordinator.AcquireDiscovery(context.Background(), 41, RepresentationSitemap)
	if err != nil {
		t.Fatalf("AcquireDiscovery() error = %v", err)
	}
	generation := int64(41)
	transition, err := coordinator.Begin(context.Background(), Plan{DiscoveryGeneration: &generation})
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- transition.Close(context.Background(), time.Now().Add(time.Second)) }()
	waitCanceled(t, lease)
	lease.Release()
	if err := <-done; err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	next := int64(42)
	if err := transition.Commit(CommittedState{DiscoveryGeneration: &next}); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	if _, err := coordinator.AcquireDiscovery(context.Background(), 41, RepresentationSitemap); err == nil {
		t.Fatal("AcquireDiscovery(old) error = nil, want mismatch")
	}
}

func TestOneDeadlineCoversAllFencesAndHandlers(t *testing.T) {
	t.Parallel()

	coordinator := newTestCoordinator(t, 41)
	first := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	second := uuid.MustParse("00000000-0000-0000-0000-000000000004")
	globalLease, err := coordinator.AcquireDiscovery(context.Background(), 41, RepresentationLLMS)
	if err != nil {
		t.Fatalf("AcquireDiscovery() error = %v", err)
	}
	firstLease, err := coordinator.AcquireResume(context.Background(), first, 7, RepresentationJSON)
	if err != nil {
		t.Fatalf("AcquireResume(first) error = %v", err)
	}
	secondLease, err := coordinator.AcquireResume(context.Background(), second, 9, RepresentationPhoto)
	if err != nil {
		t.Fatalf("AcquireResume(second) error = %v", err)
	}
	generation := int64(41)
	transition, err := coordinator.Begin(context.Background(), Plan{
		DiscoveryGeneration: &generation,
		Resumes: []ResumeTarget{
			{ID: second, ExpectedRevision: 9, Class: Revoking},
			{ID: first, ExpectedRevision: 7, Class: Revoking},
		},
	})
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	deadline := time.Now().Add(-time.Second)
	err = transition.Close(context.Background(), deadline)
	var timeout *DrainTimeoutError
	if !errors.As(err, &timeout) || !timeout.Deadline.Equal(deadline) {
		t.Fatalf("Close() error = %v, want DrainTimeoutError at %s", err, deadline)
	}
	waitCanceled(t, globalLease)
	waitCanceled(t, firstLease)
	waitCanceled(t, secondLease)
	for _, target := range []struct {
		id       uuid.UUID
		revision int64
	}{
		{first, 7}, {second, 9},
	} {
		lease, acquireErr := coordinator.AcquireResume(context.Background(), target.id, target.revision, RepresentationJSON)
		if acquireErr != nil {
			t.Fatalf("AcquireResume(reopened %s) error = %v", target.id, acquireErr)
		}
		lease.Release()
	}
	lease, err := coordinator.AcquireDiscovery(context.Background(), 41, RepresentationSitemap)
	if err != nil {
		t.Fatalf("AcquireDiscovery(reopened) error = %v", err)
	}
	lease.Release()
	globalLease.Release()
	firstLease.Release()
	secondLease.Release()
}

func TestGlobalThenUUIDOrderSupportsThreeResumeCaller(t *testing.T) {
	t.Parallel()

	coordinator := newTestCoordinator(t, 41)
	ids := []uuid.UUID{
		uuid.MustParse("00000000-0000-0000-0000-000000000007"),
		uuid.MustParse("00000000-0000-0000-0000-000000000005"),
		uuid.MustParse("00000000-0000-0000-0000-000000000006"),
	}
	for _, id := range ids {
		lease, err := coordinator.AcquireResume(context.Background(), id, 1, RepresentationJSON)
		if err != nil {
			t.Fatalf("AcquireResume(%s) error = %v", id, err)
		}
		lease.Release()
	}
	generation := int64(41)
	transition, err := coordinator.Begin(context.Background(), Plan{
		DiscoveryGeneration: &generation,
		Resumes: []ResumeTarget{
			{ID: ids[0], ExpectedRevision: 1, Class: NonDraining},
			{ID: ids[1], ExpectedRevision: 1, Class: NonDraining},
			{ID: ids[2], ExpectedRevision: 1, Class: NonDraining},
		},
	})
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := transition.Close(context.Background(), time.Now().Add(time.Second)); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	next := int64(42)
	if err := transition.Commit(CommittedState{
		DiscoveryGeneration: &next,
		ResumeRevisions:     map[uuid.UUID]int64{ids[0]: 2, ids[1]: 2, ids[2]: 2},
	}); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
}

func TestRevokingCloseRejectsClosingAndRetiredFence(t *testing.T) {
	t.Parallel()

	coordinator := newTestCoordinator(t, 41)
	id := uuid.MustParse("00000000-0000-0000-0000-000000000013")
	lease, err := coordinator.AcquireResume(context.Background(), id, 7, RepresentationHTML)
	if err != nil {
		t.Fatalf("AcquireResume() error = %v", err)
	}
	transition, err := coordinator.Begin(context.Background(), Plan{Resumes: []ResumeTarget{{
		ID: id, ExpectedRevision: 7, Class: Revoking,
	}}})
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- transition.Close(context.Background(), time.Now().Add(time.Second)) }()
	waitCanceled(t, lease)
	if _, err := coordinator.AcquireResume(context.Background(), id, 7, RepresentationHTML); !errors.Is(err, ErrAdmissionClosed) {
		t.Fatalf("AcquireResume(closing) error = %v, want ErrAdmissionClosed", err)
	}
	lease.Release()
	if err := <-done; err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := transition.Commit(CommittedState{RetiredResumes: []uuid.UUID{id}}); err != nil {
		t.Fatalf("Commit(retired) error = %v", err)
	}
	if _, err := coordinator.AcquireResume(context.Background(), id, 7, RepresentationHTML); !errors.Is(err, ErrAdmissionClosed) {
		t.Fatalf("AcquireResume(retired) error = %v, want ErrAdmissionClosed", err)
	}
}

func TestPreCloseRollbackDoesNotReplaceOrRetainFenceSets(t *testing.T) {
	t.Parallel()

	coordinator := newTestCoordinator(t, 41)
	id := uuid.MustParse("00000000-0000-0000-0000-000000000017")
	lease, err := coordinator.AcquireResume(context.Background(), id, 7, RepresentationJSON)
	if err != nil {
		t.Fatalf("AcquireResume() error = %v", err)
	}
	lease.Release()
	fence := resumeFence(t, coordinator, id)
	original := fenceCurrentSet(t, fence)

	for range 3 {
		transition, beginErr := coordinator.Begin(context.Background(), Plan{Resumes: []ResumeTarget{{
			ID: id, ExpectedRevision: 7, Class: NonDraining,
		}}})
		if beginErr != nil {
			t.Fatalf("Begin() error = %v", beginErr)
		}
		if rollbackErr := transition.Rollback(); rollbackErr != nil {
			t.Fatalf("Rollback(before Close) error = %v", rollbackErr)
		}
		assertFenceCurrentAndSetCount(t, fence, original, 1)
	}

	transition, err := coordinator.Begin(context.Background(), Plan{Resumes: []ResumeTarget{{
		ID: id, ExpectedRevision: 7, Class: NonDraining,
	}}})
	if err != nil {
		t.Fatalf("Begin(pre-canceled Close) error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if closeErr := transition.Close(ctx, time.Now().Add(time.Second)); !errors.Is(closeErr, context.Canceled) {
		t.Fatalf("Close(pre-canceled) error = %v, want context.Canceled", closeErr)
	}
	assertFenceCurrentAndSetCount(t, fence, original, 1)
	lease, err = coordinator.AcquireResume(context.Background(), id, 7, RepresentationJSON)
	if err != nil {
		t.Fatalf("AcquireResume(exact after pre-close abort) error = %v", err)
	}
	lease.Release()
}

func TestClosedTransitionsDiscardEmptyPriorSetsAndRetainActiveOldSet(t *testing.T) {
	t.Parallel()

	coordinator := newTestCoordinator(t, 41)
	id := uuid.MustParse("00000000-0000-0000-0000-000000000018")
	fence := resumeFenceAfterAdmission(t, coordinator, id, 7)
	for revision := int64(7); revision < 10; revision++ {
		transition := beginAndClose(t, coordinator, Plan{Resumes: []ResumeTarget{{
			ID: id, ExpectedRevision: revision, Class: NonDraining,
		}}})
		if err := transition.Commit(CommittedState{ResumeRevisions: map[uuid.UUID]int64{id: revision + 1}}); err != nil {
			t.Fatalf("Commit(%d) error = %v", revision, err)
		}
		if got := fenceSetCount(t, fence); got != 1 {
			t.Fatalf("set count after no-reader Commit(%d) = %d, want 1", revision, got)
		}
	}
	oldLease, err := coordinator.AcquireResume(context.Background(), id, 10, RepresentationJSON)
	if err != nil {
		t.Fatalf("AcquireResume(old) error = %v", err)
	}
	transition := beginAndClose(t, coordinator, Plan{Resumes: []ResumeTarget{{
		ID: id, ExpectedRevision: 10, Class: NonDraining,
	}}})
	if err := transition.Commit(CommittedState{ResumeRevisions: map[uuid.UUID]int64{id: 11}}); err != nil {
		t.Fatalf("Commit(active old) error = %v", err)
	}
	if got := fenceSetCount(t, fence); got != 2 {
		t.Fatalf("set count with active old lease = %d, want 2", got)
	}
	oldLease.Release()
	if got := fenceSetCount(t, fence); got != 1 {
		t.Fatalf("set count after old lease Release() = %d, want 1", got)
	}
}

func resumeFenceAfterAdmission(t *testing.T, coordinator *Coordinator, id uuid.UUID, revision int64) *fence {
	t.Helper()
	lease, err := coordinator.AcquireResume(context.Background(), id, revision, RepresentationJSON)
	if err != nil {
		t.Fatalf("AcquireResume() error = %v", err)
	}
	lease.Release()
	return resumeFence(t, coordinator, id)
}

func resumeFence(t *testing.T, coordinator *Coordinator, id uuid.UUID) *fence {
	t.Helper()
	coordinator.mu.Lock()
	fence := coordinator.resumes[id]
	coordinator.mu.Unlock()
	if fence == nil {
		t.Fatal("resume fence is missing")
	}
	return fence
}

func fenceCurrentSet(t *testing.T, fence *fence) *leaseSet {
	t.Helper()
	fence.mu.Lock()
	defer fence.mu.Unlock()
	return fence.current
}

func assertFenceCurrentAndSetCount(t *testing.T, fence *fence, wantCurrent *leaseSet, wantCount int) {
	t.Helper()
	fence.mu.Lock()
	defer fence.mu.Unlock()
	if fence.current != wantCurrent || len(fence.sets) != wantCount {
		t.Fatalf("fence current/set count = %p/%d, want %p/%d", fence.current, len(fence.sets), wantCurrent, wantCount)
	}
}

func fenceSetCount(t *testing.T, fence *fence) int {
	t.Helper()
	fence.mu.Lock()
	defer fence.mu.Unlock()
	return len(fence.sets)
}

func beginAndClose(t *testing.T, coordinator *Coordinator, plan Plan) *Transition {
	t.Helper()
	transition, err := coordinator.Begin(context.Background(), plan)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if err := transition.Close(context.Background(), time.Now().Add(time.Second)); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return transition
}

func waitCanceled(t *testing.T, lease *Lease) {
	t.Helper()
	<-lease.Context().Done()
}
