// Sign in to view (docs/design/viewer-analytics/sign-in-to-view.md; ADR
// 0022; AC-VIEW-001, AC-VIEW-007): the resumes switch and pass epoch, and
// the oauth_transactions "view" purpose bound to a resume.
package migrations_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

func insertViewTransaction(ctx context.Context, sp pgx.Tx, purpose string, resumeID *uuid.UUID, linkingUserID *uuid.UUID) error {
	_, err := sp.Exec(ctx, `
		INSERT INTO oauth_transactions (
			handle_hash, provider, purpose, linking_user_id, resume_id, state,
			pkce_verifier, redirect_uri, expires_at
		) VALUES ($1, 'google', $2, $3, $4, $5, $6, $7, $8)
	`, validOAuthDigest(), purpose, linkingUserID, resumeID, uuid.NewString(),
		"vvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvvv", oauthRedirectURI, oauthNow.Add(time.Minute))
	return err
}

func TestSignInToViewOAuthTransactionConstraints(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userID := createTestUser(ctx, t, tx)
	resumeID := insertLiveCardResume(ctx, t, tx, userID, uniqueCardSlug())
	otherUserID := createTestUser(ctx, t, tx)

	// AC-VIEW-007: resume_id is required exactly when purpose is view.
	err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
		return insertViewTransaction(ctx, sp, "view", nil, nil)
	})
	requireConstraintViolation(t, err, "oauth_transactions_view_resume_check")

	err = withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
		return insertViewTransaction(ctx, sp, "login", &resumeID, nil)
	})
	requireConstraintViolation(t, err, "oauth_transactions_view_resume_check")

	// linking_user_id must be null for purpose view.
	err = withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
		return insertViewTransaction(ctx, sp, "view", &resumeID, &otherUserID)
	})
	requireConstraintViolation(t, err, "oauth_transactions_link_needs_user")

	// A well-formed view transaction succeeds and cascades with its resume.
	if err := insertViewTransaction(ctx, tx, "view", &resumeID, nil); err != nil {
		t.Fatalf("insert well-formed view transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM resumes WHERE id = $1`, resumeID); err != nil {
		t.Fatalf("delete resume: %v", err)
	}
	var remaining int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM oauth_transactions WHERE resume_id = $1`, resumeID).Scan(&remaining); err != nil {
		t.Fatalf("count view transactions: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("view transactions after resume delete = %d, want 0 (cascade)", remaining)
	}
}

func TestSignInToViewResumeColumnsDefaultAndCheck(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userID := createTestUser(ctx, t, tx)
	resumeID := insertLiveCardResume(ctx, t, tx, userID, uniqueCardSlug())

	var signInToView bool
	var epoch int32
	if err := tx.QueryRow(ctx, `SELECT sign_in_to_view, view_pass_epoch FROM resumes WHERE id = $1`, resumeID).Scan(&signInToView, &epoch); err != nil {
		t.Fatalf("read defaults: %v", err)
	}
	if signInToView || epoch != 0 {
		t.Fatalf("sign_in_to_view=%t view_pass_epoch=%d, want false and 0", signInToView, epoch)
	}

	err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
		_, execErr := sp.Exec(ctx, `UPDATE resumes SET view_pass_epoch = -1 WHERE id = $1`, resumeID)
		return execErr
	})
	requireConstraintViolation(t, err, "resumes_view_pass_epoch_check")
}

func TestSignInToViewMigrationUpDownUp(t *testing.T) {
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
	const resumeColumns = `SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'resumes' AND column_name IN ('sign_in_to_view', 'view_pass_epoch')`
	const transactionColumn = `SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'oauth_transactions' AND column_name = 'resume_id'`

	if _, err := provider.UpTo(ctx, 9); err != nil {
		t.Fatalf("UpTo(9) error: %v", err)
	}
	if n := mustQueryInt(ctx, t, db, resumeColumns); n != 2 {
		t.Fatalf("resumes sign-in-to-view columns after up = %d, want 2", n)
	}
	if n := mustQueryInt(ctx, t, db, transactionColumn); n != 1 {
		t.Fatalf("oauth_transactions.resume_id after up = %d, want 1", n)
	}

	if _, err := provider.DownTo(ctx, 8); err != nil {
		t.Fatalf("DownTo(8) error: %v", err)
	}
	if n := mustQueryInt(ctx, t, db, resumeColumns); n != 0 {
		t.Fatalf("resumes sign-in-to-view columns after down = %d, want 0", n)
	}
	if n := mustQueryInt(ctx, t, db, transactionColumn); n != 0 {
		t.Fatalf("oauth_transactions.resume_id after down = %d, want 0", n)
	}

	if _, err := provider.UpTo(ctx, 9); err != nil {
		t.Fatalf("UpTo(9) again error: %v", err)
	}
}
