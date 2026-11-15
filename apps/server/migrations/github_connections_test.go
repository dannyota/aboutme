package migrations_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/migrations"
)

const lastVersionWithoutGitHubConnections = 19

func insertEvidenceTransaction(ctx context.Context, db sqlExecer, userID *uuid.UUID, provider, purpose string, generation *int64) error {
	_, err := db.Exec(ctx, `
		INSERT INTO oauth_transactions (handle_hash, provider, purpose, linking_user_id, state, pkce_verifier, redirect_uri, expires_at, evidence_generation)
		VALUES ($1, $2, $3, $4, 'state', 'verifier', 'https://aboutme.example/api/v1/github-evidence/callback', now() + interval '10 minutes', $5)
	`, []byte(uuid.NewString()), provider, purpose, userID, generation)
	return err
}

func TestGitHubConnectionsMigrationConstraints(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userID := createTestUser(ctx, t, tx)
	zero, seven, negative := int64(0), int64(7), int64(-1)

	if err := insertEvidenceTransaction(ctx, tx, &userID, "github", "evidence", &seven); err != nil {
		t.Fatalf("insert evidence transaction: %v", err)
	}
	if err := insertEvidenceTransaction(ctx, tx, &userID, "github", "evidence", &zero); err != nil {
		t.Fatalf("insert evidence transaction at generation 0: %v", err)
	}
	for _, tc := range []struct {
		name, provider, purpose, constraint string
		user                                *uuid.UUID
		generation                          *int64
	}{
		{"evidence without a user", "github", "evidence", "oauth_transactions_link_needs_user", nil, &seven},
		{"evidence without a generation", "github", "evidence", "oauth_transactions_evidence_check", &userID, nil},
		{"evidence with a negative generation", "github", "evidence", "oauth_transactions_evidence_check", &userID, &negative},
		{"evidence through google", "google", "evidence", "oauth_transactions_evidence_check", &userID, &seven},
		{"link carrying a generation", "github", "link", "oauth_transactions_evidence_check", &userID, &seven},
		{"unknown purpose", "github", "connect", "oauth_transactions_link_needs_user", &userID, nil},
	} {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return insertEvidenceTransaction(ctx, sp, tc.user, tc.provider, tc.purpose, tc.generation)
		})
		if err == nil {
			t.Errorf("%s: insert succeeded, want %s", tc.name, tc.constraint)
			continue
		}
		requirePGError(t, err, tc.constraint)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO github_connections (user_id, github_user_id, login, connected_at, generation) VALUES ($1, 583231, 'octocat', now(), 1)`, userID); err != nil {
		t.Fatalf("insert connection: %v", err)
	}
	for _, tc := range []struct{ name, statement, constraint string }{
		{"second row for the owner", `INSERT INTO github_connections (user_id, generation) VALUES ($1, 5)`, "github_connections_pkey"},
		{"generation repeats", `UPDATE github_connections SET generation = generation WHERE user_id = $1`, "github_connections_generation_grows"},
		{"generation shrinks", `UPDATE github_connections SET generation = generation - 1 WHERE user_id = $1`, "github_connections_generation_grows"},
		{"partial fields", `UPDATE github_connections SET login = NULL, generation = generation + 1 WHERE user_id = $1`, "github_connections_fields_check"},
		{"empty login", `UPDATE github_connections SET login = '', generation = generation + 1 WHERE user_id = $1`, "github_connections_login_check"},
		{"zero github id", `UPDATE github_connections SET github_user_id = 0, generation = generation + 1 WHERE user_id = $1`, "github_connections_github_user_id_check"},
	} {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			_, execErr := sp.Exec(ctx, tc.statement, userID)
			return execErr
		})
		if err == nil {
			t.Errorf("%s: statement succeeded, want %s", tc.name, tc.constraint)
			continue
		}
		requirePGError(t, err, tc.constraint)
	}
	other := createTestUser(ctx, t, tx)
	err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
		_, execErr := sp.Exec(ctx, `INSERT INTO github_connections (user_id, generation) VALUES ($1, 0)`, other)
		return execErr
	})
	requirePGError(t, err, "github_connections_generation_check")
	if _, err := tx.Exec(ctx, `INSERT INTO github_connections (user_id, github_user_id, login, connected_at, generation) VALUES ($1, 583231, 'octocat', now(), 1)`, other); err != nil {
		t.Fatalf("the same GitHub account on a second aboutme account: %v", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE github_connections SET github_user_id = NULL, login = NULL, connected_at = NULL, generation = generation + 1 WHERE user_id = $1`, userID); err != nil {
		t.Fatalf("clear connection: %v", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	var left int
	if err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM github_connections WHERE user_id = $1) + (SELECT count(*) FROM oauth_transactions WHERE linking_user_id = $1)`, userID).Scan(&left); err != nil || left != 0 {
		t.Fatalf("rows left after account deletion = %d, %v; want 0", left, err)
	}
}

func TestGitHubConnectionsMigrationUpDownUp(t *testing.T) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*harnessTimeout)
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
	if _, err = provider.Up(ctx); err != nil {
		t.Fatalf("Up() error: %v", err)
	}
	userID := createTestUser(ctx, t, conn)
	generation := int64(3)
	if err = insertEvidenceTransaction(ctx, conn, &userID, "github", "evidence", &generation); err != nil {
		t.Fatalf("insert evidence transaction: %v", err)
	}
	if _, err = conn.Exec(ctx, `INSERT INTO github_connections (user_id, github_user_id, login, connected_at, generation) VALUES ($1, 1, 'octocat', $2, 3)`, userID, time.Now()); err != nil {
		t.Fatalf("insert connection: %v", err)
	}
	if _, err = provider.DownTo(ctx, lastVersionWithoutGitHubConnections); err != nil {
		t.Fatalf("DownTo(%d) error: %v", lastVersionWithoutGitHubConnections, err)
	}
	var tables, columns, evidence int
	if err = conn.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM pg_class WHERE relname = 'github_connections' AND relnamespace = 'public'::regnamespace),
			(SELECT count(*) FROM information_schema.columns WHERE table_name = 'oauth_transactions' AND column_name = 'evidence_generation'),
			(SELECT count(*) FROM oauth_transactions WHERE purpose = 'evidence')
	`).Scan(&tables, &columns, &evidence); err != nil || tables != 0 || columns != 0 || evidence != 0 {
		t.Fatalf("after down: tables=%d columns=%d evidence rows=%d err=%v; want all 0", tables, columns, evidence, err)
	}
	if _, err = conn.Exec(ctx, `
		INSERT INTO oauth_transactions (handle_hash, provider, purpose, linking_user_id, state, pkce_verifier, redirect_uri, expires_at)
		VALUES ($1, 'github', 'evidence', $2, 'state', 'verifier', 'https://aboutme.example/x', now())
	`, []byte(uuid.NewString()), userID); err == nil {
		t.Fatal("after down, oauth_transactions accepted purpose evidence")
	}
	if _, err = provider.Up(ctx); err != nil {
		t.Fatalf("Up() after down error: %v", err)
	}
	if err = insertEvidenceTransaction(ctx, conn, &userID, "github", "evidence", &generation); err != nil {
		t.Fatalf("after up again, insert evidence transaction: %v", err)
	}
}
