package previewcard

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/renderjob"
)

const (
	QuietPeriod   = 10 * time.Second
	SweepInterval = 5 * time.Second
	RetryDelay    = 30 * time.Second
	MaxRetries    = 5
	workDepth     = 256
)

type Timer interface {
	Stop() bool
}

type SchedulerConfig struct {
	Service   *Service
	Store     CardStore
	Logger    *slog.Logger
	AfterFunc func(time.Duration, func()) Timer
	Wait      func(context.Context, time.Duration) bool
}

type checkKind uint8

const (
	checkMissing checkKind = iota + 1
	checkChanged
)

type check struct {
	resumeID uuid.UUID
	kind     checkKind
}

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
	busy    int
	closed  bool
	swept   chan struct{}
}

type quietTimer struct{ timer Timer }

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
	dropped = s.enqueueLocked(check{resumeID: resumeID, kind: checkMissing})
	armed := &quietTimer{}
	armed.timer = s.afterFunc(QuietPeriod, func() { s.quietEnded(resumeID, armed) })
	s.quiet[resumeID] = armed
}

func (s *Scheduler) quietEnded(resumeID uuid.UUID, ended *quietTimer) {
	if s.endQuiet(resumeID, ended) {
		s.enqueue(check{resumeID: resumeID, kind: checkChanged})
	}
}

func (s *Scheduler) endQuiet(resumeID uuid.UUID, ended *quietTimer) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.quiet[resumeID] != ended {
		return false
	}
	delete(s.quiet, resumeID)
	return true
}

func (s *Scheduler) enqueue(next check) {
	s.mu.Lock()
	dropped := s.enqueueLocked(next)
	s.mu.Unlock()
	s.logDropped(dropped)
}

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
