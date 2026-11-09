package authmail

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// Worker scheduling bounds.
const (
	pollInterval       = 1 * time.Second
	leaseDuration      = 30 * time.Second
	sendDeadline       = 10 * time.Second
	claimBatchSize     = 10
	maxConcurrentSends = 2
	maxAttempts        = 8
	maxBackoff         = 1 * time.Hour
	baseBackoff        = 30 * time.Second
	staleLeaseBatch    = 100
	cleanupBatch       = 200
	finishedJobRetain  = 7 * 24 * time.Hour
)

// WorkerOptions wires the worker to the store pool, transaction queries,
// key ring, a Sender, and deterministic clock/jitter.
// Production has no alternate store path; tests inject only package-private
// begin/commit failure hooks and the same *store.Queries transaction binding.
type WorkerOptions struct {
	// Pool opens every lease, cleanup, claim, and send transaction.
	Pool *store.Pool
	// Queries is bound to each transaction with WithTx.
	Queries *store.Queries
	// KeyRing opens each leased job's sealed payload before a send.
	KeyRing *KeyRing
	// Sender delivers each authorized message.
	Sender Sender
	// FooterNote is added to every message body; an empty note adds nothing.
	FooterNote string
	// Clock supplies the time used for leases, cleanup cutoffs, and job state.
	Clock func() time.Time
	// Jitter maps a temporary failure's backoff cap to the retry delay.
	Jitter func(time.Duration) time.Duration
	// Logger receives tick errors and worker warnings.
	Logger *slog.Logger
	// WorkerID is the lease owner stamped on claimed jobs; uuid.Nil is rejected.
	WorkerID uuid.UUID
	// NoticeAuthority authorizes privacy notice jobs; nil disables them.
	NoticeAuthority NoticeAuthority
}

// Worker polls once per second, claims at most ten jobs, and sends at most two
// at once. Every send is authorized and finalized inside one transaction that
// locks the scope row first and the leased job second, holds both locks through
// the at-most-ten-second sender call, and commits sent/terminal/requeue state.
type Worker struct {
	pool            poolBeginner
	queries         *store.Queries
	ring            *KeyRing
	sender          Sender
	footerNote      string
	clock           func() time.Time
	jitter          func(time.Duration) time.Duration
	logger          *slog.Logger
	id              uuid.UUID
	noticeAuthority NoticeAuthority

	// package-private hooks tests use as deterministic barriers; nil in
	// production.
	hooks workerHooks
}

// poolBeginner is the narrow transaction-opening surface Worker needs. Both
// *store.Pool and test fakes satisfy it.
type poolBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

type workerHooks struct {
	afterClaim func(jobs []store.AuthEmailJob)
	beforeAuth func(jobID uuid.UUID)
	beforeSend func(jobID uuid.UUID)
	afterSend  func(jobID uuid.UUID, outcome SendOutcome)
}

// NewWorker validates every option and returns a Worker. A nil pool, queries,
// ring, sender, clock, jitter, logger, or nil worker ID is rejected.
func NewWorker(opts WorkerOptions) (*Worker, error) {
	if opts.Pool == nil || opts.Queries == nil || opts.KeyRing == nil || opts.Sender == nil {
		return nil, ErrWorker
	}
	if opts.Clock == nil || opts.Jitter == nil || opts.Logger == nil {
		return nil, ErrWorker
	}
	if opts.WorkerID == uuid.Nil {
		return nil, ErrWorker
	}
	return &Worker{
		pool:            opts.Pool,
		queries:         opts.Queries,
		ring:            opts.KeyRing,
		sender:          opts.Sender,
		footerNote:      opts.FooterNote,
		clock:           opts.Clock,
		jitter:          opts.Jitter,
		logger:          opts.Logger,
		id:              opts.WorkerID,
		noticeAuthority: opts.NoticeAuthority,
	}, nil
}

// Run polls once per second until ctx is canceled. A tick error is logged and
// the loop continues; cancellation returns nil promptly after in-flight sends
// join (each send has its own ten-second deadline).
func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		if err := w.RunOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			w.logger.Error("authmail: worker tick failed", "err", "tick error")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// RunOnce runs one full tick: requeue expired leases, run the three bounded
// cleanups, claim at most ten jobs, and send each claimed job with at most two
// concurrent sends. It joins every send before returning, so a canceled ctx
// leaves in-flight jobs in a recoverable leased state rather than a partial
// finalize.
func (w *Worker) RunOnce(ctx context.Context) error {
	now := w.clock()

	if err := w.requeueExpired(ctx, now); err != nil {
		return err
	}
	if err := w.cleanup(ctx, now); err != nil {
		return err
	}

	jobs, err := w.claim(ctx, now)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrentSends)
	for _, job := range jobs {
		job := job
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := w.sendJob(ctx, job); err != nil {
				if Kind(job.Kind) == KindPrivacyNotice {
					w.logger.Warn("authmail: notice send failed")
				} else {
					w.logger.Warn("authmail: send job failed", "job", job.ID, "err", "send failed")
				}
			}
		}()
	}
	wg.Wait()
	return nil //nolint:nilerr // a canceled ctx stops new sends but is not a tick error; Run observes ctx.Done() to stop.
}

// claim atomically transitions at most ten due pending jobs to leased in one
// transaction, stamping the lease owner and a thirty-second expiry, and returns
// them in (next_attempt_at, created_at, id) order.
func (w *Worker) claim(ctx context.Context, now time.Time) ([]store.AuthEmailJob, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			w.logger.Warn("authmail: rollback transaction failed")
		}
	}()
	jobs, err := w.queries.WithTx(tx).ClaimAuthEmailJobs(ctx, store.ClaimAuthEmailJobsParams{
		LeaseOwner:     w.id.String(),
		LeaseExpiresAt: now.Add(leaseDuration),
		Now:            now,
		LimitRows:      claimBatchSize,
		IncludeNotice:  w.noticeAllowed(ctx, w.queries.WithTx(tx)),
	})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	sort.SliceStable(jobs, func(i, j int) bool {
		return Kind(jobs[i].Kind) != KindPrivacyNotice && Kind(jobs[j].Kind) == KindPrivacyNotice
	})
	if w.hooks.afterClaim != nil {
		w.hooks.afterClaim(jobs)
	}
	return jobs, nil
}

// compile-time assertion that a *store.Pool opens transactions as expected.
var _ poolBeginner = (*store.Pool)(nil)
