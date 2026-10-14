package oauthsrv

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func TestCIMDIdleSweepKeepsLiveGrantAndCollectsIdleURLClient(t *testing.T) {
	s, q, user, _ := newAuthorizeHarness(t)
	ctx := context.Background()
	clients := make([]store.OAuthClient, 0, 2)
	for range 2 {
		client, err := q.UpsertOAuthMetadataClient(ctx, store.UpsertOAuthMetadataClientParams{
			MetadataUrl: "https://client.example/" + uuid.NewString(), ClientName: "Fixture",
			RedirectURIs: []byte(`["http://localhost/callback"]`), CreatedAt: s.clock().Add(-25 * time.Hour),
		})
		if err != nil {
			t.Fatal(err)
		}
		clients = append(clients, client)
		t.Cleanup(func() {
			if _, err := q.DeleteOAuthClient(context.Background(), client.ID); err != nil {
				t.Error(err)
			}
		})
	}
	if _, err := q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: user.ID, ClientID: clients[1].ID, Scopes: "resumes:read", CreatedAt: s.clock()}); err != nil {
		t.Fatal(err)
	}
	if err := s.CollectIdleClients(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := q.GetOAuthClient(ctx, clients[0].ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("idle URL client survived sweep")
	}
	if _, err := q.GetOAuthClient(ctx, clients[1].ID); err != nil {
		t.Fatal("live URL grant lost its client", err)
	}
}
