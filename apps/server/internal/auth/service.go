package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/store"
)

const (
	GoogleStartPath    = "/api/v1/auth/google/start"
	GoogleCallbackPath = "/api/v1/auth/google/callback"
)

const (
	LinkedInStartPath    = "/api/v1/auth/linkedin/start"
	LinkedInCallbackPath = "/api/v1/auth/linkedin/callback"
)

const (
	MePath       = "/api/v1/me"
	LogoutPath   = "/api/v1/auth/logout"
	SessionsPath = "/api/v1/sessions"
)

const authFailedErrorCode = "auth_failed"

const emailNotVerifiedErrorCode = "email_not_verified"

const cancelledErrorCode = "cancelled" //nolint:misspell // Exact wire value uses double-L "cancelled".

const emailAlreadyRegisteredErrorCode = "email_already_registered"

const identityAlreadyLinkedErrorCode = "identity_already_linked"

const pgUniqueViolationCode = "23505"

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolationCode
}

const settingsSessionsPath = "/app/settings/sessions"

const secondFactorPagePath = "/login/second-factor"

type sessionIssuer interface {
	IssueTx(ctx context.Context, qtx *store.Queries, user store.User, ua, ip string) (SessionIssue, error)
}

type Service struct {
	tx         *TransactionStore
	q          *store.Queries
	pool       *store.Pool
	sessions   sessionIssuer
	sessionMgr *SessionManager
	pending    *PendingAuthenticationManager
	logger     *slog.Logger

	publicOrigin            string
	providerLogin           config.ProviderLogin
	trustedProxies          api.TrustedProxies
	viewPassKey             []byte
	googleIssuerURL         string
	linkedinIssuerURL       string
	githubOAuthAuthorizeURL string
	githubOAuthTokenURL     string
	githubAPIBaseURL        string
	googleLocalOIDC         bool
	linkedinLocalOIDC       bool
	githubLocalOAuth        bool

	startRateLimitRequests int
	startRateLimitWindow   time.Duration

	google   googleProviderConfig
	github   githubProviderConfig
	linkedin linkedinProviderConfig

	accountDeleteHandler http.Handler

	googleIssuerOverride string

	githubEndpointOverride string

	linkedinIssuerOverride string
}

func NewService(logger *slog.Logger, cfg config.Config, pool *store.Pool) (*Service, error) {
	if cfg.PublicOrigin == "" {
		return nil, fmt.Errorf("auth: NewService: config.PublicOrigin is required")
	}
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
