package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/store"
)

func NewSessionManagerForTest(q *store.Queries, now func() time.Time) *SessionManager {
	return &SessionManager{q: q, now: now}
}

func NewSessionManagerWithPoolForTest(pool *store.Pool, now func() time.Time) *SessionManager {
	return &SessionManager{q: store.New(pool), pool: pool, now: now}
}

func SetSessionLockProbeForTest(m *SessionManager, probe func()) {
	m.lockProbe = probe
}

func SetSessionRotationProbeForTest(m *SessionManager, probe func()) {
	m.rotationProbe = probe
}

const LinkedInIssuerForTest = linkedinIssuer

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

func GitHubProviderEndpointsForTest(svc *Service) (authorizeURL, tokenURL, apiBaseURL string) {
	cfg := svc.githubOAuth2Config("https://localhost:20443/api/v1/auth/github/callback")
	return cfg.Endpoint.AuthURL, cfg.Endpoint.TokenURL, svc.githubAPIBaseURLFor()
}

func ProviderHTTPTimeoutForTest() time.Duration {
	return providerHTTPClient.Timeout
}

func SetProviderHTTPTimeoutForTest(t *testing.T, timeout time.Duration) {
	t.Helper()
	previous := providerHTTPClient
	bounded := *previous
	bounded.Timeout = timeout
	providerHTTPClient = &bounded
	t.Cleanup(func() { providerHTTPClient = previous })
}

func (s *TransactionStore) BeginViewForTest(ctx context.Context, provider Provider, resumeID uuid.UUID, redirectURI string) (string, Transaction, error) {
	return s.beginView(ctx, provider, resumeID, redirectURI, "")
}

func LocalProviderHTTPClientForTest() *http.Client {
	return localProviderHTTPClient()
}

func GitHubProviderHTTPClientForTest(svc *Service) *http.Client {
	ctx := svc.withGitHubProviderHTTPClient(context.Background())
	client, ok := ctx.Value(oauth2.HTTPClient).(*http.Client)
	if !ok {
		return nil
	}
	return client
}

const SessionCookieName = sessionCookieName

const unroutableProviderSentinel = "http://127.0.0.1:1"

func defaultTestOverride(v string) string {
	if v == "" {
		return unroutableProviderSentinel
	}
	return v
}

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

const UnroutableTestSentinel = unroutableProviderSentinel

const MaxProviderResponseBytesForTest = maxProviderResponseBytes

func DefaultTestOverrideForTest(v string) string {
	return defaultTestOverride(v)
}

func GoogleProviderCacheTryLockForTest(svc *Service) (unlock func(), ok bool) {
	if !svc.google.cache.mu.TryLock() {
		return nil, false
	}
	return svc.google.cache.mu.Unlock, true
}

func ResolveReturningProviderTxForTest(ctx context.Context, svc *Service, qtx *store.Queries, subject ProviderSubject) (store.User, bool, error) {
	return svc.resolveReturningProviderTx(ctx, qtx, subject)
}

func CreateProviderAccountTxForTest(ctx context.Context, svc *Service, qtx *store.Queries, account NewProviderAccount) (store.User, error) {
	return svc.createProviderAccountTx(ctx, qtx, account)
}

func IsEmailAlreadyRegisteredForTest(err error) bool {
	return errors.Is(err, errEmailAlreadyRegistered)
}

func SetSessionIssuerForTest(svc *Service, si sessionIssuer) {
	svc.sessions = si
}

func SetSessionManagerForTest(svc *Service, m *SessionManager) {
	svc.sessionMgr = m
	svc.sessions = m
}

func RejectReasonTokensForTest() []string {
	tokens := make([]string, 0, int(numRejectReasons)-1)
	for r := reasonUnspecified + 1; r < numRejectReasons; r++ {
		tokens = append(tokens, r.String())
	}
	return tokens
}

func ZeroRejectReasonTokenForTest() string {
	return reasonUnspecified.String()
}

func SetStartRateLimitForTest(svc *Service, requests int, window time.Duration) {
	svc.startRateLimitRequests = requests
	svc.startRateLimitWindow = window
}

func SetPasswordUserLockProbeForTest(svc *PasswordService, probe func()) {
	svc.userLockProbe = probe
}

func SetPasswordLoginPreTxProbeForTest(svc *PasswordService, probe func()) {
	svc.loginPreTxProbe = probe
}

func SetPasswordVerifyRegistrationLockProbeForTest(svc *PasswordService, probe func()) {
	svc.verifyRegistrationLockProbe = probe
}

func (s *PasswordService) RegisterForTest(ctx context.Context, name, email, password, ip string) error {
	return s.register(ctx, name, email, password, ip)
}

func (s *PasswordService) VerifyForTest(ctx context.Context, token, ip string) error {
	return s.verify(ctx, token, ip)
}

func (s *PasswordService) LoginForTest(ctx context.Context, email, password, ua, ip string) (string, error) {
	return s.login(ctx, email, password, ua, ip)
}

func (s *PasswordService) ResetForTest(ctx context.Context, token, password, ip string) error {
	return s.reset(ctx, token, password, ip)
}

func (s *PasswordService) ReauthForTest(ctx context.Context, sess store.Session, password, ip string) error {
	return s.reauth(ctx, sess, password, ip)
}

func (s *PasswordService) ChangeForTest(ctx context.Context, sess store.Session, password, ip string) (string, error) {
	return s.change(ctx, sess, password, ip)
}
