// Command migrate applies aboutme's embedded Postgres migrations, or (with
// -check) reports pending migrations without applying them.
//
// It is the runtime half of docs/specs/aboutme-design.md §3's "Schema
// management" contract: migration SQL is embedded via go:embed
// (github.com/dannyota/aboutme/apps/server/migrations), so this binary
// needs no external files, no migration tooling on PATH, and no network
// access beyond the database. New migrations are hand-written into
// apps/server/migrations, which is the single source of truth for both
// what goose applies and what sqlc generates internal/store from.
//
// Concurrent safety: Apply acquires a Postgres session-level advisory
// lock before applying anything and releases it after, via goose's own
// Provider (see the migrations package). Two runners started against the
// same database at the same time do not double-apply: the second one
// blocks on the lock, and once it acquires it, re-checks the database and
// finds nothing left to do.
//
// Deadline budgets: runBudgets splits the outer context timeout into an
// explicit lock-wait budget and a separate migration budget, with the
// outer timeout strictly greater than their sum (plus a little slack for
// connecting and status bookkeeping). A single shared timeout for both
// phases would let a runner that legitimately waits the full lock-wait
// budget for a concurrent deploy acquire the lock with no time left to
// actually apply or check anything — see runBudgets' doc comment.
//
// Usage:
//
//	migrate          # apply all pending migrations
//	migrate -check   # report pending migrations; exit non-zero if any
package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3/lock"

	"github.com/dannyota/aboutme/apps/server/internal/config"
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

// validate rejects a budgets value that would silently misbehave instead
// of doing what its fields claim:
//
//   - lockPeriod below one second doesn't just round awkwardly, it
//     truncates to exactly 0 in lockOpts' uint64(lockPeriod/time.Second)
//     (integer division), while lockWait() keeps using the untruncated
//     lockPeriod to compute outer()'s deadline. The result: outer() is
//     sized for a lock-wait budget that lockOpts never actually
//     configures — goose's own lock.WithLockTimeout(0, retries) rejects a
//     zero period outright ("period must be greater than 0"), so this
//     fails closed today, but as a confusing error from deep inside
//     goose instead of a clear one here, and the zero value silently
//     "succeeding" at computing a plausible-looking (but meaningless)
//     outer() duration is exactly the kind of latent trap validation
//     exists to catch before it depends on today's specific downstream
//     behavior remaining that way.
//   - lockRetries of 0 makes lockWait() always 0 regardless of lockPeriod,
//     collapsing the "wait for a contended lock" budget to nothing.
//   - migration <= 0 leaves no time to actually apply or check anything
//     once the lock is free.
//   - slack < 0 would shrink outer() below lockWait()+migration, defeating
//     the whole point of the split (see outer's doc comment).
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

// defaultBudgets is production's split: a five-minute lock-wait budget —
// the same total goose's own library default already uses (5s period x 60
// retries), now explicit here instead of implicit — plus a separate
// five-minute budget for actually applying or checking migrations, plus
// 30s slack. Before this split existed, the command's entire deadline
// *was* the lock-wait budget (goose's default), so a runner that legitimately
// waited the full five minutes for a concurrent deploy's lock could
// acquire it with zero time left to do anything, failing a deploy that
// should have succeeded once its turn came.
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

	if err := run(*check, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run(check bool, stdout io.Writer) error {
	// Cheapest, dependency-free check first: fail fast and clearly on an
	// invalid budgets configuration before requiring DATABASE_URL/ENV or a
	// reachable database at all — see budgets.validate's doc comment for
	// what it catches and why.
	b := runBudgets
	if err := b.validate(); err != nil {
		return fmt.Errorf("invalid deadline budgets: %w", err)
	}

	cfg, err := config.LoadEnv()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			fmt.Fprintln(os.Stderr, "migrate: close database:", closeErr)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), b.outer())
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
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
		return fmt.Errorf("apply migrations: %w", err)
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

// runCheck reports every migration's state without applying anything and
// without ever taking the advisory lock (migrations.Status is
// unconditionally lock-free — see its doc comment for exactly why that
// requires more than a fast lock-wait budget), then fails (non-zero exit)
// if any are pending — so it doubles as a scriptable drift check, e.g. in
// a pre-deploy readiness gate that must return promptly even while a
// concurrent deploy is migrating, not block for the lock-wait budget and
// then report a false failure.
func runCheck(ctx context.Context, db *sql.DB, stdout io.Writer) error {
	statuses, err := migrations.Status(ctx, db)
	if err != nil {
		return fmt.Errorf("check migrations: %w", err)
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

// writeLine writes s followed by a newline to w, wrapping any write
// error so callers get a consistent, chain-friendly error to return.
func writeLine(w io.Writer, s string) error {
	if _, err := fmt.Fprintln(w, s); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}
