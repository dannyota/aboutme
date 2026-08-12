// Phase 2A task 3's constraint-boundary and trigger-behavior tests for the
// resumes, slug_tombstones, and idempotency_records tables added by
// migrations 00004/00005. Every insert here is raw parameterized SQL
// against a live, goose-migrated database -- no internal/store layer in
// the path -- because the point is proving the database itself enforces
// every invariant (D7: even a writer that bypasses the store entirely),
// not that a Go-level validation pass happens to agree with it.
//
// Every test shares TEST_DATABASE_URL (via
// internal/testutil.RequireMigratedTestDatabaseURL) with every other
// DB-backed package's tests, migrated to head first. Each test opens its
// own pgx transaction and rolls it back in t.Cleanup, so repeated runs
// against a persistent test database never accumulate rows or collide on
// a unique constraint (slug, idempotency key, ...). A single test case
// that must observe more than one write outcome without losing the rest
// of its assertions (e.g. "reject this, then prove a good row still
// works") uses a pgx nested transaction (withSavepoint) for the write
// expected to fail: pgx implements Tx.Begin on an existing Tx as a real
// SQL SAVEPOINT, so only that inner write rolls back, leaving the outer
// transaction healthy for whatever comes next in the same test.
package migrations_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// sqlExecer is the minimal pgx surface resumeSchemaTx's raw-SQL helpers
// need: both *pgxpool.Pool, pgx.Tx, and a savepoint-backed nested pgx.Tx
// (see withSavepoint) all satisfy it, so the same insert/query helpers
// work whether called against the outer transaction directly or against a
// savepoint standing in for one write that's expected to fail.
type sqlExecer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// newResumeSchemaTx returns a pgx transaction against TEST_DATABASE_URL
// (migrated to head) and the context to use with it, with the transaction
// rolled back and the pool closed in t.Cleanup. This intentionally opens a
// pgx pool directly (github.com/jackc/pgx/v5/pgxpool), not
// internal/store.NewPool: this file's whole point is exercising the
// database directly, with zero dependency on -- or path through --
// internal/store's generated query layer.
func newResumeSchemaTx(t *testing.T) (pgx.Tx, context.Context) {
	t.Helper()
	dsn := testutil.RequireMigratedTestDatabaseURL(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error: %v", err)
	}
	t.Cleanup(pool.Close)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error: %v", err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("Rollback() error: %v", err)
		}
	})

	return tx, ctx
}

// withSavepoint runs fn against a real Postgres SAVEPOINT nested inside
// tx (see pgx.Tx.Begin's doc comment: calling Begin on an existing Tx
// issues SAVEPOINT/RELEASE SAVEPOINT/ROLLBACK TO SAVEPOINT, not a second
// top-level transaction). fn's error, if any, rolls the savepoint back
// and is returned; success releases the savepoint and commits nothing
// beyond it -- either way, tx itself is left usable for whatever the
// caller does next, which is exactly what a table-driven test over many
// independent constraint-boundary cases needs: one rejected write must
// never poison the whole shared transaction for the next case.
func withSavepoint(ctx context.Context, t *testing.T, tx pgx.Tx, fn func(sp pgx.Tx) error) error {
	t.Helper()

	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("open savepoint: %v", err)
	}

	if fnErr := fn(sp); fnErr != nil {
		if rbErr := sp.Rollback(ctx); rbErr != nil {
			t.Errorf("rollback savepoint after error: %v", rbErr)
		}
		return fnErr
	}

	if commitErr := sp.Commit(ctx); commitErr != nil {
		t.Fatalf("commit savepoint: %v", commitErr)
	}
	return nil
}

// createTestUser inserts a minimal users row via raw SQL (resumes.user_id,
// slug_tombstones.released_by_user_id, and idempotency_records.user_id are
// all FKs into users) and returns its ID.
func createTestUser(ctx context.Context, t *testing.T, db sqlExecer) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := db.QueryRow(ctx,
		`INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id`,
		uuid.NewString()+"@example.com", "Resume Schema Test User",
	).Scan(&id)
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	return id
}

// requireConstraintViolation fails t unless err is (or wraps) a
// *pgconn.PgError whose ConstraintName is exactly want -- the same
// assertion shape internal/user's own store test uses
// (TestStore_Integration_CreateDuplicateEmailRejected), so a boundary case
// here names the exact constraint the DDL declares, not just "some error
// occurred".
func requireConstraintViolation(t *testing.T, err error, want string) {
	t.Helper()

	if err == nil {
		t.Fatalf("error = nil, want a %q violation", want)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("error = %v (%T), want a *pgconn.PgError", err, err)
	}
	if pgErr.ConstraintName != want {
		t.Errorf("violated constraint = %q, want %q", pgErr.ConstraintName, want)
	}
}

// -----------------------------------------------------------------------
// resumes: constraint boundary matrix.
// -----------------------------------------------------------------------

// resumeRow is every column of a resumes insert this file needs to vary.
// personal_details/content/customization are always '{}'::jsonb -- no
// boundary in this task's DDL depends on their content, only their
// NOT NULL-ness, which every row here already satisfies.
type resumeRow struct {
	userID        uuid.UUID
	title         string
	slug          *string
	live          bool
	seoGeoEnabled bool
	schemaVersion int32
	revision      int64
	lng           *string
}

// strPtr is a small helper so table entries can write &s := "..." inline
// without a local variable at each call site.
func strPtr(s string) *string { return &s }

// defaultResumeRow returns an otherwise-valid resumes row for userID, with
// a slug unique to this row (case-qualified, so many defaultResumeRow
// calls sharing one outer transaction never collide on
// resumes_slug_key) -- every boundary case below starts from this and
// overrides exactly the one field it means to test.
func defaultResumeRow(userID uuid.UUID, slugSeed string) resumeRow {
	return resumeRow{
		userID:        userID,
		title:         "Valid Resume Title",
		slug:          strPtr("resume-" + slugSeed),
		live:          false,
		seoGeoEnabled: false,
		schemaVersion: 1,
		revision:      1,
		lng:           nil,
	}
}

// insertResume inserts row into resumes via db (either the outer
// transaction directly, or a withSavepoint-nested one for a write expected
// to fail).
func insertResume(ctx context.Context, db sqlExecer, row resumeRow) error {
	_, err := db.Exec(ctx, `
		INSERT INTO resumes (
			user_id, title, slug, live, seo_geo_enabled, schema_version,
			revision, lng, personal_details, content, customization
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb)
	`, row.userID, row.title, row.slug, row.live, row.seoGeoEnabled, row.schemaVersion, row.revision, row.lng)
	return err
}

// insertResumeReturningID is insertResume, additionally returning the new
// row's id -- needed by the trigger tests, which delete a specific row to
// prove a freed slot lets a new insert through.
func insertResumeReturningID(ctx context.Context, db sqlExecer, row resumeRow) (uuid.UUID, error) {
	var id uuid.UUID
	err := db.QueryRow(ctx, `
		INSERT INTO resumes (
			user_id, title, slug, live, seo_geo_enabled, schema_version,
			revision, lng, personal_details, content, customization
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb)
		RETURNING id
	`, row.userID, row.title, row.slug, row.live, row.seoGeoEnabled, row.schemaVersion, row.revision, row.lng).Scan(&id)
	return id, err
}

func TestResumeSchema_ConstraintBoundaries(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	tests := []struct {
		name           string
		mutate         func(*resumeRow)
		wantConstraint string // "" means the insert must succeed
	}{
		{
			name:           "slug length 3 rejected",
			mutate:         func(r *resumeRow) { r.slug = strPtr(strings.Repeat("a", 3)) },
			wantConstraint: "resumes_slug_format_check",
		},
		{
			name:   "slug length 4 accepted",
			mutate: func(r *resumeRow) { r.slug = strPtr(strings.Repeat("a", 4)) },
		},
		{
			name:   "slug length 30 accepted",
			mutate: func(r *resumeRow) { r.slug = strPtr(strings.Repeat("a", 30)) },
		},
		{
			name:           "slug length 31 rejected",
			mutate:         func(r *resumeRow) { r.slug = strPtr(strings.Repeat("a", 31)) },
			wantConstraint: "resumes_slug_format_check",
		},
		{
			name:           "slug leading hyphen rejected",
			mutate:         func(r *resumeRow) { r.slug = strPtr("-lead") },
			wantConstraint: "resumes_slug_format_check",
		},
		{
			name:           "slug trailing hyphen rejected",
			mutate:         func(r *resumeRow) { r.slug = strPtr("trail-") },
			wantConstraint: "resumes_slug_format_check",
		},
		{
			name:           "slug double hyphen rejected",
			mutate:         func(r *resumeRow) { r.slug = strPtr("dou--ble") },
			wantConstraint: "resumes_slug_format_check",
		},
		{
			name:           "slug uppercase rejected",
			mutate:         func(r *resumeRow) { r.slug = strPtr("UPPERCASE") },
			wantConstraint: "resumes_slug_format_check",
		},
		{
			name: "live true slug nil rejected",
			mutate: func(r *resumeRow) {
				r.live = true
				r.slug = nil
			},
			wantConstraint: "resumes_live_requires_slug_check",
		},
		{
			name: "seo enabled live false rejected",
			mutate: func(r *resumeRow) {
				r.seoGeoEnabled = true
				r.live = false
			},
			wantConstraint: "resumes_seo_requires_live_check",
		},
		{
			name:   "title length 160 accepted",
			mutate: func(r *resumeRow) { r.title = strings.Repeat("x", 160) },
		},
		{
			name:           "title length 161 rejected",
			mutate:         func(r *resumeRow) { r.title = strings.Repeat("x", 161) },
			wantConstraint: "resumes_title_length_check",
		},
		{
			name:   "lng length 35 accepted",
			mutate: func(r *resumeRow) { r.lng = strPtr(strings.Repeat("x", 35)) },
		},
		{
			name:           "lng length 36 rejected",
			mutate:         func(r *resumeRow) { r.lng = strPtr(strings.Repeat("x", 36)) },
			wantConstraint: "resumes_lng_length_check",
		},
		{
			name:           "revision zero rejected",
			mutate:         func(r *resumeRow) { r.revision = 0 },
			wantConstraint: "resumes_revision_check",
		},
		{
			name:   "revision one accepted",
			mutate: func(r *resumeRow) { r.revision = 1 },
		},
		{
			name:           "schema version zero rejected",
			mutate:         func(r *resumeRow) { r.schemaVersion = 0 },
			wantConstraint: "resumes_schema_version_check",
		},
		{
			name:   "schema version one accepted",
			mutate: func(r *resumeRow) { r.schemaVersion = 1 },
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Each case gets its own user: a shared user across every
			// "accepted" case would itself trip resumes_enforce_cap
			// after the 3rd successful insert, which is a false
			// failure of a column-constraint boundary case, not of
			// the cap trigger under test elsewhere.
			userID := createTestUser(ctx, t, tx)

			row := defaultResumeRow(userID, fmt.Sprintf("boundary-%02d", i))
			tt.mutate(&row)

			err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
				return insertResume(ctx, sp, row)
			})

			if tt.wantConstraint == "" {
				if err != nil {
					t.Fatalf("insert failed: %v, want success", err)
				}
				return
			}
			requireConstraintViolation(t, err, tt.wantConstraint)
		})
	}
}

func TestResumeSchema_DuplicateSlugRejected(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userID := createTestUser(ctx, t, tx)

	slug := "duplicate-slug-case"
	first := defaultResumeRow(userID, "dup-first")
	first.slug = strPtr(slug)
	if err := insertResume(ctx, tx, first); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	second := defaultResumeRow(userID, "dup-second")
	second.slug = strPtr(slug)
	err := insertResume(ctx, tx, second)
	requireConstraintViolation(t, err, "resumes_slug_key")
}

// -----------------------------------------------------------------------
// slug_tombstones: format check + duplicate.
// -----------------------------------------------------------------------

func insertTombstone(ctx context.Context, db sqlExecer, slug string, releasedBy *uuid.UUID) error {
	_, err := db.Exec(ctx,
		`INSERT INTO slug_tombstones (slug, released_by_user_id) VALUES ($1, $2)`,
		slug, releasedBy)
	return err
}

// TestSlugTombstones_ConstraintBoundaries mirrors
// TestResumeSchema_ConstraintBoundaries' slug-format coverage in full,
// rather than a single short-slug case. slug_tombstones_slug_format_check
// is a textually independent second copy of resumes_slug_format_check's
// rule (there is no shared constraint, no shared function -- just the same
// regex and bounds typed twice), so a divergent bound or a mistyped regex
// in this copy would otherwise sail through with no test ever noticing.
func TestSlugTombstones_ConstraintBoundaries(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	tests := []struct {
		name           string
		slug           string
		wantConstraint string // "" means the insert must succeed
	}{
		{
			name:           "slug length 3 rejected",
			slug:           strings.Repeat("a", 3),
			wantConstraint: "slug_tombstones_slug_format_check",
		},
		{
			name: "slug length 4 accepted",
			slug: strings.Repeat("a", 4),
		},
		{
			name: "slug length 30 accepted",
			slug: strings.Repeat("a", 30),
		},
		{
			name:           "slug length 31 rejected",
			slug:           strings.Repeat("a", 31),
			wantConstraint: "slug_tombstones_slug_format_check",
		},
		{
			name:           "slug leading hyphen rejected",
			slug:           "-lead",
			wantConstraint: "slug_tombstones_slug_format_check",
		},
		{
			name:           "slug trailing hyphen rejected",
			slug:           "trail-",
			wantConstraint: "slug_tombstones_slug_format_check",
		},
		{
			name:           "slug double hyphen rejected",
			slug:           "dou--ble",
			wantConstraint: "slug_tombstones_slug_format_check",
		},
		{
			name:           "slug uppercase rejected",
			slug:           "UPPERCASE",
			wantConstraint: "slug_tombstones_slug_format_check",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Each case gets its own user (released_by_user_id), same
			// rationale as TestResumeSchema_ConstraintBoundaries: this
			// matrix is unrelated to any per-user invariant, so nothing
			// here should share state across cases.
			userID := createTestUser(ctx, t, tx)

			err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
				return insertTombstone(ctx, sp, tt.slug, &userID)
			})

			if tt.wantConstraint == "" {
				if err != nil {
					t.Fatalf("insert failed: %v, want success", err)
				}
				return
			}
			requireConstraintViolation(t, err, tt.wantConstraint)
		})
	}
}

func TestSlugTombstones_DuplicateSlugRejected(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userID := createTestUser(ctx, t, tx)

	slug := "tombstone-dup-slug"
	if err := insertTombstone(ctx, tx, slug, &userID); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	err := insertTombstone(ctx, tx, slug, &userID)
	requireConstraintViolation(t, err, "slug_tombstones_slug_key")
}

// -----------------------------------------------------------------------
// idempotency_records: (user_id, route, idempotency_key) duplicate.
// -----------------------------------------------------------------------

// idempotencyRecordExpiresAt is a fixed, deterministic expires_at for every
// idempotency_records insert in this file (repo rule: injected clocks, not
// the wall clock, in every test -- see the root CLAUDE.md's determinism
// rule). No assertion here depends on its value, but a fixed literal costs
// nothing and keeps this file consistent with that rule unconditionally
// rather than only where a test happens to care.
var idempotencyRecordExpiresAt = time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

func insertIdempotencyRecord(ctx context.Context, db sqlExecer, userID uuid.UUID, route string, key uuid.UUID) error {
	_, err := db.Exec(ctx, `
		INSERT INTO idempotency_records (
			user_id, route, idempotency_key, request_hash, response_status,
			response_body, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, userID, route, key, []byte("test-request-hash"), 201, []byte(`{}`), idempotencyRecordExpiresAt)
	return err
}

func TestIdempotencyRecords_DuplicateUserRouteKeyRejected(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userID := createTestUser(ctx, t, tx)

	route := "POST /v1/resumes"
	key := uuid.New()
	if err := insertIdempotencyRecord(ctx, tx, userID, route, key); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
		return insertIdempotencyRecord(ctx, sp, userID, route, key)
	})
	requireConstraintViolation(t, err, "idempotency_records_user_route_key_key")

	// Changing any single one of the three key columns must not conflict --
	// the unique constraint is on the (user_id, route, idempotency_key)
	// triple, not any one column alone.
	if err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
		return insertIdempotencyRecord(ctx, sp, userID, "POST /v1/other-route", key)
	}); err != nil {
		t.Errorf("insert with a different route conflicted: %v, want success", err)
	}
	if err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
		return insertIdempotencyRecord(ctx, sp, userID, route, uuid.New())
	}); err != nil {
		t.Errorf("insert with a different idempotency_key conflicted: %v, want success", err)
	}
	otherUser := createTestUser(ctx, t, tx)
	if err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
		return insertIdempotencyRecord(ctx, sp, otherUser, route, key)
	}); err != nil {
		t.Errorf("insert with a different user_id conflicted: %v, want success", err)
	}
}

// -----------------------------------------------------------------------
// resumes_enforce_cap trigger: existence + the 3-resume-per-user cap.
// -----------------------------------------------------------------------

func TestResumeCapTrigger_ExistsOnResumesTable(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	var triggerCount int
	err := tx.QueryRow(ctx, `
		SELECT count(*) FROM pg_trigger
		WHERE tgname = 'resumes_enforce_cap' AND tgrelid = 'resumes'::regclass
	`).Scan(&triggerCount)
	if err != nil {
		t.Fatalf("query pg_trigger: %v", err)
	}
	if triggerCount != 1 {
		t.Errorf("resumes_enforce_cap trigger count on resumes = %d, want 1", triggerCount)
	}

	var funcCount int
	err = tx.QueryRow(ctx, `SELECT count(*) FROM pg_proc WHERE proname = 'enforce_resume_cap'`).Scan(&funcCount)
	if err != nil {
		t.Fatalf("query pg_proc: %v", err)
	}
	if funcCount != 1 {
		t.Errorf("enforce_resume_cap function count = %d, want 1", funcCount)
	}
}

// TestResumeCapTrigger_EnforcesThreePerUser is this task's headline
// behavioral proof: 3 inserts for one user succeed, a 4th raises
// SQLSTATE 23514 with message resumes_user_cap_exceeded (D7's per-owner
// serialization via the trigger's own FOR UPDATE lock on the users row,
// not this test's transaction discipline, is what makes the count read
// consistent), deleting one row frees a slot for a new insert, and a
// second, unrelated user is completely unaffected by the first user's
// cap. Every step is raw SQL against the goose-migrated database -- this
// is what "the trigger survives the migration path" means in practice: the
// trigger is proven against a database built by replaying migrations/, the
// same path production takes.
func TestResumeCapTrigger_EnforcesThreePerUser(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userA := createTestUser(ctx, t, tx)
	userB := createTestUser(ctx, t, tx)

	var firstID uuid.UUID
	for i := range 3 {
		id, err := insertResumeReturningID(ctx, tx, defaultResumeRow(userA, fmt.Sprintf("cap-%02d", i)))
		if err != nil {
			t.Fatalf("insert %d/3 for userA: %v", i+1, err)
		}
		if i == 0 {
			firstID = id
		}
	}

	// The 4th insert for the same user must be rejected by the trigger,
	// not merely produce "some error" -- SQLSTATE 23514 (check_violation,
	// per the DDL's explicit USING ERRCODE) and the exact RAISE EXCEPTION
	// message text.
	err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
		_, insErr := insertResumeReturningID(ctx, sp, defaultResumeRow(userA, "cap-04"))
		return insErr
	})
	if err == nil {
		t.Fatal("4th insert for userA succeeded, want resumes_user_cap_exceeded")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("4th insert error = %v (%T), want a *pgconn.PgError", err, err)
	}
	if pgErr.Code != "23514" {
		t.Errorf("4th insert SQLSTATE = %q, want 23514 (check_violation)", pgErr.Code)
	}
	if pgErr.Message != "resumes_user_cap_exceeded" {
		t.Errorf("4th insert message = %q, want %q", pgErr.Message, "resumes_user_cap_exceeded")
	}

	// Deleting one row frees a slot: a new insert for the same user must
	// now succeed.
	if _, err := tx.Exec(ctx, `DELETE FROM resumes WHERE id = $1`, firstID); err != nil {
		t.Fatalf("delete freed-slot row: %v", err)
	}
	if _, err := insertResumeReturningID(ctx, tx, defaultResumeRow(userA, "cap-05-after-delete")); err != nil {
		t.Errorf("insert after delete failed: %v, want success", err)
	}

	// A second, unrelated user is unaffected by userA's cap, even though
	// userA is currently sitting right back at 3 resumes.
	if _, err := insertResumeReturningID(ctx, tx, defaultResumeRow(userB, "cap-b-01")); err != nil {
		t.Errorf("insert for unrelated userB failed: %v, want success", err)
	}
}

// updateResumeUserID reassigns an existing resume row to a new owner --
// the trigger's second arm (BEFORE UPDATE OF user_id ON resumes), as
// opposed to insertResume/insertResumeReturningID which only ever exercise
// its BEFORE INSERT arm.
func updateResumeUserID(ctx context.Context, db sqlExecer, resumeID, newUserID uuid.UUID) error {
	_, err := db.Exec(ctx, `UPDATE resumes SET user_id = $1 WHERE id = $2`, newUserID, resumeID)
	return err
}

// TestResumeCapTrigger_AllowsNoOpUpdateOfUserIDAtCap proves that the update
// arm counts only resumes newly assigned to an owner. Reassigning one of an
// owner's existing rows to that same owner does not add a resume and must
// remain valid when the owner already has three.
func TestResumeCapTrigger_AllowsNoOpUpdateOfUserIDAtCap(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userID := createTestUser(ctx, t, tx)

	var resumeID uuid.UUID
	for i := range 3 {
		id, err := insertResumeReturningID(ctx, tx, defaultResumeRow(userID, fmt.Sprintf("update-noop-%02d", i)))
		if err != nil {
			t.Fatalf("insert %d/3 for user: %v", i+1, err)
		}
		if i == 0 {
			resumeID = id
		}
	}

	if err := updateResumeUserID(ctx, tx, resumeID, userID); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			t.Fatalf("no-op user_id update at cap: SQLSTATE %s, message %q; want success", pgErr.Code, pgErr.Message)
		}
		t.Fatalf("no-op user_id update at cap: %v; want success", err)
	}

	var ownerID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT user_id FROM resumes WHERE id = $1`, resumeID).Scan(&ownerID); err != nil {
		t.Fatalf("query updated resume owner: %v", err)
	}
	if ownerID != userID {
		t.Errorf("updated resume owner = %s, want %s", ownerID, userID)
	}

	var ownedCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM resumes WHERE user_id = $1`, userID).Scan(&ownedCount); err != nil {
		t.Fatalf("count resumes after no-op user_id update: %v", err)
	}
	if ownedCount != 3 {
		t.Errorf("resume count after no-op user_id update = %d, want 3", ownedCount)
	}
}

// TestResumeCapTrigger_EnforcesCapOnUpdateOfUserID proves the trigger's
// second arm, not just its first: nothing else in this file ever issues an
// UPDATE that touches user_id, so changing the trigger definition from
// "BEFORE INSERT OR UPDATE OF user_id ON resumes" to a bare
// "BEFORE INSERT ON resumes" would leave every other test in this file
// green. Moving an existing resume to a user already holding 3 must be
// rejected exactly like a 4th INSERT would be.
func TestResumeCapTrigger_EnforcesCapOnUpdateOfUserID(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userA := createTestUser(ctx, t, tx)
	userB := createTestUser(ctx, t, tx)

	for i := range 3 {
		if _, err := insertResumeReturningID(ctx, tx, defaultResumeRow(userA, fmt.Sprintf("update-cap-a-%02d", i))); err != nil {
			t.Fatalf("insert %d/3 for userA: %v", i+1, err)
		}
	}
	resumeB, err := insertResumeReturningID(ctx, tx, defaultResumeRow(userB, "update-cap-b-01"))
	if err != nil {
		t.Fatalf("insert for userB: %v", err)
	}

	err = withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
		return updateResumeUserID(ctx, sp, resumeB, userA)
	})
	if err == nil {
		t.Fatal("moving userB's resume onto userA (already at cap) succeeded, want resumes_user_cap_exceeded")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("update error = %v (%T), want a *pgconn.PgError", err, err)
	}
	if pgErr.Code != "23514" {
		t.Errorf("update SQLSTATE = %q, want 23514 (check_violation)", pgErr.Code)
	}
	if pgErr.Message != "resumes_user_cap_exceeded" {
		t.Errorf("update message = %q, want %q", pgErr.Message, "resumes_user_cap_exceeded")
	}
}

// -----------------------------------------------------------------------
// Foreign-key cascade / set-null semantics.
// -----------------------------------------------------------------------

// TestResumes_OwningUserDeletedCascades proves resumes.user_id ON DELETE
// CASCADE: deleting a user must remove every resume that user owns, not
// leave an orphaned row or raise a foreign-key violation.
func TestResumes_OwningUserDeletedCascades(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userID := createTestUser(ctx, t, tx)

	resumeID, err := insertResumeReturningID(ctx, tx, defaultResumeRow(userID, "cascade-case"))
	if err != nil {
		t.Fatalf("insert resume: %v", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete owning user: %v", err)
	}

	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM resumes WHERE id = $1`, resumeID).Scan(&count); err != nil {
		t.Fatalf("count resumes: %v", err)
	}
	if count != 0 {
		t.Errorf("resumes row count after owning user deleted = %d, want 0 (ON DELETE CASCADE)", count)
	}
}

// TestIdempotencyRecords_OwningUserDeletedCascades proves
// idempotency_records.user_id ON DELETE CASCADE, the same way as resumes.
func TestIdempotencyRecords_OwningUserDeletedCascades(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userID := createTestUser(ctx, t, tx)

	if err := insertIdempotencyRecord(ctx, tx, userID, "POST /v1/resumes", uuid.New()); err != nil {
		t.Fatalf("insert idempotency record: %v", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete owning user: %v", err)
	}

	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM idempotency_records WHERE user_id = $1`, userID).Scan(&count); err != nil {
		t.Fatalf("count idempotency_records: %v", err)
	}
	if count != 0 {
		t.Errorf("idempotency_records row count after owning user deleted = %d, want 0 (ON DELETE CASCADE)", count)
	}
}

// TestSlugTombstones_ReleasingUserDeletedSetsNullNotCascade proves the
// deliberate spec asymmetry: a tombstone must outlive the user who
// released it -- deleting that account must never free the tombstoned
// slug early -- so released_by_user_id is ON DELETE SET NULL, unlike every
// other user_id FK in this task's DDL, which is ON DELETE CASCADE. This is
// exactly the kind of thing a later, careless "make the FKs consistent"
// change would silently flip.
func TestSlugTombstones_ReleasingUserDeletedSetsNullNotCascade(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userID := createTestUser(ctx, t, tx)

	slug := "outlives-releaser"
	if err := insertTombstone(ctx, tx, slug, &userID); err != nil {
		t.Fatalf("insert tombstone: %v", err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete releasing user: %v", err)
	}

	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM slug_tombstones WHERE slug = $1`, slug).Scan(&count); err != nil {
		t.Fatalf("count slug_tombstones: %v", err)
	}
	if count != 1 {
		t.Fatalf("slug_tombstones row count after releasing user deleted = %d, want 1 (a tombstone must outlive its releasing user)", count)
	}

	var releasedBy *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT released_by_user_id FROM slug_tombstones WHERE slug = $1`, slug).Scan(&releasedBy); err != nil {
		t.Fatalf("select released_by_user_id: %v", err)
	}
	if releasedBy != nil {
		t.Errorf("released_by_user_id = %v, want nil after releasing user deleted (ON DELETE SET NULL)", *releasedBy)
	}
}
