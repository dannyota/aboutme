package user_test

import (
	"context"
	"errors"
	"net/netip"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
	"github.com/dannyota/aboutme/apps/server/internal/user"
	"github.com/dannyota/aboutme/apps/server/migrations"
)

// Compile-time assertions that the generated shapes match what internal/user
// and internal/auth use. A failure here means migrations, queries.sql, or the
// sqlc.yaml overrides drifted from the expected shape. The
// field-level lines pin the nullable-column contract (native pointers), not
// just the type names — including the
// sqlc.yaml `rename` entries that keep initialism-bearing columns (ua, ip,
// csrf_secret, pkce_verifier, redirect_uri) from silently reverting to
// sqlc's default casing (Ua, Ip, CsrfSecret, PkceVerifier, RedirectUri).
var (
	_ store.User             = store.User{}
	_ store.Identity         = store.Identity{}
	_ store.Session          = store.Session{}
	_ store.OAuthTransaction = store.OAuthTransaction{}

	_ *string     = store.User{}.AvatarKey
	_ *time.Time  = store.Session{}.RotationGraceUntil
	_ *time.Time  = store.Session{}.RevokedAt
	_ *string     = store.Session{}.UA
	_ *netip.Addr = store.Session{}.IP
	_ []byte      = store.Session{}.TokenHash
	_ []byte      = store.Session{}.CSRFSecret
	// RotatedFrom is the exact rotation-lineage foreign key a successor row
	// carries back to its predecessor.
	_ *uuid.UUID = store.Session{}.RotatedFrom
	_ string     = store.OAuthTransaction{}.PKCEVerifier
	_ string     = store.OAuthTransaction{}.RedirectURI
	_ *uuid.UUID = store.OAuthTransaction{}.LinkingUserID
)

// TestSchema_PreservesAuthConstraints is a unit test (no database) that
// guards two load-bearing authentication constraints: losing either
// would silently defeat "one identity per provider subject"
// or the link/reauth-must-name-a-user invariant, without
// any compile-time or generated-code signal (sqlc generates fine either
// way; only a live constraint violation would catch it, and only if a test
// happens to exercise that exact path).
//
// It reads the embedded goose migrations, which are the single source of
// truth for the schema. Because migrations are immutable and append-only,
// the constraint can no longer be edited out of the migration that created
// it -- the remaining way to lose it is a LATER migration dropping it, so
// this checks both that some migration creates it and that no migration's
// "-- +goose Up" section drops it. Only Up sections are scanned:
// migrations.Apply never runs a Down section (see its doc comment), so a
// Down section's own reversing DROP is not a real loss.
//
// This is a text-level guard, not a semantic one. The database-backed
// proof that the migrated schema really carries these constraints lives in
// the DSN-gated tests below and in the migrations package's own harness.
func TestSchema_PreservesAuthConstraints(t *testing.T) {
	t.Parallel()

	up := migrationUpSections(t)

	for _, constraint := range []string{
		"identities_provider_subject_key",
		"oauth_transactions_link_needs_user",
	} {
		if !strings.Contains(up, constraint) {
			t.Errorf("no migration creates constraint %q", constraint)
		}
		if strings.Contains(up, `DROP CONSTRAINT "`+constraint+`"`) ||
			strings.Contains(up, "DROP CONSTRAINT "+constraint) {
			t.Errorf("a migration's \"-- +goose Up\" section drops constraint %q", constraint)
		}
	}
}

// migrationUpSections returns the concatenation of every embedded
// migration's "-- +goose Up" section, in filename order.
func migrationUpSections(t *testing.T) string {
	t.Helper()

	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		t.Fatal("no embedded migrations found")
	}
	sort.Strings(names)

	var combined strings.Builder
	for _, name := range names {
		data, readErr := migrations.FS.ReadFile(name)
		if readErr != nil {
			t.Fatalf("read embedded migration %s: %v", name, readErr)
		}
		content := string(data)
		_, after, found := strings.Cut(content, "-- +goose Up")
		if !found {
			t.Fatalf("embedded migration %s has no \"-- +goose Up\" marker", name)
		}
		if before, _, hasDown := strings.Cut(after, "-- +goose Down"); hasDown {
			after = before
		}
		combined.WriteString(after)
		combined.WriteString("\n")
	}
	return combined.String()
}

// newIntegrationStore returns a user.Store backed by a fresh transaction
// against TEST_DATABASE_URL, rolled back automatically when the test
// finishes so repeated runs against a persistent test database never
// accumulate rows or collide on the unique-email constraint. It skips the
// test if TEST_DATABASE_URL is unset, matching internal/store's own
// integration test so `go test ./...` stays fully hermetic by default.
//
// Schema setup goes through internal/testutil.RequireMigratedTestDatabaseURL
// -- the same shared helper internal/auth and internal/store use -- so this
// package's tests never depend on another package's test binary having
// already applied migrations first. Opening the pool and querying users with
// no migration step would make the test depend on another package's setup.
func newIntegrationStore(t *testing.T) (*user.Store, context.Context) {
	t.Helper()

	dsn := testutil.RequireMigratedTestDatabaseURL(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() error: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error: %v", err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("Rollback() error: %v", err)
		}
	})

	return user.New(tx), ctx
}

// assertUserEqual compares user fields individually rather than the whole
// struct: store.User carries pointer fields (AvatarKey), so a plain ==
// or reflect.DeepEqual on two independently-scanned rows would either
// fail to compile or be needlessly fragile.
func assertUserEqual(t *testing.T, got, want store.User) {
	t.Helper()

	if got.ID != want.ID {
		t.Errorf("ID = %v, want %v", got.ID, want.ID)
	}
	if got.Email != want.Email {
		t.Errorf("Email = %q, want %q", got.Email, want.Email)
	}
	if got.Name != want.Name {
		t.Errorf("Name = %q, want %q", got.Name, want.Name)
	}
	switch {
	case (got.AvatarKey == nil) != (want.AvatarKey == nil):
		t.Errorf("AvatarKey = %v, want %v", got.AvatarKey, want.AvatarKey)
	case got.AvatarKey != nil && *got.AvatarKey != *want.AvatarKey:
		t.Errorf("AvatarKey = %q, want %q", *got.AvatarKey, *want.AvatarKey)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
	if !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want %v", got.UpdatedAt, want.UpdatedAt)
	}
}

func TestStore_Integration_CreateAndGetRoundTrip(t *testing.T) {
	t.Parallel()
	s, ctx := newIntegrationStore(t)

	avatarKey := "avatars/alice.png"
	created, err := s.Create(ctx, "alice@example.com", "Alice Example", &avatarKey)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if created.Email != "alice@example.com" {
		t.Errorf("Create().Email = %q, want %q", created.Email, "alice@example.com")
	}
	if created.Name != "Alice Example" {
		t.Errorf("Create().Name = %q, want %q", created.Name, "Alice Example")
	}

	byID, err := s.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID() error: %v", err)
	}
	assertUserEqual(t, byID, created)

	byEmail, err := s.GetByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("GetByEmail() error: %v", err)
	}
	assertUserEqual(t, byEmail, created)
}

func TestStore_Integration_CreateDuplicateEmailRejected(t *testing.T) {
	t.Parallel()
	s, ctx := newIntegrationStore(t)

	if _, err := s.Create(ctx, "dup@example.com", "First Owner", nil); err != nil {
		t.Fatalf("first Create() error: %v", err)
	}

	_, err := s.Create(ctx, "dup@example.com", "Second Owner", nil)
	if err == nil {
		t.Fatal("second Create() error = nil, want a users_email_key unique-violation error")
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("second Create() error = %v (%T), want a *pgconn.PgError", err, err)
	}
	if pgErr.ConstraintName != "users_email_key" {
		t.Errorf("second Create() violated constraint %q, want %q", pgErr.ConstraintName, "users_email_key")
	}
}

func TestStore_Integration_NotFound(t *testing.T) {
	t.Parallel()
	s, ctx := newIntegrationStore(t)

	tests := []struct {
		name   string
		lookup func() (store.User, error)
	}{
		{
			name:   "by id",
			lookup: func() (store.User, error) { return s.GetByID(ctx, uuid.Nil) },
		},
		{
			name:   "by email",
			lookup: func() (store.User, error) { return s.GetByEmail(ctx, "does-not-exist@example.com") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.lookup()
			if !errors.Is(err, user.ErrNotFound) {
				t.Errorf("error = %v, want user.ErrNotFound", err)
			}
		})
	}
}
