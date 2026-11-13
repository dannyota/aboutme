package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/secondfactor"
	"danny.vn/aboutme/apps/server/internal/store"
)

const (
	totpKeyReencryptCommandName = "totp-key-reencrypt"
	totpReencryptCommandTimeout = 30 * time.Minute
	totpReencryptCleanupTimeout = 5 * time.Second
	totpReencryptPingTimeout    = 10 * time.Second

	totpHealthCheckInterval = 5 * time.Minute
)

func runTOTPHealthTicker(ctx context.Context, logger *slog.Logger, checker *secondfactor.TOTPHealthChecker) {
	ticker := time.NewTicker(totpHealthCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := checker.Check(ctx); err != nil {
				logger.Error("totp key health check failed", "err", "query failed")
			}
		}
	}
}

func runTOTPKeyReencrypt(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: server totp-key-reencrypt")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, totpReencryptCommandTimeout)
	defer cancel()
	return executeTOTPKeyReencrypt(ctx, os.Getenv, os.Stdout, os.Stderr)
}

func executeTOTPKeyReencrypt(ctx context.Context, getenv func(string) string, output, diagnostics io.Writer) error {
	logger := slog.New(slog.NewJSONHandler(diagnostics, nil))
	result, runErr := runTOTPReencryptJob(ctx, getenv, logger)
	report := struct {
		Job     string `json:"job"`
		Success bool   `json:"success"`
		Result  any    `json:"result"`
	}{Job: totpKeyReencryptCommandName, Success: runErr == nil, Result: result}
	if err := json.NewEncoder(output).Encode(report); err != nil {
		return errors.New("totp key reencrypt result could not be written")
	}
	if runErr != nil {
		return errors.New("totp key reencrypt command failed; inspect its fixed result and diagnostics")
	}
	return nil
}

func runTOTPReencryptJob(ctx context.Context, getenv func(string) string, logger *slog.Logger) (secondfactor.TOTPReencryptResult, error) {
	cfg, err := config.LoadTOTPReencryptJob(getenv)
	if err != nil {
		return secondfactor.TOTPReencryptResult{}, errors.New("totp key reencrypt configuration is invalid")
	}
	pool, err := store.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return secondfactor.TOTPReencryptResult{}, errors.New("totp key reencrypt database configuration is invalid")
	}
	defer func() {
		cleanupCtx, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), totpReencryptCleanupTimeout)
		defer cancelCleanup()
		pool.Close(cleanupCtx)
	}()
	probeCtx, cancelProbe := context.WithTimeout(ctx, totpReencryptPingTimeout)
	err = pool.Ping(probeCtx)
	cancelProbe()
	if err != nil {
		return secondfactor.TOTPReencryptResult{}, errors.New("totp key reencrypt database is unavailable")
	}
	ring, err := secondfactor.NewTOTPKeyRing(cfg.TOTPActiveKey, cfg.TOTPPreviousKey, rand.Reader)
	if err != nil {
		return secondfactor.TOTPReencryptResult{}, errors.New("totp key ring is invalid")
	}
	signal := secondfactor.NewTOTPUnavailableSignal(time.Now, logger)
	runner, err := secondfactor.NewTOTPRotationRunner(secondfactor.TOTPRotationConfig{
		Pool: pool, Ring: ring, Now: time.Now, Signal: signal, Logger: logger,
	})
	if err != nil {
		return secondfactor.TOTPReencryptResult{}, errors.New("totp rotation runner could not start")
	}
	return runner.Run(ctx)
}
