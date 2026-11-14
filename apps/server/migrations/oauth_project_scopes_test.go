package migrations_test

import (
	"context"
	"maps"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/migrations"
)

const oauthScopesWithProjects = "resumes:read resumes:write projects:read projects:write"

func insertOAuthGrantWithScopes(ctx context.Context, db sqlExecer, userID, clientID uuid.UUID, scopes string) (uuid.UUID, error) {
	var id uuid.UUID
	err := db.QueryRow(ctx, `
		INSERT INTO oauth_grants (user_id, client_id, scopes, created_at)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, userID, clientID, scopes, oauthNow).Scan(&id)
	return id, err
}

func insertOAuthCodeWithScopes(ctx context.Context, db sqlExecer, userID, clientID, grantID uuid.UUID, scopes string) (uuid.UUID, error) {
	var id uuid.UUID
	err := db.QueryRow(ctx, `
		INSERT INTO oauth_authorization_codes (
			code_digest, client_id, user_id, scopes, code_challenge, redirect_uri,
			created_at, expires_at, grant_id, auth_epoch
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,0) RETURNING id
	`, validOAuthDigest(), clientID, userID, scopes, oauthChallenge, oauthRedirectURI,
		oauthNow, oauthNow.Add(oauthCodeTTL), grantID).Scan(&id)
	return id, err
}

func countProjectScopeRows(ctx context.Context, t *testing.T, conn *pgx.Conn) (grants, codes int) {
	t.Helper()
	if err := conn.QueryRow(ctx, `
		SELECT
			(SELECT count(*) FROM oauth_grants WHERE scopes LIKE '%projects:%'),
			(SELECT count(*) FROM oauth_authorization_codes WHERE scopes LIKE '%projects:%')
	`).Scan(&grants, &codes); err != nil {
		t.Fatalf("count project-scope rows: %v", err)
	}
	return grants, codes
}

func oauthScopeConstraintDefinitions(ctx context.Context, t *testing.T, conn *pgx.Conn) map[string]string {
	t.Helper()
	rows, err := conn.Query(ctx, `
		SELECT conname, pg_get_constraintdef(oid) FROM pg_constraint
		WHERE conname IN ('oauth_grants_scopes_check', 'oauth_authorization_codes_scopes_check')`)
	if err != nil {
		t.Fatalf("read scope constraints: %v", err)
	}
	defer rows.Close()
	definitions := map[string]string{}
	for rows.Next() {
		var name, definition string
		if err = rows.Scan(&name, &definition); err != nil {
			t.Fatalf("scan scope constraint: %v", err)
		}
		definitions[name] = definition
	}
	if err = rows.Err(); err != nil {
		t.Fatalf("read scope constraints: %v", err)
	}
	if len(definitions) != 2 {
		t.Fatalf("scope constraints = %v, want both", definitions)
	}
	return definitions
}

func TestOAuthProjectScopesMigrationUpDownUp(t *testing.T) {
	t.Parallel()
	const lastVersionWithoutProjectScopes = 18
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

	if _, err = provider.UpTo(ctx, lastVersionWithoutProjectScopes); err != nil {
		t.Fatalf("UpTo(%d) error: %v", lastVersionWithoutProjectScopes, err)
	}
	original := oauthScopeConstraintDefinitions(ctx, t, conn)
	userID := createTestUser(ctx, t, conn)
	resumeClient := insertOAuthClient(ctx, t, conn)
	resumeGrant := insertOAuthGrant(ctx, t, conn, userID, resumeClient)
	if _, err = provider.Up(ctx); err != nil {
		t.Fatalf("Up() error: %v", err)
	}

	projectClient := insertOAuthClient(ctx, t, conn)
	projectGrant, err := insertOAuthGrantWithScopes(ctx, conn, userID, projectClient, oauthScopesWithProjects)
	if err != nil {
		t.Fatalf("after up, oauth_grants rejected a grant holding %q: %v; want the migration that allows the project scope strings", oauthScopesWithProjects, err)
	}
	projectCode, err := insertOAuthCodeWithScopes(ctx, conn, userID, projectClient, projectGrant, oauthScopesWithProjects)
	if err != nil {
		t.Fatalf("after up, oauth_authorization_codes rejected a code holding %q: %v", oauthScopesWithProjects, err)
	}
	unusedClient := insertOAuthClient(ctx, t, conn)
	for _, scopes := range []string{"projects:admin", "resumes:read projects:admin", "resumes:read resumes:write projects:read projects:write projects:read", ""} {
		_, grantErr := insertOAuthGrantWithScopes(ctx, conn, userID, unusedClient, scopes)
		requirePGError(t, grantErr, "oauth_grants_scopes_check")
		_, codeErr := insertOAuthCodeWithScopes(ctx, conn, userID, resumeClient, resumeGrant, scopes)
		requirePGError(t, codeErr, "oauth_authorization_codes_scopes_check")
	}

	requireRefusedDown := func(stage string, wantGrants, wantCodes int) {
		t.Helper()
		if _, downErr := provider.DownTo(ctx, lastVersionWithoutProjectScopes); downErr == nil {
			t.Fatalf("%s: DownTo(%d) succeeded, want the down step to refuse while a project-scope grant or code exists", stage, lastVersionWithoutProjectScopes)
		}
		version, versionErr := provider.GetDBVersion(ctx)
		if versionErr != nil || version <= lastVersionWithoutProjectScopes {
			t.Fatalf("%s: version after refused down = %d, %v; want above %d", stage, version, versionErr, lastVersionWithoutProjectScopes)
		}
		if grants, codes := countProjectScopeRows(ctx, t, conn); grants != wantGrants || codes != wantCodes {
			t.Fatalf("%s: refused down left %d grants and %d codes holding project scopes, want %d and %d", stage, grants, codes, wantGrants, wantCodes)
		}
	}
	requireRefusedDown("grant and code", 1, 1)

	if _, err = conn.Exec(ctx, `DELETE FROM oauth_authorization_codes WHERE id = $1`, projectCode); err != nil {
		t.Fatalf("delete project-scope code: %v", err)
	}
	requireRefusedDown("grant only", 1, 0)

	if _, err = conn.Exec(ctx, `UPDATE oauth_grants SET scopes = $2 WHERE id = $1`, projectGrant, oauthScopesBoth); err != nil {
		t.Fatalf("narrow project-scope grant: %v", err)
	}
	codeOnly, err := insertOAuthCodeWithScopes(ctx, conn, userID, resumeClient, resumeGrant, oauthScopesWithProjects)
	if err != nil {
		t.Fatalf("insert project-scope code on a resume grant: %v", err)
	}
	requireRefusedDown("code only", 0, 1)

	if _, err = conn.Exec(ctx, `DELETE FROM oauth_authorization_codes WHERE id = $1`, codeOnly); err != nil {
		t.Fatalf("delete project-scope code: %v", err)
	}
	if _, err = provider.DownTo(ctx, lastVersionWithoutProjectScopes); err != nil {
		t.Fatalf("DownTo(%d) with no project-scope rows error: %v", lastVersionWithoutProjectScopes, err)
	}
	if version, versionErr := provider.GetDBVersion(ctx); versionErr != nil || version != lastVersionWithoutProjectScopes {
		t.Fatalf("version after down = %d, %v; want %d", version, versionErr, lastVersionWithoutProjectScopes)
	}
	if restored := oauthScopeConstraintDefinitions(ctx, t, conn); !maps.Equal(restored, original) {
		t.Fatalf("scope constraints after down = %v, want the version %d definitions %v", restored, lastVersionWithoutProjectScopes, original)
	}
	_, err = insertOAuthGrantWithScopes(ctx, conn, userID, unusedClient, oauthScopesWithProjects)
	requirePGError(t, err, "oauth_grants_scopes_check")
	_, err = insertOAuthCodeWithScopes(ctx, conn, userID, resumeClient, resumeGrant, oauthScopesWithProjects)
	requirePGError(t, err, "oauth_authorization_codes_scopes_check")
	var kept int
	if err = conn.QueryRow(ctx, `SELECT count(*) FROM oauth_grants WHERE id = ANY($1) AND scopes = $2`,
		[]uuid.UUID{resumeGrant, projectGrant}, oauthScopesBoth).Scan(&kept); err != nil || kept != 2 {
		t.Fatalf("resume grants kept across down = %d, %v; want 2", kept, err)
	}

	if _, err = provider.Up(ctx); err != nil {
		t.Fatalf("Up() after down error: %v", err)
	}
	if _, err = insertOAuthGrantWithScopes(ctx, conn, userID, unusedClient, oauthScopesWithProjects); err != nil {
		t.Fatalf("after up again, oauth_grants rejected a grant holding %q: %v", oauthScopesWithProjects, err)
	}
}
