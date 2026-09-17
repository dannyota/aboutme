// Command migrate applies embedded Goose migrations or reports pending work
// with -check. Apply serializes through the fixed migration lock and
// always runs as aboutme_migrator (see migrations.Open). See
// docs/design/data.md and docs/design/deployment.md.
//
// Usage:
//
//	migrate          # apply all pending migrations
//	migrate -check   # report pending migrations; exit non-zero if any
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

	"github.com/dannyota/aboutme/apps/server/migrations"
)

// budgets splits this command's total deadline into a lock-wait phase (a
// contended advisory lock, e.g. a concurrent deploy already migrating) and
// a migration phase (actually applying or checking once the lock is
// held), plus slack for everything else (connecting, pinging, status
// bookkeeping).
type budgets struct {
	// lockPeriod and lockRetries configure lock.WithLockTimeout: the
	// session locker polls pg_try_advisory_lock every lockPeriod, up to
	// lockRetries times, before giving up. lockWait is their product.
	lockPeriod  time.Duration
	lockRetries uint64
	// migration is the budget for actually applying or checking pending
	// migrations once the lock is held (or immediately, if it's free).
	migration time.Duration
	// slack is extra headroom for connecting, pinging, and status
	// bookkeeping outside both phases above.
	slack time.Duration
}

func (b budgets) lockWait() time.Duration {
	return b.lockPeriod * time.Duration(b.lockRetries) //nolint:gosec // lockRetries is always a small, hardcoded retry count (defaultBudgets: 60; test overrides: single digits), never near uint64/int64 overflow range
}

// outer is the total context deadline for the whole command: strictly
// greater than lockWait() and strictly greater than migration alone,
// since it's their sum plus positive slack — so a runner that waits the
// full lock-wait budget still has the full migration budget left
// afterward, rather than racing an outer deadline that could already be
// exhausted the moment the lock became available.
func (b budgets) outer() time.Duration {
	return b.lockWait() + b.migration + b.slack
}

func (b budgets) lockOpts() []lock.SessionLockerOption {
	return []lock.SessionLockerOption{lock.WithLockTimeout(uint64(b.lockPeriod/time.Second), b.lockRetries)} //nolint:gosec // lockPeriod is always a small, hardcoded positive duration (defaultBudgets: 5s; test overrides: 1s), so lockPeriod/time.Second is always a small non-negative int64, never near uint64 overflow range
}

// validate rejects budgets that would misbehave: lockPeriod below one
// second (lockOpts truncates it to 0), lockRetries of 0, migration <= 0, or
// slack < 0.
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

// defaultBudgets is production's split: five minutes of lock wait (5s x 60,
// goose's default), five minutes to apply or check, and 30s slack. A runner
// that waits the full lock budget still has the full migration budget.
var defaultBudgets = budgets{
	lockPeriod:  5 * time.Second,
	lockRetries: 60,
	migration:   5 * time.Minute,
	slack:       30 * time.Second,
}

// runBudgets is defaultBudgets by default. Tests override it (it is not a
// constant) to exercise the lock-wait/migration-budget split on a
// timescale far shorter than five minutes, without changing production
// behavior; they always restore it via t.Cleanup.
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
	// Cheapest, dependency-free check first: fail fast and clearly on an
	// invalid budgets configuration before requiring DATABASE_URL or a
	// reachable database at all — see budgets.validate's doc comment for
	// what it catches and why.
	b := runBudgets
	if err := b.validate(); err != nil {
		return fmt.Errorf("invalid deadline budgets: %w", err)
	}

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return fmt.Errorf("load config: DATABASE_URL is required")
	}

	// migrations.Open always runs as aboutme_migrator (see its doc
	// comment): every migration, and goose_db_version itself, is created
	// by -- and so owned by -- that one role, regardless of which login
	// DATABASE_URL names.
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

	// -check never contends for the advisory lock at all (migrations.Status
	// is lock-free — see its doc comment), so the lock-wait budget below is
	// only ever built and passed for the apply path.
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

// runCheck reports each migration's state without taking the advisory lock
// and exits non-zero if any are pending, so a readiness check returns
// promptly during a concurrent migration.
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

// migrationCommandError keeps database, driver, and catalog detail out of
// this command's public output, while still naming the failing
// migration's version and its Postgres SQLSTATE — never a value, a query,
// or the DSN — so an operator can find the exact migration and error
// class from the output alone.
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

// writeLine writes s followed by a newline to w, wrapping any write
// error so callers get a consistent, chain-friendly error to return.
func writeLine(w io.Writer, s string) error {
	if _, err := fmt.Fprintln(w, s); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}
