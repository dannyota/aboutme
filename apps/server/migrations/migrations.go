// Package migrations embeds aboutme's goose SQL migrations and applies
// them through a session-locked goose Provider, so two runners can never
// double-apply — see docs/specs/aboutme-design.md §3 "Schema management"
// and its "Prod migration sequence" row.
//
// Simplified from an earlier hand-rolled migration pattern: that pattern
// hand-rolls advisory locking with a raw pg_advisory_lock/unlock
// pair around goose's legacy global API, and separately verifies
// atlas.sum checksums at runtime before applying. aboutme uses goose's own
// [github.com/pressly/goose/v3.Provider] with
// [github.com/pressly/goose/v3.WithSessionLocker] instead: the Provider
// acquires the session-level Postgres advisory lock, re-checks which
// migrations are actually still pending against the database (not the
// stale pending-list computed before the lock was acquired), applies
// them, and releases the lock — all inside goose's own well-tested code,
// not reimplemented here. Runtime atlas.sum verification is dropped
// entirely per the spec: goose tracks applied versions in the database
// itself, so migration immutability only needs to be enforced once, in
// CI, as an append-only check on this directory — not on every server
// boot.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

// FS embeds every migration in this directory. Only *.sql files are
// embedded (not atlas.sum, which is a dev-time-only artifact consumed by
// `make migrate-gen`/Atlas, never by the running server — see the package
// doc comment).
//
//go:embed *.sql
var FS embed.FS

// LockID is the Postgres advisory-lock key goose uses to serialize
// concurrent migration runners. Fixed so every environment (dev, staging,
// prod, and every CI run) coordinates through the same key; there is
// exactly one migration target (this package), so one key is enough.
// Value: crc32.ChecksumIEEE([]byte("aboutme/migrate")), the same
// derivation goose itself uses for its own DefaultLockID (crc32 of
// "goose") — chosen to make collision with an unrelated advisory lock
// vanishingly unlikely without depending on a magic number nobody can
// trace back to its source.
const LockID int64 = 2561609096

// NewProvider builds a goose Provider over fsys using the Postgres
// dialect and a session-level Postgres advisory lock (LockID). The caller
// retains ownership of db: NewProvider and the functions built on it never
// close it, so a shared pool or a database a test still wants to query
// afterward is always safe to pass in.
//
// lockOpts customizes the session locker's own retry behavior (tests use
// this to poll faster than goose's multi-second production default, so a
// deliberately-contended harness test doesn't have to wait on it);
// production callers (Apply, Check) pass none and get goose's default
// retry policy.
func NewProvider(db *sql.DB, fsys fs.FS, lockOpts ...lock.SessionLockerOption) (*goose.Provider, error) {
	allLockOpts := append([]lock.SessionLockerOption{lock.WithLockID(LockID)}, lockOpts...)
	locker, err := lock.NewPostgresSessionLocker(allLockOpts...)
	if err != nil {
		return nil, fmt.Errorf("migrations: create session locker: %w", err)
	}

	p, err := goose.NewProvider(goose.DialectPostgres, db, fsys, goose.WithSessionLocker(locker))
	if err != nil {
		return nil, fmt.Errorf("migrations: create provider: %w", err)
	}
	return p, nil
}

// newLockFreeProvider builds a goose Provider over fsys identically to
// NewProvider, EXCEPT it never configures a SessionLocker at all. This is
// deliberate and load-bearing, not an oversight, and it is the only
// correct way to make [*goose.Provider.Status] lock-free:
//
//   - goose's exported Provider.Status always requests the configured
//     lock internally when a SessionLocker is configured — its doc
//     comment carries no lock-free guarantee, unlike HasPending and
//     GetVersions ("this method will not use a SessionLocker or Locker if
//     one is configured"). Internally, Status -> status(ctx) ->
//     initialize(ctx, true), and initialize acquires the SessionLocker
//     whenever one is configured, regardless of that boolean's caller
//     (see pressly/goose/v3/provider_run.go). A fast lock-wait retry
//     budget (lock.WithLockTimeout) still takes the *real* advisory lock
//     through that path, just faster and with a shorter failure window —
//     it does not skip it. This was verified directly: with LockID held
//     by another session, Status through a NewProvider-built provider
//     blocked for its full lock-wait budget and then returned "failed to
//     acquire lock".
//   - goose's own lockEnabled config bit is set exclusively by
//     WithSessionLocker/WithLocker (pressly/goose/v3/provider_options.go)
//     and checked as `useLocker && p.cfg.lockEnabled` before ever touching
//     a locker. A provider that never calls WithSessionLocker has
//     lockEnabled permanently false, so Status's internal
//     initialize(ctx, true) is a no-op with respect to locking — this is
//     what actually skips lock acquisition, not any option passed to
//     WithSessionLocker itself.
//   - HasPending/GetVersions are genuinely lock-free but return a
//     bool/version pair, not the per-migration Source+State+AppliedAt
//     list Status (and this package's PendingCount, and cmd/migrate's
//     `-check` output) need — switching to them would mean rebuilding
//     that shape by hand instead of using goose's own Status.
func newLockFreeProvider(db *sql.DB, fsys fs.FS) (*goose.Provider, error) {
	p, err := goose.NewProvider(goose.DialectPostgres, db, fsys)
	if err != nil {
		return nil, fmt.Errorf("migrations: create lock-free provider: %w", err)
	}
	return p, nil
}

// Apply applies every pending embedded migration to db, serialized by the
// advisory lock, and returns the migrations that were actually applied
// (nil, nil when db is already at head). lockOpts customizes the session
// locker's lock-wait retry budget (see NewProvider); production callers
// pass the caller's configured budget (see cmd/migrate/main.go's
// budgets), tests can pass none for goose's own default.
//
// Only ever runs each migration's "-- +goose Up" section: this calls
// [*goose.Provider.Up], never Down/DownTo, and neither this package nor
// cmd/migrate exposes any rollback path at all. A generated migration's
// "-- +goose Down" section (Atlas emits one with real reverse DDL — see
// cmd/migrate/gen's package doc comment) is therefore inert cargo that
// ships in the file but is never executed by this runner: per this
// repo's append-only migration rule, rollback is always a new forward
// corrective migration, not a Down run against a released one.
func Apply(ctx context.Context, db *sql.DB, lockOpts ...lock.SessionLockerOption) ([]*goose.MigrationResult, error) {
	p, err := NewProvider(db, FS, lockOpts...)
	if err != nil {
		return nil, err
	}
	return p.Up(ctx)
}

// Status reports the state (applied or pending) of every embedded
// migration without applying anything and without ever taking the
// advisory lock — via newLockFreeProvider, whose doc comment explains why
// that (not a fast lock-wait budget on Apply's provider) is what's
// actually required. This makes Status always safe to call, including
// while another process holds the lock applying migrations: exactly the
// "pre-deploy readiness gate" use cmd/migrate's `-check` flag documents
// itself as.
func Status(ctx context.Context, db *sql.DB) ([]*goose.MigrationStatus, error) {
	p, err := newLockFreeProvider(db, FS)
	if err != nil {
		return nil, err
	}
	return p.Status(ctx)
}

// PendingCount returns how many of statuses (as returned by Status) are
// still pending. Shared by cmd/migrate's `-check` flag and this package's
// own tests, so both make the same "how many are pending" decision through
// one function instead of two independent reimplementations that could
// silently drift apart.
func PendingCount(statuses []*goose.MigrationStatus) int {
	n := 0
	for _, s := range statuses {
		if s.State == goose.StatePending {
			n++
		}
	}
	return n
}
