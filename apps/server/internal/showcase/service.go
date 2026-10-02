package showcase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// Database is the pool boundary the service needs: plain queries and a
// transaction.
type Database interface {
	store.DBTX
	Begin(context.Context) (pgx.Tx, error)
}

// Dependencies are the service's collaborators. Reader projects a stored row
// the way the preview card builder does; Projector lifts older document
// versions for the listing.
type Dependencies struct {
	DB        Database
	Reader    *publicresume.Reader
	Projector *docmigrate.Projector
	// Now returns the current time; nil means time.Now.
	Now func() time.Time
	// Logger receives content-free diagnostics (counts and resume IDs only).
	// Nil disables them.
	Logger *slog.Logger
}

// Service keeps showcase rows current and serves the listing.
type Service struct {
	db        Database
	reader    *publicresume.Reader
	projector *docmigrate.Projector
	now       func() time.Time
	logger    *slog.Logger
}

// New creates the showcase service.
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

// SyncTx recomputes the card version and template ID of an opted-in resume
// inside the caller's write transaction. A resume without a showcase row is
// left untouched. It locks the row, so concurrent writes serialize. A row whose
// resume is no longer live, or requires sign in to view, is deleted: neither
// state may be listed.
func (s *Service) SyncTx(ctx context.Context, qtx *store.Queries, resumeID uuid.UUID) error {
	if _, err := qtx.GetResumeShowcaseForUpdate(ctx, resumeID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("showcase: lock row: %w", err)
	}
	return s.syncLockedTx(ctx, qtx, resumeID)
}

// syncLockedTx recomputes the derived values of a row the caller has locked.
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

// deriveTx reads the committed-or-in-transaction resume row and derives its
// showcase values. listable is false when the resume is not live or requires
// sign in to view.
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

// projectionError marks a resume document that cannot be projected or derived,
// as opposed to a failure of the database.
type projectionError struct{ err error }

func (e projectionError) Error() string { return e.err.Error() }
func (e projectionError) Unwrap() error { return e.err }

// PublishChange is the effective showcase state a publish request leaves.
type PublishChange struct {
	ResumeID uuid.UUID
	// Enabled is the switch after the request, with the live and sign in to
	// view rules already applied.
	Enabled bool
	// Role is the owner's role while Enabled, nil for none.
	Role *string
}

// PublishTx applies a publish request's showcase state inside the publish
// transaction, after the resume row holds its new publish settings. Turning
// the switch off, or leaving a resume that is not live or requires sign in to
// view, deletes the row; turning it on inserts a row with a new opt-in time, so
// a later opt-in lists the resume first. A row that stays on keeps its opt-in
// time and takes the new role and derived values.
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

// RecomputeAll recomputes the derived values of every showcase row, each in its
// own transaction, so a changed scrub rule, preset, or card layout applies after
// a deploy (docs/design/showcase.md "Derived values and reports"). A row whose resume cannot be
// projected keeps its stored values, which cannot list it: that resume is not
// served either. It returns how many rows it recomputed and how many it
// skipped.
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
		// The transaction either committed or is being abandoned; a rollback
		// failure after commit is the expected closed-transaction error.
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
