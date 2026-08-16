package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"golang.org/x/oauth2"

	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// NewSessionManagerForTest builds a manager with an injected clock.
func NewSessionManagerForTest(q *store.Queries, now func() time.Time) *SessionManager {
	return &SessionManager{q: q, now: now}
}

// NewSessionManagerWithPoolForTest builds a pool-backed manager with an
// injected clock, for the deterministic user-lock fence tests.
func NewSessionManagerWithPoolForTest(pool *store.Pool, now func() time.Time) *SessionManager {
	return &SessionManager{q: store.New(pool), pool: pool, now: now}
}

// SetSessionLockProbeForTest installs a probe that runs after Issue acquires
// the user row lock. It is nil in production and exists only for deterministic
// fence tests.
func SetSessionLockProbeForTest(m *SessionManager, probe func()) {
	m.lockProbe = probe
}

// SetSessionRotationProbeForTest installs a probe that runs after rotation's
// admission update commits and before the successor transaction. It is nil in
// production and exists only for deterministic fence tests.
func SetSessionRotationProbeForTest(m *SessionManager, probe func()) {
	m.rotationProbe = probe
}

// OIDCProviderEndpointForTest drives runtime provider discovery without
// starting a transaction.
func OIDCProviderEndpointForTest(ctx context.Context, svc *Service, provider Provider) (oauth2.Endpoint, error) {
	var (
		p   interface{ Endpoint() oauth2.Endpoint }
		err error
	)
	switch provider {
	case ProviderGoogle:
		p, err = svc.googleProvider(ctx)
	case ProviderLinkedIn:
		p, err = svc.linkedinProvider(ctx)
	default:
		return oauth2.Endpoint{}, fmt.Errorf("unsupported OIDC provider %q", provider)
	}
	if err != nil {
		return oauth2.Endpoint{}, err
	}
	return p.Endpoint(), nil
}

// GitHubProviderEndpointsForTest exposes the effective OAuth and API URLs.
func GitHubProviderEndpointsForTest(svc *Service) (authorizeURL, tokenURL, apiBaseURL string) {
	cfg := svc.githubOAuth2Config("https://localhost:20443/api/v1/auth/github/callback")
	return cfg.Endpoint.AuthURL, cfg.Endpoint.TokenURL, svc.githubAPIBaseURLFor()
}

// LocalProviderHTTPClientForTest exposes the loopback-only transport boundary.
func LocalProviderHTTPClientForTest() *http.Client {
	return localProviderHTTPClient()
}

// GitHubProviderHTTPClientForTest exposes the effective runtime HTTP boundary.
func GitHubProviderHTTPClientForTest(svc *Service) *http.Client {
	ctx := svc.withGitHubProviderHTTPClient(context.Background())
	client, ok := ctx.Value(oauth2.HTTPClient).(*http.Client)
	if !ok {
		return nil
	}
	return client
}

// SessionCookieName exposes the cookie name to black-box tests.
const SessionCookieName = sessionCookieName

// unroutableProviderSentinel makes omitted test endpoints fail locally.
const unroutableProviderSentinel = "http://127.0.0.1:1"

// defaultTestOverride replaces an empty endpoint with the local sentinel.
func defaultTestOverride(v string) string {
	if v == "" {
		return unroutableProviderSentinel
	}
	return v
}

// NewServiceForTest builds a Service whose provider endpoints are local test
// doubles or the unroutable sentinel. The seam exists only in test builds.
// pool may be nil for tests that never drive a login callback.
func NewServiceForTest(logger *slog.Logger, cfg config.Config, pool *store.Pool, googleIssuer, githubEndpoint, linkedinIssuer string) (*Service, error) {
	svc, err := NewService(logger, cfg, pool)
	if err != nil {
		return nil, err
	}
	svc.googleIssuerOverride = defaultTestOverride(googleIssuer)
	svc.githubEndpointOverride = defaultTestOverride(githubEndpoint)
	svc.linkedinIssuerOverride = defaultTestOverride(linkedinIssuer)
	return svc, nil
}

// UnroutableTestSentinel exposes unroutableProviderSentinel to package
// auth_test's pure-function tests (see DefaultTestOverrideForTest).
const UnroutableTestSentinel = unroutableProviderSentinel

// MaxProviderResponseBytesForTest lets fixtures derive sizes from the real cap.
const MaxProviderResponseBytesForTest = maxProviderResponseBytes

// DefaultTestOverrideForTest exposes endpoint substitution to black-box tests.
func DefaultTestOverrideForTest(v string) string {
	return defaultTestOverride(v)
}

// GoogleProviderCacheTryLockForTest probes whether discovery holds the cache
// mutex during network I/O. A false result has a nil unlock function.
func GoogleProviderCacheTryLockForTest(svc *Service) (unlock func(), ok bool) {
	if !svc.google.cache.mu.TryLock() {
		return nil, false
	}
	return svc.google.cache.mu.Unlock, true
}

// ResolveReturningProviderTxForTest exposes the transactional returning-subject
// resolver to black-box tests. The caller supplies an already-open transaction.
func ResolveReturningProviderTxForTest(ctx context.Context, svc *Service, qtx *store.Queries, subject ProviderSubject) (store.User, bool, error) {
	return svc.resolveReturningProviderTx(ctx, qtx, subject)
}

// CreateProviderAccountTxForTest exposes the transactional new-account creator
// to black-box tests. The caller supplies an already-open transaction.
func CreateProviderAccountTxForTest(ctx context.Context, svc *Service, qtx *store.Queries, account NewProviderAccount) (store.User, error) {
	return svc.createProviderAccountTx(ctx, qtx, account)
}

// IsEmailAlreadyRegisteredForTest reports whether err is the closed
// email-already-registered outcome.
func IsEmailAlreadyRegisteredForTest(err error) bool {
	return errors.Is(err, errEmailAlreadyRegistered)
}

// SetSessionIssuerForTest injects deterministic issuance failures.
func SetSessionIssuerForTest(svc *Service, si sessionIssuer) {
	svc.sessions = si
}

// SetSessionManagerForTest replaces the manager used by middleware and session
// handlers. This lets HTTP tests drive rotation and recent-reauth boundaries
// with an injected clock.
//
// It must set both svc.sessionMgr and svc.sessions. Authentication and issuance
// share one manager in production, so tests must preserve that invariant.
func SetSessionManagerForTest(svc *Service, m *SessionManager) {
	svc.sessionMgr = m
	svc.sessions = m
}

// RejectReasonTokensForTest returns every reason token in declaration order.
func RejectReasonTokensForTest() []string {
	tokens := make([]string, 0, int(numRejectReasons)-1)
	for r := reasonUnspecified + 1; r < numRejectReasons; r++ {
		tokens = append(tokens, r.String())
	}
	return tokens
}

// ZeroRejectReasonTokenForTest returns the zero-value placeholder.
func ZeroRejectReasonTokenForTest() string {
	return reasonUnspecified.String()
}

// SetStartRateLimitForTest sets a small budget before RegisterRoutes constructs
// the limiter.
func SetStartRateLimitForTest(svc *Service, requests int, window time.Duration) {
	svc.startRateLimitRequests = requests
	svc.startRateLimitWindow = window
}
