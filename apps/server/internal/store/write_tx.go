package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const writeTxCleanupTimeout = 5 * time.Second

// WriteTxRunner runs writes inside the runtime write barrier.
type WriteTxRunner interface {
	WithWriteTx(context.Context, pgx.TxOptions, func(*Queries) error) error
	ExecWrite(context.Context, func(*Queries) error) error
}

type writeTxLease interface {
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	Release()
	Destroy(context.Context)
}

type pooledWriteTxLease struct {
	conn *pgxpool.Conn
}

// BeginTx begins a transaction while retaining the acquired pool lease.
func (l *pooledWriteTxLease) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	return l.conn.BeginTx(ctx, options)
}

// Release returns a confirmed-clean connection to the pool.
func (l *pooledWriteTxLease) Release() {
	l.conn.Release()
}

// Destroy removes the connection from the pool and closes it.
func (l *pooledWriteTxLease) Destroy(ctx context.Context) {
	if err := l.conn.Hijack().Close(ctx); err != nil {
		return
	}
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
		return &pooledWriteTxLease{conn: conn}, nil
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
		destroyWriteLease(ctx, lease)
		return fmt.Errorf("store: begin write transaction: %w", err)
	}
	if tx == nil {
		destroyWriteLease(ctx, lease)
		return errors.New("store: begin write transaction returned nil transaction")
	}

	if _, err := tx.Exec(ctx, "SELECT public.runtime_enter_write()"); err != nil {
		cleanupCtx, cancel := writeCleanupContext(ctx)
		rollbackErr := tx.Rollback(cleanupCtx)
		if isSQLState(err, "55000") && rollbackErr == nil {
			lease.Release()
		} else {
			lease.Destroy(cleanupCtx)
		}
		cancel()
		return fmt.Errorf("store: enter write transaction: %w", err)
	}

	callbackReturned := false
	defer func() {
		if !callbackReturned {
			cleanupWriteCallback(ctx, tx, lease, false)
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
		cleanupWriteCallback(ctx, tx, lease, poisoned)
		if panicked {
			panic(panicValue)
		}
		return callbackErr
	}

	if _, err := tx.Exec(ctx, "SELECT public.runtime_finish_write()"); err != nil {
		cleanupCtx, cancel := writeCleanupContext(ctx)
		ignoreWriteCleanupError(tx.Rollback(cleanupCtx))
		lease.Destroy(cleanupCtx)
		cancel()
		return fmt.Errorf("store: finish write transaction: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		destroyWriteLease(ctx, lease)
		return fmt.Errorf("store: commit write transaction: %w", err)
	}
	lease.Release()
	return nil
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

func destroyWriteLease(ctx context.Context, lease writeTxLease) {
	cleanupCtx, cancel := writeCleanupContext(ctx)
	defer cancel()
	lease.Destroy(cleanupCtx)
}

func cleanupWriteCallback(ctx context.Context, tx pgx.Tx, lease writeTxLease, poisoned bool) {
	cleanupCtx, cancel := writeCleanupContext(ctx)
	defer cancel()
	if rollbackErr := tx.Rollback(cleanupCtx); poisoned || rollbackErr != nil {
		lease.Destroy(cleanupCtx)
	} else {
		lease.Release()
	}
}

func isSQLState(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}

func ignoreWriteCleanupError(error) {}
