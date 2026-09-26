package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dannyota/aboutme/deploy/observer/internal/document"
	"github.com/dannyota/aboutme/deploy/observer/internal/platform"
	"github.com/dannyota/aboutme/deploy/observer/internal/verify"
)

type fakeLister struct {
	images []platform.Image
	err    error
}

func (f *fakeLister) Running(context.Context) ([]platform.Image, error) { return f.images, f.err }

type fakeChecker struct {
	calls int
	seen  []string
}

func (f *fakeChecker) Check(_ context.Context, digest string) verify.Evidence {
	f.calls++
	f.seen = append(f.seen, digest)
	return verify.Evidence{Digest: digest, Provenance: verify.Provenance{Status: verify.Unchecked}}
}

type fakePublisher struct {
	body []byte
	err  error
}

func (f *fakePublisher) Put(_ context.Context, body []byte) error {
	f.body = body
	return f.err
}

func fixedNow() time.Time { return time.Date(2026, 10, 2, 3, 14, 5, 0, time.UTC) }

// serverImage and webImage are enough to build a document past its required
// components (docs/design/deployment-transparency/document.md, "Summary and
// release").
func serverImage(digest string, since time.Time) platform.Image {
	return platform.Image{Component: platform.Server, Digest: digest, Replicas: 1, RunningSince: since}
}

func TestRunPublishesOnSuccess(t *testing.T) {
	digest := "sha256:" + repeatHex("a1", 32)
	lister := &fakeLister{images: []platform.Image{
		serverImage(digest, fixedNow()),
		{Component: platform.Web, Digest: digest, Replicas: 1, RunningSince: fixedNow()},
		{Component: platform.Caddy, Digest: digest, Replicas: 1, RunningSince: fixedNow()},
	}}
	checker := &fakeChecker{}
	pub := &fakePublisher{}
	obs := &observer{
		lister:    lister,
		info:      platform.Info{Provider: "aws", Orchestrator: "ecs", Region: "ap-southeast-1"},
		verifier:  checker,
		publisher: pub,
		build:     document.BuildInfo{},
	}

	if err := obs.run(context.Background(), fixedNow); err != nil {
		t.Fatalf("run: %v", err)
	}
	if pub.body == nil {
		t.Fatal("run: nothing was published")
	}
	if checker.calls != 1 {
		t.Errorf("run: verifier.Check called %d times for one distinct digest, want 1", checker.calls)
	}
}

func TestRunFailsWithoutPublishingOnListError(t *testing.T) {
	lister := &fakeLister{err: errors.New("ecs: canary failure")}
	checker := &fakeChecker{}
	pub := &fakePublisher{}
	obs := &observer{lister: lister, verifier: checker, publisher: pub}

	if err := obs.run(context.Background(), fixedNow); err == nil {
		t.Fatal("run: got nil error for a failed platform read")
	}
	if pub.body != nil {
		t.Error("run: published a document after a failed platform read")
	}
	if checker.calls != 0 {
		t.Error("run: checked a digest after a failed platform read")
	}
}

func TestRunFailsWithoutPublishingOnBuildError(t *testing.T) {
	lister := &fakeLister{images: []platform.Image{
		{Component: "not-a-component", Digest: "sha256:" + repeatHex("a1", 32), Replicas: 1, RunningSince: fixedNow()},
	}}
	checker := &fakeChecker{}
	pub := &fakePublisher{}
	obs := &observer{lister: lister, verifier: checker, publisher: pub}

	if err := obs.run(context.Background(), fixedNow); err == nil {
		t.Fatal("run: got nil error for an image with an unknown component")
	}
	if pub.body != nil {
		t.Error("run: published a document that failed to build")
	}
}

func repeatHex(pair string, n int) string {
	out := make([]byte, 0, len(pair)*n)
	for i := 0; i < n; i++ {
		out = append(out, pair...)
	}
	return string(out)
}
