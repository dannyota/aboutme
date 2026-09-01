package oauthsrv

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const idleClientGCBatch = 200

// CollectIdleClients deletes one bounded page of clients with no live grant or
// token. Candidate selection and deletion share the transaction so the query's
// FOR UPDATE SKIP LOCKED reservation remains effective through the delete.
func (s *Service) CollectIdleClients(ctx context.Context) (returnErr error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin idle-client GC transaction: %w", err)
	}
	defer func() {
		rollbackErr := tx.Rollback(context.WithoutCancel(ctx))
		if rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) && returnErr == nil {
			returnErr = fmt.Errorf("rollback idle-client GC transaction: %w", rollbackErr)
		}
	}()

	now := s.clock()
	queries := store.New(tx)
	ids, err := queries.ListIdleOAuthClientCandidates(ctx, store.ListIdleOAuthClientCandidatesParams{
		IdleBefore: now.Add(-24 * time.Hour),
		Now:        now,
		LimitRows:  idleClientGCBatch,
	})
	if err != nil {
		return fmt.Errorf("list idle OAuth clients: %w", err)
	}
	if len(ids) > idleClientGCBatch {
		return errors.New("idle OAuth client candidate batch exceeds limit")
	}
	if len(ids) > 0 {
		if _, err := queries.DeleteOAuthClients(ctx, ids); err != nil {
			return fmt.Errorf("delete idle OAuth clients: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit idle-client GC transaction: %w", err)
	}
	return nil
}
