// Package app constructs the aboutme server's components for cmd/server.
// Constructors take explicit dependencies and return concrete components;
// they read no environment, open no listeners, and start no worker loops.
// The command keeps the dependency graph, listeners, and lifecycle. Domain
// packages never import this package.
package app

import (
	"log/slog"
	"net/http"

	"danny.vn/aboutme/apps/server/internal/accountapi"
	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/publicapi"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/realtimeapi"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
	"danny.vn/aboutme/apps/server/internal/viewapi"
)

// RouterDependencies are the services whose routes the public router serves.
type RouterDependencies struct {
	// Config supplies trusted proxies and the capability flags.
	Config config.Config
	// Logger receives router diagnostics.
	Logger *slog.Logger
	// Readiness answers /readyz.
	Readiness *publicstate.Readiness
	// Public serves public resume, discovery, and artifact routes.
	Public *publicapi.Service
	// Providers serves the authentication and session routes.
	Providers *auth.Service
	// Account serves authenticated account routes.
	Account *accountapi.Service
	// Resumes serves authenticated resume routes.
	Resumes *resumeapi.Service
	// Passwords serves the password routes.
	Passwords *auth.PasswordService
	// Agents attaches the agent-access routes, or nothing when disabled.
	Agents func(*http.ServeMux)
	// SecondFactor attaches every second-factor route.
	SecondFactor func(*http.ServeMux)
	// Streams serves the authenticated account stream.
	Streams *realtimeapi.Service
	// Views serves the view start, collect, and owner routes.
	Views *viewapi.Service
}

// NewRouter returns the public HTTP handler with every route registered in a
// fixed order.
func NewRouter(deps RouterDependencies) http.Handler {
	cfg := deps.Config
	return api.New(deps.Logger, deps.Readiness, api.Options{
		// TrustedProxyCIDRs is validated by internal/config (required and
		// loopback-checked in production; see config.Load) and converts
		// directly: api.TrustedProxies is a named []netip.Prefix, the same
		// underlying type config.Config.TrustedProxyCIDRs already is.
		TrustedProxies: api.TrustedProxies(cfg.TrustedProxyCIDRs),
	}, deps.Public, deps.Providers.RegisterRoutes, deps.Account.RegisterRoutes, deps.Resumes.RegisterRoutes, deps.Passwords.RegisterRoutes, deps.Agents, deps.SecondFactor, capabilitiesRegistrar(cfg), deps.Streams.RegisterRoutes, deps.Views.RegisterRoutes)
}

// capabilitiesRegistrar exposes the optional-surface flags and the enabled
// providers to the web without a second source of truth (ADR 0016).
func capabilitiesRegistrar(cfg config.Config) func(*http.ServeMux) {
	handler := api.CapabilitiesHandler(api.Capabilities{
		ProviderLogin:        cfg.ProviderLogin.Any(),
		Providers:            cfg.ProviderLogin.Names(),
		PasswordRegistration: !cfg.PasswordRegistrationDisabled,
		AgentAccess:          cfg.AgentAccess.Enabled,
		PasskeyEnrollment:    cfg.PasskeyEnrollment,
		TotpEnrollment:       cfg.TOTPEnrollment,
		SignInToView:         cfg.SignInToViewEnabled,
	})
	return func(mux *http.ServeMux) {
		mux.Handle("/api/v1/capabilities", handler)
	}
}
