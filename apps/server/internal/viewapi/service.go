// Package viewapi serves the view-count routes: the public page script's
// start and collect, and the owner's reads of daily aggregates
// (docs/design/viewer-analytics/delivery.md, "API"). Owner routes take the
// session cookie only; no bearer token or MCP tool reaches them.
package viewapi

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/viewcount"
)

// Route paths.
const (
	StartPath   = "/api/v1/public/resumes/{slug}/views/start"
	CollectPath = "/api/v1/public/views/collect"
	SummaryPath = "/api/v1/views"
	DetailPath  = "/api/v1/views/{id}"
)

// Rate policies (docs/design/budgets.md, "Viewer analytics"; the owner
// reads share the resume read policy).
const (
	publicRequests = 30
	ownerRequests  = 600
)

// ErrInvalidConfig reports a missing dependency.
var ErrInvalidConfig = errors.New("viewapi: invalid config")

// Dependencies configure the Service.
type Dependencies struct {
	Counter        *viewcount.Counter
	Queries        OwnerQueries
	Sessions       *auth.SessionManager
	PublicOrigin   string
	TrustedProxies api.TrustedProxies
	Now            func() time.Time
	Logger         *slog.Logger
}

// Service owns the view-count routes.
type Service struct {
	counter        *viewcount.Counter
	queries        OwnerQueries
	sessions       *auth.SessionManager
	publicOrigin   string
	trustedProxies api.TrustedProxies
	now            func() time.Time
	logger         *slog.Logger
}

// New validates the dependencies.
func New(deps Dependencies) (*Service, error) {
	if deps.Counter == nil || deps.Queries == nil || deps.Sessions == nil || deps.PublicOrigin == "" {
		return nil, ErrInvalidConfig
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &Service{
		counter: deps.Counter, queries: deps.Queries, sessions: deps.Sessions,
		publicOrigin: deps.PublicOrigin, trustedProxies: deps.TrustedProxies,
		now: deps.Now, logger: deps.Logger,
	}, nil
}

// RegisterRoutes installs the four routes. Each public route checks its
// method, Origin, media type, and body before the optional session lookup,
// which only recognizes the owner.
func (s *Service) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle(StartPath, s.publicLimit()(s.publicRoute(s.handleStart)))
	mux.Handle(CollectPath, s.publicLimit()(s.publicRoute(s.handleCollect)))
	owner := s.ownerChain()
	mux.Handle(SummaryPath, getOnly(owner(http.HandlerFunc(s.handleSummary))))
	mux.Handle(DetailPath, getOnly(owner(http.HandlerFunc(s.handleDetail))))
}

func (s *Service) publicLimit() api.Middleware {
	return api.RateLimit(api.RateLimiterConfig{
		Requests: publicRequests, Window: time.Minute, TrustedProxies: s.trustedProxies,
		Clock: s.now, Key: api.IPKeyFunc, Logger: s.logger,
	})
}

func (s *Service) publicRoute(handle func(http.ResponseWriter, *http.Request, []byte)) http.Handler {
	optional := auth.OptionalSession(s.sessions)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := s.preflight(w, r)
		if !ok {
			return
		}
		optional(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handle(w, r, body)
		})).ServeHTTP(w, r)
	})
}

func (s *Service) ownerChain() api.Middleware {
	limit := api.RateLimit(api.RateLimiterConfig{
		Requests: ownerRequests, Window: time.Minute, TrustedProxies: s.trustedProxies,
		Clock: s.now, Key: api.CompositeKeyFunc(api.AccountKeyFunc, api.IPKeyFunc), Logger: s.logger,
	})
	session := auth.RequireSession(s.sessions)
	return func(next http.Handler) http.Handler {
		return session(limit(next))
	}
}

func getOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			api.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed on this route")
			return
		}
		next.ServeHTTP(w, r)
	})
}
