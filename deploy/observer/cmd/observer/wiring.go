package main

import (
	"context"
	"fmt"

	"danny.vn/aboutme/deploy/observer/internal/document"
	"danny.vn/aboutme/deploy/observer/internal/platform"
	podmanplatform "danny.vn/aboutme/deploy/observer/internal/platform/podman"
	"danny.vn/aboutme/deploy/observer/internal/publish"
	"danny.vn/aboutme/deploy/observer/internal/verify"
)

type checker interface {
	Check(ctx context.Context, digest string) verify.Evidence
}

type publisher interface {
	Put(ctx context.Context, body []byte) error
}

type observer struct {
	owner     string
	lister    platform.Lister
	info      platform.Info
	verifier  checker
	publisher publisher
	build     document.BuildInfo
}

func wire(ctx context.Context, cfg config) (*observer, error) {
	if cfg.Platform != platformPodman {
		return nil, fmt.Errorf("unknown platform %q", cfg.Platform)
	}
	return newObserver(ctx, cfg, podmanplatform.New(cfg.ObservationFile),
		platform.Info{Provider: "greennode", Orchestrator: "podman", Region: cfg.Region},
		publish.NewFilePublisher(cfg.OutputDir), verify.NewMemoryCache())
}

func newObserver(ctx context.Context, cfg config, lister platform.Lister, info platform.Info, pub publisher, cache verify.Cache) (*observer, error) {
	v, err := verify.New(ctx, verify.Options{
		Cache:       cache,
		TUFCacheDir: cfg.TUFCacheDir,
	})
	if err != nil {
		return nil, fmt.Errorf("build verifier: %w", err)
	}

	return &observer{
		owner:     owner,
		lister:    lister,
		info:      info,
		verifier:  v,
		publisher: pub,
		build:     document.BuildInfo{Version: version, Commit: commit},
	}, nil
}
