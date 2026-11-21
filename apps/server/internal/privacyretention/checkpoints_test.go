package privacyretention

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

func TestRetainDeletesOldAndUnacceptedCheckpoints(t *testing.T) {
	ctx, pool := newRetentionPool(t)
	owner := createRetentionUser(ctx, t, store.New(pool))
	t.Cleanup(func() { cleanupUsers(ctx, t, pool, owner) })
	var resumeID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO resumes (user_id, title, schema_version, personal_details, content, customization)
		VALUES ($1, 'Fixture', $2, '{}', '{}', '{}') RETURNING id`, owner, docmigrate.CurrentVersion).Scan(&resumeID); err != nil {
		t.Fatal(err)
	}
	cutoff := retentionNow.Add(-28 * 24 * time.Hour)
	for _, tc := range []struct {
		title   string
		started time.Time
		version int32
	}{{"old", cutoff.Add(-time.Microsecond), docmigrate.CurrentVersion},
		{"boundary", cutoff, docmigrate.CurrentVersion}, {"unknown", retentionNow, 9999}} {
		if _, err := pool.Exec(ctx, `INSERT INTO resume_checkpoints
			(resume_id, started_at, cause, title, schema_version, personal_details, content, customization, byte_size)
			VALUES ($1, $2, 'edit', $3, $4, '{}', '{}', '{}', 10)`, resumeID, tc.started, tc.title, tc.version); err != nil {
			t.Fatal(err)
		}
	}
	w, err := New(Config{Pool: pool, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return retentionNow }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Retain(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	var title string
	if err = pool.QueryRow(ctx, `SELECT count(*), min(title) FROM resume_checkpoints WHERE resume_id = $1`, resumeID).Scan(&count, &title); err != nil || count != 1 || title != "boundary" {
		t.Fatalf("retained %d rows, first title %q: %v", count, title, err)
	}
}
