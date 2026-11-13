package main

import (
	"context"
	"fmt"
	"time"

	"danny.vn/aboutme/deploy/observer/internal/document"
	"danny.vn/aboutme/deploy/observer/internal/platform"
	"danny.vn/aboutme/deploy/observer/internal/verify"
)

func (o *observer) run(ctx context.Context, now func() time.Time) error {
	images, err := o.lister.Running(ctx)
	if err != nil {
		return fmt.Errorf("list running images: %w", err)
	}

	vctx, cancel := verificationContext(ctx)
	defer cancel()
	evidence := make(map[string]verify.Evidence, len(images))
	for _, img := range images {
		if _, ok := evidence[img.Digest]; ok {
			continue
		}
		evidence[img.Digest] = o.verifier.Check(vctx, img.Digest)
	}

	observed := now()
	if snapshot, ok := o.lister.(platform.SnapshotLister); ok {
		observed = snapshot.ObservationTime()
	}
	doc, err := document.Build(document.Input{
		Environment: environment,
		Site:        site,
		Platform:    o.info,
		ObservedAt:  observed,
		Images:      images,
		Evidence:    evidence,
		Observer:    o.build,
	})
	if err != nil {
		return fmt.Errorf("build document: %w", err)
	}

	body, err := document.Encode(doc)
	if err != nil {
		return fmt.Errorf("encode document: %w", err)
	}

	if err := o.publisher.Put(ctx, body); err != nil {
		return fmt.Errorf("publish document: %w", err)
	}
	return nil
}

const publishReserve = 5 * time.Second

func verificationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return context.WithCancel(ctx)
	}
	return context.WithDeadline(ctx, deadline.Add(-publishReserve))
}
