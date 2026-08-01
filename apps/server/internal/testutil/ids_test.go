package testutil_test

import (
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

func TestSeededUUID_SameSeedYieldsSameID(t *testing.T) {
	t.Parallel()

	a := testutil.SeededUUID(42)
	b := testutil.SeededUUID(42)
	if a != b {
		t.Fatalf("SeededUUID(42) = %q then %q, want identical values for the same seed", a, b)
	}
}

func TestSeededUUID_DifferentSeedsYieldDifferentIDs(t *testing.T) {
	t.Parallel()

	a := testutil.SeededUUID(1)
	b := testutil.SeededUUID(2)
	if a == b {
		t.Fatalf("SeededUUID(1) and SeededUUID(2) both = %q, want different values for different seeds", a)
	}
}

// TestSeededUUID_LooksLikeAUUID checks the well-known shape
// (8-4-4-4-12 hex groups, version 4, RFC 4122 variant) so callers can rely
// on the output being accepted anywhere a real UUID string is expected.
func TestSeededUUID_LooksLikeAUUID(t *testing.T) {
	t.Parallel()

	id := testutil.SeededUUID(7)
	const wantLen = len("00000000-0000-4000-8000-000000000000")
	if len(id) != wantLen {
		t.Fatalf("SeededUUID(7) = %q (len %d), want len %d", id, len(id), wantLen)
	}
	if id[14] != '4' {
		t.Errorf("SeededUUID(7) = %q, want version nibble '4' at index 14", id)
	}
	if variant := id[19]; variant != '8' && variant != '9' && variant != 'a' && variant != 'b' {
		t.Errorf("SeededUUID(7) = %q, want RFC 4122 variant nibble (8/9/a/b) at index 19, got %q", id, variant)
	}
}

func TestNamedID_SameLabelYieldsSameID(t *testing.T) {
	t.Parallel()

	a := testutil.NamedID("user-alice")
	b := testutil.NamedID("user-alice")
	if a != b {
		t.Fatalf("NamedID(%q) = %q then %q, want identical values for the same label", "user-alice", a, b)
	}
}

func TestNamedID_DifferentLabelsYieldDifferentIDs(t *testing.T) {
	t.Parallel()

	a := testutil.NamedID("user-alice")
	b := testutil.NamedID("user-bob")
	if a == b {
		t.Fatalf("NamedID(%q) and NamedID(%q) both = %q, want different values", "user-alice", "user-bob", a)
	}
}

func TestNewSeededRand_SameSeedYieldsSameSequence(t *testing.T) {
	t.Parallel()

	r1 := testutil.NewSeededRand(99)
	r2 := testutil.NewSeededRand(99)

	for i := 0; i < 5; i++ {
		v1, v2 := r1.Int64(), r2.Int64()
		if v1 != v2 {
			t.Fatalf("draw %d: %d != %d, want identical sequences from the same seed", i, v1, v2)
		}
	}
}
