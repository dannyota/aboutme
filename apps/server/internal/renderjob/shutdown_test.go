package renderjob

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestShutdownReturnsAtItsDeadlineWhileARenderIgnoresCancellation(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	rendererStarted := make(chan struct{})
	release := make(chan struct{})
	renderer := rendererFunc(func(context.Context, Navigation) ([]byte, error) {
		close(rendererStarted)
		<-release
		return []byte("late"), nil
	})
	queue, err := New(testConfig(renderer, clock))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	renderDone := make(chan error, 1)
	go func() {
		_, renderErr := queue.Render(context.Background(), Request{Format: PDF, Prepare: func(context.Context) (Snapshot, error) {
			return testSnapshot(uuid.New(), []byte("snapshot")), nil
		}})
		renderDone <- renderErr
	}()
	<-rendererStarted

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- queue.Shutdown(ctx) }()
	select {
	case shutdownErr := <-shutdownDone:
		if !errors.Is(shutdownErr, context.DeadlineExceeded) {
			t.Fatalf("Shutdown() error = %v, want deadline exceeded", shutdownErr)
		}
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("Shutdown waited past its deadline for a render that ignores cancellation")
	}

	if readyErr := queue.Ready(); !errors.Is(readyErr, ErrClosed) {
		t.Fatalf("Ready() after Shutdown = %v, want ErrClosed", readyErr)
	}
	if _, renderErr := queue.Render(context.Background(), Request{Format: PDF, Prepare: func(context.Context) (Snapshot, error) {
		return testSnapshot(uuid.New(), []byte("snapshot")), nil
	}}); !errors.Is(renderErr, ErrClosed) {
		t.Fatalf("Render() after Shutdown = %v, want ErrClosed", renderErr)
	}

	close(release)
	if renderErr := <-renderDone; !errors.Is(renderErr, ErrClosed) {
		t.Fatalf("late Render() error = %v, want ErrClosed", renderErr)
	}
	if closeErr := queue.Close(); closeErr != nil {
		t.Fatalf("Close() after the late finisher = %v", closeErr)
	}
	assertQueueEmpty(t, queue)
}

func TestShutdownWithoutActiveRendersReturnsAtOnce(t *testing.T) {
	t.Parallel()

	queue, err := New(testConfig(rendererFunc(func(context.Context, Navigation) ([]byte, error) {
		return nil, errors.New("unused")
	}), newFakeClock()))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if shutdownErr := queue.Shutdown(ctx); shutdownErr != nil {
		t.Fatalf("Shutdown() with no renders = %v, want nil", shutdownErr)
	}
}
