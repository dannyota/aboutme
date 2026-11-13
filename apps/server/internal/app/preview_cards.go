package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"danny.vn/aboutme/apps/server/internal/previewcard"
	"danny.vn/aboutme/apps/server/internal/publicapi"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/realtime"
	"danny.vn/aboutme/apps/server/internal/renderjob"
	"danny.vn/aboutme/apps/server/internal/store"
)

type PreviewCards struct {
	Public    publicapi.PreviewCards
	Scheduler *previewcard.Scheduler
}

func NewPreviewCards(enabled bool, pool *store.Pool, reader *publicresume.Reader, queue *renderjob.Queue, logger *slog.Logger) (PreviewCards, error) {
	if !enabled {
		return PreviewCards{}, nil
	}
	cardStore, err := previewcard.NewStore(pool, reader, time.Now)
	if err != nil {
		return PreviewCards{}, fmt.Errorf("create preview card store: %w", err)
	}
	service, err := previewcard.NewService(cardStore, reader, queue)
	if err != nil {
		return PreviewCards{}, fmt.Errorf("create preview card service: %w", err)
	}
	scheduler, err := previewcard.NewScheduler(previewcard.SchedulerConfig{Service: service, Store: cardStore, Logger: logger})
	if err != nil {
		return PreviewCards{}, fmt.Errorf("create preview card scheduler: %w", err)
	}
	return PreviewCards{Public: service, Scheduler: scheduler}, nil
}

func (c PreviewCards) Observe() func(realtime.Change) {
	if c.Scheduler == nil {
		return nil
	}
	return func(change realtime.Change) { c.Scheduler.Notify(change.ResumeID, change.Deleted) }
}

func (c PreviewCards) Run(ctx context.Context, logger *slog.Logger) <-chan struct{} {
	done := make(chan struct{})
	if c.Scheduler == nil {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		if err := c.Scheduler.Run(ctx); err != nil {
			logger.Error("preview card scheduler stopped unexpectedly")
		}
	}()
	return done
}
