package main

import (
	"context"
	"fmt"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"

	"danny.vn/aboutme/deploy/observer/internal/document"
	"danny.vn/aboutme/deploy/observer/internal/platform"
	ecsplatform "danny.vn/aboutme/deploy/observer/internal/platform/ecs"
	kubernetesplatform "danny.vn/aboutme/deploy/observer/internal/platform/kubernetes"
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
	lister    platform.Lister
	info      platform.Info
	verifier  checker
	publisher publisher
	build     document.BuildInfo
}

func wire(ctx context.Context, cfg config) (*observer, error) {
	if cfg.Platform == platformPodman {
		return newObserver(ctx, cfg, podmanplatform.New(cfg.ObservationFile),
			platform.Info{Provider: "greennode", Orchestrator: "podman", Region: cfg.Region},
			publish.NewFilePublisher(cfg.OutputDir), verify.NewMemoryCache())
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	var lister platform.Lister
	var info platform.Info
	switch cfg.Platform {
	case platformECS:
		client := awsecs.NewFromConfig(awsCfg)
		lister = ecsplatform.New(client, ecsplatform.Config{
			Cluster:            cfg.Cluster,
			AppService:         cfg.AppService,
			WebService:         cfg.WebService,
			MaintenanceService: cfg.MaintenanceService,
		})
		info = platform.Info{Provider: "aws", Orchestrator: "ecs", Region: cfg.Region}
	case platformKubernetes:
		l, kerr := kubernetesplatform.New(kubernetesplatform.Config{Namespace: cfg.Namespace})
		if kerr != nil {
			return nil, fmt.Errorf("configure kubernetes platform: %w", kerr)
		}
		lister = l
		info = platform.Info{Provider: "greennode", Orchestrator: "kubernetes", Region: cfg.Region}
	default:
		return nil, fmt.Errorf("unknown platform %q", cfg.Platform)
	}

	s3Client := publish.NewClient(awsCfg, cfg.S3Endpoint)
	cache := publish.NewCache(s3Client, cfg.Bucket)
	pub := publish.NewPublisher(s3Client, cfg.Bucket)
	return newObserver(ctx, cfg, lister, info, pub, cache)
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
		lister:    lister,
		info:      info,
		verifier:  v,
		publisher: pub,
		build:     document.BuildInfo{Version: version, Commit: commit},
	}, nil
}
