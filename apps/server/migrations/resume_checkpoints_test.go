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

func TestResumeCheckpointActorConstraints(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTxAt(t, 22)
	owner := createTestUser(ctx, t, tx)
	client := insertOAuthClient(ctx, t, tx)
	grant := insertOAuthGrant(ctx, t, tx, owner, client)
	var resumeID uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO resumes (user_id,title,schema_version,personal_details,content,customization)
    VALUES ($1,'Fixture',1,'{}','{}','{}') RETURNING id`, owner).Scan(&resumeID); err != nil {
		t.Fatal(err)
	}
	const insert = `INSERT INTO resume_checkpoints
    (resume_id,started_at,cause,actor_grant_id,actor_name,title,schema_version,personal_details,content,customization,byte_size)
    VALUES ($1,now(),$2,$3,$4,'Fixture',1,'{}','{}','{}',1)`
	if _, err := tx.Exec(ctx, insert, resumeID, "assistant", grant, ""); err != nil {
		t.Fatalf("assistant with empty copied name: %v", err)
	}
	for _, tc := range []struct {
		name, constraint, query string
		args                    []any
	}{
		{"assistant without copied name", "resume_checkpoints_actor_name_check", insert, []any{resumeID, "assistant", grant, nil}},
		{"owner with a copied name", "resume_checkpoints_actor_name_check", insert, []any{resumeID, "edit", nil, "Assistant"}},
		{"owner with a grant", "resume_checkpoints_actor_grant_check", insert, []any{resumeID, "edit", grant, nil}},
		{"invalid session actor", "resumes_session_actor_check", `UPDATE resumes SET session_actor='grant:not-a-uuid',
    session_started_at=now(),session_last_write_at=now() WHERE id=$1`, []any{resumeID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sp, err := tx.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, statementErr := sp.Exec(ctx, tc.query, tc.args...)
			if err = sp.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			requireConstraintViolation(t, statementErr, tc.constraint)
		})
	}
	if _, err := tx.Exec(ctx, `UPDATE resumes SET session_actor='grant:' || $2::uuid::text,
    session_started_at=now(),session_last_write_at=now() WHERE id=$1`, resumeID, grant); err != nil {
		t.Fatalf("valid grant session: %v", err)
	}
}
