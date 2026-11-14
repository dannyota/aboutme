package oauthsrv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/store"
)

const grantScopesWithProjects = "resumes:read resumes:write projects:read projects:write"

var refusedProjectScopeRequests = []string{
	"projects:read",
	"projects:write",
	"projects:read projects:write",
	"resumes:read projects:read",
	"resumes:write projects:write",
	"resumes:read resumes:write projects:read projects:write",
}

type listedAgentGrant struct {
	ID     string   `json:"id"`
	Scopes []string `json:"scopes"`
}

func listAgentGrants(t *testing.T, h oauthSessionHTTPHarness) []listedAgentGrant {
	t.Helper()
	recorder := httptest.NewRecorder()
	h.service.AgentGrantsHTTPHandler(h.sessions).ServeHTTP(recorder, h.request(http.MethodGet, "https://aboutme.example/api/v1/me/agents", "", true, false))
	if recorder.Code != http.StatusOK {
		t.Fatalf("Connected agents list = %d %q, want 200", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Data struct {
			Grants []listedAgentGrant `json:"grants"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode Connected agents list %q: %v", recorder.Body.String(), err)
	}
	return body.Data.Grants
}

func sortedScopes(raw []string) []string {
	out := slices.Clone(raw)
	slices.Sort(out)
	return out
}

func TestSessionAgentGrantHTTPHandlers_ListAndRevokeGrantHoldingProjectScopes(t *testing.T) {
	h := newOAuthSessionHTTPHarness(t)
	ctx := context.Background()
	now := h.service.clock()
	resumeClient, err := h.queries.CreateOAuthClient(ctx, store.CreateOAuthClientParams{
		ClientName: "Resume-only agent", RedirectURIs: h.client.RedirectURIs, CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("CreateOAuthClient: %v", err)
	}
	t.Cleanup(func() {
		if _, cleanupErr := h.queries.DeleteOAuthClient(context.Background(), resumeClient.ID); cleanupErr != nil {
			t.Errorf("DeleteOAuthClient cleanup: %v", cleanupErr)
		}
	})
	resumeGrant, err := h.queries.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{
		UserID: h.user.ID, ClientID: resumeClient.ID, Scopes: "resumes:read resumes:write", CreatedAt: now.Add(-2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("seed resume-only grant: %v", err)
	}
	projectGrant, err := h.queries.UpsertOAuthGrant(ctx, store.UpsertOAuthGrantParams{
		UserID: h.user.ID, ClientID: h.client.ID, Scopes: grantScopesWithProjects, CreatedAt: now.Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("store a grant holding %q the way consent stores it: %v", grantScopesWithProjects, err)
	}
	token, err := h.queries.CreateOAuthToken(ctx, store.CreateOAuthTokenParams{
		TokenDigest: bytes.Repeat([]byte{0x6b}, 32), Kind: "access", FamilyID: uuid.New(),
		ClientID: h.client.ID, UserID: h.user.ID, GrantID: projectGrant.ID, CreatedAt: now,
		ExpiresAt: now.Add(time.Hour), FamilyExpiresAt: now.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("seed token: %v", err)
	}

	listed := listAgentGrants(t, h)
	byID := make(map[string][]string, len(listed))
	for _, grant := range listed {
		byID[grant.ID] = grant.Scopes
	}
	if len(listed) != 2 {
		t.Fatalf("Connected agents lists %d grants %#v, want the resume-only and the project-scope grant", len(listed), listed)
	}
	if got, want := sortedScopes(byID[projectGrant.ID.String()]), sortedScopes(strings.Fields(grantScopesWithProjects)); !reflect.DeepEqual(got, want) {
		t.Fatalf("project-scope grant scopes = %v, want %v", byID[projectGrant.ID.String()], want)
	}
	if got := byID[resumeGrant.ID.String()]; !reflect.DeepEqual(got, []string{"resumes:read", "resumes:write"}) {
		t.Fatalf("resume-only grant scopes = %v, want [resumes:read resumes:write]", got)
	}

	revokeRequest := h.request(http.MethodDelete, "https://aboutme.example/api/v1/me/agents/"+projectGrant.ID.String(), "", true, true)
	revokeRequest.SetPathValue("grantId", projectGrant.ID.String())
	revoked := httptest.NewRecorder()
	h.service.AgentGrantHTTPHandler(h.sessions).ServeHTTP(revoked, revokeRequest)
	if revoked.Code != http.StatusNoContent || revoked.Body.Len() != 0 {
		t.Fatalf("revoke project-scope grant = %d %q, want 204", revoked.Code, revoked.Body.String())
	}
	if _, grantErr := h.queries.GetLiveOAuthGrant(ctx, store.GetLiveOAuthGrantParams{UserID: h.user.ID, ClientID: h.client.ID}); !errors.Is(grantErr, pgx.ErrNoRows) {
		t.Fatalf("project-scope grant remained live: %v", grantErr)
	}
	authority, err := h.queries.GetOAuthTokenAuthorityByDigest(ctx, token.TokenDigest)
	if err != nil || authority.OAuthToken.RevokedAt == nil {
		t.Fatalf("token after project-scope grant revoke = %#v, %v", authority.OAuthToken, err)
	}
	remaining := listAgentGrants(t, h)
	if len(remaining) != 1 || remaining[0].ID != resumeGrant.ID.String() {
		t.Fatalf("Connected agents after revoke = %#v, want only the resume-only grant", remaining)
	}
}

func TestAuthorize_RefusesProjectScopesWhileConsentDoesNotOfferThem(t *testing.T) {
	s, q, client, user := newAuthorizeHarness(t)
	sess := issueTestSession(t, s.pool, user.ID, s.clock(), nil)
	for _, scope := range refusedProjectScopeRequests {
		t.Run(scope, func(t *testing.T) {
			for _, signedIn := range []bool{false, true} {
				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, authorizeURL(client.ID, scope), nil)
				if signedIn {
					req = req.WithContext(auth.ContextWithSession(req.Context(), sess))
				}
				rec := httptest.NewRecorder()
				s.HandleAuthorize(rec, req)
				if rec.Code != http.StatusFound {
					t.Fatalf("signed in %t: status = %d, want 302 invalid_scope", signedIn, rec.Code)
				}
				redirect := urlMustParse(t, rec.Header().Get("Location"))
				if redirect.Host != "agent.example" || redirect.Query().Get("error") != "invalid_scope" ||
					redirect.Query().Get("code") != "" || redirect.Query().Get("state") != "opaque<&state" {
					t.Fatalf("signed in %t: redirect = %q, want the registered redirect with error=invalid_scope and no code", signedIn, redirect.String())
				}
			}
		})
	}
	if _, err := q.GetLiveOAuthGrant(t.Context(), store.GetLiveOAuthGrantParams{UserID: user.ID, ClientID: client.ID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("refused project-scope requests left a grant: %v", err)
	}
	var codes int
	if err := s.pool.QueryRow(t.Context(), "SELECT count(*) FROM oauth_authorization_codes WHERE user_id = $1", user.ID).Scan(&codes); err != nil || codes != 0 {
		t.Fatalf("refused project-scope requests left %d codes, error = %v", codes, err)
	}
}

func TestSessionConsentHTTPHandler_RefusesProjectScopesWhileConsentDoesNotOfferThem(t *testing.T) {
	h := newOAuthSessionHTTPHarness(t)
	handler := h.service.ConsentHTTPHandler(h.sessions)
	for _, scope := range refusedProjectScopeRequests {
		t.Run(scope, func(t *testing.T) {
			q := consentQuery(h.client.ID, scope)
			view := httptest.NewRecorder()
			handler.ServeHTTP(view, h.request(http.MethodGet, "https://aboutme.example/api/v1/oauth/consent?"+q.values().Encode(), "", true, false))
			if view.Code != http.StatusBadRequest || responseErrorCode(t, view) != "request_invalid" {
				t.Fatalf("consent view = %d %q, want 400 request_invalid", view.Code, view.Body.String())
			}
			body, err := json.Marshal(map[string]string{
				"client_id": q.ClientID.String(), "redirect_uri": q.RedirectURI,
				"response_type": q.ResponseType, "scope": q.Scope, "state": q.State,
				"code_challenge": q.CodeChallenge, "code_challenge_method": q.CodeChallengeMethod,
				"decision": "approve",
			})
			if err != nil {
				t.Fatalf("marshal consent decision: %v", err)
			}
			approve := httptest.NewRecorder()
			handler.ServeHTTP(approve, h.request(http.MethodPost, "https://aboutme.example/api/v1/oauth/consent", string(body), true, true))
			if approve.Code != http.StatusBadRequest || responseErrorCode(t, approve) != "request_invalid" || strings.Contains(approve.Body.String(), "redirectTo") {
				t.Fatalf("consent approval = %d %q, want 400 request_invalid", approve.Code, approve.Body.String())
			}
		})
	}
	if _, err := h.queries.GetLiveOAuthGrant(t.Context(), store.GetLiveOAuthGrantParams{UserID: h.user.ID, ClientID: h.client.ID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("refused project-scope approvals left a grant: %v", err)
	}
	var codes int
	if err := h.service.pool.QueryRow(t.Context(), "SELECT count(*) FROM oauth_authorization_codes WHERE user_id = $1", h.user.ID).Scan(&codes); err != nil || codes != 0 {
		t.Fatalf("refused project-scope approvals left %d codes, error = %v", codes, err)
	}
}

func TestMetadata_AdvertisesOnlyResumeScopesWhileConsentDoesNotOfferProjectScopes(t *testing.T) {
	for _, cimdEnabled := range []bool{false, true} {
		s, _, _, _ := newAuthorizeHarness(t)
		s.cimdEnabled = cimdEnabled
		for name, handler := range map[string]http.HandlerFunc{
			"authorization server": s.HandleMetadata,
			"protected resource":   s.HandleProtectedResourceMetadata,
		} {
			recorder := httptest.NewRecorder()
			handler(recorder, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "https://aboutme.example/.well-known/metadata", nil))
			var document struct {
				ScopesSupported []string `json:"scopes_supported"`
			}
			if recorder.Code != http.StatusOK || json.Unmarshal(recorder.Body.Bytes(), &document) != nil {
				t.Fatalf("%s metadata (CIMD %t) = %d %q", name, cimdEnabled, recorder.Code, recorder.Body.String())
			}
			if !reflect.DeepEqual(document.ScopesSupported, []string{"resumes:read", "resumes:write"}) {
				t.Fatalf("%s metadata (CIMD %t) scopes_supported = %v, want [resumes:read resumes:write]", name, cimdEnabled, document.ScopesSupported)
			}
		}
	}
}

func TestAuthorize_HeldProjectScopeGrantCoversNarrowerResumeRequestAsToday(t *testing.T) {
	s, q, client, user := newAuthorizeHarness(t)
	if _, err := q.UpsertOAuthGrant(t.Context(), store.UpsertOAuthGrantParams{
		UserID: user.ID, ClientID: client.ID, Scopes: grantScopesWithProjects, CreatedAt: s.clock(),
	}); err != nil {
		t.Fatalf("store a grant holding %q the way consent stores it: %v", grantScopesWithProjects, err)
	}
	sess := issueTestSession(t, s.pool, user.ID, s.clock(), nil)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, authorizeURL(client.ID, "resumes:read resumes:write"), nil)
	req = req.WithContext(auth.ContextWithSession(req.Context(), sess))
	rec := httptest.NewRecorder()
	s.HandleAuthorize(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 with a code", rec.Code)
	}
	redirect := urlMustParse(t, rec.Header().Get("Location"))
	if redirect.Host != "agent.example" || redirect.Query().Get("code") == "" {
		t.Fatalf("redirect = %q, want a silent code on the registered redirect", redirect.String())
	}
	digest, err := ParseCode(redirect.Query().Get("code"))
	if err != nil {
		t.Fatalf("ParseCode: %v", err)
	}
	code, err := q.GetOAuthAuthorizationCodeByDigest(t.Context(), digest[:])
	if err != nil || code.Scopes != "resumes:read resumes:write" {
		t.Fatalf("code scopes = %q, %v; want the requested resume scopes", code.Scopes, err)
	}
	if got := mustLiveGrant(t, q, user.ID, client.ID).Scopes; got != "resumes:read resumes:write" {
		t.Fatalf("live grant after the narrower request = %q, want the requested resume scopes", got)
	}
}

func TestRegister_RefusesProjectScopesWhileConsentDoesNotOfferThem(t *testing.T) {
	const base = `{"client_name":"Agent","redirect_uris":["https://agent.example/callback"]`
	for _, scope := range refusedProjectScopeRequests {
		t.Run(scope, func(t *testing.T) {
			q := &registrationQueries{id: uuid.New()}
			s := newRegistrationService(t, q, &registrationAdmissionFake{allowed: true})
			rec := httptest.NewRecorder()
			s.HandleRegister(rec, registerRequest(http.MethodPost, "application/json", base+`,"scope":"`+scope+`"}`))
			if rec.Code != http.StatusBadRequest || len(q.created) != 0 {
				t.Fatalf("registration declaring %q = %d, created %d; want 400 and nothing created", scope, rec.Code, len(q.created))
			}
		})
	}
}

func TestCIMDDocumentValidation_RefusesProjectScopesWhileConsentDoesNotOfferThem(t *testing.T) {
	id := "https://client.example/metadata"
	valid := `{"client_id":"` + id + `","client_name":"Fixture","redirect_uris":["https://client.example/callback"],"token_endpoint_auth_method":"none"`
	if _, err := validateCIMDDocument([]byte(valid+`,"scope":"resumes:read resumes:write"}`), id, "client.example"); err != nil {
		t.Fatalf("document declaring the resume scopes: %v, want accepted", err)
	}
	for _, scope := range refusedProjectScopeRequests {
		if _, err := validateCIMDDocument([]byte(valid+`,"scope":"`+scope+`"}`), id, "client.example"); err == nil {
			t.Fatalf("document declaring %q accepted, want refused", scope)
		}
	}
}
