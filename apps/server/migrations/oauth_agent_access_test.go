// Phase PM task 1's constraint, cascade, and lineage tests for the four tables
// added by migration 00009 (oauth_clients, oauth_authorization_codes,
// oauth_grants, oauth_tokens). Like password_auth_test.go, every statement here
// is raw parameterized SQL against a live goose-migrated database — no
// internal/store layer — because the point is proving the database itself
// enforces every M1/M2/M3 bound, not that a Go pass happens to agree with it.
// Boundaries are exercised at limit and limit+1.
package migrations_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

// oauthNow is the fixed instant every row in this file is created at, so
// expiry-boundary cases name their offsets instead of racing wall time.
var oauthNow = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

const (
	// oauthCodeTTL is the exact M2 authorization-code lifetime the database
	// pins with an equality check.
	oauthCodeTTL = 60 * time.Second
	// oauthAccessTTL and oauthFamilyTTL are the M3 access-token lifetime and
	// refresh-family absolute lifetime the database bounds.
	oauthAccessTTL = time.Hour
	oauthFamilyTTL = 30 * 24 * time.Hour
	// oauthScopesBoth is the canonical spelling of the full closed scope set.
	oauthScopesBoth = "resumes:read resumes:write"
	// oauthChallenge is a canonical unpadded base64url S256 challenge (43
	// characters), matching oauthsrv's stored spelling.
	oauthChallenge = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	// oauthRedirectURI is a registered loopback redirect URI.
	oauthRedirectURI = "http://127.0.0.1:20090/callback"
)

// validOAuthDigest returns a fresh random 32-byte digest. Digests are globally
// unique in the schema, so a shared constant would collide across the parallel
// tests that share the one migrated database. It is deliberately NOT derived
// from any raw code or token spelling: this file never constructs raw material.
func validOAuthDigest() []byte { return []byte(uuid.NewString() + uuid.NewString())[:32] }

// insertOAuthClient inserts a minimal valid client row and returns its id,
// which is also the public client_id.
func insertOAuthClient(ctx context.Context, t *testing.T, db sqlExecer) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(ctx, `
		INSERT INTO oauth_clients (client_name, redirect_uris, created_at, last_used_at)
		VALUES ($1, $2, $3, $3) RETURNING id
	`, "Agent "+uuid.NewString(), `["`+oauthRedirectURI+`"]`, oauthNow).Scan(&id)
	if err != nil {
		t.Fatalf("insert oauth client: %v", err)
	}
	return id
}

// insertOAuthGrant inserts a live grant and returns its id.
func insertOAuthGrant(ctx context.Context, t *testing.T, db sqlExecer, userID, clientID uuid.UUID) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(ctx, `
		INSERT INTO oauth_grants (user_id, client_id, scopes, created_at)
		VALUES ($1, $2, $3, $4) RETURNING id
	`, userID, clientID, oauthScopesBoth, oauthNow).Scan(&id)
	if err != nil {
		t.Fatalf("insert oauth grant: %v", err)
	}
	return id
}

// oauthTokenRow is a full valid refresh-token row; each constraint case mutates
// exactly one field before inserting.
type oauthTokenRow struct {
	digest          []byte
	kind            string
	familyID        uuid.UUID
	rotatedFrom     *uuid.UUID
	clientID        uuid.UUID
	userID          uuid.UUID
	grantID         uuid.UUID
	createdAt       time.Time
	expiresAt       time.Time
	familyExpiresAt time.Time
	revokedAt       *time.Time
	supersededAt    *time.Time
	lastUsedAt      *time.Time
}

func defaultOAuthTokenRow(clientID, userID, grantID uuid.UUID) oauthTokenRow {
	return oauthTokenRow{
		digest:          validOAuthDigest(),
		kind:            "refresh",
		familyID:        uuid.New(),
		clientID:        clientID,
		userID:          userID,
		grantID:         grantID,
		createdAt:       oauthNow,
		expiresAt:       oauthNow.Add(oauthFamilyTTL),
		familyExpiresAt: oauthNow.Add(oauthFamilyTTL),
	}
}

func insertOAuthToken(ctx context.Context, db sqlExecer, row oauthTokenRow) error {
	_, err := db.Exec(ctx, `
		INSERT INTO oauth_tokens (
			token_digest, kind, family_id, rotated_from, client_id, user_id, grant_id,
			created_at, expires_at, family_expires_at, revoked_at, superseded_at, last_used_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
	`,
		row.digest, row.kind, row.familyID, row.rotatedFrom, row.clientID, row.userID,
		row.grantID, row.createdAt, row.expiresAt, row.familyExpiresAt, row.revokedAt,
		row.supersededAt, row.lastUsedAt,
	)
	return err
}

func insertOAuthTokenReturningID(ctx context.Context, t *testing.T, db sqlExecer, row oauthTokenRow) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.QueryRow(ctx, `
		INSERT INTO oauth_tokens (
			token_digest, kind, family_id, rotated_from, client_id, user_id, grant_id,
			created_at, expires_at, family_expires_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id
	`,
		row.digest, row.kind, row.familyID, row.rotatedFrom, row.clientID, row.userID,
		row.grantID, row.createdAt, row.expiresAt, row.familyExpiresAt,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insert oauth token: %v", err)
	}
	return id
}

// ---------------------------------------------------------------------------
// Down/up restores the exact prior schema.
// ---------------------------------------------------------------------------

func TestOAuthAgentAccessMigrationDownUp(t *testing.T) {
	t.Parallel()
	dsn := newTestDatabase(t)
	db := openTestDB(t, dsn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	provider, err := migrations.NewProvider(db, migrations.FS)
	if err != nil {
		t.Fatalf("NewProvider() error: %v", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("Up() error: %v", err)
	}
	if _, err := provider.DownTo(ctx, 8); err != nil {
		t.Fatalf("DownTo(8) error: %v", err)
	}
	var returnPathAfterDown int
	if err := db.QueryRowContext(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'oauth_transactions'
		  AND column_name = 'return_path'
	`).Scan(&returnPathAfterDown); err != nil {
		t.Fatalf("count return_path after down: %v", err)
	}
	if returnPathAfterDown != 0 {
		t.Fatalf("return_path columns after down = %d, want 0", returnPathAfterDown)
	}

	for _, table := range oauthAgentAccessTables {
		var relation *string
		if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.`+table+`')::text`).Scan(&relation); err != nil {
			t.Fatalf("probe %s after down: %v", table, err)
		}
		if relation != nil {
			t.Fatalf("%s relation after down = %q, want absent", table, *relation)
		}
	}

	if _, err := provider.UpTo(ctx, 9); err != nil {
		t.Fatalf("UpTo(9) error: %v", err)
	}
	for _, table := range oauthAgentAccessTables {
		var relation *string
		if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.`+table+`')::text`).Scan(&relation); err != nil {
			t.Fatalf("probe %s after up: %v", table, err)
		}
		if relation == nil {
			t.Fatalf("%s relation after up = absent, want present", table)
		}
	}
	var returnPathAfterUp string
	if err := db.QueryRowContext(ctx, `
		SELECT column_name FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'oauth_transactions'
		  AND column_name = 'return_path'
	`).Scan(&returnPathAfterUp); err != nil {
		t.Fatalf("return_path after up: %v", err)
	}
	if returnPathAfterUp != "return_path" {
		t.Fatalf("column after up = %q, want return_path", returnPathAfterUp)
	}
}

func TestOAuthTransactionReturnPathConstraint(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	insert := func(sp pgx.Tx, returnPath string) error {
		_, err := sp.Exec(ctx, `
			INSERT INTO oauth_transactions (
				handle_hash, provider, purpose, state, pkce_verifier,
				redirect_uri, return_path, expires_at
			) VALUES ($1, 'google', 'login', $2, $3, $4, $5, $6)
		`, validOAuthDigest(), uuid.NewString(), strings.Repeat("v", 43),
			oauthRedirectURI, returnPath, oauthNow.Add(time.Minute))
		return err
	}

	cases := []struct {
		name       string
		returnPath string
		valid      bool
	}{
		{name: "single slash path", returnPath: "/app/resumes", valid: true},
		{name: "path with query", returnPath: "/oauth/authorize?x=1", valid: true},
		{name: "empty", returnPath: ""},
		{name: "network path", returnPath: "//evil.example"},
		{name: "absolute URL", returnPath: "https://evil.example"},
		{name: "backslash", returnPath: `/\\evil.example`},
		{name: "2048 bytes", returnPath: "/" + strings.Repeat("a", 2047), valid: true},
		{name: "2049 bytes", returnPath: "/" + strings.Repeat("a", 2048)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
				return insert(sp, tc.returnPath)
			})
			if tc.valid {
				if err != nil {
					t.Fatalf("insert error = %v, want success", err)
				}
				return
			}
			requirePGError(t, err, "oauth_transactions_return_path_check")
		})
	}
}

// oauthAgentAccessTables is the exact table set migration 00009 owns.
var oauthAgentAccessTables = []string{
	"oauth_clients",
	"oauth_authorization_codes",
	"oauth_grants",
	"oauth_tokens",
}

// ---------------------------------------------------------------------------
// oauth_clients: M1 name and redirect-URI bounds.
// ---------------------------------------------------------------------------

func TestOAuthClientConstraints(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	insert := func(sp pgx.Tx, name string, uris string, lastUsed time.Time) error {
		_, err := sp.Exec(ctx, `
			INSERT INTO oauth_clients (client_name, redirect_uris, created_at, last_used_at)
			VALUES ($1, $2, $3, $4)
		`, name, uris, oauthNow, lastUsed)
		return err
	}
	validURIs := `["` + oauthRedirectURI + `"]`

	cases := []struct {
		name           string
		clientName     string
		uris           string
		lastUsed       time.Time
		wantConstraint string // "" means the insert must succeed
	}{
		{name: "minimal valid row", clientName: "A", uris: validURIs, lastUsed: oauthNow},
		{
			name: "name 64 code points accepted", clientName: strings.Repeat("n", 64),
			uris: validURIs, lastUsed: oauthNow,
		},
		{
			name: "name 65 code points rejected", clientName: strings.Repeat("n", 65),
			uris: validURIs, lastUsed: oauthNow,
			wantConstraint: "oauth_clients_client_name_length_check",
		},
		{
			// 64 four-byte code points is exactly 256 bytes: the widest name
			// the code-point bound admits, and the reason the byte bound is
			// 256.
			name: "name 64 four-byte code points accepted", clientName: strings.Repeat("\U0001F600", 64),
			uris: validURIs, lastUsed: oauthNow,
		},
		{
			name: "name 65 four-byte code points rejected", clientName: strings.Repeat("\U0001F600", 65),
			uris: validURIs, lastUsed: oauthNow,
			wantConstraint: "oauth_clients_client_name_length_check",
		},
		{
			name: "empty name rejected", clientName: "", uris: validURIs, lastUsed: oauthNow,
			wantConstraint: "oauth_clients_client_name_length_check",
		},
		{
			name: "control character in name rejected", clientName: "bad\x07name",
			uris: validURIs, lastUsed: oauthNow,
			wantConstraint: "oauth_clients_client_name_control_check",
		},
		{
			name: "zero redirect uris rejected", clientName: "A", uris: `[]`, lastUsed: oauthNow,
			wantConstraint: "oauth_clients_redirect_uris_check",
		},
		{
			name: "five redirect uris accepted", clientName: "A",
			uris:     `["https://a.example/1","https://a.example/2","https://a.example/3","https://a.example/4","https://a.example/5"]`,
			lastUsed: oauthNow,
		},
		{
			name: "six redirect uris rejected", clientName: "A",
			uris:           `["https://a.example/1","https://a.example/2","https://a.example/3","https://a.example/4","https://a.example/5","https://a.example/6"]`,
			lastUsed:       oauthNow,
			wantConstraint: "oauth_clients_redirect_uris_check",
		},
		{
			name: "non-array redirect uris rejected", clientName: "A", uris: `"https://a.example/1"`,
			lastUsed:       oauthNow,
			wantConstraint: "oauth_clients_redirect_uris_check",
		},
		{
			name: "non-string redirect uri member rejected", clientName: "A", uris: `["https://a.example/1", 5]`,
			lastUsed:       oauthNow,
			wantConstraint: "oauth_clients_redirect_uris_check",
		},
		{
			name: "redirect uri 512 bytes accepted", clientName: "A",
			uris:     `["https://a.example/` + strings.Repeat("p", 512-len("https://a.example/")) + `"]`,
			lastUsed: oauthNow,
		},
		{
			name: "redirect uri 513 bytes rejected", clientName: "A",
			uris:           `["https://a.example/` + strings.Repeat("p", 513-len("https://a.example/")) + `"]`,
			lastUsed:       oauthNow,
			wantConstraint: "oauth_clients_redirect_uris_check",
		},
		{
			name: "redirect uri with a space rejected", clientName: "A",
			uris:           `["https://a.example/a b"]`,
			lastUsed:       oauthNow,
			wantConstraint: "oauth_clients_redirect_uris_check",
		},
		{
			name: "last used before created rejected", clientName: "A", uris: validURIs,
			lastUsed:       oauthNow.Add(-time.Second),
			wantConstraint: "oauth_clients_last_used_order_check",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
				return insert(sp, tc.clientName, tc.uris, tc.lastUsed)
			})
			if tc.wantConstraint == "" {
				if err != nil {
					t.Fatalf("insert error = %v, want success", err)
				}
				return
			}
			requirePGError(t, err, tc.wantConstraint)
		})
	}
}

// ---------------------------------------------------------------------------
// oauth_authorization_codes: M2 digest, scope, challenge, redirect, and the
// exact 60-second expiry.
// ---------------------------------------------------------------------------

func TestOAuthAuthorizationCodeConstraints(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userID := createTestUser(ctx, t, tx)
	clientID := insertOAuthClient(ctx, t, tx)

	type codeRow struct {
		digest      []byte
		scopes      string
		challenge   string
		redirectURI string
		expiresAt   time.Time
		consumedAt  *time.Time
		familyID    *uuid.UUID
	}
	base := func() codeRow {
		return codeRow{
			digest:      validOAuthDigest(),
			scopes:      oauthScopesBoth,
			challenge:   oauthChallenge,
			redirectURI: oauthRedirectURI,
			expiresAt:   oauthNow.Add(oauthCodeTTL),
		}
	}
	insert := func(sp pgx.Tx, row codeRow) error {
		_, err := sp.Exec(ctx, `
			INSERT INTO oauth_authorization_codes (
				code_digest, client_id, user_id, scopes, code_challenge, redirect_uri,
				created_at, expires_at, consumed_at, issued_family_id
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		`, row.digest, clientID, userID, row.scopes, row.challenge, row.redirectURI,
			oauthNow, row.expiresAt, row.consumedAt, row.familyID)
		return err
	}

	consumed := oauthNow.Add(time.Second)
	family := uuid.New()
	beforeCreated := oauthNow.Add(-time.Second)

	cases := []struct {
		name           string
		mutate         func(*codeRow)
		wantConstraint string
	}{
		{name: "valid unconsumed code", mutate: func(*codeRow) {}},
		{
			name:           "digest 31 bytes rejected",
			mutate:         func(r *codeRow) { r.digest = bytesOf(31, 'd') },
			wantConstraint: "oauth_authorization_codes_code_digest_length_check",
		},
		{
			name:           "digest 33 bytes rejected",
			mutate:         func(r *codeRow) { r.digest = bytesOf(33, 'd') },
			wantConstraint: "oauth_authorization_codes_code_digest_length_check",
		},
		{name: "read-only scope accepted", mutate: func(r *codeRow) { r.scopes = "resumes:read" }},
		{name: "write-only scope accepted", mutate: func(r *codeRow) { r.scopes = "resumes:write" }},
		{
			name:           "unknown scope rejected",
			mutate:         func(r *codeRow) { r.scopes = "resumes:publish" },
			wantConstraint: "oauth_authorization_codes_scopes_check",
		},
		{
			name:           "non-canonical scope order rejected",
			mutate:         func(r *codeRow) { r.scopes = "resumes:write resumes:read" },
			wantConstraint: "oauth_authorization_codes_scopes_check",
		},
		{
			name:           "empty scope rejected",
			mutate:         func(r *codeRow) { r.scopes = "" },
			wantConstraint: "oauth_authorization_codes_scopes_check",
		},
		{
			name:           "challenge 42 characters rejected",
			mutate:         func(r *codeRow) { r.challenge = oauthChallenge[:42] },
			wantConstraint: "oauth_authorization_codes_code_challenge_check",
		},
		{
			name:           "challenge 44 characters rejected",
			mutate:         func(r *codeRow) { r.challenge = oauthChallenge + "x" },
			wantConstraint: "oauth_authorization_codes_code_challenge_check",
		},
		{
			name:           "padded challenge rejected",
			mutate:         func(r *codeRow) { r.challenge = oauthChallenge[:42] + "=" },
			wantConstraint: "oauth_authorization_codes_code_challenge_check",
		},
		{
			name: "redirect uri 512 bytes accepted",
			mutate: func(r *codeRow) {
				r.redirectURI = "https://a.example/" + strings.Repeat("p", 512-len("https://a.example/"))
			},
		},
		{
			name: "redirect uri 513 bytes rejected",
			mutate: func(r *codeRow) {
				r.redirectURI = "https://a.example/" + strings.Repeat("p", 513-len("https://a.example/"))
			},
			wantConstraint: "oauth_authorization_codes_redirect_uri_check",
		},
		{
			name:           "empty redirect uri rejected",
			mutate:         func(r *codeRow) { r.redirectURI = "" },
			wantConstraint: "oauth_authorization_codes_redirect_uri_check",
		},
		{
			name:           "expiry 59 seconds rejected",
			mutate:         func(r *codeRow) { r.expiresAt = oauthNow.Add(oauthCodeTTL - time.Second) },
			wantConstraint: "oauth_authorization_codes_expiry_check",
		},
		{
			name:           "expiry 61 seconds rejected",
			mutate:         func(r *codeRow) { r.expiresAt = oauthNow.Add(oauthCodeTTL + time.Second) },
			wantConstraint: "oauth_authorization_codes_expiry_check",
		},
		{
			name:   "consumed code with issued family accepted",
			mutate: func(r *codeRow) { r.consumedAt = &consumed; r.familyID = &family },
		},
		{
			name:           "consumed code without issued family rejected",
			mutate:         func(r *codeRow) { r.consumedAt = &consumed },
			wantConstraint: "oauth_authorization_codes_consumed_family_check",
		},
		{
			name:           "issued family without consumption rejected",
			mutate:         func(r *codeRow) { r.familyID = &family },
			wantConstraint: "oauth_authorization_codes_consumed_family_check",
		},
		{
			name:           "consumed before created rejected",
			mutate:         func(r *codeRow) { r.consumedAt = &beforeCreated; r.familyID = &family },
			wantConstraint: "oauth_authorization_codes_consumed_order_check",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := base()
			tc.mutate(&row)
			err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error { return insert(sp, row) })
			if tc.wantConstraint == "" {
				if err != nil {
					t.Fatalf("insert error = %v, want success", err)
				}
				return
			}
			requirePGError(t, err, tc.wantConstraint)
		})
	}

	t.Run("duplicate code digest rejected", func(t *testing.T) {
		row := base()
		if err := insert(tx, row); err != nil {
			t.Fatalf("first insert: %v", err)
		}
		duplicate := base()
		duplicate.digest = row.digest
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error { return insert(sp, duplicate) })
		requirePGError(t, err, "oauth_authorization_codes_code_digest_key")
	})
}

// ---------------------------------------------------------------------------
// oauth_grants: closed scopes, revocation ordering, and the single live grant
// per (user, client).
// ---------------------------------------------------------------------------

func TestOAuthGrantConstraints(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userID := createTestUser(ctx, t, tx)
	clientID := insertOAuthClient(ctx, t, tx)

	insert := func(sp pgx.Tx, uid, cid uuid.UUID, scopes string, revokedAt *time.Time) error {
		_, err := sp.Exec(ctx, `
			INSERT INTO oauth_grants (user_id, client_id, scopes, created_at, revoked_at)
			VALUES ($1,$2,$3,$4,$5)
		`, uid, cid, scopes, oauthNow, revokedAt)
		return err
	}

	t.Run("unknown scope rejected", func(t *testing.T) {
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return insert(sp, userID, clientID, "resumes:publish", nil)
		})
		requirePGError(t, err, "oauth_grants_scopes_check")
	})
	t.Run("revoked before created rejected", func(t *testing.T) {
		before := oauthNow.Add(-time.Second)
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return insert(sp, userID, clientID, oauthScopesBoth, &before)
		})
		requirePGError(t, err, "oauth_grants_revoked_order_check")
	})
	t.Run("second live grant for the same user and client rejected", func(t *testing.T) {
		if err := insert(tx, userID, clientID, oauthScopesBoth, nil); err != nil {
			t.Fatalf("first live grant: %v", err)
		}
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			return insert(sp, userID, clientID, "resumes:read", nil)
		})
		requirePGError(t, err, "oauth_grants_live_user_client_key")
	})
	t.Run("revoked grant leaves room for a new live grant", func(t *testing.T) {
		revoked := oauthNow.Add(time.Minute)
		if _, err := tx.Exec(ctx,
			`UPDATE oauth_grants SET revoked_at = $2 WHERE user_id = $1`, userID, revoked,
		); err != nil {
			t.Fatalf("revoke grant: %v", err)
		}
		if err := insert(tx, userID, clientID, "resumes:read", nil); err != nil {
			t.Fatalf("live grant after revocation: %v", err)
		}
		// Two revoked rows plus a live one must coexist.
		if _, err := tx.Exec(ctx,
			`INSERT INTO oauth_grants (user_id, client_id, scopes, created_at, revoked_at)
			 VALUES ($1,$2,$3,$4,$5)`,
			userID, clientID, oauthScopesBoth, oauthNow, revoked,
		); err != nil {
			t.Fatalf("second revoked grant: %v", err)
		}
	})
}

// TestOAuthGrantSingleLiveRowUnderConcurrentInserts proves the partial unique
// index — not application code — arbitrates two consent approvals racing for
// the same (user, client). Both transactions are real, independent, and
// overlap: the second blocks on the first's uncommitted index entry and fails
// only once the first commits.
func TestOAuthGrantSingleLiveRowUnderConcurrentInserts(t *testing.T) {
	t.Parallel()
	dsn := newTestDatabase(t)
	db := openTestDB(t, dsn)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	provider, err := migrations.NewProvider(db, migrations.FS)
	if err != nil {
		t.Fatalf("NewProvider() error: %v", err)
	}
	if _, upErr := provider.Up(ctx); upErr != nil {
		t.Fatalf("Up() error: %v", upErr)
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error: %v", err)
	}
	t.Cleanup(pool.Close)

	var userID, clientID uuid.UUID
	if insertUserErr := pool.QueryRow(ctx,
		`INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id`,
		uuid.NewString()+"@example.com", "OAuth Grant Race",
	).Scan(&userID); insertUserErr != nil {
		t.Fatalf("insert user: %v", insertUserErr)
	}
	if insertClientErr := pool.QueryRow(ctx, `
		INSERT INTO oauth_clients (client_name, redirect_uris, created_at, last_used_at)
		VALUES ($1, $2, $3, $3) RETURNING id
	`, "Racing Agent", `["`+oauthRedirectURI+`"]`, oauthNow).Scan(&clientID); insertClientErr != nil {
		t.Fatalf("insert client: %v", insertClientErr)
	}

	txA, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin(A): %v", err)
	}
	t.Cleanup(func() { rollbackOAuthTx(t, txA) })
	txB, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin(B): %v", err)
	}
	t.Cleanup(func() { rollbackOAuthTx(t, txB) })

	var pidB int32
	if err := txB.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pidB); err != nil {
		t.Fatalf("B backend PID: %v", err)
	}

	insert := func(tx pgx.Tx, scopes string) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO oauth_grants (user_id, client_id, scopes, created_at)
			VALUES ($1,$2,$3,$4)
		`, userID, clientID, scopes, oauthNow)
		return err
	}

	if err := insert(txA, oauthScopesBoth); err != nil {
		t.Fatalf("A insert: %v", err)
	}

	// B's insert must block on A's uncommitted unique index entry, so it is
	// started in a goroutine and its outcome is only read after A commits.
	errB := make(chan error, 1)
	go func() { errB <- insert(txB, "resumes:read") }()

	waitForBlockedOAuthBackend(ctx, t, pool, pidB)

	if err := txA.Commit(ctx); err != nil {
		t.Fatalf("A commit: %v", err)
	}
	if got := <-errB; got == nil {
		t.Fatal("B insert error = nil, want a unique violation after A committed")
	} else {
		requirePGError(t, got, "oauth_grants_live_user_client_key")
	}

	var live int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM oauth_grants WHERE user_id = $1 AND client_id = $2 AND revoked_at IS NULL`,
		userID, clientID,
	).Scan(&live); err != nil {
		t.Fatalf("count live grants: %v", err)
	}
	if live != 1 {
		t.Fatalf("live grants after the race = %d, want exactly 1", live)
	}
}

// rollbackOAuthTx rolls a test transaction back, tolerating an already-closed
// transaction (the race test commits one of its two).
func rollbackOAuthTx(t *testing.T, tx pgx.Tx) {
	t.Helper()
	if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Errorf("rollback test transaction: %v", err)
	}
}

// waitForBlockedOAuthBackend polls until PostgreSQL reports pid waiting on
// another backend's lock. It observes real server state, so the race test
// proves contention instead of guessing at it with a sleep.
func waitForBlockedOAuthBackend(ctx context.Context, t *testing.T, pool *pgxpool.Pool, pid int32) {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		if err := pool.QueryRow(ctx, `SELECT cardinality(pg_blocking_pids($1)) > 0`, pid).Scan(&blocked); err != nil {
			t.Fatalf("probe blocked backend: %v", err)
		}
		if blocked {
			return
		}
		select {
		case <-deadline.C:
			t.Fatalf("backend %d never blocked on the live-grant unique index", pid)
		case <-ticker.C:
		}
	}
}

// ---------------------------------------------------------------------------
// oauth_tokens: digest, closed kind, lifetimes, and rotation lineage.
// ---------------------------------------------------------------------------

func TestOAuthTokenConstraints(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userID := createTestUser(ctx, t, tx)
	clientID := insertOAuthClient(ctx, t, tx)
	grantID := insertOAuthGrant(ctx, t, tx, userID, clientID)

	before := oauthNow.Add(-time.Second)

	cases := []struct {
		name           string
		mutate         func(*oauthTokenRow)
		wantConstraint string
	}{
		{name: "valid refresh token", mutate: func(*oauthTokenRow) {}},
		{
			name: "valid access token",
			mutate: func(r *oauthTokenRow) {
				r.kind = "access"
				r.expiresAt = oauthNow.Add(oauthAccessTTL)
			},
		},
		{
			name:           "digest 31 bytes rejected",
			mutate:         func(r *oauthTokenRow) { r.digest = bytesOf(31, 'd') },
			wantConstraint: "oauth_tokens_token_digest_length_check",
		},
		{
			name:           "digest 33 bytes rejected",
			mutate:         func(r *oauthTokenRow) { r.digest = bytesOf(33, 'd') },
			wantConstraint: "oauth_tokens_token_digest_length_check",
		},
		{
			name:           "unknown kind rejected",
			mutate:         func(r *oauthTokenRow) { r.kind = "bearer" },
			wantConstraint: "oauth_tokens_kind_check",
		},
		{
			name:           "expiry equal to creation rejected",
			mutate:         func(r *oauthTokenRow) { r.expiresAt = r.createdAt },
			wantConstraint: "oauth_tokens_expiry_order_check",
		},
		{
			name: "expiry after family expiry rejected",
			mutate: func(r *oauthTokenRow) {
				r.expiresAt = oauthNow.Add(oauthFamilyTTL + time.Second)
				r.familyExpiresAt = oauthNow.Add(oauthFamilyTTL)
			},
			wantConstraint: "oauth_tokens_expiry_order_check",
		},
		{
			name: "family lifetime over 30 days rejected",
			mutate: func(r *oauthTokenRow) {
				r.familyExpiresAt = oauthNow.Add(oauthFamilyTTL + time.Second)
				r.expiresAt = r.familyExpiresAt
			},
			wantConstraint: "oauth_tokens_family_lifetime_check",
		},
		{
			name: "access token over one hour rejected",
			mutate: func(r *oauthTokenRow) {
				r.kind = "access"
				r.expiresAt = oauthNow.Add(oauthAccessTTL + time.Second)
			},
			wantConstraint: "oauth_tokens_access_lifetime_check",
		},
		{
			name:           "revoked before created rejected",
			mutate:         func(r *oauthTokenRow) { r.revokedAt = &before },
			wantConstraint: "oauth_tokens_revoked_order_check",
		},
		{
			name:           "superseded before created rejected",
			mutate:         func(r *oauthTokenRow) { r.supersededAt = &before },
			wantConstraint: "oauth_tokens_superseded_order_check",
		},
		{
			name:           "last used before created rejected",
			mutate:         func(r *oauthTokenRow) { r.lastUsedAt = &before },
			wantConstraint: "oauth_tokens_last_used_order_check",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := defaultOAuthTokenRow(clientID, userID, grantID)
			tc.mutate(&row)
			err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error { return insertOAuthToken(ctx, sp, row) })
			if tc.wantConstraint == "" {
				if err != nil {
					t.Fatalf("insert error = %v, want success", err)
				}
				return
			}
			requirePGError(t, err, tc.wantConstraint)
		})
	}

	t.Run("duplicate token digest rejected", func(t *testing.T) {
		row := defaultOAuthTokenRow(clientID, userID, grantID)
		if err := insertOAuthToken(ctx, tx, row); err != nil {
			t.Fatalf("first insert: %v", err)
		}
		duplicate := defaultOAuthTokenRow(clientID, userID, grantID)
		duplicate.digest = row.digest
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error { return insertOAuthToken(ctx, sp, duplicate) })
		requirePGError(t, err, "oauth_tokens_token_digest_key")
	})

	t.Run("one successor per predecessor", func(t *testing.T) {
		predecessor := defaultOAuthTokenRow(clientID, userID, grantID)
		predecessorID := insertOAuthTokenReturningID(ctx, t, tx, predecessor)

		successor := defaultOAuthTokenRow(clientID, userID, grantID)
		successor.familyID = predecessor.familyID
		successor.rotatedFrom = &predecessorID
		if err := insertOAuthToken(ctx, tx, successor); err != nil {
			t.Fatalf("first successor: %v", err)
		}

		second := defaultOAuthTokenRow(clientID, userID, grantID)
		second.familyID = predecessor.familyID
		second.rotatedFrom = &predecessorID
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error { return insertOAuthToken(ctx, sp, second) })
		requirePGError(t, err, "oauth_tokens_rotated_from_key")
	})

	t.Run("unknown predecessor rejected", func(t *testing.T) {
		missing := uuid.New()
		row := defaultOAuthTokenRow(clientID, userID, grantID)
		row.rotatedFrom = &missing
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error { return insertOAuthToken(ctx, sp, row) })
		requirePGError(t, err, "oauth_tokens_rotated_from_fkey")
	})

	t.Run("token cannot rotate from itself", func(t *testing.T) {
		id := insertOAuthTokenReturningID(ctx, t, tx, defaultOAuthTokenRow(clientID, userID, grantID))
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error {
			_, execErr := sp.Exec(ctx, `UPDATE oauth_tokens SET rotated_from = id WHERE id = $1`, id)
			return execErr
		})
		requirePGError(t, err, "oauth_tokens_rotated_from_self_check")
	})
}

// ---------------------------------------------------------------------------
// Cascade matrix.
// ---------------------------------------------------------------------------

func TestOAuthAgentAccessCascades(t *testing.T) {
	t.Parallel()

	countRows := func(ctx context.Context, t *testing.T, tx pgx.Tx, query string, arg uuid.UUID) int {
		t.Helper()
		var n int
		if err := tx.QueryRow(ctx, query, arg).Scan(&n); err != nil {
			t.Fatalf("count %q: %v", query, err)
		}
		return n
	}

	t.Run("account deletion cascades codes grants and tokens", func(t *testing.T) {
		t.Parallel()
		tx, ctx := newResumeSchemaTx(t)
		userID := createTestUser(ctx, t, tx)
		clientID := insertOAuthClient(ctx, t, tx)
		grantID := insertOAuthGrant(ctx, t, tx, userID, clientID)
		insertOAuthTokenReturningID(ctx, t, tx, defaultOAuthTokenRow(clientID, userID, grantID))
		if _, err := tx.Exec(ctx, `
			INSERT INTO oauth_authorization_codes (
				code_digest, client_id, user_id, scopes, code_challenge, redirect_uri, created_at, expires_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		`, validOAuthDigest(), clientID, userID, oauthScopesBoth, oauthChallenge,
			oauthRedirectURI, oauthNow, oauthNow.Add(oauthCodeTTL)); err != nil {
			t.Fatalf("insert code: %v", err)
		}

		if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Fatalf("delete user: %v", err)
		}

		for label, query := range map[string]string{
			"codes":  `SELECT count(*) FROM oauth_authorization_codes WHERE user_id = $1`,
			"grants": `SELECT count(*) FROM oauth_grants WHERE user_id = $1`,
			"tokens": `SELECT count(*) FROM oauth_tokens WHERE user_id = $1`,
		} {
			if n := countRows(ctx, t, tx, query, userID); n != 0 {
				t.Errorf("%s after account deletion = %d, want 0 (cascade)", label, n)
			}
		}
		// The client survives its user: another account may still use it.
		if n := countRows(ctx, t, tx, `SELECT count(*) FROM oauth_clients WHERE id = $1`, clientID); n != 1 {
			t.Errorf("client rows after account deletion = %d, want 1", n)
		}
	})

	t.Run("client deletion cascades its rows", func(t *testing.T) {
		t.Parallel()
		tx, ctx := newResumeSchemaTx(t)
		userID := createTestUser(ctx, t, tx)
		clientID := insertOAuthClient(ctx, t, tx)
		grantID := insertOAuthGrant(ctx, t, tx, userID, clientID)
		insertOAuthTokenReturningID(ctx, t, tx, defaultOAuthTokenRow(clientID, userID, grantID))
		if _, err := tx.Exec(ctx, `
			INSERT INTO oauth_authorization_codes (
				code_digest, client_id, user_id, scopes, code_challenge, redirect_uri, created_at, expires_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		`, validOAuthDigest(), clientID, userID, oauthScopesBoth, oauthChallenge,
			oauthRedirectURI, oauthNow, oauthNow.Add(oauthCodeTTL)); err != nil {
			t.Fatalf("insert code: %v", err)
		}

		if _, err := tx.Exec(ctx, `DELETE FROM oauth_clients WHERE id = $1`, clientID); err != nil {
			t.Fatalf("delete client: %v", err)
		}

		for label, query := range map[string]string{
			"codes":  `SELECT count(*) FROM oauth_authorization_codes WHERE client_id = $1`,
			"grants": `SELECT count(*) FROM oauth_grants WHERE client_id = $1`,
			"tokens": `SELECT count(*) FROM oauth_tokens WHERE client_id = $1`,
		} {
			if n := countRows(ctx, t, tx, query, clientID); n != 0 {
				t.Errorf("%s after client deletion = %d, want 0 (cascade)", label, n)
			}
		}
		// The account survives its clients.
		if n := countRows(ctx, t, tx, `SELECT count(*) FROM users WHERE id = $1`, userID); n != 1 {
			t.Errorf("user rows after client deletion = %d, want 1", n)
		}
	})

	t.Run("grant deletion cascades its tokens", func(t *testing.T) {
		t.Parallel()
		tx, ctx := newResumeSchemaTx(t)
		userID := createTestUser(ctx, t, tx)
		clientID := insertOAuthClient(ctx, t, tx)
		grantID := insertOAuthGrant(ctx, t, tx, userID, clientID)
		insertOAuthTokenReturningID(ctx, t, tx, defaultOAuthTokenRow(clientID, userID, grantID))

		if _, err := tx.Exec(ctx, `DELETE FROM oauth_grants WHERE id = $1`, grantID); err != nil {
			t.Fatalf("delete grant: %v", err)
		}
		if n := countRows(ctx, t, tx, `SELECT count(*) FROM oauth_tokens WHERE grant_id = $1`, grantID); n != 0 {
			t.Errorf("tokens after grant deletion = %d, want 0 (cascade)", n)
		}
	})

	t.Run("predecessor deletion detaches the successor instead of cascading", func(t *testing.T) {
		t.Parallel()
		tx, ctx := newResumeSchemaTx(t)
		userID := createTestUser(ctx, t, tx)
		clientID := insertOAuthClient(ctx, t, tx)
		grantID := insertOAuthGrant(ctx, t, tx, userID, clientID)

		predecessor := defaultOAuthTokenRow(clientID, userID, grantID)
		predecessorID := insertOAuthTokenReturningID(ctx, t, tx, predecessor)
		successor := defaultOAuthTokenRow(clientID, userID, grantID)
		successor.familyID = predecessor.familyID
		successor.rotatedFrom = &predecessorID
		successorID := insertOAuthTokenReturningID(ctx, t, tx, successor)

		if _, err := tx.Exec(ctx, `DELETE FROM oauth_tokens WHERE id = $1`, predecessorID); err != nil {
			t.Fatalf("delete predecessor: %v", err)
		}
		var rotatedFrom *uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT rotated_from FROM oauth_tokens WHERE id = $1`, successorID).
			Scan(&rotatedFrom); err != nil {
			t.Fatalf("read successor: %v", err)
		}
		if rotatedFrom != nil {
			t.Errorf("successor rotated_from after predecessor deletion = %v, want NULL", *rotatedFrom)
		}
	})
}

// ---------------------------------------------------------------------------
// No raw token or code material is representable in, or present in, the schema.
// ---------------------------------------------------------------------------

// TestOAuthAgentAccessStoresNoRawMaterial proves the adversarial rule two ways:
// migration 00009 and sql/queries.sql never mention the raw access/refresh
// token prefixes at all, and a full fixture set leaves no text column holding a
// value with those prefixes. Digests are bytea and length-checked, so a 43-plus
// character raw spelling cannot be stored in a digest column either.
func TestOAuthAgentAccessStoresNoRawMaterial(t *testing.T) {
	t.Parallel()

	const (
		accessPrefix  = "amat_"
		refreshPrefix = "amrt_"
	)

	for _, path := range []string{
		"00009_add_oauth_agent_access.sql",
		"../sql/queries.sql",
	} {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, prefix := range []string{accessPrefix, refreshPrefix} {
			if strings.Contains(string(content), prefix) {
				t.Errorf("%s contains the raw material prefix %q; storage is digest-only", path, prefix)
			}
		}
	}

	tx, ctx := newResumeSchemaTx(t)
	userID := createTestUser(ctx, t, tx)
	clientID := insertOAuthClient(ctx, t, tx)
	grantID := insertOAuthGrant(ctx, t, tx, userID, clientID)
	insertOAuthTokenReturningID(ctx, t, tx, defaultOAuthTokenRow(clientID, userID, grantID))
	if _, err := tx.Exec(ctx, `
		INSERT INTO oauth_authorization_codes (
			code_digest, client_id, user_id, scopes, code_challenge, redirect_uri, created_at, expires_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`, validOAuthDigest(), clientID, userID, oauthScopesBoth, oauthChallenge,
		oauthRedirectURI, oauthNow, oauthNow.Add(oauthCodeTTL)); err != nil {
		t.Fatalf("insert code: %v", err)
	}

	// Every text-typed column in the four tables, scanned for the raw
	// prefixes. The catalog drives the list so a column added later is
	// covered without editing this test.
	rows, err := tx.Query(ctx, `
		SELECT table_name, column_name
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = ANY($1::text[])
		  AND data_type IN ('text', 'character varying')
		ORDER BY table_name, column_name
	`, oauthAgentAccessTables)
	if err != nil {
		t.Fatalf("list text columns: %v", err)
	}
	type column struct{ table, name string }
	var columns []column
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.table, &c.name); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		columns = append(columns, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate columns: %v", err)
	}
	if len(columns) == 0 {
		t.Fatal("found no text columns to scan; the probe would pass vacuously")
	}

	for _, c := range columns {
		var hits int
		query := `SELECT count(*) FROM ` + c.table + ` WHERE ` + c.name + ` LIKE $1 OR ` + c.name + ` LIKE $2`
		if err := tx.QueryRow(ctx, query, accessPrefix+"%", refreshPrefix+"%").Scan(&hits); err != nil {
			t.Fatalf("scan %s.%s: %v", c.table, c.name, err)
		}
		if hits != 0 {
			t.Errorf("%s.%s holds %d value(s) with a raw material prefix, want 0", c.table, c.name, hits)
		}
	}
}
