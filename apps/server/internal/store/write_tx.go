package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const writeTxCleanupTimeout = 5 * time.Second

// WriteTxStarter starts a transaction that has entered the runtime write barrier.
type WriteTxStarter interface {
	BeginWrite(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

type poolWriteTxStarter struct {
	begin func(context.Context, pgx.TxOptions) (pgx.Tx, error)
}

// NewWriteTxStarter returns a write transaction starter backed by pool.
func NewWriteTxStarter(pool *pgxpool.Pool) WriteTxStarter {
	return &poolWriteTxStarter{begin: func(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
		if pool == nil {
			return nil, errors.New("store: nil write transaction pool")
		}
		return pool.BeginTx(ctx, options)
	}}
}

// BeginWrite starts a transaction and enters the runtime write barrier.
func (s *poolWriteTxStarter) BeginWrite(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	if s == nil || s.begin == nil {
		return nil, errors.New("store: nil write transaction starter")
	}
	tx, err := s.begin(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("store: begin write transaction: %w", err)
	}
	if tx == nil {
		return nil, errors.New("store: begin write transaction returned nil transaction")
	}
	if _, err := tx.Exec(ctx, "SELECT public.runtime_enter_write()"); err != nil {
		rollbackWriteTx(ctx, tx)
		return nil, fmt.Errorf("store: enter write transaction: %w", err)
	}
	return tx, nil
}

// WithWriteTx runs callback in a transaction that has entered the runtime write barrier.
func WithWriteTx(ctx context.Context, starter WriteTxStarter, options pgx.TxOptions, callback func(*Queries) error) error {
	if starter == nil {
		return errors.New("store: nil write transaction starter")
	}
	if callback == nil {
		return errors.New("store: nil write transaction callback")
	}

	tx, err := starter.BeginWrite(ctx, options)
	if err != nil {
		return err
	}
	finalized := false
	defer func() {
		if !finalized {
			rollbackWriteTx(ctx, tx)
		}
	}()

	if err := callback(New(tx)); err != nil {
		rollbackWriteTx(ctx, tx)
		finalized = true
		return err
	}
	finalized = true
	return tx.Commit(ctx)
}

// ExecWrite runs one logical write in a default-options write transaction.
func ExecWrite(ctx context.Context, starter WriteTxStarter, callback func(*Queries) error) error {
	return WithWriteTx(ctx, starter, pgx.TxOptions{}, callback)
}

func rollbackWriteTx(ctx context.Context, tx pgx.Tx) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTxCleanupTimeout)
	defer cancel()
	if err := tx.Rollback(cleanupCtx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return
	}
}
