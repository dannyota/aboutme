package githubevidence

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestGitHubRetry_StaysWithinOneSecondAndOneHour(t *testing.T) {
	now := fixedClock()()
	reset := func(at int64) http.Header {
		return http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {strconv.FormatInt(at, 10)}}
	}
	for _, tc := range []struct {
		name   string
		header http.Header
		want   time.Duration
	}{
		{"retry-after zero", http.Header{"Retry-After": {"0"}}, time.Second},
		{"retry-after one", http.Header{"Retry-After": {"1"}}, time.Second},
		{"retry-after one hour", http.Header{"Retry-After": {"3600"}}, time.Hour},
		{"retry-after past one hour", http.Header{"Retry-After": {"3601"}}, time.Hour},
		{"huge retry-after", http.Header{"Retry-After": {strconv.FormatInt(math.MaxInt32, 10)}}, time.Hour},
		{"reset now", reset(now.Unix()), time.Second},
		{"reset in the past", reset(now.Add(-time.Hour).Unix()), time.Second},
		{"reset in one hour", reset(now.Add(time.Hour).Unix()), time.Hour},
		{"reset past one hour", reset(now.Add(time.Hour + time.Second).Unix()), time.Hour},
		{"near-maximum reset", reset(math.MaxInt64), time.Hour},
		{"near-maximum reset less a day", reset(math.MaxInt64 - 86400), time.Hour},
		{"minimum reset", reset(math.MinInt64), time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := githubRetry(tc.header, now)
			if got != tc.want {
				t.Fatalf("githubRetry = %v, want %v", got, tc.want)
			}
			rec := httptest.NewRecorder()
			writeLimited(rec, got)
			if seconds, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || seconds < 1 || seconds > 3600 {
				t.Fatalf("Retry-After = %q, want 1 to 3600", rec.Header().Get("Retry-After"))
			}
		})
	}
}
