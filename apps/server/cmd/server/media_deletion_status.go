package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/accountapi"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const mediaDeletionStatusCommandName = "media-deletion-status"

func runMediaDeletionStatus(args []string) error {
	if len(args) != 0 {
		return accountDeleteExitError{code: 2, message: "usage: server media-deletion-status"}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return executeMediaDeletionStatus(ctx, os.Getenv, os.Stdout)
}

func executeMediaDeletionStatus(ctx context.Context, getenv func(string) string, output io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, operatorReadTimeout)
	defer cancel()
	pool, err := operatorReadPool(ctx, getenv)
	if err != nil {
		return errors.New("media-deletion-status failed")
	}
	defer closeOperatorReadPool(ctx, pool)
	return reportMediaDeletionStatus(ctx, output, func(readCtx context.Context) (int64, error) {
		var pending int64
		snapshotErr := accountapi.ReadOperatorSnapshot(readCtx, pool, func(snapshotCtx context.Context, tx pgx.Tx) error {
			now := time.Now().UTC()
			row, queryErr := store.New(tx).GetMediaDeletionQueueState(snapshotCtx, store.GetMediaDeletionQueueStateParams{Now: now, OverdueCutoff: now})
			if queryErr != nil {
				return queryErr
			}
			pending = row.Pending
			return nil
		})
		return pending, snapshotErr
	})
}

func reportMediaDeletionStatus(ctx context.Context, output io.Writer, read func(context.Context) (int64, error)) error {
	pending, err := read(ctx)
	if err != nil || pending < 0 {
		return errors.New("media-deletion-status failed")
	}
	result := struct {
		Success bool  `json:"success"`
		Pending int64 `json:"pending"`
	}{true, pending}
	if err := json.NewEncoder(output).Encode(result); err != nil {
		return errors.New("media-deletion-status failed")
	}
	return nil
}
