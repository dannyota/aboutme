package app

import (
	"log/slog"
	"net/http"

	"danny.vn/aboutme/apps/server/internal/accountapi"
	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/projectapi"
	"danny.vn/aboutme/apps/server/internal/publicapi"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/realtimeapi"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
	"danny.vn/aboutme/apps/server/internal/viewapi"
)

type RouterDependencies struct {
	Config       config.Config
	Logger       *slog.Logger
	Readiness    *publicstate.Readiness
	Public       *publicapi.Service
	Providers    *auth.Service
	Account      *accountapi.Service
	Resumes      *resumeapi.Service
	Projects     *projectapi.Service
	Passwords    *auth.PasswordService
	Agents       func(*http.ServeMux)
	SecondFactor func(*http.ServeMux)
	Streams      *realtimeapi.Service
	Views        *viewapi.Service
}

func NewRouter(deps RouterDependencies) http.Handler {
	cfg := deps.Config
	return api.New(deps.Logger, deps.Readiness, api.Options{
		TrustedProxies: api.TrustedProxies(cfg.TrustedProxyCIDRs),
	}, deps.Public, deps.Providers.RegisterRoutes, deps.Account.RegisterRoutes, deps.Resumes.RegisterRoutes, deps.Projects.RegisterRoutes, deps.Passwords.RegisterRoutes, deps.Agents, deps.SecondFactor, capabilitiesRegistrar(cfg), deps.Streams.RegisterRoutes, deps.Views.RegisterRoutes)
}

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
