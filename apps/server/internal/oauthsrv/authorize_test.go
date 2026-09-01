package oauthsrv

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

func newAuthorizeHarness(t *testing.T) (*Service, *store.Queries, store.OAuthClient, store.User) {
	t.Helper()
	ctx := context.Background()
	pool, err := store.NewPool(ctx, testutil.RequireMigratedTestDatabaseURL(t))
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	q := store.New(pool)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	user, err := q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.test", Name: "Authorize owner"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	client, err := q.CreateOAuthClient(ctx, store.CreateOAuthClientParams{
		ClientName: "Authorize agent", RedirectURIs: json.RawMessage(`["https://agent.example/callback?fixed=yes"]`), CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateOAuthClient: %v", err)
	}
	t.Cleanup(func() {
		if _, cleanupErr := q.DeleteOAuthClient(context.Background(), client.ID); cleanupErr != nil {
			t.Errorf("DeleteOAuthClient cleanup: %v", cleanupErr)
		}
		if _, cleanupErr := pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID); cleanupErr != nil {
			t.Errorf("Delete user cleanup: %v", cleanupErr)
		}
	})
	admission := &registrationAdmissionFake{allowed: true}
	s, err := NewService(ctx, ServiceDependencies{
		Pool: pool, Queries: q, Clock: func() time.Time { return now },
		Entropy: testEntropy(32 * 32), PublicOrigin: "https://aboutme.example",
		RegisterAdmission: admission, TokenAdmission: admission, LiveGrantLimit: 10,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return s, q, client, user
}

func testEntropy(bytesNeeded int) *bytes.Reader {
	buf := make([]byte, bytesNeeded)
	for i := range buf {
		buf[i] = byte(i)
	}
	return bytes.NewReader(buf)
}

func authorizeURL(clientID uuid.UUID, scope string) string {
	q := url.Values{
		"client_id":             {clientID.String()},
		"redirect_uri":          {"https://agent.example/callback?fixed=yes"},
		"response_type":         {"code"},
		"scope":                 {scope},
		"state":                 {"opaque<&state"},
		"code_challenge":        {strings.Repeat("A", 43)},
		"code_challenge_method": {"S256"},
	}
	return "https://aboutme.example/oauth/authorize?" + q.Encode()
}

func TestAuthorize_ValidationAndSessionBranches(t *testing.T) {
	s, q, client, user := newAuthorizeHarness(t)

	t.Run("untrusted redirect is closed", func(t *testing.T) {
		raw := strings.Replace(authorizeURL(client.ID, "resumes:read"), url.QueryEscape("https://agent.example/callback?fixed=yes"), url.QueryEscape("https://agent.example.evil/callback"), 1)
		rec := httptest.NewRecorder()
		s.HandleAuthorize(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, raw, nil))
		if rec.Code != http.StatusBadRequest || rec.Header().Get("Location") != "" {
			t.Fatalf("response = %d location %q, want closed 400", rec.Code, rec.Header().Get("Location"))
		}
	})

	t.Run("trusted invalid request redirects OAuth error", func(t *testing.T) {
		raw := strings.Replace(authorizeURL(client.ID, "resumes:read"), "response_type=code", "response_type=token", 1)
		rec := httptest.NewRecorder()
		s.HandleAuthorize(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, raw, nil))
		if rec.Code != http.StatusFound {
			t.Fatalf("status = %d, want 302", rec.Code)
		}
		got, err := url.Parse(rec.Header().Get("Location"))
		if err != nil {
			t.Fatalf("parse redirect: %v", err)
		}
		if got.String() == "https://agent.example/callback?fixed=yes" || got.Query().Get("error") != "invalid_request" || got.Query().Get("state") != "opaque<&state" {
			t.Fatalf("trusted error redirect = %q", got.String())
		}
	})

	t.Run("no session preserves validated request in login next", func(t *testing.T) {
		raw := authorizeURL(client.ID, "resumes:read")
		rec := httptest.NewRecorder()
		s.HandleAuthorize(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, raw, nil))
		if rec.Code != http.StatusFound {
			t.Fatalf("status = %d, want 302", rec.Code)
		}
		got, err := url.Parse(rec.Header().Get("Location"))
		if err != nil || got.Path != "/login" || got.Query().Get("next") != "/oauth/authorize?"+strings.SplitN(raw, "?", 2)[1] {
			t.Fatalf("login redirect = %q", rec.Header().Get("Location"))
		}
	})

	t.Run("session without grant goes to stateless consent", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, authorizeURL(client.ID, "resumes:read"), nil)
		req = req.WithContext(auth.ContextWithSession(req.Context(), store.Session{UserID: user.ID}))
		rec := httptest.NewRecorder()
		s.HandleAuthorize(rec, req)
		if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/authorize?") {
			t.Fatalf("response = %d %q, want consent redirect", rec.Code, rec.Header().Get("Location"))
		}
	})

	for _, tc := range []struct {
		name       string
		grantScope string
		request    string
		wantCode   bool
	}{
		{"equal grant skips consent", "resumes:read", "resumes:read", true},
		{"wider grant skips consent", "resumes:read resumes:write", "resumes:read", true},
		{"narrower grant requires consent", "resumes:read", "resumes:read resumes:write", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := q.UpsertOAuthGrant(context.Background(), store.UpsertOAuthGrantParams{UserID: user.ID, ClientID: client.ID, Scopes: tc.grantScope, CreatedAt: time.Now().UTC()}); err != nil {
				t.Fatalf("seed grant: %v", err)
			}
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, authorizeURL(client.ID, tc.request), nil)
			req = req.WithContext(auth.ContextWithSession(req.Context(), store.Session{UserID: user.ID}))
			rec := httptest.NewRecorder()
			s.HandleAuthorize(rec, req)
			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302; body=%q location=%q", rec.Code, rec.Body.String(), rec.Header().Get("Location"))
			}
			if tc.wantCode && urlMustParse(t, rec.Header().Get("Location")).Query().Get("code") == "" {
				t.Fatalf("redirect = %q, want code", rec.Header().Get("Location"))
			}
			if tc.name == "wider grant skips consent" {
				codeRaw := urlMustParse(t, rec.Header().Get("Location")).Query().Get("code")
				digest, err := ParseCode(codeRaw)
				if err != nil {
					t.Fatalf("ParseCode: %v", err)
				}
				code, err := q.GetOAuthAuthorizationCodeByDigest(context.Background(), digest[:])
				if err != nil || code.Scopes != "resumes:read" {
					t.Fatalf("narrower request code scopes = %q, %v; want resumes:read", code.Scopes, err)
				}
				if got := mustLiveGrant(t, q, user.ID, client.ID).Scopes; got != "resumes:read" {
					t.Fatalf("live grant after narrower request = %q, want resumes:read", got)
				}
			}
			if !tc.wantCode && !strings.HasPrefix(rec.Header().Get("Location"), "/authorize?") {
				t.Fatalf("redirect = %q, want consent", rec.Header().Get("Location"))
			}
			if _, err := q.RevokeOAuthGrant(context.Background(), store.RevokeOAuthGrantParams{ID: mustLiveGrant(t, q, user.ID, client.ID).ID, RevokedAt: time.Now().UTC()}); err != nil {
				t.Fatalf("revoke seeded grant: %v", err)
			}
		})
	}
}

func TestAuthorizeInternalRedirectRejectsExternalTarget(t *testing.T) {
	rec := httptest.NewRecorder()
	redirectInternal(rec, "//evil.example")
	if rec.Code != http.StatusInternalServerError || rec.Header().Get("Location") != "" {
		t.Fatalf("response = %d location %q, want closed 500", rec.Code, rec.Header().Get("Location"))
	}
}

func TestAuthorize_TrustedAndUntrustedValidationMatrix(t *testing.T) {
	s, _, client, _ := newAuthorizeHarness(t)
	valid := urlMustParse(t, authorizeURL(client.ID, "resumes:read"))
	set := func(key, value string) string {
		clone := *valid
		values := clone.Query()
		if value == "" {
			values.Del(key)
		} else {
			values.Set(key, value)
		}
		clone.RawQuery = values.Encode()
		return clone.String()
	}
	for _, tc := range []struct {
		name       string
		raw        string
		status     int
		wantError  string
		wantClosed bool
	}{
		{"unknown client", set("client_id", uuid.NewString()), http.StatusBadRequest, "", true},
		{"malformed client", set("client_id", "not-a-uuid"), http.StatusBadRequest, "", true},
		{"substring redirect", set("redirect_uri", "https://agent.example/callback.evil?fixed=yes"), http.StatusBadRequest, "", true},
		{"suffix redirect", set("redirect_uri", "https://agent.example/callback?fixed=yes.evil"), http.StatusBadRequest, "", true},
		{"case variant redirect", set("redirect_uri", "https://AGENT.example/callback?fixed=yes"), http.StatusBadRequest, "", true},
		{"userinfo redirect", set("redirect_uri", "https://agent.example@evil.example/callback"), http.StatusBadRequest, "", true},
		{"encoded slash redirect", set("redirect_uri", "https://agent.example%2F.evil/callback"), http.StatusBadRequest, "", true},
		{"response type", set("response_type", "token"), http.StatusFound, "invalid_request", false},
		{"invalid scope", set("scope", "resumes:admin"), http.StatusFound, "invalid_scope", false},
		{"noncanonical scope", set("scope", "resumes:write resumes:read"), http.StatusFound, "invalid_request", false},
		{"missing challenge", set("code_challenge", ""), http.StatusFound, "invalid_request", false},
		{"plain PKCE", set("code_challenge_method", "plain"), http.StatusFound, "invalid_request", false},
		{"invalid challenge", set("code_challenge", strings.Repeat("*", 43)), http.StatusFound, "invalid_request", false},
		{"state over bound", set("state", strings.Repeat("s", stateMaxBytes+1)), http.StatusFound, "invalid_request", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.HandleAuthorize(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, tc.raw, nil))
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d; body=%q", rec.Code, tc.status, rec.Body.String())
			}
			if tc.wantClosed && rec.Header().Get("Location") != "" {
				t.Fatalf("untrusted failure redirected to %q", rec.Header().Get("Location"))
			}
			if tc.wantError != "" {
				redirect := urlMustParse(t, rec.Header().Get("Location"))
				wantState := "opaque<&state"
				if tc.name == "state over bound" {
					wantState = ""
				}
				if redirect.Query().Get("error") != tc.wantError || redirect.Query().Get("state") != wantState {
					t.Fatalf("redirect = %q, want %s with state", redirect, tc.wantError)
				}
			}
		})
	}
}

func urlMustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", raw, err)
	}
	return u
}

func mustLiveGrant(t *testing.T, q *store.Queries, userID, clientID uuid.UUID) store.OAuthGrant {
	t.Helper()
	grant, err := q.GetLiveOAuthGrant(context.Background(), store.GetLiveOAuthGrantParams{UserID: userID, ClientID: clientID})
	if err != nil {
		t.Fatalf("GetLiveOAuthGrant: %v", err)
	}
	return grant
}
