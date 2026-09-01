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

// oauthPrimSentinel is the marker embedded in malformed inputs across this
// package's tests. No error, panic, or test log may echo it back.
const oauthPrimSentinel = "S3NT1NEL-RAW-VALUE"

// primEntropy returns a reader over n copies of fill so every generated
// spelling in these tests is deterministic.
func primEntropy(fill byte, n int) *bytes.Reader {
	return bytes.NewReader(bytes.Repeat([]byte{fill}, n))
}

func TestNewTokenSpelling(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		kind   TokenKind
		prefix string
	}{
		{"access", TokenKindAccess, "amat_"},
		{"refresh", TokenKindRefresh, "amrt_"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// The reader holds 40 bytes so the test can prove NewToken
			// consumes exactly 32 of them.
			r := primEntropy(0x42, 40)
			raw, digest, err := NewToken(tc.kind, r)
			if err != nil {
				t.Fatalf("NewToken error = %v, want nil", err)
			}
			if r.Len() != 8 {
				t.Errorf("entropy bytes consumed = %d, want 32", 40-r.Len())
			}
			if got := len(raw); got != 48 {
				t.Errorf("len(raw) = %d, want 48 (5-byte prefix + 43-character body)", got)
			}
			if !strings.HasPrefix(raw, tc.prefix) {
				t.Errorf("raw prefix = %q, want %q", raw[:min(len(raw), 5)], tc.prefix)
			}
			wantBody := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x42}, 32))
			if got := raw[len(tc.prefix):]; got != wantBody {
				t.Error("token body is not the unpadded base64url of the 32 entropy bytes")
			}
			if want := sha256.Sum256([]byte(raw)); digest != want {
				t.Error("digest is not the SHA-256 of the canonical raw spelling")
			}
		})
	}
}

func TestNewTokenShortEntropyFailsClosed(t *testing.T) {
	t.Parallel()

	for _, n := range []int{0, 31} {
		raw, digest, err := NewToken(TokenKindAccess, primEntropy(0x11, n))
		if err == nil {
			t.Fatalf("NewToken(%d entropy bytes) error = nil, want a short-read error", n)
		}
		if !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			t.Errorf("NewToken(%d entropy bytes) error = %v, want an io short-read error", n, err)
		}
		if raw != "" {
			t.Errorf("NewToken(%d entropy bytes) returned a token spelling, want empty", n)
		}
		if digest != ([32]byte{}) {
			t.Errorf("NewToken(%d entropy bytes) returned a non-zero digest, want zero", n)
		}
	}
}

func TestNewTokenRejectsUnknownKindBeforeReadingEntropy(t *testing.T) {
	t.Parallel()

	for _, kind := range []TokenKind{"", "bearer", "Access", "access "} {
		r := primEntropy(0x22, 32)
		raw, digest, err := NewToken(kind, r)
		if !errors.Is(err, ErrTokenInvalid) {
			t.Errorf("NewToken(kind %q) error = %v, want ErrTokenInvalid", kind, err)
		}
		if raw != "" || digest != ([32]byte{}) {
			t.Errorf("NewToken(kind %q) returned a token, want none", kind)
		}
		if r.Len() != 32 {
			t.Errorf("NewToken(kind %q) consumed entropy before the kind check", kind)
		}
	}
}

func TestParseTokenRoundTrip(t *testing.T) {
	t.Parallel()

	for _, kind := range []TokenKind{TokenKindAccess, TokenKindRefresh} {
		raw, digest, err := NewToken(kind, primEntropy(0x7f, 32))
		if err != nil {
			t.Fatalf("NewToken error = %v, want nil", err)
		}
		gotKind, gotDigest, err := ParseToken(raw)
		if err != nil {
			t.Fatalf("ParseToken error = %v, want nil", err)
		}
		if gotKind != kind {
			t.Errorf("ParseToken kind = %q, want %q", gotKind, kind)
		}
		if gotDigest != digest {
			t.Error("ParseToken digest differs from the digest NewToken returned")
		}
	}
}

func TestParseTokenKindsAreDomainSeparated(t *testing.T) {
	t.Parallel()

	access, accessDigest, err := NewToken(TokenKindAccess, primEntropy(0x5a, 32))
	if err != nil {
		t.Fatalf("NewToken error = %v, want nil", err)
	}
	// The same 32 entropy bytes spelled as the other kind must not collide
	// with the access digest, so a refresh spelling can never match a stored
	// access row.
	refresh := "amrt_" + access[5:]
	kind, refreshDigest, err := ParseToken(refresh)
	if err != nil {
		t.Fatalf("ParseToken error = %v, want nil", err)
	}
	if kind != TokenKindRefresh {
		t.Errorf("ParseToken kind = %q, want %q", kind, TokenKindRefresh)
	}
	if refreshDigest == accessDigest {
		t.Error("access and refresh spellings of the same entropy share a digest")
	}
}

func TestParseTokenRejectsMalformedSpellings(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("A", 43)
	cases := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"prefix only", "amat_"},
		{"body only", body},
		{"one character short", "amat_" + strings.Repeat("A", 42)},
		{"one character long", "amat_" + strings.Repeat("A", 44)},
		{"padded body", "amat_" + strings.Repeat("A", 42) + "="},
		{"standard base64 plus", "amat_+" + strings.Repeat("A", 42)},
		{"standard base64 slash", "amat_/" + strings.Repeat("A", 42)},
		{"non-canonical trailing bits", "amat_" + strings.Repeat("A", 42) + "B"},
		{"unknown prefix", "amxx_" + body},
		{"uppercase prefix", "AMAT_" + body},
		{"missing underscore", "amat" + body + "A"},
		{"code prefix of another scheme", "amat-" + body},
		{"leading space", " amat" + body},
		{"trailing newline", "amat_" + body[:42] + "\n"},
		{"non-ascii body", "amat_" + strings.Repeat("A", 41) + "\u00e9"},
		{"bearer header form", "Bearer amat_" + body},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			kind, digest, err := ParseToken(tc.raw)
			if !errors.Is(err, ErrTokenInvalid) {
				t.Errorf("ParseToken error = %v, want ErrTokenInvalid", err)
			}
			if kind != "" || digest != ([32]byte{}) {
				t.Error("ParseToken returned a kind or digest for a malformed spelling")
			}
		})
	}
}

func TestParseTokenDigestIsStable(t *testing.T) {
	t.Parallel()

	// Locks the stored-digest definition: SHA-256 over the canonical raw
	// spelling, prefix included. 32 zero bytes encode as 43 'A' characters.
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			"access",
			"amat_" + strings.Repeat("A", 43),
			"1d27fc505e3fa27a392a20d2fc4865f9d58741cced8059c6c07a8a1965ba4112",
		},
		{
			"refresh",
			"amrt_" + strings.Repeat("A", 43),
			"d875937565c36633464eab77f21696871711515e8bf957ecdb6246135ecb8c27",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, digest, err := ParseToken(tc.raw)
			if err != nil {
				t.Fatalf("ParseToken error = %v, want nil", err)
			}
			if got := fmt.Sprintf("%x", digest); got != tc.want {
				t.Errorf("digest = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestTokenErrorsCarryNoInput(t *testing.T) {
	t.Parallel()

	if _, _, err := ParseToken("amat_" + oauthPrimSentinel); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("ParseToken error = %v, want ErrTokenInvalid", err)
	} else if strings.Contains(err.Error(), oauthPrimSentinel) {
		t.Error("ParseToken error text echoes the presented token")
	}
	if _, _, err := NewToken(TokenKind(oauthPrimSentinel), primEntropy(0x01, 32)); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("NewToken error = %v, want ErrTokenInvalid", err)
	} else if strings.Contains(err.Error(), oauthPrimSentinel) {
		t.Error("NewToken error text echoes the requested kind")
	}
	if got := ErrTokenInvalid.Error(); got != "oauth token invalid" {
		t.Errorf("ErrTokenInvalid.Error() = %q, want %q", got, "oauth token invalid")
	}
}
