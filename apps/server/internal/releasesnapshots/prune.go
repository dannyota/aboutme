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
	Region          = "ap-southeast-1"
	Instance        = "aboutme-prod"
	TagKey          = "aboutme:created-by"
	TagValue        = "deploy.sh"
	MaxAge          = 27 * 24 * time.Hour
	maxErrorCodeLen = 64
	KeepNewest      = 30
)

var namePattern = regexp.MustCompile(`^aboutme-prod-v[0-9a-z-]+-[0-9]{12}$`)

var untagged = map[string]struct{}{
	"aboutme-prod-v0-1-1-202609171438": {},
}

type Client interface {
	rds.DescribeDBSnapshotsAPIClient
	DeleteDBSnapshot(context.Context, *rds.DeleteDBSnapshotInput, ...func(*rds.Options)) (*rds.DeleteDBSnapshotOutput, error)
}

type Result struct {
	Examined int `json:"examined"`
	Deleted  int `json:"deleted"`
	Failed   int `json:"failed"`
}

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

func release(s types.DBSnapshot) bool {
	id := aws.ToString(s.DBSnapshotIdentifier)
	if aws.ToString(s.DBInstanceIdentifier) != Instance || aws.ToString(s.SnapshotType) != "manual" ||
		aws.ToString(s.Status) != "available" || !namePattern.MatchString(id) || s.SnapshotCreateTime == nil {
		return false
	}
	_, ok := untagged[id]
	return ok || tagged(s)
}

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
