package user_test

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/user"
)

// Compile-time assertions that the generated shapes match what internal/user
// and internal/auth build on. A failure here means schema.sql, queries.sql,
// or the sqlc.yaml overrides drifted from what later tasks expect. The
// field-level lines pin the nullable-column contract (native pointers, per
// the design decision above), not just the type names — including the
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
	// RotatedFrom (fix round 3, DD-C14c): the exact rotation-lineage FK a
	// successor row carries back to its predecessor -- additive to the
	// original Task 0.3 pin set above, same nullable-native-pointer
	// contract as every other nullable column here.
	_ *uuid.UUID = store.Session{}.RotatedFrom
	_ string     = store.OAuthTransaction{}.PKCEVerifier
	_ string     = store.OAuthTransaction{}.RedirectURI
	_ *uuid.UUID = store.OAuthTransaction{}.LinkingUserID
)

// TestSchema_PreservesAuthConstraints is a unit test (no database) that
// guards two constraints load-bearing for later auth tasks: an accidental
// drop of either from sql/schema.sql would silently defeat "one identity
// per provider subject" (AC-AUTH-004/005) or the link/reauth-must-name-a-
// user invariant, without any compile-time or generated-code signal (sqlc
// generates fine either way; only a live constraint violation would catch
// it, and only if a test happens to exercise that exact path).
func TestSchema_PreservesAuthConstraints(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../../sql/schema.sql")
	if err != nil {
		t.Fatalf("ReadFile(sql/schema.sql) error: %v", err)
	}
	schema := string(data)

	for _, constraint := range []string{
		"identities_provider_subject_key",
		"oauth_transactions_link_needs_user",
	} {
		if !strings.Contains(schema, constraint) {
			t.Errorf("sql/schema.sql missing constraint %q", constraint)
		}
	}
}

// requireTestDatabaseURL returns TEST_DATABASE_URL, skipping the calling
// test (not failing it) when unset -- UNLESS REQUIRE_TEST_DB=1 is also set
// in the environment, in which case a missing TEST_DATABASE_URL is a hard
// t.Fatal instead, matching internal/auth's own requireTestDatabaseURL
// (transaction_test.go): a gate run (`make server-test-db`, which sets
// REQUIRE_TEST_DB=1) must never pass vacuously with this test silently
// skipped.
func requireTestDatabaseURL(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("REQUIRE_TEST_DB") == "1" {
			t.Fatal("REQUIRE_TEST_DB=1 is set but TEST_DATABASE_URL is unset; refusing to silently skip this live-database test")
		}
		t.Skip("TEST_DATABASE_URL not set; skipping live-database integration test")
	}
	return dsn
}

// newIntegrationStore returns a user.Store backed by a fresh transaction
// against TEST_DATABASE_URL, rolled back automatically when the test
// finishes so repeated runs against a persistent test database never
// accumulate rows or collide on the unique-email constraint. It skips the
// test if TEST_DATABASE_URL is unset, matching internal/store's own
// integration test so `go test ./...` stays fully hermetic by default.
func newIntegrationStore(t *testing.T) (*user.Store, context.Context) {
	t.Helper()

	dsn := requireTestDatabaseURL(t)

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
