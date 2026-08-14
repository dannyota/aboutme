package resumeapi

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSlugAttemptLimiterAllowsThirtyChangedAttemptsThenDenies(t *testing.T) {
	t.Parallel()

	limiter := newSlugAttemptLimiter()
	accountID := uuid.New()
	now := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
	for attempt := 1; attempt <= 30; attempt++ {
		if !limiter.AllowChangedSlug(accountID, now) {
			t.Fatalf("attempt %d was denied, want admitted", attempt)
		}
	}
	if limiter.AllowChangedSlug(accountID, now) {
		t.Fatal("31st changed-slug attempt was admitted")
	}
}

func TestSlugAttemptLimiterCountsDeniedAttemptsInItsRollingHour(t *testing.T) {
	t.Parallel()

	limiter := newSlugAttemptLimiter()
	accountID := uuid.New()
	start := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
	for attempt := 0; attempt < 30; attempt++ {
		if !limiter.AllowChangedSlug(accountID, start) {
			t.Fatalf("initial attempt %d was denied", attempt+1)
		}
	}
	deniedAt := start.Add(30 * time.Minute)
	for attempt := 0; attempt < 30; attempt++ {
		if limiter.AllowChangedSlug(accountID, deniedAt) {
			t.Fatalf("denied attempt %d was admitted", attempt+1)
		}
	}
	if limiter.AllowChangedSlug(accountID, start.Add(time.Hour).Add(time.Nanosecond)) {
		t.Fatal("a denied changed-slug attempt did not retain rolling-hour debt")
	}
	if !limiter.AllowChangedSlug(accountID, start.Add(90*time.Minute).Add(time.Nanosecond)) {
		t.Fatal("attempt after the rolling-hour boundary was denied")
	}
}

func TestSlugAttemptLimiterUsesBoundedSharedOverflow(t *testing.T) {
	t.Parallel()

	limiter := newSlugAttemptLimiter(1)
	now := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
	tracked := uuid.New()
	if !limiter.AllowChangedSlug(tracked, now) {
		t.Fatal("tracked account was denied")
	}
	overflowA := uuid.New()
	overflowB := uuid.New()
	for attempt := 0; attempt < 30; attempt++ {
		if !limiter.AllowChangedSlug(overflowA, now) {
			t.Fatalf("overflow attempt %d was denied", attempt+1)
		}
	}
	if limiter.AllowChangedSlug(overflowB, now) {
		t.Fatal("separate untracked account did not share bounded overflow")
	}
	if got := limiter.trackedBucketCount(); got != 1 {
		t.Fatalf("tracked bucket count = %d, want 1", got)
	}
}

func TestSlugAttemptLimiterSerializesConcurrentAdmissions(t *testing.T) {
	t.Parallel()

	limiter := newSlugAttemptLimiter()
	accountID := uuid.New()
	now := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
	var group sync.WaitGroup
	admitted := make(chan bool, 64)
	for attempt := 0; attempt < cap(admitted); attempt++ {
		group.Add(1)
		go func() {
			defer group.Done()
			admitted <- limiter.AllowChangedSlug(accountID, now)
		}()
	}
	group.Wait()
	close(admitted)
	got := 0
	for allowed := range admitted {
		if allowed {
			got++
		}
	}
	if got != 30 {
		t.Fatalf("concurrent admissions = %d, want 30", got)
	}
}
