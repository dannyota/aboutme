package viewcount

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// Queries is the generated query subset PGStore uses.
type Queries interface {
	GetLiveViewResumeBySlug(ctx context.Context, slug string) (store.GetLiveViewResumeBySlugRow, error)
	GetLiveViewResumeByID(ctx context.Context, id uuid.UUID) (store.GetLiveViewResumeByIDRow, error)
	AddResumeViewDays(ctx context.Context, arg store.AddResumeViewDaysParams) error
	AddResumeShareSignalDays(ctx context.Context, arg store.AddResumeShareSignalDaysParams) error
}

var _ Queries = (*store.Queries)(nil)

// PGStore implements Store over the generated queries.
type PGStore struct {
	Queries Queries
}

// LiveResumeBySlug implements Store.
func (s PGStore) LiveResumeBySlug(ctx context.Context, slug string) (LiveResume, error) {
	row, err := s.Queries.GetLiveViewResumeBySlug(ctx, slug)
	if errors.Is(err, pgx.ErrNoRows) {
		return LiveResume{}, ErrNotFound
	}
	if err != nil {
		return LiveResume{}, err
	}
	return LiveResume{ID: row.ID, Owner: row.UserID}, nil
}

// LiveResumeByID implements Store.
func (s PGStore) LiveResumeByID(ctx context.Context, id uuid.UUID) (LiveResume, error) {
	row, err := s.Queries.GetLiveViewResumeByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return LiveResume{}, ErrNotFound
	}
	if err != nil {
		return LiveResume{}, err
	}
	return LiveResume{ID: row.ID, Owner: row.UserID}, nil
}

// AddCounts implements Store.
func (s PGStore) AddCounts(ctx context.Context, days []DayCell, signals []SignalCell) error {
	if len(days) > 0 {
		params := store.AddResumeViewDaysParams{}
		for _, cell := range days {
			params.ResumeIds = append(params.ResumeIds, cell.ResumeID)
			params.Days = append(params.Days, Date(cell.Day))
			params.Counted = append(params.Counted, cell.Counted)
			params.Bot = append(params.Bot, cell.Bot)
			params.Datacenter = append(params.Datacenter, cell.Datacenter)
			params.Anomaly = append(params.Anomaly, cell.Anomaly)
			params.Invalid = append(params.Invalid, cell.Invalid)
			params.Crawler = append(params.Crawler, cell.Crawler)
		}
		if err := s.Queries.AddResumeViewDays(ctx, params); err != nil {
			return err
		}
	}
	if len(signals) > 0 {
		params := store.AddResumeShareSignalDaysParams{}
		for _, cell := range signals {
			params.ResumeIds = append(params.ResumeIds, cell.ResumeID)
			params.Days = append(params.Days, Date(cell.Day))
			params.Platforms = append(params.Platforms, string(cell.Platform))
			params.Fetches = append(params.Fetches, cell.Fetches)
		}
		if err := s.Queries.AddResumeShareSignalDays(ctx, params); err != nil {
			return err
		}
	}
	return nil
}

// Date converts a Day value to a store date.
func Date(day time.Time) pgtype.Date {
	return pgtype.Date{Time: day, Valid: true}
}
