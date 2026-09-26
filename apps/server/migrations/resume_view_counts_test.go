package migrations_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/migrations"
)

// The view count tables hold numbers only, cascade with the resume, and
// accept the closed platform set (docs/design/viewer-analytics/delivery.md,
// "Schema").

func viewDate(y int, m time.Month, d int) pgtype.Date {
	return pgtype.Date{Time: time.Date(y, m, d, 0, 0, 0, 0, time.UTC), Valid: true}
}

func TestResumeViewCountConstraints(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	resumeID := insertLiveCardResume(ctx, t, tx, createTestUser(ctx, t, tx), uniqueCardSlug())

	tests := []struct {
		name  string
		query string
		args  []any
		want  string
	}{
		{"negative count", `INSERT INTO resume_view_days (resume_id, day, bot) VALUES ($1, '2026-09-26', -1)`, []any{resumeID}, "resume_view_days_counts_check"},
		{"missing resume", `INSERT INTO resume_view_days (resume_id, day) VALUES ($1, '2026-09-26')`, []any{uuid.New()}, "resume_view_days_resume_id_fkey"},
		{"unknown platform", `INSERT INTO resume_share_signal_days (resume_id, day, platform) VALUES ($1, '2026-09-26', 'myspace')`, []any{resumeID}, "resume_share_signal_days_platform_check"},
		{"negative fetches", `INSERT INTO resume_share_signal_days (resume_id, day, platform, fetches) VALUES ($1, '2026-09-26', 'zalo', -1)`, []any{resumeID}, "resume_share_signal_days_fetches_check"},
		{"missing signal resume", `INSERT INTO resume_share_signal_days (resume_id, day, platform) VALUES ($1, '2026-09-26', 'zalo')`, []any{uuid.New()}, "resume_share_signal_days_resume_id_fkey"},
	}
	for _, test := range tests {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			_, execErr := sp.Exec(ctx, test.query, test.args...)
			return execErr
		})
		requireConstraintViolation(t, err, test.want)
	}
}

func TestResumeViewCountsUpsertCascadeAndSweep(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	q := store.New(tx)
	userID := createTestUser(ctx, t, tx)
	resumeID := insertLiveCardResume(ctx, t, tx, userID, uniqueCardSlug())
	today := viewDate(2026, time.September, 26)
	old := viewDate(2025, time.August, 1)

	add := func(id uuid.UUID, day pgtype.Date, counted, bot int32) {
		t.Helper()
		if err := q.AddResumeViewDays(ctx, store.AddResumeViewDaysParams{
			ResumeIds: []uuid.UUID{id}, Days: []pgtype.Date{day},
			Counted: []int32{counted}, Bot: []int32{bot}, Datacenter: []int32{0},
			Anomaly: []int32{0}, Invalid: []int32{0}, Crawler: []int32{0},
		}); err != nil {
			t.Fatalf("AddResumeViewDays: %v", err)
		}
	}
	add(resumeID, today, 2, 1)
	add(resumeID, today, 3, 0)
	add(resumeID, old, 7, 0)
	// A resume deleted before the flush is skipped, not an error.
	add(uuid.New(), today, 1, 0)

	if err := q.AddResumeShareSignalDays(ctx, store.AddResumeShareSignalDaysParams{
		ResumeIds: []uuid.UUID{resumeID, resumeID}, Days: []pgtype.Date{today, today},
		Platforms: []string{"zalo", "facebook"}, Fetches: []int32{3, 1},
	}); err != nil {
		t.Fatalf("AddResumeShareSignalDays: %v", err)
	}

	summaries, err := q.ListViewSummaries(ctx, store.ListViewSummariesParams{
		Since7: viewDate(2026, time.September, 20), Since30: viewDate(2026, time.August, 28),
		Since90: viewDate(2026, time.June, 29), UserID: userID,
	})
	if err != nil {
		t.Fatalf("ListViewSummaries: %v", err)
	}
	if len(summaries) != 1 || summaries[0].Real7 != 5 || summaries[0].Filtered7 != 1 || summaries[0].Real90 != 5 {
		t.Fatalf("summaries = %+v, want one resume with 5 real and 1 filtered", summaries)
	}
	signals, err := q.ListResumeShareSignals(ctx, store.ListResumeShareSignalsParams{ResumeID: resumeID, Since: viewDate(2026, time.June, 29)})
	if err != nil {
		t.Fatalf("ListResumeShareSignals: %v", err)
	}
	if len(signals) != 2 || signals[0].Platform != "zalo" || signals[0].Fetches != 3 {
		t.Fatalf("signals = %+v, want zalo 3 first", signals)
	}

	// Rows older than the 400-day cutoff go; newer rows stay.
	deleted, err := q.DeleteOldResumeViewDaysPage(ctx, store.DeleteOldResumeViewDaysPageParams{Cutoff: viewDate(2025, time.August, 22), LimitRows: 1000})
	if err != nil || deleted != 1 {
		t.Fatalf("DeleteOldResumeViewDaysPage = %d, %v; want 1", deleted, err)
	}

	if _, err := tx.Exec(ctx, `DELETE FROM resumes WHERE id = $1`, resumeID); err != nil {
		t.Fatalf("delete resume: %v", err)
	}
	var remaining int
	if err := tx.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM resume_view_days WHERE resume_id = $1)
		     + (SELECT count(*) FROM resume_share_signal_days WHERE resume_id = $1)`, resumeID).Scan(&remaining); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("rows after resume delete = %d, want 0", remaining)
	}
}

func TestResumeViewCountsMigrationUpDownUp(t *testing.T) {
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
	const tables = `SELECT count(*) FROM pg_class WHERE relname IN ('resume_view_days', 'resume_share_signal_days')`
	if _, err := provider.UpTo(ctx, 8); err != nil {
		t.Fatalf("UpTo(8) error: %v", err)
	}
	if n := mustQueryInt(ctx, t, db, tables); n != 2 {
		t.Fatalf("view count tables after up = %d, want 2", n)
	}
	if _, err := provider.DownTo(ctx, 7); err != nil {
		t.Fatalf("DownTo(7) error: %v", err)
	}
	if n := mustQueryInt(ctx, t, db, tables); n != 0 {
		t.Fatalf("view count tables after down = %d, want 0", n)
	}
	if _, err := provider.UpTo(ctx, 8); err != nil {
		t.Fatalf("UpTo(8) again error: %v", err)
	}
}
