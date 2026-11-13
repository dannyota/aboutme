// Migration 00018 (docs/design/release-floors.md "Schema floor"): the unused
// showcase review columns, their checks, and the approval index are gone, the
// rest of the table and every kept value stay, and Down restores the
// version-17 table.
package migrations_test

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"danny.vn/aboutme/apps/server/migrations"
)

// droppedShowcaseObjects are the showcaseCatalog keys of every object 00018
// drops.
var droppedShowcaseObjects = []string{
	"column review_key", "column reviewed_key", "column review_outcome",
	"column reviewed_at", "column first_listed_at",
	"constraint resume_showcase_review_key_check",
	"constraint resume_showcase_reviewed_key_check",
	"constraint resume_showcase_review_outcome_check",
	"constraint resume_showcase_review_columns_check",
	"constraint resume_showcase_first_listed_check",
	"index resume_showcase_listing_idx",
}

// showcaseCatalogQuery describes resume_showcase by object name: each column's
// type, nullability, and default; each constraint's definition; and each
// index's definition. Names, not column positions, identify the objects, so a
// column that Down re-adds at the end compares equal. PostgreSQL 18 also lists
// each NOT NULL as a named constraint; nullability is read from the column, so
// those are left out and the snapshot is the same on every server version.
const showcaseCatalogQuery = `
SELECT 'column ' || a.attname,
       format_type(a.atttypid, a.atttypmod)
           || CASE WHEN a.attnotnull THEN ' not null' ELSE ' null' END
           || coalesce(' default ' || pg_get_expr(d.adbin, d.adrelid), '')
FROM pg_attribute a
LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
WHERE a.attrelid = 'public.resume_showcase'::regclass AND a.attnum > 0 AND NOT a.attisdropped
UNION ALL
SELECT 'constraint ' || c.conname, pg_get_constraintdef(c.oid)
FROM pg_constraint c
WHERE c.conrelid = 'public.resume_showcase'::regclass AND c.contype <> 'n'
UNION ALL
SELECT 'index ' || ic.relname, pg_get_indexdef(i.indexrelid)
FROM pg_index i
JOIN pg_class ic ON ic.oid = i.indexrelid
WHERE i.indrelid = 'public.resume_showcase'::regclass`

// keptShowcase is the value of every column 00018 keeps, for one row.
type keptShowcase struct {
	resumeID    uuid.UUID
	requestedAt time.Time
	role        *string
	cardVersion string
	templateID  *string
}

func sameOptional(a, b *string) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}

func (k keptShowcase) equal(other keptShowcase) bool {
	return k.resumeID == other.resumeID && k.requestedAt.Equal(other.requestedAt) &&
		sameOptional(k.role, other.role) && k.cardVersion == other.cardVersion &&
		sameOptional(k.templateID, other.templateID)
}

// showcaseCatalog reads resume_showcase's catalog, keyed by object kind and
// name.
func showcaseCatalog(ctx context.Context, t *testing.T, conn *pgx.Conn) map[string]string {
	t.Helper()
	rows, err := conn.Query(ctx, showcaseCatalogQuery)
	if err != nil {
		t.Fatalf("read showcase catalog: %v", err)
	}
	defer rows.Close()
	catalog := map[string]string{}
	for rows.Next() {
		var key, definition string
		if err = rows.Scan(&key, &definition); err != nil {
			t.Fatalf("scan showcase catalog: %v", err)
		}
		if _, repeated := catalog[key]; repeated {
			t.Fatalf("showcase catalog repeats %s", key)
		}
		catalog[key] = definition
	}
	if err = rows.Err(); err != nil {
		t.Fatalf("read showcase catalog: %v", err)
	}
	return catalog
}

// requireCatalog fails with every entry where got differs from want.
func requireCatalog(t *testing.T, stage string, got, want map[string]string) {
	t.Helper()
	keys := slices.Collect(maps.Keys(got))
	for key := range want {
		if _, inGot := got[key]; !inGot {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	for _, key := range keys {
		gotDefinition, inGot := got[key]
		wantDefinition, inWant := want[key]
		switch {
		case !inWant:
			t.Errorf("%s: unexpected %s: %s", stage, key, gotDefinition)
		case !inGot:
			t.Errorf("%s: missing %s: %s", stage, key, wantDefinition)
		case gotDefinition != wantDefinition:
			t.Errorf("%s: %s = %s, want %s", stage, key, gotDefinition, wantDefinition)
		}
	}
}

// seedShowcaseOptIns commits three opt-ins the way v0.9.2 inserts them, with
// and without a role and a template, each on its own resume.
func seedShowcaseOptIns(ctx context.Context, t *testing.T, conn *pgx.Conn) map[uuid.UUID]keptShowcase {
	t.Helper()
	requested := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	seeds := []struct{ role, templateID *string }{
		{role: strPtr("backend"), templateID: strPtr("ats-plain")},
		{role: nil, templateID: nil},
		{role: strPtr("qa"), templateID: nil},
	}
	seeded := make(map[uuid.UUID]keptShowcase, len(seeds))
	for i, seed := range seeds {
		row := defaultResumeRow(createTestUser(ctx, t, conn), "unused")
		row.slug, row.live = strPtr(uniqueCardSlug()), true
		resumeID, err := insertResumeReturningID(ctx, conn, row)
		if err != nil {
			t.Fatalf("insert resume %d: %v", i, err)
		}
		kept := keptShowcase{
			resumeID: resumeID, requestedAt: requested.Add(time.Duration(i) * time.Minute),
			role: seed.role, cardVersion: showcaseHexB, templateID: seed.templateID,
		}
		if _, err = conn.Exec(ctx, `
			INSERT INTO resume_showcase (resume_id, requested_at, role, card_version, template_id)
			VALUES ($1, $2, $3, $4, $5)`,
			kept.resumeID, kept.requestedAt, kept.role, kept.cardVersion, kept.templateID); err != nil {
			t.Fatalf("insert showcase row %d: %v", i, err)
		}
		seeded[resumeID] = kept
	}
	return seeded
}

// requireKeptShowcaseRows fails unless the table holds exactly want, by the
// columns 00018 keeps.
func requireKeptShowcaseRows(ctx context.Context, t *testing.T, conn *pgx.Conn, stage string, want map[uuid.UUID]keptShowcase) {
	t.Helper()
	rows, err := conn.Query(ctx, `SELECT resume_id, requested_at, role, card_version, template_id FROM resume_showcase`)
	if err != nil {
		t.Fatalf("%s: read showcase rows: %v", stage, err)
	}
	defer rows.Close()
	got := 0
	for rows.Next() {
		var row keptShowcase
		if err = rows.Scan(&row.resumeID, &row.requestedAt, &row.role, &row.cardVersion, &row.templateID); err != nil {
			t.Fatalf("%s: scan showcase row: %v", stage, err)
		}
		got++
		if seeded, found := want[row.resumeID]; !found || !seeded.equal(row) {
			t.Errorf("%s: showcase row %+v, want %+v (seeded %t)", stage, row, seeded, found)
		}
	}
	if err = rows.Err(); err != nil {
		t.Fatalf("%s: read showcase rows: %v", stage, err)
	}
	if got != len(want) {
		t.Errorf("%s: showcase rows = %d, want %d", stage, got, len(want))
	}
}

// requireShowcaseReviewObjectsDropped fails unless the catalog is version 17's
// without exactly the objects 00018 drops, and a v0.8.28 read fails on it.
func requireShowcaseReviewObjectsDropped(ctx context.Context, t *testing.T, conn *pgx.Conn, stage string, version17 map[string]string) {
	t.Helper()
	want := maps.Clone(version17)
	for _, key := range droppedShowcaseObjects {
		delete(want, key)
	}
	requireCatalog(t, stage, showcaseCatalog(ctx, t, conn), want)
	err := conn.QueryRow(ctx, releasedShowcaseRead, uuid.New()).Scan()
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42703" {
		t.Errorf("%s: v0.8.28 showcase read = %v, want an undefined column", stage, err)
	}
}

// Up drops the five review columns, their five checks, and the approval index
// and keeps every other object and value; Down restores the version-17 catalog
// with review defaults on every row; Up runs again.
func TestShowcaseReviewColumnsDropMigrationUpDownUp(t *testing.T) {
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
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := conn.Close(context.Background()); closeErr != nil {
			t.Errorf("close connection: %v", closeErr)
		}
	})

	if _, err = provider.UpTo(ctx, showcaseReviewVersion); err != nil {
		t.Fatalf("UpTo(%d) error: %v", showcaseReviewVersion, err)
	}
	seeded := seedShowcaseOptIns(ctx, t, conn)
	version17 := showcaseCatalog(ctx, t, conn)
	for _, key := range droppedShowcaseObjects {
		if _, found := version17[key]; !found {
			t.Fatalf("version %d has no %s", showcaseReviewVersion, key)
		}
	}

	if _, err = provider.UpTo(ctx, 18); err != nil {
		t.Fatalf("UpTo(18) error: %v", err)
	}
	requireShowcaseReviewObjectsDropped(ctx, t, conn, "up", version17)
	requireKeptShowcaseRows(ctx, t, conn, "up", seeded)

	if _, err = provider.DownTo(ctx, showcaseReviewVersion); err != nil {
		t.Fatalf("DownTo(%d) error: %v", showcaseReviewVersion, err)
	}
	requireCatalog(t, "down", showcaseCatalog(ctx, t, conn), version17)
	requireKeptShowcaseRows(ctx, t, conn, "down", seeded)
	var defaulted int
	if err = conn.QueryRow(ctx, `
		SELECT count(*) FROM resume_showcase
		WHERE review_key = '0000000000000000' AND reviewed_key IS NULL AND review_outcome IS NULL
		  AND reviewed_at IS NULL AND first_listed_at IS NULL`).Scan(&defaulted); err != nil {
		t.Fatalf("count defaulted rows: %v", err)
	}
	if defaulted != len(seeded) {
		t.Errorf("rows with review defaults after down = %d, want %d", defaulted, len(seeded))
	}

	if _, err = provider.UpTo(ctx, 18); err != nil {
		t.Fatalf("UpTo(18) after down error: %v", err)
	}
	requireShowcaseReviewObjectsDropped(ctx, t, conn, "up again", version17)
	requireKeptShowcaseRows(ctx, t, conn, "up again", seeded)
}
