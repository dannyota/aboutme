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

	"danny.vn/aboutme/apps/server/internal/store"
)

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

type WorkerOptions struct {
	Pool            *store.Pool
	Queries         *store.Queries
	KeyRing         *KeyRing
	Sender          Sender
	FooterNote      string
	Clock           func() time.Time
	Jitter          func(time.Duration) time.Duration
	Logger          *slog.Logger
	WorkerID        uuid.UUID
	NoticeAuthority NoticeAuthority
}

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

	hooks workerHooks
}

type poolBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

type workerHooks struct {
	afterClaim func(jobs []store.AuthEmailJob)
	beforeAuth func(jobID uuid.UUID)
	beforeSend func(jobID uuid.UUID)
	afterSend  func(jobID uuid.UUID, outcome SendOutcome)
}

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

var _ poolBeginner = (*store.Pool)(nil)
