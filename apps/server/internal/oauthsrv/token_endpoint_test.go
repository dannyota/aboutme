package oauthsrv

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

func TestToken_RejectsFormStrictnessWithClosedBody(t *testing.T) {
	s := &Service{}
	cases := []struct {
		name, method, media, body string
		status                    int
	}{
		{"method", http.MethodGet, "application/x-www-form-urlencoded", "grant_type=unsupported", http.StatusMethodNotAllowed},
		{"media", http.MethodPost, "application/x-www-form-urlencoded; charset=utf-8", "grant_type=unsupported", http.StatusUnsupportedMediaType},
		{"duplicate", http.MethodPost, "application/x-www-form-urlencoded", "grant_type=refresh_token&grant_type=refresh_token", http.StatusBadRequest},
		{"unknown", http.MethodPost, "application/x-www-form-urlencoded", "grant_type=unsupported&extra=x", http.StatusBadRequest},
		{"grant", http.MethodPost, "application/x-www-form-urlencoded", "grant_type=unsupported", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(context.Background(), tc.method, "https://aboutme.example/oauth/token", bytes.NewBufferString(tc.body))
			r.Header.Set("Content-Type", tc.media)
			w := httptest.NewRecorder()
			s.HandleToken(w, r)
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d", w.Code, tc.status)
			}
			want := `{"error":"invalid_request","error_description":"The request is invalid."}`
			if tc.name == "grant" {
				want = `{"error":"unsupported_grant_type","error_description":"The request is invalid."}`
			}
			if got := w.Body.String(); got != want {
				t.Errorf("body = %q, want %q", got, want)
			}
		})
	}
}

// This catches a body reader that either accepts 4097 bytes or lets a cookie
// alter the raw bearer-world endpoint.
func TestToken_BodyLimitAndCookieIsolation(t *testing.T) {
	s := &Service{}
	within := "grant_type=unsupported" + strings.Repeat(" ", 4096-len("grant_type=unsupported"))
	over := within + " "
	for _, tc := range []struct{ body, want string }{
		{within, `{"error":"unsupported_grant_type","error_description":"The request is invalid."}`},
		{over, `{"error":"invalid_request","error_description":"The request is invalid."}`},
	} {
		r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "https://aboutme.example/oauth/token", bytes.NewBufferString(tc.body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		s.HandleToken(w, r)
		if w.Body.String() != tc.want {
			t.Fatalf("limit response was not closed")
		}
	}
	plain := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "https://aboutme.example/oauth/token", strings.NewReader("grant_type=unsupported"))
	plain.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	withCookie := plain.Clone(context.Background())
	withCookie.Body = io.NopCloser(strings.NewReader("grant_type=unsupported"))
	withCookie.AddCookie(&http.Cookie{Name: "__Host-session", Value: "sentinel-session"})
	a, b := httptest.NewRecorder(), httptest.NewRecorder()
	s.HandleToken(a, plain)
	s.HandleToken(b, withCookie)
	if a.Code != b.Code || a.Body.String() != b.Body.String() {
		t.Fatal("cookie changed token endpoint result")
	}
}

func validSessionCookie(t *testing.T, pool *store.Pool, userID uuid.UUID) *http.Cookie {
	t.Helper()
	raw, _, err := auth.NewSessionManagerWithPool(pool).Issue(context.Background(), userID, "oauthsrv-test", "127.0.0.1")
	if err != nil {
		t.Fatalf("issue session cookie: %v", err)
	}
	return &http.Cookie{Name: "__Host-session", Value: raw}
}

// This uses a database-backed, live browser-session value. The two full HTTP
// responses must match because a bearer-world endpoint has no session branch.
func TestToken_IgnoresValidHostSessionCookieByteIdentically(t *testing.T) {
	f := newCodeFixture(t, time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC))
	body := "grant_type=unsupported"
	plain := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "https://aboutme.example/oauth/token", strings.NewReader(body))
	plain.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	withCookie := plain.Clone(context.Background())
	withCookie.Body = io.NopCloser(strings.NewReader(body))
	withCookie.AddCookie(validSessionCookie(t, f.pool, f.userID))
	plainResponse, cookieResponse := httptest.NewRecorder(), httptest.NewRecorder()
	f.s.HandleToken(plainResponse, plain)
	f.s.HandleToken(cookieResponse, withCookie)
	if plainResponse.Code != cookieResponse.Code || plainResponse.Body.String() != cookieResponse.Body.String() || plainResponse.Header().Get("Content-Type") != cookieResponse.Header().Get("Content-Type") {
		t.Fatal("valid session cookie changed token response")
	}
}

// Raw OAuth credentials may appear only in the form, never in closed error
// values or bodies. Diagnostics deliberately avoid interpolating them too.
func TestToken_ClosedErrorsNeverEchoCredentials(t *testing.T) {
	f := newCodeFixture(t, time.Date(2026, 9, 1, 11, 30, 0, 0, time.UTC))
	badVerifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~credential-sentinel"
	w := f.exchange(t, f.clientID, "http://127.0.0.1:20090/callback", badVerifier)
	if w.Code != http.StatusBadRequest {
		t.Fatal("bad credential binding did not fail")
	}
	for _, credential := range []string{f.code, badVerifier} {
		if strings.Contains(w.Body.String(), credential) || strings.Contains(errOAuthInvalidGrant.Error(), credential) {
			t.Fatal("closed OAuth error echoed a credential")
		}
	}
}

// This catches a missing consume/issue transaction or a replay branch that
// returns invalid_grant without revoking the family it exposed.
func TestToken_CodeExchangeConsumesAndReplayRevokesFamily(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	s, pool, q := newTokenTestService(t, now)
	ctx := context.Background()
	user, err := q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.test", Name: "Token test"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	client, err := q.CreateOAuthClient(ctx, store.CreateOAuthClientParams{ClientName: "Token test", RedirectURIs: json.RawMessage(`["http://127.0.0.1:20090/callback"]`), CreatedAt: now})
	if err != nil {
		t.Fatalf("CreateOAuthClient: %v", err)
	}
	t.Cleanup(func() {
		cleanupTokenTestClientAndUser(t, pool, client.ID, user.ID)
	})
	grant, err := q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: user.ID, ClientID: client.ID, Scopes: "resumes:read", CreatedAt: now})
	if err != nil {
		t.Fatalf("UpsertOAuthGrant: %v", err)
	}
	code, digest, err := NewCode(bytes.NewReader(bytes.Repeat([]byte{9}, 32)))
	if err != nil {
		t.Fatalf("NewCode: %v", err)
	}
	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~abcdefgh"
	challengeSum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeSum[:])
	if _, err := q.CreateOAuthAuthorizationCode(ctx, store.CreateOAuthAuthorizationCodeParams{CodeDigest: digest[:], ClientID: client.ID, UserID: user.ID, Scopes: "resumes:read", CodeChallenge: challenge, RedirectURI: "http://127.0.0.1:20090/callback", CreatedAt: now}); err != nil {
		t.Fatalf("CreateOAuthAuthorizationCode: %v", err)
	}
	body := "grant_type=authorization_code&code=" + code + "&redirect_uri=http%3A%2F%2F127.0.0.1%3A20090%2Fcallback&client_id=" + client.ID.String() + "&code_verifier=" + verifier
	w := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "https://aboutme.example/oauth/token", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	s.HandleToken(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("exchange status = %d", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	var response tokenResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.ExpiresIn != 3600 || response.Scope != "resumes:read" {
		t.Fatalf("response metadata = expires_in %d, scope %q", response.ExpiresIn, response.Scope)
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequestWithContext(ctx, http.MethodPost, "https://aboutme.example/oauth/token", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	s.HandleToken(w, r)
	if w.Code != http.StatusBadRequest || w.Body.String() != `{"error":"invalid_grant","error_description":"The request is invalid."}` {
		t.Fatalf("replay = %d %q", w.Code, w.Body.String())
	}
	for _, raw := range []string{response.AccessToken, response.RefreshToken} {
		_, tokenDigest, parseErr := ParseToken(raw)
		if parseErr != nil {
			t.Fatalf("ParseToken: %v", parseErr)
		}
		authority, lookupErr := q.GetOAuthTokenAuthorityByDigest(ctx, tokenDigest[:])
		if lookupErr != nil {
			t.Fatalf("token lookup: %v", lookupErr)
		}
		if authority.OAuthToken.RevokedAt == nil || authority.OAuthToken.GrantID != grant.ID {
			t.Fatal("replayed code left a live token")
		}
	}
}

// A consumed code is replay evidence, not fresh authority. Once client,
// redirect, and PKCE still bind it to this request, it must revoke the issued
// family even when mutable grant scopes or the code TTL have changed.
func TestToken_ConsumedReplayRevokesFamilyAfterScopeChangeOrExpiry(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 30, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		mutate func(t *testing.T, f codeFixture)
	}{
		{
			name: "live grant scopes changed",
			mutate: func(t *testing.T, f codeFixture) {
				t.Helper()
				if _, err := f.q.UpsertOAuthGrant(context.Background(), store.UpsertOAuthGrantParams{UserID: f.userID, ClientID: f.clientID, Scopes: "resumes:read resumes:write", CreatedAt: f.now}); err != nil {
					t.Fatalf("change grant scopes: %v", err)
				}
			},
		},
		{
			name: "at code expiry",
			mutate: func(t *testing.T, f codeFixture) {
				t.Helper()
				f.s.clock = func() time.Time { return f.now.Add(60 * time.Second) }
			},
		},
		{
			name: "after code expiry",
			mutate: func(t *testing.T, f codeFixture) {
				t.Helper()
				f.s.clock = func() time.Time { return f.now.Add(60*time.Second + time.Nanosecond) }
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCodeFixture(t, now)
			first := f.exchange(t, f.clientID, "http://127.0.0.1:20090/callback", f.verifier)
			if first.Code != http.StatusOK {
				t.Fatalf("initial exchange status = %d, want 200", first.Code)
			}
			var issued tokenResponse
			if err := json.Unmarshal(first.Body.Bytes(), &issued); err != nil {
				t.Fatalf("decode initial response: %v", err)
			}
			_, refreshDigest, err := ParseToken(issued.RefreshToken)
			if err != nil {
				t.Fatalf("ParseToken: %v", err)
			}
			authority, err := f.q.GetOAuthTokenAuthorityByDigest(context.Background(), refreshDigest[:])
			if err != nil {
				t.Fatalf("GetOAuthTokenAuthorityByDigest: %v", err)
			}
			familyID := authority.OAuthToken.FamilyID
			var initialLive int
			if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM oauth_tokens WHERE family_id = $1 AND revoked_at IS NULL", familyID).Scan(&initialLive); err != nil {
				t.Fatalf("initial live family token count: %v", err)
			}
			if initialLive != 2 {
				t.Fatal("initial exchange did not leave its family live")
			}
			tc.mutate(t, f)
			replay := f.exchange(t, f.clientID, "http://127.0.0.1:20090/callback", f.verifier)
			if replay.Code != http.StatusBadRequest || replay.Body.String() != `{"error":"invalid_grant","error_description":"The request is invalid."}` {
				t.Fatal("consumed replay did not return closed invalid_grant")
			}
			var live int
			if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM oauth_tokens WHERE family_id = $1 AND revoked_at IS NULL", familyID).Scan(&live); err != nil {
				t.Fatalf("live family token count: %v", err)
			}
			if live != 0 {
				t.Fatal("consumed replay left its issued family live")
			}
		})
	}
}

// A mismatched replay is not evidence that the caller holds the original code
// binding. It must remain a closed invalid_grant without becoming a family
// revocation oracle.
func TestToken_ConsumedReplayWrongBindingDoesNotRevokeFamily(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 45, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		exchange func(t *testing.T, f codeFixture) *httptest.ResponseRecorder
	}{
		{
			name: "wrong verifier",
			exchange: func(t *testing.T, f codeFixture) *httptest.ResponseRecorder {
				t.Helper()
				return f.exchange(t, f.clientID, "http://127.0.0.1:20090/callback", "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~different")
			},
		},
		{
			name: "wrong redirect",
			exchange: func(t *testing.T, f codeFixture) *httptest.ResponseRecorder {
				t.Helper()
				return f.exchange(t, f.clientID, "http://127.0.0.1:20090/other", f.verifier)
			},
		},
		{
			name: "wrong existing client",
			exchange: func(t *testing.T, f codeFixture) *httptest.ResponseRecorder {
				t.Helper()
				other, err := f.q.CreateOAuthClient(context.Background(), store.CreateOAuthClientParams{ClientName: "Other replay client", RedirectURIs: json.RawMessage(`["http://127.0.0.1:20090/callback"]`), CreatedAt: f.now})
				if err != nil {
					t.Fatalf("CreateOAuthClient: %v", err)
				}
				return f.exchange(t, other.ID, "http://127.0.0.1:20090/callback", f.verifier)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCodeFixture(t, now)
			first := f.exchange(t, f.clientID, "http://127.0.0.1:20090/callback", f.verifier)
			if first.Code != http.StatusOK {
				t.Fatalf("initial exchange status = %d, want 200", first.Code)
			}
			var issued tokenResponse
			if err := json.Unmarshal(first.Body.Bytes(), &issued); err != nil {
				t.Fatalf("decode initial response: %v", err)
			}
			_, refreshDigest, err := ParseToken(issued.RefreshToken)
			if err != nil {
				t.Fatalf("ParseToken: %v", err)
			}
			authority, err := f.q.GetOAuthTokenAuthorityByDigest(context.Background(), refreshDigest[:])
			if err != nil {
				t.Fatalf("GetOAuthTokenAuthorityByDigest: %v", err)
			}
			replay := tc.exchange(t, f)
			if replay.Code != http.StatusBadRequest || replay.Body.String() != `{"error":"invalid_grant","error_description":"The request is invalid."}` {
				t.Fatal("wrong-bound replay did not return closed invalid_grant")
			}
			var live int
			if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM oauth_tokens WHERE family_id = $1 AND revoked_at IS NULL", authority.OAuthToken.FamilyID).Scan(&live); err != nil {
				t.Fatalf("live family token count: %v", err)
			}
			if live != 2 {
				t.Fatal("wrong-bound replay revoked its issued family")
			}
		})
	}
}

// This catches a grant that is narrowed after code issue being silently
// replaced with the narrower grant while the old code still mints authority.
func TestToken_RejectsCodeWhenGrantScopesChangedAfterIssue(t *testing.T) {
	now := time.Date(2026, 9, 1, 13, 0, 0, 0, time.UTC)
	s, pool, q := newTokenTestService(t, now)
	ctx := context.Background()
	user, err := q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.test", Name: "Scope test"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	client, err := q.CreateOAuthClient(ctx, store.CreateOAuthClientParams{ClientName: "Scope test", RedirectURIs: json.RawMessage(`["http://127.0.0.1:20090/callback"]`), CreatedAt: now})
	if err != nil {
		t.Fatalf("CreateOAuthClient: %v", err)
	}
	t.Cleanup(func() {
		cleanupTokenTestClientAndUser(t, pool, client.ID, user.ID)
	})
	if _, err = q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: user.ID, ClientID: client.ID, Scopes: "resumes:read resumes:write", CreatedAt: now}); err != nil {
		t.Fatalf("seed grant: %v", err)
	}
	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~abcdefgh"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	code, digest, err := NewCode(bytes.NewReader(bytes.Repeat([]byte{3}, 32)))
	if err != nil {
		t.Fatalf("NewCode: %v", err)
	}
	if _, err := q.CreateOAuthAuthorizationCode(ctx, store.CreateOAuthAuthorizationCodeParams{CodeDigest: digest[:], ClientID: client.ID, UserID: user.ID, Scopes: "resumes:read resumes:write", CodeChallenge: challenge, RedirectURI: "http://127.0.0.1:20090/callback", CreatedAt: now}); err != nil {
		t.Fatalf("seed code: %v", err)
	}
	if _, err := q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: user.ID, ClientID: client.ID, Scopes: "resumes:read", CreatedAt: now}); err != nil {
		t.Fatalf("narrow grant: %v", err)
	}
	body := "grant_type=authorization_code&code=" + code + "&redirect_uri=http%3A%2F%2F127.0.0.1%3A20090%2Fcallback&client_id=" + client.ID.String() + "&code_verifier=" + verifier
	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "https://aboutme.example/oauth/token", bytes.NewBufferString(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.HandleToken(w, r)
	if w.Code != http.StatusBadRequest || w.Body.String() != `{"error":"invalid_grant","error_description":"The request is invalid."}` {
		t.Fatalf("scope-changed exchange = %d", w.Code)
	}
}

// This catches refresh reuse failing open or a successor leaving its family.
func TestToken_RefreshRotationChainAndSupersededReuseRevokesFamily(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	s, pool, q := newTokenTestService(t, now)
	ctx := context.Background()
	user, err := q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.test", Name: "Rotate test"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	client, err := q.CreateOAuthClient(ctx, store.CreateOAuthClientParams{ClientName: "Rotate test", RedirectURIs: json.RawMessage(`["http://127.0.0.1/callback"]`), CreatedAt: now})
	if err != nil {
		t.Fatalf("CreateOAuthClient: %v", err)
	}
	t.Cleanup(func() {
		cleanupTokenTestClientAndUser(t, pool, client.ID, user.ID)
	})
	grant, err := q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: user.ID, ClientID: client.ID, Scopes: "resumes:read", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	raw, digest, err := NewToken(TokenKindRefresh, bytes.NewReader(bytes.Repeat([]byte{99}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	family := uuid.New()
	if _, err := q.CreateOAuthToken(ctx, store.CreateOAuthTokenParams{TokenDigest: digest[:], Kind: "refresh", FamilyID: family, ClientID: client.ID, UserID: user.ID, GrantID: grant.ID, CreatedAt: now, ExpiresAt: now.Add(30 * 24 * time.Hour), FamilyExpiresAt: now.Add(30 * 24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	rotate := func(input string) tokenResponse {
		r := httptest.NewRequestWithContext(ctx, http.MethodPost, "https://aboutme.example/oauth/token", strings.NewReader("grant_type=refresh_token&refresh_token="+input))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		s.HandleToken(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("rotation status = %d", w.Code)
		}
		var out tokenResponse
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	first := rotate(raw)
	second := rotate(first.RefreshToken)
	_ = rotate(second.RefreshToken)
	r := httptest.NewRequestWithContext(ctx, http.MethodPost, "https://aboutme.example/oauth/token", strings.NewReader("grant_type=refresh_token&refresh_token="+raw))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.HandleToken(w, r)
	if w.Code != http.StatusBadRequest || w.Body.String() != `{"error":"invalid_grant","error_description":"The request is invalid."}` {
		t.Fatalf("superseded reuse = %d", w.Code)
	}
	var live int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM oauth_tokens WHERE family_id=$1 AND revoked_at IS NULL", family).Scan(&live); err != nil {
		t.Fatal(err)
	}
	if live != 0 {
		t.Fatalf("live family rows = %d", live)
	}
}

type codeFixture struct {
	s              *Service
	pool           *store.Pool
	q              *store.Queries
	clientID       uuid.UUID
	userID         uuid.UUID
	grantID        uuid.UUID
	code, verifier string
	digest         [32]byte
	now            time.Time
}

func newCodeFixture(t *testing.T, now time.Time) codeFixture {
	t.Helper()
	s, pool, q := newTokenTestService(t, now)
	ctx := context.Background()
	user, err := q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.test", Name: "Code matrix"})
	if err != nil {
		t.Fatal(err)
	}
	client, err := q.CreateOAuthClient(ctx, store.CreateOAuthClientParams{ClientName: "Code matrix", RedirectURIs: json.RawMessage(`["http://127.0.0.1:20090/callback"]`), CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupTokenTestClientAndUser(t, pool, client.ID, user.ID)
	})
	grant, err := q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: user.ID, ClientID: client.ID, Scopes: "resumes:read", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~abcdefgh"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	code, digest, err := NewCode(bytes.NewReader(bytes.Repeat([]byte{44}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.CreateOAuthAuthorizationCode(ctx, store.CreateOAuthAuthorizationCodeParams{CodeDigest: digest[:], ClientID: client.ID, UserID: user.ID, Scopes: "resumes:read", CodeChallenge: challenge, RedirectURI: "http://127.0.0.1:20090/callback", CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	return codeFixture{s: s, pool: pool, q: q, clientID: client.ID, userID: user.ID, grantID: grant.ID, code: code, verifier: verifier, digest: digest, now: now}
}

func (f codeFixture) exchange(t *testing.T, clientID uuid.UUID, redirect, verifier string) *httptest.ResponseRecorder {
	t.Helper()
	body := "grant_type=authorization_code&code=" + f.code + "&redirect_uri=" + url.QueryEscape(redirect) + "&client_id=" + clientID.String() + "&code_verifier=" + verifier
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "https://aboutme.example/oauth/token", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.s.HandleToken(w, r)
	return w
}

// This catches validation branches that consume a code or insert tokens before
// rejecting a bad binding, and pins the 60-second exclusive expiry boundary.
func TestToken_CodeBindingAndExpiryMatrixDoesNotConsume(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*codeFixture)
		want   int
		body   string
	}{
		{"wrong verifier", func(f *codeFixture) {
			f.verifier = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~abcdefx"
		}, http.StatusBadRequest, `{"error":"invalid_grant","error_description":"The request is invalid."}`},
		{"unknown client", func(f *codeFixture) { f.clientID = uuid.New() }, http.StatusBadRequest, `{"error":"invalid_client","error_description":"The request is invalid."}`},
		{"wrong redirect", func(f *codeFixture) {}, http.StatusBadRequest, `{"error":"invalid_grant","error_description":"The request is invalid."}`},
		{"expiry exact", func(f *codeFixture) { f.s.clock = func() time.Time { return f.now.Add(60 * time.Second) } }, http.StatusBadRequest, `{"error":"invalid_grant","error_description":"The request is invalid."}`},
		{"expiry after", func(f *codeFixture) {
			f.s.clock = func() time.Time { return f.now.Add(60*time.Second + time.Nanosecond) }
		}, http.StatusBadRequest, `{"error":"invalid_grant","error_description":"The request is invalid."}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newCodeFixture(t, time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC))
			tc.mutate(&f)
			redirect := "http://127.0.0.1:20090/callback"
			if tc.name == "wrong redirect" {
				redirect = "http://127.0.0.1:20090/other"
			}
			w := f.exchange(t, f.clientID, redirect, f.verifier)
			if w.Code != tc.want || w.Body.String() != tc.body {
				t.Fatalf("closed validation response was wrong")
			}
			code, err := f.q.GetOAuthAuthorizationCodeByDigest(context.Background(), f.digest[:])
			if err != nil || code.ConsumedAt != nil {
				t.Fatal("invalid exchange consumed code")
			}
			var n int
			if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM oauth_tokens WHERE client_id=$1", code.ClientID).Scan(&n); err != nil || n != 0 {
				t.Fatal("invalid exchange wrote token")
			}
		})
	}
	t.Run("wrong existing client", func(t *testing.T) {
		f := newCodeFixture(t, time.Date(2026, 9, 4, 13, 0, 0, 0, time.UTC))
		other, err := f.q.CreateOAuthClient(context.Background(), store.CreateOAuthClientParams{ClientName: "Other", RedirectURIs: json.RawMessage(`["http://127.0.0.1:20090/callback"]`), CreatedAt: f.now})
		if err != nil {
			t.Fatal(err)
		}
		w := f.exchange(t, other.ID, "http://127.0.0.1:20090/callback", f.verifier)
		if w.Code != http.StatusBadRequest || w.Body.String() != `{"error":"invalid_grant","error_description":"The request is invalid."}` {
			t.Fatal("existing wrong client did not close")
		}
	})
	t.Run("expiry just before", func(t *testing.T) {
		f := newCodeFixture(t, time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC))
		f.s.clock = func() time.Time { return f.now.Add(60*time.Second - time.Nanosecond) }
		w := f.exchange(t, f.clientID, "http://127.0.0.1:20090/callback", f.verifier)
		if w.Code != http.StatusOK {
			t.Fatalf("pre-expiry status=%d", w.Code)
		}
	})
}

func cleanupTokenTestClientAndUser(t *testing.T, pool *store.Pool, clientID, userID uuid.UUID) {
	t.Helper()
	cleanupCtx := context.Background()
	if _, err := pool.Exec(cleanupCtx, "DELETE FROM oauth_clients WHERE id = $1", clientID); err != nil {
		t.Errorf("cleanup OAuth client: %v", err)
	}
	if _, err := pool.Exec(cleanupCtx, "DELETE FROM users WHERE id = $1", userID); err != nil {
		t.Errorf("cleanup user: %v", err)
	}
}

func newTokenTestService(t *testing.T, now time.Time) (*Service, *store.Pool, *store.Queries) {
	t.Helper()
	pool, err := store.NewPool(context.Background(), testutil.RequireMigratedTestDatabaseURL(t))
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	entropy := make([]byte, 0, 512)
	for block := byte(1); block <= 16; block++ {
		entropy = append(entropy, bytes.Repeat([]byte{block}, 32)...)
	}
	admission := &registrationAdmissionFake{allowed: true}
	s, err := NewService(context.Background(), ServiceDependencies{Pool: pool, Queries: store.New(pool), Clock: func() time.Time { return now }, Entropy: bytes.NewReader(entropy), PublicOrigin: "https://aboutme.example", RegisterAdmission: admission, TokenAdmission: admission, LiveGrantLimit: 10})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return s, pool, store.New(pool)
}

type refreshFixture struct {
	s        *Service
	pool     *store.Pool
	q        *store.Queries
	userID   uuid.UUID
	clientID uuid.UUID
	grantID  uuid.UUID
	familyID uuid.UUID
	raw      string
	digest   [32]byte
	now      time.Time
}

func newRefreshFixture(t *testing.T, now, familyExpiresAt time.Time) refreshFixture {
	t.Helper()
	s, pool, q := newTokenTestService(t, now)
	ctx := context.Background()
	user, err := q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.test", Name: "Refresh matrix"})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	client, err := q.CreateOAuthClient(ctx, store.CreateOAuthClientParams{ClientName: "Refresh matrix", RedirectURIs: json.RawMessage(`["http://127.0.0.1/callback"]`), CreatedAt: now})
	if err != nil {
		t.Fatalf("CreateOAuthClient: %v", err)
	}
	t.Cleanup(func() {
		cleanupTokenTestClientAndUser(t, pool, client.ID, user.ID)
	})
	grant, err := q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: user.ID, ClientID: client.ID, Scopes: "resumes:read", CreatedAt: now})
	if err != nil {
		t.Fatalf("UpsertOAuthGrant: %v", err)
	}
	raw, digest, err := NewToken(TokenKindRefresh, bytes.NewReader(bytes.Repeat([]byte{99}, 32)))
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	familyID := uuid.New()
	if _, err := q.CreateOAuthToken(ctx, store.CreateOAuthTokenParams{TokenDigest: digest[:], Kind: "refresh", FamilyID: familyID, ClientID: client.ID, UserID: user.ID, GrantID: grant.ID, CreatedAt: now, ExpiresAt: familyExpiresAt, FamilyExpiresAt: familyExpiresAt}); err != nil {
		t.Fatalf("CreateOAuthToken: %v", err)
	}
	return refreshFixture{s: s, pool: pool, q: q, userID: user.ID, clientID: client.ID, grantID: grant.ID, familyID: familyID, raw: raw, digest: digest, now: now}
}

func (f refreshFixture) rotate(t *testing.T, raw string) (*httptest.ResponseRecorder, tokenResponse) {
	t.Helper()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "https://aboutme.example/oauth/token", strings.NewReader("grant_type=refresh_token&refresh_token="+raw))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.s.HandleToken(w, r)
	var response tokenResponse
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode rotation response: %v", err)
		}
	}
	return w, response
}

// This catches a rotation that changes its predecessor's authority or lets an
// access response outlive the 30-day family boundary.
func TestToken_RotationPreservesLineageAndClampsExpiry(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	t.Run("ordinary rotation has one-hour access lifetime", func(t *testing.T) {
		f := newRefreshFixture(t, now, now.Add(refreshFamilyTTL))
		w, response := f.rotate(t, f.raw)
		if w.Code != http.StatusOK || response.ExpiresIn != 3600 {
			t.Fatalf("ordinary rotation metadata was not one hour")
		}
	})
	t.Run("three successors retain the original family authority", func(t *testing.T) {
		f := newRefreshFixture(t, now, now.Add(refreshFamilyTTL))
		_, first := f.rotate(t, f.raw)
		_, second := f.rotate(t, first.RefreshToken)
		_, third := f.rotate(t, second.RefreshToken)
		for _, raw := range []string{first.RefreshToken, second.RefreshToken, third.RefreshToken} {
			_, digest, err := ParseToken(raw)
			if err != nil {
				t.Fatalf("ParseToken: %v", err)
			}
			authority, err := f.q.GetOAuthTokenAuthorityByDigest(context.Background(), digest[:])
			if err != nil {
				t.Fatalf("GetOAuthTokenAuthorityByDigest: %v", err)
			}
			token := authority.OAuthToken
			if token.FamilyID != f.familyID || token.ClientID != f.clientID || token.UserID != f.userID || token.GrantID != f.grantID || !token.FamilyExpiresAt.Equal(now.Add(refreshFamilyTTL)) {
				t.Fatal("rotation changed family authority")
			}
		}
		var crossFamily int
		if err := f.pool.QueryRow(context.Background(), `
			SELECT count(*)
			FROM oauth_tokens AS successor
			JOIN oauth_tokens AS predecessor ON predecessor.id = successor.rotated_from
			WHERE successor.family_id <> predecessor.family_id
		`).Scan(&crossFamily); err != nil {
			t.Fatalf("cross-family lineage query: %v", err)
		}
		if crossFamily != 0 {
			t.Fatal("a rotated token crossed families")
		}
	})
	t.Run("near family death clamps access lifetime", func(t *testing.T) {
		familyExpiry := now.Add(1500 * time.Millisecond)
		f := newRefreshFixture(t, now, familyExpiry)
		w, response := f.rotate(t, f.raw)
		if w.Code != http.StatusOK || response.ExpiresIn != 1 {
			t.Fatalf("near-expiry response was not clamped to one second")
		}
		_, accessDigest, err := ParseToken(response.AccessToken)
		if err != nil {
			t.Fatalf("ParseToken: %v", err)
		}
		authority, err := f.q.GetOAuthTokenAuthorityByDigest(context.Background(), accessDigest[:])
		if err != nil {
			t.Fatalf("GetOAuthTokenAuthorityByDigest: %v", err)
		}
		if !authority.OAuthToken.ExpiresAt.Equal(familyExpiry) {
			t.Fatal("access token outlived family expiry")
		}
	})
	for _, tc := range []struct {
		name string
		now  time.Time
	}{
		{name: "at family expiry", now: now.Add(refreshFamilyTTL)},
		{name: "after family expiry", now: now.Add(refreshFamilyTTL + time.Nanosecond)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRefreshFixture(t, now, now.Add(refreshFamilyTTL))
			f.s.clock = func() time.Time { return tc.now }
			w, _ := f.rotate(t, f.raw)
			if w.Code != http.StatusBadRequest || w.Body.String() != `{"error":"invalid_grant","error_description":"The request is invalid."}` {
				t.Fatal("expired family did not return closed invalid_grant")
			}
			var successors int
			if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM oauth_tokens WHERE rotated_from IS NOT NULL AND family_id = $1", f.familyID).Scan(&successors); err != nil {
				t.Fatalf("successor count: %v", err)
			}
			if successors != 0 {
				t.Fatal("expired family created a successor")
			}
		})
	}
}

// waitForBlockedTransactions observes PostgreSQL's actual transaction locks;
// test ordering never relies on a duration-based scheduling guess.
func waitForBlockedTransactions(t *testing.T, pool *store.Pool, holderPID int32, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked int
		err := pool.QueryRow(context.Background(), `
			SELECT count(*)
			FROM pg_locks AS waiting
			JOIN pg_locks AS holding
			  ON holding.locktype = waiting.locktype
			 AND holding.transactionid = waiting.transactionid
			WHERE waiting.locktype = 'transactionid'
			  AND NOT waiting.granted
			  AND holding.granted
			  AND holding.pid = $1
		`, holderPID).Scan(&blocked)
		if err != nil {
			t.Fatalf("observe database lock: %v", err)
		}
		if blocked >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("observed %d blocked transactions, want at least %d", blocked, want)
		}
		<-ticker.C
	}
}

// waitForBlockedDescendants follows transaction-id waits from the fixture's
// external user-lock holder. It therefore proves both endpoint requests are in
// this fixture's lock chain, rather than counting unrelated package activity.
func waitForBlockedDescendants(t *testing.T, pool *store.Pool, holderPID int32, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked int
		err := pool.QueryRow(context.Background(), `
			WITH RECURSIVE descendants(pid) AS (
				SELECT waiting.pid
				FROM pg_locks AS waiting
				JOIN pg_locks AS holding
				  ON holding.locktype = waiting.locktype
				 AND holding.transactionid = waiting.transactionid
				WHERE waiting.locktype = 'transactionid'
				  AND NOT waiting.granted
				  AND holding.granted
				  AND holding.pid = $1
				UNION
				SELECT waiting.pid
				FROM pg_locks AS waiting
				JOIN pg_locks AS holding
				  ON holding.locktype = waiting.locktype
				 AND holding.transactionid = waiting.transactionid
				JOIN descendants ON holding.pid = descendants.pid
				WHERE waiting.locktype = 'transactionid'
				  AND NOT waiting.granted
				  AND holding.granted
			)
			SELECT count(*) FROM descendants
		`, holderPID).Scan(&blocked)
		if err != nil {
			t.Fatalf("observe fixture lock chain: %v", err)
		}
		if blocked >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("observed %d fixture lock descendants, want at least %d", blocked, want)
		}
		<-ticker.C
	}
}

func lockUserForRace(t *testing.T, f refreshFixture) (pgx.Tx, int32) {
	t.Helper()
	tx, err := f.pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin race lock transaction: %v", err)
	}
	if _, err := tx.Exec(context.Background(), "SELECT id FROM users WHERE id = $1 FOR UPDATE", f.userID); err != nil {
		rollbackTestTx(t, tx)
		t.Fatalf("lock user: %v", err)
	}
	var pid int32
	if err := tx.QueryRow(context.Background(), "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		rollbackTestTx(t, tx)
		t.Fatalf("lock transaction pid: %v", err)
	}
	return tx, pid
}

func rollbackTestTx(t *testing.T, tx pgx.Tx) {
	t.Helper()
	if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Errorf("rollback transaction: %v", err)
	}
}

type tokenHandlerResult struct {
	status int
	body   string
}

func runTokenRequest(s *Service, body string) <-chan tokenHandlerResult {
	result := make(chan tokenHandlerResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		r := httptest.NewRequestWithContext(ctx, http.MethodPost, "https://aboutme.example/oauth/token", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		s.HandleToken(w, r)
		result <- tokenHandlerResult{status: w.Code, body: w.Body.String()}
	}()
	return result
}

func awaitTokenResult(t *testing.T, result <-chan tokenHandlerResult) tokenHandlerResult {
	t.Helper()
	select {
	case response := <-result:
		return response
	case <-time.After(4 * time.Second):
		t.Fatal("token handler did not finish after the database barrier released")
		return tokenHandlerResult{}
	}
}

// This catches a second code exchange escaping the conditional consume. The
// user lock creates a deterministic queue: one exchange holds the client lock
// while waiting for user, then the other waits behind that client lock.
func TestToken_ConcurrentCodeExchangeHasOneSuccessAndRevokesReplayFamily(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	f := newCodeFixture(t, now)
	lock, err := f.pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin lock transaction: %v", err)
	}
	locked := true
	defer func() {
		if locked {
			rollbackTestTx(t, lock)
		}
	}()
	if _, err := lock.Exec(context.Background(), "SELECT id FROM users WHERE id = $1 FOR UPDATE", f.userID); err != nil {
		rollbackTestTx(t, lock)
		t.Fatalf("lock user: %v", err)
	}
	var pid int32
	if err := lock.QueryRow(context.Background(), "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		rollbackTestTx(t, lock)
		t.Fatalf("lock pid: %v", err)
	}
	body := "grant_type=authorization_code&code=" + f.code + "&redirect_uri=http%3A%2F%2F127.0.0.1%3A20090%2Fcallback&client_id=" + f.clientID.String() + "&code_verifier=" + f.verifier
	first := runTokenRequest(f.s, body)
	waitForBlockedTransactions(t, f.pool, pid, 1)
	second := runTokenRequest(f.s, body)
	waitForBlockedDescendants(t, f.pool, pid, 2)
	if err := lock.Commit(context.Background()); err != nil {
		t.Fatalf("release user lock: %v", err)
	}
	locked = false
	firstResult, secondResult := awaitTokenResult(t, first), awaitTokenResult(t, second)
	if (firstResult.status == http.StatusOK) == (secondResult.status == http.StatusOK) || (firstResult.status != http.StatusBadRequest && secondResult.status != http.StatusBadRequest) {
		t.Fatalf("concurrent exchange statuses = %d and %d, want one success and one closed failure", firstResult.status, secondResult.status)
	}
	for _, result := range []tokenHandlerResult{firstResult, secondResult} {
		if result.status == http.StatusBadRequest && result.body != `{"error":"invalid_grant","error_description":"The request is invalid."}` {
			t.Fatal("concurrent replay did not return closed invalid_grant")
		}
	}
	var live int
	if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM oauth_tokens WHERE grant_id = $1 AND revoked_at IS NULL", f.grantID).Scan(&live); err != nil {
		t.Fatalf("live token count: %v", err)
	}
	if live != 0 {
		t.Fatal("code replay defense left an issued family live")
	}
}
