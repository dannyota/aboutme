package viewcount

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// FlushInterval is how often buffered counts reach the database. A crash
// loses at most this much (docs/design/viewer-analytics/counting.md,
// "Aggregation").
const FlushInterval = 60 * time.Second

const flushTimeout = 10 * time.Second

// DayCell is one resume day's increments in resume_view_days.
type DayCell struct {
	ResumeID   uuid.UUID
	Day        time.Time
	Counted    int32
	Bot        int32
	Datacenter int32
	Anomaly    int32
	Invalid    int32
	Crawler    int32
}

// SignalCell is one resume day platform's increment in
// resume_share_signal_days.
type SignalCell struct {
	ResumeID uuid.UUID
	Day      time.Time
	Platform Platform
	Fetches  int32
}

type dayKey struct {
	resumeID uuid.UUID
	day      time.Time
}

type platformKey struct {
	resumeID uuid.UUID
	day      time.Time
	platform Platform
}

// buffer holds pending increments, bounded to limit cells in total.
type buffer struct {
	limit   int
	days    map[dayKey]*DayCell
	signals map[platformKey]*SignalCell
}

func newBuffer(limit int) *buffer {
	return &buffer{limit: limit, days: make(map[dayKey]*DayCell), signals: make(map[platformKey]*SignalCell)}
}

func (b *buffer) size() int {
	return len(b.days) + len(b.signals)
}

func (b *buffer) addDay(resumeID uuid.UUID, day time.Time, outcome Outcome) bool {
	key := dayKey{resumeID: resumeID, day: day}
	cell, ok := b.days[key]
	if !ok {
		if b.size() >= b.limit {
			return false
		}
		cell = &DayCell{ResumeID: resumeID, Day: day}
		b.days[key] = cell
	}
	switch outcome {
	case OutcomeCounted:
		cell.Counted++
	case OutcomeBot:
		cell.Bot++
	case OutcomeDatacenter:
		cell.Datacenter++
	case OutcomeAnomaly:
		cell.Anomaly++
	case OutcomeInvalid:
		cell.Invalid++
	case OutcomeCrawler:
		cell.Crawler++
	}
	return true
}

func (b *buffer) addSignal(resumeID uuid.UUID, day time.Time, platform Platform) bool {
	key := platformKey{resumeID: resumeID, day: day, platform: platform}
	cell, ok := b.signals[key]
	if !ok {
		if b.size() >= b.limit {
			return false
		}
		cell = &SignalCell{ResumeID: resumeID, Day: day, Platform: platform}
		b.signals[key] = cell
	}
	cell.Fetches++
	return true
}

func (b *buffer) cells() ([]DayCell, []SignalCell) {
	days := make([]DayCell, 0, len(b.days))
	for _, cell := range b.days {
		days = append(days, *cell)
	}
	signals := make([]SignalCell, 0, len(b.signals))
	for _, cell := range b.signals {
		signals = append(signals, *cell)
	}
	return days, signals
}

// Flush writes and clears the buffer. A failed write drops the batch, so a
// store outage loses counts rather than holding memory without bound.
func (c *Counter) Flush(ctx context.Context) error {
	c.mu.Lock()
	pending := c.buffer
	c.buffer = newBuffer(c.bufferLimit)
	c.mu.Unlock()
	if pending.size() == 0 {
		return nil
	}
	days, signals := pending.cells()
	writeCtx, cancel := context.WithTimeout(ctx, flushTimeout)
	defer cancel()
	if err := c.store.AddCounts(writeCtx, days, signals); err != nil {
		c.logger.ErrorContext(ctx, "viewcount: flush failed", "cells", len(days)+len(signals))
		return err
	}
	return nil
}

// Run flushes every FlushInterval until ctx ends, then flushes once more
// with a fresh deadline, so a graceful shutdown keeps the last counts.
func (c *Counter) Run(ctx context.Context) {
	ticker := time.NewTicker(FlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// The final flush must outlive the canceled run context; Flush
			// bounds it with its own timeout and logs a failure.
			if err := c.Flush(context.WithoutCancel(ctx)); err != nil { //nolint:contextcheck // See above.
				c.logger.Warn("viewcount: final flush dropped counts")
			}
			return
		case <-ticker.C:
			if err := c.Flush(ctx); err != nil {
				c.logger.WarnContext(ctx, "viewcount: flush dropped counts")
			}
		}
	}
}
