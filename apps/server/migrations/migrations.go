// Package migrations embeds and applies aboutme's append-only goose SQL
// migrations.
//
// The schema-source and immutability rules are in
// docs/design/data.md#schema-and-migrations. Production sequencing and lock
// rationale are in docs/design/deployment.md#database-and-releases.
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

// FS embeds every migration in this directory. The pattern is deliberately
// *.sql and nothing looser: this directory also holds this package's own
// Go sources and test files, and a broader pattern would embed those into
// the shipped server binary. TestFS_EmbedsOnlySQLFiles guards that.
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

// Apply applies every pending embedded migration through the fixed migration
// identity state machine and returns the migrations that were actually applied
// (nil, nil when db is already at head). lockOpts customizes the session
// locker's lock-wait retry budget (see NewProvider); production callers
// pass the caller's configured budget (see cmd/migrate/main.go's
// budgets), tests can pass none for goose's own default.
//
// Only ever runs each migration's "-- +goose Up" section. It never runs
// Down/DownTo, and neither this package nor
// cmd/migrate exposes any rollback path at all. A migration's
// "-- +goose Down" section is therefore inert cargo that ships in the file
// but is never executed by this runner: per this repo's append-only
// migration rule, rollback is always a new forward corrective migration,
// not a Down run against a released one.
func Apply(ctx context.Context, db *sql.DB, identity MigrationIdentity, lockOpts ...lock.SessionLockerOption) ([]*goose.MigrationResult, error) {
	return applyFS(ctx, db, FS, identity, lockOpts...)
}

// Status reports the state (applied or pending) of every embedded
// migration without applying anything and without ever taking the
// advisory lock. It reads catalog and history state in one pinned, repeatable
// read-only transaction. This makes Status safe to call, including
// while another process holds the lock applying migrations: exactly the
// "pre-deploy readiness gate" use cmd/migrate's `-check` flag documents
// itself as.
func Status(ctx context.Context, db *sql.DB, identity MigrationIdentity) ([]*goose.MigrationStatus, error) {
	return statusFS(ctx, db, FS, identity)
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
