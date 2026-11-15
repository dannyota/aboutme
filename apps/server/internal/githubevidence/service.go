package githubevidence

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/store"
)

const (
	StatusPath     = "/api/v1/github-evidence"
	StartPath      = "/api/v1/github-evidence/start"
	CallbackPath   = "/api/v1/github-evidence/callback"
	SessionEndPath = "/api/v1/github-evidence/session/end"
)

const (
	sessionTTL       = 30 * time.Minute
	requestTimeout   = 5 * time.Second
	maxResponseBytes = 512 << 10
	startLimit       = 10
	requestLimit     = 120
	limitWindow      = time.Hour
	shutdownWait     = requestTimeout + time.Second
	settingsPath     = "/app/settings/sessions"
)

type Options struct {
	Config       config.GitHubEvidenceConfig
	PublicOrigin string
	Pool         *store.Pool
	Sessions     *auth.SessionManager
	Logger       *slog.Logger
	Clock        func() time.Time
}

type Service struct {
	enabled      bool
	publicOrigin string
	callbackURL  string
	pool         *store.Pool
	q            *store.Queries
	transactions *auth.TransactionStore
	sessions     *auth.SessionManager
	client       *client
	registry     *registry
	starts       *windowLimiter
	logger       *slog.Logger
	now          func() time.Time
}

func New(opts Options) (*Service, error) {
	if !opts.Config.Enabled() {
		return &Service{}, nil
	}
	return newService(opts, "", newTransport())
}

func newService(opts Options, endpoint string, transport http.RoundTripper) (*Service, error) {
	switch {
	case !opts.Config.Enabled():
		return nil, errors.New("githubevidence: incomplete configuration")
	case opts.PublicOrigin == "":
		return nil, errors.New("githubevidence: empty public origin")
	case opts.Pool == nil:
		return nil, errors.New("githubevidence: nil pool")
	case opts.Sessions == nil:
		return nil, errors.New("githubevidence: nil session manager")
	case transport == nil:
		return nil, errors.New("githubevidence: nil transport")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	q := store.New(opts.Pool)
	s := &Service{
		enabled:      true,
		publicOrigin: opts.PublicOrigin,
		callbackURL:  opts.PublicOrigin + CallbackPath,
		pool:         opts.Pool,
		q:            q,
		transactions: auth.NewTransactionStore(q),
		sessions:     opts.Sessions,
		starts:       newWindowLimiter(startLimit, limitWindow),
		logger:       opts.Logger,
		now:          opts.Clock,
	}
	s.client = newClient(opts.Config.ClientID, opts.Config.ClientSecret, endpoint, transport,
		newWindowLimiter(requestLimit, limitWindow), opts.Clock)
	s.registry = newRegistry(sessionTTL, opts.Clock, s.revokeToken)
	return s, nil
}

func (s *Service) Enabled() bool {
	return s.enabled
}

func (s *Service) AccountSessionsEnded(userID uuid.UUID) {
	if s.registry != nil {
		s.registry.end(userID)
	}
}

func (s *Service) Run(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		if s.registry != nil {
			s.registry.close()
		}
	}()
	return done
}

func (s *Service) Shutdown(ctx context.Context) error {
	if s.registry == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, shutdownWait)
	defer cancel()
	s.registry.close()
	return s.registry.wait(ctx)
}

func (s *Service) revokeToken(token string) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	if err := s.client.revoke(ctx, token); err != nil {
		s.logger.Warn("github evidence: token revocation failed", "category", revocationCategory(err))
	}
}

func revocationCategory(err error) string {
	switch {
	case errors.Is(err, errTimeout):
		return "timeout"
	case errors.Is(err, errUpstreamRejected), errors.Is(err, errResponseTooLarge):
		return "rejected"
	case errors.Is(err, errDestinationRefused), errors.Is(err, errRedirectRefused):
		return "refused"
	default:
		return "network"
	}
}
