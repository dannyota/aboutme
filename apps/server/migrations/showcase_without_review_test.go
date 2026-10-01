// Migration 00011 (docs/design/showcase.md "Data and contract", ADR 0029):
// the showcase has no review. It ends declined opt-ins, clears every stored
// review result, defaults the unused review key, and adds the listing index.
package migrations_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

// seedShowcaseRow inserts a resume and its showcase row through plain SQL at
// schema version 10, with the review columns as given.
func seedShowcaseRow(ctx context.Context, t *testing.T, db *sql.DB, userID uuid.UUID, slug, reviewedKey, outcome, firstListed string) uuid.UUID {
	t.Helper()
	var resumeID uuid.UUID
	if err := db.QueryRowContext(ctx, `
		INSERT INTO resumes (user_id, title, slug, live, schema_version, revision, personal_details, content, customization)
		VALUES ($1, 'Seed', $2, true, 1, 1, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb) RETURNING id`, userID, slug).Scan(&resumeID); err != nil {
		t.Fatalf("insert resume %s: %v", slug, err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO resume_showcase (
			resume_id, requested_at, review_key, card_version,
			reviewed_key, review_outcome, reviewed_at, first_listed_at
		) VALUES (
			$1, now(), $2, $3,
			NULLIF($4, ''), NULLIF($5, ''),
			CASE WHEN $5 = '' THEN NULL ELSE now() END,
			CASE WHEN $6 = '' THEN NULL ELSE now() END
		)`, resumeID, showcaseHexA, showcaseHexB, reviewedKey, outcome, firstListed); err != nil {
		t.Fatalf("insert showcase row %s: %v", slug, err)
	}
	return resumeID
}

// AC-SHOW-001, AC-SHOW-005: rows in each earlier review state end as the design
// says, and the previous release's listing condition matches none of them.
func TestShowcaseWithoutReviewMigrationClearsReviewState(t *testing.T) {
	t.Parallel()
	dsn := newTestDatabase(t)
	db := openTestDB(t, dsn)
	ctx, cancel := context.WithTimeout(context.Background(), harnessTimeout)
	defer cancel()
	provider, err := migrations.NewProvider(db, migrations.FS)
	if err != nil {
		t.Fatalf("NewProvider() error: %v", err)
	}
	if _, err = provider.UpTo(ctx, 10); err != nil {
		t.Fatalf("UpTo(10) error: %v", err)
	}
	var userID uuid.UUID
	if err = db.QueryRowContext(ctx, `INSERT INTO users (email, name) VALUES ($1, 'Showcase') RETURNING id`, uuid.NewString()+"@example.com").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	pending := seedShowcaseRow(ctx, t, db, userID, "nr-pending", "", "", "")
	approved := seedShowcaseRow(ctx, t, db, userID, "nr-approved", showcaseHexA, "approved", "x")
	stale := seedShowcaseRow(ctx, t, db, userID, "nr-stale", showcaseHexB, "approved", "x")
	declined := seedShowcaseRow(ctx, t, db, userID, "nr-declined", showcaseHexA, "declined", "")
	declinedAfterListing := seedShowcaseRow(ctx, t, db, userID, "nr-declined-listed", showcaseHexA, "declined", "x")

	const previousListing = `SELECT count(*) FROM resume_showcase WHERE review_outcome = 'approved' AND reviewed_key = review_key`
	if n := mustQueryInt(ctx, t, db, previousListing); n != 1 {
		t.Fatalf("rows the previous release lists before the migration = %d, want 1", n)
	}

	if _, err = provider.UpTo(ctx, 11); err != nil {
		t.Fatalf("UpTo(11) error: %v", err)
	}

	present := func(id uuid.UUID) bool {
		var n int
		if scanErr := db.QueryRowContext(ctx, `SELECT count(*) FROM resume_showcase WHERE resume_id = $1`, id).Scan(&n); scanErr != nil {
			t.Fatalf("count row: %v", scanErr)
		}
		return n == 1
	}
	for name, id := range map[string]uuid.UUID{"pending": pending, "approved": approved, "stale approved": stale} {
		if !present(id) {
			t.Errorf("%s row was deleted, want kept", name)
		}
	}
	for name, id := range map[string]uuid.UUID{"declined": declined, "declined after listing": declinedAfterListing} {
		if present(id) {
			t.Errorf("%s row was kept, want deleted", name)
		}
	}
	if n := mustQueryInt(ctx, t, db, `SELECT count(*) FROM resume_showcase`); n != 3 {
		t.Errorf("rows after the migration = %d, want 3", n)
	}
	const stored = `SELECT count(*) FROM resume_showcase WHERE reviewed_key IS NOT NULL OR review_outcome IS NOT NULL OR reviewed_at IS NOT NULL OR first_listed_at IS NOT NULL`
	if n := mustQueryInt(ctx, t, db, stored); n != 0 {
		t.Errorf("rows with a stored review result = %d, want 0", n)
	}
	const keyed = `SELECT count(*) FROM resume_showcase WHERE review_key <> '0000000000000000'`
	if n := mustQueryInt(ctx, t, db, keyed); n != 0 {
		t.Errorf("rows with a stored review key = %d, want 0", n)
	}
	if n := mustQueryInt(ctx, t, db, previousListing); n != 0 {
		t.Errorf("rows the previous release lists after the migration = %d, want 0", n)
	}
}

// AC-SHOW-005, AC-SHOW-015: an insert without review_key takes the default, and
// the listing index serves the opt-in order.
func TestShowcaseWithoutReviewMigrationDefaultAndIndex(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	resumeID := insertLiveCardResume(ctx, t, tx, createTestUser(ctx, t, tx), uniqueCardSlug())
	if _, err := tx.Exec(ctx, `
		INSERT INTO resume_showcase (resume_id, requested_at, card_version) VALUES ($1, now(), $2)`,
		resumeID, showcaseHexB); err != nil {
		t.Fatalf("insert without review_key: %v", err)
	}
	var key string
	if err := tx.QueryRow(ctx, `SELECT review_key FROM resume_showcase WHERE resume_id = $1`, resumeID).Scan(&key); err != nil {
		t.Fatalf("read review_key: %v", err)
	}
	if key != "0000000000000000" {
		t.Errorf("default review_key = %q, want 0000000000000000", key)
	}

	var definition string
	if err := tx.QueryRow(ctx, `
		SELECT indexdef FROM pg_indexes
		WHERE schemaname = 'public' AND tablename = 'resume_showcase' AND indexname = 'resume_showcase_requested_idx'`,
	).Scan(&definition); err != nil {
		t.Fatalf("read requested index: %v", err)
	}
	for _, want := range []string{"requested_at DESC", "resume_id DESC"} {
		if !strings.Contains(definition, want) {
			t.Errorf("index definition %q lacks %q", definition, want)
		}
	}
	if strings.Contains(definition, "WHERE") {
		t.Errorf("index definition %q is partial, want a full index", definition)
	}
}

// The migration reverses to the version-10 shape: no default, no new index.
func TestShowcaseWithoutReviewMigrationUpDownUp(t *testing.T) {
	t.Parallel()
	dsn := newTestDatabase(t)
	db := openTestDB(t, dsn)
	ctx, cancel := context.WithTimeout(context.Background(), harnessTimeout)
	defer cancel()
	provider, err := migrations.NewProvider(db, migrations.FS)
	if err != nil {
		t.Fatalf("NewProvider() error: %v", err)
	}
	const index = `SELECT count(*) FROM pg_indexes WHERE indexname = 'resume_showcase_requested_idx'`
	const withDefault = `SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'resume_showcase' AND column_name = 'review_key' AND column_default IS NOT NULL`
	if _, err = provider.UpTo(ctx, 11); err != nil {
		t.Fatalf("UpTo(11) error: %v", err)
	}
	if n := mustQueryInt(ctx, t, db, index); n != 1 {
		t.Fatalf("requested index after up = %d, want 1", n)
	}
	if n := mustQueryInt(ctx, t, db, withDefault); n != 1 {
		t.Fatalf("review_key default after up = %d, want 1", n)
	}
	if _, err = provider.DownTo(ctx, 10); err != nil {
		t.Fatalf("DownTo(10) error: %v", err)
	}
	if n := mustQueryInt(ctx, t, db, index); n != 0 {
		t.Errorf("requested index after down = %d, want 0", n)
	}
	if n := mustQueryInt(ctx, t, db, withDefault); n != 0 {
		t.Errorf("review_key default after down = %d, want 0", n)
	}
	if _, err = provider.UpTo(ctx, 11); err != nil {
		t.Fatalf("UpTo(11) after down error: %v", err)
	}
}
