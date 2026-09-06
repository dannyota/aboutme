// Package mediacleanup drains exact-key media deletion work and reconciles
// old private objects against current PostgreSQL ownership state.
package mediacleanup

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const (
	deletionPageSize = 200
	deletionRunLimit = 2_000
	orphanPageSize   = 1_000
	orphanRunLimit   = 10_000
	workerCount      = 4
	leaseDuration    = 30 * time.Second
	objectDeadline   = 5 * time.Second
	orphanMinimumAge = 48 * time.Hour
	overdueAge       = 24 * time.Hour

	advisoryNamespace = int32(0x61626d65)
	deletionLock      = int32(1)
	orphanLock        = int32(2)
)

var (
	// ErrDatabase reports a cleanup database failure without exposing database
	// details through a command diagnostic.
	ErrDatabase = errors.New("mediacleanup: database operation failed")
	// ErrBackend reports an invalid or failed listing without exposing an
	// object key or backend response.
	ErrBackend = errors.New("mediacleanup: media backend operation failed")
	// ErrLockRelease reports that a run could not prove its advisory lock was
	// released. The locked connection is destroyed before this is returned.
	ErrLockRelease = errors.New("mediacleanup: advisory lock release failed")
)

// Config provides the cleanup worker's production dependencies.
type Config struct {
	Pool   *store.Pool
	Media  media.Backend
	Logger *slog.Logger
	Now    func() time.Time
}

// Result is the fixed, identifier-free command result surface used by both
// cleanup modes. Backlog is the pending-job count for deletion and 1 when an
// orphan run stops at its ceiling with another page available.
type Result struct {
	Overlap    bool
	Scanned    int64
	Candidates int64
	Claimed    int64
	Succeeded  int64
	Deleted    int64
	Absent     int64
	Live       int64
	Queued     int64
	Enqueued   int64
	Failed     int64
	Overdue    int64
	Backlog    int64
	OldestAge  time.Duration
	Duration   time.Duration
}

// Worker runs bounded one-shot media cleanup jobs.
type Worker struct {
	pool   *store.Pool
	media  media.Backend
	logger *slog.Logger
	now    func() time.Time
	wait   func(context.Context, time.Duration) bool
	unlock func(context.Context, *pgxpool.Conn, int32) (bool, error)
	hooks  workerHooks
}

type workerHooks struct {
	afterOrphanClaim func(store.MediaDeletionJob)
}

// New constructs a worker and rejects missing stateful dependencies.
func New(config Config) (*Worker, error) {
	if config.Pool == nil {
		return nil, errors.New("mediacleanup: nil pool")
	}
	if config.Media == nil {
		return nil, errors.New("mediacleanup: nil media backend")
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Worker{
		pool: config.Pool, media: config.Media, logger: config.Logger,
		now: config.Now, wait: waitBackoff, unlock: unlockRun,
	}, nil
}

func retryDelay(attempt int32) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Minute
	for i := int32(1); i < attempt; i++ {
		if delay >= 3*time.Hour {
			return 6 * time.Hour
		}
		delay *= 2
	}
	return delay
}

type listedObject struct {
	object   media.Object
	resumeID uuid.UUID
}

func validateListedPage(cursor string, objects []media.Object, nextCursor string) ([]listedObject, error) {
	if len(objects) > orphanPageSize {
		return nil, ErrBackend
	}
	if cursor != "" {
		if _, err := parseListedKey(cursor); err != nil {
			return nil, ErrBackend
		}
	}
	validated := make([]listedObject, 0, len(objects))
	previous := cursor
	for _, object := range objects {
		resumeID, err := parseListedKey(object.Key)
		if err != nil || object.UpdatedAt.IsZero() || (previous != "" && object.Key <= previous) {
			return nil, ErrBackend
		}
		validated = append(validated, listedObject{object: object, resumeID: resumeID})
		previous = object.Key
	}
	if nextCursor != "" {
		if len(objects) != orphanPageSize || nextCursor != previous {
			return nil, ErrBackend
		}
		if _, err := parseListedKey(nextCursor); err != nil {
			return nil, ErrBackend
		}
	}
	return validated, nil
}

func parseListedKey(key string) (uuid.UUID, error) {
	segments := strings.Split(key, "/")
	if len(segments) != 3 || segments[0] != "resumes" {
		return uuid.Nil, media.ErrInvalidKey
	}
	resumeID, err := uuid.Parse(segments[1])
	if err != nil || resumeID.String() != segments[1] {
		return uuid.Nil, media.ErrInvalidKey
	}
	if _, err := media.ParsePhotoKey(resumeID, key); err != nil {
		return uuid.Nil, err
	}
	return resumeID, nil
}

type runItemResult struct {
	succeeded bool
	deleted   bool
	absent    bool
	live      bool
	queued    bool
	enqueued  bool
	failed    bool
	unsafe    bool
}

func addItemResult(result *Result, item runItemResult) {
	if item.succeeded {
		result.Succeeded++
	}
	if item.deleted {
		result.Deleted++
	}
	if item.absent {
		result.Absent++
	}
	if item.live {
		result.Live++
	}
	if item.queued {
		result.Queued++
	}
	if item.enqueued {
		result.Enqueued++
	}
	if item.failed {
		result.Failed++
	}
}

func processBounded[T any](ctx context.Context, items []T, fn func(context.Context, T) runItemResult) []runItemResult {
	results := make([]runItemResult, len(items))
	if len(items) == 0 {
		return results
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(workerCount, len(items)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				results[index] = fn(ctx, items[index])
			}
		}()
	}
	for index := range items {
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	return results
}

func (w *Worker) acquireRunLock(ctx context.Context, lock int32) (*pgxpool.Conn, bool, error) {
	connection, err := w.pool.Acquire(ctx)
	if err != nil {
		return nil, false, ErrDatabase
	}
	var acquired bool
	if err := connection.QueryRow(ctx, `SELECT pg_try_advisory_lock($1, $2)`, advisoryNamespace, lock).Scan(&acquired); err != nil {
		connection.Release()
		return nil, false, ErrDatabase
	}
	if !acquired {
		connection.Release()
		return nil, false, nil
	}
	return connection, true, nil
}

func (w *Worker) releaseRunLock(ctx context.Context, connection *pgxpool.Conn, lock int32) error {
	cleanupBase := context.WithoutCancel(ctx)
	unlockCtx, cancelUnlock := context.WithTimeout(cleanupBase, objectDeadline)
	released, err := w.unlock(unlockCtx, connection, lock)
	cancelUnlock()
	if err != nil || !released {
		w.logger.ErrorContext(cleanupBase, "mediacleanup: advisory unlock failed")
		closeCtx, cancelClose := context.WithTimeout(cleanupBase, objectDeadline)
		if closeErr := connection.Hijack().Close(closeCtx); closeErr != nil {
			w.logger.ErrorContext(cleanupBase, "mediacleanup: advisory lock connection close failed")
		}
		cancelClose()
		return ErrLockRelease
	}
	connection.Release()
	return nil
}

func unlockRun(ctx context.Context, connection *pgxpool.Conn, lock int32) (bool, error) {
	var released bool
	err := connection.QueryRow(ctx, `SELECT pg_advisory_unlock($1, $2)`, advisoryNamespace, lock).Scan(&released)
	return released, err
}

func durationSince(now func() time.Time, started time.Time) time.Duration {
	elapsed := now().Sub(started)
	if elapsed < 0 {
		return 0
	}
	return elapsed
}
