package renderjob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

type directJob struct {
	queue      *Queue
	attempt    *attempt
	job        *job
	capability string
	controller string
}

type blockedCallbackTimer struct {
	fn      func()
	started chan struct{}
	release chan struct{}
	fired   atomic.Bool
	stopped atomic.Bool
}

func (timer *blockedCallbackTimer) Stop() bool {
	return !timer.fired.Load() && timer.stopped.CompareAndSwap(false, true)
}

func (timer *blockedCallbackTimer) Fire() {
	if !timer.fired.CompareAndSwap(false, true) {
		return
	}
	go func() {
		close(timer.started)
		<-timer.release
		timer.fn()
	}()
}

type blockedTimerFactory struct {
	timers chan *blockedCallbackTimer
}

func (factory *blockedTimerFactory) AfterFunc(_ time.Duration, fn func()) Timer {
	timer := &blockedCallbackTimer{fn: fn, started: make(chan struct{}), release: make(chan struct{})}
	factory.timers <- timer
	return timer
}

func newDirectJob(t *testing.T, snapshot Snapshot, validate func(context.Context, Snapshot) error) directJob {
	t.Helper()
	clock := newFakeClock()
	queue, err := New(testConfig(rendererFunc(func(context.Context, Navigation) ([]byte, error) {
		return nil, errors.New("unused renderer")
	}), clock))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	active, err := queue.admit(context.Background())
	if err != nil {
		t.Fatalf("admit() error = %v", err)
	}
	jobID, err := queue.newJobID()
	if err != nil {
		t.Fatalf("newJobID() error = %v", err)
	}
	capabilityRaw := bytes.Repeat([]byte{0x31}, capabilityBytes)
	controllerRaw := bytes.Repeat([]byte{0x72}, capabilityBytes)
	capability := base64.RawURLEncoding.EncodeToString(capabilityRaw)
	controller := base64.RawURLEncoding.EncodeToString(controllerRaw)
	stored, err := queue.installJob(active, Request{Format: PDF, ValidateGeneration: validate}, cloneSnapshot(snapshot),
		jobID, sha256.Sum256(capabilityRaw), sha256.Sum256(controllerRaw))
	if err != nil {
		t.Fatalf("installJob() error = %v", err)
	}
	return directJob{queue: queue, attempt: active, job: stored, capability: capability, controller: controller}
}

func (d directJob) redeem(t *testing.T) {
	t.Helper()
	_, err := d.queue.Redeem(context.Background(), Redemption{
		ResumeID: d.job.snapshot.ResumeID, JobID: d.attempt.jobID, Audience: audiencePrint, Capability: d.capability,
	})
	if err != nil {
		t.Fatalf("Redeem() error = %v", err)
	}
}

func (d directJob) release() { d.queue.releaseAttempt(d.attempt) }

func TestCompletionRequiresConsumedCapabilityAndControllerAuthority(t *testing.T) {
	t.Parallel()

	direct := newDirectJob(t, testSnapshot(uuid.New(), []byte("snapshot")), nil)
	defer direct.release()
	for _, test := range []struct {
		name       string
		jobID      uuid.UUID
		controller string
	}{
		{name: "before redemption", jobID: direct.attempt.jobID, controller: direct.controller},
		{name: "missing handle", jobID: direct.attempt.jobID},
		{name: "wrong handle", jobID: direct.attempt.jobID, controller: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x73}, capabilityBytes))},
		{name: "job id only", jobID: direct.attempt.jobID, controller: direct.attempt.jobID.String()},
		{name: "unknown job", jobID: uuid.New(), controller: direct.controller},
	} {
		if _, err := direct.queue.complete(context.Background(), test.jobID, test.controller, []byte("pdf")); !errors.Is(err, ErrNotActive) {
			t.Fatalf("complete(%s) error = %v, want ErrNotActive", test.name, err)
		}
	}
	direct.redeem(t)
	result, err := direct.queue.complete(context.Background(), direct.attempt.jobID, direct.controller, []byte("accepted"))
	if err != nil {
		t.Fatalf("complete(valid) error = %v", err)
	}
	if result.Digest != sha256.Sum256([]byte("accepted")) || result.Revision != direct.job.snapshot.Revision {
		t.Fatalf("result = %+v", result)
	}
	if _, err := direct.queue.complete(context.Background(), direct.attempt.jobID, direct.controller, []byte("duplicate")); !errors.Is(err, ErrNotActive) {
		t.Fatalf("complete(duplicate) error = %v, want ErrNotActive", err)
	}
}

func TestCanceledAttemptCannotRedeemBeforeCleanupCallbackRuns(t *testing.T) {
	t.Parallel()

	direct := newDirectJob(t, testSnapshot(uuid.New(), []byte("snapshot")), nil)
	defer direct.release()
	direct.attempt.cancel()
	if _, err := direct.queue.Redeem(context.Background(), Redemption{
		ResumeID: direct.job.snapshot.ResumeID, JobID: direct.attempt.jobID,
		Audience: audiencePrint, Capability: direct.capability,
	}); !errors.Is(err, ErrNotActive) {
		t.Fatalf("Redeem(canceled attempt) error = %v, want ErrNotActive", err)
	}
	direct.queue.mu.Lock()
	jobs := len(direct.queue.jobs)
	direct.queue.mu.Unlock()
	if jobs != 0 {
		t.Fatalf("canceled redemption retained %d jobs", jobs)
	}
}

func TestCanceledCompletionContextsCannotPublishArtifact(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		cancel func(context.CancelFunc, directJob)
	}{
		{name: "attempt context", cancel: func(_ context.CancelFunc, direct directJob) { direct.attempt.cancel() }},
		{name: "completion context", cancel: func(cancel context.CancelFunc, _ directJob) { cancel() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			direct := newDirectJob(t, testSnapshot(uuid.New(), []byte("snapshot")), nil)
			defer direct.release()
			direct.redeem(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			test.cancel(cancel, direct)
			result, err := direct.queue.complete(ctx, direct.attempt.jobID, direct.controller, []byte("must not publish"))
			if !errors.Is(err, context.Canceled) || len(result.Bytes) != 0 {
				t.Fatalf("complete() result=%+v error=%v, want no artifact and context canceled", result, err)
			}
			direct.queue.mu.Lock()
			jobs := len(direct.queue.jobs)
			direct.queue.mu.Unlock()
			if jobs != 0 {
				t.Fatalf("canceled completion retained %d jobs", jobs)
			}
		})
	}
}

func TestValidationCancellationCannotWinFinalPublication(t *testing.T) {
	t.Parallel()

	snapshot := testSnapshot(uuid.New(), []byte("public snapshot"))
	snapshot.PublicGeneration = snapshot.Revision
	var direct directJob
	direct = newDirectJob(t, snapshot, func(context.Context, Snapshot) error {
		direct.attempt.cancel()
		return nil
	})
	defer direct.release()
	direct.redeem(t)
	result, err := direct.queue.complete(direct.attempt.ctx, direct.attempt.jobID, direct.controller, []byte("must not publish"))
	if !errors.Is(err, context.Canceled) || len(result.Bytes) != 0 {
		t.Fatalf("complete() result=%+v error=%v, want no artifact and context canceled", result, err)
	}
	direct.queue.mu.Lock()
	jobs := len(direct.queue.jobs)
	direct.queue.mu.Unlock()
	if jobs != 0 {
		t.Fatalf("validation cancellation retained %d jobs", jobs)
	}
}

func TestConcurrentCompletionHasOneWinnerAndNoTombstone(t *testing.T) {
	t.Parallel()

	direct := newDirectJob(t, testSnapshot(uuid.New(), []byte("snapshot")), nil)
	defer direct.release()
	direct.redeem(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := direct.queue.complete(context.Background(), direct.attempt.jobID, direct.controller, []byte("pdf"))
			results <- err
		}()
	}
	close(start)
	var success, inactive int
	for range 2 {
		switch err := <-results; {
		case err == nil:
			success++
		case errors.Is(err, ErrNotActive):
			inactive++
		default:
			t.Fatalf("complete() error = %v", err)
		}
	}
	if success != 1 || inactive != 1 {
		t.Fatalf("completion outcomes success=%d inactive=%d", success, inactive)
	}
	direct.queue.mu.Lock()
	jobs := len(direct.queue.jobs)
	direct.queue.mu.Unlock()
	if jobs != 0 {
		t.Fatalf("terminal jobs retained = %d", jobs)
	}
}

func TestPublicCompletionValidatesFrozenGenerationOutsideQueueLock(t *testing.T) {
	t.Parallel()

	snapshot := testSnapshot(uuid.New(), []byte("public snapshot"))
	snapshot.PublicGeneration = snapshot.Revision
	var queue *Queue
	var calls atomic.Int64
	validate := func(_ context.Context, got Snapshot) error {
		calls.Add(1)
		got.Payload[0] = 'X'
		if err := queue.Ready(); err != nil {
			t.Fatalf("Ready() from validator error = %v", err)
		}
		return nil
	}
	direct := newDirectJob(t, snapshot, validate)
	queue = direct.queue
	defer direct.release()
	direct.redeem(t)
	result, err := direct.queue.complete(context.Background(), direct.attempt.jobID, direct.controller, []byte("public png"))
	if err != nil {
		t.Fatalf("complete() error = %v", err)
	}
	if calls.Load() != 1 || result.Digest != sha256.Sum256([]byte("public png")) {
		t.Fatalf("validator calls=%d result=%+v", calls.Load(), result)
	}
	if string(snapshot.Payload) != "public snapshot" {
		t.Fatalf("input payload mutated: %q", snapshot.Payload)
	}
}

func TestPublicGenerationChangeIsTerminalDiscard(t *testing.T) {
	t.Parallel()

	snapshot := testSnapshot(uuid.New(), []byte("public snapshot"))
	snapshot.PublicGeneration = snapshot.Revision
	direct := newDirectJob(t, snapshot, func(context.Context, Snapshot) error { return errors.New("stale raw state") })
	defer direct.release()
	direct.redeem(t)
	if _, err := direct.queue.complete(context.Background(), direct.attempt.jobID, direct.controller, []byte("discarded")); !errors.Is(err, ErrGenerationChanged) || bytes.Contains([]byte(err.Error()), []byte("raw")) {
		t.Fatalf("complete(stale) error = %v, want sanitized ErrGenerationChanged", err)
	}
	if _, err := direct.queue.complete(context.Background(), direct.attempt.jobID, direct.controller, []byte("retry")); !errors.Is(err, ErrNotActive) {
		t.Fatalf("complete(after discard) error = %v, want ErrNotActive", err)
	}
}

func TestDeadlineCancelsPublicValidationAndWinsDiscardRace(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	resumeID := uuid.New()
	validationStarted := make(chan struct{})
	var queue *Queue
	renderer := rendererFunc(func(ctx context.Context, navigation Navigation) ([]byte, error) {
		if _, err := queue.Redeem(ctx, Redemption{
			ResumeID: resumeID, JobID: navigation.JobID, Audience: audiencePrint, Capability: navigation.Capability,
		}); err != nil {
			return nil, err
		}
		return []byte("artifact"), nil
	})
	config := testConfig(renderer, clock)
	config.JobTimeout = time.Second
	var err error
	queue, err = New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, renderErr := queue.Render(context.Background(), Request{
			Format: PNG,
			Prepare: func(context.Context) (Snapshot, error) {
				snapshot := testSnapshot(resumeID, []byte("public"))
				snapshot.PublicGeneration = snapshot.Revision
				return snapshot, nil
			},
			ValidateGeneration: func(ctx context.Context, _ Snapshot) error {
				close(validationStarted)
				<-ctx.Done()
				return ctx.Err()
			},
		})
		done <- renderErr
	}()
	<-validationStarted
	clock.Advance(time.Second)
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Render() error = %v, want context deadline exceeded", err)
	}
	assertQueueEmpty(t, queue)
}

func TestCorruptedFrozenPayloadAndProcessStateLossRejectCompletion(t *testing.T) {
	t.Parallel()

	direct := newDirectJob(t, testSnapshot(uuid.New(), []byte("snapshot")), nil)
	defer direct.release()
	direct.redeem(t)
	direct.queue.mu.Lock()
	direct.job.snapshot.Payload[0] = 'X'
	direct.queue.mu.Unlock()
	if _, err := direct.queue.complete(context.Background(), direct.attempt.jobID, direct.controller, []byte("pdf")); !errors.Is(err, ErrNotActive) {
		t.Fatalf("complete(corrupt snapshot) error = %v, want ErrNotActive", err)
	}
	direct.queue.mu.Lock()
	direct.queue.removeJobLocked(direct.job)
	direct.queue.mu.Unlock()
	if _, err := direct.queue.complete(context.Background(), direct.attempt.jobID, direct.controller, []byte("pdf")); !errors.Is(err, ErrNotActive) {
		t.Fatalf("complete(after process loss) error = %v, want ErrNotActive", err)
	}
}

func TestPreparationDeadlineCancelsAndJoinsBeforeCapacityRecovery(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	entered := make(chan struct{})
	exited := make(chan struct{})
	config := testConfig(rendererFunc(func(context.Context, Navigation) ([]byte, error) {
		t.Fatal("renderer called after preparation timeout")
		return nil, nil
	}), clock)
	config.JobTimeout = time.Second
	queue, err := New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, renderErr := queue.Render(context.Background(), Request{Format: PDF, Prepare: func(ctx context.Context) (Snapshot, error) {
			close(entered)
			<-ctx.Done()
			close(exited)
			return Snapshot{}, ctx.Err()
		}})
		done <- renderErr
	}()
	<-entered
	clock.Advance(time.Second)
	<-exited
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Render() error = %v, want context deadline exceeded", err)
	}
	if err := queue.Ready(); err != nil {
		t.Fatalf("Ready() after preparation joined error = %v", err)
	}
	assertQueueEmpty(t, queue)
}

func TestCanceledRendererJoinsBeforePermitReuse(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	resumeID := uuid.New()
	firstCanceled := make(chan struct{})
	allowFirstReturn := make(chan struct{})
	secondStarted := make(chan struct{})
	var calls atomic.Int64
	var queue *Queue
	renderer := rendererFunc(func(ctx context.Context, navigation Navigation) ([]byte, error) {
		if _, err := queue.Redeem(ctx, Redemption{
			ResumeID: resumeID, JobID: navigation.JobID, Audience: audiencePrint, Capability: navigation.Capability,
		}); err != nil {
			return nil, err
		}
		if calls.Add(1) == 1 {
			<-ctx.Done()
			close(firstCanceled)
			<-allowFirstReturn
			return nil, ctx.Err()
		}
		close(secondStarted)
		return []byte("second"), nil
	})
	var err error
	queue, err = New(testConfig(renderer, clock))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, renderErr := queue.Render(firstCtx, Request{Format: PDF, Prepare: func(context.Context) (Snapshot, error) {
			return testSnapshot(resumeID, []byte("first")), nil
		}})
		firstDone <- renderErr
	}()
	waitFor(t, func() bool { return calls.Load() == 1 })
	secondDone := make(chan error, 1)
	go func() {
		_, renderErr := queue.Render(context.Background(), Request{Format: PDF, Prepare: func(context.Context) (Snapshot, error) {
			return testSnapshot(resumeID, []byte("second")), nil
		}})
		secondDone <- renderErr
	}()
	cancelFirst()
	<-firstCanceled
	select {
	case <-secondStarted:
		t.Fatal("second renderer started before canceled renderer joined")
	case <-time.After(20 * time.Millisecond):
	}
	close(allowFirstReturn)
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Fatalf("first Render() error = %v, want context canceled", err)
	}
	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("second renderer did not start after first joined")
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second Render() error = %v", err)
	}
}

func TestCloseWaitsForRendererJoin(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	resumeID := uuid.New()
	rendererStarted := make(chan struct{})
	rendererCanceled := make(chan struct{})
	allowRendererReturn := make(chan struct{})
	var queue *Queue
	renderer := rendererFunc(func(ctx context.Context, navigation Navigation) ([]byte, error) {
		if _, err := queue.Redeem(ctx, Redemption{
			ResumeID: resumeID, JobID: navigation.JobID, Audience: audiencePrint, Capability: navigation.Capability,
		}); err != nil {
			return nil, err
		}
		close(rendererStarted)
		<-ctx.Done()
		close(rendererCanceled)
		<-allowRendererReturn
		return nil, ctx.Err()
	})
	var err error
	queue, err = New(testConfig(renderer, clock))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	renderDone := make(chan error, 1)
	go func() {
		_, renderErr := queue.Render(context.Background(), Request{Format: PDF, Prepare: func(context.Context) (Snapshot, error) {
			return testSnapshot(resumeID, []byte("snapshot")), nil
		}})
		renderDone <- renderErr
	}()
	<-rendererStarted
	closeDone := make(chan error, 1)
	go func() { closeDone <- queue.Close() }()
	<-rendererCanceled
	select {
	case <-closeDone:
		t.Fatal("Close returned before renderer joined")
	case <-time.After(20 * time.Millisecond):
	}
	close(allowRendererReturn)
	if err := <-closeDone; err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := <-renderDone; !errors.Is(err, ErrClosed) {
		t.Fatalf("Render() error = %v, want ErrClosed", err)
	}
	assertQueueEmpty(t, queue)
}

func TestRenderJoinsAlreadyStartedTimerCallbackBeforeReturning(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	resumeID := uuid.New()
	factory := &blockedTimerFactory{timers: make(chan *blockedCallbackTimer, 2)}
	rendererStarted := make(chan struct{})
	allowRendererReturn := make(chan struct{})
	var queue *Queue
	renderer := rendererFunc(func(ctx context.Context, navigation Navigation) ([]byte, error) {
		if _, err := queue.Redeem(ctx, Redemption{
			ResumeID: resumeID, JobID: navigation.JobID, Audience: audiencePrint, Capability: navigation.Capability,
		}); err != nil {
			return nil, err
		}
		close(rendererStarted)
		<-allowRendererReturn
		return []byte("artifact"), nil
	})
	config := testConfig(renderer, clock)
	config.AfterFunc = factory.AfterFunc
	var err error
	queue, err = New(config)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, renderErr := queue.Render(context.Background(), Request{Format: PDF, Prepare: func(context.Context) (Snapshot, error) {
			return testSnapshot(resumeID, []byte("snapshot")), nil
		}})
		done <- renderErr
	}()
	deadlineTimer := <-factory.timers
	<-factory.timers // capability-expiry timer
	<-rendererStarted
	deadlineTimer.Fire()
	<-deadlineTimer.started
	close(allowRendererReturn)
	select {
	case err := <-done:
		t.Fatalf("Render returned before timer callback joined: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(deadlineTimer.release)
	if err := <-done; err != nil {
		t.Fatalf("Render() error = %v", err)
	}
}

func TestRepeatedTerminalJobsDoNotGrowState(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	resumeID := uuid.New()
	var queue *Queue
	renderer := rendererFunc(func(ctx context.Context, navigation Navigation) ([]byte, error) {
		if _, err := queue.Redeem(ctx, Redemption{
			ResumeID: resumeID, JobID: navigation.JobID, Audience: audiencePrint, Capability: navigation.Capability,
		}); err != nil {
			return nil, err
		}
		return []byte("artifact"), nil
	})
	var err error
	queue, err = New(testConfig(renderer, clock))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for range 100 {
		if _, err := queue.Render(context.Background(), Request{Format: PDF, Prepare: func(context.Context) (Snapshot, error) {
			return testSnapshot(resumeID, []byte("snapshot")), nil
		}}); err != nil {
			t.Fatalf("Render() error = %v", err)
		}
		assertQueueEmpty(t, queue)
	}
}

func TestFormatSpecificBoundsAndPublicGenerationBinding(t *testing.T) {
	t.Parallel()

	clock := newFakeClock()
	for _, config := range []Config{
		{Renderer: rendererFunc(func(context.Context, Navigation) ([]byte, error) { return nil, nil }), ConcurrentRenders: 2},
		{Renderer: rendererFunc(func(context.Context, Navigation) ([]byte, error) { return nil, nil }), QueueDepth: MaxQueueDepth + 1},
		{Renderer: rendererFunc(func(context.Context, Navigation) ([]byte, error) { return nil, nil }), JobTimeout: MaxJobTimeout + 1},
		{Renderer: rendererFunc(func(context.Context, Navigation) ([]byte, error) { return nil, nil }), CapabilityTTL: MaxCapabilityTTL + 1},
		{Renderer: rendererFunc(func(context.Context, Navigation) ([]byte, error) { return nil, nil }), SnapshotLimit: MaxSnapshotBytes + 1},
		{Renderer: rendererFunc(func(context.Context, Navigation) ([]byte, error) { return nil, nil }), PDFLimit: PDFMaxBytes + 1},
		{Renderer: rendererFunc(func(context.Context, Navigation) ([]byte, error) { return nil, nil }), PNGLimit: PNGMaxBytes + 1},
	} {
		if _, err := New(config); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("New(over limit %+v) error = %v, want ErrInvalidRequest", config, err)
		}
	}

	queue, err := New(testConfig(rendererFunc(func(context.Context, Navigation) ([]byte, error) {
		t.Fatal("renderer called for invalid public binding")
		return nil, nil
	}), clock))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for _, snapshot := range []Snapshot{
		{ResumeID: uuid.New(), Revision: 7, SchemaVersion: 2, PublicGeneration: 6, Payload: []byte("snapshot")},
		{ResumeID: uuid.New(), Revision: 7, SchemaVersion: 2, PublicGeneration: 7, Payload: []byte("snapshot")},
	} {
		_, renderErr := queue.Render(context.Background(), Request{Format: PNG, Prepare: func(context.Context) (Snapshot, error) {
			return snapshot, nil
		}})
		if !errors.Is(renderErr, ErrInvalidRequest) {
			t.Fatalf("Render(public generation %d) error = %v, want ErrInvalidRequest", snapshot.PublicGeneration, renderErr)
		}
	}

	resumeID := uuid.New()
	var boundedQueue *Queue
	renderer := rendererFunc(func(ctx context.Context, navigation Navigation) ([]byte, error) {
		if _, redeemErr := boundedQueue.Redeem(ctx, Redemption{
			ResumeID: resumeID, JobID: navigation.JobID, Audience: audiencePrint, Capability: navigation.Capability,
		}); redeemErr != nil {
			return nil, redeemErr
		}
		return []byte("four"), nil
	})
	config := testConfig(renderer, clock)
	config.PDFLimit = 4
	config.PNGLimit = 3
	boundedQueue, err = New(config)
	if err != nil {
		t.Fatalf("New(bounded) error = %v", err)
	}
	prepare := func(context.Context) (Snapshot, error) { return testSnapshot(resumeID, []byte("snapshot")), nil }
	if _, err := boundedQueue.Render(context.Background(), Request{Format: PDF, Prepare: prepare}); err != nil {
		t.Fatalf("Render(PDF at limit) error = %v", err)
	}
	if _, err := boundedQueue.Render(context.Background(), Request{Format: PNG, Prepare: prepare}); !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("Render(PNG over limit) error = %v, want ErrOutputTooLarge", err)
	}
	if _, err := boundedQueue.Render(context.Background(), Request{Format: "jpeg", Prepare: prepare}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("Render(unknown format) error = %v, want ErrInvalidRequest", err)
	}
}
