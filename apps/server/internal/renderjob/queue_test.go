package renderjob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

var testEpoch = time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)

type rendererFunc func(context.Context, Navigation) ([]byte, error)

func (f rendererFunc) Render(ctx context.Context, navigation Navigation) ([]byte, error) {
	return f(ctx, navigation)
}

type fakeTimer struct {
	clock   *fakeClock
	when    time.Time
	fn      func()
	stopped bool
}

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	if t.stopped {
		return false
	}
	t.stopped = true
	return true
}

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

func newFakeClock() *fakeClock { return &fakeClock{now: testEpoch} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(delay time.Duration, fn func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	timer := &fakeTimer{clock: c, when: c.now.Add(delay), fn: fn}
	c.timers = append(c.timers, timer)
	return timer
}

func (c *fakeClock) Advance(delay time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(delay)
	var callbacks []func()
	for _, timer := range c.timers {
		if !timer.stopped && !timer.when.After(c.now) {
			timer.stopped = true
			callbacks = append(callbacks, timer.fn)
		}
	}
	c.mu.Unlock()
	for _, callback := range callbacks {
		callback()
	}
}

type sequentialUUIDs struct{ next atomic.Uint64 }

func (s *sequentialUUIDs) New() (uuid.UUID, error) {
	value := s.next.Add(1)
	return uuid.MustParse("00000000-0000-4000-8000-" + leftPad12(value)), nil
}

func leftPad12(value uint64) string {
	const digits = "0123456789"
	result := [12]byte{'0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0', '0'}
	for index := len(result) - 1; value > 0; index-- {
		result[index] = digits[value%10]
		value /= 10
	}
	return string(result[:])
}

func testConfig(renderer Renderer, clock *fakeClock) Config {
	ids := &sequentialUUIDs{}
	return Config{
		Renderer:  renderer,
		Entropy:   bytes.NewReader(bytes.Repeat([]byte{0x42}, 64*1024)),
		NewUUID:   ids.New,
		Now:       clock.Now,
		AfterFunc: clock.AfterFunc,
	}
}

func testSnapshot(id uuid.UUID, payload []byte) Snapshot {
	return Snapshot{ResumeID: id, Revision: 7, SchemaVersion: 2, Payload: payload}
}

func TestRenderRedeemsOneUseCapabilityAndReturnsDetachedResult(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	resumeID := uuid.MustParse("10000000-0000-4000-8000-000000000001")
	original := []byte(`{"title":"frozen"}`)
	var queue *Queue
	renderer := rendererFunc(func(ctx context.Context, navigation Navigation) ([]byte, error) {
		if navigation.ResumeID != resumeID || navigation.JobID == uuid.Nil || navigation.Format != PDF {
			t.Fatalf("navigation = %+v", navigation)
		}
		snapshot, err := queue.Redeem(ctx, Redemption{
			ResumeID: resumeID, JobID: navigation.JobID, Audience: "nuxt-print", Capability: navigation.Capability,
		})
		if err != nil {
			t.Fatalf("Redeem() error = %v", err)
		}
		snapshot.Payload[0] = 'X'
		if _, err := queue.Redeem(ctx, Redemption{
			ResumeID: resumeID, JobID: navigation.JobID, Audience: "nuxt-print", Capability: navigation.Capability,
		}); !errors.Is(err, ErrNotActive) {
			t.Fatalf("Redeem(replay) error = %v, want ErrNotActive", err)
		}
		return []byte("pdf bytes"), nil
	})
	var err error
	queue, err = New(testConfig(renderer, clock))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	result, err := queue.Render(context.Background(), Request{
		Format: PDF,
		Prepare: func(context.Context) (Snapshot, error) {
			return testSnapshot(resumeID, original), nil
		},
	})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if string(result.Bytes) != "pdf bytes" || result.Digest != sha256.Sum256([]byte("pdf bytes")) || result.Revision != 7 {
		t.Fatalf("result = %+v", result)
	}
	result.Bytes[0] = 'X'
	if string(original) != `{"title":"frozen"}` {
		t.Fatalf("preparation payload mutated: %q", original)
	}
	if err := queue.Ready(); err != nil {
		t.Fatalf("Ready() after completion error = %v", err)
	}
	assertQueueEmpty(t, queue)
}

func TestAdmissionBoundsNineJobsAndSerializesRenderer(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	resumeID := uuid.MustParse("10000000-0000-4000-8000-000000000002")
	entered := make(chan struct{}, 9)
	var active atomic.Int64
	var maximum atomic.Int64
	var queue *Queue
	renderer := rendererFunc(func(ctx context.Context, navigation Navigation) ([]byte, error) {
		snapshot, err := queue.Redeem(ctx, Redemption{
			ResumeID: resumeID, JobID: navigation.JobID, Audience: "nuxt-print", Capability: navigation.Capability,
		})
		if err != nil {
			return nil, err
		}
		_ = snapshot
		current := active.Add(1)
		for current > maximum.Load() && !maximum.CompareAndSwap(maximum.Load(), current) {
		}
		entered <- struct{}{}
		defer active.Add(-1)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	var err error
	queue, err = New(testConfig(renderer, clock))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	var prepared atomic.Int64
	done := make(chan error, 9)
	for range 9 {
		go func() {
			_, renderErr := queue.Render(context.Background(), Request{Format: PDF, Prepare: func(context.Context) (Snapshot, error) {
				prepared.Add(1)
				return testSnapshot(resumeID, []byte("snapshot")), nil
			}})
			done <- renderErr
		}()
	}
	waitFor(t, func() bool { return prepared.Load() == 9 })
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("renderer did not start")
	}
	_, err = queue.Render(context.Background(), Request{Format: PDF, Prepare: func(context.Context) (Snapshot, error) {
		t.Fatal("Prepare called after saturated admission")
		return Snapshot{}, nil
	}})
	if !errors.Is(err, ErrSaturated) {
		t.Fatalf("tenth Render() error = %v, want ErrSaturated", err)
	}
	if err := queue.Ready(); !errors.Is(err, ErrSaturated) {
		t.Fatalf("Ready() error = %v, want ErrSaturated", err)
	}
	if err := queue.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	for range 9 {
		if err := <-done; err == nil {
			t.Fatal("Render() error = nil after shutdown")
		}
	}
	if maximum.Load() != 1 {
		t.Fatalf("maximum concurrent renders = %d, want 1", maximum.Load())
	}
	if err := queue.Ready(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Ready() after Close error = %v, want ErrClosed", err)
	}
	assertQueueEmpty(t, queue)
}

func TestAuthorityRejectsWrongBindingsWithoutConsuming(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	resumeID := uuid.MustParse("10000000-0000-4000-8000-000000000003")
	var queue *Queue
	renderer := rendererFunc(func(ctx context.Context, navigation Navigation) ([]byte, error) {
		valid := Redemption{ResumeID: resumeID, JobID: navigation.JobID, Audience: "nuxt-print", Capability: navigation.Capability}
		wrong := []Redemption{
			{ResumeID: resumeID, JobID: navigation.JobID, Audience: "nuxt-print", Capability: "short"},
			{ResumeID: resumeID, JobID: navigation.JobID, Audience: "other", Capability: navigation.Capability},
			{ResumeID: uuid.New(), JobID: navigation.JobID, Audience: "nuxt-print", Capability: navigation.Capability},
			{ResumeID: resumeID, JobID: uuid.New(), Audience: "nuxt-print", Capability: navigation.Capability},
		}
		for _, redemption := range wrong {
			if _, err := queue.Redeem(ctx, redemption); !errors.Is(err, ErrNotActive) {
				t.Fatalf("Redeem(%+v) error = %v, want ErrNotActive", redemption, err)
			}
		}
		if _, err := queue.Redeem(ctx, valid); err != nil {
			t.Fatalf("Redeem(valid after rejection) error = %v", err)
		}
		return []byte("ok"), nil
	})
	var err error
	queue, err = New(testConfig(renderer, clock))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := queue.Render(context.Background(), Request{Format: PDF, Prepare: func(context.Context) (Snapshot, error) {
		return testSnapshot(resumeID, []byte("snapshot")), nil
	}}); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
}

func TestConcurrentRedemptionHasOneWinner(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	resumeID := uuid.MustParse("10000000-0000-4000-8000-000000000004")
	var queue *Queue
	renderer := rendererFunc(func(ctx context.Context, navigation Navigation) ([]byte, error) {
		redemption := Redemption{ResumeID: resumeID, JobID: navigation.JobID, Audience: "nuxt-print", Capability: navigation.Capability}
		start := make(chan struct{})
		results := make(chan error, 2)
		for range 2 {
			go func() {
				<-start
				_, redeemErr := queue.Redeem(ctx, redemption)
				results <- redeemErr
			}()
		}
		close(start)
		var success, inactive int
		for range 2 {
			switch redeemErr := <-results; {
			case redeemErr == nil:
				success++
			case errors.Is(redeemErr, ErrNotActive):
				inactive++
			default:
				t.Fatalf("Redeem() error = %v", redeemErr)
			}
		}
		if success != 1 || inactive != 1 {
			t.Fatalf("redemption outcomes: success=%d inactive=%d", success, inactive)
		}
		return []byte("ok"), nil
	})
	var err error
	queue, err = New(testConfig(renderer, clock))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := queue.Render(context.Background(), Request{Format: PDF, Prepare: func(context.Context) (Snapshot, error) {
		return testSnapshot(resumeID, []byte("snapshot")), nil
	}}); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
}

func TestDeadlineAdvancesWithoutAnotherQueueCall(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	resumeID := uuid.MustParse("10000000-0000-4000-8000-000000000005")
	started := make(chan struct{})
	exited := make(chan struct{})
	var queue *Queue
	renderer := rendererFunc(func(ctx context.Context, navigation Navigation) ([]byte, error) {
		if _, err := queue.Redeem(ctx, Redemption{ResumeID: resumeID, JobID: navigation.JobID, Audience: "nuxt-print", Capability: navigation.Capability}); err != nil {
			return nil, err
		}
		close(started)
		<-ctx.Done()
		close(exited)
		return nil, ctx.Err()
	})
	config := testConfig(renderer, clock)
	config.QueueDepth = 1
	config.JobTimeout = 5 * time.Second
	var err error
	queue, err = New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	done := make(chan error, 2)
	for range 2 {
		go func() {
			_, renderErr := queue.Render(context.Background(), Request{Format: PDF, Prepare: func(context.Context) (Snapshot, error) {
				return testSnapshot(resumeID, []byte("snapshot")), nil
			}})
			done <- renderErr
		}()
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("renderer did not start")
	}
	waitFor(t, func() bool {
		queue.mu.Lock()
		defer queue.mu.Unlock()
		return len(queue.jobs) == 2
	})
	clock.Advance(5 * time.Second)
	select {
	case <-exited:
	case <-time.After(time.Second):
		t.Fatal("deadline did not cancel active renderer")
	}
	for range 2 {
		if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Render() error = %v, want context deadline exceeded", err)
		}
	}
	assertQueueEmpty(t, queue)
}

func TestCapabilityExpiryBoundaryCancelsUnusedQueuedJob(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	resumeID := uuid.MustParse("10000000-0000-4000-8000-000000000006")
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var calls atomic.Int64
	var queue *Queue
	renderer := rendererFunc(func(ctx context.Context, navigation Navigation) ([]byte, error) {
		call := calls.Add(1)
		if call == 1 {
			if _, err := queue.Redeem(ctx, Redemption{ResumeID: resumeID, JobID: navigation.JobID, Audience: "nuxt-print", Capability: navigation.Capability}); err != nil {
				return nil, err
			}
			close(firstStarted)
			<-releaseFirst
			return []byte("first"), nil
		}
		t.Fatalf("expired queued job reached renderer")
		return nil, nil
	})
	config := testConfig(renderer, clock)
	config.QueueDepth = 1
	config.CapabilityTTL = time.Second
	config.JobTimeout = 10 * time.Second
	var err error
	queue, err = New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	done := make(chan error, 2)
	for range 2 {
		go func() {
			_, renderErr := queue.Render(context.Background(), Request{Format: PDF, Prepare: func(context.Context) (Snapshot, error) {
				return testSnapshot(resumeID, []byte("snapshot")), nil
			}})
			done <- renderErr
		}()
	}
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("first renderer did not start")
	}
	waitFor(t, func() bool {
		queue.mu.Lock()
		defer queue.mu.Unlock()
		return len(queue.jobs) == 2
	})
	clock.Advance(time.Second)
	close(releaseFirst)
	var success, inactive int
	for range 2 {
		switch err := <-done; {
		case err == nil:
			success++
		case errors.Is(err, ErrNotActive):
			inactive++
		default:
			t.Fatalf("Render() error = %v", err)
		}
	}
	if success != 1 || inactive != 1 || calls.Load() != 1 {
		t.Fatalf("outcomes success=%d inactive=%d renderer calls=%d", success, inactive, calls.Load())
	}
	assertQueueEmpty(t, queue)
}

func TestEntropyAndUUIDFailuresReleaseAdmissionWithoutLeakingCause(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	secret := "raw-secret-from-source"
	for _, test := range []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "entropy", mutate: func(config *Config) { config.Entropy = errorReader{errors.New(secret)} }},
		{name: "entropy panic", mutate: func(config *Config) { config.Entropy = panicReader{secret} }},
		{name: "uuid", mutate: func(config *Config) {
			config.NewUUID = func() (uuid.UUID, error) { return uuid.Nil, errors.New(secret) }
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := testConfig(rendererFunc(func(context.Context, Navigation) ([]byte, error) {
				t.Fatal("renderer called after source failure")
				return nil, nil
			}), clock)
			test.mutate(&config)
			queue, err := New(config)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			_, err = queue.Render(context.Background(), Request{Format: PDF, Prepare: func(context.Context) (Snapshot, error) {
				return testSnapshot(uuid.New(), []byte("snapshot")), nil
			}})
			if err == nil || bytes.Contains([]byte(err.Error()), []byte(secret)) {
				t.Fatalf("Render() error = %q, want sanitized failure", err)
			}
			assertQueueEmpty(t, queue)
		})
	}
}

func TestDependencyFailuresAndPanicsAreSanitizedAndReleaseCapacity(t *testing.T) {
	t.Parallel()

	const secret = "raw-document-or-browser-secret"
	clock := newFakeClock()
	resumeID := uuid.New()
	var queue *Queue
	renderer := rendererFunc(func(ctx context.Context, navigation Navigation) ([]byte, error) {
		if _, err := queue.Redeem(ctx, Redemption{
			ResumeID: resumeID, JobID: navigation.JobID, Audience: audiencePrint, Capability: navigation.Capability,
		}); err != nil {
			return nil, err
		}
		return nil, errors.New(secret)
	})
	var err error
	queue, err = New(testConfig(renderer, clock))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for _, test := range []struct {
		name    string
		prepare func(context.Context) (Snapshot, error)
		want    error
	}{
		{name: "prepare error", prepare: func(context.Context) (Snapshot, error) { return Snapshot{}, errors.New(secret) }, want: ErrPreparation},
		{name: "prepare panic", prepare: func(context.Context) (Snapshot, error) { panic(secret) }, want: ErrPreparation},
		{name: "renderer error", prepare: func(context.Context) (Snapshot, error) { return testSnapshot(resumeID, []byte("snapshot")), nil }, want: ErrRendering},
	} {
		_, err := queue.Render(context.Background(), Request{Format: PDF, Prepare: test.prepare})
		if !errors.Is(err, test.want) || bytes.Contains([]byte(err.Error()), []byte(secret)) {
			t.Fatalf("Render(%s) error = %q, want sanitized %v", test.name, err, test.want)
		}
		assertQueueEmpty(t, queue)
	}
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

type panicReader struct{ value string }

func (r panicReader) Read([]byte) (int, error) { panic(r.value) }

func TestSnapshotAndOutputBoundsAndPreparationCancellation(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	resumeID := uuid.MustParse("10000000-0000-4000-8000-000000000007")
	config := testConfig(rendererFunc(func(context.Context, Navigation) ([]byte, error) {
		return []byte("toolarge"), nil
	}), clock)
	config.SnapshotLimit = 8
	config.PDFLimit = 4
	config.PNGLimit = 4
	queue, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := queue.Render(context.Background(), Request{Format: PDF, Prepare: func(context.Context) (Snapshot, error) {
		return testSnapshot(resumeID, bytes.Repeat([]byte("x"), 9)), nil
	}}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("oversize snapshot error = %v, want ErrInvalidRequest", err)
	}
	var queueForRenderer = queue
	queue.renderer = rendererFunc(func(ctx context.Context, navigation Navigation) ([]byte, error) {
		if _, err := queueForRenderer.Redeem(ctx, Redemption{ResumeID: resumeID, JobID: navigation.JobID, Audience: "nuxt-print", Capability: navigation.Capability}); err != nil {
			return nil, err
		}
		return []byte("toolarge"), nil
	})
	if _, err := queue.Render(context.Background(), Request{Format: PDF, Prepare: func(context.Context) (Snapshot, error) {
		return testSnapshot(resumeID, []byte("small")), nil
	}}); !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("oversize output error = %v, want ErrOutputTooLarge", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	if _, err := queue.Render(canceled, Request{Format: PDF, Prepare: func(context.Context) (Snapshot, error) {
		called = true
		return Snapshot{}, nil
	}}); !errors.Is(err, context.Canceled) || called {
		t.Fatalf("canceled Render() error=%v Prepare called=%v", err, called)
	}
	assertQueueEmpty(t, queue)
}

func TestOversizedSnapshotIsRejectedBeforePayloadCopy(t *testing.T) {
	clock := newFakeClock()
	queue, err := New(testConfig(rendererFunc(func(context.Context, Navigation) ([]byte, error) {
		t.Fatal("renderer called for oversized snapshot")
		return nil, nil
	}), clock))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	payload := make([]byte, MaxSnapshotBytes+1)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err = queue.Render(context.Background(), Request{Format: PDF, Prepare: func(context.Context) (Snapshot, error) {
		return testSnapshot(uuid.New(), payload), nil
	}})
	runtime.ReadMemStats(&after)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Render() error = %v, want ErrInvalidRequest", err)
	}
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated >= uint64(MaxSnapshotBytes) {
		t.Fatalf("oversized rejection allocated %d bytes, want less than payload copy", allocated)
	}
}

func TestTokenEncodingIsCanonicalRawURLForThirtyTwoBytes(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	resumeID := uuid.MustParse("10000000-0000-4000-8000-000000000008")
	var queue *Queue
	renderer := rendererFunc(func(ctx context.Context, navigation Navigation) ([]byte, error) {
		decoded, err := base64.RawURLEncoding.DecodeString(navigation.Capability)
		if err != nil || len(decoded) != 32 || len(navigation.Capability) != 43 {
			t.Fatalf("capability shape length=%d decoded=%d error=%v", len(navigation.Capability), len(decoded), err)
		}
		if _, err := queue.Redeem(ctx, Redemption{ResumeID: resumeID, JobID: navigation.JobID, Audience: "nuxt-print", Capability: navigation.Capability}); err != nil {
			return nil, err
		}
		return []byte("ok"), nil
	})
	var err error
	queue, err = New(testConfig(renderer, clock))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := queue.Render(context.Background(), Request{Format: PDF, Prepare: func(context.Context) (Snapshot, error) {
		return testSnapshot(resumeID, []byte("snapshot")), nil
	}}); err != nil {
		t.Fatalf("Render() error = %v", err)
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition was not met")
		}
		time.Sleep(time.Millisecond)
	}
}

func assertQueueEmpty(t *testing.T, queue *Queue) {
	t.Helper()
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if len(queue.attempts) != 0 || len(queue.jobs) != 0 {
		t.Fatalf("queue retained attempts=%d jobs=%d", len(queue.attempts), len(queue.jobs))
	}
}
