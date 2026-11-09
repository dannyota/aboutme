package app

import (
	"context"
	"fmt"

	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/media"
)

// NewMedia returns the configured media backend: the local filesystem for
// "fs" or S3-compatible storage for "s3". Any other MEDIA_BACKEND fails.
func NewMedia(ctx context.Context, cfg config.Config) (media.Backend, error) {
	switch cfg.MediaBackend {
	case "fs":
		return media.NewFS(cfg.MediaFSDir)
	case "s3":
		return media.NewS3(ctx, media.S3Config{
			Bucket:          cfg.MediaBucket,
			Region:          cfg.MediaRegion,
			Endpoint:        cfg.MediaEndpoint,
			AccessKeyID:     cfg.MediaAccessKeyID,
			SecretAccessKey: cfg.MediaSecretAccessKey,
			ForcePathStyle:  cfg.MediaForcePathStyle,
		})
	default:
		return nil, fmt.Errorf("unsupported configured backend %q", cfg.MediaBackend)
	}
}
