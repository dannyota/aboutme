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
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/publicroots"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const (
	checkPublicRootCommandName  = "check-public-root"
	rootOccupancyTimeout        = 30 * time.Second
	rootOccupancyCleanupTimeout = 5 * time.Second

	rootOccupancyClearExitCode     = 0
	rootOccupancyResumeExitCode    = 10
	rootOccupancyTombstoneExitCode = 11
	rootOccupancyBothExitCode      = 12
)

var (
	errCheckPublicRootUsage = errors.New("usage: server check-public-root <root>")
	rootOccupancyTxOptions  = pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
)

type rootOccupancy struct {
	ResumeOccupied    bool `json:"resumeOccupied"`
	TombstoneOccupied bool `json:"tombstoneOccupied"`
}

type rootOccupancyExitError struct{ code int }

func (err rootOccupancyExitError) Error() string { return "check public root occupied" }

func parseCheckPublicRootCommand(args []string) (string, error) {
	if len(args) != 1 || !validRootGrammar(args[0]) || !publicroots.Reserved(args[0]) {
		return "", errCheckPublicRootUsage
	}
	return args[0], nil
}

func validRootGrammar(root string) bool {
	if len(root) < 4 || len(root) > 30 || root[0] == '-' || root[len(root)-1] == '-' {
		return false
	}
	previousHyphen := false
	for i := range root {
		letter := root[i] >= 'a' && root[i] <= 'z'
		digit := root[i] >= '0' && root[i] <= '9'
		if root[i] == '-' {
			if previousHyphen {
				return false
			}
			previousHyphen = true
			continue
		}
		if !letter && !digit {
			return false
		}
		previousHyphen = false
	}
	return true
}

func runCheckPublicRoot(args []string) error {
	root, err := parseCheckPublicRootCommand(args)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, rootOccupancyTimeout)
	defer cancel()
	return executeCheckPublicRoot(ctx, root, os.Getenv, os.Stdout)
}

func executeCheckPublicRoot(ctx context.Context, root string, getenv func(string) string, output io.Writer) error {
	if err := ctx.Err(); err != nil {
		return errors.New("check public root canceled")
	}
	cfg, err := config.LoadPrivacyJob(getenv, false)
	if err != nil {
		return errors.New("check public root configuration is invalid")
	}
	pool, err := store.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return errors.New("check public root database configuration is invalid")
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rootOccupancyCleanupTimeout)
		defer cancel()
		pool.Close(cleanupCtx)
	}()

	occupancy, err := checkPublicRootOccupancy(ctx, pool.Pool, root)
	if err != nil {
		return errors.New("check public root database is unavailable")
	}
	if err := json.NewEncoder(output).Encode(occupancy); err != nil {
		return errors.New("check public root result could not be written")
	}
	if code := rootOccupancyExitCode(occupancy); code != rootOccupancyClearExitCode {
		return rootOccupancyExitError{code: code}
	}
	return nil
}

func checkPublicRootOccupancy(ctx context.Context, pool *pgxpool.Pool, root string) (occupancy rootOccupancy, resultErr error) {
	tx, err := pool.BeginTx(ctx, rootOccupancyTxOptions)
	if err != nil {
		return rootOccupancy{}, err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rootOccupancyCleanupTimeout)
		defer cancel()
		if rollbackErr := tx.Rollback(cleanupCtx); resultErr == nil && rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			resultErr = rollbackErr
		}
	}()
	occupancy, resultErr = readRootOccupancy(ctx, tx, root)
	if resultErr != nil {
		return rootOccupancy{}, resultErr
	}
	if err := tx.Commit(ctx); err != nil {
		return rootOccupancy{}, err
	}
	return occupancy, nil
}

func readRootOccupancy(ctx context.Context, tx pgx.Tx, root string) (rootOccupancy, error) {
	var occupancy rootOccupancy
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM resumes WHERE slug = $1)`, root).Scan(&occupancy.ResumeOccupied); err != nil {
		return rootOccupancy{}, err
	}
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM slug_tombstones WHERE slug = $1)`, root).Scan(&occupancy.TombstoneOccupied); err != nil {
		return rootOccupancy{}, err
	}
	return occupancy, nil
}

func rootOccupancyExitCode(occupancy rootOccupancy) int {
	switch {
	case occupancy.ResumeOccupied && occupancy.TombstoneOccupied:
		return rootOccupancyBothExitCode
	case occupancy.ResumeOccupied:
		return rootOccupancyResumeExitCode
	case occupancy.TombstoneOccupied:
		return rootOccupancyTombstoneExitCode
	default:
		return rootOccupancyClearExitCode
	}
}
