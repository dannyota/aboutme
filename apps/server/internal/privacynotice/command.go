package privacynotice

import (
	"context"
	"errors"
	"io/fs"

	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// Run executes only the four fixed operator actions and returns aggregates.
func (s *Service) Run(ctx context.Context, action string) (Report, error) {
	if action == "status" {
		return s.Status(ctx)
	}
	if action == "activate" {
		return s.Activate(ctx)
	}
	if action != "prepare" && action != "enqueue" {
		return Report{Code: 1}, ErrState
	}
	if action == "prepare" {
		_, err := store.New(s.pool).GetPrivacyNoticeCampaign(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			if _, epochErr := s.epoch(false); epochErr != nil {
				return Report{Code: 1}, ErrState
			}
			for _, name := range []string{"send.json", "activation.json"} {
				if _, readErr := s.authorityFile(name); !errors.Is(readErr, fs.ErrNotExist) {
					return Report{Code: 1}, ErrState
				}
			}
			if prepareErr := s.Prepare(ctx); prepareErr != nil {
				return Report{Code: 1}, prepareErr
			}
			return s.preparedReport(ctx)
		}
		if err != nil {
			return Report{Code: 1}, ErrState
		}
	}
	report, err := s.Status(ctx)
	if err != nil || report.Code == 1 {
		return report, err
	}
	if action == "prepare" {
		switch report.Campaign.State {
		case "capturing":
			if prepareErr := s.Prepare(ctx); prepareErr != nil {
				return Report{Code: 1}, prepareErr
			}
		case "sealed":
		default:
			return Report{Code: 1}, ErrState
		}
		return s.preparedReport(ctx)
	}
	if report.Campaign.State != "sealed" {
		return Report{Code: 1}, ErrState
	}
	workErr := s.Enqueue(ctx)
	if workErr != nil && !errors.Is(workErr, ErrQueueFull) {
		return Report{Code: 1}, workErr
	}
	report, err = s.Status(ctx)
	if err == nil && report.Code != 1 && errors.Is(workErr, ErrQueueFull) {
		report.Code = 2
		report.result = "queue_full"
	}
	return report, err
}

func (s *Service) preparedReport(ctx context.Context) (Report, error) {
	report, err := s.Status(ctx)
	if err == nil && report.Code != 1 && report.Campaign.State == "sealed" {
		report.Code = 0
		report.result = "sealed"
	}
	return report, err
}
