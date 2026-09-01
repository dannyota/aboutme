package mcpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/oauthsrv"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

const bearerPublicOrigin = "https://aboutme.example"

type bearerHarness struct {
	bearer *Bearer
	clock  *testutil.Clock
	pool   *store.Pool
	q      *store.Queries
	user   store.User
	client store.OAuthClient
	grant  store.OAuthGrant
}

func newBearerHarness(t *testing.T, scopes string) *bearerHarness {
	t.Helper()
	ctx := context.Background()
	pool, err := store.NewPool(ctx, testutil.RequireMigratedTestDatabaseURL(t))
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	q := store.New(pool)
	clock := testutil.NewClock(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
	user, err := q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.test", Name: "Bearer owner"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	var client store.OAuthClient
	var grant store.OAuthGrant
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		qtx := store.New(tx)
		var createErr error
		client, createErr = qtx.CreateOAuthClient(ctx, store.CreateOAuthClientParams{
			ClientName: "Bearer agent", RedirectURIs: json.RawMessage(`["https://agent.example/callback"]`), CreatedAt: clock.Now(),
		})
		if createErr != nil {
			return createErr
		}
		grant, createErr = qtx.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{
			UserID: user.ID, ClientID: client.ID, Scopes: scopes, CreatedAt: clock.Now(),
		})
		return createErr
	})
	if err != nil {
		t.Fatalf("create bearer authority: %v", err)
	}
	t.Cleanup(func() {
		if _, cleanupErr := q.DeleteOAuthClient(context.Background(), client.ID); cleanupErr != nil {
			t.Errorf("delete OAuth client: %v", cleanupErr)
		}
		if _, cleanupErr := pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID); cleanupErr != nil {
			t.Errorf("delete user: %v", cleanupErr)
		}
	})
	bearer, err := NewBearer(BearerDependencies{Queries: q, Clock: clock.Now, PublicOrigin: bearerPublicOrigin})
	if err != nil {
		t.Fatalf("NewBearer: %v", err)
	}
	return &bearerHarness{bearer: bearer, clock: clock, pool: pool, q: q, user: user, client: client, grant: grant}
}

func (h *bearerHarness) createToken(t *testing.T, kind oauthsrv.TokenKind) (string, store.OAuthToken) {
	t.Helper()
	raw, digest, err := oauthsrv.NewToken(kind, bytes.NewReader(bytes.Repeat([]byte{kind[0]}, 32)))
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	now := h.clock.Now()
	token, err := h.q.CreateOAuthToken(context.Background(), store.CreateOAuthTokenParams{
		TokenDigest: digest[:], Kind: string(kind), FamilyID: uuid.New(), ClientID: h.client.ID, UserID: h.user.ID, GrantID: h.grant.ID,
		CreatedAt: now, ExpiresAt: now.Add(time.Hour), FamilyExpiresAt: now.Add(30 * 24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateOAuthToken: %v", err)
	}
	return raw, token
}

func bearerRequest(headers ...string) *http.Request {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, bearerPublicOrigin+"/mcp", nil)
	for _, header := range headers {
		r.Header.Add("Authorization", header)
	}
	return r
}

func assertClosedUnauthorized(t *testing.T, err error) {
	t.Helper()
	recorder := httptest.NewRecorder()
	writeMCPError(recorder, err)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
	if got := recorder.Header().Get("WWW-Authenticate"); got != `Bearer resource_metadata="https://aboutme.example/.well-known/oauth-protected-resource"` {
		t.Fatalf("WWW-Authenticate = %q", got)
	}
	if got := recorder.Body.String(); got != `{"error":"unauthorized"}` {
		t.Fatalf("body = %q", got)
	}
}

func TestBearer_RejectsEveryInvalidAuthorityWithOneClosedResponse(t *testing.T) {
	cases := []struct {
		name    string
		headers func(access, refresh, unknown string) []string
		prepare func(t *testing.T, h *bearerHarness, token store.OAuthToken)
	}{
		{"absent", func(_, _, _ string) []string { return nil }, nil},
		{"duplicate", func(access, _, _ string) []string { return []string{"Bearer " + access, "Bearer " + access} }, nil},
		{"malformed", func(_, _, _ string) []string { return []string{"Bearer amat_not-a-token"} }, nil},
		{"wrong prefix", func(access, _, _ string) []string { return []string{"Basic " + access} }, nil},
		{"lowercase scheme", func(access, _, _ string) []string { return []string{"bearer " + access} }, nil},
		{"double space", func(access, _, _ string) []string { return []string{"Bearer  " + access} }, nil},
		{"trailing space", func(access, _, _ string) []string { return []string{"Bearer " + access + " "} }, nil},
		{"empty bearer", func(_, _, _ string) []string { return []string{"Bearer "} }, nil},
		{"refresh", func(_, refresh, _ string) []string { return []string{"Bearer " + refresh} }, nil},
		{"unknown", func(_, _, unknown string) []string { return []string{"Bearer " + unknown} }, nil},
		{"expired one nanosecond after", func(access, _, _ string) []string { return []string{"Bearer " + access} }, func(t *testing.T, h *bearerHarness, token store.OAuthToken) {
			t.Helper()
			h.clock.Set(token.ExpiresAt.Add(time.Nanosecond))
		}},
		{"revoked", func(access, _, _ string) []string { return []string{"Bearer " + access} }, func(t *testing.T, h *bearerHarness, token store.OAuthToken) {
			t.Helper()
			h.clock.Set(token.CreatedAt)
			if _, err := h.pool.Exec(context.Background(), "UPDATE oauth_tokens SET revoked_at = $1 WHERE id = $2", token.CreatedAt, token.ID); err != nil {
				t.Fatalf("revoke token: %v", err)
			}
		}},
		{"superseded", func(access, _, _ string) []string { return []string{"Bearer " + access} }, func(t *testing.T, h *bearerHarness, token store.OAuthToken) {
			t.Helper()
			h.clock.Set(token.CreatedAt)
			if _, err := h.pool.Exec(context.Background(), "UPDATE oauth_tokens SET superseded_at = $1 WHERE id = $2", token.CreatedAt, token.ID); err != nil {
				t.Fatalf("supersede token: %v", err)
			}
		}},
		{"grant revoked", func(access, _, _ string) []string { return []string{"Bearer " + access} }, func(t *testing.T, h *bearerHarness, _ store.OAuthToken) {
			t.Helper()
			if _, err := h.pool.Exec(context.Background(), "UPDATE oauth_grants SET revoked_at = $1 WHERE id = $2", h.clock.Now(), h.grant.ID); err != nil {
				t.Fatalf("revoke grant: %v", err)
			}
		}},
		{"stored access digest marked refresh", func(access, _, _ string) []string { return []string{"Bearer " + access} }, func(t *testing.T, h *bearerHarness, token store.OAuthToken) {
			t.Helper()
			if _, err := h.pool.Exec(context.Background(), "UPDATE oauth_tokens SET kind = 'refresh' WHERE id = $1", token.ID); err != nil {
				t.Fatalf("change stored kind: %v", err)
			}
		}},
		{"grant client differs", func(access, _, _ string) []string { return []string{"Bearer " + access} }, func(t *testing.T, h *bearerHarness, token store.OAuthToken) {
			t.Helper()
			other, err := h.q.CreateOAuthClient(context.Background(), store.CreateOAuthClientParams{ClientName: "Other bearer agent", RedirectURIs: json.RawMessage(`["https://other.example/callback"]`), CreatedAt: h.clock.Now()})
			if err != nil {
				t.Fatalf("CreateOAuthClient other: %v", err)
			}
			t.Cleanup(func() {
				if _, cleanupErr := h.q.DeleteOAuthClient(context.Background(), other.ID); cleanupErr != nil {
					t.Errorf("delete other OAuth client: %v", cleanupErr)
				}
			})
			if _, err := h.pool.Exec(context.Background(), "UPDATE oauth_tokens SET client_id = $1 WHERE id = $2", other.ID, token.ID); err != nil {
				t.Fatalf("make client authority inconsistent: %v", err)
			}
		}},
	}

	var wantResponse struct {
		status int
		header string
		body   string
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newBearerHarness(t, "resumes:read")
			access, token := h.createToken(t, oauthsrv.TokenKindAccess)
			refresh, _ := h.createToken(t, oauthsrv.TokenKindRefresh)
			unknown, _, err := oauthsrv.NewToken(oauthsrv.TokenKindAccess, bytes.NewReader(bytes.Repeat([]byte{99}, 32)))
			if err != nil {
				t.Fatalf("NewToken unknown: %v", err)
			}
			headers := tc.headers(access, refresh, unknown)
			if tc.prepare != nil {
				tc.prepare(t, h, token)
			}
			_, err = h.bearer.Authenticate(bearerRequest(headers...))
			if err == nil {
				t.Fatal("Authenticate error = nil, want closed unauthorized")
			}
			for _, header := range headers {
				if strings.Contains(err.Error(), header) {
					t.Fatal("Authenticate error echoes the presented authorization header")
				}
			}
			assertClosedUnauthorized(t, err)
			recorder := httptest.NewRecorder()
			writeMCPError(recorder, err)
			got := struct {
				status int
				header string
				body   string
			}{recorder.Code, recorder.Header().Get("WWW-Authenticate"), recorder.Body.String()}
			if wantResponse.body == "" {
				wantResponse = got
			} else if got != wantResponse {
				t.Fatal("closed unauthorized response differs between invalid classes")
			}
		})
	}
}

func TestBearer_RejectsExpirationBoundariesAndInconsistentAuthority(t *testing.T) {
	for _, tc := range []struct {
		name         string
		offset       time.Duration
		inconsistent bool
		wantOK       bool
	}{
		{"one nanosecond before expiry", time.Hour - time.Nanosecond, false, true},
		{"at expiry", time.Hour, false, false},
		{"one nanosecond after expiry", time.Hour + time.Nanosecond, false, false},
		{"grant user differs", 0, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newBearerHarness(t, "resumes:read")
			raw, token := h.createToken(t, oauthsrv.TokenKindAccess)
			if tc.inconsistent {
				other, err := h.q.CreateUser(context.Background(), store.CreateUserParams{Email: uuid.NewString() + "@example.test", Name: "Other owner"})
				if err != nil {
					t.Fatalf("CreateUser other: %v", err)
				}
				t.Cleanup(func() {
					if _, cleanupErr := h.pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", other.ID); cleanupErr != nil {
						t.Errorf("delete other user: %v", cleanupErr)
					}
				})
				if _, err := h.pool.Exec(context.Background(), "UPDATE oauth_tokens SET user_id = $1 WHERE id = $2", other.ID, token.ID); err != nil {
					t.Fatalf("make authority inconsistent: %v", err)
				}
			}
			h.clock.Set(token.CreatedAt.Add(tc.offset))
			principal, err := h.bearer.Authenticate(bearerRequest("Bearer " + raw))
			if !tc.wantOK {
				assertClosedUnauthorized(t, err)
				return
			}
			if err != nil || principal.UserID != h.user.ID || principal.GrantID != h.grant.ID || principal.TokenID != token.ID || principal.Scopes.String() != "resumes:read" {
				t.Fatalf("principal = %#v, err = %v", principal, err)
			}
		})
	}
}

func TestBearer_IgnoresSessionCookieAndTouchesOncePerMinute(t *testing.T) {
	h := newBearerHarness(t, "resumes:read")
	raw, token := h.createToken(t, oauthsrv.TokenKindAccess)
	for _, advance := range []time.Duration{0, 30 * time.Second, 31 * time.Second} {
		if advance > 0 {
			h.clock.Advance(advance)
		}
		bearerOnly, bearerErr := h.bearer.Authenticate(bearerRequest("Bearer " + raw))
		cookieRequest := bearerRequest("Bearer " + raw)
		cookieRequest.Header.Set("Cookie", "__Host-session=must-not-be-read")
		withCookie, cookieErr := h.bearer.Authenticate(cookieRequest)
		if bearerErr != nil || cookieErr != nil {
			t.Fatalf("Authenticate errors = %v, %v", bearerErr, cookieErr)
		}
		if bearerOnly.UserID != withCookie.UserID || bearerOnly.GrantID != withCookie.GrantID || bearerOnly.TokenID != withCookie.TokenID || bearerOnly.Scopes.String() != withCookie.Scopes.String() {
			t.Fatal("session cookie changed bearer principal")
		}
		authority, err := h.q.GetOAuthTokenAuthorityByDigest(context.Background(), token.TokenDigest)
		if err != nil {
			t.Fatalf("GetOAuthTokenAuthorityByDigest: %v", err)
		}
		want := token.CreatedAt
		if advance == 31*time.Second {
			want = token.CreatedAt.Add(61 * time.Second)
		}
		if authority.OAuthToken.LastUsedAt == nil || !authority.OAuthToken.LastUsedAt.Equal(want) {
			t.Fatalf("last_used_at = %v, want %v", authority.OAuthToken.LastUsedAt, want)
		}
	}
}

func TestRequireScope_ReadWriteMatrix(t *testing.T) {
	read, err := oauthsrv.ParseScopes("resumes:read")
	if err != nil {
		t.Fatal(err)
	}
	write, err := oauthsrv.ParseScopes("resumes:read resumes:write")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		scopes oauthsrv.Scopes
		need   oauthsrv.Scope
		allow  bool
	}{
		{"read token reads", read, oauthsrv.ScopeResumesRead, true},
		{"read token cannot write", read, oauthsrv.ScopeResumesWrite, false},
		{"write token reads", write, oauthsrv.ScopeResumesRead, true},
		{"write token writes", write, oauthsrv.ScopeResumesWrite, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := RequireScope(Principal{Scopes: tc.scopes}, tc.need)
			if tc.allow && err != nil {
				t.Fatalf("RequireScope: %v", err)
			}
			if !tc.allow {
				recorder := httptest.NewRecorder()
				writeMCPError(recorder, err)
				if recorder.Code != http.StatusForbidden || recorder.Body.String() != `{"error":"scope_denied"}` {
					t.Fatalf("response = %d %q", recorder.Code, recorder.Body.String())
				}
			}
		})
	}
}
