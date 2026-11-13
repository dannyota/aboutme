package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"danny.vn/aboutme/apps/server/migrations"
)

type budgets struct {
	lockPeriod  time.Duration
	lockRetries uint64
	migration   time.Duration
	slack       time.Duration
}

func (b budgets) lockWait() time.Duration {
	return b.lockPeriod * time.Duration(b.lockRetries) //nolint:gosec // lockRetries is always a small, hardcoded retry count (defaultBudgets: 60; test overrides: single digits), never near uint64/int64 overflow range
}

func (b budgets) outer() time.Duration {
	return b.lockWait() + b.migration + b.slack
}

func (b budgets) lockOpts() []lock.SessionLockerOption {
	return []lock.SessionLockerOption{lock.WithLockTimeout(uint64(b.lockPeriod/time.Second), b.lockRetries)} //nolint:gosec // lockPeriod is always a small, hardcoded positive duration (defaultBudgets: 5s; test overrides: 1s), so lockPeriod/time.Second is always a small non-negative int64, never near uint64 overflow range
}

func (b budgets) validate() error {
	if b.lockPeriod < time.Second {
		return fmt.Errorf("lockPeriod must be at least 1s, got %s (shorter values truncate to 0 in lockOpts)", b.lockPeriod)
	}
	if b.lockRetries == 0 {
		return fmt.Errorf("lockRetries must be at least 1, got %d", b.lockRetries)
	}
	if b.migration <= 0 {
		return fmt.Errorf("migration budget must be positive, got %s", b.migration)
	}
	if b.slack < 0 {
		return fmt.Errorf("slack must not be negative, got %s", b.slack)
	}
	return nil
}

// goose's default Postgres session lock retries every 5s up to 60 times (lock.NewPostgresSessionLocker).
var defaultBudgets = budgets{
	lockPeriod:  5 * time.Second,
	lockRetries: 60,
	migration:   5 * time.Minute,
	slack:       30 * time.Second,
}

var runBudgets = defaultBudgets

func main() {
	check := flag.Bool("check", false, "report pending migrations without applying them")
	flag.Parse()
	if flag.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "migrate: arguments are not accepted")
		os.Exit(1)
	}

	if err := run(*check, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run(check bool, stdout io.Writer) error {
	b := runBudgets
	if err := b.validate(); err != nil {
		return fmt.Errorf("invalid deadline budgets: %w", err)
	}

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return fmt.Errorf("load config: DATABASE_URL is required")
	}

	db, err := migrations.Open(databaseURL)
	if err != nil {
		return fmt.Errorf("open database: invalid configuration")
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			fmt.Fprintln(os.Stderr, "migrate: close database failed")
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), b.outer())
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: connection failed")
	}

	if check {
		return runCheck(ctx, db, stdout)
	}
	return runApply(ctx, db, stdout, b.lockOpts()...)
}

func runApply(ctx context.Context, db *sql.DB, stdout io.Writer, lockOpts ...lock.SessionLockerOption) error {
	results, err := migrations.Apply(ctx, db, lockOpts...)
	if err != nil {
		return migrationCommandError(err)
	}

	if len(results) == 0 {
		return writeLine(stdout, "migrate: already at head, nothing to apply")
	}
	for _, r := range results {
		if _, err := fmt.Fprintf(stdout, "migrate: applied %s (%s)\n", r.Source.Path, r.Duration); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
	}
	return nil
}

func runCheck(ctx context.Context, db *sql.DB, stdout io.Writer) error {
	statuses, err := migrations.Status(ctx, db)
	if err != nil {
		return migrationCommandError(err)
	}

	for _, s := range statuses {
		if _, err := fmt.Fprintf(stdout, "%-8s %s\n", s.State, s.Source.Path); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
	}

	if pending := migrations.PendingCount(statuses); pending > 0 {
		return fmt.Errorf("%d migration(s) pending", pending)
	}
	return writeLine(stdout, "migrate: up to date")
}

func migrationCommandError(err error) error {
	var partial *goose.PartialError
	if errors.As(err, &partial) {
		version := int64(0)
		if partial.Failed != nil && partial.Failed.Source != nil {
			version = partial.Failed.Source.Version
		}
		sqlstate := "unknown"
		var pgErr *pgconn.PgError
		if errors.As(partial.Err, &pgErr) {
			sqlstate = pgErr.Code
		}
		return fmt.Errorf("migration %d failed: SQLSTATE %s", version, sqlstate)
	}
	return errors.New("migration operation failed")
}

func writeLine(w io.Writer, s string) error {
	if _, err := fmt.Fprintln(w, s); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}
