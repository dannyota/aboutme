package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dannyota/aboutme/apps/server/internal/pgtransport"
)

const writeTxCleanupTimeout = 5 * time.Second

// WriteTxRunner runs writes inside the runtime write barrier.
type WriteTxRunner interface {
	WithWriteTx(context.Context, pgx.TxOptions, func(*Queries) error) error
	ExecWrite(context.Context, func(*Queries) error) error
}

type writeTxLease interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	Release(context.Context) error
	Destroy(context.Context) error
}

type pooledWriteTxLease struct {
	conn       *pgxpool.Conn
	retirement *pgtransport.Capability
}

// BeginTx begins a transaction while retaining the acquired pool lease.
func (l *pooledWriteTxLease) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	return l.conn.BeginTx(ctx, options)
}

// Release returns a confirmed-clean connection to the pool.
func (l *pooledWriteTxLease) Release(ctx context.Context) error {
	if err := l.retirement.ReleaseCleanPGX(l.conn.Conn()); err != nil {
		return errors.Join(err, l.Destroy(ctx))
	}
	l.conn.Release()
	return nil
}

// Destroy removes the connection from the pool and closes it.
func (l *pooledWriteTxLease) Destroy(ctx context.Context) error {
	conn := l.conn.Hijack()
	result := l.retirement.RetirePGX(ctx, conn)
	if !result.PhysicalClosed {
		return errors.Join(result.Err, errors.New("store: physical write connection retirement was not confirmed"))
	}
	return result.Err
}

type writeTxRunner struct {
	acquire func(context.Context) (writeTxLease, error)
}

// NewWriteTxRunner constructs a runner backed by pool.
func NewWriteTxRunner(pool *pgxpool.Pool) WriteTxRunner {
	return &writeTxRunner{acquire: func(ctx context.Context) (writeTxLease, error) {
		if pool == nil {
			return nil, errors.New("store: nil write transaction pool")
		}
		conn, err := pool.Acquire(ctx)
		if err != nil {
			return nil, err
		}
		retirement, err := pgtransport.CapturePGX(conn.Conn())
		if err != nil {
			closeErr := conn.Hijack().Close(ctx)
			return nil, errors.Join(fmt.Errorf("store: capture write transaction transport: %w", err), closeErr)
		}
		return &pooledWriteTxLease{conn: conn, retirement: retirement}, nil
	}}
}

// WithWriteTx runs callback after entry and finishes before committing.
func (r *writeTxRunner) WithWriteTx(ctx context.Context, options pgx.TxOptions, callback func(*Queries) error) error {
	if r == nil || r.acquire == nil {
		return errors.New("store: nil write transaction runner")
	}
	if callback == nil {
		return errors.New("store: nil write transaction callback")
	}

	lease, err := r.acquire(ctx)
	if err != nil {
		return fmt.Errorf("store: acquire write transaction connection: %w", err)
	}
	if lease == nil {
		return errors.New("store: acquire write transaction returned nil connection")
	}

	tx, err := lease.BeginTx(ctx, options)
	if err != nil {
		return errors.Join(fmt.Errorf("store: begin write transaction: %w", err), destroyWriteLease(ctx, lease))
	}
	if tx == nil {
		return errors.Join(errors.New("store: begin write transaction returned nil transaction"), destroyWriteLease(ctx, lease))
	}

	if _, err := tx.Exec(ctx, "SELECT public.runtime_enter_write()"); err != nil {
		cleanupCtx, cancel := writeCleanupContext(ctx)
		rollbackErr := tx.Rollback(cleanupCtx)
		var cleanupErr error
		if isSQLState(err, "55000") && rollbackErr == nil {
			cleanupErr = lease.Release(cleanupCtx)
		} else {
			cleanupErr = errors.Join(rollbackErr, lease.Destroy(cleanupCtx))
		}
		cancel()
		return errors.Join(fmt.Errorf("store: enter write transaction: %w", err), cleanupErr)
	}

	callbackReturned := false
	defer func() {
		if !callbackReturned {
			ignoreWriteCleanupError(cleanupWriteCallback(ctx, tx, lease, false))
		}
	}()
	panicValue, panicked, callbackErr := invokeWriteCallback(callback, New(tx))
	callbackReturned = true
	if callbackErr != nil || panicked {
		poisoned := isSQLState(callbackErr, "AM001")
		if panicked {
			if panicErr, ok := panicValue.(error); ok {
				poisoned = isSQLState(panicErr, "AM001")
			}
		}
		cleanupErr := cleanupWriteCallback(ctx, tx, lease, poisoned)
		if panicked {
			panic(panicValue)
		}
		return errors.Join(callbackErr, cleanupErr)
	}

	if _, err := tx.Exec(ctx, "SELECT public.runtime_finish_write()"); err != nil {
		cleanupCtx, cancel := writeCleanupContext(ctx)
		rollbackErr := tx.Rollback(cleanupCtx)
		destroyErr := lease.Destroy(cleanupCtx)
		cancel()
		return errors.Join(fmt.Errorf("store: finish write transaction: %w", err), rollbackErr, destroyErr)
	}
	if err := tx.Commit(ctx); err != nil {
		return errors.Join(fmt.Errorf("store: commit write transaction: %w", err), destroyWriteLease(ctx, lease))
	}
	return lease.Release(ctx)
}

// ExecWrite runs callback with default transaction options.
func (r *writeTxRunner) ExecWrite(ctx context.Context, callback func(*Queries) error) error {
	return r.WithWriteTx(ctx, pgx.TxOptions{}, callback)
}

func invokeWriteCallback(callback func(*Queries) error, queries *Queries) (panicValue any, panicked bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panicValue = recovered
			panicked = true
		}
	}()
	err = callback(queries)
	return nil, false, err
}

func writeCleanupContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), writeTxCleanupTimeout)
}

func destroyWriteLease(ctx context.Context, lease writeTxLease) error {
	cleanupCtx, cancel := writeCleanupContext(ctx)
	defer cancel()
	return lease.Destroy(cleanupCtx)
}

func cleanupWriteCallback(ctx context.Context, tx pgx.Tx, lease writeTxLease, poisoned bool) error {
	cleanupCtx, cancel := writeCleanupContext(ctx)
	defer cancel()
	if rollbackErr := tx.Rollback(cleanupCtx); poisoned || rollbackErr != nil {
		return errors.Join(rollbackErr, lease.Destroy(cleanupCtx))
	}
	return lease.Release(cleanupCtx)
}

func isSQLState(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}

func ignoreWriteCleanupError(error) {}
