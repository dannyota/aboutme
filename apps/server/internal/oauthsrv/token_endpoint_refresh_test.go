package oauthsrv

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const (
	closedInvalidGrant   = `{"error":"invalid_grant","error_description":"The request is invalid."}`
	closedInvalidClient  = `{"error":"invalid_client","error_description":"The request is invalid."}`
	closedInvalidRequest = `{"error":"invalid_request","error_description":"The request is invalid."}`
)

// grantResultRecorder admits every request and records how each admitted
// grant attempt finished.
type grantResultRecorder struct {
	mu      sync.Mutex
	results []grantAttemptResult
}

func (r *grantResultRecorder) AdmitToken(time.Time, *http.Request) (bool, int) { return true, 0 }

func (r *grantResultRecorder) AdmitGrant(clientID uuid.UUID, _ time.Time) (grantAttempt, bool, int) {
	return grantAttempt{clientID: clientID, leaseID: 1}, true, 0
}

func (r *grantResultRecorder) FinishGrant(_ grantAttempt, result grantAttemptResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.results = append(r.results, result)
}

func (r *grantResultRecorder) snapshot() []grantAttemptResult {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]grantAttemptResult(nil), r.results...)
}

func postTokenForm(t *testing.T, s *Service, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "https://aboutme.example/oauth/token", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.HandleToken(w, r)
	return w
}

func refreshBody(raw string, extra string) string {
	return "grant_type=refresh_token&refresh_token=" + raw + extra
}

// assertRefreshFamilyUnchanged proves a rejected refresh wrote nothing: the
// family has the same live and rotated rows as before, and the presented
// token is neither superseded nor revoked by the request.
func assertRefreshFamilyUnchanged(t *testing.T, f refreshFixture, digest [32]byte, wantLive, wantRotated int, wantSuperseded bool) {
	t.Helper()
	var live, rotated int
	if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FILTER (WHERE revoked_at IS NULL), count(*) FILTER (WHERE rotated_from IS NOT NULL) FROM oauth_tokens WHERE family_id = $1", f.familyID).Scan(&live, &rotated); err != nil {
		t.Fatalf("family counts: %v", err)
	}
	if live != wantLive || rotated != wantRotated {
		t.Fatalf("family live=%d rotated=%d, want live=%d rotated=%d", live, rotated, wantLive, wantRotated)
	}
	authority, err := f.q.GetOAuthTokenAuthorityByDigest(context.Background(), digest[:])
	if err != nil {
		t.Fatalf("GetOAuthTokenAuthorityByDigest: %v", err)
	}
	if authority.OAuthToken.RevokedAt != nil || (authority.OAuthToken.SupersededAt != nil) != wantSuperseded {
		t.Fatal("rejected refresh changed the presented token")
	}
}

func createOtherClient(t *testing.T, f refreshFixture) store.OAuthClient {
	t.Helper()
	other, err := f.q.CreateOAuthClient(context.Background(), store.CreateOAuthClientParams{ClientName: "Other agent", RedirectURIs: json.RawMessage(`["http://127.0.0.1/callback"]`), CreatedAt: f.now})
	if err != nil {
		t.Fatalf("CreateOAuthClient: %v", err)
	}
	t.Cleanup(func() {
		if _, err := f.pool.Exec(context.Background(), "DELETE FROM oauth_clients WHERE id = $1", other.ID); err != nil {
			t.Errorf("cleanup other client: %v", err)
		}
	})
	return other
}

// Rules 1 and 4 of docs/design/mcp-client-compatibility.md: refresh accepts
// the same two resource spellings as authorize and the code exchange, and
// every other value is invalid_grant with nothing written.
func TestToken_RefreshResource(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	for _, tc := range resourceCases {
		t.Run(tc.name, func(t *testing.T) {
			f := newRefreshFixture(t, now, now.Add(refreshFamilyTTL))
			w := postTokenForm(t, f.s, refreshBody(f.raw, "&client_id="+f.clientID.String()+tc.suffix))
			if tc.valid {
				if w.Code != http.StatusOK {
					t.Fatalf("valid refresh resource = %d %q", w.Code, w.Body.String())
				}
				return
			}
			want := closedInvalidGrant
			if tc.name == "malformed" {
				// A body that does not parse as a form is a malformed request.
				want = closedInvalidRequest
			}
			if w.Code != http.StatusBadRequest || w.Body.String() != want {
				t.Fatalf("invalid refresh resource = %d %q, want %q", w.Code, w.Body.String(), want)
			}
			assertRefreshFamilyUnchanged(t, f, f.digest, 1, 0, false)
		})
	}
}

// Rule 4 of docs/design/mcp-client-compatibility.md: a refresh client_id must
// name the refresh token's own client. A mismatch is invalid_grant, counts as
// a failed grant attempt, and writes nothing, so neither the rightful client's
// token nor its family is harmed by another client presenting it.
func TestToken_RefreshClientBinding(t *testing.T) {
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

	t.Run("another existing client is rejected and the owner still rotates", func(t *testing.T) {
		f := newRefreshFixture(t, now, now.Add(refreshFamilyTTL))
		recorder := &grantResultRecorder{}
		f.s.tokenAdmission = recorder
		other := createOtherClient(t, f)
		for _, resource := range []string{"", "&resource=https%3A%2F%2Faboutme.example%2Fmcp"} {
			w := postTokenForm(t, f.s, refreshBody(f.raw, "&client_id="+other.ID.String()+resource))
			if w.Code != http.StatusBadRequest || w.Body.String() != closedInvalidGrant {
				t.Fatalf("foreign client refresh = %d %q", w.Code, w.Body.String())
			}
			assertRefreshFamilyUnchanged(t, f, f.digest, 1, 0, false)
		}
		if got := recorder.snapshot(); len(got) != 2 || got[0] != grantAttemptFailure || got[1] != grantAttemptFailure {
			t.Fatalf("grant attempt results = %v, want two failures", got)
		}
		w := postTokenForm(t, f.s, refreshBody(f.raw, "&client_id="+f.clientID.String()+"&resource=https%3A%2F%2Faboutme.example%2Fmcp"))
		if w.Code != http.StatusOK {
			t.Fatalf("rightful refresh after foreign attempt = %d %q", w.Code, w.Body.String())
		}
		var response tokenResponse
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.RefreshToken == "" || response.Scope != "resumes:read" {
			t.Fatalf("rightful refresh response: %v", err)
		}
	})

	t.Run("unparseable or nil client_id is rejected", func(t *testing.T) {
		for _, clientID := range []string{"not-a-uuid", uuid.Nil.String(), "%20" + uuid.NewString()} {
			t.Run(clientID, func(t *testing.T) {
				f := newRefreshFixture(t, now, now.Add(refreshFamilyTTL))
				w := postTokenForm(t, f.s, refreshBody(f.raw, "&client_id="+clientID))
				if w.Code != http.StatusBadRequest || w.Body.String() != closedInvalidGrant {
					t.Fatalf("client_id %q refresh = %d %q", clientID, w.Code, w.Body.String())
				}
				assertRefreshFamilyUnchanged(t, f, f.digest, 1, 0, false)
			})
		}
	})

	t.Run("superseded token with another client does not revoke the family", func(t *testing.T) {
		f := newRefreshFixture(t, now, now.Add(refreshFamilyTTL))
		other := createOtherClient(t, f)
		w, first := f.rotate(t, f.raw)
		if w.Code != http.StatusOK {
			t.Fatalf("first rotation = %d", w.Code)
		}
		// One superseded refresh, one live successor refresh, one live access.
		assertRefreshFamilyUnchanged(t, f, f.digest, 3, 1, true)
		w = postTokenForm(t, f.s, refreshBody(f.raw, "&client_id="+other.ID.String()))
		if w.Code != http.StatusBadRequest || w.Body.String() != closedInvalidGrant {
			t.Fatalf("superseded foreign refresh = %d %q", w.Code, w.Body.String())
		}
		assertRefreshFamilyUnchanged(t, f, f.digest, 3, 1, true)
		w = postTokenForm(t, f.s, refreshBody(first.RefreshToken, "&client_id="+f.clientID.String()))
		if w.Code != http.StatusOK {
			t.Fatalf("successor refresh after foreign reuse = %d %q", w.Code, w.Body.String())
		}
	})

	t.Run("superseded token with its own client still revokes the family", func(t *testing.T) {
		f := newRefreshFixture(t, now, now.Add(refreshFamilyTTL))
		if w, _ := f.rotate(t, f.raw); w.Code != http.StatusOK {
			t.Fatalf("first rotation = %d", w.Code)
		}
		w := postTokenForm(t, f.s, refreshBody(f.raw, "&client_id="+f.clientID.String()+"&resource=https%3A%2F%2Faboutme.example%2Fmcp"))
		if w.Code != http.StatusBadRequest || w.Body.String() != closedInvalidGrant {
			t.Fatalf("superseded own-client refresh = %d %q", w.Code, w.Body.String())
		}
		var live int
		if err := f.pool.QueryRow(context.Background(), "SELECT count(*) FROM oauth_tokens WHERE family_id = $1 AND revoked_at IS NULL", f.familyID).Scan(&live); err != nil || live != 0 {
			t.Fatalf("reuse left %d live family rows: %v", live, err)
		}
	})

	t.Run("expired token with its own client stays invalid_grant", func(t *testing.T) {
		f := newRefreshFixture(t, now, now.Add(time.Second))
		f.s.clock = func() time.Time { return now.Add(time.Second) }
		w := postTokenForm(t, f.s, refreshBody(f.raw, "&client_id="+f.clientID.String()))
		if w.Code != http.StatusBadRequest || w.Body.String() != closedInvalidGrant {
			t.Fatalf("expired own-client refresh = %d %q", w.Code, w.Body.String())
		}
	})

	t.Run("form keys stay closed", func(t *testing.T) {
		f := newRefreshFixture(t, now, now.Add(refreshFamilyTTL))
		for _, extra := range []string{
			"&scope=resumes%3Aread",
			"&client_id=",
			"&client_id=" + f.clientID.String() + "&client_id=" + f.clientID.String(),
			"&refresh_token=" + f.raw,
			"&code=x",
			"&redirect_uri=http%3A%2F%2F127.0.0.1%2Fcallback",
			"&code_verifier=x",
			"&client_secret=x",
		} {
			w := postTokenForm(t, f.s, refreshBody(f.raw, extra))
			if w.Code != http.StatusBadRequest || w.Body.String() != closedInvalidRequest {
				t.Fatalf("refresh with %q = %d %q", extra, w.Code, w.Body.String())
			}
		}
		if w := postTokenForm(t, f.s, "grant_type=refresh_token&client_id="+f.clientID.String()); w.Code != http.StatusBadRequest || w.Body.String() != closedInvalidRequest {
			t.Fatalf("refresh without refresh_token = %d %q", w.Code, w.Body.String())
		}
		assertRefreshFamilyUnchanged(t, f, f.digest, 1, 0, false)
	})
}

// Rule 8 of docs/design/mcp-client-compatibility.md: a token request whose
// client_id names no stored client is 401 invalid_client, so a client whose
// registration was swept registers again. A known client with a bad grant
// stays 400 invalid_grant.
func TestToken_UnknownClientIs401(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	assert401 := func(t *testing.T, w *httptest.ResponseRecorder) {
		t.Helper()
		if w.Code != http.StatusUnauthorized || w.Body.String() != closedInvalidClient {
			t.Fatalf("response = %d %q, want 401 invalid_client", w.Code, w.Body.String())
		}
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("headers = %v", w.Header())
		}
	}
	assert400 := func(t *testing.T, w *httptest.ResponseRecorder, want string) {
		t.Helper()
		if w.Code != http.StatusBadRequest || w.Body.String() != want {
			t.Fatalf("response = %d %q, want 400 %q", w.Code, w.Body.String(), want)
		}
	}
	unknownRefresh, _, err := NewToken(TokenKindRefresh, bytes.NewReader(bytes.Repeat([]byte{77}, 32)))
	if err != nil {
		t.Fatalf("NewToken: %v", err)
	}

	t.Run("code exchange naming a deleted client", func(t *testing.T) {
		f := newCodeFixture(t, now)
		if _, err := f.pool.Exec(context.Background(), "DELETE FROM oauth_clients WHERE id = $1", f.clientID); err != nil {
			t.Fatalf("delete client: %v", err)
		}
		assert401(t, f.exchange(t, f.clientID, "http://127.0.0.1:20090/callback", f.verifier))
	})

	t.Run("code exchange naming a client that never existed", func(t *testing.T) {
		f := newCodeFixture(t, now)
		assert401(t, f.exchange(t, uuid.New(), "http://127.0.0.1:20090/callback", f.verifier))
		code, err := f.q.GetOAuthAuthorizationCodeByDigest(context.Background(), f.digest[:])
		if err != nil || code.ConsumedAt != nil {
			t.Fatal("unknown client consumed the code")
		}
	})

	t.Run("code exchange with an unparseable client_id stays 400", func(t *testing.T) {
		f := newCodeFixture(t, now)
		body := "grant_type=authorization_code&code=" + f.code + "&redirect_uri=http%3A%2F%2F127.0.0.1%3A20090%2Fcallback&client_id=not-a-uuid&code_verifier=" + f.verifier
		assert400(t, postTokenForm(t, f.s, body), closedInvalidClient)
	})

	t.Run("known client with a never-issued code stays invalid_grant", func(t *testing.T) {
		f := newCodeFixture(t, now)
		other, _, err := NewCode(bytes.NewReader(bytes.Repeat([]byte{45}, 32)))
		if err != nil {
			t.Fatalf("NewCode: %v", err)
		}
		f.code = other
		assert400(t, f.exchange(t, f.clientID, "http://127.0.0.1:20090/callback", f.verifier), closedInvalidGrant)
	})

	t.Run("refresh with an unknown token and an unknown client", func(t *testing.T) {
		f := newRefreshFixture(t, now, now.Add(refreshFamilyTTL))
		assert401(t, postTokenForm(t, f.s, refreshBody(unknownRefresh, "&client_id="+uuid.NewString())))
		assert401(t, postTokenForm(t, f.s, refreshBody(unknownRefresh, "&client_id="+uuid.NewString()+"&resource=https%3A%2F%2Faboutme.example%2Fmcp")))
		assert401(t, postTokenForm(t, f.s, refreshBody("not-a-token", "&client_id="+uuid.NewString())))
	})

	t.Run("refresh token of a deleted client", func(t *testing.T) {
		f := newRefreshFixture(t, now, now.Add(refreshFamilyTTL))
		if _, err := f.pool.Exec(context.Background(), "DELETE FROM oauth_clients WHERE id = $1", f.clientID); err != nil {
			t.Fatalf("delete client: %v", err)
		}
		assert401(t, postTokenForm(t, f.s, refreshBody(f.raw, "&client_id="+f.clientID.String())))
	})

	t.Run("refresh with an unknown token stays invalid_grant otherwise", func(t *testing.T) {
		f := newRefreshFixture(t, now, now.Add(refreshFamilyTTL))
		for _, extra := range []string{"", "&client_id=" + f.clientID.String(), "&client_id=not-a-uuid"} {
			assert400(t, postTokenForm(t, f.s, refreshBody(unknownRefresh, extra)), closedInvalidGrant)
		}
	})
}
