package showcase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

type Database interface {
	store.DBTX
	Begin(context.Context) (pgx.Tx, error)
}

type Dependencies struct {
	DB        Database
	Reader    *publicresume.Reader
	Projector *docmigrate.Projector
	Now       func() time.Time
	Logger    *slog.Logger
}

type Service struct {
	db        Database
	reader    *publicresume.Reader
	projector *docmigrate.Projector
	now       func() time.Time
	logger    *slog.Logger
}

func New(dependencies Dependencies) (*Service, error) {
	if dependencies.DB == nil || dependencies.Reader == nil || dependencies.Projector == nil {
		return nil, errors.New("showcase: invalid service dependencies")
	}
	now := dependencies.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		db: dependencies.DB, reader: dependencies.Reader, projector: dependencies.Projector,
		now: now, logger: dependencies.Logger,
	}, nil
}

func (s *Service) SyncTx(ctx context.Context, qtx *store.Queries, resumeID uuid.UUID) error {
	if _, err := qtx.GetResumeShowcaseForUpdate(ctx, resumeID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("showcase: lock row: %w", err)
	}
	return s.syncLockedTx(ctx, qtx, resumeID)
}

func (s *Service) syncLockedTx(ctx context.Context, qtx *store.Queries, resumeID uuid.UUID) error {
	derived, listable, err := s.deriveTx(ctx, qtx, resumeID)
	if err != nil {
		return err
	}
	if !listable {
		if _, deleteErr := qtx.DeleteResumeShowcase(ctx, resumeID); deleteErr != nil {
			return fmt.Errorf("showcase: delete row: %w", deleteErr)
		}
		return nil
	}
	if updateErr := qtx.UpdateResumeShowcaseDerived(ctx, store.UpdateResumeShowcaseDerivedParams{
		ResumeID: resumeID, CardVersion: derived.CardVersion, TemplateID: derived.TemplateID,
	}); updateErr != nil {
		return fmt.Errorf("showcase: update derived values: %w", updateErr)
	}
	return nil
}

func (s *Service) deriveTx(ctx context.Context, qtx *store.Queries, resumeID uuid.UUID) (derived Derived, listable bool, err error) {
	row, err := qtx.GetResumeByID(ctx, resumeID)
	if err != nil {
		return Derived{}, false, fmt.Errorf("showcase: read resume: %w", err)
	}
	if !row.Live || row.SignInToView {
		return Derived{}, false, nil
	}
	snapshot, err := s.reader.ProjectRow(row)
	if err != nil {
		return Derived{}, false, projectionError{err: fmt.Errorf("showcase: project resume: %w", err)}
	}
	derived, err = Derive(snapshot)
	if err != nil {
		return Derived{}, false, projectionError{err: fmt.Errorf("showcase: derive values: %w", err)}
	}
	return derived, true, nil
}

type projectionError struct{ err error }

func (e projectionError) Error() string { return e.err.Error() }
func (e projectionError) Unwrap() error { return e.err }

type PublishChange struct {
	ResumeID uuid.UUID
	Enabled  bool
	Role     *string
}

func (s *Service) PublishTx(ctx context.Context, qtx *store.Queries, change PublishChange) error {
	_, err := qtx.GetResumeShowcaseForUpdate(ctx, change.ResumeID)
	exists := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("showcase: lock row: %w", err)
	}
	if !change.Enabled {
		if exists {
			if _, deleteErr := qtx.DeleteResumeShowcase(ctx, change.ResumeID); deleteErr != nil {
				return fmt.Errorf("showcase: delete row: %w", deleteErr)
			}
		}
		return nil
	}
	if exists {
		if roleErr := qtx.UpdateResumeShowcaseRole(ctx, store.UpdateResumeShowcaseRoleParams{
			ResumeID: change.ResumeID, Role: change.Role,
		}); roleErr != nil {
			return fmt.Errorf("showcase: update role: %w", roleErr)
		}
		return s.syncLockedTx(ctx, qtx, change.ResumeID)
	}
	derived, listable, err := s.deriveTx(ctx, qtx, change.ResumeID)
	if err != nil {
		return err
	}
	if !listable {
		return nil
	}
	if insertErr := qtx.InsertResumeShowcase(ctx, store.InsertResumeShowcaseParams{
		ResumeID: change.ResumeID, RequestedAt: s.now(), Role: change.Role,
		CardVersion: derived.CardVersion, TemplateID: derived.TemplateID,
	}); insertErr != nil {
		return fmt.Errorf("showcase: insert row: %w", insertErr)
	}
	return nil
}

func (s *Service) RecomputeAll(ctx context.Context) (recomputed, skipped int, err error) {
	ids, err := store.New(s.db).ListResumeShowcaseIDs(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("showcase: list rows: %w", err)
	}
	for _, id := range ids {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return recomputed, skipped, ctxErr
		}
		syncErr := s.recomputeOne(ctx, id)
		var unprojectable projectionError
		switch {
		case syncErr == nil:
			recomputed++
		case errors.As(syncErr, &unprojectable):
			skipped++
			if s.logger != nil {
				s.logger.ErrorContext(ctx, "showcase recompute skipped a row", "resume_id", id.String())
			}
		default:
			return recomputed, skipped, syncErr
		}
	}
	return recomputed, skipped, nil
}

func (s *Service) recomputeOne(ctx context.Context, id uuid.UUID) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("showcase: begin recompute: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) && s.logger != nil {
			s.logger.ErrorContext(ctx, "showcase recompute rollback failed")
		}
	}()
	if syncErr := s.SyncTx(ctx, store.New(tx), id); syncErr != nil {
		return syncErr
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		return fmt.Errorf("showcase: commit recompute: %w", commitErr)
	}
	return nil
}
