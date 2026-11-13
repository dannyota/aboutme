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

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

func newSessionAPITestService(t *testing.T) (http.Handler, *store.Queries) {
	t.Helper()

	pool := newTestPool(t)
	q := store.New(pool)
	svc, err := auth.NewService(testLogger(), config.Config{PublicOrigin: testPublicOrigin}, pool)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	handler := api.New(testLogger(), noopPinger{}, api.Options{}, nil, svc.RegisterRoutes)
	return handler, q
}

func issueTestSession(t *testing.T, q *store.Queries, userID uuid.UUID) (raw string, sess store.Session) {
	t.Helper()

	sm := auth.NewSessionManager(q)
	raw, sess, err := sm.Issue(context.Background(), userID, "test-agent/1.0", "203.0.113.60")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	return raw, sess
}

func sessionRequestCookie(raw string) *http.Cookie {
	return requestCookie(auth.SessionCookieName, raw)
}

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

type errorEnvelopeBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

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

	wantToken := csrfTokenFor(sess)
	if body.Data.CSRFToken != wantToken {
		t.Errorf("csrfToken = %q, want %q (base64.RawURLEncoding of the session's own csrf_secret)", body.Data.CSRFToken, wantToken)
	}

	if len(body.Data.Identities) != 1 || body.Data.Identities[0].Provider != string(auth.ProviderGoogle) {
		t.Errorf("identities = %+v, want exactly one entry with provider %q", body.Data.Identities, auth.ProviderGoogle)
	}
}

func TestGetMe_EqualTimeIdentitiesUseIDTiebreaker(t *testing.T) {
	handler, q := newSessionAPITestService(t)
	ctx := context.Background()

	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open fixture pool: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })

	userID := createTestUser(t, q)
	t.Cleanup(func() {
		if _, cleanupErr := pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID); cleanupErr != nil {
			t.Errorf("delete test user: %v", cleanupErr)
		}
	})

	lowerID := userID
	lowerID[0] = 0x00
	higherID := userID
	higherID[0] = 0xff
	createdAt := time.Date(2026, time.August, 12, 0, 0, 0, 0, time.UTC)

	fixtures := []struct {
		id       uuid.UUID
		provider auth.Provider
	}{
		{id: higherID, provider: auth.ProviderGitHub},
		{id: lowerID, provider: auth.ProviderGoogle},
	}
	for _, fixture := range fixtures {
		if _, err := pool.Exec(ctx, `
			INSERT INTO identities (id, user_id, provider, provider_user_id, created_at)
			VALUES ($1, $2, $3, $4, $5)
		`, fixture.id, userID, string(fixture.provider), string(fixture.provider)+"-subject-"+userID.String(), createdAt); err != nil {
			t.Fatalf("insert %s identity: %v", fixture.provider, err)
		}
	}

	raw, _ := issueTestSession(t, q, userID)
	resp := doJSON(t, handler, http.MethodGet, auth.MePath, "", "", "", sessionRequestCookie(raw)) //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /me status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body meEnvelopeBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode GET /me response: %v", err)
	}
	want := []string{string(auth.ProviderGoogle), string(auth.ProviderGitHub)}
	if len(body.Data.Identities) != len(want) {
		t.Fatalf("GET /me identities = %+v, want %d entries", body.Data.Identities, len(want))
	}
	for i, provider := range want {
		if got := body.Data.Identities[i].Provider; got != provider {
			t.Fatalf("GET /me identities[%d].provider = %q, want %q", i, got, provider)
		}
	}
}

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

	if want := csrfTokenFor(sess); token != want {
		t.Errorf("csrfToken = %q, want %q", token, want)
	}
}

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

func TestGetMe_WrongMethod_Returns405(t *testing.T) {
	handler, _ := newSessionAPITestService(t)

	resp := doJSON(t, handler, http.MethodPost, auth.MePath, "", "", "") //nolint:bodyclose // doJSON closes the body itself before returning.
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST %s status = %d, want %d", auth.MePath, resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestGetMe_RotatesSessionOnAuthenticate_SetsNewCookie(t *testing.T) {
	pool := newTestPool(t)
	q := store.New(pool)
	svc, err := auth.NewService(testLogger(), config.Config{PublicOrigin: testPublicOrigin}, pool)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	clk := testutil.NewClockAtEpoch()
	sm := auth.NewSessionManagerForTest(q, clk.Now)
	auth.SetSessionManagerForTest(svc, sm)
	handler := api.New(testLogger(), noopPinger{}, api.Options{}, nil, svc.RegisterRoutes)

	userID := createTestUser(t, q)
	raw, predecessor, err := sm.Issue(context.Background(), userID, "ua", "203.0.113.61")
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}

	clk.Advance(25 * time.Hour)

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
