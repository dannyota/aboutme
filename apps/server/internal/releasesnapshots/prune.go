// Package releasesnapshots deletes the manual RDS snapshots that deploy.sh
// takes before each release once they are more than 27 days old, so with a
// daily run no database backup outlives the 30-day retention, even after one
// missed run (docs/design/operations.md). It also keeps only the newest
// KeepNewest, so frequent releases stay under the AWS per-region limit of 100
// manual snapshots.
package releasesnapshots

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/aws/smithy-go"
)

const (
	// Region is the production database's region.
	Region = "ap-southeast-1"
	// Instance is the production database instance.
	Instance = "aboutme-prod"
	// TagKey is the tag deploy.sh puts on each release snapshot.
	TagKey = "aboutme:created-by"
	// TagValue is TagKey's value on a release snapshot.
	TagValue = "deploy.sh"
	// MaxAge is how long a release snapshot is kept. A daily run deletes a
	// snapshot within a day of passing it, so even with one missed run every
	// one is gone before 30 days.
	MaxAge = 27 * 24 * time.Hour
	// maxErrorCodeLen bounds the API error code a failed-delete log carries.
	maxErrorCodeLen = 64
	// KeepNewest is how many release snapshots survive regardless of age,
	// newest first by creation time.
	KeepNewest = 30
)

// namePattern is the identifier deploy.sh generates:
// aboutme-prod-<v* release tag with dots as hyphens>-<YYYYMMDDHHMM>. Only v*
// tags have release images, and RDS stores identifiers in lowercase.
var namePattern = regexp.MustCompile(`^aboutme-prod-v[0-9a-z-]+-[0-9]{12}$`)

// untagged lists release snapshots taken before deploy.sh tagged them.
var untagged = map[string]struct{}{
	"aboutme-prod-v0-1-1-202609171438": {},
}

// Client is the RDS surface Prune uses; tests inject a fake.
type Client interface {
	rds.DescribeDBSnapshotsAPIClient
	DeleteDBSnapshot(context.Context, *rds.DeleteDBSnapshotInput, ...func(*rds.Options)) (*rds.DeleteDBSnapshotOutput, error)
}

// Result is the job's fixed, identifier-free report. Each deleted or failed
// snapshot identifier goes to the diagnostic log instead.
type Result struct {
	Examined int `json:"examined"`
	Deleted  int `json:"deleted"`
	Failed   int `json:"failed"`
}

// Expired reports whether s is a deploy.sh release snapshot of the production
// instance that is available and more than MaxAge old at now. Automated
// backups, the final snapshot, a snapshot created after now, and every other
// snapshot never qualify.
func Expired(s types.DBSnapshot, now time.Time) bool {
	if !release(s) {
		return false
	}
	created := *s.SnapshotCreateTime
	if created.After(now) {
		return false
	}
	return created.Before(now.Add(-MaxAge))
}

// release reports whether s is a deploy.sh release snapshot of the production
// instance that is manual, available, and has a creation time. These are the
// only snapshots the job ever counts or deletes.
func release(s types.DBSnapshot) bool {
	id := aws.ToString(s.DBSnapshotIdentifier)
	if aws.ToString(s.DBInstanceIdentifier) != Instance || aws.ToString(s.SnapshotType) != "manual" ||
		aws.ToString(s.Status) != "available" || !namePattern.MatchString(id) || s.SnapshotCreateTime == nil {
		return false
	}
	_, ok := untagged[id]
	return ok || tagged(s)
}

// doomed returns the identifiers to delete, in listing order: every expired
// snapshot plus every one beyond the newest KeepNewest.
func doomed(releases []types.DBSnapshot, now time.Time) []string {
	newest := slices.Clone(releases)
	slices.SortStableFunc(newest, func(a, b types.DBSnapshot) int {
		return b.SnapshotCreateTime.Compare(*a.SnapshotCreateTime)
	})
	excess := make(map[string]struct{})
	if len(newest) > KeepNewest {
		for _, s := range newest[KeepNewest:] {
			excess[aws.ToString(s.DBSnapshotIdentifier)] = struct{}{}
		}
	}
	var ids []string
	for _, s := range releases {
		id := aws.ToString(s.DBSnapshotIdentifier)
		if _, over := excess[id]; over || Expired(s, now) {
			ids = append(ids, id)
		}
	}
	return ids
}

func tagged(s types.DBSnapshot) bool {
	for _, tag := range s.TagList {
		if aws.ToString(tag.Key) == TagKey && aws.ToString(tag.Value) == TagValue {
			return true
		}
	}
	return false
}

// Prune deletes every expired release snapshot and every one beyond the newest
// KeepNewest by creation time. It lists all manual snapshots
// of the instance before deleting any, continues past a failed deletion, and
// returns an error if listing or any deletion failed. Errors and logs never
// carry raw SDK text.
func Prune(ctx context.Context, client Client, now time.Time, logger *slog.Logger) (Result, error) {
	var result Result
	var releases []types.DBSnapshot
	pages := rds.NewDescribeDBSnapshotsPaginator(client, &rds.DescribeDBSnapshotsInput{
		DBInstanceIdentifier: aws.String(Instance),
		SnapshotType:         aws.String("manual"),
	})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return result, errors.New("releasesnapshots: listing snapshots failed; none deleted")
		}
		for _, s := range page.DBSnapshots {
			result.Examined++
			if release(s) {
				releases = append(releases, s)
			}
		}
	}
	for _, id := range doomed(releases, now) {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if _, err := client.DeleteDBSnapshot(ctx, &rds.DeleteDBSnapshotInput{DBSnapshotIdentifier: aws.String(id)}); err != nil {
			attrs := []any{"snapshot", id}
			if code := errorCode(err); code != "" {
				attrs = append(attrs, "code", code)
			}
			logger.Warn("releasesnapshots: delete failed", attrs...)
			result.Failed++
			continue
		}
		logger.Info("releasesnapshots: deleted", "snapshot", id)
		result.Deleted++
	}
	if result.Failed > 0 {
		return result, errors.New("releasesnapshots: one or more snapshots could not be deleted")
	}
	return result, nil
}

// errorCode returns the API error code of err, such as
// "InvalidDBSnapshotState", only when it is 1-64 ASCII letters, so no other
// SDK text reaches the log.
func errorCode(err error) string {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return ""
	}
	code := apiErr.ErrorCode()
	if code == "" || len(code) > maxErrorCodeLen {
		return ""
	}
	for _, r := range code {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			return ""
		}
	}
	return code
}
