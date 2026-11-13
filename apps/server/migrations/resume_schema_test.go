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

	"danny.vn/aboutme/apps/server/internal/testutil"
	"danny.vn/aboutme/apps/server/migrations"
)

type sqlExecer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func newResumeSchemaTx(t *testing.T) (pgx.Tx, context.Context) {
	t.Helper()
	return beginSchemaTx(t, testutil.RequireMigratedTestDatabaseURL(t))
}

func newResumeSchemaTxAt(t *testing.T, version int64) (pgx.Tx, context.Context) {
	t.Helper()
	dsn := newTestDatabase(t)
	db, err := migrations.Open(dsn)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("close database: %v", closeErr)
		}
	})
	provider, err := migrations.NewProvider(db, migrations.FS)
	if err != nil {
		t.Fatalf("NewProvider() error: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), harnessTimeout)
	defer cancel()
	if _, err = provider.UpTo(ctx, version); err != nil {
		t.Fatalf("UpTo(%d) error: %v", version, err)
	}
	return beginSchemaTx(t, dsn)
}

func beginSchemaTx(t *testing.T, dsn string) (pgx.Tx, context.Context) {
	t.Helper()
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

// pgx.Tx.Begin on an existing Tx issues SAVEPOINT, not a second transaction (pgx docs).
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

func strPtr(s string) *string { return &s }

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

func insertResume(ctx context.Context, db sqlExecer, row resumeRow) error {
	_, err := db.Exec(ctx, `
		INSERT INTO resumes (
			user_id, title, slug, live, seo_geo_enabled, schema_version,
			revision, lng, personal_details, content, customization
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb)
	`, row.userID, row.title, row.slug, row.live, row.seoGeoEnabled, row.schemaVersion, row.revision, row.lng)
	return err
}

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
		wantConstraint string
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

func insertTombstone(ctx context.Context, db sqlExecer, slug string) error {
	_, err := db.Exec(ctx, `INSERT INTO slug_tombstones (slug) VALUES ($1)`, slug)
	return err
}

func TestSlugTombstones_ConstraintBoundaries(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	tests := []struct {
		name           string
		slug           string
		wantConstraint string
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
			err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
				return insertTombstone(ctx, sp, tt.slug)
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

	slug := "tombstone-dup-slug"
	if err := insertTombstone(ctx, tx, slug); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	err := insertTombstone(ctx, tx, slug)
	requireConstraintViolation(t, err, "slug_tombstones_slug_key")
}

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

	if _, err := tx.Exec(ctx, `DELETE FROM resumes WHERE id = $1`, firstID); err != nil {
		t.Fatalf("delete freed-slot row: %v", err)
	}
	if _, err := insertResumeReturningID(ctx, tx, defaultResumeRow(userA, "cap-05-after-delete")); err != nil {
		t.Errorf("insert after delete failed: %v, want success", err)
	}

	if _, err := insertResumeReturningID(ctx, tx, defaultResumeRow(userB, "cap-b-01")); err != nil {
		t.Errorf("insert for unrelated userB failed: %v, want success", err)
	}
}

func updateResumeUserID(ctx context.Context, db sqlExecer, resumeID, newUserID uuid.UUID) error {
	_, err := db.Exec(ctx, `UPDATE resumes SET user_id = $1 WHERE id = $2`, newUserID, resumeID)
	return err
}

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

func TestSlugTombstonesCarryNoAccountLinkAndRetentionIndexesExist(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	var columnCount int
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'slug_tombstones' AND column_name = 'released_by_user_id'
	`).Scan(&columnCount); err != nil {
		t.Fatalf("count released_by_user_id column: %v", err)
	}
	if columnCount != 0 {
		t.Errorf("slug_tombstones.released_by_user_id column count = %d, want 0", columnCount)
	}

	for _, indexName := range []string{"slug_tombstones_released_at_idx", "sessions_metadata_expiry_idx"} {
		var indexCount int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE indexname = $1`, indexName).Scan(&indexCount); err != nil {
			t.Fatalf("count index %s: %v", indexName, err)
		}
		if indexCount != 1 {
			t.Errorf("index %s count = %d, want 1", indexName, indexCount)
		}
	}

	var oldIndexCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE indexname = 'sessions_metadata_age_idx'`).Scan(&oldIndexCount); err != nil {
		t.Fatalf("count sessions_metadata_age_idx: %v", err)
	}
	if oldIndexCount != 0 {
		t.Errorf("sessions_metadata_age_idx count = %d, want 0 (replaced by sessions_metadata_expiry_idx)", oldIndexCount)
	}
}
