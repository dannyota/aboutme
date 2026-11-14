package oauthsrv

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

func storedScopeCandidates() []string {
	var candidates []string
	var permute func(prefix, rest []string)
	permute = func(prefix, rest []string) {
		if len(prefix) > 0 {
			candidates = append(candidates, strings.Join(prefix, " "))
		}
		for i := range rest {
			permute(append(slices.Clone(prefix), rest[i]), slices.Concat(rest[:i:i], rest[i+1:]))
		}
	}
	permute(nil, []string{"resumes:read", "resumes:write", "projects:read", "projects:write"})
	return append(candidates,
		"resumes:read resumes:read",
		"projects:read projects:read",
		"projects:write projects:write",
		"resumes:read resumes:write projects:read projects:write projects:write",
		"resumes:read resumes:write resumes:read",
		"projects:admin",
		"resumes:read projects:admin",
		"resumes:admin",
		"openid",
		"Projects:Read",
		"projects:rea",
		"projects:reads",
		"",
		" ",
		" resumes:read",
		"resumes:read ",
		"resumes:read  resumes:write",
		"resumes:read resumes:write projects:read  projects:write",
		"resumes:read\tresumes:write",
		"resumes:read,projects:read",
	)
}

func insertAccepted(ctx context.Context, t *testing.T, tx pgx.Tx, constraint, query string, args ...any) bool {
	t.Helper()
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("open savepoint: %v", err)
	}
	_, insertErr := savepoint.Exec(ctx, query, args...)
	if rollbackErr := savepoint.Rollback(ctx); rollbackErr != nil {
		t.Fatalf("roll back savepoint: %v", rollbackErr)
	}
	if insertErr == nil {
		return true
	}
	var pgErr *pgconn.PgError
	if !errors.As(insertErr, &pgErr) || pgErr.ConstraintName != constraint {
		t.Fatalf("insert error = %v, want success or a %s violation", insertErr, constraint)
	}
	return false
}

func TestParseStoredScopesAcceptsExactlyWhatTheDatabaseStores(t *testing.T) {
	ctx := context.Background()
	pool, err := store.NewPool(ctx, testutil.RequireMigratedTestDatabaseURL(t))
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	t.Cleanup(func() {
		if rollbackErr := tx.Rollback(context.Background()); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("Rollback: %v", rollbackErr)
		}
	})
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	q := store.New(tx)
	user, err := q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.test", Name: "Scope parity"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	client, err := q.CreateOAuthClient(ctx, store.CreateOAuthClientParams{
		ClientName: "Scope parity", RedirectURIs: json.RawMessage(`["https://agent.example/callback"]`), CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateOAuthClient: %v", err)
	}
	grant, err := q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: user.ID, ClientID: client.ID, Scopes: "resumes:read", CreatedAt: now})
	if err != nil {
		t.Fatalf("UpsertOAuthGrant: %v", err)
	}

	accepted := 0
	for _, candidate := range storedScopeCandidates() {
		grantStored := insertAccepted(ctx, t, tx, "oauth_grants_scopes_check", `
			INSERT INTO oauth_grants (user_id, client_id, scopes, created_at, revoked_at)
			VALUES ($1, $2, $3, $4, $4)`, user.ID, client.ID, candidate, now)
		codeStored := insertAccepted(ctx, t, tx, "oauth_authorization_codes_scopes_check", `
			INSERT INTO oauth_authorization_codes (
				code_digest, client_id, user_id, scopes, code_challenge, redirect_uri,
				created_at, expires_at, grant_id, auth_epoch
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 0)`,
			[]byte(strings.Repeat("c", 32)), client.ID, user.ID, candidate, strings.Repeat("A", 43),
			"https://agent.example/callback", now, now.Add(60*time.Second), grant.ID)
		scopes, readErr := ParseStoredScopes(candidate)
		readerAccepts := readErr == nil
		if readerAccepts != grantStored || readerAccepts != codeStored {
			t.Errorf("ParseStoredScopes(%q) accepts = %t, oauth_grants stores = %t, oauth_authorization_codes stores = %t", candidate, readerAccepts, grantStored, codeStored)
		}
		if readerAccepts {
			accepted++
			if scopes.String() != candidate {
				t.Errorf("ParseStoredScopes(%q) = %q, want the stored string unchanged", candidate, scopes.String())
			}
		}
	}
	if accepted != 15 {
		t.Errorf("accepted %d stored scope strings, want the 15 non-empty subsets in canonical order", accepted)
	}
}
