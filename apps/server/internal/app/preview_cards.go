package app

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

// PreviewCards is the stored link-preview card wiring
// (docs/adr/0014-public-page-head-and-link-preview.md). With PREVIEW_CARD_ENABLED off
// every field is nil: pages keep the og.png share image and no scheduler
// runs.
type PreviewCards struct {
	// Public serves stored cards to the public service.
	Public publicapi.PreviewCards
	// Scheduler keeps each live resume's stored card current.
	Scheduler *previewcard.Scheduler
}

// NewPreviewCards builds the card store, service, and scheduler when enabled
// and returns the zero PreviewCards otherwise.
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

// Observe forwards committed resume changes to the scheduler, or is nil.
func (c PreviewCards) Observe() func(realtime.Change) {
	if c.Scheduler == nil {
		return nil
	}
	return func(change realtime.Change) { c.Scheduler.Notify(change.ResumeID, change.Deleted) }
}

// Run starts the scheduler and returns a channel that closes when it stops.
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
