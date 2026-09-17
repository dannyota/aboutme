package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type mediaConfig struct {
	backend         string
	fsDir           string
	bucket          string
	region          string
	endpoint        string
	accessKeyID     string
	secretAccessKey string
	forcePathStyle  bool
}

// loadMediaConfig validates the selected backend as one closed mode. Values
// for the other mode are rejected rather than ignored, so a stale credential
// or path setting cannot silently take effect after a later backend switch.
// Errors name variables only and never include credential or endpoint values.
func loadMediaConfig(getenv func(string) string) (mediaConfig, error) {
	cfg := mediaConfig{
		backend:         strings.ToLower(strings.TrimSpace(getenv("MEDIA_BACKEND"))),
		fsDir:           strings.TrimSpace(getenv("MEDIA_FS_DIR")),
		bucket:          strings.TrimSpace(getenv("MEDIA_BUCKET")),
		region:          strings.TrimSpace(getenv("MEDIA_REGION")),
		endpoint:        strings.TrimSpace(getenv("MEDIA_ENDPOINT")),
		accessKeyID:     strings.TrimSpace(getenv("MEDIA_ACCESS_KEY_ID")),
		secretAccessKey: strings.TrimSpace(getenv("MEDIA_SECRET_ACCESS_KEY")),
	}
	forcePathStyleRaw := strings.TrimSpace(getenv("MEDIA_FORCE_PATH_STYLE"))
	forcePathStyleSet := forcePathStyleRaw != ""
	if forcePathStyleSet {
		switch forcePathStyleRaw {
		case "true":
			cfg.forcePathStyle = true
		case "false":
			cfg.forcePathStyle = false
		default:
			return mediaConfig{}, errors.New("config: MEDIA_FORCE_PATH_STYLE must be true or false")
		}
	}

	switch cfg.backend {
	case "":
		return mediaConfig{}, errors.New("config: MEDIA_BACKEND is required: must be fs or s3")
	case "fs":
		if cfg.fsDir == "" {
			return mediaConfig{}, errors.New("config: MEDIA_FS_DIR is required when MEDIA_BACKEND=fs")
		}
		for _, crossMode := range []struct {
			name  string
			value string
		}{
			{"MEDIA_BUCKET", cfg.bucket},
			{"MEDIA_REGION", cfg.region},
			{"MEDIA_ENDPOINT", cfg.endpoint},
			{"MEDIA_ACCESS_KEY_ID", cfg.accessKeyID},
			{"MEDIA_SECRET_ACCESS_KEY", cfg.secretAccessKey},
		} {
			if crossMode.value != "" {
				return mediaConfig{}, fmt.Errorf("config: %s must be absent when MEDIA_BACKEND=fs", crossMode.name)
			}
		}
		if forcePathStyleSet {
			return mediaConfig{}, errors.New("config: MEDIA_FORCE_PATH_STYLE must be absent when MEDIA_BACKEND=fs")
		}
		return cfg, nil
	case "s3":
		if cfg.fsDir != "" {
			return mediaConfig{}, errors.New("config: MEDIA_FS_DIR must be absent when MEDIA_BACKEND=s3")
		}
		if cfg.bucket == "" {
			return mediaConfig{}, errors.New("config: MEDIA_BUCKET is required when MEDIA_BACKEND=s3")
		}
		if cfg.region == "" {
			return mediaConfig{}, errors.New("config: MEDIA_REGION is required when MEDIA_BACKEND=s3")
		}
	default:
		return mediaConfig{}, errors.New("config: MEDIA_BACKEND must be fs or s3")
	}

	if cfg.endpoint == "" {
		if cfg.accessKeyID != "" {
			return mediaConfig{}, errors.New("config: MEDIA_ACCESS_KEY_ID must be absent in AWS S3 mode")
		}
		if cfg.secretAccessKey != "" {
			return mediaConfig{}, errors.New("config: MEDIA_SECRET_ACCESS_KEY must be absent in AWS S3 mode")
		}
		if forcePathStyleSet {
			return mediaConfig{}, errors.New("config: MEDIA_FORCE_PATH_STYLE must be absent in AWS S3 mode")
		}
		return cfg, nil
	}

	u, err := url.Parse(cfg.endpoint)
	if err != nil || !u.IsAbs() || u.Host == "" ||
		(strings.ToLower(u.Scheme) != "http" && strings.ToLower(u.Scheme) != "https") ||
		u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return mediaConfig{}, errors.New("config: MEDIA_ENDPOINT must be an absolute scheme://host[:port] HTTP(S) URL")
	}
	if !forcePathStyleSet || !cfg.forcePathStyle {
		return mediaConfig{}, errors.New("config: MEDIA_FORCE_PATH_STYLE=true is required with MEDIA_ENDPOINT")
	}
	if cfg.accessKeyID == "" {
		return mediaConfig{}, errors.New("config: MEDIA_ACCESS_KEY_ID is required with MEDIA_ENDPOINT")
	}
	if cfg.secretAccessKey == "" {
		return mediaConfig{}, errors.New("config: MEDIA_SECRET_ACCESS_KEY is required with MEDIA_ENDPOINT")
	}
	return cfg, nil
}
