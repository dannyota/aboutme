package project

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/store"
)

func (s *Store) checkSlug(ctx context.Context, q *store.Queries, owner, id uuid.UUID, slug string, now time.Time) error {
	if !ValidSlug(slug) {
		return ErrInvalid
	}
	claim, err := q.GetProjectSlugClaim(ctx, store.GetProjectSlugClaimParams{UserID: owner, Slug: slug})
	if err == nil {
		if claim == id {
			return nil
		}
		return ErrSlugTaken
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("project: read slug claim: %w", err)
	}
	released, err := q.GetProjectSlugHold(ctx, store.GetProjectSlugHoldParams{UserID: owner, Slug: slug})
	if err == nil {
		if now.Before(released.Add(SlugHoldDays * 24 * time.Hour)) {
			return ErrSlugHeld
		}
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("project: read slug hold: %w", err)
	}
	return nil
}

func (s *Store) proposeAvailable(ctx context.Context, q *store.Queries, owner uuid.UUID, name string, now time.Time) (string, error) {
	base := ProposeSlug(name)
	// Every collision consumes a previously claimed slug. The query selects the
	// first unused numeric suffix in one statement, including unexpired holds.
	return q.ProposeProjectSlug(ctx, store.ProposeProjectSlugParams{UserID: owner, Base: base, Cutoff: now.Add(-SlugHoldDays * 24 * time.Hour)})
}
