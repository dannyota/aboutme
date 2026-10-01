package showcase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// Reviewer runs the operator's review commands. It reads and writes only the
// showcase row and the slug of its resume, never a name or headline
// (docs/design/showcase.md "Review").
type Reviewer struct {
	queries *store.Queries
	now     func() time.Time
}

// NewReviewer creates a reviewer over db. now is nil for time.Now.
func NewReviewer(db store.DBTX, now func() time.Time) *Reviewer {
	if now == nil {
		now = time.Now
	}
	return &Reviewer{queries: store.New(db), now: now}
}

// PendingItem is one opt-in with no review result for its current key.
type PendingItem struct {
	Slug        string
	ReviewKey   string
	CardVersion string
	RequestedAt time.Time
}

// State is the showcase state of one resume, as the operator reads it.
type State struct {
	Slug          string
	State         string
	Role          *string
	ReviewKey     string
	CardVersion   string
	TemplateID    *string
	ReviewedKey   *string
	Outcome       *string
	RequestedAt   time.Time
	ReviewedAt    *time.Time
	FirstListedAt *time.Time
}

// Pending lists every opt-in whose current key has no review result, oldest
// request first.
func (r *Reviewer) Pending(ctx context.Context) ([]PendingItem, error) {
	rows, err := r.queries.ListPendingResumeShowcases(ctx)
	if err != nil {
		return nil, fmt.Errorf("showcase: list pending: %w", err)
	}
	items := make([]PendingItem, 0, len(rows))
	for _, row := range rows {
		if row.Slug == nil {
			continue
		}
		items = append(items, PendingItem{
			Slug: *row.Slug, ReviewKey: row.ReviewKey, CardVersion: row.CardVersion, RequestedAt: row.RequestedAt,
		})
	}
	return items, nil
}

// Approve approves slug while key is still its current review key, and reports
// whether it did. A stale key, an unknown slug, and a resume with no opt-in all
// change nothing and report false.
func (r *Reviewer) Approve(ctx context.Context, slug, key string) (bool, error) {
	_, err := r.queries.ApproveResumeShowcase(ctx, store.ApproveResumeShowcaseParams{
		Slug: slug, ReviewKey: key, ReviewedAt: r.now(),
	})
	return reviewApplied(err)
}

// Decline declines slug while key is still its current review key, and reports
// whether it did. A declined resume leaves the listing at once.
func (r *Reviewer) Decline(ctx context.Context, slug, key string) (bool, error) {
	_, err := r.queries.DeclineResumeShowcase(ctx, store.DeclineResumeShowcaseParams{
		Slug: slug, ReviewKey: key, ReviewedAt: r.now(),
	})
	return reviewApplied(err)
}

func reviewApplied(err error) (bool, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("showcase: review: %w", err)
	}
	return true, nil
}

// Show returns the showcase state of slug, and false when it has no opt-in.
func (r *Reviewer) Show(ctx context.Context, slug string) (State, bool, error) {
	row, err := r.queries.GetResumeShowcaseBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, fmt.Errorf("showcase: show: %w", err)
	}
	return State{
		Slug: slug, State: resume.ShowcaseStateOf(row.ReviewKey, row.ReviewedKey, row.ReviewOutcome),
		Role: row.Role, ReviewKey: row.ReviewKey, CardVersion: row.CardVersion, TemplateID: row.TemplateID,
		ReviewedKey: row.ReviewedKey, Outcome: row.ReviewOutcome,
		RequestedAt: row.RequestedAt, ReviewedAt: row.ReviewedAt, FirstListedAt: row.FirstListedAt,
	}, true, nil
}
