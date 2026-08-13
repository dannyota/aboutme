package media

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAdmissionAllowsOneConcurrentPhoto(t *testing.T) {
	admission := newPhotoAdmission(25 * time.Millisecond)
	release, err := admission.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}

	started := time.Now()
	if _, acquireErr := admission.Acquire(context.Background()); !errors.Is(acquireErr, ErrMediaBusy) {
		t.Fatalf("second Acquire error = %v, want ErrMediaBusy", acquireErr)
	}
	if elapsed := time.Since(started); elapsed < 20*time.Millisecond {
		t.Fatalf("busy admission returned after %v, want it to wait", elapsed)
	}

	release()
	releaseAgain, err := admission.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
	releaseAgain()
}

func TestAdmissionReleaseIsIdempotent(t *testing.T) {
	admission := newPhotoAdmission(10 * time.Millisecond)
	release, err := admission.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	release()
	release()

	releaseNext, err := admission.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire after duplicate release: %v", err)
	}
	releaseNext()
}

func TestAdmissionHonorsCallerCancellation(t *testing.T) {
	admission := newPhotoAdmission(time.Second)
	release, err := admission.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if _, err := admission.Acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Acquire error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("canceled admission took %v", elapsed)
	}
}

func TestAdmissionRejectsPreCanceledCallerWhenPermitIsFree(t *testing.T) {
	admission := newPhotoAdmission(time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if release, err := admission.Acquire(ctx); !errors.Is(err, context.Canceled) {
		if release != nil {
			release()
		}
		t.Fatalf("Acquire error = %v, want context.Canceled", err)
	}
}

func TestAdmissionSerializesConcurrentCallers(t *testing.T) {
	admission := newPhotoAdmission(time.Second)
	var active atomic.Int32
	var maximum atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})

	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			release, err := admission.Acquire(context.Background())
			if err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}
			n := active.Add(1)
			for {
				old := maximum.Load()
				if n <= old || maximum.CompareAndSwap(old, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			active.Add(-1)
			release()
		}()
	}
	close(start)
	wg.Wait()
	if got := maximum.Load(); got != 1 {
		t.Fatalf("maximum concurrent holders = %d, want 1", got)
	}
}

func TestAdmissionUsesOneSecondProductionWait(t *testing.T) {
	if got := NewPhotoAdmission().wait; got != time.Second {
		t.Fatalf("production wait = %v, want 1s", got)
	}
}
