// Head-state constraint tests for the idempotency_usage counters and the
// media_deletion_jobs ledger (ADR 0016, ADR 0019). See docs/design/data.md
// for the retention and cleanup rules these tables enforce.
package migrations_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func insertMediaDeletionJob(ctx context.Context, db sqlExecer, resumeID, objectKey string) error {
	_, err := db.Exec(ctx, `INSERT INTO media_deletion_jobs (resume_id, object_key) VALUES ($1, $2)`, resumeID, objectKey)
	return err
}

func TestMediaDeletionJobsConstraints(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userID := createTestUser(ctx, t, tx)
	resumeID, err := insertResumeReturningID(ctx, tx, defaultResumeRow(userID, "media-ledger"))
	if err != nil {
		t.Fatalf("insert resume: %v", err)
	}
	otherResumeID, err := insertResumeReturningID(ctx, tx, defaultResumeRow(userID, "media-ledger-2"))
	if err != nil {
		t.Fatalf("insert second resume: %v", err)
	}

	goodKey := "resumes/" + resumeID.String() + "/photo-0123456789abcdef0123456789abcdef.jpg"
	if err := insertMediaDeletionJob(ctx, tx, resumeID.String(), goodKey); err != nil {
		t.Fatalf("insert valid deletion job: %v", err)
	}

	t.Run("duplicate object key rejected", func(t *testing.T) {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return insertMediaDeletionJob(ctx, sp, resumeID.String(), goodKey)
		})
		requireConstraintViolation(t, err, "media_deletion_jobs_object_key_key")
	})

	badKeys := []struct{ name, resumeID, key string }{
		{"cross-resume key", otherResumeID.String(), goodKey},
		{"malformed prefix", resumeID.String(), "assets/" + resumeID.String() + "/photo-0123456789abcdef0123456789abcdef.jpg"},
		{"uppercase hex", resumeID.String(), "resumes/" + resumeID.String() + "/photo-0123456789ABCDEF0123456789ABCDEF.jpg"},
		{"short hex", resumeID.String(), "resumes/" + resumeID.String() + "/photo-0123456789abcdef.jpg"},
		{"webp extension", resumeID.String(), "resumes/" + resumeID.String() + "/photo-0123456789abcdef0123456789abcdef.webp"},
		{"traversal", resumeID.String(), "resumes/" + resumeID.String() + "/../photo-0123456789abcdef0123456789abcdef.jpg"},
	}
	for _, bad := range badKeys {
		t.Run(bad.name, func(t *testing.T) {
			err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
				return insertMediaDeletionJob(ctx, sp, bad.resumeID, bad.key)
			})
			requireConstraintViolation(t, err, "media_deletion_jobs_key_matches_resume_check")
		})
	}

	t.Run("negative attempt count rejected", func(t *testing.T) {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			_, err := sp.Exec(ctx, `UPDATE media_deletion_jobs SET attempt_count = -1 WHERE object_key = $1`, goodKey)
			return err
		})
		requireConstraintViolation(t, err, "media_deletion_jobs_attempt_count_nonnegative_check")
	})

	t.Run("no cascade from the owning account", func(t *testing.T) {
		if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Fatalf("delete owning user: %v", err)
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM media_deletion_jobs WHERE object_key = $1`, goodKey).Scan(&n); err != nil {
			t.Fatalf("count deletion jobs: %v", err)
		}
		if n != 1 {
			t.Errorf("deletion job rows after account deletion = %d, want 1 (the ledger must survive)", n)
		}
	})
}

func TestMediaDeletionJobsNextAttemptIndexIsDueOrderedAndPartial(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	var indexdef string
	if err := tx.QueryRow(ctx, `
		SELECT indexdef FROM pg_indexes
		WHERE tablename = 'media_deletion_jobs' AND indexname = 'media_deletion_jobs_next_attempt_idx'
	`).Scan(&indexdef); err != nil {
		t.Fatalf("read due-order index definition: %v", err)
	}
	if !strings.Contains(indexdef, "(next_attempt_at, id)") || !strings.Contains(indexdef, "completed_at IS NULL") {
		t.Errorf("due-order index definition = %q, want columns (next_attempt_at, id) with a completed_at IS NULL predicate", indexdef)
	}
}

func TestIdempotencyUsageConstraints(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userID := createTestUser(ctx, t, tx)
	if _, err := tx.Exec(ctx, `INSERT INTO idempotency_usage (user_id, retained_records, stored_bytes) VALUES ($1, 0, 0)`, userID); err != nil {
		t.Fatalf("insert usage row: %v", err)
	}

	t.Run("negative retained_records rejected", func(t *testing.T) {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			_, err := sp.Exec(ctx, `UPDATE idempotency_usage SET retained_records = -1 WHERE user_id = $1`, userID)
			return err
		})
		requireConstraintViolation(t, err, "idempotency_usage_retained_records_nonnegative_check")
	})
	t.Run("negative stored_bytes rejected", func(t *testing.T) {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			_, err := sp.Exec(ctx, `UPDATE idempotency_usage SET stored_bytes = -1 WHERE user_id = $1`, userID)
			return err
		})
		requireConstraintViolation(t, err, "idempotency_usage_stored_bytes_nonnegative_check")
	})
}

func TestIdempotencyRecordsResponseHeadersMustBeObject(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userID := createTestUser(ctx, t, tx)

	for _, headers := range []string{`[]`, `"x"`, `null`, `1`} {
		t.Run(headers, func(t *testing.T) {
			err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
				_, err := sp.Exec(ctx, `
					INSERT INTO idempotency_records
					    (user_id, route, idempotency_key, request_hash, response_status, response_body, response_headers, expires_at)
					VALUES ($1, 'op-bad-headers', uuidv7(), '\x00'::bytea, 200, '{}'::jsonb, $2::jsonb, $3)
				`, userID, headers, idempotencyRecordExpiresAt)
				return err
			})
			requireConstraintViolation(t, err, "idempotency_records_response_headers_object_check")
		})
	}
}
