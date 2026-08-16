package password

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"sort"
	"testing"

	"golang.org/x/text/unicode/norm"
)

// testBlocklistData builds a valid digests.bin-style blob for the given
// passwords, NFC-normalizing each exactly like the generator does.
func testBlocklistData(passwords ...string) []byte {
	digests := make([][32]byte, 0, len(passwords))
	for _, p := range passwords {
		digests = append(digests, sha256.Sum256([]byte(norm.NFC.String(p))))
	}
	sort.Slice(digests, func(i, j int) bool {
		return bytes.Compare(digests[i][:], digests[j][:]) < 0
	})
	data := make([]byte, 12+len(digests)*32)
	copy(data[:8], blocklistMagic)
	binary.BigEndian.PutUint32(data[8:12], uint32(len(digests)))
	for i, d := range digests {
		copy(data[12+i*32:], d[:])
	}
	return data
}

func TestLoadBlocklistEmbeddedArtifact(t *testing.T) {
	t.Parallel()
	b, err := LoadBlocklist()
	if err != nil {
		t.Fatalf("LoadBlocklist error = %v", err)
	}
	if b.Len() != 99839 {
		t.Errorf("Len = %d, want 99839", b.Len())
	}
}

func TestNewBlocklistRejectsCorruption(t *testing.T) {
	t.Parallel()

	valid := testBlocklistData("alpha", "bravo")

	t.Run("too short", func(t *testing.T) {
		t.Parallel()
		if _, err := NewBlocklist(valid[:5]); err == nil {
			t.Fatal("NewBlocklist error = nil, want error for short input")
		}
	})
	t.Run("bad magic", func(t *testing.T) {
		t.Parallel()
		bad := append([]byte(nil), valid...)
		copy(bad[:8], "BOGUS!!!")
		if _, err := NewBlocklist(bad); err == nil {
			t.Fatal("NewBlocklist error = nil, want error for bad magic")
		}
	})
	t.Run("size mismatch", func(t *testing.T) {
		t.Parallel()
		bad := valid[:len(valid)-1]
		if _, err := NewBlocklist(bad); err == nil {
			t.Fatal("NewBlocklist error = nil, want error for truncated digests")
		}
	})
	t.Run("not increasing", func(t *testing.T) {
		t.Parallel()
		bad := append([]byte(nil), valid...)
		// Swap the two digests to break ordering.
		copy(bad[12:12+32], valid[12+32:12+64])
		copy(bad[12+32:12+64], valid[12:12+32])
		if _, err := NewBlocklist(bad); err == nil {
			t.Fatal("NewBlocklist error = nil, want error for non-increasing digests")
		}
	})
}

func TestBlocklistContains(t *testing.T) {
	t.Parallel()
	data := testBlocklistData("password123", "letmein123", "welcome123")
	b, err := NewBlocklist(data)
	if err != nil {
		t.Fatalf("NewBlocklist error = %v", err)
	}

	present := sha256.Sum256([]byte(norm.NFC.String("password123")))
	absent := sha256.Sum256([]byte(norm.NFC.String("not-in-list")))
	if !b.Contains(present) {
		t.Error("Contains(present) = false, want true")
	}
	if b.Contains(absent) {
		t.Error("Contains(absent) = true, want false")
	}
}
