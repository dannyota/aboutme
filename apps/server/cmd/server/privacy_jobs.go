package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/mediacleanup"
	"github.com/dannyota/aboutme/apps/server/internal/privacyretention"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const (
	privacyJobTimeout        = 30 * time.Minute
	privacyJobCleanupTimeout = 5 * time.Second
)

func runCommand(args []string) error {
	if len(args) == 0 {
		return run()
	}
	command, err := parsePrivacyCommand(args)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, privacyJobTimeout)
	defer cancel()
	return executePrivacyCommand(ctx, command, os.Getenv, os.Stdout, os.Stderr)
}

func executePrivacyCommand(ctx context.Context, command privacyCommand, getenv func(string) string, output, diagnostics io.Writer) error {
	logger := slog.New(slog.NewJSONHandler(diagnostics, nil))
	result, runErr := runPrivacyJob(ctx, command, getenv, logger)
	report := struct {
		Job     string `json:"job"`
		DryRun  bool   `json:"dryRun"`
		Success bool   `json:"success"`
		Result  any    `json:"result"`
	}{Job: command.name, DryRun: command.dryRun, Success: runErr == nil, Result: result}
	if err := json.NewEncoder(output).Encode(report); err != nil {
		return errors.New("privacy command result could not be written")
	}
	if runErr != nil {
		return errors.New("privacy command failed; inspect its fixed result and diagnostics")
	}
	return nil
}

func runPrivacyJob(ctx context.Context, command privacyCommand, getenv func(string) string, logger *slog.Logger) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	needsMedia := command.name == "media-deletion-sweep" || command.name == "media-orphan-sweep"
	cfg, err := config.LoadPrivacyJob(getenv, needsMedia)
	if err != nil {
		return nil, errors.New("privacy job configuration is invalid")
	}
	pool, err := store.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, errors.New("privacy job database configuration is invalid")
	}
	defer func() {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), privacyJobCleanupTimeout)
		defer cancelCleanup()
		pool.Close(cleanupCtx)
	}()
	probeCtx, cancelProbe := context.WithTimeout(ctx, 10*time.Second)
	err = pool.Ping(probeCtx)
	cancelProbe()
	if err != nil {
		return nil, errors.New("privacy job database is unavailable")
	}
	if needsMedia {
		blobs, backendErr := newMediaBackend(ctx, cfg)
		if backendErr != nil {
			return nil, errors.New("privacy job media is unavailable")
		}
		worker, workerErr := mediacleanup.New(mediacleanup.Config{Pool: pool, Media: blobs, Logger: logger, Now: time.Now})
		if workerErr != nil {
			return nil, errors.New("privacy media job could not start")
		}
		if command.name == "media-deletion-sweep" {
			return worker.DeleteDue(ctx)
		}
		return worker.Reconcile(ctx, command.dryRun)
	}
	worker, err := privacyretention.New(privacyretention.Config{Pool: pool, Logger: logger, Now: time.Now})
	if err != nil {
		return nil, errors.New("privacy retention job could not start")
	}
	if command.name == "idempotency-expiry-sweep" {
		return worker.ExpireIdempotency(ctx)
	}
	return worker.Retain(ctx)
}
