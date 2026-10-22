package privacynotice

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func TestAggregateOutputIsClosedAndContainsNoMemberData(t *testing.T) {
	now := time.Date(2026, 10, 19, 1, 2, 3, 456789000, time.UTC)
	cursor := uuid.New()
	r := Report{Code: 2, Campaign: store.PrivacyNoticeCampaign{ID: authmail.PrivacyNoticeID, ContentSha256: ContentDigest(), CutoffAt: now, State: "sealed", ExpectedCount: 1, CaptureCursor: &cursor, SealedAt: &now}, Totals: store.PrivacyNoticeTotalsRow{Actual: 1, Unqueued: 1}}
	data, err := r.JSON(false)
	if err != nil || len(data) > 4096 || data[len(data)-1] != '\n' || strings.Contains(string(data), cursor.String()) {
		t.Fatal("aggregate output leaked member data or framing")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || len(fields) != 18 {
		t.Fatal("aggregate output field set differs")
	}
	for _, key := range []string{"schema_version", "result", "campaign_id", "content_sha256", "cutoff_at", "expected_count", "state", "actual_count", "counts", "failure_counts", "recovery_count", "recovery_counts", "activation_count", "sealed_at", "first_activated_at", "activated_at", "aborted_at", "last_accepted_at"} {
		if _, ok := fields[key]; !ok {
			t.Fatal("aggregate output field missing")
		}
	}
	if string(fields["result"]) != `"pending"` || string(fields["last_accepted_at"]) != "null" || string(fields["cutoff_at"]) != `"2026-10-19T01:02:03.456789Z"` {
		t.Fatal("aggregate output values differ")
	}
	for _, key := range []string{"counts", "failure_counts", "recovery_counts"} {
		var counts map[string]int64
		if json.Unmarshal(fields[key], &counts) != nil || len(counts) != 6 {
			t.Fatal("aggregate nested count set differs")
		}
	}
	r.Code = 0
	r.Totals.Unqueued = 0
	r.Totals.Accepted = 1
	r.Totals.LastAcceptedAt = now
	r.Campaign.State = "activated"
	r.Campaign.ActivatedAt = &now
	r.Campaign.FirstActivatedAt = &now
	r.Campaign.ActivationCount = 1
	data, err = r.JSON(true)
	if err != nil || !strings.Contains(string(data), `"result":"activated"`) {
		t.Fatal("activation result differs")
	}
}

func TestAggregateOutputRejectsResultStateMismatch(t *testing.T) {
	r := Report{Code: 0, Campaign: store.PrivacyNoticeCampaign{ID: authmail.PrivacyNoticeID, ContentSha256: ContentDigest(), CutoffAt: time.Now().Truncate(time.Microsecond), State: "capturing"}}
	if _, err := r.JSON(false); err == nil {
		t.Fatal("capturing cohort emitted ready result")
	}
	r.Campaign.State = "sealed"
	if _, err := r.JSON(true); err == nil {
		t.Fatal("sealed cohort emitted activated result")
	}
}

func TestAggregateOutputRejectsOverflowAndInconsistentCounts(t *testing.T) {
	r := Report{Code: 1, Campaign: store.PrivacyNoticeCampaign{ID: authmail.PrivacyNoticeID, ContentSha256: ContentDigest(), CutoffAt: time.Now().Truncate(time.Microsecond), State: "retired", RecoveryCount: math.MaxInt64, RecoveryExpiredCount: math.MaxInt64}, Totals: store.PrivacyNoticeTotalsRow{}}
	if _, err := r.JSON(false); err != nil {
		t.Fatal("maximum exact count rejected")
	}
	r.Campaign.RecoveryJobMissingCount = 1
	if _, err := r.JSON(false); err == nil {
		t.Fatal("overflowing recovery sum accepted")
	}
	r.Campaign.RecoveryJobMissingCount = 0
	r.Totals.Actual = 1
	if _, err := r.JSON(false); err == nil {
		t.Fatal("inconsistent aggregate accepted")
	}
}
