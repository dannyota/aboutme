package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/accountapi"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const operatorReadTimeout = 30 * time.Second

func operatorReadPool(ctx context.Context, getenv func(string) string) (*store.Pool, error) {
	cfg, err := config.LoadPrivacyJob(getenv, false)
	if err != nil {
		return nil, err
	}
	return store.NewPool(ctx, cfg.DatabaseURL)
}

func executeAccountStatus(ctx context.Context, id uuid.UUID, getenv func(string) string, output io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, operatorReadTimeout)
	defer cancel()
	pool, err := operatorReadPool(ctx, getenv)
	if err != nil {
		return errors.New("account-delete status failed")
	}
	defer closeOperatorReadPool(ctx, pool)
	return reportAccountStatus(ctx, id, output, func(readCtx context.Context, userID uuid.UUID) (bool, error) {
		return accountapi.AccountExists(readCtx, pool, userID)
	})
}

func closeOperatorReadPool(ctx context.Context, pool *store.Pool) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	pool.Close(cleanup)
}

func reportAccountStatus(ctx context.Context, id uuid.UUID, output io.Writer, read func(context.Context, uuid.UUID) (bool, error)) error {
	present, err := read(ctx, id)
	if err != nil {
		return errors.New("account-delete status failed")
	}
	state := "absent"
	if present {
		state = "present"
	}
	result := struct {
		Success bool   `json:"success"`
		State   string `json:"state"`
	}{true, state}
	if err := json.NewEncoder(output).Encode(result); err != nil {
		return errors.New("account-delete status failed")
	}
	return nil
}
