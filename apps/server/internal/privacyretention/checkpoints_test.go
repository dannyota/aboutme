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
	q := store.New(pool)
	backlogArgs := store.GetResumeCheckpointsBacklogParams{Cutoff: cutoff, AcceptedVersions: docmigrate.AcceptedVersions()}
	before, err := q.GetResumeCheckpointsBacklog(ctx, backlogArgs)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		title   string
		started time.Time
		version int32
	}{{"old", cutoff.Add(-3 * time.Second), docmigrate.CurrentVersion},
		{"boundary", cutoff, docmigrate.CurrentVersion}, {"unknown", retentionNow, 9999}} {
		if _, insertErr := pool.Exec(ctx, `INSERT INTO resume_checkpoints
			(resume_id, started_at, cause, title, schema_version, personal_details, content, customization, byte_size)
			VALUES ($1, $2, 'edit', $3, $4, '{}', '{}', '{}', 10)`, resumeID, tc.started, tc.title, tc.version); insertErr != nil {
			t.Fatal(insertErr)
		}
	}
	seeded, err := q.GetResumeCheckpointsBacklog(ctx, backlogArgs)
	if err != nil || seeded.Backlog != before.Backlog+2 || seeded.OldestAgeSeconds != max(before.OldestAgeSeconds, 3) {
		t.Fatalf("backlog before=%+v seeded=%+v error=%v", before, seeded, err)
	}
	w, err := New(Config{Pool: pool, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return retentionNow }})
	if err != nil {
		t.Fatal(err)
	}
	result, err := w.Retain(ctx)
	if err != nil {
		t.Fatal(err)
	}
	after, err := q.GetResumeCheckpointsBacklog(ctx, backlogArgs)
	if err != nil || result.ResumeCheckpointsDeleted < 2 || after.Backlog > before.Backlog || result.ResumeCheckpointsBacklog != after.Backlog || result.ResumeCheckpointsOldestSeconds != after.OldestAgeSeconds {
		t.Fatalf("sweep result=%+v backlog=%+v error=%v", result, after, err)
	}
	var count int
	var title string
	if err = pool.QueryRow(ctx, `SELECT count(*), min(title) FROM resume_checkpoints WHERE resume_id = $1`, resumeID).Scan(&count, &title); err != nil || count != 1 || title != "boundary" {
		t.Fatalf("retained %d rows, first title %q: %v", count, title, err)
	}
}
