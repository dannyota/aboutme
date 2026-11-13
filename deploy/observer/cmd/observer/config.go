package main

import (
	"fmt"
	"os"
)

const (
	environment = "production"
	site        = "https://aboutme.vn"
)

const (
	platformECS        = "ecs"
	platformKubernetes = "kubernetes"
	platformPodman     = "podman"
)

const defaultNamespace = "aboutme"
const defaultTUFCacheDir = "/tmp/sigstore-tuf"
const defaultObservationFile = "/observation/observation.json"
const defaultOutputDir = "/public"

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
	ObservationFile    string
	OutputDir          string
}

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
		ObservationFile:    firstNonEmpty(os.Getenv("OBSERVER_OBSERVATION_FILE"), defaultObservationFile),
		OutputDir:          firstNonEmpty(os.Getenv("OBSERVER_OUTPUT_DIR"), defaultOutputDir),
	}

	if cfg.Platform != platformPodman && cfg.Bucket == "" {
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
	case platformPodman:
	case "":
		return config{}, fmt.Errorf("OBSERVER_PLATFORM is required (ecs, kubernetes, or podman)")
	default:
		return config{}, fmt.Errorf("OBSERVER_PLATFORM %q is not ecs, kubernetes, or podman", cfg.Platform)
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
