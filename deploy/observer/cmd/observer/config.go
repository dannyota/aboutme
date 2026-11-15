package main

import (
	"fmt"
	"os"
)

const (
	environment = "production"
	site        = "https://aboutme.vn"
)

const platformPodman = "podman"

const defaultTUFCacheDir = "/tmp/sigstore-tuf"
const defaultObservationFile = "/observation/observation.json"
const defaultOutputDir = "/public"

type config struct {
	Platform        string
	Region          string
	TUFCacheDir     string
	ObservationFile string
	OutputDir       string
}

func loadConfig() (config, error) {
	cfg := config{
		Platform:        os.Getenv("OBSERVER_PLATFORM"),
		Region:          os.Getenv("OBSERVER_REGION"),
		TUFCacheDir:     firstNonEmpty(os.Getenv("OBSERVER_TUF_CACHE"), defaultTUFCacheDir),
		ObservationFile: firstNonEmpty(os.Getenv("OBSERVER_OBSERVATION_FILE"), defaultObservationFile),
		OutputDir:       firstNonEmpty(os.Getenv("OBSERVER_OUTPUT_DIR"), defaultOutputDir),
	}

	if cfg.Region == "" {
		return config{}, fmt.Errorf("OBSERVER_REGION is required")
	}

	switch cfg.Platform {
	case platformPodman:
	case "":
		return config{}, fmt.Errorf("OBSERVER_PLATFORM is required (podman)")
	default:
		return config{}, fmt.Errorf("OBSERVER_PLATFORM %q is not podman", cfg.Platform)
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
