package oauthsrv

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const revokeClaudeClientID = "https://claude.ai/oauth/claude-code-client-metadata"

type revokeFixture struct {
	refreshFixture
	publicID  string
	accessRaw string
	code      string
	verifier  string
	otherID   uuid.UUID
	untouched []uuid.UUID
}

func newRevokeFixture(t *testing.T, mode string) revokeFixture {
	t.Helper()
	now := cimdTestNow()
	f := revokeFixture{refreshFixture: newRefreshFixture(t, now, now.Add(refreshFamilyTTL))}
	f.publicID = f.clientID.String()
	if mode != "dcr" {
		f.publicID = revokeClaudeClientID
		if _, err := f.pool.Exec(t.Context(), "UPDATE oauth_clients SET metadata_url=$1 WHERE id=$2", f.publicID, f.clientID); err != nil {
			t.Fatal(err)
		}
	}
	f.s.cimdEnabled = mode == "cimd on"
	resolver, err := NewCIMDResolver(cimdTestConfig(), f.s.publicOrigin, func() time.Time {
		panic("revocation accessed metadata cache")
	}, 10)
	if err != nil {
		t.Fatal(err)
	}
	resolver.lookup = func(context.Context, string) ([]netip.Addr, error) {
		panic("revocation performed DNS")
	}
	resolver.dialAddress = func(context.Context, string, string) (net.Conn, error) {
		panic("revocation fetched metadata")
	}
	f.s.cimd = resolver
	otherFamily := uuid.New()
	for index, kind := range []TokenKind{TokenKindAccess, TokenKindRefresh, TokenKindAccess} {
		raw, digest, tokenErr := NewToken(kind, bytes.NewReader(bytes.Repeat([]byte{byte(index + 100)}, 32)))
		if tokenErr != nil {
			t.Fatal(tokenErr)
		}
		family := otherFamily
		if index == 0 {
			f.accessRaw, family = raw, f.familyID
		}
		if _, tokenErr = f.q.CreateOAuthToken(t.Context(), store.CreateOAuthTokenParams{
			TokenDigest: digest[:], Kind: string(kind), FamilyID: family, ClientID: f.clientID,
			UserID: f.userID, GrantID: f.grantID, CreatedAt: now,
			ExpiresAt: now.Add(time.Hour), FamilyExpiresAt: now.Add(refreshFamilyTTL),
		}); tokenErr != nil {
			t.Fatal(tokenErr)
		}
	}
	other, err := f.q.CreateOAuthClient(t.Context(), store.CreateOAuthClientParams{
		ClientName: "Other agent", RedirectURIs: []byte(`["https://other.example/callback"]`), CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.otherID = other.ID
	t.Cleanup(func() {
		if _, cleanupErr := f.q.DeleteOAuthClient(context.Background(), other.ID); cleanupErr != nil {
			t.Error(cleanupErr)
		}
	})
	second, err := f.q.UpsertOAuthGrant(t.Context(), store.UpsertOAuthGrantParams{
		UserID: f.userID, ClientID: other.ID, Scopes: "resumes:read", CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	owner, err := f.q.CreateUser(t.Context(), store.CreateUserParams{Email: uuid.NewString() + "@example.test", Name: "Other owner"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, cleanupErr := f.pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", owner.ID); cleanupErr != nil {
			t.Error(cleanupErr)
		}
	})
	shared, err := f.q.UpsertOAuthGrant(t.Context(), store.UpsertOAuthGrantParams{
		UserID: owner.ID, ClientID: f.clientID, Scopes: "resumes:read", CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.untouched = []uuid.UUID{second.ID, shared.ID}
	for index, grant := range []store.OAuthGrant{second, shared} {
		_, digest, tokenErr := NewToken(TokenKindRefresh, bytes.NewReader(bytes.Repeat([]byte{byte(index + 110)}, 32)))
		if tokenErr != nil {
			t.Fatal(tokenErr)
		}
		if _, tokenErr = f.q.CreateOAuthToken(t.Context(), store.CreateOAuthTokenParams{
			TokenDigest: digest[:], Kind: "refresh", FamilyID: uuid.New(), ClientID: grant.ClientID,
			UserID: grant.UserID, GrantID: grant.ID, CreatedAt: now,
			ExpiresAt: now.Add(refreshFamilyTTL), FamilyExpiresAt: now.Add(refreshFamilyTTL),
		}); tokenErr != nil {
			t.Fatal(tokenErr)
		}
	}
	f.verifier = strings.Repeat("v", 43)
	sum := sha256.Sum256([]byte(f.verifier))
	f.code, _, err = NewCode(bytes.NewReader(bytes.Repeat([]byte{121}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	codeDigest, err := ParseCode(f.code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.q.CreateOAuthAuthorizationCode(t.Context(), store.CreateOAuthAuthorizationCodeParams{
		CodeDigest: codeDigest[:], ClientID: f.clientID, UserID: f.userID, GrantID: &f.grantID,
		Scopes: "resumes:read", CodeChallenge: base64.RawURLEncoding.EncodeToString(sum[:]),
		RedirectURI: "http://127.0.0.1/callback", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	return f
}

func postRevoke(s *Service, body string, headers http.Header) *httptest.ResponseRecorder {
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "https://aboutme.example/oauth/revoke", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for name, values := range headers {
		r.Header[name] = append([]string(nil), values...)
	}
	w := httptest.NewRecorder()
	s.HandleRevoke(w, r)
	return w
}

func assertEmptyRevoke(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Code != http.StatusOK || w.Body.Len() != 0 || len(w.Header()) != 0 {
		t.Fatal("revocation did not return the fixed empty 200 and application headers")
	}
}

func (f revokeFixture) assertIsolation(t *testing.T) {
	t.Helper()
	for _, id := range f.untouched {
		grant, err := f.q.GetOAuthGrantForUpdate(t.Context(), id)
		if err != nil || grant.RevokedAt != nil {
			t.Fatal("revocation changed another grant", err)
		}
		var live int
		if err = f.pool.QueryRow(t.Context(), "SELECT count(*) FROM oauth_tokens WHERE grant_id=$1 AND revoked_at IS NULL", id).Scan(&live); err != nil || live != 1 {
			t.Fatal("revocation changed another grant's token", err)
		}
	}
}

func (f revokeFixture) assertCannotIssue(t *testing.T) {
	t.Helper()
	refresh, _ := f.rotate(t, f.raw)
	assertClosedInvalidGrant(t, refresh)
	form := url.Values{
		"grant_type": {"authorization_code"}, "code": {f.code}, "client_id": {f.publicID},
		"redirect_uri": {"http://127.0.0.1/callback"}, "code_verifier": {f.verifier},
	}
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://aboutme.example/oauth/token", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.s.HandleToken(w, r)
	assertClosedInvalidGrant(t, w)
}

func TestRevokeClaimSelectsNoAuthority(t *testing.T) {
	for _, mode := range []string{"dcr", "cimd on", "cimd off"} {
		for _, kind := range []TokenKind{TokenKindAccess, TokenKindRefresh} {
			for _, claim := range []string{"omitted", "matching", "other UUID", "unknown URL", "existing URL", "internal UUID", "UUID spelling", "encoded URL"} {
				t.Run(mode+"/"+string(kind)+"/"+claim, func(t *testing.T) {
					f := newRevokeFixture(t, mode)
					form := url.Values{"token": {f.raw}}
					if kind == TokenKindAccess {
						form.Set("token", f.accessRaw)
					}
					id := ""
					switch claim {
					case "matching":
						id = f.publicID
					case "other UUID":
						id = f.otherID.String()
					case "unknown URL":
						id = "https://unknown.example/synthetic-sensitive-path"
					case "existing URL":
						id = "https://other.example/metadata"
						if _, err := f.pool.Exec(t.Context(), "UPDATE oauth_clients SET metadata_url=$1 WHERE id=$2", id, f.otherID); err != nil {
							t.Fatal(err)
						}
					case "internal UUID":
						id = f.clientID.String()
					case "UUID spelling":
						id = "urn:uuid:" + strings.ToUpper(f.clientID.String())
					case "encoded URL":
						id = strings.Replace(revokeClaudeClientID, "/oauth/", "/%6fauth/", 1)
					}
					if claim != "omitted" {
						form.Set("client_id", id)
					}
					assertEmptyRevoke(t, postRevoke(f.s, form.Encode(), nil))
					assertGrantHasNoLiveTokens(t, f.refreshFixture)
					f.assertIsolation(t)
					f.assertCannotIssue(t)
				})
			}
		}
	}
}

func TestRevokeIgnoresEveryHint(t *testing.T) {
	for _, mode := range []string{"dcr", "cimd on", "cimd off"} {
		for _, kind := range []TokenKind{TokenKindAccess, TokenKindRefresh} {
			for _, hint := range []string{"omitted", "", "id_token", "access_token", "refresh_token"} {
				t.Run(mode+"/"+string(kind)+"/"+hint, func(t *testing.T) {
					f := newRevokeFixture(t, mode)
					form := url.Values{"token": {f.raw}, "client_id": {f.publicID}}
					if kind == TokenKindAccess {
						form.Set("token", f.accessRaw)
					}
					if hint != "omitted" {
						form.Set("token_type_hint", hint)
					}
					assertEmptyRevoke(t, postRevoke(f.s, form.Encode(), nil))
					assertGrantHasNoLiveTokens(t, f.refreshFixture)
					f.assertIsolation(t)
				})
			}
		}
	}
}

func TestRevokeClaudeLogoutSequenceAndReplay(t *testing.T) {
	for _, mode := range []string{"cimd on", "cimd off"} {
		for _, reverse := range []bool{false, true} {
			t.Run(mode+"/reverse="+strconv.FormatBool(reverse), func(t *testing.T) {
				f := newRevokeFixture(t, mode)
				forms := []url.Values{
					{"token": {f.raw}, "token_type_hint": {"refresh_token"}, "client_id": {revokeClaudeClientID}},
					{"token": {f.accessRaw}, "token_type_hint": {"access_token"}, "client_id": {revokeClaudeClientID}},
				}
				if reverse {
					forms[0], forms[1] = forms[1], forms[0]
				}
				for range 2 {
					for _, form := range forms {
						assertEmptyRevoke(t, postRevoke(f.s, form.Encode(), nil))
						assertGrantHasNoLiveTokens(t, f.refreshFixture)
						f.assertIsolation(t)
					}
				}
				f.assertCannotIssue(t)
				fresh, err := f.q.UpsertOAuthGrant(t.Context(), store.UpsertOAuthGrantParams{
					UserID: f.userID, ClientID: f.clientID, Scopes: "resumes:read", CreatedAt: f.now,
				})
				if err != nil || fresh.ID == f.grantID {
					t.Fatal("reauthorization did not create a distinct grant", err)
				}
				assertEmptyRevoke(t, postRevoke(f.s, forms[0].Encode(), nil))
				live, err := f.q.GetLiveOAuthGrant(t.Context(), store.GetLiveOAuthGrantParams{UserID: f.userID, ClientID: f.clientID})
				if err != nil || live.ID != fresh.ID {
					t.Fatal("old token revoked the replacement grant", err)
				}
			})
		}
	}
}

func TestRevokeResponsesMatchAcrossTokenStatesAndAmbientCredentials(t *testing.T) {
	baseline := postRevoke(&Service{}, "token=synthetic-malformed", nil)
	assertEmptyRevoke(t, baseline)
	for _, mode := range []string{"dcr", "cimd on", "cimd off"} {
		for _, kind := range []TokenKind{TokenKindAccess, TokenKindRefresh} {
			for _, state := range []string{"live", "expired", "rotated", "revoked", "unknown", "malformed"} {
				t.Run(mode+"/"+string(kind)+"/"+state, func(t *testing.T) {
					f := newRevokeFixture(t, mode)
					otherOwnerGrant, grantErr := f.q.GetOAuthGrantForUpdate(t.Context(), f.untouched[1])
					if grantErr != nil {
						t.Fatal(grantErr)
					}
					cookie := validSessionCookie(t, f.pool, otherOwnerGrant.UserID)
					if state == "expired" {
						f.s.clock = func() time.Time { return f.now.Add(refreshFamilyTTL + time.Second) }
					}
					if state == "rotated" {
						if _, err := f.s.rotateRefreshToken(t.Context(), f.raw, nil); err != nil {
							t.Fatal(err)
						}
					}
					if state == "revoked" {
						if _, err := f.pool.Exec(t.Context(), "UPDATE oauth_tokens SET revoked_at=created_at WHERE grant_id=$1", f.grantID); err != nil {
							t.Fatal(err)
						}
					}
					raw := f.raw
					if kind == TokenKindAccess {
						raw = f.accessRaw
					}
					if state == "unknown" {
						var err error
						raw, _, err = NewToken(kind, bytes.NewReader(bytes.Repeat([]byte{233}, 32)))
						if err != nil {
							t.Fatal(err)
						}
					}
					if state == "malformed" {
						raw = "synthetic-malformed"
					}
					for _, claim := range []string{"", f.publicID, f.otherID.String(), "https://unknown.example/metadata"} {
						for _, headers := range []http.Header{
							nil, {"Cookie": {"__Host-session=synthetic-unrelated"}},
							{"Cookie": {cookie.Name + "=" + cookie.Value}},
							{"Authorization": {"Basic c3ludGhldGljOnN5bnRoZXRpYw=="}}, {"Authorization": {"Bearer synthetic-unrelated"}},
						} {
							// Reset only revocation marks; retained terminal state is intentional.
							if _, err := f.pool.Exec(t.Context(), "UPDATE oauth_grants SET revoked_at=NULL WHERE id=$1", f.grantID); err != nil {
								t.Fatal(err)
							}
							if state != "revoked" {
								if _, err := f.pool.Exec(t.Context(), "UPDATE oauth_tokens SET revoked_at=NULL WHERE grant_id=$1", f.grantID); err != nil {
									t.Fatal(err)
								}
							}
							form := url.Values{"token": {raw}}
							if claim != "" {
								form.Set("client_id", claim)
							}
							assertSameTokenResponse(t, baseline, postRevoke(f.s, form.Encode(), headers))
							f.assertIsolation(t)
							if state != "unknown" && state != "malformed" {
								assertGrantHasNoLiveTokens(t, f.refreshFixture)
							} else {
								grant, err := f.q.GetOAuthGrantForUpdate(t.Context(), f.grantID)
								if err != nil || grant.RevokedAt != nil {
									t.Fatal("unknown token revoked a stored grant", err)
								}
							}
						}
					}
				})
			}
		}
	}
}

func TestRevokeClosedFormErrorsPrecedeStorage(t *testing.T) {
	for _, mode := range []string{"dcr", "cimd on", "cimd off"} {
		t.Run(mode, func(t *testing.T) { assertRevokeClosedFormErrors(t, mode) })
	}
}

func assertRevokeClosedFormErrors(t *testing.T, mode string) {
	t.Helper()
	f := newRevokeFixture(t, mode)
	trace := traceRevokeStorage(t, &f)
	baseline := postRevoke(f.s, "", nil)
	if baseline.Code != http.StatusBadRequest || baseline.Body.String() != `{"error":"invalid_request","error_description":"The request is invalid."}` {
		t.Fatal("missing token did not return the closed form error")
	}
	unknown, _, err := NewToken(TokenKindRefresh, bytes.NewReader(bytes.Repeat([]byte{233}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{f.raw, unknown, "synthetic-malformed"} {
		bodies := []string{
			"token=", "client_id=" + url.QueryEscape(f.publicID), "token=" + token + "&client_id=",
			"token=" + token + "&token=" + token, "token=" + token + "&%74oken=" + token,
			"token=" + token + "&token_type_hint=&token_type_hint=access_token",
			"token=" + token + "&token_type_hint=x&token_type_%68int=y",
			"token=" + token + "&client_id=x&client_id=y", "token=" + token + "&client_id=x&client_%69d=y",
			"token=" + token + "&client_id=%xx", "token=" + token + "&token_type_hint=%xx",
			"token=" + token + "&%63lient_secret=synthetic-sensitive-value",
			"token=%xx", "token=" + token + ";client_id=x", "token=" + strings.Repeat("a", oauthFormBodyLimit),
		}
		for _, field := range []string{"client_secret", "client_assertion", "client_assertion_type", "resource", "grant_type", "unknown"} {
			bodies = append(bodies, "token="+token+"&"+field+"=synthetic-sensitive-value")
		}
		for index, body := range bodies {
			w := postRevoke(f.s, body, http.Header{"Authorization": {"Bearer " + f.accessRaw}, "Cookie": {"__Host-session=synthetic"}})
			if w.Code != baseline.Code || !bytes.Equal(w.Body.Bytes(), baseline.Body.Bytes()) || !reflect.DeepEqual(w.Header(), baseline.Header()) {
				t.Fatalf("closed form error differs at case %d", index)
			}
		}
	}
	if steps := trace.snapshot(); len(steps) != 0 {
		t.Fatalf("malformed form accessed storage: %v", steps)
	}
	f.assertIsolation(t)
	grant, err := f.q.GetOAuthGrantForUpdate(t.Context(), f.grantID)
	if err != nil || grant.RevokedAt != nil {
		t.Fatal("malformed form revoked a grant", err)
	}
}

func TestRevokeMalformedClientIDPrecedesTokenAndStorage(t *testing.T) {
	for _, mode := range []string{"dcr", "cimd on", "cimd off"} {
		t.Run(mode, func(t *testing.T) {
			f := newRevokeFixture(t, mode)
			trace := traceRevokeStorage(t, &f)
			baseline := postRevoke(f.s, "token=", nil)
			unknown, _, err := NewToken(TokenKindAccess, bytes.NewReader(bytes.Repeat([]byte{233}, 32)))
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{
				"", "garbage", "http://client.example/a", "HTTPS://client.example/a", "https://client.example/", "https://client.example",
				"https://Client.example/a", "https://client.example./a", "https://client..example/a", "https://-client.example/a", "https://client-.example/a",
				"https://client.example:443/a", "https://user@client.example/a", "https://client.example/a?", "https://client.example/a?q=x",
				"https://client.example/a#", "https://client.example/a\\b", "https://client.example/a%xx", "https://client.example/./a",
				"https://client.example/%2E%2E/a", "https://client.example/a%2f..%2fb", "https://127.0.0.1/a", "https://2130706433/a",
				"https://0x7f.0.0.1/a", "https://[::1]/a", "https://localhost/a", "https://a.localhost/a", "https://a.local/a",
				"https://a.internal/a", "https://a.home.arpa/a", "https://a.onion/a", "https://aboutme.example/a", "https://sub.aboutme.example/a",
				" https://client.example/a", "https://client.example/a ", "https://client.example/a\t", "https://client%2eexample/a",
				"https://client.example/" + strings.Repeat("a", 234), "https://" + strings.Repeat("a", 64) + ".example/a",
			} {
				for _, raw := range []string{f.raw, unknown, "synthetic-malformed"} {
					form := url.Values{"token": {raw}, "client_id": {id}}
					assertSameTokenResponse(t, baseline, postRevoke(f.s, form.Encode(), nil))
				}
			}
			if steps := trace.snapshot(); len(steps) != 0 {
				t.Fatalf("malformed client ID accessed storage: %v", steps)
			}
			grant, err := f.q.GetOAuthGrantForUpdate(t.Context(), f.grantID)
			if err != nil || grant.RevokedAt != nil {
				t.Fatal("malformed client ID revoked a grant", err)
			}
			f.assertIsolation(t)
		})
	}
}

func TestRevokeMethodMediaTypeAndBodyBound(t *testing.T) {
	s := &Service{publicOrigin: "https://aboutme.example"}
	for _, tc := range []struct {
		method string
		media  []string
		status int
	}{
		{http.MethodGet, []string{"application/x-www-form-urlencoded"}, http.StatusMethodNotAllowed},
		{http.MethodPost, nil, http.StatusUnsupportedMediaType},
		{http.MethodPost, []string{"application/json"}, http.StatusUnsupportedMediaType},
		{http.MethodPost, []string{"application/x-www-form-urlencoded; charset=utf-8"}, http.StatusUnsupportedMediaType},
		{http.MethodPost, []string{"application/x-www-form-urlencoded", "application/x-www-form-urlencoded"}, http.StatusUnsupportedMediaType},
	} {
		r := httptest.NewRequestWithContext(t.Context(), tc.method, "https://aboutme.example/oauth/revoke", strings.NewReader("token=synthetic"))
		r.Header["Content-Type"] = tc.media
		w := httptest.NewRecorder()
		s.HandleRevoke(w, r)
		if w.Code != tc.status || w.Body.String() != `{"error":"invalid_request","error_description":"The request is invalid."}` {
			t.Fatal("method or media type did not return the closed error")
		}
	}
	atLimit := "token=" + strings.Repeat("a", oauthFormBodyLimit-len("token="))
	assertEmptyRevoke(t, postRevoke(s, atLimit, nil))
	if w := postRevoke(s, atLimit+"a", nil); w.Code != http.StatusBadRequest {
		t.Fatal("revocation accepted one byte over the body limit")
	}
}
