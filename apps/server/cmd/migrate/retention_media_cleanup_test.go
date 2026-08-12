// Prior-head proof for migration 00006 (bounded idempotency retention and
// the media deletion-job ledger). Every test here builds a fresh throwaway
// database, applies the released migrations 00001–00005 (the prior head),
// seeds representative legacy idempotency records, then applies head and
// proves the migration's own transaction did what ADR 0016 and P2B D13
// require:
//
//   - already-expired legacy records are purged first;
//   - retained legacy records gain response_headers = '{}';
//   - one idempotency_usage row per user with retained records is
//     backfilled using the single canonical byte expression
//     octet_length(response_body::text) + octet_length(response_headers::text),
//     so a JSON null body counts as 4 bytes and empty headers count as 2;
//   - the non-negative checks hold, and a cleanup that releases the exact
//     backfilled counters lands at zero rather than going negative;
//   - the (user_id, expires_at, id) cleanup index and the media deletion-job
//     ledger (unique canonical key tied to resume_id, due-order index, no
//     FK to resumes) exist with their declared constraints.
//
// Uses this package's own live-DB helpers (testdb_test.go), the same
// convention as budgets_test.go.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

// priorHeadVersion is the last released migration before 00006. The seed
// below must run against exactly this schema so the test proves 00006
// upgrades a real prior-release database, not a fresh head.
const priorHeadVersion = 5

// retentionByteExpr is the one canonical stored-byte expression ADR 0016
// fixes for backfill, insert returns, cleanup returns, and quota
// accounting. The test recomputes expectations with this exact SQL rather
// than a Go reimplementation, so the proof cannot drift from the database's
// own arithmetic.
const retentionByteExpr = "octet_length(response_body::text) + octet_length(response_headers::text)"

type retentionSeed struct {
	db *sql.DB

	userA, userB string // uuids
}

// seedPriorHead applies migrations up to priorHeadVersion on a fresh
// database and seeds two users:
//
//   - userA: three live records with object, array, and JSON null bodies,
//     plus two already-expired records;
//   - userB: only already-expired records.
func seedPriorHead(ctx context.Context, t *testing.T, dsn string) retentionSeed {
	t.Helper()

	db := openMigrateTestDB(t, dsn)
	p, err := migrations.NewProvider(db, migrations.FS)
	if err != nil {
		t.Fatalf("NewProvider() error: %v", err)
	}
	if _, err := p.UpTo(ctx, priorHeadVersion); err != nil {
		t.Fatalf("UpTo(%d) error: %v", priorHeadVersion, err)
	}

	var userA, userB string
	if err := db.QueryRowContext(ctx,
		`INSERT INTO users (email, name) VALUES ('retention-a@example.com', 'A') RETURNING id::text`,
	).Scan(&userA); err != nil {
		t.Fatalf("seed user A: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`INSERT INTO users (email, name) VALUES ('retention-b@example.com', 'B') RETURNING id::text`,
	).Scan(&userB); err != nil {
		t.Fatalf("seed user B: %v", err)
	}

	seedRecord := func(user, route, body string, expiresIn time.Duration) {
		t.Helper()
		if _, err := db.ExecContext(ctx,
			`INSERT INTO idempotency_records
			     (user_id, route, idempotency_key, request_hash, response_status, response_body, expires_at)
			 VALUES ($1, $2, uuidv7(), '\x00'::bytea, 200, $3::jsonb, now() + $4::interval)`,
			user, route, body, fmt.Sprintf("%f seconds", expiresIn.Seconds()),
		); err != nil {
			t.Fatalf("seed record (user=%s route=%s body=%s): %v", user, route, body, err)
		}
	}

	// userA: live object/array/null bodies — the three jsonb shapes whose
	// byte accounting the backfill must handle (null is 4 bytes).
	seedRecord(userA, "op-object", `{"n": 1, "nested": {"a": [1, 2]}}`, time.Hour)
	seedRecord(userA, "op-array", `[1, 2, 3]`, time.Hour)
	seedRecord(userA, "op-null", `null`, time.Hour)
	// userA: expired backlog that must be purged before backfill.
	seedRecord(userA, "op-expired-1", `{"stale": true}`, -time.Hour)
	seedRecord(userA, "op-expired-2", `"stale"`, -time.Minute)
	// userB: only expired records — after purge this user retains nothing
	// and must get NO usage row.
	seedRecord(userB, "op-expired-3", `{"gone": 1}`, -time.Hour)

	return retentionSeed{db: db, userA: userA, userB: userB}
}

func migrateToHead(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := migrations.Apply(ctx, db); err != nil {
		t.Fatalf("Apply() to head from prior head error: %v", err)
	}
}

func queryInt64(ctx context.Context, t *testing.T, db *sql.DB, query string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return n
}

// isSQLState reports whether err is a PostgreSQL error with the given
// SQLSTATE, from either the pgx or database/sql driver path.
func isSQLState(err error, code string) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == code
	}
	return false
}

func TestMigration00006_PriorHead_PurgeBackfillAndCounts(t *testing.T) {
	base := requireMigrateTestDatabaseURL(t)
	dsn := newMigrateTestDatabase(t, base)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	seed := seedPriorHead(ctx, t, dsn)
	migrateToHead(ctx, t, seed.db)

	// Purge: userA keeps exactly its three live records, userB keeps none.
	if n := queryInt64(ctx, t, seed.db,
		`SELECT count(*) FROM idempotency_records WHERE user_id = $1`, seed.userA); n != 3 {
		t.Errorf("userA retained records = %d, want 3 (expired legacy rows must be purged first)", n)
	}
	if n := queryInt64(ctx, t, seed.db,
		`SELECT count(*) FROM idempotency_records WHERE user_id = $1`, seed.userB); n != 0 {
		t.Errorf("userB retained records = %d, want 0", n)
	}

	// Every retained legacy record got the empty-object headers default.
	if n := queryInt64(ctx, t, seed.db,
		`SELECT count(*) FROM idempotency_records
		 WHERE response_headers::text <> '{}' OR jsonb_typeof(response_headers) <> 'object'`); n != 0 {
		t.Errorf("%d retained records lack response_headers = '{}'", n)
	}

	// Backfill: userA's usage row matches the canonical byte expression
	// recomputed over its retained rows; userB has no usage row at all.
	var retained, storedBytes int64
	if err := seed.db.QueryRowContext(ctx,
		`SELECT retained_records, stored_bytes FROM idempotency_usage WHERE user_id = $1`,
		seed.userA).Scan(&retained, &storedBytes); err != nil {
		t.Fatalf("read userA usage row: %v", err)
	}
	if retained != 3 {
		t.Errorf("userA usage retained_records = %d, want 3", retained)
	}
	wantBytes := queryInt64(ctx, t, seed.db,
		`SELECT COALESCE(sum(`+retentionByteExpr+`), 0) FROM idempotency_records WHERE user_id = $1`,
		seed.userA)
	if storedBytes != wantBytes {
		t.Errorf("userA usage stored_bytes = %d, want %d (the canonical byte expression over retained rows)",
			storedBytes, wantBytes)
	}
	// The JSON null body must have contributed exactly 4 body bytes + 2
	// header bytes: prove the per-row expression directly.
	if n := queryInt64(ctx, t, seed.db,
		`SELECT `+retentionByteExpr+` FROM idempotency_records WHERE user_id = $1 AND route = 'op-null'`,
		seed.userA); n != 6 {
		t.Errorf("null-body record stored bytes = %d, want 6 (4-byte JSON null + 2-byte '{}')", n)
	}
	if n := queryInt64(ctx, t, seed.db,
		`SELECT count(*) FROM idempotency_usage WHERE user_id = $1`, seed.userB); n != 0 {
		t.Errorf("userB usage rows = %d, want 0 (no retained records, no backfilled row)", n)
	}

	// The deterministic-cleanup composite index exists with the exact
	// column order the bounded delete's ORDER BY needs.
	var indexdef string
	if err := seed.db.QueryRowContext(ctx,
		`SELECT indexdef FROM pg_indexes
		 WHERE tablename = 'idempotency_records' AND indexname = 'idempotency_records_user_expires_id_idx'`,
	).Scan(&indexdef); err != nil {
		t.Fatalf("read cleanup index definition: %v", err)
	}
	if !strings.Contains(indexdef, "(user_id, expires_at, id)") {
		t.Errorf("cleanup index definition = %q, want columns (user_id, expires_at, id)", indexdef)
	}
}

func TestMigration00006_PriorHead_ChecksHoldAndCleanupCannotGoNegative(t *testing.T) {
	base := requireMigrateTestDatabaseURL(t)
	dsn := newMigrateTestDatabase(t, base)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	seed := seedPriorHead(ctx, t, dsn)
	migrateToHead(ctx, t, seed.db)

	// Non-negative checks reject direct underflow on both counters.
	if _, err := seed.db.ExecContext(ctx,
		`UPDATE idempotency_usage SET retained_records = -1 WHERE user_id = $1`, seed.userA); !isSQLState(err, "23514") {
		t.Errorf("negative retained_records error = %v, want SQLSTATE 23514", err)
	}
	if _, err := seed.db.ExecContext(ctx,
		`UPDATE idempotency_usage SET stored_bytes = -1 WHERE user_id = $1`, seed.userA); !isSQLState(err, "23514") {
		t.Errorf("negative stored_bytes error = %v, want SQLSTATE 23514", err)
	}

	// response_headers must be a JSON object.
	for _, headers := range []string{`[]`, `"x"`, `null`, `1`} {
		if _, err := seed.db.ExecContext(ctx,
			`INSERT INTO idempotency_records
			     (user_id, route, idempotency_key, request_hash, response_status, response_body, response_headers, expires_at)
			 VALUES ($1, 'op-bad-headers', uuidv7(), '\x00'::bytea, 200, '{}'::jsonb, $2::jsonb, now() + interval '1 hour')`,
			seed.userA, headers); !isSQLState(err, "23514") {
			t.Errorf("response_headers = %s insert error = %v, want SQLSTATE 23514 (object-type check)", headers, err)
		}
	}

	// The purge-then-backfill order guarantees the counters cover every
	// physically retained row, so deleting ALL of a user's rows and
	// releasing their exact counted bytes lands at exactly zero — it can
	// never need to go negative.
	tx, err := seed.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin cleanup tx: %v", err)
	}
	defer tx.Rollback() //nolint:errcheck // best-effort rollback if any step below fails first
	var deletedRecords, deletedBytes int64
	if err := tx.QueryRowContext(ctx,
		`WITH deleted AS (
		     DELETE FROM idempotency_records WHERE user_id = $1
		     RETURNING response_body, response_headers
		 )
		 SELECT count(*)::bigint, COALESCE(sum(`+retentionByteExpr+`), 0)::bigint FROM deleted`,
		seed.userA).Scan(&deletedRecords, &deletedBytes); err != nil {
		t.Fatalf("delete all retained rows: %v", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE idempotency_usage
		 SET retained_records = retained_records - $2, stored_bytes = stored_bytes - $3
		 WHERE user_id = $1`,
		seed.userA, deletedRecords, deletedBytes); err != nil {
		t.Fatalf("release exact counters after full cleanup: %v (the non-negative check must not fire)", err)
	}
	var retained, storedBytes int64
	if err := tx.QueryRowContext(ctx,
		`SELECT retained_records, stored_bytes FROM idempotency_usage WHERE user_id = $1`,
		seed.userA).Scan(&retained, &storedBytes); err != nil {
		t.Fatalf("read usage after full cleanup: %v", err)
	}
	if retained != 0 || storedBytes != 0 {
		t.Errorf("usage after deleting every retained row = (%d, %d), want exactly (0, 0)", retained, storedBytes)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit cleanup tx: %v", err)
	}
}

func TestMigration00006_PriorHead_MediaDeletionJobLedger(t *testing.T) {
	base := requireMigrateTestDatabaseURL(t)
	dsn := newMigrateTestDatabase(t, base)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	seed := seedPriorHead(ctx, t, dsn)
	migrateToHead(ctx, t, seed.db)

	// A real owner and resume, so the no-cascade proof below deletes rows
	// that actually exist.
	var resumeID string
	if err := seed.db.QueryRowContext(ctx,
		`INSERT INTO resumes (user_id, title, schema_version, personal_details, content, customization)
		 VALUES ($1, 'media ledger', 1, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb)
		 RETURNING id::text`, seed.userA).Scan(&resumeID); err != nil {
		t.Fatalf("seed resume: %v", err)
	}

	goodKey := "resumes/" + resumeID + "/photo-0123456789abcdef0123456789abcdef.jpg"
	if _, err := seed.db.ExecContext(ctx,
		`INSERT INTO media_deletion_jobs (resume_id, object_key) VALUES ($1, $2)`,
		resumeID, goodKey); err != nil {
		t.Fatalf("insert valid deletion job: %v", err)
	}

	// Defaults: enqueued now, due now, zero attempts.
	var attempts int64
	var enqueuedOK, dueOK bool
	if err := seed.db.QueryRowContext(ctx,
		`SELECT attempt_count, enqueued_at <= now(), next_attempt_at <= now()
		 FROM media_deletion_jobs WHERE object_key = $1`, goodKey,
	).Scan(&attempts, &enqueuedOK, &dueOK); err != nil {
		t.Fatalf("read inserted job: %v", err)
	}
	if attempts != 0 || !enqueuedOK || !dueOK {
		t.Errorf("job defaults = (attempts=%d, enqueued<=now=%t, due<=now=%t), want (0, true, true)",
			attempts, enqueuedOK, dueOK)
	}

	// The canonical key is unique: re-enqueueing it conflicts rather than
	// duplicating cleanup work.
	if _, err := seed.db.ExecContext(ctx,
		`INSERT INTO media_deletion_jobs (resume_id, object_key) VALUES ($1, $2)`,
		resumeID, goodKey); !isSQLState(err, "23505") {
		t.Errorf("duplicate key insert error = %v, want SQLSTATE 23505", err)
	}

	// The key's embedded canonical resume ID must equal resume_id, and the
	// grammar is D11's exactly: lowercase UUID, 32 lowercase hex, jpg|png.
	var otherResumeID string
	if err := seed.db.QueryRowContext(ctx,
		`INSERT INTO resumes (user_id, title, schema_version, personal_details, content, customization)
		 VALUES ($1, 'other', 1, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb)
		 RETURNING id::text`, seed.userA).Scan(&otherResumeID); err != nil {
		t.Fatalf("seed second resume: %v", err)
	}
	badKeys := []struct {
		name, resumeID, key string
	}{
		{"cross-resume key", otherResumeID, goodKey},
		{"malformed prefix", resumeID, "assets/" + resumeID + "/photo-0123456789abcdef0123456789abcdef.jpg"},
		{"uppercase hex", resumeID, "resumes/" + resumeID + "/photo-0123456789ABCDEF0123456789ABCDEF.jpg"},
		{"short hex", resumeID, "resumes/" + resumeID + "/photo-0123456789abcdef.jpg"},
		{"webp extension", resumeID, "resumes/" + resumeID + "/photo-0123456789abcdef0123456789abcdef.webp"},
		{"traversal", resumeID, "resumes/" + resumeID + "/../photo-0123456789abcdef0123456789abcdef.jpg"},
	}
	for _, bad := range badKeys {
		if _, err := seed.db.ExecContext(ctx,
			`INSERT INTO media_deletion_jobs (resume_id, object_key) VALUES ($1, $2)`,
			bad.resumeID, bad.key); !isSQLState(err, "23514") {
			t.Errorf("%s insert error = %v, want SQLSTATE 23514 (key/resume check)", bad.name, err)
		}
	}

	// Attempt counts cannot go negative.
	if _, err := seed.db.ExecContext(ctx,
		`UPDATE media_deletion_jobs SET attempt_count = -1 WHERE object_key = $1`, goodKey); !isSQLState(err, "23514") {
		t.Errorf("negative attempt_count error = %v, want SQLSTATE 23514", err)
	}

	// The due-order index for bounded oldest-due claims exists.
	var indexdef string
	if err := seed.db.QueryRowContext(ctx,
		`SELECT indexdef FROM pg_indexes
		 WHERE tablename = 'media_deletion_jobs' AND indexname = 'media_deletion_jobs_next_attempt_idx'`,
	).Scan(&indexdef); err != nil {
		t.Fatalf("read due-order index definition: %v", err)
	}
	if !strings.Contains(indexdef, "(next_attempt_at, id)") {
		t.Errorf("due-order index definition = %q, want columns (next_attempt_at, id)", indexdef)
	}

	// No foreign key to resumes: deleting the resume's owner (which
	// cascades the resumes rows away) must NOT cascade away the pending
	// physical-deletion job.
	if _, err := seed.db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, seed.userA); err != nil {
		t.Fatalf("delete owning user: %v", err)
	}
	if n := queryInt64(ctx, t, seed.db,
		`SELECT count(*) FROM resumes WHERE id = $1`, resumeID); n != 0 {
		t.Fatalf("resume rows after owner delete = %d, want 0 (cascade sanity)", n)
	}
	if n := queryInt64(ctx, t, seed.db,
		`SELECT count(*) FROM media_deletion_jobs WHERE object_key = $1`, goodKey); n != 1 {
		t.Errorf("deletion job rows after resume/account deletion = %d, want 1 (the ledger must survive)", n)
	}
}
