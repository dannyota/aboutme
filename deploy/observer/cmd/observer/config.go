package main

import (
	"fmt"
	"os"
)

// Fixed by the design: the observer serves one environment and one site
// (docs/design/deployment-transparency/document.md, "Fields").
const (
	environment = "production"
	site        = "https://aboutme.vn"
)

// platformECS and platformKubernetes are the two OBSERVER_PLATFORM values.
const (
	platformECS        = "ecs"
	platformKubernetes = "kubernetes"
)

const defaultNamespace = "aboutme"
const defaultTUFCacheDir = "/tmp/sigstore-tuf"

// config holds every value the observer reads from its environment. Every
// value is non-secret (docs/design/deployment-transparency/README.md,
// "Access on AWS": "Its environment holds only the cluster name, the three
// service names, and the bucket name; none of them reaches the document").
type config struct {
	Platform           string
	Cluster            string
	AppService         string
	WebService         string
	MaintenanceService string
	Bucket             string
	Region             string
	Namespace          string
	S3Endpoint         string
	TUFCacheDir        string
}

// loadConfig reads and validates the observer's configuration from the
// environment. It never logs a value it reads (docs/design/deployment-
// transparency/README.md, "Run, freshness, and staleness").
func loadConfig() (config, error) {
	cfg := config{
		Platform:           os.Getenv("OBSERVER_PLATFORM"),
		Cluster:            os.Getenv("OBSERVER_CLUSTER"),
		AppService:         os.Getenv("OBSERVER_APP_SERVICE"),
		WebService:         os.Getenv("OBSERVER_WEB_SERVICE"),
		MaintenanceService: os.Getenv("OBSERVER_MAINTENANCE_SERVICE"),
		Bucket:             os.Getenv("OBSERVER_BUCKET"),
		Region:             firstNonEmpty(os.Getenv("OBSERVER_REGION"), os.Getenv("AWS_REGION")),
		Namespace:          firstNonEmpty(os.Getenv("OBSERVER_NAMESPACE"), defaultNamespace),
		S3Endpoint:         os.Getenv("OBSERVER_S3_ENDPOINT"),
		TUFCacheDir:        firstNonEmpty(os.Getenv("OBSERVER_TUF_CACHE"), defaultTUFCacheDir),
	}

	if cfg.Bucket == "" {
		return config{}, fmt.Errorf("OBSERVER_BUCKET is required")
	}
	if cfg.Region == "" {
		return config{}, fmt.Errorf("OBSERVER_REGION or AWS_REGION is required")
	}

	switch cfg.Platform {
	case platformECS:
		for name, v := range map[string]string{
			"OBSERVER_CLUSTER":             cfg.Cluster,
			"OBSERVER_APP_SERVICE":         cfg.AppService,
			"OBSERVER_WEB_SERVICE":         cfg.WebService,
			"OBSERVER_MAINTENANCE_SERVICE": cfg.MaintenanceService,
		} {
			if v == "" {
				return config{}, fmt.Errorf("%s is required when OBSERVER_PLATFORM=ecs", name)
			}
		}
	case platformKubernetes:
		// Namespace defaults, and the in-cluster host, port, token, and CA
		// come from the pod's own environment and service account.
	case "":
		return config{}, fmt.Errorf("OBSERVER_PLATFORM is required (ecs or kubernetes)")
	default:
		return config{}, fmt.Errorf("OBSERVER_PLATFORM %q is not ecs or kubernetes", cfg.Platform)
	}

	return cfg, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
