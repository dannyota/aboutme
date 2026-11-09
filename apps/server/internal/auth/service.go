package auth

// OAuth callbacks share the no-oracle and identity rules in
// docs/design/security.md.

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// GoogleStartPath and GoogleCallbackPath are the registered Google routes.
const (
	// GoogleStartPath starts a Google authorization: GET for login or sign in
	// to view, POST for a signed-in account's link or reauthentication.
	GoogleStartPath = "/api/v1/auth/google/start"
	// GoogleCallbackPath receives Google's authorization response (GET).
	GoogleCallbackPath = "/api/v1/auth/google/callback"
)

// LinkedInStartPath and LinkedInCallbackPath are the registered LinkedIn routes.
const (
	// LinkedInStartPath starts a LinkedIn authorization: GET for login or sign
	// in to view, POST for a signed-in account's link or reauthentication.
	LinkedInStartPath = "/api/v1/auth/linkedin/start"
	// LinkedInCallbackPath receives LinkedIn's authorization response (GET).
	LinkedInCallbackPath = "/api/v1/auth/linkedin/callback"
)

// MePath, LogoutPath, and SessionsPath are the session API routes.
const (
	// MePath reads (GET) or deletes (DELETE) the signed-in account.
	MePath = "/api/v1/me"
	// LogoutPath ends the current session (POST).
	LogoutPath = "/api/v1/auth/logout"
	// SessionsPath lists the account's live sessions (GET) or revokes every
	// session (DELETE); SessionsPath+"/{id}" revokes one session (DELETE).
	SessionsPath = "/api/v1/sessions"
)

// Callback error codes are closed. Security-sensitive failures collapse to
// auth_failed; user-actionable outcomes remain distinct. See
// docs/design/security.md.

// authFailedErrorCode collapses security-sensitive callback failures.
const authFailedErrorCode = "auth_failed"

// emailNotVerifiedErrorCode identifies a failed registration email check.
const emailNotVerifiedErrorCode = "email_not_verified"

// cancelledErrorCode identifies provider-reported consent denial.
const cancelledErrorCode = "cancelled" //nolint:misspell // Exact wire value uses double-L "cancelled".

// emailAlreadyRegisteredErrorCode identifies a verified-email collision.
const emailAlreadyRegisteredErrorCode = "email_already_registered"

// identityAlreadyLinkedErrorCode identifies an identity owned by another user.
const identityAlreadyLinkedErrorCode = "identity_already_linked"

// pgUniqueViolationCode is PostgreSQL's unique-violation SQLSTATE.
const pgUniqueViolationCode = "23505"

// isUniqueViolation matches only PostgreSQL unique violations.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolationCode
}

// settingsSessionsPath is the success and error target for privileged callbacks.
const settingsSessionsPath = "/app/settings/sessions"

// secondFactorPagePath is where an enrolled account completes a pending
// authentication. See docs/design/passkey-second-factor-contract.md.
const secondFactorPagePath = "/login/second-factor"

// sessionIssuer is the callback's injectable session-issuance seam. It exposes
// the transaction-scoped primitive only: the caller already holds the user row
// lock in the same transaction.
type sessionIssuer interface {
	IssueTx(ctx context.Context, qtx *store.Queries, user store.User, ua, ip string) (SessionIssue, error)
}

// Service implements the OAuth login HTTP surface for Google, GitHub, and
// LinkedIn. It holds shared transaction and session machinery plus each
// provider's credentials.
type Service struct {
	tx   *TransactionStore
	q    *store.Queries
	pool *store.Pool
	// sessions is the injectable issuance seam; sessionMgr supplies the full
	// session surface so tests can inject failures without replacing
	// authentication.
	sessions   sessionIssuer
	sessionMgr *SessionManager
	// pending creates the pending authentication an enrolled account gets
	// instead of a session after provider login or reauthentication.
	pending *PendingAuthenticationManager
	logger  *slog.Logger

	publicOrigin   string
	providerLogin  config.ProviderLogin
	trustedProxies api.TrustedProxies
	// viewPassKey seals the __Host-view-pass cookie (docs/design/viewer-analytics/
	// sign-in-to-view.md "Pass cookie"). It is 32 bytes, decoded once at
	// construction from config.Config.ViewPassKey.
	viewPassKey             []byte
	googleIssuerURL         string
	linkedinIssuerURL       string
	githubOAuthAuthorizeURL string
	githubOAuthTokenURL     string
	githubAPIBaseURL        string
	googleLocalOIDC         bool
	linkedinLocalOIDC       bool
	githubLocalOAuth        bool

	// These fields let tests reduce the shared start-route budget.
	startRateLimitRequests int
	startRateLimitWindow   time.Duration

	google   googleProviderConfig
	github   githubProviderConfig
	linkedin linkedinProviderConfig

	accountDeleteHandler http.Handler

	// Provider overrides are empty in production and route test traffic to
	// in-process providers through the production verification paths.
	googleIssuerOverride string

	// GitHub uses one override for both OAuth and API endpoints.
	githubEndpointOverride string

	linkedinIssuerOverride string
}

// NewService builds a Service without network I/O. Provider discovery is lazy
// and cached. A nil logger disables auth logging. pool may be nil for callers
// that only exercise route registration or provider discovery, never the login
// callback; every callback requires it to open the user-lock transaction that
// docs/design/second-factor-authentication.md orders.
func NewService(logger *slog.Logger, cfg config.Config, pool *store.Pool) (*Service, error) {
	if cfg.PublicOrigin == "" {
		return nil, fmt.Errorf("auth: NewService: config.PublicOrigin is required")
	}
	// config.Config.ViewPassKey is validated at config load (docs/design/
	// viewer-analytics/sign-in-to-view.md "Pass cookie"); a test config that
	// leaves it blank carries no key. A non-blank key that does not decode
	// to 32 bytes is an error: an empty or wrong-length HMAC key would let
	// anyone forge a pass.
	var viewPassKey []byte
	if cfg.ViewPassKey != "" {
		decoded, decodeErr := base64.RawURLEncoding.Strict().DecodeString(cfg.ViewPassKey)
		if decodeErr != nil || len(decoded) != 32 {
			return nil, errors.New("auth: NewService: config.ViewPassKey must be base64url encoding exactly 32 bytes")
		}
		viewPassKey = decoded
	}
	q := store.New(pool)
	sessionMgr := NewSessionManagerWithPool(pool)
	return &Service{
		tx:                      NewTransactionStore(q),
		q:                       q,
		pool:                    pool,
		sessions:                sessionMgr,
		sessionMgr:              sessionMgr,
		pending:                 NewPendingAuthenticationManager(pool, logger),
		logger:                  logger,
		publicOrigin:            cfg.PublicOrigin,
		providerLogin:           cfg.ProviderLogin,
		trustedProxies:          api.TrustedProxies(cfg.TrustedProxyCIDRs),
		viewPassKey:             viewPassKey,
		googleIssuerURL:         endpointOrDefault(cfg.GoogleOIDCIssuerURL, googleIssuer),
		linkedinIssuerURL:       endpointOrDefault(cfg.LinkedInOIDCIssuerURL, linkedinIssuer),
		githubOAuthAuthorizeURL: endpointOrDefault(cfg.GitHubOAuthAuthorizeURL, githubAuthorizeURL),
		githubOAuthTokenURL:     endpointOrDefault(cfg.GitHubOAuthTokenURL, githubTokenURL),
		githubAPIBaseURL:        endpointOrDefault(cfg.GitHubAPIBaseURL, githubAPIBaseURL),
		googleLocalOIDC:         cfg.GoogleOIDCIssuerURL != "",
		linkedinLocalOIDC:       cfg.LinkedInOIDCIssuerURL != "",
		githubLocalOAuth:        cfg.GitHubOAuthAuthorizeURL != "",

		startRateLimitRequests: startRateLimitRequests,
		startRateLimitWindow:   startRateLimitWindow,
		google: googleProviderConfig{
			clientID:     cfg.GoogleClientID,
			clientSecret: cfg.GoogleClientSecret,
		},
		github: githubProviderConfig{
			clientID:     cfg.GitHubClientID,
			clientSecret: cfg.GitHubClientSecret,
		},
		linkedin: linkedinProviderConfig{
			clientID:     cfg.LinkedInClientID,
			clientSecret: cfg.LinkedInClientSecret,
		},
	}, nil
}

func endpointOrDefault(configured, fallback string) string {
	if configured != "" {
		return configured
	}
	return fallback
}
