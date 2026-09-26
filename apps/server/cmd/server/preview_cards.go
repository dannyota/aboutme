package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/previewcard"
	"github.com/dannyota/aboutme/apps/server/internal/publicapi"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/realtime"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// previewCards is the stored link-preview card wiring
// (docs/adr/0055-stored-link-preview-card.md). With PREVIEW_CARD_ENABLED off
// every field is nil: pages keep the og.png share image and no scheduler
// runs.
type previewCards struct {
	public    publicapi.PreviewCards
	scheduler *previewcard.Scheduler
}

func newPreviewCards(enabled bool, pool *store.Pool, reader *publicresume.Reader, queue *renderjob.Queue, logger *slog.Logger) (previewCards, error) {
	if !enabled {
		return previewCards{}, nil
	}
	cardStore, err := previewcard.NewStore(pool, reader, time.Now)
	if err != nil {
		return previewCards{}, fmt.Errorf("create preview card store: %w", err)
	}
	service, err := previewcard.NewService(cardStore, reader, queue)
	if err != nil {
		return previewCards{}, fmt.Errorf("create preview card service: %w", err)
	}
	scheduler, err := previewcard.NewScheduler(previewcard.SchedulerConfig{Service: service, Store: cardStore, Logger: logger})
	if err != nil {
		return previewCards{}, fmt.Errorf("create preview card scheduler: %w", err)
	}
	return previewCards{public: service, scheduler: scheduler}, nil
}

// observe forwards committed resume changes to the scheduler, or is nil.
func (c previewCards) observe() func(realtime.Change) {
	if c.scheduler == nil {
		return nil
	}
	return func(change realtime.Change) { c.scheduler.Notify(change.ResumeID, change.Deleted) }
}

// run starts the scheduler and returns a channel that closes when it stops.
func (c previewCards) run(ctx context.Context, logger *slog.Logger) <-chan struct{} {
	done := make(chan struct{})
	if c.scheduler == nil {
		close(done)
		return done
	}
	go func() {
		defer close(done)
		if err := c.scheduler.Run(ctx); err != nil {
			logger.Error("preview card scheduler stopped unexpectedly")
		}
	}()
	return done
}
