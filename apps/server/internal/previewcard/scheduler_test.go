package previewcard

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
)

type fakeTimer struct {
	clock   *fakeClock
	when    time.Duration
	fn      func()
	stopped bool
}

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	stopped := t.stopped
	t.stopped = true
	return !stopped
}

type fakeClock struct {
	mu     sync.Mutex
	now    time.Duration
	timers []*fakeTimer
}

func (c *fakeClock) AfterFunc(delay time.Duration, fn func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	timer := &fakeTimer{clock: c, when: c.now + delay, fn: fn}
	c.timers = append(c.timers, timer)
	return timer
}

func (c *fakeClock) Advance(delay time.Duration) {
	c.mu.Lock()
	c.now += delay
	var due []func()
	for _, timer := range c.timers {
		if !timer.stopped && timer.when <= c.now {
			timer.stopped = true
			due = append(due, timer.fn)
		}
	}
	c.mu.Unlock()
	for _, fn := range due {
		fn()
	}
}

type schedulerHarness struct {
	scheduler *Scheduler
	cards     *fakeStore
	queue     *fakeQueue
	clock     *fakeClock
	waits     *atomic.Int64
}

func newSchedulerHarness(t *testing.T, live ...publicresume.Snapshot) schedulerHarness {
	t.Helper()
	cards, queue, clock := newFakeStore(), &fakeQueue{}, &fakeClock{}
	for _, snapshot := range live {
		cards.setLive(snapshot)
	}
	waits := &atomic.Int64{}
	scheduler, err := NewScheduler(SchedulerConfig{
		Service: newTestService(t, cards, queue), Store: cards, AfterFunc: clock.AfterFunc,
		Wait: func(_ context.Context, delay time.Duration) bool {
			if delay == SweepInterval {
				waits.Add(1)
			}
			return true
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return schedulerHarness{scheduler: scheduler, cards: cards, queue: queue, clock: clock, waits: waits}
}

func (h schedulerHarness) run(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.scheduler.Run(ctx) }()
	<-h.scheduler.swept
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Run() error = %v", err)
		}
	})
}

func (h schedulerHarness) idle(t *testing.T) {
	t.Helper()
	waitUntil(t, func() bool {
		h.scheduler.mu.Lock()
		defer h.scheduler.mu.Unlock()
		return h.scheduler.busy == 0
	})
}

func (h schedulerHarness) storedVersion(id uuid.UUID) string {
	h.cards.mu.Lock()
	defer h.cards.mu.Unlock()
	return h.cards.stored[id].Version
}

func mustVersion(t *testing.T, snapshot publicresume.Snapshot) string {
	t.Helper()
	version, err := VersionOf(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return version
}

func TestNotifyBuildsAtOnceWhenALiveResumeHasNoCard(t *testing.T) {
	t.Parallel()
	h := newSchedulerHarness(t)
	h.run(t)
	h.idle(t)
	snapshot := testSnapshot(t, "Ada Lovelace", "Engineer", nil)
	h.cards.setLive(snapshot)

	h.scheduler.Notify(snapshot.ResumeID, false)
	h.idle(t)
	if got := h.storedVersion(snapshot.ResumeID); got != mustVersion(t, snapshot) {
		t.Fatalf("stored version = %q, want the current card at once", got)
	}
	h.queue.mu.Lock()
	defer h.queue.mu.Unlock()
	if len(h.queue.priorities) != 1 || h.queue.priorities[0] != renderjob.PriorityLow {
		t.Fatalf("render priorities = %v, want one low-priority build", h.queue.priorities)
	}
}

func TestEditRebuildsOnlyAfterTheQuietPeriod(t *testing.T) {
	t.Parallel()
	before := testSnapshot(t, "Ada Lovelace", "Engineer", nil)
	h := newSchedulerHarness(t)
	h.cards.setLive(before)
	h.cards.stored[before.ResumeID] = StoredCard{Version: mustVersion(t, before), PNG: testPNG}
	h.run(t)
	h.idle(t)

	after, err := testReader(t).ProjectRow(testRow(t, before.ResumeID, "Ada Lovelace", "Mathematician", nil))
	if err != nil {
		t.Fatal(err)
	}
	h.cards.setLive(after)
	h.scheduler.Notify(before.ResumeID, false)
	h.idle(t)
	h.clock.Advance(QuietPeriod - time.Second)
	h.scheduler.Notify(before.ResumeID, false)
	h.idle(t)
	h.clock.Advance(QuietPeriod - time.Second)
	h.idle(t)
	if h.queue.renderCalls() != 0 {
		t.Fatalf("renders before a quiet period = %d, want 0", h.queue.renderCalls())
	}
	h.clock.Advance(time.Second)
	h.idle(t)
	if got := h.storedVersion(before.ResumeID); got != mustVersion(t, after) || h.queue.renderCalls() != 1 {
		t.Fatalf("after the quiet period stored %q with %d renders, want the new card once", got, h.queue.renderCalls())
	}
}

func TestChangeThatKeepsTheCardBuildsNothing(t *testing.T) {
	t.Parallel()
	snapshot := testSnapshot(t, "Ada Lovelace", "Engineer", nil)
	h := newSchedulerHarness(t, snapshot)
	h.cards.stored[snapshot.ResumeID] = StoredCard{Version: mustVersion(t, snapshot), PNG: testPNG}
	h.run(t)
	h.idle(t)
	h.scheduler.Notify(snapshot.ResumeID, false)
	h.clock.Advance(QuietPeriod)
	h.idle(t)
	if h.queue.renderCalls() != 0 {
		t.Fatalf("renders = %d, want 0 for an unchanged card", h.queue.renderCalls())
	}
}

func TestDeleteAndUnpublishBuildNothing(t *testing.T) {
	t.Parallel()
	h := newSchedulerHarness(t)
	h.run(t)
	h.idle(t)
	unpublished := uuid.New()
	h.scheduler.Notify(unpublished, false)
	h.idle(t)
	deleted := testSnapshot(t, "Ada Lovelace", "", nil)
	h.cards.setLive(deleted)
	h.scheduler.Notify(deleted.ResumeID, false)
	h.idle(t)
	h.cards.mu.Lock()
	delete(h.cards.live, deleted.ResumeID)
	delete(h.cards.stored, deleted.ResumeID)
	h.cards.mu.Unlock()
	h.scheduler.Notify(deleted.ResumeID, true)
	h.clock.Advance(QuietPeriod)
	h.idle(t)
	if h.queue.renderCalls() != 1 {
		t.Fatalf("renders = %d, want only the build before the delete", h.queue.renderCalls())
	}
}

func TestStartSweepQueuesStaleCardsOneInterval(t *testing.T) {
	t.Parallel()
	current := testSnapshot(t, "Ada Lovelace", "", nil)
	missing := testSnapshot(t, "Grace Hopper", "", nil)
	stale := testSnapshot(t, "Alan Turing", "", nil)
	h := newSchedulerHarness(t, current, missing, stale)
	h.cards.stored[current.ResumeID] = StoredCard{Version: mustVersion(t, current), PNG: testPNG}
	h.cards.stored[stale.ResumeID] = StoredCard{Version: "0123456789abcdef", PNG: testPNG}
	h.run(t)
	waitUntil(t, func() bool {
		return h.storedVersion(missing.ResumeID) == mustVersion(t, missing) &&
			h.storedVersion(stale.ResumeID) == mustVersion(t, stale)
	})
	h.idle(t)
	if h.queue.renderCalls() != 2 {
		t.Fatalf("sweep renders = %d, want 2", h.queue.renderCalls())
	}
	if got := h.waits.Load(); got != 2 {
		t.Fatalf("sweep waits = %d, want one per queued build", got)
	}
}

func TestFailedBuildRetriesAfterTheDelayAtMostFiveTimes(t *testing.T) {
	t.Parallel()
	h := newSchedulerHarness(t)
	h.queue.err = errors.New("renderer down")
	h.run(t)
	h.idle(t)
	snapshot := testSnapshot(t, "Ada Lovelace", "", nil)
	h.cards.setLive(snapshot)
	h.scheduler.Notify(snapshot.ResumeID, false)
	h.idle(t)
	for range MaxRetries + 2 {
		h.clock.Advance(RetryDelay - time.Second)
		h.idle(t)
		h.clock.Advance(time.Second)
		h.idle(t)
	}
	// One build at once, one after the quiet period, and MaxRetries retries.
	if got, want := h.queue.renderCalls(), 2+MaxRetries; got != want {
		t.Fatalf("renders = %d, want %d", got, want)
	}
}

// A rename in the middle of an edit burst removes the card; the change that
// follows builds at once instead of waiting for the quiet period.
func TestRenameDuringAnEditBurstBuildsAtOnce(t *testing.T) {
	t.Parallel()
	before := testSnapshot(t, "Ada Lovelace", "Engineer", nil)
	h := newSchedulerHarness(t, before)
	h.cards.stored[before.ResumeID] = StoredCard{Version: mustVersion(t, before), PNG: testPNG}
	h.run(t)
	h.idle(t)
	h.scheduler.Notify(before.ResumeID, false)
	h.idle(t)

	after, err := testReader(t).ProjectRow(testRow(t, before.ResumeID, "Ada Lovelace", "Engineer", nil))
	if err != nil {
		t.Fatal(err)
	}
	after.Public.Slug = "ada-renamed"
	h.cards.mu.Lock()
	h.cards.live[before.ResumeID] = after
	delete(h.cards.stored, before.ResumeID)
	h.cards.mu.Unlock()
	h.scheduler.Notify(before.ResumeID, false)
	h.idle(t)
	if got := h.storedVersion(before.ResumeID); got != mustVersion(t, after) {
		t.Fatalf("stored version before the quiet period = %q, want the renamed card at once", got)
	}
}

func TestSuccessClearsTheRetryCount(t *testing.T) {
	t.Parallel()
	h := newSchedulerHarness(t)
	h.queue.err = errors.New("renderer down")
	h.run(t)
	h.idle(t)
	snapshot := testSnapshot(t, "Ada Lovelace", "", nil)
	h.cards.setLive(snapshot)
	h.scheduler.Notify(snapshot.ResumeID, false)
	h.idle(t)
	h.queue.mu.Lock()
	h.queue.err = nil
	h.queue.mu.Unlock()
	h.clock.Advance(QuietPeriod)
	h.idle(t)
	if got := h.storedVersion(snapshot.ResumeID); got != mustVersion(t, snapshot) {
		t.Fatalf("stored version = %q, want the card after the quiet period", got)
	}
	h.scheduler.mu.Lock()
	defer h.scheduler.mu.Unlock()
	if len(h.scheduler.retries) != 0 {
		t.Fatalf("retry counts after success = %v, want none", h.scheduler.retries)
	}
}

func TestStartSweepRetriesAFailedList(t *testing.T) {
	t.Parallel()
	missing := testSnapshot(t, "Grace Hopper", "", nil)
	cards, queue := newFakeStore(), &fakeQueue{}
	cards.setLive(missing)
	cards.listFailures = 2
	scheduler, err := NewScheduler(SchedulerConfig{
		Service: newTestService(t, cards, queue), Store: cards, AfterFunc: (&fakeClock{}).AfterFunc,
		Wait: func(context.Context, time.Duration) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	h := schedulerHarness{scheduler: scheduler, cards: cards, queue: queue}
	h.run(t)
	h.idle(t)
	if got := h.storedVersion(missing.ResumeID); got != mustVersion(t, missing) {
		t.Fatalf("stored version after two failed lists = %q, want the swept card", got)
	}
}
