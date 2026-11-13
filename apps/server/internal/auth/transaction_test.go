package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

func newTestPool(t *testing.T) *store.Pool {
	t.Helper()
	dsn := testutil.RequireMigratedTestDatabaseURL(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })

	return pool
}

func newTestQueries(t *testing.T) *store.Queries {
	t.Helper()
	return store.New(newTestPool(t))
}

func createTestUser(t *testing.T, q *store.Queries) uuid.UUID {
	t.Helper()

	user, err := q.CreateUser(context.Background(), store.CreateUserParams{
		Email: uuid.NewString() + "@example.com",
		Name:  "Test User",
	})
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	return user.ID
}

func TestTransactionStore_BeginThenConsume_ReturnsSameData(t *testing.T) {
	q := newTestQueries(t)
	ts := auth.NewTransactionStore(q)
	ctx := context.Background()

	handle, tx, err := ts.Begin(ctx, auth.ProviderGoogle, auth.PurposeLogin, uuid.Nil, "https://aboutme.vn/api/v1/auth/google/callback")
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if len(handle) != 43 {
		t.Errorf("Begin() handle length = %d, want 43 (32 raw bytes, base64.RawURLEncoding)", len(handle))
	}
	if tx.PKCEVerifier == "" {
		t.Error("Begin() tx.PKCEVerifier is empty, want non-empty")
	}
	if tx.Nonce == "" {
		t.Error("Begin() tx.Nonce is empty, want non-empty (google requires an OIDC nonce)")
	}
	if tx.State == "" {
		t.Error("Begin() tx.State is empty, want non-empty")
	}
	if tx.Provider != auth.ProviderGoogle {
		t.Errorf("Begin() tx.Provider = %q, want %q", tx.Provider, auth.ProviderGoogle)
	}
	if tx.Purpose != auth.PurposeLogin {
		t.Errorf("Begin() tx.Purpose = %q, want %q", tx.Purpose, auth.PurposeLogin)
	}
	if tx.LinkingUserID != uuid.Nil {
		t.Errorf("Begin() tx.LinkingUserID = %v, want uuid.Nil for PurposeLogin", tx.LinkingUserID)
	}

	got, err := ts.Consume(ctx, handle, auth.ProviderGoogle)
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if got.State != tx.State {
		t.Errorf("Consume() State = %q, want %q", got.State, tx.State)
	}
	if got.PKCEVerifier != tx.PKCEVerifier {
		t.Errorf("Consume() PKCEVerifier = %q, want %q", got.PKCEVerifier, tx.PKCEVerifier)
	}
	if got.Nonce != tx.Nonce {
		t.Errorf("Consume() Nonce = %q, want %q", got.Nonce, tx.Nonce)
	}
	if got.RedirectURI != tx.RedirectURI {
		t.Errorf("Consume() RedirectURI = %q, want %q", got.RedirectURI, tx.RedirectURI)
	}
}

func TestTransactionStore_BeginThenConsume_GitHubHasNoNonce(t *testing.T) {
	q := newTestQueries(t)
	ts := auth.NewTransactionStore(q)
	ctx := context.Background()

	_, tx, err := ts.Begin(ctx, auth.ProviderGitHub, auth.PurposeLogin, uuid.Nil, "https://aboutme.vn/api/v1/auth/github/callback")
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if tx.Nonce != "" {
		t.Errorf("Begin() tx.Nonce = %q, want empty for ProviderGitHub", tx.Nonce)
	}
}

func TestTransactionStore_BeginThenConsume_LinkCarriesLinkingUserID(t *testing.T) {
	q := newTestQueries(t)
	ts := auth.NewTransactionStore(q)
	ctx := context.Background()

	linkingUserID := createTestUser(t, q)
	handle, tx, err := ts.Begin(ctx, auth.ProviderLinkedIn, auth.PurposeLink, linkingUserID, "https://aboutme.vn/api/v1/auth/linkedin/callback")
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if tx.LinkingUserID != linkingUserID {
		t.Fatalf("Begin() tx.LinkingUserID = %v, want %v", tx.LinkingUserID, linkingUserID)
	}

	got, err := ts.Consume(ctx, handle, auth.ProviderLinkedIn)
	if err != nil {
		t.Fatalf("Consume() error = %v", err)
	}
	if got.LinkingUserID != linkingUserID {
		t.Errorf("Consume() LinkingUserID = %v, want %v", got.LinkingUserID, linkingUserID)
	}
	if got.Purpose != auth.PurposeLink {
		t.Errorf("Consume() Purpose = %q, want %q", got.Purpose, auth.PurposeLink)
	}
}
