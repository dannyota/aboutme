package app

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/mcpapi"
	"github.com/dannyota/aboutme/apps/server/internal/oauthsrv"
	"github.com/dannyota/aboutme/apps/server/internal/publicroots"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

type agentRouteHandlers struct {
	AuthorizationMetadata http.Handler
	ProtectedMetadata     http.Handler
	Register              http.Handler
	Authorize             http.Handler
	Token                 http.Handler
	Revoke                http.Handler
	MCP                   http.Handler
	Consent               http.Handler
	AgentGrants           http.Handler
	AgentGrant            http.Handler
}

func newAgentRouteRegistrar(enabled bool, registry []publicroots.Route, handlers agentRouteHandlers) (func(*http.ServeMux), error) {
	if !enabled {
		return func(*http.ServeMux) {}, nil
	}
	goRoots := make(map[string]bool, len(registry))
	for _, route := range registry {
		goRoots[route.Root] = route.Dispatch == publicroots.DispatchGo
	}
	for _, root := range []string{".well-known", "oauth", "mcp", "api"} {
		if !goRoots[root] {
			return nil, fmt.Errorf("agent access: public root %q is not dispatched to Go", root)
		}
	}
	if handlers.AuthorizationMetadata == nil || handlers.ProtectedMetadata == nil || handlers.Register == nil ||
		handlers.Authorize == nil || handlers.Token == nil || handlers.Revoke == nil || handlers.MCP == nil ||
		handlers.Consent == nil || handlers.AgentGrants == nil || handlers.AgentGrant == nil {
		return nil, errors.New("agent access: partial route handlers")
	}
	return func(mux *http.ServeMux) {
		mux.Handle("/.well-known/oauth-authorization-server", handlers.AuthorizationMetadata)
		mux.Handle("/.well-known/oauth-protected-resource", handlers.ProtectedMetadata)
		mux.Handle("/oauth/register", handlers.Register)
		mux.Handle("/oauth/authorize", handlers.Authorize)
		mux.Handle("/oauth/token", handlers.Token)
		mux.Handle("/oauth/revoke", handlers.Revoke)
		mux.Handle("/mcp", handlers.MCP)
		mux.Handle("/api/v1/oauth/consent", handlers.Consent)
		mux.Handle("/api/v1/me/agents", handlers.AgentGrants)
		mux.Handle("/api/v1/me/agents/{grantId}", handlers.AgentGrant)
	}, nil
}

// NewAgents validates the agent-access settings and returns the registrar for
// the OAuth authorization server, MCP, consent, and agent-grant routes. With
// agent access disabled the registrar attaches nothing.
func NewAgents(ctx context.Context, cfg config.Config, pool *store.Pool, queries store.OAuthQueries,
	resumes mcpapi.AgentExecutor, sessions *auth.SessionManager,
) (func(*http.ServeMux), error) {
	if err := cfg.ValidateAgentAccess(); err != nil {
		return nil, err
	}
	if !cfg.AgentAccess.Enabled {
		return newAgentRouteRegistrar(false, publicroots.Routes[:], agentRouteHandlers{})
	}
	a := cfg.AgentAccess
	oauthRates, err := oauthsrv.NewRatePolicies(oauthsrv.RateConfig{
		TrustedProxies:         api.TrustedProxies(cfg.TrustedProxyCIDRs),
		RegisterRequests:       a.OAuthRegisterRequests,
		RegisterEgressRanges:   a.OAuthRegisterEgressRanges,
		RegisterRangeRequests:  a.OAuthRegisterRangeRequests,
		RegisterGlobalRequests: a.OAuthRegisterGlobalRequests,
		RegisterWindow:         a.OAuthRegisterWindow,
		TokenRequests:          a.OAuthTokenRequests,
		TokenWindow:            a.OAuthTokenWindow,
		FailedGrantLimit:       a.OAuthFailedGrantLimit,
		FailedGrantWindow:      a.OAuthFailedGrantWindow,
		MaxKeys:                a.MaxRateKeys,
	})
	if err != nil {
		return nil, err
	}
	var cimd *oauthsrv.CIMDResolver
	if a.CIMD.Enabled {
		cimd, err = oauthsrv.NewCIMDResolver(a.CIMD, cfg.PublicOrigin, time.Now, a.MaxRateKeys)
		if err != nil {
			return nil, err
		}
	}
	oauthService, err := oauthsrv.NewService(ctx, oauthsrv.ServiceDependencies{
		CIMD: cimd, CIMDEnabled: a.CIMD.Enabled, TrustedProxies: api.TrustedProxies(cfg.TrustedProxyCIDRs),
		Pool: pool, Queries: queries, Clock: time.Now, Entropy: rand.Reader,
		PublicOrigin: cfg.PublicOrigin, RegisterAdmission: oauthRates, TokenAdmission: oauthRates,
		LiveGrantLimit: a.OAuthLiveGrantLimit,
	})
	if err != nil {
		return nil, err
	}
	bearer, err := mcpapi.NewBearer(mcpapi.BearerDependencies{Queries: queries, Clock: time.Now, PublicOrigin: cfg.PublicOrigin})
	if err != nil {
		return nil, err
	}
	mcpRates, err := mcpapi.NewRatePolicies(mcpapi.RateConfig{
		TokenRequests: a.MCPTokenRequests, TokenWindow: a.MCPTokenWindow,
		UserRequests: a.MCPUserRequests, UserWindow: a.MCPUserWindow,
		ConcurrentPerUser: a.MCPConcurrentPerUser, MaxKeys: a.MaxRateKeys, Clock: time.Now,
	})
	if err != nil {
		return nil, err
	}
	mcpHandler, err := mcpapi.NewServer(mcpapi.ServerDependencies{
		Bearer: bearer, Resumes: resumes, Rates: mcpRates, MaxRequestBodyBytes: a.MCPBodyLimitBytes,
	})
	if err != nil {
		return nil, err
	}
	authorize := auth.OptionalSession(sessions)(http.HandlerFunc(oauthService.HandleAuthorize))
	return newAgentRouteRegistrar(true, publicroots.Routes[:], agentRouteHandlers{
		AuthorizationMetadata: http.HandlerFunc(oauthService.HandleMetadata),
		ProtectedMetadata:     http.HandlerFunc(oauthService.HandleProtectedResourceMetadata),
		Register:              http.HandlerFunc(oauthService.HandleRegister),
		Authorize:             authorize,
		Token:                 http.HandlerFunc(oauthService.HandleToken),
		Revoke:                http.HandlerFunc(oauthService.HandleRevoke),
		MCP:                   mcpHandler,
		Consent:               oauthService.ConsentHTTPHandler(sessions),
		AgentGrants:           oauthService.AgentGrantsHTTPHandler(sessions),
		AgentGrant:            oauthService.AgentGrantHTTPHandler(sessions),
	})
}
