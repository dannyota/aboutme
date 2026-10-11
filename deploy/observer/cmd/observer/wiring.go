package main

import (
	"context"
	"fmt"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"

	"github.com/dannyota/aboutme/deploy/observer/internal/document"
	"github.com/dannyota/aboutme/deploy/observer/internal/platform"
	ecsplatform "github.com/dannyota/aboutme/deploy/observer/internal/platform/ecs"
	kubernetesplatform "github.com/dannyota/aboutme/deploy/observer/internal/platform/kubernetes"
	podmanplatform "github.com/dannyota/aboutme/deploy/observer/internal/platform/podman"
	"github.com/dannyota/aboutme/deploy/observer/internal/publish"
	"github.com/dannyota/aboutme/deploy/observer/internal/verify"
)

// checker checks one running digest against the signed build record. It is
// satisfied by *verify.Verifier; a narrow interface lets tests fake it
// (docs/design/deployment-transparency/verification.md, "What verified
// means").
type checker interface {
	Check(ctx context.Context, digest string) verify.Evidence
}

// publisher writes the encoded document to object storage or a host directory.
type publisher interface {
	Put(ctx context.Context, body []byte) error
}

// observer holds everything one run needs, built once so the verifier's
// warm state (its TUF trust root, its cache) survives across Lambda
// invocations (docs/design/deployment-transparency/README.md, "Where the
// observer runs").
type observer struct {
	lister    platform.Lister
	info      platform.Info
	verifier  checker
	publisher publisher
	build     document.BuildInfo
}

// wire builds one adapter, verifier, cache, and publisher per process.
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
