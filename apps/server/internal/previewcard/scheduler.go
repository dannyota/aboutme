package previewcard

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
)

const (
	// QuietPeriod is how long a live resume must go without a committed
	// change before its card is rebuilt.
	QuietPeriod = 10 * time.Second
	// SweepInterval spaces the start sweep's builds.
	SweepInterval = 5 * time.Second
	// RetryDelay is the wait before a failed scheduled build runs again.
	RetryDelay = 30 * time.Second
	// MaxRetries bounds the retries of one failed scheduled build.
	MaxRetries = 5
	// workDepth bounds the scheduled checks waiting for the worker. A check
	// that does not fit is dropped: the route still builds a missing card
	// on request, and the next start sweep catches up.
	workDepth = 256
)

// Timer is the stoppable part of a real or test timer.
type Timer interface {
	Stop() bool
}

// SchedulerConfig supplies the scheduler's dependencies. AfterFunc and Wait
// default to real time.
type SchedulerConfig struct {
	Service *Service
	Store   CardStore
	// Logger receives closed reason codes only. Nil disables it.
	Logger    *slog.Logger
	AfterFunc func(time.Duration, func()) Timer
	// Wait blocks for the duration or until ctx ends, reporting whether the
	// full duration passed.
	Wait func(context.Context, time.Duration) bool
}

// checkKind names why the worker looks at a resume.
type checkKind uint8

const (
	// checkMissing builds only when the live resume has no stored card:
	// right after it goes live or is renamed.
	checkMissing checkKind = iota + 1
	// checkChanged builds when the stored card is missing or not current.
	checkChanged
)

type check struct {
	resumeID uuid.UUID
	kind     checkKind
}

// Scheduler keeps every live resume's stored card current. It learns of
// committed changes from the resume revision notifications, builds at once
// when a live resume has no card, rebuilds after QuietPeriod without further
// changes, and sweeps every live resume at start. Scheduled builds use the
// low render priority.
type Scheduler struct {
	service   *Service
	store     CardStore
	logger    *slog.Logger
	afterFunc func(time.Duration, func()) Timer
	wait      func(context.Context, time.Duration) bool

	mu      sync.Mutex
	quiet   map[uuid.UUID]*quietTimer
	retries map[uuid.UUID]int
	queued  map[check]bool
	work    chan check
	// busy counts checks queued or being processed.
	busy   int
	closed bool
	// swept closes when the start sweep has queued its checks.
	swept chan struct{}
}

type quietTimer struct{ timer Timer }

// NewScheduler creates a scheduler; Run starts it.
func NewScheduler(config SchedulerConfig) (*Scheduler, error) {
	if config.Service == nil || config.Store == nil {
		return nil, errors.New("previewcard: invalid scheduler dependencies")
	}
	if config.AfterFunc == nil {
		config.AfterFunc = func(delay time.Duration, fn func()) Timer { return time.AfterFunc(delay, fn) }
	}
	if config.Wait == nil {
		config.Wait = waitFor
	}
	return &Scheduler{
		service: config.Service, store: config.Store, logger: config.Logger,
		afterFunc: config.AfterFunc, wait: config.Wait,
		quiet: make(map[uuid.UUID]*quietTimer), retries: make(map[uuid.UUID]int),
		queued: make(map[check]bool), work: make(chan check, workDepth), swept: make(chan struct{}),
	}, nil
}

// Notify records one committed change of a resume. It never blocks, so the
// notification listener can call it directly.
func (s *Scheduler) Notify(resumeID uuid.UUID, deleted bool) {
	if resumeID == uuid.Nil {
		return
	}
	dropped := false
	defer func() { s.logDropped(dropped) }()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	delete(s.retries, resumeID)
	previous := s.quiet[resumeID]
	if previous != nil {
		previous.timer.Stop()
	}
	if deleted {
		delete(s.quiet, resumeID)
		return
	}
	// Every change checks at once for a missing card, so a publish or rename
	// in the middle of an edit burst builds without waiting for quiet.
	dropped = s.enqueueLocked(check{resumeID: resumeID, kind: checkMissing})
	// A fresh entry per change, so a timer that fired just before this
	// change finds itself replaced and does nothing.
	armed := &quietTimer{}
	armed.timer = s.afterFunc(QuietPeriod, func() { s.quietEnded(resumeID, armed) })
	s.quiet[resumeID] = armed
}

func (s *Scheduler) quietEnded(resumeID uuid.UUID, ended *quietTimer) {
	if s.endQuiet(resumeID, ended) {
		s.enqueue(check{resumeID: resumeID, kind: checkChanged})
	}
}

// endQuiet removes the quiet timer that ended and reports whether it was
// still the current one.
func (s *Scheduler) endQuiet(resumeID uuid.UUID, ended *quietTimer) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.quiet[resumeID] != ended {
		return false
	}
	delete(s.quiet, resumeID)
	return true
}

// enqueue queues next and logs a drop after it releases the lock.
func (s *Scheduler) enqueue(next check) {
	s.mu.Lock()
	dropped := s.enqueueLocked(next)
	s.mu.Unlock()
	s.logDropped(dropped)
}

// enqueueLocked queues next unless it is already queued, and reports
// whether the full queue dropped it. The caller logs a drop after it
// releases the lock, so a slow log never stalls the notification listener.
func (s *Scheduler) enqueueLocked(next check) bool {
	if s.closed || s.queued[next] {
		return false
	}
	select {
	case s.work <- next:
		s.queued[next] = true
		s.busy++
		return false
	default:
		return true
	}
}

func (s *Scheduler) logDropped(dropped bool) {
	if dropped {
		s.log("queue_full")
	}
}

// Run sweeps every live resume once and processes scheduled checks until
// ctx ends. It returns nil after it stops every timer.
func (s *Scheduler) Run(ctx context.Context) error {
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		for {
			select {
			case <-ctx.Done():
				return
			case next := <-s.work:
				s.mu.Lock()
				delete(s.queued, next)
				s.mu.Unlock()
				s.process(ctx, next)
				s.mu.Lock()
				s.busy--
				s.mu.Unlock()
			}
		}
	}()
	s.sweep(ctx)
	close(s.swept)
	<-ctx.Done()
	<-workerDone
	s.mu.Lock()
	s.closed = true
	for _, pending := range s.quiet {
		pending.timer.Stop()
	}
	s.mu.Unlock()
	return nil
}

// sweep queues a check for every live resume whose stored card is missing
// or not current, one every SweepInterval. A failed list read retries after
// RetryDelay, at most MaxRetries times.
func (s *Scheduler) sweep(ctx context.Context) {
	cards, err := s.store.LiveCards(ctx)
	for attempt := 0; err != nil && attempt < MaxRetries; attempt++ {
		s.log("sweep_list_failed")
		if !s.wait(ctx, RetryDelay) {
			return
		}
		cards, err = s.store.LiveCards(ctx)
	}
	if err != nil {
		s.log("sweep_list_failed")
		return
	}
	for _, card := range cards {
		if ctx.Err() != nil {
			return
		}
		if current, ok := s.currentVersion(ctx, card.ResumeID); !ok || current == card.StoredVersion {
			continue
		}
		s.enqueue(check{resumeID: card.ResumeID, kind: checkChanged})
		if !s.wait(ctx, SweepInterval) {
			return
		}
	}
}

func (s *Scheduler) currentVersion(ctx context.Context, resumeID uuid.UUID) (string, bool) {
	snapshot, live, err := s.store.Live(ctx, resumeID)
	if err != nil || !live {
		return "", false
	}
	version, err := VersionOf(snapshot)
	return version, err == nil
}

// process builds one resume's card when its check calls for it. A failed
// build retries after RetryDelay, at most MaxRetries times in a row.
func (s *Scheduler) process(ctx context.Context, next check) {
	snapshot, live, err := s.store.Live(ctx, next.resumeID)
	if err != nil {
		s.retry(next.resumeID, "read_failed")
		return
	}
	if !live {
		s.settled(next.resumeID)
		return
	}
	version, err := VersionOf(snapshot)
	if err != nil {
		s.log("invalid_card")
		return
	}
	stored, found, err := s.store.Stored(ctx, next.resumeID)
	if err != nil {
		s.retry(next.resumeID, "read_failed")
		return
	}
	if found && stored.Version == version {
		s.settled(next.resumeID)
		return
	}
	if found && next.kind == checkMissing {
		return
	}
	_, err = s.service.Build(ctx, snapshot, renderjob.PriorityLow)
	switch {
	case err == nil, errors.Is(err, ErrStale), errors.Is(err, ErrNotLive):
		s.settled(next.resumeID)
	case ctx.Err() == nil:
		s.retry(next.resumeID, "build_failed")
	}
}

// settled clears the retry count of a resume whose card needs no more work.
func (s *Scheduler) settled(resumeID uuid.UUID) {
	s.mu.Lock()
	delete(s.retries, resumeID)
	s.mu.Unlock()
}

func (s *Scheduler) retry(resumeID uuid.UUID, reason string) {
	s.log(reason)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.retries[resumeID] >= MaxRetries {
		return
	}
	s.retries[resumeID]++
	s.afterFunc(RetryDelay, func() { s.enqueue(check{resumeID: resumeID, kind: checkChanged}) })
}

func (s *Scheduler) log(reason string) {
	if s.logger != nil {
		s.logger.Warn("previewcard: scheduled build", "reason", reason)
	}
}

func waitFor(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
