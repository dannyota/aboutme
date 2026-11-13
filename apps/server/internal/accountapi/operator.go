package accountapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

var (
	ErrSlugNotFound = errors.New("accountapi: no resume holds the slug")
	ErrSlugMismatch = errors.New("accountapi: slug is not owned by the named account")
)

var ErrOutcomeUnknown = errors.New("accountapi: deletion outcome unknown")

type OperatorDependencies struct {
	Pool      *store.Pool
	Projector *docmigrate.Projector
	Logger    *slog.Logger
	Now       func() time.Time
}

type Account struct {
	UserID uuid.UUID
	Slugs  []string
}

func NewOperator(deps OperatorDependencies) (*Service, error) {
	switch {
	case deps.Pool == nil:
		return nil, errors.New("accountapi: nil pool")
	case deps.Projector == nil:
		return nil, errors.New("accountapi: nil projector")
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	s := &Service{pool: deps.Pool, projector: deps.Projector, logger: deps.Logger, now: deps.Now}
	s.initDeletion()
	return s, nil
}

func (s *Service) AccountBySlug(ctx context.Context, slug string) (Account, error) {
	q := store.New(s.pool)
	owner, err := q.GetResumeOwnerBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Account{}, ErrSlugNotFound
		}
		return Account{}, fmt.Errorf("accountapi: resolve slug: %w", err)
	}
	rows, err := q.ListResumesForUser(ctx, owner.UserID)
	if err != nil {
		return Account{}, fmt.Errorf("accountapi: list account resumes: %w", err)
	}
	account := Account{UserID: owner.UserID, Slugs: []string{}}
	for _, row := range rows {
		if row.Slug != nil {
			account.Slugs = append(account.Slugs, *row.Slug)
		}
	}
	sort.Strings(account.Slugs)
	return account, nil
}

func (s *Service) DeleteAccountBySlug(ctx context.Context, slug string, userID uuid.UUID) (Account, error) {
	plan, err := s.deleteAccountForUser(ctx, deletionRequest{userID: userID, reportedSlug: slug})
	if err != nil {
		return Account{}, err
	}
	account := Account{UserID: plan.user.ID, Slugs: []string{}}
	for _, item := range plan.resumes {
		if item.Slug != nil {
			account.Slugs = append(account.Slugs, *item.Slug)
		}
	}
	sort.Strings(account.Slugs)
	return account, nil
}

func (s *Service) deleteAccountAsOperator(ctx context.Context, req deletionRequest) (accountDeletionPlan, error) {
	for attempt := 0; attempt < deletePlanAttempts; attempt++ {
		if err := requireSlugOwner(ctx, store.New(s.pool), req.reportedSlug, req.userID); err != nil {
			return accountDeletionPlan{}, err
		}
		plan, err := s.prepareDeletion(ctx, req)
		if err != nil {
			if errors.Is(err, errAccountChanged) {
				continue
			}
			return accountDeletionPlan{}, err
		}
		if s.afterPrepare != nil {
			s.afterPrepare()
		}
		outcome, _, err := s.executeDeletion(ctx, plan)
		switch outcome {
		case deleteCommitted:
			return plan, nil
		case deleteDefinitelyRolledBack:
			if errors.Is(err, errAccountChanged) {
				continue
			}
			return accountDeletionPlan{}, err
		case deleteCommitUnknown:
			proofCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deleteDrainTimeout)
			_, readErr := store.New(s.pool).GetUserByID(proofCtx, plan.user.ID)
			cancel()
			if errors.Is(readErr, pgx.ErrNoRows) {
				return plan, nil
			}
			return accountDeletionPlan{}, ErrOutcomeUnknown
		default:
			return accountDeletionPlan{}, errors.New("accountapi: invalid commit outcome")
		}
	}
	return accountDeletionPlan{}, errAccountChanged
}

func requireSlugOwner(ctx context.Context, q *store.Queries, slug string, userID uuid.UUID) error {
	owner, err := q.GetResumeOwnerBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSlugNotFound
		}
		return fmt.Errorf("accountapi: resolve slug: %w", err)
	}
	if owner.UserID != userID {
		return ErrSlugMismatch
	}
	return nil
}
