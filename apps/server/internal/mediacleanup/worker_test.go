package mediacleanup

import (
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/media"
)

func TestRetryDelayStartsAtOneMinuteAndCapsAtSixHours(t *testing.T) {
	t.Parallel()

	cases := []struct {
		attempt int32
		want    time.Duration
	}{
		{attempt: 1, want: time.Minute},
		{attempt: 2, want: 2 * time.Minute},
		{attempt: 3, want: 4 * time.Minute},
		{attempt: 9, want: 4*time.Hour + 16*time.Minute},
		{attempt: 10, want: 6 * time.Hour},
		{attempt: 50, want: 6 * time.Hour},
	}
	for _, tc := range cases {
		if got := retryDelay(tc.attempt); got != tc.want {
			t.Errorf("retryDelay(%d) = %v, want %v", tc.attempt, got, tc.want)
		}
	}
}

func TestValidateListedPageRejectsBackendContractViolations(t *testing.T) {
	t.Parallel()

	validKey := "resumes/018cc251-f400-7000-8000-000000000001/photo-0123456789abcdef0123456789abcdef.jpg"
	cases := []struct {
		name       string
		cursor     string
		objects    []media.Object
		nextCursor string
	}{
		{name: "zero updated time", objects: []media.Object{{Key: validKey}}},
		{name: "does not advance", cursor: validKey, objects: []media.Object{{Key: validKey, UpdatedAt: time.Unix(1, 0)}}},
		{name: "bad next cursor", objects: []media.Object{{Key: validKey, UpdatedAt: time.Unix(1, 0)}}, nextCursor: "../bad"},
		{name: "next cursor not last key", objects: []media.Object{{Key: validKey, UpdatedAt: time.Unix(1, 0)}}, nextCursor: "resumes/018cc251-f400-7000-8000-000000000002/photo-0123456789abcdef0123456789abcdef.jpg"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validateListedPage(tc.cursor, tc.objects, tc.nextCursor); err == nil {
				t.Fatal("validateListedPage() error = nil, want fail-closed rejection")
			}
		})
	}
}
