package oauthsrv

import (
	"bytes"
	"context"
	"crypto/rand"
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

// issueTestSession issues a real database-backed session for userID and pins
// its primary and, when factorVerifiedAt is non-nil, second-factor
// verification timestamps to deterministic values, so a reauthentication-
// window check compares against the fixture's injected clock rather than the
// real wall clock the session manager's default Issue path would use.
func issueTestSession(t *testing.T, pool *store.Pool, userID uuid.UUID, now time.Time, factorVerifiedAt *time.Time) store.Session {
	t.Helper()
	_, sess, err := auth.NewSessionManagerWithPool(pool).Issue(t.Context(), userID, "oauthsrv-test", "127.0.0.1")
	if err != nil {
		t.Fatalf("issue test session: %v", err)
	}
	if _, err = pool.Exec(t.Context(), "UPDATE sessions SET reauthenticated_at = $1, second_factor_verified_at = $2 WHERE id = $3", now, factorVerifiedAt, sess.ID); err != nil {
		t.Fatalf("set test session verification times: %v", err)
	}
	sess.ReauthenticatedAt = now
	sess.SecondFactorVerifiedAt = factorVerifiedAt
	return sess
}

// enrollTestSecondFactor gives userID a second-factor policy so the recent
// reauthentication gate requires both primary and factor proof. The random
// handle carries no meaning beyond the required-unique 32-byte shape.
func enrollTestSecondFactor(t *testing.T, q *store.Queries, userID uuid.UUID, now time.Time) {
	t.Helper()
	handle := make([]byte, 32)
	if _, err := rand.Read(handle); err != nil {
		t.Fatalf("generate test factor handle: %v", err)
	}
	if _, err := q.CreateSecondFactorPolicy(context.Background(), store.CreateSecondFactorPolicyParams{
		UserID: userID, WebauthnUserHandle: handle, EnabledAt: now,
	}); err != nil {
		t.Fatalf("CreateSecondFactorPolicy: %v", err)
	}
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
		if got.String() == "https://agent.example/callback?fixed=yes" || got.Query().Get("error") != "invalid_request" || got.Query().Get("state") != "opaque<&state" || got.Query().Get("iss") != "https://aboutme.example" || got.Query().Get("fixed") != "yes" {
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
			sess := issueTestSession(t, s.pool, user.ID, s.clock(), nil)
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, authorizeURL(client.ID, tc.request), nil)
			req = req.WithContext(auth.ContextWithSession(req.Context(), sess))
			rec := httptest.NewRecorder()
			s.HandleAuthorize(rec, req)
			if rec.Code != http.StatusFound {
				t.Fatalf("status = %d, want 302; body=%q location=%q", rec.Code, rec.Body.String(), rec.Header().Get("Location"))
			}
			if tc.wantCode {
				redirect := urlMustParse(t, rec.Header().Get("Location"))
				if redirect.Query().Get("code") == "" || redirect.Query().Get("iss") != "https://aboutme.example" || redirect.Query().Get("fixed") != "yes" {
					t.Fatalf("redirect = %q, want code, canonical issuer, and registered query", rec.Header().Get("Location"))
				}
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

// This catches the silent-reuse path minting a code from a live grant without
// the recent second-factor proof an enrolled account needs, and pins the
// grant and code it does issue to the account's current authentication
// epoch. See docs/design/second-factor-authentication.md.
func TestAuthorize_SilentReuseRequiresRecentSecondFactorProof(t *testing.T) {
	s, q, client, user := newAuthorizeHarness(t)
	enrollTestSecondFactor(t, q, user.ID, s.clock())
	if _, err := q.UpsertOAuthGrant(t.Context(), store.UpsertOAuthGrantParams{UserID: user.ID, ClientID: client.ID, Scopes: "resumes:read", CreatedAt: s.clock()}); err != nil {
		t.Fatalf("seed grant: %v", err)
	}

	t.Run("missing factor proof falls back to interactive consent", func(t *testing.T) {
		sess := issueTestSession(t, s.pool, user.ID, s.clock(), nil)
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, authorizeURL(client.ID, "resumes:read"), nil)
		req = req.WithContext(auth.ContextWithSession(req.Context(), sess))
		rec := httptest.NewRecorder()
		s.HandleAuthorize(rec, req)
		if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/authorize?") {
			t.Fatalf("response = %d %q, want interactive consent redirect", rec.Code, rec.Header().Get("Location"))
		}
	})

	t.Run("stale account epoch falls back to interactive consent", func(t *testing.T) {
		now := s.clock()
		sess := issueTestSession(t, s.pool, user.ID, now, &now)
		if _, err := q.AdvanceUserAuthEpoch(t.Context(), user.ID); err != nil {
			t.Fatalf("AdvanceUserAuthEpoch: %v", err)
		}
		t.Cleanup(func() {
			if _, err := s.pool.Exec(context.Background(), "UPDATE users SET auth_epoch = 0 WHERE id = $1", user.ID); err != nil {
				t.Errorf("restore user epoch: %v", err)
			}
		})
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, authorizeURL(client.ID, "resumes:read"), nil)
		req = req.WithContext(auth.ContextWithSession(req.Context(), sess))
		rec := httptest.NewRecorder()
		s.HandleAuthorize(rec, req)
		if rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/authorize?") {
			t.Fatalf("response = %d %q, want interactive consent redirect", rec.Code, rec.Header().Get("Location"))
		}
	})

	t.Run("both recent proofs mint a code bound to the current epoch", func(t *testing.T) {
		now := s.clock()
		sess := issueTestSession(t, s.pool, user.ID, now, &now)
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, authorizeURL(client.ID, "resumes:read"), nil)
		req = req.WithContext(auth.ContextWithSession(req.Context(), sess))
		rec := httptest.NewRecorder()
		s.HandleAuthorize(rec, req)
		if rec.Code != http.StatusFound {
			t.Fatalf("status = %d, want 302; body=%q", rec.Code, rec.Body.String())
		}
		codeRaw := urlMustParse(t, rec.Header().Get("Location")).Query().Get("code")
		if codeRaw == "" {
			t.Fatalf("redirect = %q, want code", rec.Header().Get("Location"))
		}
		digest, err := ParseCode(codeRaw)
		if err != nil {
			t.Fatalf("ParseCode: %v", err)
		}
		code, err := q.GetOAuthAuthorizationCodeByDigest(t.Context(), digest[:])
		if err != nil {
			t.Fatalf("issued code lookup: %v", err)
		}
		grant := mustLiveGrant(t, q, user.ID, client.ID)
		if code.GrantID != grant.ID || code.AuthEpoch != 0 || grant.AuthEpoch != 0 {
			t.Fatalf("code = %#v, grant = %#v, want both bound to epoch 0", code, grant)
		}
	})
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
				if redirect.Query().Get("error") != tc.wantError || redirect.Query().Get("state") != wantState || redirect.Query().Get("iss") != "https://aboutme.example" || redirect.Query().Get("fixed") != "yes" {
					t.Fatalf("redirect = %q, want %s with state", redirect, tc.wantError)
				}
			}
		})
	}
}

func TestAuthorize_ResourceCompatibility(t *testing.T) {
	s, q, client, user := newAuthorizeHarness(t)
	base := urlMustParse(t, authorizeURL(client.ID, "resumes:read"))
	if _, err := q.UpsertOAuthGrant(context.Background(), store.UpsertOAuthGrantParams{UserID: user.ID, ClientID: client.ID, Scopes: "resumes:read", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatalf("seed grant: %v", err)
	}
	for _, tc := range []struct {
		name  string
		raw   string
		valid bool
	}{
		{"missing", base.String(), true},
		{"canonical", base.String() + "&resource=https%3A%2F%2Faboutme.example", true},
		{"empty", base.String() + "&resource=", false},
		{"duplicate", base.String() + "&resource=https%3A%2F%2Faboutme.example&resource=https%3A%2F%2Faboutme.example", false},
		{"encoded", base.String() + "&resource=https%3A%2F%2Faboutme%2Eexample", false},
		{"path", base.String() + "&resource=https%3A%2F%2Faboutme.example%2Fmcp", false},
		{"other origin", base.String() + "&resource=https%3A%2F%2Fagent.example", false},
		{"malformed only", base.String() + "&resource=%ZZ", false},
		{"canonical and malformed duplicate", base.String() + "&resource=https%3A%2F%2Faboutme.example&resource=%ZZ", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var before int
			if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM oauth_authorization_codes WHERE client_id = $1", client.ID).Scan(&before); err != nil {
				t.Fatalf("count codes before: %v", err)
			}
			sess := issueTestSession(t, s.pool, user.ID, s.clock(), nil)
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, tc.raw, nil)
			req = req.WithContext(auth.ContextWithSession(req.Context(), sess))
			rec := httptest.NewRecorder()
			s.HandleAuthorize(rec, req)
			if tc.valid {
				if rec.Code != http.StatusFound || urlMustParse(t, rec.Header().Get("Location")).Query().Get("code") == "" {
					t.Fatalf("valid resource response = %d %q", rec.Code, rec.Header().Get("Location"))
				}
			} else if rec.Code != http.StatusFound || urlMustParse(t, rec.Header().Get("Location")).Query().Get("error") != "invalid_request" {
				t.Fatalf("invalid resource response = %d %q", rec.Code, rec.Header().Get("Location"))
			}
			var after int
			if err := s.pool.QueryRow(context.Background(), "SELECT count(*) FROM oauth_authorization_codes WHERE client_id = $1", client.ID).Scan(&after); err != nil {
				t.Fatalf("count codes after: %v", err)
			}
			if !tc.valid && after != before {
				t.Fatalf("invalid resource issued %d codes", after-before)
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
	grant, err := q.GetLiveOAuthGrant(t.Context(), store.GetLiveOAuthGrantParams{UserID: userID, ClientID: clientID})
	if err != nil {
		t.Fatalf("GetLiveOAuthGrant: %v", err)
	}
	return grant
}
