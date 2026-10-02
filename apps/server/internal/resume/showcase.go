package resume

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// ShowcaseListed is the only owner-visible showcase state: an opt-in exists
// only while its resume is live with sign in to view off, so it is always
// shown (docs/design/showcase.md "Opt-in").
const ShowcaseListed = "listed"

// Showcase is the owner's community-showcase state for one resume. A resume
// that is not opted in has none (nil).
type Showcase struct {
	State string
	Role  *string
}

func showcaseOf(row store.ResumeShowcase) *Showcase {
	return &Showcase{State: ShowcaseListed, Role: row.Role}
}

// ShowcaseSync keeps the showcase row's derived values current inside a
// resume write transaction. SyncTx touches nothing for a resume without a row.
type ShowcaseSync interface {
	SyncTx(ctx context.Context, qtx *store.Queries, resumeID uuid.UUID) error
}

// StoreOption configures a Store.
type StoreOption func(*Store)

// WithShowcaseSync makes every committed document write recompute the showcase
// derived values in the same transaction. Without it a Store leaves showcase
// rows alone, which only tests that never opt in should rely on.
func WithShowcaseSync(sync ShowcaseSync) StoreOption {
	return func(s *Store) { s.showcase = sync }
}

// syncShowcaseTx runs the showcase hook after a document write.
func (s *Store) syncShowcaseTx(ctx context.Context, qtx *store.Queries, id uuid.UUID) error {
	if s.showcase == nil {
		return nil
	}
	return s.showcase.SyncTx(ctx, qtx, id)
}

// attachShowcase loads the opt-in of one resume into r.
func attachShowcase(ctx context.Context, qtx *store.Queries, r *Resume) error {
	row, err := qtx.GetResumeShowcase(ctx, r.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("resume: read showcase: %w", err)
	}
	r.Showcase = showcaseOf(row)
	return nil
}

// attachShowcases loads the opt-ins of every listed resume of one owner.
func attachShowcases(ctx context.Context, qtx *store.Queries, userID uuid.UUID, resumes []Resume) error {
	if len(resumes) == 0 {
		return nil
	}
	rows, err := qtx.ListResumeShowcasesForUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("resume: list showcase: %w", err)
	}
	byResume := make(map[uuid.UUID]store.ResumeShowcase, len(rows))
	for _, row := range rows {
		byResume[row.ResumeID] = row
	}
	for i := range resumes {
		if row, found := byResume[resumes[i].ID]; found {
			resumes[i].Showcase = showcaseOf(row)
		}
	}
	return nil
}
