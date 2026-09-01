package oauthsrv

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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
)

type oauthSessionHTTPHarness struct {
	service    *Service
	queries    *store.Queries
	client     store.OAuthClient
	user       store.User
	sessions   *auth.SessionManager
	rawSession string
	session    store.Session
}

func newOAuthSessionHTTPHarness(t *testing.T) oauthSessionHTTPHarness {
	t.Helper()
	service, queries, client, user := newAuthorizeHarness(t)
	issuer := auth.NewSessionManagerWithPool(service.pool)
	raw, session, err := issuer.Issue(context.Background(), user.ID, "oauth-session-http-test", "127.0.0.1")
	if err != nil {
		t.Fatalf("Issue session: %v", err)
	}
	return oauthSessionHTTPHarness{
		service: service, queries: queries, client: client, user: user,
		sessions: auth.NewSessionManager(queries), rawSession: raw, session: session,
	}
}

func (h oauthSessionHTTPHarness) request(method, target, body string, authenticated, csrf bool) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if authenticated {
		req.AddCookie(&http.Cookie{Name: "__Host-session", Value: h.rawSession})
	}
	if csrf {
		req.Header.Set("Origin", h.service.publicOrigin)
		req.Header.Set(auth.CSRFHeaderName, base64.RawURLEncoding.EncodeToString(h.session.CSRFSecret))
	}
	return req
}

func consentAPIURL(clientID uuid.UUID) string {
	values := consentQuery(clientID, "resumes:read resumes:write").values()
	return "https://aboutme.example/api/v1/oauth/consent?" + values.Encode()
}

func consentDecisionBody(clientID uuid.UUID, decision string) string {
	q := consentQuery(clientID, "resumes:read")
	body, err := json.Marshal(map[string]string{
		"client_id": q.ClientID.String(), "redirect_uri": q.RedirectURI,
		"response_type": q.ResponseType, "scope": q.Scope, "state": q.State,
		"code_challenge": q.CodeChallenge, "code_challenge_method": q.CodeChallengeMethod,
		"decision": decision,
	})
	if err != nil {
		panic(err)
	}
	return string(body)
}

func responseErrorCode(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response %q: %v", recorder.Body.String(), err)
	}
	return body.Error.Code
}

func TestSessionConsentHTTPHandler_RequiresSessionAndRevalidatesQuery(t *testing.T) {
	h := newOAuthSessionHTTPHarness(t)
	handler := h.service.ConsentHTTPHandler(h.sessions)

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, h.request(http.MethodGet, consentAPIURL(h.client.ID), "", false, false))
	if unauthenticated.Code != http.StatusUnauthorized || responseErrorCode(t, unauthenticated) != "session_required" {
		t.Fatalf("unauthenticated response = %d %q", unauthenticated.Code, unauthenticated.Body.String())
	}

	valid := httptest.NewRecorder()
	handler.ServeHTTP(valid, h.request(http.MethodGet, consentAPIURL(h.client.ID), "", true, false))
	if valid.Code != http.StatusOK || !strings.Contains(valid.Body.String(), `"clientName":"Authorize agent"`) ||
		!strings.Contains(valid.Body.String(), `"scopes":["resumes:read","resumes:write"]`) ||
		strings.Contains(valid.Body.String(), "opaque<&state") || strings.Contains(valid.Body.String(), "code_challenge") {
		t.Fatalf("consent view = %d %q", valid.Code, valid.Body.String())
	}

	duplicate := httptest.NewRecorder()
	duplicateURL := consentAPIURL(h.client.ID) + "&scope=resumes%3Aread"
	handler.ServeHTTP(duplicate, h.request(http.MethodGet, duplicateURL, "", true, false))
	if duplicate.Code != http.StatusBadRequest || responseErrorCode(t, duplicate) != "request_invalid" {
		t.Fatalf("duplicate query response = %d %q", duplicate.Code, duplicate.Body.String())
	}

	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, h.request(http.MethodGet, consentAPIURL(uuid.New()), "", true, false))
	if missing.Code != http.StatusNotFound || responseErrorCode(t, missing) != "not_found" {
		t.Fatalf("missing client response = %d %q", missing.Code, missing.Body.String())
	}
}

func TestSessionConsentHTTPHandler_EnforcesCSRFMediaTypeAndStrictBody(t *testing.T) {
	h := newOAuthSessionHTTPHarness(t)
	handler := h.service.ConsentHTTPHandler(h.sessions)
	validBody := consentDecisionBody(h.client.ID, "deny")

	noCSRF := httptest.NewRecorder()
	handler.ServeHTTP(noCSRF, h.request(http.MethodPost, "https://aboutme.example/api/v1/oauth/consent", validBody, true, false))
	if noCSRF.Code != http.StatusForbidden || responseErrorCode(t, noCSRF) != "csrf_rejected" {
		t.Fatalf("missing CSRF response = %d %q", noCSRF.Code, noCSRF.Body.String())
	}

	wrongMedia := h.request(http.MethodPost, "https://aboutme.example/api/v1/oauth/consent", validBody, true, true)
	wrongMedia.Header.Set("Content-Type", "application/json; charset=utf-8")
	wrongMediaRecorder := httptest.NewRecorder()
	handler.ServeHTTP(wrongMediaRecorder, wrongMedia)
	if wrongMediaRecorder.Code != http.StatusUnsupportedMediaType || responseErrorCode(t, wrongMediaRecorder) != "media_type_unsupported" {
		t.Fatalf("wrong media response = %d %q", wrongMediaRecorder.Code, wrongMediaRecorder.Body.String())
	}

	duplicateBody := strings.Replace(validBody, `"decision":"deny"`, `"decision":"deny","decision":"approve"`, 1)
	duplicate := httptest.NewRecorder()
	handler.ServeHTTP(duplicate, h.request(http.MethodPost, "https://aboutme.example/api/v1/oauth/consent", duplicateBody, true, true))
	if duplicate.Code != http.StatusBadRequest || responseErrorCode(t, duplicate) != "request_invalid" {
		t.Fatalf("duplicate body response = %d %q", duplicate.Code, duplicate.Body.String())
	}

	tooLarge := httptest.NewRecorder()
	handler.ServeHTTP(tooLarge, h.request(http.MethodPost, "https://aboutme.example/api/v1/oauth/consent", strings.Repeat(" ", 4097), true, true))
	if tooLarge.Code != http.StatusRequestEntityTooLarge || responseErrorCode(t, tooLarge) != "body_too_large" {
		t.Fatalf("oversized response = %d %q", tooLarge.Code, tooLarge.Body.String())
	}

	exactLimit := validBody + strings.Repeat(" ", 4096-len(validBody))
	valid := httptest.NewRecorder()
	handler.ServeHTTP(valid, h.request(http.MethodPost, "https://aboutme.example/api/v1/oauth/consent", exactLimit, true, true))
	if valid.Code != http.StatusOK {
		t.Fatalf("4,096-byte decision response = %d %q", valid.Code, valid.Body.String())
	}
	var response struct {
		Data struct {
			RedirectTo string `json:"redirectTo"`
		} `json:"data"`
	}
	if err := json.Unmarshal(valid.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode decision response: %v", err)
	}
	redirect, err := url.Parse(response.Data.RedirectTo)
	if err != nil || redirect.Host != "agent.example" || redirect.Query().Get("error") != "access_denied" || redirect.Query().Get("state") != "opaque<&state" {
		t.Fatalf("decision redirect = %q, error = %v", response.Data.RedirectTo, err)
	}
}

func TestSessionAgentGrantHTTPHandlers_ListAndOwnerScopedRevoke(t *testing.T) {
	h := newOAuthSessionHTTPHarness(t)
	ctx := context.Background()
	now := h.service.clock()
	grant, err := h.queries.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{
		UserID: h.user.ID, ClientID: h.client.ID, Scopes: "resumes:read resumes:write", CreatedAt: now.Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("seed grant: %v", err)
	}
	createdAt := now
	token, err := h.queries.CreateOAuthToken(ctx, store.CreateOAuthTokenParams{
		TokenDigest: bytes.Repeat([]byte{0x5a}, 32), Kind: "access", FamilyID: uuid.New(),
		ClientID: h.client.ID, UserID: h.user.ID, GrantID: grant.ID, CreatedAt: createdAt,
		ExpiresAt: createdAt.Add(time.Hour), FamilyExpiresAt: createdAt.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("seed token: %v", err)
	}

	listHandler := h.service.AgentGrantsHTTPHandler(h.sessions)
	list := httptest.NewRecorder()
	listHandler.ServeHTTP(list, h.request(http.MethodGet, "https://aboutme.example/api/v1/me/agents", "", true, false))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), grant.ID.String()) ||
		!strings.Contains(list.Body.String(), `"clientName":"Authorize agent"`) || strings.Contains(list.Body.String(), "Wlpa") {
		t.Fatalf("grant list = %d %q", list.Code, list.Body.String())
	}

	revokeHandler := h.service.AgentGrantHTTPHandler(h.sessions)
	noCSRFRequest := h.request(http.MethodDelete, "https://aboutme.example/api/v1/me/agents/"+grant.ID.String(), "", true, false)
	noCSRFRequest.SetPathValue("grantId", grant.ID.String())
	noCSRF := httptest.NewRecorder()
	revokeHandler.ServeHTTP(noCSRF, noCSRFRequest)
	if noCSRF.Code != http.StatusForbidden || responseErrorCode(t, noCSRF) != "csrf_rejected" {
		t.Fatalf("revoke without CSRF = %d %q", noCSRF.Code, noCSRF.Body.String())
	}

	missingRequest := h.request(http.MethodDelete, "https://aboutme.example/api/v1/me/agents/"+uuid.NewString(), "", true, true)
	missingRequest.SetPathValue("grantId", uuid.NewString())
	missing := httptest.NewRecorder()
	revokeHandler.ServeHTTP(missing, missingRequest)
	if missing.Code != http.StatusNotFound || responseErrorCode(t, missing) != "not_found" {
		t.Fatalf("missing revoke = %d %q", missing.Code, missing.Body.String())
	}
	foreignUser, err := h.queries.CreateUser(ctx, store.CreateUserParams{
		Email: uuid.NewString() + "@example.test", Name: "Foreign grant owner",
	})
	if err != nil {
		t.Fatalf("seed foreign user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = h.service.pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", foreignUser.ID)
	})
	foreignGrant, err := h.queries.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{
		UserID: foreignUser.ID, ClientID: h.client.ID, Scopes: "resumes:read", CreatedAt: now.Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("seed foreign grant: %v", err)
	}
	foreignRequest := h.request(http.MethodDelete, "https://aboutme.example/api/v1/me/agents/"+foreignGrant.ID.String(), "", true, true)
	foreignRequest.SetPathValue("grantId", foreignGrant.ID.String())
	foreign := httptest.NewRecorder()
	revokeHandler.ServeHTTP(foreign, foreignRequest)
	if foreign.Code != missing.Code || foreign.Body.String() != missing.Body.String() {
		t.Fatalf("foreign revoke = %d %q, want byte-identical missing response %d %q", foreign.Code, foreign.Body.String(), missing.Code, missing.Body.String())
	}
	if _, err := h.queries.GetLiveOAuthGrant(ctx, store.GetLiveOAuthGrantParams{UserID: foreignUser.ID, ClientID: h.client.ID}); err != nil {
		t.Fatalf("foreign grant changed: %v", err)
	}

	revokeRequest := h.request(http.MethodDelete, "https://aboutme.example/api/v1/me/agents/"+grant.ID.String(), "", true, true)
	revokeRequest.SetPathValue("grantId", grant.ID.String())
	revoked := httptest.NewRecorder()
	revokeHandler.ServeHTTP(revoked, revokeRequest)
	if revoked.Code != http.StatusNoContent || revoked.Body.Len() != 0 {
		t.Fatalf("revoke response = %d %q", revoked.Code, revoked.Body.String())
	}
	if _, err := h.queries.GetLiveOAuthGrant(ctx, store.GetLiveOAuthGrantParams{UserID: h.user.ID, ClientID: h.client.ID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("grant remained live: %v", err)
	}
	authority, err := h.queries.GetOAuthTokenAuthorityByDigest(ctx, token.TokenDigest)
	if err != nil || authority.OAuthToken.RevokedAt == nil {
		t.Fatalf("token after grant revoke = %#v, %v", authority.OAuthToken, err)
	}

	repeat := httptest.NewRecorder()
	revokeHandler.ServeHTTP(repeat, revokeRequest.Clone(context.Background()))
	if repeat.Code != http.StatusNotFound || responseErrorCode(t, repeat) != "not_found" {
		t.Fatalf("repeat revoke = %d %q", repeat.Code, repeat.Body.String())
	}
}
