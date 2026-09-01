package oauthsrv

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// This catches an RFC 7009 existence oracle for malformed or unknown values.
func TestRevoke_StrictFormAndUnknownTokenNoop(t *testing.T) {
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	f := newRefreshFixture(t, now, now.Add(refreshFamilyTTL))
	unknownRaw, _, err := NewToken(TokenKindAccess, bytes.NewReader(bytes.Repeat([]byte{17}, 32)))
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}
	for _, raw := range []string{"not-a-token", unknownRaw} {
		unknown := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "https://aboutme.example/oauth/revoke", strings.NewReader("token="+raw))
		unknown.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		f.s.HandleRevoke(w, unknown)
		if w.Code != http.StatusOK || w.Body.Len() != 0 {
			t.Fatal("unknown revoke did not return empty 200")
		}
		var live int
		if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM oauth_tokens WHERE token_digest = $1 AND revoked_at IS NULL", f.digest[:]).Scan(&live); err != nil || live != 1 {
			t.Fatal("unknown revoke changed stored token state")
		}
	}
	wrongMedia := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "https://aboutme.example/oauth/revoke", strings.NewReader("token=x"))
	wrongMedia.Header.Set("Content-Type", "application/x-www-form-urlencoded; charset=utf-8")
	w := httptest.NewRecorder()
	f.s.HandleRevoke(w, wrongMedia)
	if w.Code != http.StatusUnsupportedMediaType || w.Body.String() != `{"error":"invalid_request","error_description":"The request is invalid."}` {
		t.Fatalf("media = %d %q", w.Code, w.Body.String())
	}
}

// Both raw OAuth routes are bearer-world endpoints. This test compares the
// complete no-oracle response rather than relying on a handler implementation
// detail such as never calling r.Cookie.
func TestRevoke_IgnoresHostSessionCookieByteIdentically(t *testing.T) {
	now := time.Date(2026, 9, 3, 11, 0, 0, 0, time.UTC)
	f := newRefreshFixture(t, now, now.Add(refreshFamilyTTL))
	plain := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "https://aboutme.example/oauth/revoke", strings.NewReader("token=not-a-token"))
	plain.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	withCookie := plain.Clone(context.Background())
	withCookie.Body = io.NopCloser(strings.NewReader("token=not-a-token"))
	withCookie.AddCookie(validSessionCookie(t, f.pool, f.userID))
	plainResponse, cookieResponse := httptest.NewRecorder(), httptest.NewRecorder()
	f.s.HandleRevoke(plainResponse, plain)
	f.s.HandleRevoke(cookieResponse, withCookie)
	if plainResponse.Code != cookieResponse.Code || plainResponse.Body.String() != cookieResponse.Body.String() || plainResponse.Header().Get("Content-Type") != cookieResponse.Header().Get("Content-Type") {
		t.Fatal("session cookie changed revoke response")
	}
}

func TestRevoke_ClosedErrorsNeverEchoToken(t *testing.T) {
	now := time.Date(2026, 9, 3, 11, 30, 0, 0, time.UTC)
	f := newRefreshFixture(t, now, now.Add(refreshFamilyTTL))
	body := "token=" + f.raw + "&token=" + f.raw
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "https://aboutme.example/oauth/revoke", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.s.HandleRevoke(w, r)
	if w.Code != http.StatusBadRequest || strings.Contains(w.Body.String(), f.raw) {
		t.Fatal("closed revoke error echoed a token or accepted a duplicate")
	}
}

// This catches revoking just one row instead of the grant and all its families.
// RFC 7009's hint is advisory, so both omission and a wrong known hint must
// revoke the same refresh-token grant without exposing an oracle.
func TestRevoke_ValidAccessOrRefreshRevokesGrantAndEveryAuthority(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		hint string
		kind TokenKind
	}{
		{name: "refresh omitted hint", kind: TokenKindRefresh},
		{name: "refresh mismatched hint", hint: "access_token", kind: TokenKindRefresh},
		{name: "access omitted hint", kind: TokenKindAccess},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRefreshFixture(t, now, now.Add(refreshFamilyTTL))
			ctx := context.Background()
			otherFamily := uuid.New()
			var accessRaw string
			for index, kind := range []TokenKind{TokenKindAccess, TokenKindRefresh, TokenKindAccess} {
				raw, digest, err := NewToken(kind, bytes.NewReader(bytes.Repeat([]byte{byte(index + 31)}, 32)))
				if err != nil {
					t.Fatalf("NewToken: %v", err)
				}
				if index == 0 {
					accessRaw = raw
				}
				familyID := f.familyID
				if index == 2 {
					familyID = otherFamily
				}
				if _, err := f.q.CreateOAuthToken(ctx, store.CreateOAuthTokenParams{TokenDigest: digest[:], Kind: string(kind), FamilyID: familyID, ClientID: f.clientID, UserID: f.userID, GrantID: f.grantID, CreatedAt: now, ExpiresAt: now.Add(time.Hour), FamilyExpiresAt: now.Add(refreshFamilyTTL)}); err != nil {
					t.Fatalf("seed token %d: %v", index, err)
				}
			}
			verifier := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-._~abcdefgh"
			sum := sha256.Sum256([]byte(verifier))
			code, codeDigest, err := NewCode(bytes.NewReader(bytes.Repeat([]byte{61}, 32)))
			if err != nil {
				t.Fatalf("NewCode: %v", err)
			}
			if _, err = f.q.CreateOAuthAuthorizationCode(ctx, store.CreateOAuthAuthorizationCodeParams{CodeDigest: codeDigest[:], ClientID: f.clientID, UserID: f.userID, Scopes: "resumes:read", CodeChallenge: base64.RawURLEncoding.EncodeToString(sum[:]), RedirectURI: "http://127.0.0.1/callback", CreatedAt: now}); err != nil {
				t.Fatalf("CreateOAuthAuthorizationCode: %v", err)
			}
			raw := f.raw
			if tc.kind == TokenKindAccess {
				raw = accessRaw
			}
			body := "token=" + raw
			if tc.hint != "" {
				body += "&token_type_hint=" + tc.hint
			}
			r := httptest.NewRequestWithContext(ctx, http.MethodPost, "https://aboutme.example/oauth/revoke", strings.NewReader(body))
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			f.s.HandleRevoke(w, r)
			if w.Code != http.StatusOK || w.Body.Len() != 0 {
				t.Fatal("valid token revocation did not return empty 200")
			}
			var live int
			if err = f.pool.QueryRow(ctx, "SELECT count(*) FROM oauth_tokens WHERE grant_id = $1 AND revoked_at IS NULL", f.grantID).Scan(&live); err != nil {
				t.Fatalf("live token count: %v", err)
			}
			if live != 0 {
				t.Fatal("grant revocation left a token live")
			}
			grant, err := f.q.GetOAuthGrantForUpdate(ctx, f.grantID)
			if err != nil {
				t.Fatalf("GetOAuthGrantForUpdate: %v", err)
			}
			if grant.RevokedAt == nil {
				t.Fatal("grant remained live after token revocation")
			}
			codeRequest := httptest.NewRequestWithContext(ctx, http.MethodPost, "https://aboutme.example/oauth/token", strings.NewReader("grant_type=authorization_code&code="+code+"&redirect_uri=http%3A%2F%2F127.0.0.1%2Fcallback&client_id="+f.clientID.String()+"&code_verifier="+verifier))
			codeRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			codeResponse := httptest.NewRecorder()
			f.s.HandleToken(codeResponse, codeRequest)
			if codeResponse.Code != http.StatusBadRequest || codeResponse.Body.String() != `{"error":"invalid_grant","error_description":"The request is invalid."}` {
				t.Fatal("revoked grant code retained token authority")
			}
		})
	}
}

func runRevokeRequest(s *Service, raw, hint string) <-chan int {
	result := make(chan int, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		body := "token=" + raw
		if hint != "" {
			body += "&token_type_hint=" + hint
		}
		r := httptest.NewRequestWithContext(ctx, http.MethodPost, "https://aboutme.example/oauth/revoke", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		s.HandleRevoke(w, r)
		result <- w.Code
	}()
	return result
}

func awaitRevokeStatus(t *testing.T, result <-chan int) int {
	t.Helper()
	select {
	case status := <-result:
		return status
	case <-time.After(4 * time.Second):
		t.Fatal("revoke handler did not finish after the database barrier released")
		return 0
	}
}

// These cases lock the user row, which is after the client lock in the frozen
// protocol. PostgreSQL lock observation proves the desired order before the
// blocker is released, rather than relying on scheduler sleeps.
func TestToken_ConcurrentRefreshRotationAndRevokeCannotResurrectGrant(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	t.Run("queued revoke after in-flight rotation leaves no live token", func(t *testing.T) {
		f := newRefreshFixture(t, now, now.Add(refreshFamilyTTL))
		blocker, pid := lockUserForRace(t, f)
		locked := true
		defer func() {
			if locked {
				rollbackTestTx(t, blocker)
			}
		}()
		rotation := runTokenRequest(f.s, "grant_type=refresh_token&refresh_token="+f.raw)
		waitForBlockedTransactions(t, f.pool, pid, 1)
		revocation := runRevokeRequest(f.s, f.raw, "")
		waitForBlockedDescendants(t, f.pool, pid, 2)
		if err := blocker.Commit(context.Background()); err != nil {
			t.Fatalf("release user lock: %v", err)
		}
		locked = false
		if rotationResult := awaitTokenResult(t, rotation); rotationResult.status != http.StatusOK {
			t.Fatalf("in-flight rotation status = %d, want 200", rotationResult.status)
		}
		if revokeStatus := awaitRevokeStatus(t, revocation); revokeStatus != http.StatusOK {
			t.Fatalf("queued revoke status = %d, want 200", revokeStatus)
		}
		assertGrantHasNoLiveTokens(t, f)
	})
	t.Run("committed revoke before waiting rotation creates no successor", func(t *testing.T) {
		f := newRefreshFixture(t, now, now.Add(refreshFamilyTTL))
		blocker, pid := lockUserForRace(t, f)
		locked := true
		defer func() {
			if locked {
				rollbackTestTx(t, blocker)
			}
		}()
		revocation := runRevokeRequest(f.s, f.raw, "")
		waitForBlockedTransactions(t, f.pool, pid, 1)
		rotation := runTokenRequest(f.s, "grant_type=refresh_token&refresh_token="+f.raw)
		waitForBlockedDescendants(t, f.pool, pid, 2)
		if err := blocker.Commit(context.Background()); err != nil {
			t.Fatalf("release user lock: %v", err)
		}
		locked = false
		if revokeStatus := awaitRevokeStatus(t, revocation); revokeStatus != http.StatusOK {
			t.Fatalf("revoke status = %d, want 200", revokeStatus)
		}
		rotationResult := awaitTokenResult(t, rotation)
		if rotationResult.status != http.StatusBadRequest || rotationResult.body != `{"error":"invalid_grant","error_description":"The request is invalid."}` {
			t.Fatal("waiting rotation did not return closed invalid_grant")
		}
		var rows int
		if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM oauth_tokens WHERE grant_id = $1", f.grantID).Scan(&rows); err != nil {
			t.Fatalf("token row count: %v", err)
		}
		if rows != 1 {
			t.Fatal("revoked grant gained a refresh or access successor")
		}
		assertGrantHasNoLiveTokens(t, f)
	})
}

func assertGrantHasNoLiveTokens(t *testing.T, f refreshFixture) {
	t.Helper()
	var live int
	if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM oauth_tokens WHERE grant_id = $1 AND revoked_at IS NULL", f.grantID).Scan(&live); err != nil {
		t.Fatalf("live token count: %v", err)
	}
	if live != 0 {
		t.Fatal("revoked grant retained a live token")
	}
	grant, err := f.q.GetOAuthGrantForUpdate(context.Background(), f.grantID)
	if err != nil {
		t.Fatalf("GetOAuthGrantForUpdate: %v", err)
	}
	if grant.RevokedAt == nil {
		t.Fatal("revoked grant became live")
	}
}
