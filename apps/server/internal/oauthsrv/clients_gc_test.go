package oauthsrv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

func TestClientGC_BoundsProtectsLiveAuthorityAndIsIdempotent(t *testing.T) {
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() error: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	q := store.New(pool)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	var created []uuid.UUID
	createClient := func(createdAt time.Time) store.OAuthClient {
		t.Helper()
		client, createErr := q.CreateOAuthClient(ctx, store.CreateOAuthClientParams{
			ClientName:   "GC " + uuid.NewString(),
			RedirectURIs: json.RawMessage(`["https://agent.example/callback"]`),
			CreatedAt:    createdAt,
		})
		if createErr != nil {
			t.Fatalf("CreateOAuthClient: %v", createErr)
		}
		created = append(created, client.ID)
		return client
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM oauth_clients WHERE id = ANY($1)", created)
	})

	fresh := createClient(now.Add(-24*time.Hour + time.Second))
	exact := createClient(now.Add(-24 * time.Hour))
	stale := createClient(now.Add(-24*time.Hour - time.Second))
	liveGrant := createClient(now.Add(-25 * time.Hour))
	liveToken := createClient(now.Add(-25 * time.Hour))
	batch := make([]store.OAuthClient, 201)
	for i := range batch {
		batch[i] = createClient(now.Add(-48 * time.Hour))
	}

	user, err := q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.test", Name: "GC owner"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
	})
	if _, err := q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: user.ID, ClientID: liveGrant.ID, Scopes: "resumes:read", CreatedAt: now.Add(-25 * time.Hour)}); err != nil {
		t.Fatalf("UpsertOAuthGrant: %v", err)
	}
	tokenGrant, err := q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: user.ID, ClientID: liveToken.ID, Scopes: "resumes:read", CreatedAt: now.Add(-25 * time.Hour)})
	if err != nil {
		t.Fatalf("UpsertOAuthGrant(token): %v", err)
	}
	if _, err := q.RevokeOAuthGrant(ctx, store.RevokeOAuthGrantParams{ID: tokenGrant.ID, RevokedAt: now}); err != nil {
		t.Fatalf("RevokeOAuthGrant(token): %v", err)
	}
	tokenCreatedAt := now.Add(-25 * time.Hour)
	if _, err := q.CreateOAuthToken(ctx, store.CreateOAuthTokenParams{
		TokenDigest: []byte(uuid.NewString() + uuid.NewString())[:32], Kind: "refresh", FamilyID: uuid.New(), ClientID: liveToken.ID, UserID: user.ID, GrantID: tokenGrant.ID,
		CreatedAt: tokenCreatedAt, ExpiresAt: tokenCreatedAt.Add(30 * 24 * time.Hour), FamilyExpiresAt: tokenCreatedAt.Add(30 * 24 * time.Hour),
	}); err != nil {
		t.Fatalf("CreateOAuthToken: %v", err)
	}

	s, err := NewService(ctx, ServiceDependencies{Pool: pool, Queries: q, Clock: func() time.Time { return now }, Entropy: bytes.NewReader(make([]byte, 32)), PublicOrigin: "https://aboutme.example", RegisterAdmission: &registrationAdmissionFake{allowed: true}})
	if err != nil {
		t.Fatalf("NewService startup sweep: %v", err)
	}

	exists := func(id uuid.UUID) bool {
		t.Helper()
		_, getErr := q.GetOAuthClient(ctx, id)
		return !errors.Is(getErr, pgx.ErrNoRows)
	}
	if exists(fresh.ID) {
		// retained exactly inside the 24-hour boundary.
	} else {
		t.Error("client created 24h-1s ago was deleted")
	}
	if !exists(liveGrant.ID) || !exists(liveToken.ID) {
		t.Error("live grant or token client was deleted")
	}
	remainingBatch := 0
	for _, client := range batch {
		if exists(client.ID) {
			remainingBatch++
		}
	}
	if remainingBatch != 1 || !exists(exact.ID) || !exists(stale.ID) {
		t.Errorf("first sweep did not stop at the 200-row cap: batch=%d exact=%t stale=%t", remainingBatch, exists(exact.ID), exists(stale.ID))
	}
	if err := s.CollectIdleClients(ctx); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if err := s.CollectIdleClients(ctx); err != nil {
		t.Fatalf("idempotent empty sweep: %v", err)
	}
	for _, client := range batch {
		if exists(client.ID) {
			t.Error("second sweep did not make monotonic GC progress")
			break
		}
	}
	if !exists(exact.ID) || exists(stale.ID) {
		t.Errorf("strict 24-hour boundary wrong after second sweep: exact=%t stale=%t", exists(exact.ID), exists(stale.ID))
	}
	if !exists(fresh.ID) || !exists(liveGrant.ID) || !exists(liveToken.ID) {
		t.Error("repeat GC deleted protected client")
	}
}
