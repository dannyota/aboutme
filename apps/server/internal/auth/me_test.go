// Package auth_test exercises GET /api/v1/me and shared session API helpers
// against live Postgres. Tests issue sessions directly, then use the real HTTP
// middleware chain.
package auth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// newSessionAPITestService builds the production router without OAuth test
// endpoints because these tests drive only session APIs.
func newSessionAPITestService(t *testing.T) (http.Handler, *store.Queries) {
	t.Helper()

	q := newTestQueries(t)
	svc, err := auth.NewService(testLogger(), config.Config{PublicOrigin: testPublicOrigin}, q)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	handler := api.New(testLogger(), noopPinger{}, api.Options{}, svc.RegisterRoutes)
	return handler, q
}

// issueTestSession mints a row used by the real RequireSession path.
func issueTestSession(t *testing.T, q *store.Queries, userID uuid.UUID) (raw string, sess store.Session) {
	t.Helper()

	sm := auth.NewSessionManager(q)
	raw, sess, err := sm.Issue(context.Background(), userID, "test-agent/1.0", "203.0.113.60")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	return raw, sess
}

// sessionRequestCookie returns the __Host-session cookie a request
// carrying raw (issueTestSession's own return value) would send.
func sessionRequestCookie(raw string) *http.Cookie {
	return requestCookie(auth.SessionCookieName, raw)
}

// doJSON sends a request with optional Origin, CSRF token, body, and cookies.
// The recorder body remains readable after Close.
func doJSON(t *testing.T, handler http.Handler, method, path, origin, csrfToken string, body string, cookies ...*http.Cookie) *http.Response {
	t.Helper()

	req := httptest.NewRequestWithContext(context.Background(), method, path, requestBody(body))
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if csrfToken != "" {
		req.Header.Set(auth.CSRFHeaderName, csrfToken)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	resp := rec.Result()
	if err := resp.Body.Close(); err != nil {
		t.Errorf("close response body: %v", err)
	}
	return resp
}

// meEnvelopeBody decodes GET /me's success envelope.
type meEnvelopeBody struct {
	Data struct {
		User struct {
			ID        string  `json:"id"`
			Email     string  `json:"email"`
			Name      string  `json:"name"`
			AvatarKey *string `json:"avatarKey"`
		} `json:"user"`
		CSRFToken  string `json:"csrfToken"`
		Identities []struct {
			Provider string `json:"provider"`
		} `json:"identities"`
	} `json:"data"`
}

// errorEnvelopeBody decodes the standard {"error":{"code","message"}}
// envelope every rejection in this package returns.
type errorEnvelopeBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// decodeErrorCode reads resp's body as the standard error envelope and
// returns its code, failing the test if the body doesn't decode or the
// code/message are empty.
func decodeErrorCode(t *testing.T, resp *http.Response) string {
	t.Helper()

	var body errorEnvelopeBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if body.Error.Code == "" {
		t.Error("error.code is empty, want a non-empty code")
	}
	if body.Error.Message == "" {
		t.Error("error.message is empty, want a non-empty message")
	}
	return body.Error.Code
}

// ---- happy path -----------------------------------------------------------

// TestGetMe_ReturnsUserCSRFTokenAndIdentitiesEnvelope pins the full envelope
// shape
// {data:{user:{id,email,name,avatarKey}, csrfToken, identities:[{provider}]}},
// with the user fields matching the caller's own row, csrfToken decoding
// (base64.RawURLEncoding) to exactly the session's own csrf_secret, and
// the identities list reflecting a real linked identity.
func TestGetMe_ReturnsUserCSRFTokenAndIdentitiesEnvelope(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	ctx := context.Background()

	avatarKey := "avatars/" + uuid.NewString() + ".png"
	usr, err := q.CreateUser(ctx, store.CreateUserParams{
		Email:     uuid.NewString() + "@example.com",
		Name:      "Ada Lovelace",
		AvatarKey: &avatarKey,
	})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if _, err := q.CreateIdentity(ctx, store.CreateIdentityParams{
		UserID:         usr.ID,
		Provider:       string(auth.ProviderGoogle),
		ProviderUserID: "g-sub-" + uuid.NewString(),
	}); err != nil {
		t.Fatalf("CreateIdentity() error = %v", err)
	}

	raw, sess := issueTestSession(t, q, usr.ID)

	resp := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /me status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body meEnvelopeBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}

	if body.Data.User.ID != usr.ID.String() {
		t.Errorf("user.id = %q, want %q", body.Data.User.ID, usr.ID.String())
	}
	if body.Data.User.Email != usr.Email {
		t.Errorf("user.email = %q, want %q", body.Data.User.Email, usr.Email)
	}
	if body.Data.User.Name != "Ada Lovelace" {
		t.Errorf("user.name = %q, want %q", body.Data.User.Name, "Ada Lovelace")
	}
	if body.Data.User.AvatarKey == nil || *body.Data.User.AvatarKey != avatarKey {
		t.Errorf("user.avatarKey = %v, want %q", body.Data.User.AvatarKey, avatarKey)
	}

	wantToken := csrfTokenFor(sess) // csrf_test.go's shared helper
	if body.Data.CSRFToken != wantToken {
		t.Errorf("csrfToken = %q, want %q (base64.RawURLEncoding of the session's own csrf_secret)", body.Data.CSRFToken, wantToken)
	}

	if len(body.Data.Identities) != 1 || body.Data.Identities[0].Provider != string(auth.ProviderGoogle) {
		t.Errorf("identities = %+v, want exactly one entry with provider %q", body.Data.Identities, auth.ProviderGoogle)
	}
}

// TestGetMe_AvatarKeyNull_SerializesAsJSONNull guards the nullable half of
// avatarKey's contract: a user with no avatar_key (createTestUser's
// default) must serialize as JSON null, not an omitted field or an empty
// string -- inspected via the raw body text, since decoding into *string
// alone can't distinguish "absent from the JSON" from "present and null".
func TestGetMe_AvatarKeyNull_SerializesAsJSONNull(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)
	raw, _ := issueTestSession(t, q, userID)

	resp := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var raw2 map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw2); err != nil {
		t.Fatalf("decode top-level envelope: %v", err)
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(raw2["data"], &data); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	var user map[string]json.RawMessage
	if err := json.Unmarshal(data["user"], &user); err != nil {
		t.Fatalf("decode user: %v", err)
	}
	got, ok := user["avatarKey"]
	if !ok {
		t.Fatal("user object has no \"avatarKey\" key at all, want it present with a JSON null value")
	}
	if string(got) != "null" {
		t.Errorf("user.avatarKey raw JSON = %s, want the literal null", got)
	}
}

// ---- CSRF token appears only in the response body -------------------------

// TestGetMe_CSRFTokenOnlyInResponseBody proves the token never enters response
// headers or cookies.
func TestGetMe_CSRFTokenOnlyInResponseBody(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	userID := createTestUser(t, q)
	raw, sess := issueTestSession(t, q, userID)

	resp := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body meEnvelopeBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body.Data.CSRFToken == "" {
		t.Fatal("csrfToken is empty in the response body, want the session's own token")
	}

	token := body.Data.CSRFToken
	for name, values := range resp.Header {
		for _, v := range values {
			if strings.Contains(v, token) {
				t.Errorf("response header %s = %q contains the CSRF token, want it absent from every header", name, v)
			}
		}
	}
	for _, c := range resp.Cookies() {
		if strings.Contains(c.Value, token) {
			t.Errorf("Set-Cookie %s = %q contains the CSRF token, want it absent from every cookie", c.Name, c.Value)
		}
	}

	// Cross-checked directly against the session row too, independent of
	// the handler's own encoding step.
	if want := csrfTokenFor(sess); token != want {
		t.Errorf("csrfToken = %q, want %q", token, want)
	}
}

// ---- RequireSession failure modes, exercised via GET /me -----------------

// TestGetMe_NoSessionCookie_Returns401 covers RequireSession's simplest
// rejection: no __Host-session cookie at all.
func TestGetMe_NoSessionCookie_Returns401(t *testing.T) {
	handler, _ := newSessionAPITestService(t)

	resp := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "") //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	if got := decodeErrorCode(t, resp); got != "session_required" {
		t.Errorf("error.code = %q, want %q", got, "session_required")
	}
}

// TestGetMe_InvalidSessionToken_Returns401AndClearsCookie covers a
// syntactically-present but never-issued token: RequireSession must
// reject it (401) AND clear the __Host-session cookie in the same
// response, so a browser holding a dead token stops resending it.
func TestGetMe_InvalidSessionToken_Returns401AndClearsCookie(t *testing.T) {
	handler, _ := newSessionAPITestService(t)

	fakeCookie := requestCookie(auth.SessionCookieName, "never-issued-session-token")
	resp := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", fakeCookie) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	if got := decodeErrorCode(t, resp); got != "session_required" {
		t.Errorf("error.code = %q, want %q", got, "session_required")
	}

	cleared := extractCookie(resp, auth.SessionCookieName)
	if cleared == nil {
		t.Fatal("response missing a __Host-session Set-Cookie header, want one clearing it")
	}
	if cleared.MaxAge >= 0 {
		t.Errorf("__Host-session MaxAge = %d, want negative (cleared)", cleared.MaxAge)
	}
}

// TestGetMe_WrongMethod_Returns405 proves the route accepts only exact GET.
func TestGetMe_WrongMethod_Returns405(t *testing.T) {
	handler, _ := newSessionAPITestService(t)

	resp := doJSON(t, handler, http.MethodPost, auth.MePath, "", "", "") //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST %s status = %d, want %d", auth.MePath, resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

// TestGetMe_RotatesSessionOnAuthenticate_SetsNewCookie proves
// RequireSession forwards Authenticate's rotation outcome onto the
// response: a session past the 24-hour rotation age must still
// authenticate GET /me successfully AND carry a new __Host-session
// Set-Cookie for the rotated successor -- exercised through the real HTTP
// chain, not SessionManager directly (session_test.go/
// session_adversarial_test.go already cover the algorithm itself).
func TestGetMe_RotatesSessionOnAuthenticate_SetsNewCookie(t *testing.T) {
	q := newTestQueries(t)
	svc, err := auth.NewService(testLogger(), config.Config{PublicOrigin: testPublicOrigin}, q)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	// The Service's own internal *SessionManager (built by NewService)
	// uses the real wall clock, which would make a session anchored to
	// testutil.Epoch already look absolute-expired (90d timeout, and
	// Epoch is years in the past) rather than merely "past rotationAge" --
	// SetSessionManagerForTest (export_test.go) swaps in a fake-clocked
	// manager so RequireSession authenticates against the SAME clock this
	// test advances, driving rotation deterministically through the real
	// HTTP handler chain.
	clk := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerForTest(q, clk.Now)
	auth.SetSessionManagerForTest(svc, sm)
	handler := api.New(testLogger(), noopPinger{}, api.Options{}, svc.RegisterRoutes)

	userID := createTestUser(t, q)
	raw, predecessor, err := sm.Issue(context.Background(), userID, "ua", "203.0.113.61")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	clk.Advance(25 * time.Hour) // past rotationAge (24h)

	resp := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d (rotation must not fail the request)", resp.StatusCode, http.StatusOK)
	}

	rotated := extractCookie(resp, auth.SessionCookieName)
	if rotated == nil {
		t.Fatal("response missing a __Host-session Set-Cookie header, want the rotated successor's token")
	}
	if rotated.Value == "" || rotated.Value == raw {
		t.Errorf("rotated cookie value = %q, want a new, non-empty token distinct from the predecessor's %q", rotated.Value, raw)
	}

	var body meEnvelopeBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	if body.Data.User.ID != userID.String() {
		t.Errorf("user.id = %q, want %q (rotation must not change which user this session belongs to)", body.Data.User.ID, userID.String())
	}
	_ = predecessor
}
