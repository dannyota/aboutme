package testutil_test

import (
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

func TestClock_NowReturnsStartUntilAdvanced(t *testing.T) {
	t.Parallel()

	start := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	clock := testutil.NewClock(start)

	if got := clock.Now(); !got.Equal(start) {
		t.Fatalf("Now() = %v, want %v", got, start)
	}
	// Calling Now() again must not itself advance the clock (unlike
	// time.Now(), which never returns the same instant twice).
	if got := clock.Now(); !got.Equal(start) {
		t.Fatalf("second Now() = %v, want unchanged %v", got, start)
	}
}

func TestClock_AdvanceMovesTimeForwardWithoutSleeping(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()

	got := clock.Advance(90 * time.Second)
	want := testutil.Epoch.Add(90 * time.Second)
	if !got.Equal(want) {
		t.Fatalf("Advance() returned %v, want %v", got, want)
	}
	if now := clock.Now(); !now.Equal(want) {
		t.Fatalf("Now() after Advance() = %v, want %v", now, want)
	}
}

func TestClock_SetMovesToExactInstant(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	target := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)

	clock.Set(target)

	if got := clock.Now(); !got.Equal(target) {
		t.Fatalf("Now() after Set() = %v, want %v", got, target)
	}
}
