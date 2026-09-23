package main

// TOTP key-ring composition helpers: the issuer derived from PUBLIC_ORIGIN,
// the periodic key-health ticker, and the one-shot `server totp-key-reencrypt`
// command. See docs/design/totp-key-management.md and
// docs/design/totp-second-factor-contract.md.

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/secondfactor"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const (
	totpKeyReencryptCommandName = "totp-key-reencrypt"
	totpReencryptCommandTimeout = 30 * time.Minute
	totpReencryptCleanupTimeout = 5 * time.Second
	totpReencryptPingTimeout    = 10 * time.Second

	// totpHealthCheckInterval is the periodic key-health tick
	// (docs/design/totp-key-management.md "Key failures": "At startup and
	// every five minutes, a bounded query reads at most three distinct key
	// IDs...").
	totpHealthCheckInterval = 5 * time.Minute
)

// totpIssuerFromPublicOrigin derives the canonical TOTP issuer — the
// lowercase ASCII PUBLIC_ORIGIN host, no port — from cfg.PublicOrigin, which
// internal/config.loadPublicOrigin has already normalized to a plain
// scheme://host[:port] origin. secondfactor.New's ValidateTOTPIssuer call
// rejects an IP literal or non-canonical host when TOTP enrollment is
// enabled, so this helper only extracts the host; it does not itself
// validate it (docs/design/totp-second-factor-contract.md "Provisioning
// data").
func totpIssuerFromPublicOrigin(publicOrigin string) (string, error) {
	u, err := url.Parse(publicOrigin)
	if err != nil || u.Host == "" {
		return "", errors.New("parse public origin for totp issuer")
	}
	return u.Hostname(), nil
}

// runTOTPHealthTicker calls check every totpHealthCheckInterval until ctx is
// done. A query failure is logged and never stops the ticker or crashes the
// process: only a stored key ID outside the ring or a decrypt failure is a
// totp_unavailable reason, and those are emitted by checker.Check itself
// through the shared signal, not through this loop's return value
// (docs/design/totp-key-management.md "Key failures").
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

// runTOTPKeyReencrypt is the `server totp-key-reencrypt` entry point: the
// bounded one-shot re-encryption command
// (docs/design/totp-key-management.md "Rotation"). It takes no arguments.
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

// executeTOTPKeyReencrypt runs the job and writes one fixed JSON report to
// output, the same shape executePrivacyCommand uses for the other bounded
// server commands. The report carries only the job name, success, and
// runTOTPReencryptJob's result — counts and internal row IDs, never a key,
// secret, nonce, ciphertext, email, or account ID
// (docs/design/totp-key-management.md "Rotation").
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

// runTOTPReencryptJob loads the reduced TOTP re-encryption configuration
// (config.LoadTOTPReencryptJob — DATABASE_URL and the key ring only, never
// PUBLIC_ORIGIN, auth-email, OAuth, or password-rate values), opens a pool,
// and runs one bounded TOTPRotationRunner pass.
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
