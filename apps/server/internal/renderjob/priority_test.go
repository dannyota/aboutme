package renderjob

import (
	"bytes"
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

// A low-priority render enters only while at most LowPriorityMaxWaiting
// places behind the running render are taken, so owner exports keep the
// rest of the queue and readiness never fails because of background work.
func TestLowPriorityAdmissionLeavesPlacesForExports(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	resumeID := uuid.MustParse("10000000-0000-4000-8000-000000000031")
	queue, err := New(testConfig(rendererFunc(func(ctx context.Context, _ Navigation) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}), clock))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	var prepared atomic.Int64
	done := make(chan error, MaxAdmittedJobs)
	start := func(priority Priority) {
		go func() {
			_, renderErr := queue.Render(context.Background(), Request{Format: PDF, Priority: priority, Prepare: func(context.Context) (Snapshot, error) {
				prepared.Add(1)
				return testSnapshot(resumeID, []byte("snapshot")), nil
			}})
			done <- renderErr
		}()
	}
	rejectedLow := func() error {
		_, renderErr := queue.Render(context.Background(), Request{Format: Card, Priority: PriorityLow, Prepare: func(context.Context) (Snapshot, error) {
			t.Fatal("Prepare called after refused low-priority admission")
			return Snapshot{}, nil
		}})
		return renderErr
	}

	admittedLow := MaxConcurrentRenders + LowPriorityMaxWaiting + 1
	for range admittedLow {
		start(PriorityLow)
		want := prepared.Load() + 1
		waitFor(t, func() bool { return prepared.Load() == want })
	}
	if err := rejectedLow(); !errors.Is(err, ErrSaturated) {
		t.Fatalf("low-priority Render() with %d admitted = %v, want ErrSaturated", admittedLow, err)
	}
	if err := queue.Ready(); err != nil {
		t.Fatalf("Ready() with only low-priority work = %v, want nil", err)
	}
	for range MaxAdmittedJobs - admittedLow {
		start(PriorityNormal)
	}
	waitFor(t, func() bool { return prepared.Load() == MaxAdmittedJobs })
	if err := queue.Ready(); !errors.Is(err, ErrSaturated) {
		t.Fatalf("Ready() with every place taken = %v, want ErrSaturated", err)
	}
	if err := queue.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	for range MaxAdmittedJobs {
		if err := <-done; err == nil {
			t.Fatal("Render() error = nil after shutdown")
		}
	}
	assertQueueEmpty(t, queue)
}

func TestLowPriorityIsRefusedBehindQueuedExports(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	resumeID := uuid.MustParse("10000000-0000-4000-8000-000000000032")
	queue, err := New(testConfig(rendererFunc(func(ctx context.Context, _ Navigation) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}), clock))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	var prepared atomic.Int64
	done := make(chan error, 4)
	for range MaxConcurrentRenders + LowPriorityMaxWaiting + 1 {
		go func() {
			_, renderErr := queue.Render(context.Background(), Request{Format: PDF, Prepare: func(context.Context) (Snapshot, error) {
				prepared.Add(1)
				return testSnapshot(resumeID, []byte("snapshot")), nil
			}})
			done <- renderErr
		}()
	}
	waitFor(t, func() bool { return prepared.Load() == int64(MaxConcurrentRenders+LowPriorityMaxWaiting+1) })
	if _, err := queue.Render(context.Background(), Request{Format: Card, Priority: PriorityLow, Prepare: func(context.Context) (Snapshot, error) {
		t.Fatal("Prepare called after refused low-priority admission")
		return Snapshot{}, nil
	}}); !errors.Is(err, ErrSaturated) {
		t.Fatalf("low-priority Render() behind three exports = %v, want ErrSaturated", err)
	}
	if err := queue.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	for range MaxConcurrentRenders + LowPriorityMaxWaiting + 1 {
		<-done
	}
	assertQueueEmpty(t, queue)
}

func TestCardFormatHasItsOwnOutputLimit(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	resumeID := uuid.MustParse("10000000-0000-4000-8000-000000000033")
	config := testConfig(nil, clock)
	config.CardLimit = 8
	var queue *Queue
	var output []byte
	config.Renderer = rendererFunc(func(ctx context.Context, navigation Navigation) ([]byte, error) {
		if navigation.Format != Card {
			t.Fatalf("navigation format = %q, want %q", navigation.Format, Card)
		}
		if _, err := queue.Redeem(ctx, Redemption{ResumeID: resumeID, JobID: navigation.JobID, Audience: "nuxt-print", Capability: navigation.Capability}); err != nil {
			return nil, err
		}
		return output, nil
	})
	var err error
	queue, err = New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	render := func() (Result, error) {
		return queue.Render(context.Background(), Request{Format: Card, Priority: PriorityLow, Prepare: func(context.Context) (Snapshot, error) {
			return testSnapshot(resumeID, []byte(`{"kind":"card"}`)), nil
		}})
	}
	output = bytes.Repeat([]byte{1}, 8)
	if result, err := render(); err != nil || len(result.Bytes) != 8 {
		t.Fatalf("card at the limit = %d bytes, %v", len(result.Bytes), err)
	}
	output = bytes.Repeat([]byte{1}, 9)
	if _, err := render(); !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("card over the limit error = %v, want ErrOutputTooLarge", err)
	}
	if _, err := New(Config{Renderer: config.Renderer, CardLimit: CardMaxBytes + 1}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("New() with a card limit above CardMaxBytes = %v, want ErrInvalidRequest", err)
	}
	if _, err := queue.Render(context.Background(), Request{Format: Card, Priority: Priority(9), Prepare: func(context.Context) (Snapshot, error) {
		t.Fatal("Prepare called for an unknown priority")
		return Snapshot{}, nil
	}}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unknown priority error = %v, want ErrInvalidRequest", err)
	}
	assertQueueEmpty(t, queue)
}
