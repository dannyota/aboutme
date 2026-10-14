package accountapi

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// ReadOperatorSnapshot bounds operator reads without locking account rows.
func ReadOperatorSnapshot(ctx context.Context, pool *store.Pool, read func(context.Context, pgx.Tx) error) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		// Rollback is also safe after a successful commit.
		if rollbackErr := tx.Rollback(cleanup); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			slog.ErrorContext(cleanup, "operator snapshot rollback failed")
		}
	}()
	if _, err = tx.Exec(ctx, `SET LOCAL statement_timeout = '30s'`); err != nil {
		return err
	}
	if err = read(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AccountExists returns only UUID existence, independent of mutable slugs.
func AccountExists(ctx context.Context, pool *store.Pool, id uuid.UUID) (bool, error) {
	var present bool
	err := ReadOperatorSnapshot(ctx, pool, func(readCtx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(readCtx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1)`, id).Scan(&present)
	})
	if err != nil {
		return false, errors.New("account status unavailable")
	}
	return present, nil
}
