package migrations_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/migrations"
)

func TestResumeCheckpointsMigrationUpDownUp(t *testing.T) {
	t.Parallel()
	dsn := newTestDatabase(t)
	db := openTestDB(t, dsn)
	ctx, cancel := context.WithTimeout(t.Context(), 2*harnessTimeout)
	defer cancel()
	provider, err := migrations.NewProvider(db, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provider.UpTo(ctx, 21); err != nil {
		t.Fatalf("apply pre-history migrations: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := conn.Close(context.Background()); closeErr != nil {
			t.Error(closeErr)
		}
	})
	owner := createTestUser(ctx, t, conn)
	var resumeID uuid.UUID
	if err = conn.QueryRow(ctx, `INSERT INTO resumes (user_id, title, schema_version, personal_details, content, customization)
		VALUES ($1, 'Existing', 1, '{}', '{}', '{}') RETURNING id`, owner).Scan(&resumeID); err != nil {
		t.Fatal(err)
	}

	for _, stage := range []string{"first up", "second up"} {
		if _, err = provider.UpTo(ctx, 22); err != nil {
			t.Fatalf("%s: %v", stage, err)
		}
		var empty bool
		if err = conn.QueryRow(ctx, `SELECT session_actor IS NULL AND session_started_at IS NULL AND session_last_write_at IS NULL
			FROM resumes WHERE id = $1`, resumeID).Scan(&empty); err != nil || !empty {
			t.Fatalf("%s: existing resume session not empty: %v", stage, err)
		}
		if _, err = conn.Exec(ctx, `UPDATE resumes SET session_actor = 'owner' WHERE id = $1`, resumeID); err == nil {
			t.Fatalf("%s: accepted partially set session", stage)
		}
		if _, err = conn.Exec(ctx, `INSERT INTO resume_checkpoints
			(resume_id, started_at, cause, title, schema_version, personal_details, content, customization, byte_size)
			VALUES ($1, now(), 'edit', 'Existing', 1, '{}', '{}', '{}', 10)`, resumeID); err != nil {
			t.Fatalf("%s: insert owner checkpoint: %v", stage, err)
		}
		if stage == "second up" {
			break
		}
		if _, err = provider.DownTo(ctx, 21); err != nil {
			t.Fatalf("down: %v", err)
		}
		var absent bool
		if err = conn.QueryRow(ctx, `SELECT to_regclass('public.resume_checkpoints') IS NULL AND NOT EXISTS (
			SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'resumes'
			AND column_name IN ('session_actor', 'session_started_at', 'session_last_write_at'))`).Scan(&absent); err != nil || !absent {
			t.Fatalf("down left checkpoint storage or session columns: %v", err)
		}
	}
	if _, err = conn.Exec(ctx, `DELETE FROM resumes WHERE id = $1`, resumeID); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM resume_checkpoints WHERE resume_id = $1`, resumeID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("deleted resume has %d checkpoints: %v", count, err)
	}
}
