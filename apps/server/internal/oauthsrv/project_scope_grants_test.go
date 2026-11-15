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

type projectScopeRequest struct {
	requested string
	canonical string
}

var projectScopeRequests = []projectScopeRequest{
	{"projects:read", "projects:read"},
	{"projects:write", "projects:write"},
	{"projects:read projects:write", "projects:read projects:write"},
	{"resumes:read projects:read", "resumes:read projects:read"},
	{"projects:write resumes:write", "resumes:write projects:write"},
	{"projects:write projects:read resumes:write resumes:read", "resumes:read resumes:write projects:read projects:write"},
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

func TestAuthorize_SortsProjectScopeRequestsAndShowsConsent(t *testing.T) {
	s, q, client, user := newAuthorizeHarness(t)
	sess := issueTestSession(t, s.pool, user.ID, s.clock(), nil)
	for _, tc := range projectScopeRequests {
		t.Run(tc.requested, func(t *testing.T) {
			signedOut := httptest.NewRecorder()
			s.HandleAuthorize(signedOut, httptest.NewRequestWithContext(context.Background(), http.MethodGet, authorizeURL(client.ID, tc.requested), nil))
			login := urlMustParse(t, signedOut.Header().Get("Location"))
			if signedOut.Code != http.StatusFound || login.Path != "/login" {
				t.Fatalf("signed out = %d %q, want 302 to /login", signedOut.Code, login.String())
			}
			if next := urlMustParse(t, login.Query().Get("next")); next.Path != "/oauth/authorize" || next.Query().Get("scope") != tc.canonical {
				t.Fatalf("signed out next = %q, want /oauth/authorize with scope %q", next.String(), tc.canonical)
			}
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, authorizeURL(client.ID, tc.requested), nil)
			signedIn := httptest.NewRecorder()
			s.HandleAuthorize(signedIn, req.WithContext(auth.ContextWithSession(req.Context(), sess)))
			consent := urlMustParse(t, signedIn.Header().Get("Location"))
			if signedIn.Code != http.StatusFound || consent.Path != "/authorize" || consent.Query().Get("scope") != tc.canonical ||
				consent.Query().Get("code") != "" {
				t.Fatalf("signed in = %d %q, want 302 to the consent page with scope %q", signedIn.Code, consent.String(), tc.canonical)
			}
		})
	}
	for _, scope := range []string{"projects:read projects:read", "projects:admin", "resumes:read projects:read resumes:read"} {
		t.Run("refuses "+scope, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.HandleAuthorize(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, authorizeURL(client.ID, scope), nil))
			redirect := urlMustParse(t, rec.Header().Get("Location"))
			if rec.Code != http.StatusFound || redirect.Host != "agent.example" || redirect.Query().Get("error") != "invalid_scope" {
				t.Fatalf("%q = %d %q, want error=invalid_scope on the registered redirect", scope, rec.Code, redirect.String())
			}
		})
	}
	if _, err := q.GetLiveOAuthGrant(t.Context(), store.GetLiveOAuthGrantParams{UserID: user.ID, ClientID: client.ID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("authorize without consent left a grant: %v", err)
	}
}

func TestSessionConsentHTTPHandler_ShowsApprovesAndNarrowsProjectScopes(t *testing.T) {
	h := newOAuthSessionHTTPHarness(t)
	handler := h.service.ConsentHTTPHandler(h.sessions)
	for _, tc := range append(projectScopeRequests, projectScopeRequest{"resumes:read", "resumes:read"}) {
		t.Run(tc.requested, func(t *testing.T) {
			q := consentQuery(h.client.ID, tc.requested)
			view := httptest.NewRecorder()
			handler.ServeHTTP(view, h.request(http.MethodGet, "https://aboutme.example/api/v1/oauth/consent?"+q.values().Encode(), "", true, false))
			var shown struct {
				Data struct {
					Scopes []string `json:"scopes"`
				} `json:"data"`
			}
			if view.Code != http.StatusOK || json.Unmarshal(view.Body.Bytes(), &shown) != nil ||
				strings.Join(shown.Data.Scopes, " ") != tc.canonical {
				t.Fatalf("consent view = %d %q, want 200 listing %q", view.Code, view.Body.String(), tc.canonical)
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
			var decided struct {
				Data struct {
					RedirectTo string `json:"redirectTo"`
				} `json:"data"`
			}
			if approve.Code != http.StatusOK || json.Unmarshal(approve.Body.Bytes(), &decided) != nil {
				t.Fatalf("consent approval = %d %q, want 200 with a redirect", approve.Code, approve.Body.String())
			}
			digest, err := ParseCode(urlMustParse(t, decided.Data.RedirectTo).Query().Get("code"))
			if err != nil {
				t.Fatalf("approval redirect %q carries no code: %v", decided.Data.RedirectTo, err)
			}
			code, err := h.queries.GetOAuthAuthorizationCodeByDigest(t.Context(), digest[:])
			if err != nil || code.Scopes != tc.canonical {
				t.Fatalf("code scopes = %q, %v; want %q", code.Scopes, err, tc.canonical)
			}
			if got := mustLiveGrant(t, h.queries, h.user.ID, h.client.ID).Scopes; got != tc.canonical {
				t.Fatalf("live grant = %q, want %q", got, tc.canonical)
			}
		})
	}
}

func TestMetadata_AdvertisesResumeAndProjectScopes(t *testing.T) {
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
			want := []string{"resumes:read", "resumes:write", "projects:read", "projects:write"}
			if !reflect.DeepEqual(document.ScopesSupported, want) {
				t.Fatalf("%s metadata (CIMD %t) scopes_supported = %v, want %v", name, cimdEnabled, document.ScopesSupported, want)
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

func TestRegister_AcceptsProjectScopes(t *testing.T) {
	const base = `{"client_name":"Agent","redirect_uris":["https://agent.example/callback"]`
	for _, tc := range projectScopeRequests {
		t.Run(tc.requested, func(t *testing.T) {
			q := &registrationQueries{id: uuid.New()}
			s := newRegistrationService(t, q, &registrationAdmissionFake{allowed: true})
			rec := httptest.NewRecorder()
			s.HandleRegister(rec, registerRequest(http.MethodPost, "application/json", base+`,"scope":"`+tc.requested+`"}`))
			if rec.Code != http.StatusCreated || len(q.created) != 1 {
				t.Fatalf("registration declaring %q = %d, created %d; want 201 and one client", tc.requested, rec.Code, len(q.created))
			}
		})
	}
}

func TestCIMDDocumentValidation_AcceptsProjectScopes(t *testing.T) {
	id := "https://client.example/metadata"
	valid := `{"client_id":"` + id + `","client_name":"Fixture","redirect_uris":["https://client.example/callback"],"token_endpoint_auth_method":"none"`
	for _, tc := range projectScopeRequests {
		if _, err := validateCIMDDocument([]byte(valid+`,"scope":"`+tc.requested+`"}`), id, "client.example"); err != nil {
			t.Fatalf("document declaring %q: %v, want accepted", tc.requested, err)
		}
	}
	if _, err := validateCIMDDocument([]byte(valid+`,"scope":"projects:read projects:read"}`), id, "client.example"); err == nil {
		t.Fatal("document declaring a repeated scope accepted, want refused")
	}
}

func TestSessionConsentHTTPHandler_SortsTheSchemaNonCanonicalExample(t *testing.T) {
	h := newOAuthSessionHTTPHarness(t)
	q := consentQuery(h.client.ID, "projects:write resumes:write")
	view := httptest.NewRecorder()
	h.service.ConsentHTTPHandler(h.sessions).ServeHTTP(view,
		h.request(http.MethodGet, "https://aboutme.example/api/v1/oauth/consent?"+q.values().Encode(), "", true, false))
	var shown struct {
		Data struct {
			Scopes []string `json:"scopes"`
		} `json:"data"`
	}
	if view.Code != http.StatusOK || json.Unmarshal(view.Body.Bytes(), &shown) != nil ||
		!reflect.DeepEqual(shown.Data.Scopes, []string{"resumes:write", "projects:write"}) {
		t.Fatalf("consent view of %q = %d %q, want 200 listing resumes:write then projects:write", q.Scope, view.Code, view.Body.String())
	}
}
