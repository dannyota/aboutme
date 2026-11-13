package resume

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/store"
)

const ShowcaseListed = "listed"

type Showcase struct {
	State string
	Role  *string
}

func showcaseOf(role *string) *Showcase {
	return &Showcase{State: ShowcaseListed, Role: role}
}

type ShowcaseSync interface {
	SyncTx(ctx context.Context, qtx *store.Queries, resumeID uuid.UUID) error
}

type StoreOption func(*Store)

func WithShowcaseSync(sync ShowcaseSync) StoreOption {
	return func(s *Store) { s.showcase = sync }
}

func (s *Store) syncShowcaseTx(ctx context.Context, qtx *store.Queries, id uuid.UUID) error {
	if s.showcase == nil {
		return nil
	}
	return s.showcase.SyncTx(ctx, qtx, id)
}

func attachShowcase(ctx context.Context, qtx *store.Queries, r *Resume) error {
	row, err := qtx.GetResumeShowcase(ctx, r.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("resume: read showcase: %w", err)
	}
	r.Showcase = showcaseOf(row.Role)
	return nil
}

func attachShowcases(ctx context.Context, qtx *store.Queries, userID uuid.UUID, resumes []Resume) error {
	if len(resumes) == 0 {
		return nil
	}
	rows, err := qtx.ListResumeShowcasesForUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("resume: list showcase: %w", err)
	}
	roles := make(map[uuid.UUID]*string, len(rows))
	for _, row := range rows {
		roles[row.ResumeID] = row.Role
	}
	for i := range resumes {
		if role, found := roles[resumes[i].ID]; found {
			resumes[i].Showcase = showcaseOf(role)
		}
	}
	return nil
}
