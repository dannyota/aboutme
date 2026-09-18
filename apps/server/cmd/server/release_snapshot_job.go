package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/rds"

	"github.com/dannyota/aboutme/apps/server/internal/releasesnapshots"
)

// runReleaseSnapshotSweep deletes deploy.sh release snapshots older than
// releasesnapshots.MaxAge, using the task role's credentials.
func runReleaseSnapshotSweep(ctx context.Context, logger *slog.Logger) (any, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(releasesnapshots.Region))
	if err != nil {
		return nil, errors.New("release snapshot job AWS configuration is invalid")
	}
	return releasesnapshots.Prune(ctx, rds.NewFromConfig(cfg), time.Now(), logger)
}
