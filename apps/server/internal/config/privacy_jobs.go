package config

import (
	"errors"
	"strings"
)

// LoadPrivacyJob loads only the database and optional private-media settings
// needed by a one-shot privacy command.
func LoadPrivacyJob(getenv func(string) string, needsMedia bool) (Config, error) {
	if getenv == nil {
		return Config{}, errors.New("config: environment reader is required")
	}
	cfg := Config{DatabaseURL: strings.TrimSpace(getenv("DATABASE_URL"))}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("config: DATABASE_URL is required")
	}
	if !needsMedia {
		return cfg, nil
	}
	media, err := loadMediaConfig(getenv)
	if err != nil {
		return Config{}, err
	}
	cfg.MediaBackend = media.backend
	cfg.MediaFSDir = media.fsDir
	cfg.MediaBucket = media.bucket
	cfg.MediaRegion = media.region
	cfg.MediaEndpoint = media.endpoint
	cfg.MediaAccessKeyID = media.accessKeyID
	cfg.MediaSecretAccessKey = media.secretAccessKey
	cfg.MediaForcePathStyle = media.forcePathStyle
	return cfg, nil
}
