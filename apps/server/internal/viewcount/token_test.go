package viewcount

import (
	mathrand "math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// View tokens are sealed and authenticated (docs/design/viewer-analytics/
// counting.md, "Layers 5 and 6").
func TestTokenRoundTripAndTamper(t *testing.T) {
	s, err := newSealer(mathrand.NewChaCha8([32]byte{1}))
	if err != nil {
		t.Fatalf("newSealer: %v", err)
	}
	resumeID := uuid.MustParse("018f5b6a-9a3e-7c21-8b1e-000000000010")
	issued := time.UnixMilli(1_790_000_000_123)
	token, id, err := s.seal(resumeID, issued)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if strings.Contains(token, "=") || len(token) > maxTokenText {
		t.Fatalf("token %q is not bounded unpadded base64url", token)
	}
	claims, err := s.open(token)
	if err != nil || claims.resumeID != resumeID || !claims.issuedAt.Equal(issued) || claims.id != id {
		t.Fatalf("open = %+v, %v", claims, err)
	}
	flipped := []byte(token)
	flipped[10] ^= 1
	for _, bad := range []string{"", "!!!!", token[:len(token)-1], string(flipped), strings.Repeat("A", maxTokenText+1)} {
		if _, openErr := s.open(bad); openErr == nil {
			t.Errorf("open(%q) accepted a bad token", bad)
		}
	}
	other, err := newSealer(mathrand.NewChaCha8([32]byte{2}))
	if err != nil {
		t.Fatalf("newSealer: %v", err)
	}
	if _, openErr := other.open(token); openErr == nil {
		t.Error("a token opened under another process key")
	}
}
