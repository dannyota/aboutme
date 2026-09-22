package auth_test

// These tests pin DELETE /api/v1/me/identities/{id}: the full cookie CSRF rule
// set plus recent reauthentication, a uniform not-found for malformed, absent,
// and foreign IDs, a refusal to remove the last usable sign-in method, and a
// user-row lock that serializes concurrent unlinks.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

var allProviders = config.ProviderLogin{Google: true, GitHub: true, LinkedIn: true}

// newUnlinkTestService builds the production router with the given providers
// enabled; unlink never starts a provider flow, so no issuer is needed.
func newUnlinkTestService(t *testing.T, providers config.ProviderLogin) (http.Handler, *store.Queries) {
	t.Helper()
	pool := newTestPool(t)
	svc, err := auth.NewService(testLogger(), config.Config{PublicOrigin: testPublicOrigin, ProviderLogin: providers}, pool)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	return api.New(testLogger(), noopPinger{}, api.Options{}, nil, svc.RegisterRoutes), store.New(pool)
}

func linkIdentity(t *testing.T, q *store.Queries, userID uuid.UUID, provider string) uuid.UUID {
	t.Helper()
	identity, err := q.CreateIdentity(context.Background(), store.CreateIdentityParams{
		UserID: userID, Provider: provider, ProviderUserID: provider + "-" + uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("CreateIdentity(%s) error = %v", provider, err)
	}
	return identity.ID
}

func addPassword(t *testing.T, q *store.Queries, userID uuid.UUID) {
	t.Helper()
	now := time.Now()
	if _, err := q.UpsertPasswordCredential(context.Background(), store.UpsertPasswordCredentialParams{
		UserID: userID, EncodedHash: []byte("test-encoded-hash"), CreatedAt: now, ChangedAt: now,
	}); err != nil {
		t.Fatalf("UpsertPasswordCredential() error = %v", err)
	}
}

func unlinkPath(id string) string { return auth.MeIdentitiesPath + "/" + id }

// unlinkResult is what a test needs from one unlink response.
type unlinkResult struct {
	StatusCode int
	RetryAfter string
}

func unlink(t *testing.T, handler http.Handler, sess store.Session, raw, id string) (unlinkResult, string) {
	t.Helper()
	resp := doJSON(t, handler, http.MethodDelete, unlinkPath(id), testPublicOrigin, csrfTokenFor(sess), "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	result := unlinkResult{StatusCode: resp.StatusCode, RetryAfter: resp.Header.Get("Retry-After")}
	if resp.StatusCode == http.StatusNoContent {
		return result, ""
	}
	return result, decodeErrorCode(t, resp)
}

func identityIDs(t *testing.T, q *store.Queries, userID uuid.UUID) map[uuid.UUID]bool {
	t.Helper()
	rows, err := q.ListIdentitiesByUserID(context.Background(), userID)
	if err != nil {
		t.Fatalf("ListIdentitiesByUserID() error = %v", err)
	}
	ids := make(map[uuid.UUID]bool, len(rows))
	for _, row := range rows {
		ids[row.ID] = true
	}
	return ids
}

func TestUnlinkIdentity_RemovesOwnIdentityAndKeepsSessions(t *testing.T) {
	handler, q := newUnlinkTestService(t, allProviders)
	userID := createTestUser(t, q)
	addPassword(t, q, userID)
	google := linkIdentity(t, q, userID, "google")
	raw, sess := issueTestSession(t, q, userID)
	otherRaw, _ := issueTestSession(t, q, userID)

	resp, code := unlink(t, handler, sess, raw, google.String())
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("unlink status = %d (%s), want 204", resp.StatusCode, code)
	}
	if identityIDs(t, q, userID)[google] {
		t.Fatal("identity still linked after 204")
	}
	// Sessions are not tied to a provider: the current and other sessions live on.
	for _, cookie := range []string{raw, otherRaw} {
		if me := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", sessionRequestCookie(cookie)); me.StatusCode != http.StatusOK { //nolint:bodyclose // doJSON closes the body itself before returning.
			t.Fatalf("GET /me after unlink = %d, want 200", me.StatusCode)
		}
	}
}

func TestUnlinkIdentity_RequiresTheCookieCSRFRuleSet(t *testing.T) {
	handler, q := newUnlinkTestService(t, allProviders)
	userID := createTestUser(t, q)
	addPassword(t, q, userID)
	google := linkIdentity(t, q, userID, "google")
	raw, sess := issueTestSession(t, q, userID)
	path := unlinkPath(google.String())

	for _, tc := range []struct {
		name, origin, token string
		cookie              bool
		want                int
	}{
		{"no session", testPublicOrigin, csrfTokenFor(sess), false, http.StatusUnauthorized},
		{"no csrf token", testPublicOrigin, "", true, http.StatusForbidden},
		{"wrong csrf token", testPublicOrigin, csrfTokenFor(csrfTestSession(7)), true, http.StatusForbidden},
		{"no origin", "", csrfTokenFor(sess), true, http.StatusForbidden},
		{"foreign origin", "https://evil.example", csrfTokenFor(sess), true, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cookies []*http.Cookie
			if tc.cookie {
				cookies = append(cookies, sessionRequestCookie(raw))
			}
			resp := doJSON(t, handler, http.MethodDelete, path, tc.origin, tc.token, "", cookies...) //nolint:bodyclose // doJSON closes the body itself before returning.
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
	if !identityIDs(t, q, userID)[google] {
		t.Fatal("a rejected request removed the identity")
	}
}

// Agents authenticate with bearer tokens; this cookie-mode route never parses
// one, so a bearer-only request is an anonymous request.
func TestUnlinkIdentity_BearerTokenIsNotAuthentication(t *testing.T) {
	handler, q := newUnlinkTestService(t, allProviders)
	userID := createTestUser(t, q)
	addPassword(t, q, userID)
	google := linkIdentity(t, q, userID, "google")

	req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, unlinkPath(google.String()), nil)
	req.Header.Set("Origin", testPublicOrigin)
	req.Header.Set("Authorization", "Bearer agent-access-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), `"session_required"`) {
		t.Fatalf("bearer-only unlink = %d %s, want 401 session_required", rec.Code, rec.Body.String())
	}
	if !identityIDs(t, q, userID)[google] {
		t.Fatal("a bearer-only request removed the identity")
	}
}

func TestUnlinkIdentity_RequiresRecentReauth(t *testing.T) {
	handler, q := newUnlinkTestService(t, allProviders)
	userID := createTestUser(t, q)
	addPassword(t, q, userID)
	google := linkIdentity(t, q, userID, "google")
	raw, sess := issueTestSession(t, q, userID)
	forceReauthenticatedAtStale(t, sess.ID)

	resp, code := unlink(t, handler, sess, raw, google.String())
	if resp.StatusCode != http.StatusForbidden || code != "reauth_required" {
		t.Fatalf("stale unlink = %d %q, want 403 reauth_required", resp.StatusCode, code)
	}
	if !identityIDs(t, q, userID)[google] {
		t.Fatal("identity removed without recent reauthentication")
	}
}

func TestUnlinkIdentity_RefusesTheLastSignInMethod(t *testing.T) {
	handler, q := newUnlinkTestService(t, allProviders)
	userID := createTestUser(t, q)
	google := linkIdentity(t, q, userID, "google")
	raw, sess := issueTestSession(t, q, userID)

	resp, code := unlink(t, handler, sess, raw, google.String())
	if resp.StatusCode != http.StatusConflict || code != "last_sign_in_method" {
		t.Fatalf("last-method unlink = %d %q, want 409 last_sign_in_method", resp.StatusCode, code)
	}
	if !identityIDs(t, q, userID)[google] {
		t.Fatal("the last sign-in method was removed")
	}
}

func TestUnlinkIdentity_OneOfTwoProvidersThenRefusesTheLast(t *testing.T) {
	handler, q := newUnlinkTestService(t, allProviders)
	userID := createTestUser(t, q)
	google := linkIdentity(t, q, userID, "google")
	github := linkIdentity(t, q, userID, "github")
	raw, sess := issueTestSession(t, q, userID)

	if resp, code := unlink(t, handler, sess, raw, github.String()); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("first unlink = %d %q, want 204", resp.StatusCode, code)
	}
	if resp, code := unlink(t, handler, sess, raw, google.String()); resp.StatusCode != http.StatusConflict || code != "last_sign_in_method" {
		t.Fatalf("second unlink = %d %q, want 409 last_sign_in_method", resp.StatusCode, code)
	}
}

// A disabled provider's identity is not a sign-in method, but it can always be
// removed.
func TestUnlinkIdentity_CountsOnlyEnabledProvidersAsSignInMethods(t *testing.T) {
	handler, q := newUnlinkTestService(t, config.ProviderLogin{Google: true})
	userID := createTestUser(t, q)
	google := linkIdentity(t, q, userID, "google")
	linkedin := linkIdentity(t, q, userID, "linkedin")
	raw, sess := issueTestSession(t, q, userID)

	if resp, code := unlink(t, handler, sess, raw, google.String()); resp.StatusCode != http.StatusConflict || code != "last_sign_in_method" {
		t.Fatalf("unlink of the only enabled provider = %d %q, want 409 last_sign_in_method", resp.StatusCode, code)
	}
	if resp, code := unlink(t, handler, sess, raw, linkedin.String()); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("unlink of a disabled provider = %d %q, want 204", resp.StatusCode, code)
	}
}

func TestUnlinkIdentity_WorksWithProviderLoginOff(t *testing.T) {
	handler, q := newUnlinkTestService(t, config.ProviderLogin{})
	userID := createTestUser(t, q)
	addPassword(t, q, userID)
	google := linkIdentity(t, q, userID, "google")
	raw, sess := issueTestSession(t, q, userID)

	if resp, code := unlink(t, handler, sess, raw, google.String()); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("unlink with provider login off = %d %q, want 204", resp.StatusCode, code)
	}
}

func TestUnlinkIdentity_UniformNotFound(t *testing.T) {
	handler, q := newUnlinkTestService(t, allProviders)
	userID := createTestUser(t, q)
	addPassword(t, q, userID)
	raw, sess := issueTestSession(t, q, userID)
	otherUser := createTestUser(t, q)
	foreign := linkIdentity(t, q, otherUser, "google")

	var bodies []string
	for _, id := range []string{"google", "not-a-uuid", uuid.NewString(), foreign.String()} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, unlinkPath(id), nil)
		req.Header.Set("Origin", testPublicOrigin)
		req.Header.Set(auth.CSRFHeaderName, csrfTokenFor(sess))
		req.AddCookie(sessionRequestCookie(raw))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("unlink %q = %d, want 404", id, rec.Code)
		}
		if strings.Contains(rec.Body.String(), id) {
			t.Fatalf("404 body echoes the path value %q: %s", id, rec.Body.String())
		}
		bodies = append(bodies, rec.Body.String())
	}
	for _, body := range bodies[1:] {
		if body != bodies[0] {
			t.Fatalf("not-found bodies differ: %q vs %q", body, bodies[0])
		}
	}
	if !identityIDs(t, q, otherUser)[foreign] {
		t.Fatal("another account's identity was removed")
	}
}

func TestUnlinkIdentity_OnlyDeleteIsAllowed(t *testing.T) {
	handler, q := newUnlinkTestService(t, allProviders)
	userID := createTestUser(t, q)
	google := linkIdentity(t, q, userID, "google")
	raw, sess := issueTestSession(t, q, userID)

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodHead} {
		resp := doJSON(t, handler, method, unlinkPath(google.String()), testPublicOrigin, csrfTokenFor(sess), "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s = %d, want 405", method, resp.StatusCode)
		}
	}
}

func TestUnlinkIdentity_IsRateLimitedPerAccountAndIP(t *testing.T) {
	handler, q := newUnlinkTestService(t, allProviders)
	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID)

	for i := range 5 {
		if resp, _ := unlink(t, handler, sess, raw, uuid.NewString()); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("request %d = %d, want 404 within the budget", i+1, resp.StatusCode)
		}
	}
	resp, code := unlink(t, handler, sess, raw, uuid.NewString())
	if resp.StatusCode != http.StatusTooManyRequests || code != "rate_limited" || resp.RetryAfter == "" {
		t.Fatalf("sixth request = %d %q, want 429 rate_limited with Retry-After", resp.StatusCode, code)
	}
}

func TestGetMe_IdentitiesCarryIDAndCreatedAt(t *testing.T) {
	handler, q := newUnlinkTestService(t, allProviders)
	userID := createTestUser(t, q)
	google := linkIdentity(t, q, userID, "google")
	raw, _ := issueTestSession(t, q, userID)

	resp := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	var body struct {
		Data struct {
			Identities []struct {
				ID        string    `json:"id"`
				Provider  string    `json:"provider"`
				CreatedAt time.Time `json:"createdAt"`
			} `json:"identities"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode /me: %v", err)
	}
	if len(body.Data.Identities) != 1 {
		t.Fatalf("identities = %+v, want one", body.Data.Identities)
	}
	got := body.Data.Identities[0]
	if got.ID != google.String() || got.Provider != "google" || got.CreatedAt.IsZero() || got.CreatedAt.Location() != time.UTC {
		t.Fatalf("identity = %+v, want id %s, provider google, and a UTC createdAt", got, google)
	}
}

// Two concurrent unlinks of an account's only two identities must not both
// succeed: the user-row lock serializes the count check with the delete.
func TestUnlinkIdentity_ConcurrentUnlinksKeepOneSignInMethod(t *testing.T) {
	handler, q := newUnlinkTestService(t, allProviders)
	for round := range 10 {
		userID := createTestUser(t, q)
		ids := []uuid.UUID{linkIdentity(t, q, userID, "google"), linkIdentity(t, q, userID, "github")}
		raw, sess := issueTestSession(t, q, userID)

		statuses := make([]int, len(ids))
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i, id := range ids {
			wg.Go(func() {
				<-start
				req := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, unlinkPath(id.String()), nil)
				req.Header.Set("Origin", testPublicOrigin)
				req.Header.Set(auth.CSRFHeaderName, csrfTokenFor(sess))
				req.AddCookie(sessionRequestCookie(raw))
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				statuses[i] = rec.Code
			})
		}
		close(start)
		wg.Wait()

		ok, conflict := 0, 0
		for _, status := range statuses {
			switch status {
			case http.StatusNoContent:
				ok++
			case http.StatusConflict:
				conflict++
			}
		}
		if ok != 1 || conflict != 1 {
			t.Fatalf("round %d statuses = %v, want one 204 and one 409", round, statuses)
		}
		if n := len(identityIDs(t, q, userID)); n != 1 {
			t.Fatalf("round %d left %d identities, want 1", round, n)
		}
	}
}

// unlinkAuditCount counts identity_unlinked audit events. The event carries no
// user, provider, or identity reference, so tests compare counts around one
// request; no test in this package writes the kind concurrently with them.
func unlinkAuditCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := newRowInspectorPool(t).QueryRow(context.Background(),
		`SELECT count(*) FROM lifecycle_audit_events WHERE kind = 'identity_unlinked'`).Scan(&n); err != nil {
		t.Fatalf("count unlink audit events: %v", err)
	}
	return n
}

func TestUnlinkIdentity_AuditsOnlyASuccessfulUnlink(t *testing.T) {
	handler, q := newUnlinkTestService(t, allProviders)
	userID := createTestUser(t, q)
	google := linkIdentity(t, q, userID, "google")
	github := linkIdentity(t, q, userID, "github")
	raw, sess := issueTestSession(t, q, userID)

	before := unlinkAuditCount(t)
	if resp, code := unlink(t, handler, sess, raw, github.String()); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("unlink = %d %q, want 204", resp.StatusCode, code)
	}
	if got := unlinkAuditCount(t) - before; got != 1 {
		t.Fatalf("a 204 wrote %d audit events, want exactly 1", got)
	}

	before = unlinkAuditCount(t)
	if resp, code := unlink(t, handler, sess, raw, google.String()); resp.StatusCode != http.StatusConflict {
		t.Fatalf("last-method unlink = %d %q, want 409", resp.StatusCode, code)
	}
	if got := unlinkAuditCount(t) - before; got != 0 {
		t.Fatalf("a 409 wrote %d audit events, want 0", got)
	}

	var extra int
	if err := newRowInspectorPool(t).QueryRow(context.Background(),
		`SELECT count(*) FROM lifecycle_audit_events WHERE kind = 'identity_unlinked' AND media_job_id IS NOT NULL`).Scan(&extra); err != nil {
		t.Fatalf("read audit scope: %v", err)
	}
	if extra != 0 {
		t.Fatalf("%d identity_unlinked events carry a media job, want none", extra)
	}
}

// TestUnlinkIdentity_EnrolledAccountNeedsFactorProof proves unlink rechecks
// both recent proofs for an enrolled account under the user lock and removes
// nothing without the factor proof.
func TestUnlinkIdentity_EnrolledAccountNeedsFactorProof(t *testing.T) {
	handler, q := newUnlinkTestService(t, allProviders)
	userID := createTestUser(t, q)
	addPassword(t, q, userID)
	google := linkIdentity(t, q, userID, "google")
	enrollForTest(t, q, userID)
	raw, sess := issueTestSession(t, q, userID)

	resp, code := unlink(t, handler, sess, raw, google.String())
	if resp.StatusCode != http.StatusForbidden || code != "reauth_required" {
		t.Fatalf("enrolled unlink without factor proof = %d %s, want 403 reauth_required", resp.StatusCode, code)
	}
	if !identityIDs(t, q, userID)[google] {
		t.Fatal("identity removed by a rejected unlink")
	}

	fresh := time.Now()
	setFactorProofForTest(t, sess.ID, &fresh)
	resp, code = unlink(t, handler, sess, raw, google.String())
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("enrolled unlink with both proofs = %d %s, want 204", resp.StatusCode, code)
	}
	if identityIDs(t, q, userID)[google] {
		t.Error("identity still linked after 204")
	}
}
