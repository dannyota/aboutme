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

	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// The operator account deletion answers a reported showcase listing: it
// deletes the account that owns the reported slug through the same deletion
// path as a self-delete (docs/design/showcase.md "Report and account
// deletion", ADR 0003).
var (
	// ErrSlugNotFound means no resume holds the slug now. A tombstoned or
	// unknown slug has no owner.
	ErrSlugNotFound = errors.New("accountapi: no resume holds the slug")
	// ErrSlugMismatch means the slug is held by an account other than the one
	// named. Nothing is deleted.
	ErrSlugMismatch = errors.New("accountapi: slug is not owned by the named account")
)

// OperatorDependencies are the dependencies of the one-shot deletion command.
// The command runs outside the server process, so it has no sessions, public
// state coordinator, or media backend; queued media deletions run in the
// media sweep.
type OperatorDependencies struct {
	Pool      *store.Pool
	Projector *docmigrate.Projector
	Logger    *slog.Logger
	Now       func() time.Time
}

// Account names an account by its ID and the current slugs of its resumes,
// sorted. It carries no email, name, or headline.
type Account struct {
	UserID uuid.UUID
	Slugs  []string
}

// NewOperator constructs a Service for the operator deletion command only.
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

// AccountBySlug reads the account that holds slug. It writes nothing.
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

// DeleteAccountBySlug deletes userID's account after checking, under the slug
// lock inside the deletion transaction, that userID still holds slug. It
// returns ErrSlugNotFound or ErrSlugMismatch and deletes nothing when the
// check fails.
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

// deleteAccountAsOperator runs the deletion without a session and without the
// in-process coordinator: the server's fences cannot be reached from this
// process. Sessions, OAuth grants, showcase rows, and resumes go with the user
// row exactly as in a self-delete.
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
			// The user row goes in the same transaction as everything else, so
			// its absence proves the commit.
			proofCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deleteDrainTimeout)
			_, readErr := store.New(s.pool).GetUserByID(proofCtx, plan.user.ID)
			cancel()
			if errors.Is(readErr, pgx.ErrNoRows) {
				return plan, nil
			}
			return accountDeletionPlan{}, err
		default:
			return accountDeletionPlan{}, errors.New("accountapi: invalid commit outcome")
		}
	}
	return accountDeletionPlan{}, errAccountChanged
}

// requireSlugOwner re-resolves slug and checks its owner is userID.
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
