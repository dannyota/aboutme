package password

import (
	"context"
	"errors"
	"testing"
	"time"
)

// waitFor polls a condition with a deadline, failing the test if it never
// holds. It synchronizes the admission tests against the goroutines that are
// blocked waiting for a slot.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached within timeout")
		}
		time.Sleep(time.Millisecond)
	}
}

// fillAdmission takes both running slots and fills the 16-waiter queue,
// returning a channel of the waiters' results. Each waiter releases its slot on
// success, so the caller drains them by releasing the two running slots.
func fillAdmission(ctx context.Context, t *testing.T, a *Admission) chan error {
	t.Helper()
	if err := a.Acquire(ctx); err != nil {
		t.Fatalf("fill running slot: %v", err)
	}
	if err := a.Acquire(ctx); err != nil {
		t.Fatalf("fill running slot: %v", err)
	}
	results := make(chan error, admissionWaiting)
	for range admissionWaiting {
		go func() {
			err := a.Acquire(ctx)
			if err == nil {
				a.Release()
			}
			results <- err
		}()
	}
	waitFor(t, func() bool { return len(a.queue) == admissionWaiting })
	return results
}

func TestAdmissionTwoRunSixteenWaitSeventeenthFails(t *testing.T) {
	a := NewAdmission()
	ctx := context.Background()
	results := fillAdmission(ctx, t, a)

	// The seventeenth waiter fails immediately with ErrHashAdmission.
	if err := a.Acquire(ctx); !errors.Is(err, ErrHashAdmission) {
		t.Fatalf("seventeenth Acquire error = %v, want ErrHashAdmission", err)
	}

	// Releasing both running slots drains all sixteen waiters.
	a.Release()
	a.Release()
	for range admissionWaiting {
		if err := <-results; err != nil {
			t.Errorf("waiter error = %v, want nil", err)
		}
	}
	if got := len(a.slots); got != admissionRunning {
		t.Errorf("free slots = %d, want %d after drain", got, admissionRunning)
	}
}

func TestAdmissionCancellationReleasesQueueSlot(t *testing.T) {
	a := NewAdmission()
	ctx := context.Background()
	if err := a.Acquire(ctx); err != nil {
		t.Fatalf("first Acquire error = %v", err)
	}
	if err := a.Acquire(ctx); err != nil {
		t.Fatalf("second Acquire error = %v", err)
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- a.Acquire(cancelCtx) }()
	waitFor(t, func() bool { return len(a.queue) == 1 })

	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("canceled Acquire error = %v, want context.Canceled", err)
	}
	waitFor(t, func() bool { return len(a.queue) == 0 })
	if got := len(a.slots); got != 0 {
		t.Errorf("free slots = %d, want 0 (cancellation must not release a running slot)", got)
	}
}

func TestAdmissionAlreadyCanceledContext(t *testing.T) {
	t.Parallel()
	a := NewAdmission()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := a.Acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Acquire error = %v, want context.Canceled", err)
	}
	if got := len(a.slots); got != admissionRunning {
		t.Errorf("free slots = %d, want %d (unchanged)", got, admissionRunning)
	}
	if got := len(a.queue); got != 0 {
		t.Errorf("queued waiters = %d, want 0", got)
	}
}

func TestAdmissionReleaseRestoresAllSlots(t *testing.T) {
	a := NewAdmission()
	ctx := context.Background()

	for i := 0; i < 50; i++ {
		if err := a.Acquire(ctx); err != nil {
			t.Fatalf("Acquire %d error = %v", i, err)
		}
		a.Release()
	}
	if got := len(a.slots); got != admissionRunning {
		t.Errorf("free slots = %d, want %d after balanced acquire/release", got, admissionRunning)
	}
}
