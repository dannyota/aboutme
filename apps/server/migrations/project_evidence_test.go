package migrations_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/migrations"
)

const lastVersionWithoutProjectEvidence = 20

type evidenceRow struct {
	project        uuid.UUID
	position       int
	kind           string
	repositoryID   int64
	itemID         int64
	pullNumber     *int64
	releaseTag     *string
	url            string
	repositoryName string
	title          string
	checkedBinding int64
	ruleMet        bool
	flagReason     *string
}

func repositoryEvidence(project uuid.UUID, position int) evidenceRow {
	return evidenceRow{project: project, position: position, kind: "repository", repositoryID: 101, itemID: 101,
		url: "https://github.com/octocat/hello", repositoryName: "octocat/hello", title: "octocat/hello", checkedBinding: 1, ruleMet: true}
}

func insertEvidence(ctx context.Context, db sqlExecer, row evidenceRow) error {
	_, err := db.Exec(ctx, `
		INSERT INTO project_evidence (project_id, position, kind, repository_id, item_id, pull_number, release_tag, url,
			repository_name, title, item_date, checked_at, checked_binding, rule_met, flag_reason)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now(), now(), $11, $12, $13)
	`, row.project, row.position, row.kind, row.repositoryID, row.itemID, row.pullNumber, row.releaseTag, row.url,
		row.repositoryName, row.title, row.checkedBinding, row.ruleMet, row.flagReason)
	return err
}

func createEvidenceProject(ctx context.Context, t *testing.T, db sqlExecer, userID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRow(ctx, `INSERT INTO projects (user_id, slug, document) VALUES ($1, $2, '{}') RETURNING id`,
		userID, "p-"+strings.ReplaceAll(uuid.NewString()[:8], "-", "")).Scan(&id); err != nil {
		t.Fatalf("create project: %v", err)
	}
	return id
}

func TestProjectEvidenceMigrationConstraints(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userID := createTestUser(ctx, t, tx)
	project := createEvidenceProject(ctx, t, tx, userID)
	number, tag := int64(42), "v1.0.0"
	gone, failed, unknown := "gone", "rule_failed", "stale"

	pull := evidenceRow{project: project, position: 1, kind: "pullRequest", repositoryID: 101, itemID: 9001, pullNumber: &number,
		url: "https://github.com/octocat/hello/pull/42", repositoryName: "octocat/hello", title: "Fix the thing", checkedBinding: 1, ruleMet: true}
	release := evidenceRow{project: project, position: 2, kind: "release", repositoryID: 101, itemID: 7001, releaseTag: &tag,
		url: "https://github.com/octocat/hello/releases/tag/v1.0.0", repositoryName: "octocat/hello", title: "v1.0.0", checkedBinding: 2, ruleMet: false, flagReason: &gone}
	for _, row := range []evidenceRow{repositoryEvidence(project, 0), pull, release} {
		if err := insertEvidence(ctx, tx, row); err != nil {
			t.Fatalf("insert %s evidence: %v", row.kind, err)
		}
	}

	mutate := func(base evidenceRow, change func(*evidenceRow)) evidenceRow {
		base.position = 9
		change(&base)
		return base
	}
	zero, other := int64(0), "v2"
	for _, tc := range []struct {
		name, constraint string
		row              evidenceRow
	}{
		{"unknown kind", "project_evidence_kind_check", mutate(repositoryEvidence(project, 0), func(r *evidenceRow) { r.kind = "commit"; r.itemID = 5 })},
		{"repository item that is not the repository", "project_evidence_kind_fields_check", mutate(repositoryEvidence(project, 0), func(r *evidenceRow) { r.itemID = 102 })},
		{"repository with a pull number", "project_evidence_kind_fields_check", mutate(repositoryEvidence(project, 0), func(r *evidenceRow) { r.itemID = 103; r.repositoryID = 103; r.pullNumber = &number })},
		{"pull request without a number", "project_evidence_kind_fields_check", mutate(pull, func(r *evidenceRow) { r.itemID = 9002; r.pullNumber = nil })},
		{"pull request with a zero number", "project_evidence_kind_fields_check", mutate(pull, func(r *evidenceRow) { r.itemID = 9003; r.pullNumber = &zero })},
		{"pull request with a tag", "project_evidence_kind_fields_check", mutate(pull, func(r *evidenceRow) { r.itemID = 9004; r.releaseTag = &tag })},
		{"release without a tag", "project_evidence_kind_fields_check", mutate(release, func(r *evidenceRow) { r.itemID = 7002; r.releaseTag = nil })},
		{"release with a pull number", "project_evidence_kind_fields_check", mutate(release, func(r *evidenceRow) { r.itemID = 7003; r.pullNumber = &number })},
		{"empty tag", "project_evidence_release_tag_check", mutate(release, func(r *evidenceRow) { empty := ""; r.itemID = 7004; r.releaseTag = &empty })},
		{"zero repository id", "project_evidence_ids_check", mutate(pull, func(r *evidenceRow) { r.itemID = 9005; r.repositoryID = 0 })},
		{"http URL", "project_evidence_url_check", mutate(repositoryEvidence(project, 0), func(r *evidenceRow) { r.itemID, r.repositoryID, r.url = 104, 104, "http://github.com/octocat/hello" })},
		{"another host", "project_evidence_url_check", mutate(repositoryEvidence(project, 0), func(r *evidenceRow) {
			r.itemID, r.repositoryID, r.url = 105, 105, "https://github.com.evil.example/octocat/hello"
		})},
		{"URL over 300 characters", "project_evidence_url_check", mutate(repositoryEvidence(project, 0), func(r *evidenceRow) {
			r.itemID, r.repositoryID, r.url = 106, 106, "https://github.com/octocat/"+strings.Repeat("a", 300-len("https://github.com/octocat/")+1)
		})},
		{"pull request URL on a repository", "project_evidence_url_check", mutate(repositoryEvidence(project, 0), func(r *evidenceRow) {
			r.itemID, r.repositoryID, r.url = 107, 107, "https://github.com/octocat/hello/pull/1"
		})},
		{"repository URL on a release", "project_evidence_url_check", mutate(release, func(r *evidenceRow) { r.itemID, r.releaseTag, r.url = 7005, &other, "https://github.com/octocat/hello" })},
		{"URL with a query", "project_evidence_url_check", mutate(pull, func(r *evidenceRow) { r.itemID, r.url = 9006, "https://github.com/octocat/hello/pull/42?x=1" })},
		{"empty title", "project_evidence_title_check", mutate(pull, func(r *evidenceRow) { r.itemID, r.title = 9007, "" })},
		{"title over 200 characters", "project_evidence_title_check", mutate(pull, func(r *evidenceRow) { r.itemID, r.title = 9008, strings.Repeat("é", 201) })},
		{"repository name over 200 characters", "project_evidence_repository_name_check", mutate(pull, func(r *evidenceRow) { r.itemID, r.repositoryName = 9009, strings.Repeat("a", 201) })},
		{"unknown flag", "project_evidence_flag_reason_check", mutate(pull, func(r *evidenceRow) { r.itemID, r.ruleMet, r.flagReason = 9010, false, &unknown })},
		{"met rule with a flag", "project_evidence_rule_flag_check", mutate(pull, func(r *evidenceRow) { r.itemID, r.flagReason = 9011, &failed })},
		{"failed rule without a flag", "project_evidence_rule_flag_check", mutate(pull, func(r *evidenceRow) { r.itemID, r.ruleMet = 9012, false })},
		{"zero binding", "project_evidence_checked_binding_check", mutate(pull, func(r *evidenceRow) { r.itemID, r.checkedBinding = 9013, 0 })},
		{"negative position", "project_evidence_position_check", mutate(pull, func(r *evidenceRow) { r.itemID, r.position = 9014, -1 })},
		{"the same item twice", "project_evidence_item_key", mutate(pull, func(r *evidenceRow) {})},
		{"the same position twice", "project_evidence_position_key", mutate(pull, func(r *evidenceRow) { r.itemID, r.position = 9015, 1 })},
	} {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error { return insertEvidence(ctx, sp, tc.row) })
		if err == nil {
			t.Errorf("%s: insert succeeded, want %s", tc.name, tc.constraint)
			continue
		}
		requirePGError(t, err, tc.constraint)
	}

	if _, err := tx.Exec(ctx, `UPDATE project_evidence SET position = 2 - position WHERE project_id = $1`, project); err != nil {
		t.Fatalf("reorder in one statement: %v", err)
	}
	second := createEvidenceProject(ctx, t, tx, userID)
	if err := insertEvidence(ctx, tx, repositoryEvidence(second, 0)); err != nil {
		t.Fatalf("the same item on another project: %v", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM projects WHERE id = $1`, project); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	var left int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_evidence WHERE project_id = $1`, project).Scan(&left); err != nil || left != 0 {
		t.Fatalf("evidence left after deleting its project = %d, %v; want 0", left, err)
	}
}

func TestProjectEvidenceMigrationBindingAndTransactionProject(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userID := createTestUser(ctx, t, tx)
	if _, err := tx.Exec(ctx, `INSERT INTO github_connections (user_id, github_user_id, login, connected_at, generation) VALUES ($1, 583231, 'octocat', now(), 1)`, userID); err != nil {
		t.Fatalf("insert connection: %v", err)
	}
	var binding int64
	if err := tx.QueryRow(ctx, `SELECT binding FROM github_connections WHERE user_id = $1`, userID).Scan(&binding); err != nil || binding != 1 {
		t.Fatalf("new connection binding = %d, %v; want 1", binding, err)
	}
	if _, err := tx.Exec(ctx, `UPDATE github_connections SET generation = generation + 1 WHERE user_id = $1`, userID); err != nil {
		t.Fatalf("advance generation alone: %v", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE github_connections SET binding = binding + 2, generation = generation + 1 WHERE user_id = $1`, userID); err != nil {
		t.Fatalf("advance binding: %v", err)
	}
	for _, tc := range []struct{ name, statement, constraint string }{
		{"binding shrinks", `UPDATE github_connections SET binding = binding - 1, generation = generation + 1 WHERE user_id = $1`, "github_connections_binding_never_shrinks"},
		{"zero binding", `INSERT INTO github_connections (user_id, generation, binding) VALUES ($1, 1, 0)`, "github_connections_binding_check"},
	} {
		target := userID
		if strings.HasPrefix(tc.statement, "INSERT") {
			target = createTestUser(ctx, t, tx)
		}
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			_, execErr := sp.Exec(ctx, tc.statement, target)
			return execErr
		})
		if err == nil {
			t.Errorf("%s: statement succeeded, want %s", tc.name, tc.constraint)
			continue
		}
		requirePGError(t, err, tc.constraint)
	}

	project := uuid.New()
	insert := func(db sqlExecer, purpose string, generation *int64) error {
		_, err := db.Exec(ctx, `
			INSERT INTO oauth_transactions (handle_hash, provider, purpose, linking_user_id, state, pkce_verifier, redirect_uri, expires_at, evidence_generation, evidence_project_id)
			VALUES ($1, 'github', $2, $3, 'state', 'verifier', 'https://aboutme.example/api/v1/github-evidence/callback', now() + interval '10 minutes', $4, $5)
		`, []byte(uuid.NewString()), purpose, userID, generation, project)
		return err
	}
	one := int64(1)
	if err := insert(tx, "evidence", &one); err != nil {
		t.Fatalf("evidence transaction with a project that has no row: %v", err)
	}
	err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error { return insert(sp, "link", nil) })
	requirePGError(t, err, "oauth_transactions_evidence_project_check")
}

func TestProjectEvidenceMigrationUpDownUp(t *testing.T) {
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
	if _, err = provider.UpTo(ctx, lastVersionWithoutProjectEvidence); err != nil {
		t.Fatalf("UpTo(%d) error: %v", lastVersionWithoutProjectEvidence, err)
	}
	userID := createTestUser(ctx, t, conn)
	if _, err = conn.Exec(ctx, `INSERT INTO github_connections (user_id, github_user_id, login, connected_at, generation) VALUES ($1, 1, 'octocat', $2, 4)`, userID, time.Now()); err != nil {
		t.Fatalf("insert connection before the migration: %v", err)
	}
	if _, err = provider.Up(ctx); err != nil {
		t.Fatalf("Up() error: %v", err)
	}
	var binding int64
	if err = conn.QueryRow(ctx, `SELECT binding FROM github_connections WHERE user_id = $1`, userID).Scan(&binding); err != nil || binding != 1 {
		t.Fatalf("existing connection binding = %d, %v; want 1", binding, err)
	}
	project := createEvidenceProject(ctx, t, conn, userID)
	if err = insertEvidence(ctx, conn, repositoryEvidence(project, 0)); err != nil {
		t.Fatalf("insert evidence: %v", err)
	}
	if _, err = provider.DownTo(ctx, lastVersionWithoutProjectEvidence); err != nil {
		t.Fatalf("DownTo(%d) error: %v", lastVersionWithoutProjectEvidence, err)
	}
	var tables, columns int
	if err = conn.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM pg_class WHERE relname = 'project_evidence' AND relnamespace = 'public'::regnamespace),
			(SELECT count(*) FROM information_schema.columns
			 WHERE (table_name = 'github_connections' AND column_name = 'binding')
			    OR (table_name = 'oauth_transactions' AND column_name = 'evidence_project_id'))
	`).Scan(&tables, &columns); err != nil || tables != 0 || columns != 0 {
		t.Fatalf("after down: tables=%d columns=%d err=%v; want 0 and 0", tables, columns, err)
	}
	if _, err = conn.Exec(ctx, `UPDATE github_connections SET generation = generation + 1 WHERE user_id = $1`, userID); err != nil {
		t.Fatalf("after down, the connection row no longer updates: %v", err)
	}
	if _, err = provider.Up(ctx); err != nil {
		t.Fatalf("Up() after down error: %v", err)
	}
	if err = insertEvidence(ctx, conn, repositoryEvidence(project, 0)); err != nil {
		t.Fatalf("after up again, insert evidence: %v", err)
	}
}
