package migrations_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestPrivacyLifecycleSchema(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM lifecycle_audit_events`).Scan(&count); err != nil {
		t.Fatalf("read lifecycle audit: %v", err)
	}
	checks := []struct {
		name, query, constraint string
	}{
		{"unknown audit kind", `INSERT INTO lifecycle_audit_events (kind) VALUES ('untrusted')`, "lifecycle_audit_events_kind_check"},
		{"media event needs job", `INSERT INTO lifecycle_audit_events (kind) VALUES ('media_deletion_completed')`, "lifecycle_audit_events_scope_check"},
		{"account event has no job", `INSERT INTO lifecycle_audit_events (kind, media_job_id) VALUES ('account_deleted', uuidv7())`, "lifecycle_audit_events_scope_check"},
		{"identity unlink event has no job", `INSERT INTO lifecycle_audit_events (kind, media_job_id) VALUES ('identity_unlinked', uuidv7())`, "lifecycle_audit_events_scope_check"},
		{"unknown sweep", `INSERT INTO privacy_sweep_state (name) VALUES ('untrusted')`, "privacy_sweep_state_name_check"},
		{"long cursor", `UPDATE privacy_sweep_state SET cursor = repeat('x', 1025)`, "privacy_sweep_state_cursor_check"},
		{"control cursor", `UPDATE privacy_sweep_state SET cursor = E'bad\nvalue'`, "privacy_sweep_state_cursor_check"},
	}
	for _, tt := range checks {
		t.Run(tt.name, func(t *testing.T) {
			err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
				_, err := sp.Exec(ctx, tt.query)
				return err
			})
			requireConstraintViolation(t, err, tt.constraint)
		})
	}
}

// An identity unlink records only its kind and time.
func TestLifecycleAuditAcceptsIdentityUnlinked(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	if _, err := tx.Exec(ctx, `INSERT INTO lifecycle_audit_events (kind) VALUES ('identity_unlinked')`); err != nil {
		t.Fatalf("insert identity_unlinked audit event: %v", err)
	}
}

func TestPrivacyLifecycleQueueSurvivesAccountDeletion(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	var userID, resumeID, jobID string
	if err := tx.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ('privacy-schema@example.test', 'Example') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `SELECT uuidv7()::text`).Scan(&resumeID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO media_deletion_jobs (resume_id, object_key) VALUES ($1::uuid, 'resumes/' || $1 || '/photo-00000000000000000000000000000000.png') RETURNING id::text`, resumeID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO lifecycle_audit_events (kind) VALUES ('account_deleted')`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatal(err)
	}
	var completed *time.Time
	if err := tx.QueryRow(ctx, `SELECT completed_at FROM media_deletion_jobs WHERE id = $1`, jobID).Scan(&completed); err != nil || completed != nil {
		t.Fatalf("pending cleanup survived: completed=%v err=%v", completed, err)
	}
	if _, err := tx.Exec(ctx, `UPDATE media_deletion_jobs SET completed_at = now(), outcome = 'deleted' WHERE id = $1`, jobID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO lifecycle_audit_events (kind, media_job_id) VALUES ('media_deletion_completed', $1)`, jobID); err != nil {
		t.Fatal(err)
	}
	err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, `UPDATE media_deletion_jobs SET lease_id = uuidv7(), lease_expires_at = now() + interval '30 seconds' WHERE id = $1`, jobID)
		return err
	})
	requireConstraintViolation(t, err, "media_deletion_jobs_lease_check")
	err = withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
		_, insertErr := sp.Exec(ctx, `INSERT INTO lifecycle_audit_events (kind, media_job_id) VALUES ('media_deletion_completed', $1)`, jobID)
		return insertErr
	})
	requireConstraintViolation(t, err, "lifecycle_audit_events_job_kind_key")
}
