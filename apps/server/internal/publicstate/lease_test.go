package publicstate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLeaseReleaseIsIdempotentAndCancelHookIsSynchronous(t *testing.T) {
	t.Parallel()

	coordinator := newTestCoordinator(t, 41)
	id := uuid.MustParse("00000000-0000-0000-0000-000000000010")
	lease, err := coordinator.AcquireResume(context.Background(), id, 7, RepresentationPhoto)
	if err != nil {
		t.Fatalf("AcquireResume() error = %v", err)
	}
	canceled := make(chan struct{}, 1)
	if hookErr := lease.OnCancel(func() { canceled <- struct{}{} }); hookErr != nil {
		t.Fatalf("OnCancel() error = %v", hookErr)
	}
	transition, err := coordinator.Begin(context.Background(), Plan{Resumes: []ResumeTarget{{
		ID: id, ExpectedRevision: 7, Class: Revoking,
	}}})
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- transition.Close(context.Background(), time.Now().Add(time.Second)) }()
	<-canceled
	if err := lease.Context().Err(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Lease Context() error = %v, want context.Canceled", err)
	}
	lease.Release()
	lease.Release()
	if err := <-done; err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := transition.Rollback(); err != nil {
		t.Fatalf("Rollback() error = %v", err)
	}
}

func TestLeaseRejectsNilCancelHook(t *testing.T) {
	t.Parallel()

	coordinator := newTestCoordinator(t, 41)
	lease, err := coordinator.AcquireDiscovery(context.Background(), 41, RepresentationSitemap)
	if err != nil {
		t.Fatalf("AcquireDiscovery() error = %v", err)
	}
	defer lease.Release()
	if err := lease.OnCancel(nil); err == nil {
		t.Fatal("OnCancel(nil) error = nil, want error")
	}
}
