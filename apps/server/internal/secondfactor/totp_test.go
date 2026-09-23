package secondfactor

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"
)

// rfc6238SHA1Secret is the 20-ASCII-byte secret RFC 6238 Appendix B uses for
// every SHA-1 test vector.
var rfc6238SHA1Secret = mustTOTPSecret([]byte("12345678901234567890"))

// zeroTOTPSecret is a deterministic 20-byte secret used for vectors that do
// not need a published reference value, such as duplicate-match and
// step-boundary behavior.
var zeroTOTPSecret TOTPSecret

func mustTOTPSecret(b []byte) TOTPSecret {
	var s TOTPSecret
	if len(b) != totpSecretBytes {
		panic("secondfactor: test secret is not 20 bytes")
	}
	copy(s[:], b)
	return s
}

// TestVerifyTOTPCode_RFC6238Vectors proves the HMAC-SHA-1 profile against
// every SHA-1 row of RFC 6238 Appendix B, truncated to the accepted six
// digits (AC-AUTH-026). The RFC's own 8-digit values mod 10^6 equal our
// 6-digit values, since truncation only changes how many trailing digits are
// kept.
func TestVerifyTOTPCode_RFC6238Vectors(t *testing.T) {
	cases := []struct {
		name string
		unix int64
		code string
	}{
		{"1970-01-01T00:00:59Z", 59, "287082"},
		{"2005-03-18T01:58:29Z", 1111111109, "081804"},
		{"2005-03-18T01:58:31Z", 1111111111, "050471"},
		// This vector's code has two leading zeroes, proving the truncated
		// value is zero-padded to six digits rather than left short.
		{"2009-02-13T23:31:30Z", 1234567890, "005924"},
		{"2033-05-18T03:33:20Z", 2000000000, "279037"},
		{"2603-10-11T11:33:20Z", 20000000000, "353130"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			now := time.Unix(c.unix, 0).UTC()
			step, matched, err := VerifyTOTPCode(rfc6238SHA1Secret, c.code, now)
			if err != nil {
				t.Fatalf("VerifyTOTPCode: %v", err)
			}
			if !matched {
				t.Fatalf("VerifyTOTPCode: code %q did not match at %v", c.code, now)
			}
			wantStep := uint64(c.unix) / totpPeriodSeconds
			if step != wantStep {
				t.Fatalf("step = %d, want %d", step, wantStep)
			}
		})
	}
}

// TestVerifyTOTPCode_StepBoundary proves the previous, current, and next
// windows at the first period after the epoch, and that no wraparound
// "previous" step (computed as an unsigned underflow) is ever tried when the
// current step is zero.
func TestVerifyTOTPCode_StepBoundary(t *testing.T) {
	now := time.Unix(15, 0).UTC() // current step 0: unix in [0, 30)

	step, matched, err := VerifyTOTPCode(zeroTOTPSecret, "328482", now) // step 0
	if err != nil || !matched || step != 0 {
		t.Fatalf("step 0: step=%d matched=%v err=%v", step, matched, err)
	}
	step, matched, err = VerifyTOTPCode(zeroTOTPSecret, "812658", now) // step 1 (next)
	if err != nil || !matched || step != 1 {
		t.Fatalf("step 1: step=%d matched=%v err=%v", step, matched, err)
	}
	// This code only matches the wraparound counter 2^64-1, the value an
	// unguarded "current-1" computation would produce at step 0. It must
	// never match.
	_, matched, err = VerifyTOTPCode(zeroTOTPSecret, "566304", now)
	if err != nil {
		t.Fatalf("wraparound probe: %v", err)
	}
	if matched {
		t.Fatal("wraparound probe: matched a step that must never be tried at the epoch's first period")
	}
}

// TestVerifyTOTPCode_DuplicateMatches proves that when the same code is valid
// for two candidate steps, VerifyTOTPCode returns the greater one. Steps
// 418590 and 418591 both produce code "903848" under the all-zero secret.
func TestVerifyTOTPCode_DuplicateMatches(t *testing.T) {
	now := time.Unix(418591*totpPeriodSeconds, 0).UTC() // current step 418591
	step, matched, err := VerifyTOTPCode(zeroTOTPSecret, "903848", now)
	if err != nil {
		t.Fatalf("VerifyTOTPCode: %v", err)
	}
	if !matched {
		t.Fatal("VerifyTOTPCode: expected a match across the duplicate steps")
	}
	if step != 418591 {
		t.Fatalf("step = %d, want the greater duplicate step 418591", step)
	}
}

// TestVerifyTOTPCode_NegativeTime proves a clock reading before the Unix
// epoch is rejected as ErrTOTPNegativeTime rather than silently wrapping.
func TestVerifyTOTPCode_NegativeTime(t *testing.T) {
	cases := []time.Time{
		time.Unix(-1, 0).UTC(),
		{}, // the zero value is deep in year 1, far before the epoch
	}
	for _, now := range cases {
		_, _, err := VerifyTOTPCode(zeroTOTPSecret, "328482", now)
		if !errors.Is(err, ErrTOTPNegativeTime) {
			t.Fatalf("VerifyTOTPCode(%v): err = %v, want ErrTOTPNegativeTime", now, err)
		}
	}
}

// TestVerifyTOTPCode_MalformedCode proves every non-canonical code shape
// fails as ErrTOTPMalformedCode before any cryptographic work, including
// ahead of a negative-time input ("TOTP profile and code verification":
// "Input is exactly six ASCII digits").
func TestVerifyTOTPCode_MalformedCode(t *testing.T) {
	cases := map[string]string{
		"empty":            "",
		"five digits":      "12345",
		"seven digits":     "1234567",
		"interior space":   "12 345",
		"interior hyphen":  "123-456",
		"leading sign":     "-12345",
		"fullwidth digits": "１２３４５６", // six fullwidth "123456"
		"arabic-indic":     "١٢٣٤٥٦", // six Arabic-Indic digits
	}
	for name, code := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := VerifyTOTPCode(zeroTOTPSecret, code, time.Unix(-1, 0))
			if !errors.Is(err, ErrTOTPMalformedCode) {
				t.Fatalf("VerifyTOTPCode(%q): err = %v, want ErrTOTPMalformedCode even with a negative clock", code, err)
			}
		})
	}
}

// TestTOTPSecret_Base32AndGroupedDisplay checks the canonical Base32 and
// grouped-display encodings against the exact example
// docs/design/totp-second-factor-contract.md "Provisioning data" shows.
func TestTOTPSecret_Base32AndGroupedDisplay(t *testing.T) {
	secret := docExampleTOTPSecret(t)
	const wantBase32 = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"
	const wantGrouped = "ABCD EFGH IJKL MNOP QRST UVWX YZ23 4567"
	if got := secret.Base32(); got != wantBase32 {
		t.Fatalf("Base32() = %q, want %q", got, wantBase32)
	}
	if got := secret.GroupedDisplay(); got != wantGrouped {
		t.Fatalf("GroupedDisplay() = %q, want %q", got, wantGrouped)
	}
	if n := len(wantGrouped); n != 39 {
		t.Fatalf("test fixture bug: grouped display is %d bytes, want 39", n)
	}
}

// docExampleTOTPSecret decodes the 20-byte secret behind the contract's
// documented Base32 example (the RFC 4648 Base32 alphabet itself, so its
// bytes have no independent published source beyond that round trip).
func docExampleTOTPSecret(t *testing.T) TOTPSecret {
	t.Helper()
	raw, err := totpBase32.DecodeString("ABCDEFGHIJKLMNOPQRSTUVWXYZ234567")
	if err != nil || len(raw) != totpSecretBytes {
		t.Fatalf("test fixture bug: decode doc example secret: %v", err)
	}
	return mustTOTPSecret(raw)
}

// TestGenerateTOTPSecret proves the secret is copied verbatim from entropy
// and that a short entropy source fails instead of returning a short or
// zero-padded secret.
func TestGenerateTOTPSecret(t *testing.T) {
	want := make([]byte, totpSecretBytes)
	for i := range want {
		want[i] = byte(i + 1)
	}
	secret, err := GenerateTOTPSecret(bytes.NewReader(want))
	if err != nil {
		t.Fatalf("GenerateTOTPSecret: %v", err)
	}
	if !bytes.Equal(secret[:], want) {
		t.Fatalf("GenerateTOTPSecret = %x, want %x", secret[:], want)
	}

	if _, err := GenerateTOTPSecret(bytes.NewReader(want[:totpSecretBytes-1])); err == nil {
		t.Fatal("GenerateTOTPSecret: want an error from a short entropy source")
	}
	if _, err := GenerateTOTPSecret(io.LimitReader(bytes.NewReader(nil), 0)); err == nil {
		t.Fatal("GenerateTOTPSecret: want an error from an empty entropy source")
	}
}
