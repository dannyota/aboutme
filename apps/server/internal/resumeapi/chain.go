package resumeapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
)

const (
	resumeReadRequests   = 600
	resumeWriteRequests  = 240
	resumeUploadRequests = 20
)

var resumeRateLimitKey = api.CompositeKeyFunc(resumeAccountKey, api.IPKeyFunc)

func resumeAccountKey(r *http.Request, _ api.TrustedProxies) (string, bool) {
	accountID, ok := api.AccountIDFromContext(r.Context())
	if !ok || accountID == "" {
		return "", false
	}
	return "acct:" + accountID, true
}

type routeChains struct {
	read          api.Middleware
	write         api.Middleware
	upload        api.Middleware
	session       api.Middleware
	csrfJSON      api.Middleware
	csrfMultipart api.Middleware
}

func (s *Service) newRouteChains() routeChains {
	common := func(requests int, window time.Duration) api.Middleware {
		return api.RateLimit(api.RateLimiterConfig{
			Requests: requests, Window: window, TrustedProxies: s.trustedProxies,
			Clock: s.clock, Key: resumeRateLimitKey, Logger: s.logger,
		})
	}
	return routeChains{
		read:          common(resumeReadRequests, time.Minute),
		write:         common(resumeWriteRequests, time.Minute),
		upload:        common(resumeUploadRequests, time.Hour),
		session:       auth.RequireSession(s.sessions),
		csrfJSON:      auth.RequireCSRF(s.publicOrigin),
		csrfMultipart: auth.RequireCSRFMultipart(s.publicOrigin),
	}
}

func (s *Service) sharedRouteChains() routeChains {
	s.chainsOnce.Do(func() { s.chains = s.newRouteChains() })
	return s.chains
}

func (s *Service) ReadAdmission() api.Middleware { return s.sharedRouteChains().read }

func (s *Service) WriteAdmission() api.Middleware { return s.sharedRouteChains().write }

func (s *Service) AllowSlugClaim(accountID uuid.UUID, now time.Time) bool {
	return s.slugAttempts.AllowChangedSlug(accountID, now)
}

func (c routeChains) wrap(route routeSpec, handler http.Handler) http.Handler {
	if !route.Mutation {
		return c.session(c.read(handler))
	}
	if route.Upload {
		return c.session(c.csrfMultipart(c.upload(handler)))
	}
	return c.session(c.csrfJSON(c.write(handler)))
}
