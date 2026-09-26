package main

import (
	"context"
	"fmt"
	"time"

	"github.com/dannyota/aboutme/deploy/observer/internal/document"
	"github.com/dannyota/aboutme/deploy/observer/internal/verify"
)

// run performs one observation: list the running images, check every
// distinct digest, build and encode the document, and publish it. A
// platform read failure or an encode failure writes nothing
// (docs/design/deployment-transparency/README.md, "Run, freshness, and
// staleness").
func (o *observer) run(ctx context.Context, now func() time.Time) error {
	images, err := o.lister.Running(ctx)
	if err != nil {
		return fmt.Errorf("list running images: %w", err)
	}

	// Verification stops early enough to leave time to publish, so a slow
	// GitHub or registry turns into unchecked images, not a missed write.
	vctx, cancel := verificationContext(ctx)
	defer cancel()
	evidence := make(map[string]verify.Evidence, len(images))
	for _, img := range images {
		if _, ok := evidence[img.Digest]; ok {
			continue
		}
		evidence[img.Digest] = o.verifier.Check(vctx, img.Digest)
	}

	doc, err := document.Build(document.Input{
		Environment: environment,
		Site:        site,
		Platform:    o.info,
		ObservedAt:  now(),
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

// publishReserve is the time a run keeps for building and publishing the
// document after verification stops.
const publishReserve = 5 * time.Second

// verificationContext ends publishReserve before ctx's deadline.
func verificationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return context.WithCancel(ctx)
	}
	return context.WithDeadline(ctx, deadline.Add(-publishReserve))
}
