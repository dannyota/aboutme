package oauthsrv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func cimdAuthorizeHarness(t *testing.T) (*Service, *store.Queries, store.User, ConsentQuery) {
	t.Helper()
	s, q, _, user := newAuthorizeHarness(t)
	f, _ := cimdTLSResolver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprint(w, cimdFixture(r.URL.Path)); err != nil {
			return
		}
	}), s.clock)
	s.cimd, s.cimdEnabled = f, true
	request := consentQuery(uuid.Nil, "resumes:read")
	request.PublicClientID = "https://example.com/" + uuid.NewString()
	request.RedirectURI = "http://localhost:20095/callback"
	request.address = netip.MustParseAddr("8.8.8.8")
	t.Cleanup(func() {
		client, err := q.GetOAuthClientByMetadataURL(context.Background(), request.PublicClientID)
		if errors.Is(err, pgx.ErrNoRows) {
			return
		}
		if err != nil {
			t.Error(err)
			return
		}
		if _, deleteErr := q.DeleteOAuthClient(context.Background(), client.ID); deleteErr != nil {
			t.Error(deleteErr)
		}
	})
	return s, q, user, request
}

func TestCIMDNoWritesBeforeApprovalAndAtomicConcurrentApprovals(t *testing.T) {
	s, q, user, request := cimdAuthorizeHarness(t)
	ctx := context.Background()
	sess := issueTestSession(t, s.pool, user.ID, s.clock(), nil)
	if _, err := s.ConsentContext(ctx, user.ID, request); err != nil {
		t.Fatal(err)
	}
	anonymous := httptest.NewRequestWithContext(context.Background(), http.MethodGet, s.publicOrigin+"/oauth/authorize?"+request.values().Encode(), nil)
	anonymous.RemoteAddr = "8.8.8.8:1234"
	w := httptest.NewRecorder()
	s.HandleAuthorize(w, anonymous)
	if w.Code != 302 || !strings.HasPrefix(w.Header().Get("Location"), "/login?") {
		t.Fatal("anonymous authorization did not reach login")
	}
	if _, err := s.ConsentDecision(ctx, sess, ConsentDecision{ConsentQuery: request, Decision: "deny"}); err != nil {
		t.Fatal(err)
	}
	if _, lookupErr := q.GetOAuthClientByMetadataURL(ctx, request.PublicClientID); !errors.Is(lookupErr, pgx.ErrNoRows) {
		t.Fatal("consent read or denial wrote a client")
	}
	var wg sync.WaitGroup
	errorsOut := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.ConsentDecision(ctx, sess, ConsentDecision{ConsentQuery: request, Decision: "approve"})
			errorsOut <- err
		}()
	}
	wg.Wait()
	close(errorsOut)
	for err := range errorsOut {
		if err != nil {
			t.Fatal(err)
		}
	}
	client, err := q.GetOAuthClientByMetadataURL(ctx, request.PublicClientID)
	if err != nil {
		t.Fatal(err)
	}
	grant := mustLiveGrant(t, q, user.ID, client.ID)
	if _, err := s.ConsentDecision(ctx, sess, ConsentDecision{ConsentQuery: request, Decision: "approve"}); err != nil {
		t.Fatal(err)
	}
	reapproved := mustLiveGrant(t, q, user.ID, client.ID)
	if grant.ID != reapproved.ID || !grant.CreatedAt.Equal(reapproved.CreatedAt) {
		t.Fatal("reapproval replaced live grant")
	}
	if _, err := s.storedPublicClient(ctx, q, client.ID.String()); !errors.Is(err, errOAuthUnknownClient) {
		t.Fatal("internal UUID alias accepted")
	}
	s.cimdEnabled = false
	if _, err := s.ConsentContext(ctx, user.ID, request); !errors.Is(err, ErrConsentInvalid) {
		t.Fatalf("disabled consent: %v", err)
	}
	if _, err := s.storedPublicClient(ctx, q, request.PublicClientID); err != nil {
		t.Fatal("disabled flag lost token identity")
	}
}

func TestCIMDStaleSessionRollsBackClient(t *testing.T) {
	s, q, user, request := cimdAuthorizeHarness(t)
	sess := issueTestSession(t, s.pool, user.ID, s.clock(), nil)
	now := s.clock()
	if err := q.RevokeSession(context.Background(), store.RevokeSessionParams{ID: sess.ID, RevokedAt: &now}); err != nil {
		t.Fatal(err)
	}
	_, err := s.ConsentDecision(context.Background(), sess, ConsentDecision{ConsentQuery: request, Decision: "approve"})
	if !errors.Is(err, auth.ErrSessionInvalid) {
		t.Fatalf("stale session: %v", err)
	}
	if _, err := q.GetOAuthClientByMetadataURL(context.Background(), request.PublicClientID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("failed approval retained client")
	}
}

func TestCIMDLoopbackNeverIssuesSilently(t *testing.T) {
	s, _, user, request := cimdAuthorizeHarness(t)
	sess := issueTestSession(t, s.pool, user.ID, s.clock(), nil)
	if _, err := s.ConsentDecision(context.Background(), sess, ConsentDecision{ConsentQuery: request, Decision: "approve"}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "https://aboutme.example/oauth/authorize?"+request.values().Encode(), nil)
	r.RemoteAddr = "8.8.8.8:1234"
	r = r.WithContext(auth.ContextWithSession(r.Context(), sess))
	w := httptest.NewRecorder()
	s.HandleAuthorize(w, r)
	if w.Code != 302 || !strings.HasPrefix(w.Header().Get("Location"), "/authorize?") {
		t.Fatalf("loopback silent issue: status %d", w.Code)
	}
}

func TestCIMDGrantCapRollsBackNewClient(t *testing.T) {
	s, q, user, request := cimdAuthorizeHarness(t)
	ctx := context.Background()
	sess := issueTestSession(t, s.pool, user.ID, s.clock(), nil)
	for range 10 {
		client, err := q.CreateOAuthClient(ctx, store.CreateOAuthClientParams{ClientName: "Fixture", RedirectURIs: []byte(`["http://localhost/callback"]`), CreatedAt: s.clock()})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if _, err := q.DeleteOAuthClient(context.Background(), client.ID); err != nil {
				t.Error(err)
			}
		})
		if _, err := q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: user.ID, ClientID: client.ID, Scopes: "resumes:read", CreatedAt: s.clock()}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ConsentDecision(ctx, sess, ConsentDecision{ConsentQuery: request, Decision: "approve"}); !errors.Is(err, ErrGrantLimit) {
		t.Fatalf("grant cap: %v", err)
	}
	if _, lookupErr := q.GetOAuthClientByMetadataURL(ctx, request.PublicClientID); !errors.Is(lookupErr, pgx.ErrNoRows) {
		t.Fatal("grant cap retained URL row")
	}
}

func TestCIMDCurrentDocumentNeverFallsBackToStoredRedirects(t *testing.T) {
	s, q, user, request := cimdAuthorizeHarness(t)
	sess := issueTestSession(t, s.pool, user.ID, s.clock(), nil)
	if _, err := s.ConsentDecision(context.Background(), sess, ConsentDecision{ConsentQuery: request, Decision: "approve"}); err != nil {
		t.Fatal(err)
	}
	client, err := q.GetOAuthClientByMetadataURL(context.Background(), request.PublicClientID)
	if err != nil {
		t.Fatal(err)
	}
	// Expired metadata must refetch even when the approved stored snapshot is
	// still valid. A failed DNS lookup cannot restore that snapshot.
	s.cimd.clock = func() time.Time { return s.clock().Add(6 * time.Minute) }
	s.cimd.cfg.DevStubAddr = ""
	s.cimd.lookup = func(context.Context, string) ([]netip.Addr, error) { return nil, errCIMDInvalid }
	if _, err := s.ConsentContext(context.Background(), user.ID, request); !errors.Is(err, ErrConsentInvalid) {
		t.Fatalf("expired metadata: %v", err)
	}
	if !registeredRedirect(client, "http://localhost/callback") {
		t.Fatal("stored fixture redirect changed")
	}
}

func TestCIMDHTTPSGrantFromAnotherEpochRequiresConsent(t *testing.T) {
	s, _, user, request := cimdAuthorizeHarness(t)
	f, _ := cimdTLSResolver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := `{"client_id":"https://example.com` + r.URL.Path + `","client_name":"Fixture","redirect_uris":["https://example.com/callback"],"token_endpoint_auth_method":"none"}`
		if _, err := fmt.Fprint(w, body); err != nil {
			return
		}
	}), s.clock)
	s.cimd = f
	request.RedirectURI = "https://example.com/callback"
	sess := issueTestSession(t, s.pool, user.ID, s.clock(), nil)
	if _, err := s.ConsentDecision(context.Background(), sess, ConsentDecision{ConsentQuery: request, Decision: "approve"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(context.Background(), `UPDATE users SET auth_epoch=auth_epoch+1 WHERE id=$1`, user.ID); err != nil {
		t.Fatal(err)
	}
	fresh := issueTestSession(t, s.pool, user.ID, s.clock(), nil)
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, s.publicOrigin+"/oauth/authorize?"+request.values().Encode(), nil)
	r.RemoteAddr = "8.8.8.8:1234"
	r = r.WithContext(auth.ContextWithSession(r.Context(), fresh))
	w := httptest.NewRecorder()
	s.HandleAuthorize(w, r)
	if w.Code != 302 || !strings.HasPrefix(w.Header().Get("Location"), "/authorize?") {
		t.Fatal("stale grant epoch minted silent authority")
	}
}

func TestCIMDConcurrentOwnersShareOnlyClient(t *testing.T) {
	s, q, user, request := cimdAuthorizeHarness(t)
	ctx := context.Background()
	other, err := q.CreateUser(ctx, store.CreateUserParams{Email: uuid.NewString() + "@example.test", Name: "Fixture"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, deleteErr := s.pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, other.ID); deleteErr != nil {
			t.Error(deleteErr)
		}
	})
	sessions := []store.Session{issueTestSession(t, s.pool, user.ID, s.clock(), nil), issueTestSession(t, s.pool, other.ID, s.clock(), nil)}
	errorsOut := make(chan error, 2)
	for _, session := range sessions {
		go func() {
			_, decisionErr := s.ConsentDecision(ctx, session, ConsentDecision{ConsentQuery: request, Decision: "approve"})
			errorsOut <- decisionErr
		}()
	}
	for range 2 {
		if decisionErr := <-errorsOut; decisionErr != nil {
			t.Fatal(decisionErr)
		}
	}
	client, err := q.GetOAuthClientByMetadataURL(ctx, request.PublicClientID)
	if err != nil {
		t.Fatal(err)
	}
	first := mustLiveGrant(t, q, user.ID, client.ID)
	second := mustLiveGrant(t, q, other.ID, client.ID)
	if first.ID == second.ID || first.UserID == second.UserID {
		t.Fatal("owners shared grant authority")
	}
}

func TestCIMDExpiredRedirectAndFailedMetadataUpdate(t *testing.T) {
	s, q, user, request := cimdAuthorizeHarness(t)
	var changed atomic.Bool
	f, _ := cimdTLSResolver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		name, redirect := "Fixture", "https://example.com/callback"
		if changed.Load() {
			name, redirect = "Changed", "https://example.com/replacement"
		}
		body := `{"client_id":"https://example.com` + r.URL.Path + `","client_name":"` + name + `","redirect_uris":["` + redirect + `"],"token_endpoint_auth_method":"none"}`
		if _, err := fmt.Fprint(w, body); err != nil {
			return
		}
	}), s.clock)
	s.cimd = f
	request.RedirectURI = "https://example.com/callback"
	sess := issueTestSession(t, s.pool, user.ID, s.clock(), nil)
	if _, err := s.ConsentDecision(context.Background(), sess, ConsentDecision{ConsentQuery: request, Decision: "approve"}); err != nil {
		t.Fatal(err)
	}
	changed.Store(true)
	f.clock = func() time.Time { return s.clock().Add(6 * time.Minute) }
	r := httptest.NewRequestWithContext(context.Background(), http.MethodGet, s.publicOrigin+"/oauth/authorize?"+request.values().Encode(), nil)
	r.RemoteAddr = "8.8.8.8:1234"
	r = r.WithContext(auth.ContextWithSession(r.Context(), sess))
	w := httptest.NewRecorder()
	s.HandleAuthorize(w, r)
	if w.Code != 400 || w.Header().Get("Location") != "" {
		t.Fatal("expired redirect reused stored metadata")
	}
	request.RedirectURI = "https://example.com/replacement"
	now := s.clock()
	if err := q.RevokeSession(context.Background(), store.RevokeSessionParams{ID: sess.ID, RevokedAt: &now}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConsentDecision(context.Background(), sess, ConsentDecision{ConsentQuery: request, Decision: "approve"}); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Fatal("stale approval updated metadata")
	}
	client, err := q.GetOAuthClientByMetadataURL(context.Background(), request.PublicClientID)
	if err != nil || client.ClientName != "Fixture" || !registeredRedirect(client, "https://example.com/callback") {
		t.Fatal("failed approval committed metadata")
	}
}

func TestCIMDCodeWriteFailureRollsBackClientAndGrant(t *testing.T) {
	s, q, user, request := cimdAuthorizeHarness(t)
	ctx := context.Background()
	client, err := q.CreateOAuthClient(ctx, store.CreateOAuthClientParams{ClientName: "Fixture", RedirectURIs: []byte(`["http://localhost/callback"]`), CreatedAt: s.clock()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, deleteErr := q.DeleteOAuthClient(context.Background(), client.ID); deleteErr != nil {
			t.Error(deleteErr)
		}
	})
	grant, err := q.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{UserID: user.ID, ClientID: client.ID, Scopes: "resumes:read", CreatedAt: s.clock()})
	if err != nil {
		t.Fatal(err)
	}
	entropy := bytes.Repeat([]byte{7}, 32)
	_, digest, err := NewCode(bytes.NewReader(entropy))
	if err != nil {
		t.Fatal(err)
	}
	epoch := user.AuthEpoch
	if _, codeErr := q.CreateOAuthAuthorizationCode(ctx, store.CreateOAuthAuthorizationCodeParams{CodeDigest: digest[:], ClientID: client.ID, UserID: user.ID, Scopes: "resumes:read", CodeChallenge: strings.Repeat("A", 43), RedirectURI: "http://localhost/callback", CreatedAt: s.clock(), GrantID: &grant.ID, AuthEpoch: &epoch}); codeErr != nil {
		t.Fatal(codeErr)
	}
	s.entropy = bytes.NewReader(entropy)
	sess := issueTestSession(t, s.pool, user.ID, s.clock(), nil)
	_, err = s.ConsentDecision(ctx, sess, ConsentDecision{ConsentQuery: request, Decision: "approve"})
	if err == nil || !strings.Contains(err.Error(), "create authorization code") {
		t.Fatal("code-write failure was not observed by the operation")
	}
	if _, lookupErr := q.GetOAuthClientByMetadataURL(ctx, request.PublicClientID); !errors.Is(lookupErr, pgx.ErrNoRows) {
		t.Fatal("code-write failure retained client")
	}
	count, err := q.CountLiveOAuthGrantsForUser(ctx, user.ID)
	if err != nil || count != 1 {
		t.Fatal("code-write failure retained grant")
	}
}
