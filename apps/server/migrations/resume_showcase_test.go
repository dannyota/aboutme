// Community showcase table (docs/design/showcase.md "Data and contract", ADR
// 0028; AC-SHOW-001, AC-SHOW-009, AC-SHOW-015): its column checks, cascades,
// and the slug refusal.
package migrations_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/migrations"
)

const (
	showcaseHexA = "0123456789abcdef"
	showcaseHexB = "fedcba9876543210"
)

// showcaseRow is every column of a resume_showcase insert the tests vary.
type showcaseRow struct {
	role        *string
	cardVersion string
	templateID  *string
}

func validShowcaseRow() showcaseRow {
	return showcaseRow{cardVersion: showcaseHexB}
}

func insertShowcase(ctx context.Context, db sqlExecer, resumeID uuid.UUID, row showcaseRow) error {
	_, err := db.Exec(ctx, `
		INSERT INTO resume_showcase (resume_id, requested_at, role, card_version, template_id)
		VALUES ($1, now(), $2, $3, $4)`,
		resumeID, row.role, row.cardVersion, row.templateID)
	return err
}

func showcaseCount(ctx context.Context, t *testing.T, tx pgx.Tx, resumeID uuid.UUID) int {
	t.Helper()
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM resume_showcase WHERE resume_id = $1`, resumeID).Scan(&n); err != nil {
		t.Fatalf("count showcase rows: %v", err)
	}
	return n
}

// AC-SHOW-015: every column rule of the design is a CHECK constraint.
func TestResumeShowcaseConstraints(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	resumeID := insertLiveCardResume(ctx, t, tx, createTestUser(ctx, t, tx), uniqueCardSlug())

	tests := []struct {
		name           string
		mutate         func(*showcaseRow)
		wantConstraint string
	}{
		{"role outside the ten", func(r *showcaseRow) { r.role = strPtr("designer") }, "resume_showcase_role_check"},
		{"empty role", func(r *showcaseRow) { r.role = strPtr("") }, "resume_showcase_role_check"},
		{"non hex card version", func(r *showcaseRow) { r.cardVersion = "0123456789abcdeg" }, "resume_showcase_card_version_check"},
		{"short card version", func(r *showcaseRow) { r.cardVersion = "0123" }, "resume_showcase_card_version_check"},
		{"template with uppercase", func(r *showcaseRow) { r.templateID = strPtr("Ats-Plain") }, "resume_showcase_template_id_check"},
		{"template with double hyphen", func(r *showcaseRow) { r.templateID = strPtr("ats--plain") }, "resume_showcase_template_id_check"},
		{"template with trailing hyphen", func(r *showcaseRow) { r.templateID = strPtr("ats-") }, "resume_showcase_template_id_check"},
		{"empty template", func(r *showcaseRow) { r.templateID = strPtr("") }, "resume_showcase_template_id_check"},
		{"template over 64 characters", func(r *showcaseRow) { r.templateID = strPtr(strings.Repeat("a", 65)) }, "resume_showcase_template_id_check"},
	}
	for _, test := range tests {
		row := validShowcaseRow()
		test.mutate(&row)
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error { return insertShowcase(ctx, sp, resumeID, row) })
		if err == nil {
			t.Errorf("%s: insert succeeded, want %s", test.name, test.wantConstraint)
			continue
		}
		requireConstraintViolation(t, err, test.wantConstraint)
	}

	err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error { return insertShowcase(ctx, sp, uuid.New(), validShowcaseRow()) })
	requireConstraintViolation(t, err, "resume_showcase_resume_id_fkey")

	for _, role := range []string{"backend", "frontend", "mobile", "devops", "data-ai", "qa", "fresher", "brse", "security", "other"} {
		row := validShowcaseRow()
		row.role = strPtr(role)
		roleResume := insertLiveCardResume(ctx, t, tx, createTestUser(ctx, t, tx), uniqueCardSlug())
		if insertErr := insertShowcase(ctx, tx, roleResume, row); insertErr != nil {
			t.Errorf("role %q rejected: %v", role, insertErr)
		}
	}

	good := validShowcaseRow()
	good.templateID = strPtr(strings.Repeat("a", 64))
	if insertErr := insertShowcase(ctx, tx, resumeID, good); insertErr != nil {
		t.Fatalf("insert row with a 64 character template: %v", insertErr)
	}
	err = withSavepoint(ctx, t, tx, func(sp pgx.Tx) error { return insertShowcase(ctx, sp, resumeID, validShowcaseRow()) })
	requireConstraintViolation(t, err, "resume_showcase_pkey")
}

// AC-SHOW-001, AC-SHOW-015: the row goes with its resume, and with its account
// through the resume.
func TestResumeShowcaseCascades(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userID := createTestUser(ctx, t, tx)
	resumeID := insertLiveCardResume(ctx, t, tx, userID, uniqueCardSlug())
	if err := insertShowcase(ctx, tx, resumeID, validShowcaseRow()); err != nil {
		t.Fatalf("insert showcase row: %v", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM resumes WHERE id = $1`, resumeID); err != nil {
		t.Fatalf("delete resume: %v", err)
	}
	if got := showcaseCount(ctx, t, tx, resumeID); got != 0 {
		t.Fatalf("showcase rows after resume delete = %d, want 0", got)
	}

	accountResume := insertLiveCardResume(ctx, t, tx, userID, uniqueCardSlug())
	if err := insertShowcase(ctx, tx, accountResume, validShowcaseRow()); err != nil {
		t.Fatalf("insert showcase row: %v", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if got := showcaseCount(ctx, t, tx, accountResume); got != 0 {
		t.Fatalf("showcase rows after account delete = %d, want 0", got)
	}
}

// AC-SHOW-009: the migration refuses while a resume holds the slug showcase,
// and adds only the table once the slug is free.
func TestResumeShowcaseMigrationRefusesTheShowcaseSlug(t *testing.T) {
	t.Parallel()
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
	ctx, cancel := context.WithTimeout(context.Background(), harnessTimeout)
	defer cancel()
	provider, err := migrations.NewProvider(db, migrations.FS)
	if err != nil {
		t.Fatalf("NewProvider() error: %v", err)
	}
	if _, err = provider.UpTo(ctx, 9); err != nil {
		t.Fatalf("UpTo(9) error: %v", err)
	}
	var userID, resumeID uuid.UUID
	if err = db.QueryRowContext(ctx, `INSERT INTO users (email, name) VALUES ($1, 'Showcase Slug') RETURNING id`, uuid.NewString()+"@example.com").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if err = db.QueryRowContext(ctx, `
		INSERT INTO resumes (user_id, title, slug, live, schema_version, revision, personal_details, content, customization)
		VALUES ($1, 'Holder', 'showcase', false, 1, 1, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb) RETURNING id`, userID).Scan(&resumeID); err != nil {
		t.Fatalf("insert resume holding the slug: %v", err)
	}

	_, err = provider.UpTo(ctx, 10)
	if err == nil {
		t.Fatal("UpTo(10) succeeded while a resume holds the slug showcase")
	}
	if !strings.Contains(err.Error(), "the slug showcase") {
		t.Fatalf("UpTo(10) error = %v, want the slug refusal", err)
	}
	if n := mustQueryInt(ctx, t, db, `SELECT count(*) FROM pg_class WHERE relname = 'resume_showcase'`); n != 0 {
		t.Fatalf("resume_showcase tables after a refused migration = %d, want 0", n)
	}

	if _, err = db.ExecContext(ctx, `DELETE FROM resumes WHERE id = $1`, resumeID); err != nil {
		t.Fatalf("delete the slug holder: %v", err)
	}
	if _, err = provider.UpTo(ctx, 10); err != nil {
		t.Fatalf("UpTo(10) after the slug is free: %v", err)
	}
	if n := mustQueryInt(ctx, t, db, `SELECT count(*) FROM pg_class WHERE relname = 'resume_showcase'`); n != 1 {
		t.Fatalf("resume_showcase tables after the migration = %d, want 1", n)
	}
}

func TestResumeShowcaseMigrationUpDownUp(t *testing.T) {
	t.Parallel()
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
	ctx, cancel := context.WithTimeout(context.Background(), harnessTimeout)
	defer cancel()
	provider, err := migrations.NewProvider(db, migrations.FS)
	if err != nil {
		t.Fatalf("NewProvider() error: %v", err)
	}
	const table = `SELECT count(*) FROM pg_class WHERE relname = 'resume_showcase'`
	if _, err = provider.UpTo(ctx, 10); err != nil {
		t.Fatalf("UpTo(10) error: %v", err)
	}
	if n := mustQueryInt(ctx, t, db, table); n != 1 {
		t.Fatalf("resume_showcase tables after up = %d, want 1", n)
	}
	if _, err = provider.DownTo(ctx, 9); err != nil {
		t.Fatalf("DownTo(9) error: %v", err)
	}
	if n := mustQueryInt(ctx, t, db, table); n != 0 {
		t.Fatalf("resume_showcase tables after down = %d, want 0", n)
	}
	if _, err = provider.UpTo(ctx, 10); err != nil {
		t.Fatalf("UpTo(10) after down error: %v", err)
	}
}
