package oauthsrv

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestNewCodeSpelling(t *testing.T) {
	t.Parallel()

	// The reader holds 40 bytes so the test can prove NewCode consumes
	// exactly 32 of them.
	r := primEntropy(0x3c, 40)
	raw, digest, err := NewCode(r)
	if err != nil {
		t.Fatalf("NewCode error = %v, want nil", err)
	}
	if r.Len() != 8 {
		t.Errorf("entropy bytes consumed = %d, want 32", 40-r.Len())
	}
	if got := len(raw); got != 43 {
		t.Errorf("len(raw) = %d, want 43 (bare, unprefixed)", got)
	}
	if want := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x3c}, 32)); raw != want {
		t.Error("code is not the unpadded base64url of the 32 entropy bytes")
	}
	if want := sha256.Sum256([]byte(raw)); digest != want {
		t.Error("digest is not the SHA-256 of the canonical raw spelling")
	}
}

func TestNewCodeShortEntropyFailsClosed(t *testing.T) {
	t.Parallel()

	for _, n := range []int{0, 31} {
		raw, digest, err := NewCode(primEntropy(0x11, n))
		if err == nil {
			t.Fatalf("NewCode(%d entropy bytes) error = nil, want a short-read error", n)
		}
		if !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			t.Errorf("NewCode(%d entropy bytes) error = %v, want an io short-read error", n, err)
		}
		if raw != "" || digest != ([32]byte{}) {
			t.Errorf("NewCode(%d entropy bytes) returned a code, want none", n)
		}
	}
}

func TestParseCodeRoundTrip(t *testing.T) {
	t.Parallel()

	raw, digest, err := NewCode(primEntropy(0x6d, 32))
	if err != nil {
		t.Fatalf("NewCode error = %v, want nil", err)
	}
	got, err := ParseCode(raw)
	if err != nil {
		t.Fatalf("ParseCode error = %v, want nil", err)
	}
	if got != digest {
		t.Error("ParseCode digest differs from the digest NewCode returned")
	}
}

func TestParseCodeRejectsMalformedSpellings(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("A", 43)
	cases := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"one character short", strings.Repeat("A", 42)},
		{"one character long", strings.Repeat("A", 44)},
		{"padded", strings.Repeat("A", 42) + "="},
		{"padded to 44", strings.Repeat("A", 43) + "="},
		{"standard base64 plus", "+" + strings.Repeat("A", 42)},
		{"standard base64 slash", "/" + strings.Repeat("A", 42)},
		{"non-canonical trailing bits", strings.Repeat("A", 42) + "B"},
		{"access token spelling", "amat_" + body},
		{"refresh token spelling", "amrt_" + body},
		{"leading space", " " + strings.Repeat("A", 42)},
		{"trailing newline", strings.Repeat("A", 42) + "\n"},
		{"non-ascii", strings.Repeat("A", 41) + "\u00e9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			digest, err := ParseCode(tc.raw)
			if !errors.Is(err, ErrCodeInvalid) {
				t.Errorf("ParseCode error = %v, want ErrCodeInvalid", err)
			}
			if digest != ([32]byte{}) {
				t.Error("ParseCode returned a digest for a malformed spelling")
			}
		})
	}
}

func TestParseCodeDigestIsStable(t *testing.T) {
	t.Parallel()

	// Locks the stored-digest definition: SHA-256 over the canonical raw
	// spelling. 32 zero bytes encode as 43 'A' characters.
	digest, err := ParseCode(strings.Repeat("A", 43))
	if err != nil {
		t.Fatalf("ParseCode error = %v, want nil", err)
	}
	const want = "0f007385b6f9d4b7eeb2748605afe1a984a0a3bfa3f014d09e2a784ce9e5cd1a"
	if got := fmt.Sprintf("%x", digest); got != want {
		t.Errorf("digest = %s, want %s", got, want)
	}
}

func TestCodeAndTokenDigestsAreDomainSeparated(t *testing.T) {
	t.Parallel()

	code, codeDigest, err := NewCode(primEntropy(0x5a, 32))
	if err != nil {
		t.Fatalf("NewCode error = %v, want nil", err)
	}
	_, tokenDigest, err := ParseToken("amat_" + code)
	if err != nil {
		t.Fatalf("ParseToken error = %v, want nil", err)
	}
	if codeDigest == tokenDigest {
		t.Error("a code and a token built from the same entropy share a digest")
	}
}

func TestCodeErrorsCarryNoInput(t *testing.T) {
	t.Parallel()

	if _, err := ParseCode(oauthPrimSentinel); !errors.Is(err, ErrCodeInvalid) {
		t.Fatalf("ParseCode error = %v, want ErrCodeInvalid", err)
	} else if strings.Contains(err.Error(), oauthPrimSentinel) {
		t.Error("ParseCode error text echoes the presented code")
	}
	if got := ErrCodeInvalid.Error(); got != "oauth code invalid" {
		t.Errorf("ErrCodeInvalid.Error() = %q, want %q", got, "oauth code invalid")
	}
}
