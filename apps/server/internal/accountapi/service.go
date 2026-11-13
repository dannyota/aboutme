package accountapi

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/media"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

const ExportPath = "/api/v1/me/export"

type Dependencies struct {
	Pool           *store.Pool
	Sessions       *auth.SessionManager
	Coordinator    *publicstate.Coordinator
	Media          media.Backend
	Projector      *docmigrate.Projector
	Logger         *slog.Logger
	TrustedProxies api.TrustedProxies
	PublicOrigin   string
	Now            func() time.Time
}

type Service struct {
	pool            *store.Pool
	sessions        *auth.SessionManager
	coordinator     *publicstate.Coordinator
	media           media.Backend
	projector       *docmigrate.Projector
	logger          *slog.Logger
	trustedProxies  api.TrustedProxies
	publicOrigin    string
	now             func() time.Time
	deleteAdmission api.Middleware
	beginTx         func(context.Context) (pgx.Tx, error)
	commitTx        func(context.Context, pgx.Tx) error
	newAuditID      func() (uuid.UUID, error)
	enqueueMediaJob func(context.Context, *store.Queries, store.EnqueueMediaDeletionJobParams) (int64, error)
	afterClose      func()
	afterPrepare    func()
	afterUserLock   func()
}

func New(deps Dependencies) (*Service, error) {
	switch {
	case deps.Pool == nil:
		return nil, errors.New("accountapi: nil pool")
	case deps.Sessions == nil:
		return nil, errors.New("accountapi: nil sessions")
	case deps.Coordinator == nil:
		return nil, errors.New("accountapi: nil coordinator")
	case deps.Media == nil:
		return nil, errors.New("accountapi: nil media")
	case deps.Projector == nil:
		return nil, errors.New("accountapi: nil projector")
	case deps.PublicOrigin == "":
		return nil, errors.New("accountapi: empty public origin")
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	s := &Service{
		pool: deps.Pool, sessions: deps.Sessions, coordinator: deps.Coordinator,
		media: deps.Media, projector: deps.Projector, logger: deps.Logger,
		trustedProxies: deps.TrustedProxies, publicOrigin: deps.PublicOrigin,
		now: deps.Now,
	}
	s.deleteAdmission = api.RateLimit(api.RateLimiterConfig{
		Requests: 5, Window: time.Minute, TrustedProxies: deps.TrustedProxies,
		Clock: deps.Now, Key: api.CompositeKeyFunc(api.AccountKeyFunc, api.IPKeyFunc),
		Logger: deps.Logger,
	})
	s.initDeletion()
	return s, nil
}

func (s *Service) initDeletion() {
	s.beginTx = s.pool.Begin
	s.commitTx = func(ctx context.Context, tx pgx.Tx) error { return tx.Commit(ctx) }
	s.newAuditID = uuid.NewV7
	s.enqueueMediaJob = func(ctx context.Context, q *store.Queries, params store.EnqueueMediaDeletionJobParams) (int64, error) {
		return q.EnqueueMediaDeletionJob(ctx, params)
	}
}
