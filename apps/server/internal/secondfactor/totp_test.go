package secondfactor

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"
)

var rfc6238SHA1Secret = mustTOTPSecret([]byte("12345678901234567890"))

var zeroTOTPSecret TOTPSecret

func mustTOTPSecret(b []byte) TOTPSecret {
	var s TOTPSecret
	if len(b) != totpSecretBytes {
		panic("secondfactor: test secret is not 20 bytes")
	}
	copy(s[:], b)
	return s
}

func TestVerifyTOTPCode_RFC6238Vectors(t *testing.T) {
	cases := []struct {
		name string
		unix int64
		code string
	}{
		{"1970-01-01T00:00:59Z", 59, "287082"},
		{"2005-03-18T01:58:29Z", 1111111109, "081804"},
		{"2005-03-18T01:58:31Z", 1111111111, "050471"},
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

func TestVerifyTOTPCode_StepBoundary(t *testing.T) {
	now := time.Unix(15, 0).UTC()

	step, matched, err := VerifyTOTPCode(zeroTOTPSecret, "328482", now)
	if err != nil || !matched || step != 0 {
		t.Fatalf("step 0: step=%d matched=%v err=%v", step, matched, err)
	}
	step, matched, err = VerifyTOTPCode(zeroTOTPSecret, "812658", now)
	if err != nil || !matched || step != 1 {
		t.Fatalf("step 1: step=%d matched=%v err=%v", step, matched, err)
	}
	_, matched, err = VerifyTOTPCode(zeroTOTPSecret, "566304", now)
	if err != nil {
		t.Fatalf("wraparound probe: %v", err)
	}
	if matched {
		t.Fatal("wraparound probe: matched a step that must never be tried at the epoch's first period")
	}
}

func TestVerifyTOTPCode_PreviousStepWindow(t *testing.T) {
	now := time.Unix(1111111109, 0).UTC()

	step, matched, err := VerifyTOTPCode(rfc6238SHA1Secret, "731029", now)
	if err != nil || !matched || step != 37037035 {
		t.Fatalf("previous step: step=%d matched=%v err=%v, want step 37037035 matched", step, matched, err)
	}
	_, matched, err = VerifyTOTPCode(rfc6238SHA1Secret, "150727", now)
	if err != nil {
		t.Fatalf("two steps back: %v", err)
	}
	if matched {
		t.Fatal("two steps back: a code outside the previous, current, and next window matched")
	}
}

func TestVerifyTOTPCode_DuplicateMatches(t *testing.T) {
	now := time.Unix(418591*totpPeriodSeconds, 0).UTC()
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

func TestVerifyTOTPCode_NegativeTime(t *testing.T) {
	cases := []time.Time{
		time.Unix(-1, 0).UTC(),
		{},
	}
	for _, now := range cases {
		_, _, err := VerifyTOTPCode(zeroTOTPSecret, "328482", now)
		if !errors.Is(err, ErrTOTPNegativeTime) {
			t.Fatalf("VerifyTOTPCode(%v): err = %v, want ErrTOTPNegativeTime", now, err)
		}
	}
}

func TestVerifyTOTPCode_MalformedCode(t *testing.T) {
	cases := map[string]string{
		"empty":            "",
		"five digits":      "12345",
		"seven digits":     "1234567",
		"interior space":   "12 345",
		"interior hyphen":  "123-456",
		"leading sign":     "-12345",
		"fullwidth digits": "１２３４５６",
		"arabic-indic":     "١٢٣٤٥٦",
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

func docExampleTOTPSecret(t *testing.T) TOTPSecret {
	t.Helper()
	raw, err := totpBase32.DecodeString("ABCDEFGHIJKLMNOPQRSTUVWXYZ234567")
	if err != nil || len(raw) != totpSecretBytes {
		t.Fatalf("test fixture bug: decode doc example secret: %v", err)
	}
	return mustTOTPSecret(raw)
}

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
