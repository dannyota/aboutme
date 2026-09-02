package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/testutil"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const validDSN = "postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme_dev?sslmode=disable"

func TestParseConfigGuardsTheDatabase(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		args []string
		want string // substring the error must contain; empty = must succeed
	}{
		{name: "seed ok", args: []string{"seed", "--database-url", validDSN}},
		{name: "cleanup ok", args: []string{"cleanup", "--database-url", validDSN}},
		{name: "missing subcommand", args: nil, want: "subcommand"},
		{name: "unknown subcommand", args: []string{"drop", "--database-url", validDSN}, want: "unknown subcommand"},
		{name: "missing url", args: []string{"seed"}, want: "--database-url is required"},
		{name: "test database refused", args: []string{"seed", "--database-url", "postgres://127.0.0.1/aboutme"}, want: "aboutme_dev"},
		{name: "remote host refused", args: []string{"seed", "--database-url", "postgres://db.example.com/aboutme_dev"}, want: "127.0.0.1"},
		{name: "query remote host refused", args: []string{"seed", "--database-url", validDSN + "&host=db.example.com"}, want: "127.0.0.1"},
		{name: "query fallback host refused", args: []string{"seed", "--database-url", validDSN + "&host=127.0.0.1,db.example.com"}, want: "127.0.0.1"},
		{name: "query test database refused", args: []string{"seed", "--database-url", validDSN + "&dbname=aboutme"}, want: "aboutme_dev"},
		{name: "mysql refused", args: []string{"seed", "--database-url", "mysql://127.0.0.1/aboutme_dev"}, want: "postgres"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cmd, cfg, err := parseConfig(tt.args)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("parseConfig() error = %v", err)
				}
				if cmd != tt.args[0] || cfg.DatabaseURL != validDSN {
					t.Fatalf("cmd=%q dsn=%q", cmd, cfg.DatabaseURL)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("parseConfig() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestParseConfigDoesNotExposeCredentials(t *testing.T) {
	t.Parallel()
	const credentialMarker = "dev-seed-test-secret"
	_, _, err := parseConfig([]string{
		"seed",
		"--database-url",
		"postgres://aboutme:" + credentialMarker + "@127.0.0.1:20432/%zz",
	})
	if err == nil {
		t.Fatal("parseConfig() error = nil, want malformed database URL refusal")
	}
	if strings.Contains(err.Error(), credentialMarker) {
		t.Fatalf("parseConfig() exposed credential marker: %v", err)
	}
}

func TestEmbeddedFixtureMatchesSchemaPackage(t *testing.T) {
	t.Parallel()
	upstream, err := os.ReadFile("../../../../packages/schema/fixtures/full.json")
	if err != nil {
		t.Fatalf("read schema fixture: %v", err)
	}
	if !bytes.Equal(upstream, fullFixture) {
		t.Fatal("cmd/dev-seed/testdata/full.json drifted from packages/schema/fixtures/full.json; copy it again")
	}
	if _, _, _, err := splitResumeDoc(fullFixture); err != nil {
		t.Fatalf("splitResumeDoc: %v", err)
	}
}

func TestSplitResumeDocStripsPhoto(t *testing.T) {
	t.Parallel()
	personalDetails, _, _, err := splitResumeDoc(fullFixture)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte(`{
    "fullName": "Ada Lovelace",
    "headline": "Analytical Engineer",
    "details": [
      {
        "id": "f0cf483d-585c-4340-9adc-773b3bea3db0",
        "type": "email",
        "value": "ada@example.com",
        "isHidden": false
      },
      {
        "id": "2c25e47b-3eaa-4670-ac85-c23626079998",
        "type": "phone",
        "value": "+84 90 000 0000",
        "isHidden": false
      },
      {
        "id": "9d150558-23be-4bcd-b7ec-181932f94907",
        "type": "location",
        "value": "Hanoi, Vietnam",
        "isHidden": false
      },
      {
        "id": "062fe149-9545-4484-8df0-332c302cd3b8",
        "type": "linkedin",
        "label": "LinkedIn",
        "value": "https://linkedin.com/in/ada",
        "isHidden": false
      },
      {
        "id": "09418968-1a86-49cb-8dd8-c746a2780249",
        "type": "website",
        "value": "https://ada.example.com",
        "isHidden": false
      }
    ]
  }`)
	if !bytes.Equal(personalDetails, want) {
		t.Fatalf("personal details bytes differ from fixture minus photo:\n got: %s\nwant: %s", personalDetails, want)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(personalDetails, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["photo"]; ok {
		t.Fatal("seeded personal details must not reference a photo object")
	}
	for _, key := range []string{"fullName", "headline", "details"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("seeded personal details lost %q", key)
		}
	}
}

func TestWithoutPhotoCopiesWhenPhotoIsAbsent(t *testing.T) {
	t.Parallel()
	raw := json.RawMessage(`{"fullName":"Ada Lovelace"}`)
	want := append([]byte(nil), raw...)
	got, err := withoutPhoto(raw)
	if err != nil {
		t.Fatal(err)
	}
	raw[2] = 'X'
	if !bytes.Equal(got, want) {
		t.Fatalf("withoutPhoto changed its result after input mutation: got %q, want %q", got, want)
	}
}

func TestWithoutPhotoHandlesPhotoPosition(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		raw  string
		want string
	}{
		{name: "first", raw: `{"photo":{"key":"p"},"fullName":"Ada","details":[]}`, want: `{"fullName":"Ada","details":[]}`},
		{name: "middle", raw: `{"fullName":"Ada","photo":[1,2],"details":[]}`, want: `{"fullName":"Ada","details":[]}`},
		{name: "last", raw: `{"fullName":"Ada","details":[],"photo":null}`, want: `{"fullName":"Ada","details":[]}`},
		{name: "sole", raw: `{"photo":true}`, want: `{}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := withoutPhoto(json.RawMessage(tt.raw))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, []byte(tt.want)) {
				t.Fatalf("withoutPhoto(%s) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestSeedIdentitiesAreFixed(t *testing.T) {
	t.Parallel()
	if seedUser.ID.String() != "5d000000-0000-4000-8000-000000000001" ||
		seedResumeID.String() != "5d000000-0000-4000-8000-000000000002" ||
		seedUser.Email != "dev@aboutme.invalid" || seedUser.Password != "aboutme-dev-password-1" {
		t.Fatal("seed identities changed; the spec, runbook, and entry proof pin them")
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	})
	return db
}

func countRows(ctx context.Context, t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func deleteSeedTestRows(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `DELETE FROM resumes WHERE id = $1`, seedResumeID); err != nil {
		t.Errorf("delete seed resume: %v", err)
	}
	if _, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, seedUser.ID); err != nil {
		t.Errorf("delete seed user: %v", err)
	}
}

func TestSeedIsIdempotentAndCleanupIsExact(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := runCleanupWithDB(ctx, db); err != nil {
		t.Fatalf("pre-clean: %v", err)
	}
	t.Cleanup(func() {
		if err := runCleanupWithDB(ctx, db); err != nil {
			t.Errorf("cleanup seed rows: %v", err)
		}
	})

	for i := 0; i < 2; i++ {
		if err := runSeedWithDB(ctx, db); err != nil {
			t.Fatalf("seed run %d: %v", i+1, err)
		}
	}
	if n := countRows(ctx, t, db, `SELECT count(*) FROM users WHERE id = $1`, seedUser.ID); n != 1 {
		t.Fatalf("users = %d, want 1", n)
	}
	if n := countRows(ctx, t, db, `SELECT count(*) FROM password_credentials WHERE user_id = $1`, seedUser.ID); n != 1 {
		t.Fatalf("credentials = %d, want 1", n)
	}
	var originalHash []byte
	if err := db.QueryRowContext(ctx, `SELECT encoded_hash FROM password_credentials WHERE user_id = $1`, seedUser.ID).Scan(&originalHash); err != nil {
		t.Fatalf("read original credential hash: %v", err)
	}
	if n := countRows(ctx, t, db, `SELECT count(*) FROM resumes WHERE id = $1 AND user_id = $2 AND live = false AND slug IS NULL AND revision = 1 AND schema_version = 2`, seedResumeID, seedUser.ID); n != 1 {
		t.Fatalf("seed resume rows = %d, want 1 private v2 resume at revision 1", n)
	}

	// An edited document survives a re-seed.
	if _, err := db.ExecContext(ctx, `UPDATE resumes SET title = 'edited', revision = 7 WHERE id = $1`, seedResumeID); err != nil {
		t.Fatal(err)
	}
	if err := runSeedWithDB(ctx, db); err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	var reseededHash []byte
	if err := db.QueryRowContext(ctx, `SELECT encoded_hash FROM password_credentials WHERE user_id = $1`, seedUser.ID).Scan(&reseededHash); err != nil {
		t.Fatalf("read reseeded credential hash: %v", err)
	}
	if !bytes.Equal(originalHash, reseededHash) {
		t.Fatal("re-seed overwrote an existing credential hash")
	}
	if n := countRows(ctx, t, db, `SELECT count(*) FROM resumes WHERE id = $1 AND title = 'edited' AND revision = 7`, seedResumeID); n != 1 {
		t.Fatal("re-seed overwrote an existing document")
	}

	if err := runCleanupWithDB(ctx, db); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if n := countRows(ctx, t, db, `SELECT count(*) FROM users WHERE id = $1`, seedUser.ID) + countRows(ctx, t, db, `SELECT count(*) FROM resumes WHERE id = $1`, seedResumeID); n != 0 {
		t.Fatalf("rows after cleanup = %d, want 0", n)
	}
}

func TestSeedRefusesFixedIDOwnedByDifferentEmail(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	deleteSeedTestRows(ctx, t, db)
	t.Cleanup(func() { deleteSeedTestRows(ctx, t, db) })

	const otherEmail = "seed-id-collision@aboutme.invalid"
	if _, err := db.ExecContext(ctx,
		`INSERT INTO users (id, email, name) VALUES ($1, $2, 'Fixed ID collision')`, seedUser.ID, otherEmail); err != nil {
		t.Fatalf("insert fixed-ID collision: %v", err)
	}

	err := runSeedWithDB(ctx, db)
	if err == nil || !strings.Contains(err.Error(), "fixed id exists under a different email") {
		t.Fatalf("runSeedWithDB error = %v, want fixed-ID refusal", err)
	}
	if err := runCleanupWithDB(ctx, db); err == nil || !strings.Contains(err.Error(), "different email") {
		t.Fatalf("runCleanupWithDB error = %v, want fixed-ID refusal", err)
	}

	var email, name string
	if err := db.QueryRowContext(ctx, `SELECT email::text, name FROM users WHERE id = $1`, seedUser.ID).Scan(&email, &name); err != nil {
		t.Fatalf("read collision user: %v", err)
	}
	if email != otherEmail || name != "Fixed ID collision" {
		t.Fatalf("collision user = (%q, %q), want (%q, %q)", email, name, otherEmail, "Fixed ID collision")
	}
	if n := countRows(ctx, t, db, `SELECT count(*) FROM password_credentials WHERE user_id = $1`, seedUser.ID); n != 0 {
		t.Fatalf("credentials = %d, want 0", n)
	}
	if n := countRows(ctx, t, db, `SELECT count(*) FROM resumes WHERE id = $1`, seedResumeID); n != 0 {
		t.Fatalf("seed resumes = %d, want 0", n)
	}
}

func TestSeedAndCleanupRefuseFixedResumeIDOwnedByAnotherUser(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	deleteSeedTestRows(ctx, t, db)

	const otherUserID = "5d000000-0000-4000-8000-0000000000fe"
	const otherEmail = "seed-resume-collision@aboutme.invalid"
	t.Cleanup(func() {
		if _, err := db.ExecContext(ctx, `DELETE FROM resumes WHERE id = $1 AND user_id = $2`, seedResumeID, otherUserID); err != nil {
			t.Errorf("delete colliding resume: %v", err)
		}
		if _, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, otherUserID); err != nil {
			t.Errorf("delete colliding resume owner: %v", err)
		}
		deleteSeedTestRows(ctx, t, db)
	})

	if _, err := db.ExecContext(ctx,
		`INSERT INTO users (id, email, name) VALUES ($1, $2, 'Fixed resume collision')`, otherUserID, otherEmail); err != nil {
		t.Fatalf("insert resume owner: %v", err)
	}
	personalDetails, content, customization, err := splitResumeDoc(fullFixture)
	if err != nil {
		t.Fatalf("split resume document: %v", err)
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO resumes
			(id, user_id, title, slug, live, download_enabled, seo_geo_enabled,
			 schema_version, revision, personal_details, content, customization)
		VALUES ($1, $2, 'Other resume', NULL, false, true, false, 2, 7, $3, $4, $5)`,
		seedResumeID, otherUserID, personalDetails, content, customization)
	if err != nil {
		t.Fatalf("insert colliding resume: %v", err)
	}

	err = runSeedWithDB(ctx, db)
	if err == nil || !strings.Contains(err.Error(), "seed resume id is owned by a different user") {
		t.Fatalf("runSeedWithDB error = %v, want fixed-resume refusal", err)
	}
	assertFixedResumeCollisionUnchanged(ctx, t, db, otherUserID, otherEmail)

	err = runCleanupWithDB(ctx, db)
	if err == nil || !strings.Contains(err.Error(), "seed resume id is owned by a different user") {
		t.Fatalf("runCleanupWithDB error = %v, want fixed-resume refusal", err)
	}
	assertFixedResumeCollisionUnchanged(ctx, t, db, otherUserID, otherEmail)
}

func assertFixedResumeCollisionUnchanged(ctx context.Context, t *testing.T, db *sql.DB, otherUserID, otherEmail string) {
	t.Helper()
	if n := countRows(ctx, t, db, `SELECT count(*) FROM users WHERE id = $1`, seedUser.ID); n != 0 {
		t.Fatalf("seed users = %d, want 0", n)
	}
	if n := countRows(ctx, t, db, `SELECT count(*) FROM password_credentials WHERE user_id = $1`, seedUser.ID); n != 0 {
		t.Fatalf("seed credentials = %d, want 0", n)
	}
	if n := countRows(ctx, t, db, `SELECT count(*) FROM users WHERE id = $1 AND email::text = $2 AND name = 'Fixed resume collision'`, otherUserID, otherEmail); n != 1 {
		t.Fatalf("resume owners = %d, want unchanged row", n)
	}
	if n := countRows(ctx, t, db, `SELECT count(*) FROM resumes WHERE id = $1 AND user_id = $2 AND title = 'Other resume' AND revision = 7`, seedResumeID, otherUserID); n != 1 {
		t.Fatalf("colliding resumes = %d, want unchanged row", n)
	}
}

func TestCleanupRefusesWhenSeedUserOwnsNonSeedResume(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := runCleanupWithDB(ctx, db); err != nil {
		t.Fatalf("pre-clean: %v", err)
	}
	t.Cleanup(func() { deleteSeedTestRows(ctx, t, db) })
	if err := runSeedWithDB(ctx, db); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const extraResumeID = "5d000000-0000-4000-8000-000000000003"
	if _, err := db.ExecContext(ctx, `
		INSERT INTO resumes
			(id, user_id, title, slug, live, download_enabled, seo_geo_enabled,
			 schema_version, revision, personal_details, content, customization)
		SELECT $1, user_id, 'Developer resume', NULL, false, true, false,
		       schema_version, 1, personal_details, content, customization
		FROM resumes WHERE id = $2`, extraResumeID, seedResumeID); err != nil {
		t.Fatalf("insert extra resume: %v", err)
	}
	var originalHash []byte
	if err := db.QueryRowContext(ctx, `SELECT encoded_hash FROM password_credentials WHERE user_id = $1`, seedUser.ID).Scan(&originalHash); err != nil {
		t.Fatalf("read credential hash: %v", err)
	}

	err := runCleanupWithDB(ctx, db)
	if err == nil || !strings.Contains(err.Error(), "non-seed resume") {
		t.Fatalf("runCleanupWithDB error = %v, want non-seed resume refusal", err)
	}

	var email, name string
	if err := db.QueryRowContext(ctx, `SELECT email::text, name FROM users WHERE id = $1`, seedUser.ID).Scan(&email, &name); err != nil {
		t.Fatalf("read seed user: %v", err)
	}
	if email != seedUser.Email || name != seedUser.Name {
		t.Fatalf("seed user = (%q, %q), want (%q, %q)", email, name, seedUser.Email, seedUser.Name)
	}
	if n := countRows(ctx, t, db, `SELECT count(*) FROM resumes WHERE id = $1 AND title = $2 AND revision = 1`, seedResumeID, seedResumeTitle); n != 1 {
		t.Fatalf("seed resumes = %d, want unchanged row", n)
	}
	if n := countRows(ctx, t, db, `SELECT count(*) FROM resumes WHERE id = $1 AND title = 'Developer resume' AND revision = 1`, extraResumeID); n != 1 {
		t.Fatalf("extra resumes = %d, want unchanged row", n)
	}
	var retainedHash []byte
	if err := db.QueryRowContext(ctx, `SELECT encoded_hash FROM password_credentials WHERE user_id = $1`, seedUser.ID).Scan(&retainedHash); err != nil {
		t.Fatalf("read retained credential hash: %v", err)
	}
	if !bytes.Equal(originalHash, retainedHash) {
		t.Fatal("cleanup changed the credential hash")
	}
}

func TestSeedFailsWhenEmailBelongsToAnotherAccount(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	if err := runCleanupWithDB(ctx, db); err != nil {
		t.Fatalf("pre-clean: %v", err)
	}
	other := "5d000000-0000-4000-8000-0000000000ff"
	if _, err := db.ExecContext(ctx, `INSERT INTO users (id, email, name) VALUES ($1, $2, 'Other')`, other, seedUser.Email); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, other); err != nil {
			t.Errorf("delete other user: %v", err)
		}
	})

	err := runSeedWithDB(ctx, db)
	if err == nil || !strings.Contains(err.Error(), "exists under a different id") {
		t.Fatalf("runSeedWithDB error = %v, want the different-id refusal", err)
	}
}
