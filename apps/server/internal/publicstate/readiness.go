package publicstate

import (
	"context"
	"errors"
)

// ReadinessDependencies are the external checks required before this process
// may admit public traffic.
type ReadinessDependencies struct {
	PingDatabase  func(context.Context) error
	ProbeRenderer func(context.Context) error
}

// Readiness combines durable public-state safety with PostgreSQL and the
// direct renderer. It intentionally returns one opaque error so /readyz does
// not disclose dependency or recovery details.
type Readiness struct {
	coordinator  *Coordinator
	dependencies ReadinessDependencies
}

var errNotReady = errors.New("public service is not ready")

func NewReadiness(coordinator *Coordinator, dependencies ReadinessDependencies) *Readiness {
	return &Readiness{coordinator: coordinator, dependencies: dependencies}
}

func (r *Readiness) Ping(ctx context.Context) error {
	if r == nil || r.coordinator == nil || r.dependencies.PingDatabase == nil || r.dependencies.ProbeRenderer == nil {
		return errNotReady
	}
	if err := ctx.Err(); err != nil {
		return errNotReady
	}
	if err := r.coordinator.Ready(); err != nil {
		return errNotReady
	}
	if err := r.dependencies.PingDatabase(ctx); err != nil {
		return errNotReady
	}
	if err := r.dependencies.ProbeRenderer(ctx); err != nil {
		return errNotReady
	}
	return nil
}
