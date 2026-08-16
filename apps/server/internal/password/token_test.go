package password

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
)

func TestNewTokenUsesExactly32Bytes(t *testing.T) {
	t.Parallel()
	entropy := bytes.Repeat([]byte{0x42}, 32)
	tok, err := NewToken(bytes.NewReader(entropy))
	if err != nil {
		t.Fatalf("NewToken error = %v", err)
	}
	if len(tok.Raw) != 43 {
		t.Errorf("Raw length = %d, want 43", len(tok.Raw))
	}
	wantDigest := sha256.Sum256(entropy)
	if tok.Digest != wantDigest {
		t.Errorf("Digest = %x, want %x (SHA-256 of the 32 random bytes)", tok.Digest, wantDigest)
	}
}

func TestNewTokenShortEntropyFails(t *testing.T) {
	t.Parallel()
	_, err := NewToken(bytes.NewReader(make([]byte, 31)))
	if err == nil {
		t.Fatal("NewToken error = nil, want short-read error")
	}
}

func TestTokenRoundTripDigest(t *testing.T) {
	t.Parallel()
	tok, err := NewToken(bytes.NewReader(bytes.Repeat([]byte{0x24}, 32)))
	if err != nil {
		t.Fatalf("NewToken error = %v", err)
	}
	digest, err := DigestToken(tok.Raw)
	if err != nil {
		t.Fatalf("DigestToken error = %v", err)
	}
	if digest != tok.Digest {
		t.Errorf("DigestToken = %x, want %x", digest, tok.Digest)
	}
}

func TestDigestTokenKnownSpelling(t *testing.T) {
	t.Parallel()
	// 32 zero bytes encode as 43 unpadded base64url 'A' characters.
	raw := strings.Repeat("A", 43)
	digest, err := DigestToken(raw)
	if err != nil {
		t.Fatalf("DigestToken error = %v", err)
	}
	if want := sha256.Sum256(make([]byte, 32)); digest != want {
		t.Errorf("DigestToken = %x, want %x", digest, want)
	}
}

func TestDigestTokenRejectsMalformed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"too short", strings.Repeat("A", 42)},
		{"too long", strings.Repeat("A", 44)},
		{"padding", strings.Repeat("A", 42) + "="},
		{"standard base64 plus", "+" + strings.Repeat("A", 42)},
		{"standard base64 slash", "/" + strings.Repeat("A", 42)},
		{"noncanonical trailing bits", strings.Repeat("A", 42) + "B"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := DigestToken(tc.raw); !errors.Is(err, ErrTokenInvalid) {
				t.Errorf("DigestToken(%q) error = %v, want ErrTokenInvalid", tc.raw, err)
			}
		})
	}
}

func TestEqualDigestConstantTime(t *testing.T) {
	t.Parallel()
	a := sha256.Sum256([]byte("a"))
	b := sha256.Sum256([]byte("a"))
	c := sha256.Sum256([]byte("b"))
	if !EqualDigest(a, b) {
		t.Error("EqualDigest(a, b) = false, want true for equal digests")
	}
	if EqualDigest(a, c) {
		t.Error("EqualDigest(a, c) = true, want false for distinct digests")
	}
}
