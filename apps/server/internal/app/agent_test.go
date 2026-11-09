package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"danny.vn/aboutme/apps/server/internal/publicroots"
)

func TestAgentRoutesFollowPublicRootRegistryAndDisableCleanly(t *testing.T) {
	t.Parallel()

	seen := make(chan string, 16)
	mark := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			seen <- name
			w.WriteHeader(http.StatusNoContent)
		}
	}
	handlers := agentRouteHandlers{
		AuthorizationMetadata: mark("authorization metadata"),
		ProtectedMetadata:     mark("protected metadata"),
		Register:              mark("register"),
		Authorize:             mark("authorize"),
		Token:                 mark("token"),
		Revoke:                mark("revoke"),
		MCP:                   mark("mcp"),
		Consent:               mark("consent"),
		AgentGrants:           mark("agent grants"),
		AgentGrant:            mark("agent grant"),
	}
	register, err := newAgentRouteRegistrar(true, publicroots.Routes[:], handlers)
	if err != nil {
		t.Fatalf("newAgentRouteRegistrar: %v", err)
	}
	mux := http.NewServeMux()
	register(mux)

	for _, tc := range []struct{ path, want string }{
		{"/.well-known/oauth-authorization-server", "authorization metadata"},
		{"/.well-known/oauth-protected-resource", "protected metadata"},
		{"/oauth/register", "register"},
		{"/oauth/authorize", "authorize"},
		{"/oauth/token", "token"},
		{"/oauth/revoke", "revoke"},
		{"/mcp", "mcp"},
		{"/api/v1/oauth/consent", "consent"},
		{"/api/v1/me/agents", "agent grants"},
		{"/api/v1/me/agents/018f5b6a-9a3e-7c21-8b1e-000000000030", "agent grant"},
	} {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequestWithContext(context.Background(), http.MethodPost, tc.path, nil))
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("%s status = %d", tc.path, recorder.Code)
		}
		if got := <-seen; got != tc.want {
			t.Fatalf("%s reached %q, want %q", tc.path, got, tc.want)
		}
	}
	for _, nearMatch := range []string{"/.well-known/other", "/oauth", "/oauth/other", "/mcp/other", "/api/v1/me/agents/extra/path"} {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequestWithContext(context.Background(), http.MethodGet, nearMatch, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("near-match %s status = %d, want 404", nearMatch, recorder.Code)
		}
	}

	disabled, err := newAgentRouteRegistrar(false, publicroots.Routes[:], handlers)
	if err != nil {
		t.Fatalf("disabled registrar: %v", err)
	}
	disabledMux := http.NewServeMux()
	disabled(disabledMux)
	for _, path := range []string{"/mcp", "/api/v1/oauth/consent", "/api/v1/me/agents", "/api/v1/me/agents/018f5b6a-9a3e-7c21-8b1e-000000000030"} {
		recorder := httptest.NewRecorder()
		disabledMux.ServeHTTP(recorder, httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, nil))
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("disabled %s status = %d, want 404", path, recorder.Code)
		}
	}
}

func TestAgentRoutesFailClosedOnPartialRegistryOrHandlers(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	complete := agentRouteHandlers{
		AuthorizationMetadata: handler,
		ProtectedMetadata:     handler,
		Register:              handler,
		Authorize:             handler,
		Token:                 handler,
		Revoke:                handler,
		MCP:                   handler,
		Consent:               handler,
		AgentGrants:           handler,
		AgentGrant:            handler,
	}
	registry := append([]publicroots.Route(nil), publicroots.Routes[:]...)
	for i := range registry {
		if registry[i].Root == "mcp" {
			registry[i].Dispatch = publicroots.DispatchNuxt
		}
	}
	if _, err := newAgentRouteRegistrar(true, registry, complete); err == nil {
		t.Fatal("newAgentRouteRegistrar() accepted registry without Go-owned mcp root")
	}

	partial := complete
	partial.Token = nil
	if _, err := newAgentRouteRegistrar(true, publicroots.Routes[:], partial); err == nil {
		t.Fatal("newAgentRouteRegistrar() accepted partial handlers")
	}
}
