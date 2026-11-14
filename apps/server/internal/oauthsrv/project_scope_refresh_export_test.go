package oauthsrv

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/store"
)

type ScopedRefreshFixture struct {
	Queries    *store.Queries
	Clock      func() time.Time
	UserID     uuid.UUID
	GrantID    uuid.UUID
	RefreshRaw string
	fixture    refreshFixture
}

type RotatedTokens struct {
	Status       int
	Body         string
	TokenType    string
	Scope        string
	AccessToken  string
	RefreshToken string
}

func NewScopedRefreshFixture(t *testing.T, scopes string) ScopedRefreshFixture {
	t.Helper()
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	f := newRefreshFixture(t, now, now.Add(refreshFamilyTTL))
	grant, err := f.q.UpsertOAuthGrant(context.Background(), store.UpsertOAuthGrantParams{
		UserID: f.userID, ClientID: f.clientID, Scopes: scopes, CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("store a grant holding %q the way consent stores it: %v", scopes, err)
	}
	if grant.ID != f.grantID {
		t.Fatalf("upserted grant %s, want the refresh family's live grant %s", grant.ID, f.grantID)
	}
	return ScopedRefreshFixture{
		Queries: f.q, Clock: f.s.clock, UserID: f.userID, GrantID: f.grantID, RefreshRaw: f.raw, fixture: f,
	}
}

func (f ScopedRefreshFixture) Rotate(t *testing.T, raw string) RotatedTokens {
	t.Helper()
	w, response := f.fixture.rotate(t, raw)
	if w.Code != http.StatusOK {
		return RotatedTokens{Status: w.Code, Body: w.Body.String()}
	}
	return RotatedTokens{
		Status: w.Code, Body: w.Body.String(), TokenType: response.TokenType, Scope: response.Scope,
		AccessToken: response.AccessToken, RefreshToken: response.RefreshToken,
	}
}
