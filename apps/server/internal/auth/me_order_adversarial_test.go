package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

func TestMeIdentityOrderAdversarial(t *testing.T) {
	ctx := context.Background()
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })

	q := store.New(pool)
	svc, err := auth.NewService(testLogger(), config.Config{PublicOrigin: testPublicOrigin}, pool)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	handler := api.New(testLogger(), noopPinger{}, api.Options{}, nil, svc.RegisterRoutes)

	userID := createTestUser(t, q)
	t.Cleanup(func() {
		if _, cleanupErr := pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID); cleanupErr != nil {
			t.Errorf("delete test user: %v", cleanupErr)
		}
	})

	lowerID := userID
	lowerID[0] = 0x00
	higherID := userID
	higherID[0] = 0xff
	createdAt := time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC)

	fixtures := []struct {
		id       uuid.UUID
		provider auth.Provider
	}{
		{id: higherID, provider: auth.ProviderGitHub},
		{id: lowerID, provider: auth.ProviderGoogle},
	}
	for _, fixture := range fixtures {
		if _, err := pool.Exec(ctx, `
			INSERT INTO identities (id, user_id, provider, provider_user_id, created_at)
			VALUES ($1, $2, $3, $4, $5)
		`, fixture.id, userID, string(fixture.provider), string(fixture.provider)+"-subject-"+userID.String(), createdAt); err != nil {
			t.Fatalf("insert %s identity: %v", fixture.provider, err)
		}
	}

	rawSession, _ := issueTestSession(t, q, userID)
	resp := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", sessionRequestCookie(rawSession)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /me status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body meEnvelopeBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode GET /me response: %v", err)
	}
	if len(body.Data.Identities) != 2 {
		t.Fatalf("GET /me identities = %+v, want exactly 2", body.Data.Identities)
	}

	want := []string{string(auth.ProviderGoogle), string(auth.ProviderGitHub)}
	for i, provider := range want {
		if got := body.Data.Identities[i].Provider; got != provider {
			t.Fatalf("GET /me identities[%d].provider = %q, want %q; equal-time identities must follow (created_at, id), not insertion order", i, got, provider)
		}
	}
}
