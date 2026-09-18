package releasesnapshots

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/aws/smithy-go"
)

var now = time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)

func snapshot(id, instance, kind, status string, created time.Time, tagged bool) types.DBSnapshot {
	s := types.DBSnapshot{
		DBSnapshotIdentifier: aws.String(id),
		DBInstanceIdentifier: aws.String(instance),
		SnapshotType:         aws.String(kind),
		Status:               aws.String(status),
		SnapshotCreateTime:   aws.Time(created),
	}
	if tagged {
		s.TagList = []types.Tag{{Key: aws.String(TagKey), Value: aws.String(TagValue)}}
	}
	return s
}

func day(year int, month time.Month, d int) time.Time {
	return time.Date(year, month, d, 0, 0, 0, 0, time.UTC)
}

// fixture has three snapshots to delete and one of every kind that must stay.
func fixture() []types.DBSnapshot {
	return []types.DBSnapshot{
		snapshot("aboutme-prod-v0-1-2-202607010000", Instance, "manual", "available", day(2026, 7, 1), true),
		snapshot("aboutme-prod-v0-1-1-202609171438", Instance, "manual", "available", time.Date(2026, 9, 17, 7, 38, 12, 0, time.UTC), false),
		// 29.5 days old: past MaxAge, so the daily run deletes it before 30 days.
		snapshot("aboutme-prod-v0-1-2-202609201200", Instance, "manual", "available", time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC), true),
		// Kept: recent; exactly 27 days old; created after now; a name
		// deploy.sh cannot generate; untagged and unlisted; foreign
		// name; another instance; automated; the final snapshot; not available;
		// a wrong tag value; missing fields.
		snapshot("aboutme-prod-v0-1-3-202610010000", Instance, "manual", "available", day(2026, 10, 1), true),
		snapshot("aboutme-prod-v0-1-2-202609230000", Instance, "manual", "available", day(2026, 9, 23), true),
		snapshot("aboutme-prod-v0-1-5-202610250000", Instance, "manual", "available", day(2026, 10, 25), true),
		snapshot("aboutme-prod-manual-202601010000", Instance, "manual", "available", day(2026, 1, 1), true),
		snapshot("aboutme-prod-v0-0-9-202601010000", Instance, "manual", "available", day(2026, 1, 1), false),
		snapshot("manual-before-upgrade", Instance, "manual", "available", day(2026, 1, 1), true),
		snapshot("aboutme-prod-v0-1-0-202601010001", "aboutme-staging", "manual", "available", day(2026, 1, 1), true),
		snapshot("rds:aboutme-prod-2026-07-01-00-00", Instance, "automated", "available", day(2026, 7, 1), true),
		snapshot("aboutme-prod-final", Instance, "manual", "available", day(2026, 1, 1), true),
		snapshot("aboutme-prod-v0-1-2-202606010000", Instance, "manual", "deleting", day(2026, 6, 1), true),
		{
			DBSnapshotIdentifier: aws.String("aboutme-prod-v0-1-2-202605010000"), DBInstanceIdentifier: aws.String(Instance),
			SnapshotType: aws.String("manual"), Status: aws.String("available"), SnapshotCreateTime: aws.Time(day(2026, 5, 1)),
			TagList: []types.Tag{{Key: aws.String(TagKey), Value: aws.String("someone-else")}},
		},
		{DBSnapshotIdentifier: aws.String("aboutme-prod-v0-1-2-202604010000")},
	}
}

type fakeClient struct {
	pages       [][]types.DBSnapshot
	describeErr error
	failDelete  map[string]bool
	deleteErr   map[string]error
	inputs      []*rds.DescribeDBSnapshotsInput
	deleted     []string
}

func (f *fakeClient) DescribeDBSnapshots(_ context.Context, in *rds.DescribeDBSnapshotsInput, _ ...func(*rds.Options)) (*rds.DescribeDBSnapshotsOutput, error) {
	f.inputs = append(f.inputs, in)
	if f.describeErr != nil {
		return nil, f.describeErr
	}
	page := 0
	if in.Marker != nil {
		page = int((*in.Marker)[0] - '0')
	}
	out := &rds.DescribeDBSnapshotsOutput{DBSnapshots: f.pages[page]}
	if page+1 < len(f.pages) {
		out.Marker = aws.String(string(rune('0' + page + 1)))
	}
	return out, nil
}

func (f *fakeClient) DeleteDBSnapshot(_ context.Context, in *rds.DeleteDBSnapshotInput, _ ...func(*rds.Options)) (*rds.DeleteDBSnapshotOutput, error) {
	id := aws.ToString(in.DBSnapshotIdentifier)
	if err := f.deleteErr[id]; err != nil {
		return nil, err
	}
	if f.failDelete[id] {
		return nil, errors.New("SECRET SDK DETAIL")
	}
	f.deleted = append(f.deleted, id)
	return &rds.DeleteDBSnapshotOutput{}, nil
}

func testLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

func TestPruneDeletesOnlyExpiredReleaseSnapshots(t *testing.T) {
	t.Parallel()
	all := fixture()
	// Split across pages so pagination is exercised.
	client := &fakeClient{pages: [][]types.DBSnapshot{all[:5], all[5:]}}
	logger, _ := testLogger()

	result, err := Prune(t.Context(), client, now, logger)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	want := []string{"aboutme-prod-v0-1-2-202607010000", "aboutme-prod-v0-1-1-202609171438", "aboutme-prod-v0-1-2-202609201200"}
	if !slices.Equal(client.deleted, want) {
		t.Fatalf("deleted = %v, want %v", client.deleted, want)
	}
	if result != (Result{Examined: len(all), Deleted: 3}) {
		t.Fatalf("result = %+v, want %d examined, 3 deleted, none failed", result, len(all))
	}
	if len(client.inputs) != 2 {
		t.Fatalf("describe calls = %d, want 2 pages", len(client.inputs))
	}
	in := client.inputs[0]
	if aws.ToString(in.DBInstanceIdentifier) != Instance || aws.ToString(in.SnapshotType) != "manual" {
		t.Fatalf("describe input = %+v, want manual snapshots of %s", in, Instance)
	}
}

func TestPruneContinuesPastAFailedDeleteAndReportsIt(t *testing.T) {
	t.Parallel()
	client := &fakeClient{
		pages:      [][]types.DBSnapshot{fixture()},
		failDelete: map[string]bool{"aboutme-prod-v0-1-2-202607010000": true},
	}
	logger, logs := testLogger()

	result, err := Prune(t.Context(), client, now, logger)
	if err == nil {
		t.Fatal("Prune error = nil, want a failure for the undeleted snapshot")
	}
	if result.Deleted != 2 || result.Failed != 1 ||
		!slices.Equal(client.deleted, []string{"aboutme-prod-v0-1-1-202609171438", "aboutme-prod-v0-1-2-202609201200"}) {
		t.Fatalf("result = %+v, deleted = %v", result, client.deleted)
	}
	if !strings.Contains(logs.String(), "aboutme-prod-v0-1-2-202607010000") {
		t.Fatalf("log does not name the snapshot that failed: %q", logs.String())
	}
	if strings.Contains(logs.String(), "SECRET SDK DETAIL") || strings.Contains(err.Error(), "SECRET SDK DETAIL") {
		t.Fatalf("raw SDK error leaked: log=%q err=%v", logs.String(), err)
	}
}

func TestPruneDeletesNothingWhenListingFails(t *testing.T) {
	t.Parallel()
	client := &fakeClient{describeErr: errors.New("SECRET SDK DETAIL")}
	logger, _ := testLogger()

	result, err := Prune(t.Context(), client, now, logger)
	if err == nil || strings.Contains(err.Error(), "SECRET SDK DETAIL") {
		t.Fatalf("Prune error = %v, want a fixed listing failure", err)
	}
	if len(client.deleted) != 0 || result.Deleted != 0 {
		t.Fatalf("deleted %v after a listing failure", client.deleted)
	}
}

// A snapshot is deleted by the first daily run after it passes MaxAge. MaxAge
// plus two schedule intervals must stay strictly under the 30-day promise, so
// one missed run and a late task start cannot break it.
func TestMaxAgeKeepsEverySnapshotWithinThirtyDaysDespiteAMissedRun(t *testing.T) {
	t.Parallel()
	const scheduleInterval = 24 * time.Hour
	if MaxAge+2*scheduleInterval >= 30*24*time.Hour {
		t.Fatalf("MaxAge %s plus two daily intervals is not under 30 days", MaxAge)
	}
}

func TestExpiredBoundary(t *testing.T) {
	t.Parallel()
	cutoff := now.Add(-MaxAge)
	for _, tc := range []struct {
		created time.Time
		want    bool
	}{
		{cutoff.Add(-time.Second), true},
		{cutoff, false},
		{cutoff.Add(time.Second), false},
		{now.Add(time.Second), false},
		{now.Add(400 * 24 * time.Hour), false},
	} {
		s := snapshot("aboutme-prod-v1-0-0-202601010000", Instance, "manual", "available", tc.created, true)
		if got := Expired(s, now); got != tc.want {
			t.Errorf("Expired(created %s) = %t, want %t", tc.created, got, tc.want)
		}
	}
}

func TestPruneHonorsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	client := &fakeClient{pages: [][]types.DBSnapshot{fixture()}}
	logger, _ := testLogger()
	if _, err := Prune(ctx, client, now, logger); err == nil {
		t.Fatal("Prune on a canceled context: error = nil")
	}
	if len(client.deleted) != 0 {
		t.Fatalf("deleted %v on a canceled context", client.deleted)
	}
}

func TestExpiredRejectsNamesDeploySHCannotGenerate(t *testing.T) {
	t.Parallel()
	old := day(2026, 1, 1)
	for _, tc := range []struct {
		id   string
		want bool
	}{
		{"aboutme-prod-v0-1-2-202601010000", true},
		{"aboutme-prod-v1-0-0-rc1-202601010000", true},
		{"aboutme-prod-x0-1-2-202601010000", false},
		{"aboutme-prod-V0-1-2-202601010000", false},
		{"aboutme-prod-v-202601010000", false},
		{"aboutme-prod-v0-1-2-20260101000", false},
		{"aboutme-prod-final", false},
		{"keep-aboutme-prod-v0-1-2-202601010000", false},
	} {
		if got := Expired(snapshot(tc.id, Instance, "manual", "available", old, true), now); got != tc.want {
			t.Errorf("Expired(%q) = %t, want %t", tc.id, got, tc.want)
		}
	}
}

func TestPruneLogsOnlyATokenErrorCodeOnAFailedDelete(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"api error", &smithy.GenericAPIError{Code: "InvalidDBSnapshotState", Message: "SECRET SDK DETAIL"}, "code=InvalidDBSnapshotState"},
		{"malformed code", &smithy.GenericAPIError{Code: "Bad code=x", Message: "SECRET SDK DETAIL"}, ""},
		{"not an api error", errors.New("SECRET SDK DETAIL"), ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client := &fakeClient{
				pages:     [][]types.DBSnapshot{fixture()},
				deleteErr: map[string]error{"aboutme-prod-v0-1-2-202607010000": tc.err},
			}
			logger, logs := testLogger()
			if _, err := Prune(t.Context(), client, now, logger); err == nil {
				t.Fatal("Prune error = nil, want a failure")
			}
			logged := logs.String()
			if strings.Contains(logged, "SECRET SDK DETAIL") {
				t.Fatalf("log leaks the SDK message: %q", logged)
			}
			if tc.want == "" && strings.Contains(logged, "code=") {
				t.Fatalf("log carries a code it should not: %q", logged)
			}
			if tc.want != "" && !strings.Contains(logged, tc.want) {
				t.Fatalf("log missing %q: %q", tc.want, logged)
			}
		})
	}
}
