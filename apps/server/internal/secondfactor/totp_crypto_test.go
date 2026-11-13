package secondfactor

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"
)

const (
	testTOTPKeyZeroText = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	testTOTPKeyOnesText = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE"
	testTOTPKeySeqText  = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8"

	testTOTPKeyZeroID = "tk1_t0FlWyjQnyTqF7sRwJ3dpg"
	testTOTPKeyOnesID = "tk1_s6jB5gOLeklelSyHRAByPA"
	testTOTPKeySeqID  = "tk1_IGfWZJe_8w6pkQMZQQ96Bg"
)

const testTOTPGoldenCiphertextHex = "7630e52ff0d3f523b471a6b982dd4d5bb4eebe04375f39d12c5db9b9778eb56734e57234"

func testTOTPKeyFill(fill func(i int) byte) TOTPKey {
	var k TOTPKey
	for i := range k {
		k[i] = fill(i)
	}
	return k
}

func sequentialReader() io.Reader {
	b := make([]byte, 256)
	for i := range b {
		b[i] = byte(i)
	}
	return bytes.NewReader(b)
}

var (
	testTOTPAccountA = uuid.MustParse("11111111-1111-1111-1111-111111111111")
	testTOTPAccountB = uuid.MustParse("33333333-3333-3333-3333-333333333333")
	testTOTPRowA     = uuid.MustParse("22222222-2222-2222-2222-222222222222")
	testTOTPRowB     = uuid.MustParse("44444444-4444-4444-4444-444444444444")
)

func TestDecodeTOTPKey_Vectors(t *testing.T) {
	cases := []struct {
		name string
		text string
		want TOTPKey
	}{
		{"zero", testTOTPKeyZeroText, testTOTPKeyFill(func(int) byte { return 0 })},
		{"ones", testTOTPKeyOnesText, testTOTPKeyFill(func(int) byte { return 1 })},
		{"seq", testTOTPKeySeqText, testTOTPKeyFill(func(i int) byte { return byte(i) })},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			key, err := DecodeTOTPKey(c.text)
			if err != nil {
				t.Fatalf("DecodeTOTPKey: %v", err)
			}
			if key != c.want {
				t.Fatalf("DecodeTOTPKey(%q) = %x, want %x", c.text, key, c.want)
			}
		})
	}
}

func TestDecodeTOTPKey_Malformed(t *testing.T) {
	cases := map[string]string{
		"empty":         "",
		"one short":     testTOTPKeySeqText[:len(testTOTPKeySeqText)-1],
		"one long":      testTOTPKeySeqText + "A",
		"padded":        testTOTPKeySeqText[:len(testTOTPKeySeqText)-1] + "=",
		"non-canonical": "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHhB",
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeTOTPKey(text); !errors.Is(err, ErrTOTPKeyRing) {
				t.Fatalf("DecodeTOTPKey(%q): err = %v, want ErrTOTPKeyRing", text, err)
			}
		})
	}
}

func TestDeriveTOTPKeyID_Vectors(t *testing.T) {
	cases := []struct {
		name string
		key  TOTPKey
		want string
	}{
		{"zero", testTOTPKeyFill(func(int) byte { return 0 }), testTOTPKeyZeroID},
		{"ones", testTOTPKeyFill(func(int) byte { return 1 }), testTOTPKeyOnesID},
		{"seq", testTOTPKeyFill(func(i int) byte { return byte(i) }), testTOTPKeySeqID},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DeriveTOTPKeyID(c.key)
			if got != c.want {
				t.Fatalf("DeriveTOTPKeyID = %q, want %q", got, c.want)
			}
			if len(got) != 26 {
				t.Fatalf("DeriveTOTPKeyID length = %d, want 26", len(got))
			}
		})
	}
}

func TestNewTOTPKeyRing_Valid(t *testing.T) {
	ring, err := NewTOTPKeyRing(testTOTPKeySeqText, "", sequentialReader())
	if err != nil {
		t.Fatalf("NewTOTPKeyRing: %v", err)
	}
	if ring.ActiveKeyID() != testTOTPKeySeqID {
		t.Fatalf("ActiveKeyID() = %q, want %q", ring.ActiveKeyID(), testTOTPKeySeqID)
	}
	if !ring.KnownKeyID(testTOTPKeySeqID) {
		t.Fatal("KnownKeyID: active key not recognized")
	}
	if ring.KnownKeyID(testTOTPKeyZeroID) {
		t.Fatal("KnownKeyID: unconfigured key reported as known")
	}

	ring, err = NewTOTPKeyRing(testTOTPKeySeqText, testTOTPKeyZeroText, sequentialReader())
	if err != nil {
		t.Fatalf("NewTOTPKeyRing with previous: %v", err)
	}
	if !ring.KnownKeyID(testTOTPKeySeqID) || !ring.KnownKeyID(testTOTPKeyZeroID) {
		t.Fatal("KnownKeyID: active and previous key must both be recognized")
	}
}

func TestNewTOTPKeyRing_Invalid(t *testing.T) {
	cases := []struct {
		name     string
		active   string
		previous string
		entropy  io.Reader
	}{
		{"missing active", "", "", sequentialReader()},
		{"malformed active", testTOTPKeySeqText[:10], "", sequentialReader()},
		{"malformed previous", testTOTPKeySeqText, testTOTPKeySeqText[:10], sequentialReader()},
		{"equal active and previous", testTOTPKeySeqText, testTOTPKeySeqText, sequentialReader()},
		{"nil entropy", testTOTPKeySeqText, "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := NewTOTPKeyRing(c.active, c.previous, c.entropy); !errors.Is(err, ErrTOTPKeyRing) {
				t.Fatalf("NewTOTPKeyRing: err = %v, want ErrTOTPKeyRing", err)
			}
		})
	}
}

func TestTOTPKeyRing_SealGoldenVector(t *testing.T) {
	ring, err := NewTOTPKeyRing(testTOTPKeySeqText, "", bytes.NewReader([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}))
	if err != nil {
		t.Fatalf("NewTOTPKeyRing: %v", err)
	}
	sealed, err := ring.Seal(TOTPRecordKindCredential, testTOTPAccountA, testTOTPRowA, rfc6238SHA1Secret)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	want, err := hex.DecodeString(testTOTPGoldenCiphertextHex)
	if err != nil {
		t.Fatalf("test fixture bug: decode golden hex: %v", err)
	}
	if !bytes.Equal(sealed.Ciphertext, want) {
		t.Fatalf("Seal ciphertext = %x, want %x", sealed.Ciphertext, want)
	}
	if sealed.KeyID != testTOTPKeySeqID {
		t.Fatalf("Seal KeyID = %q, want %q", sealed.KeyID, testTOTPKeySeqID)
	}
	if sealed.Version != totpFormatVersion {
		t.Fatalf("Seal Version = %d, want %d", sealed.Version, totpFormatVersion)
	}
	wantNonce := [totpNonceBytes]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	if sealed.Nonce != wantNonce {
		t.Fatalf("Seal Nonce = %x, want %x", sealed.Nonce, wantNonce)
	}

	got, err := ring.Open(TOTPRecordKindCredential, testTOTPAccountA, testTOTPRowA, sealed)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got != rfc6238SHA1Secret {
		t.Fatalf("Open = %x, want %x", got, rfc6238SHA1Secret)
	}
}

func TestTOTPKeyRing_OpenHostile(t *testing.T) {
	ring, err := NewTOTPKeyRing(testTOTPKeySeqText, "", sequentialReader())
	if err != nil {
		t.Fatalf("NewTOTPKeyRing: %v", err)
	}
	sealed, err := ring.Seal(TOTPRecordKindCredential, testTOTPAccountA, testTOTPRowA, rfc6238SHA1Secret)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	t.Run("unknown key", func(t *testing.T) {
		bad := sealed
		bad.KeyID = testTOTPKeyZeroID
		if _, err := ring.Open(TOTPRecordKindCredential, testTOTPAccountA, testTOTPRowA, bad); !errors.Is(err, ErrTOTPUnknownKey) {
			t.Fatalf("Open: err = %v, want ErrTOTPUnknownKey", err)
		}
	})
	t.Run("tampered ciphertext", func(t *testing.T) {
		bad := sealed
		bad.Ciphertext = append([]byte(nil), sealed.Ciphertext...)
		bad.Ciphertext[0] ^= 0xff
		if _, err := ring.Open(TOTPRecordKindCredential, testTOTPAccountA, testTOTPRowA, bad); !errors.Is(err, ErrTOTPAuthentication) {
			t.Fatalf("Open: err = %v, want ErrTOTPAuthentication", err)
		}
	})
	t.Run("tampered nonce", func(t *testing.T) {
		bad := sealed
		bad.Nonce[0] ^= 0xff
		if _, err := ring.Open(TOTPRecordKindCredential, testTOTPAccountA, testTOTPRowA, bad); !errors.Is(err, ErrTOTPAuthentication) {
			t.Fatalf("Open: err = %v, want ErrTOTPAuthentication", err)
		}
	})
	t.Run("truncated ciphertext", func(t *testing.T) {
		bad := sealed
		bad.Ciphertext = sealed.Ciphertext[:len(sealed.Ciphertext)-1]
		if _, err := ring.Open(TOTPRecordKindCredential, testTOTPAccountA, testTOTPRowA, bad); !errors.Is(err, ErrTOTPAuthentication) {
			t.Fatalf("Open: err = %v, want ErrTOTPAuthentication", err)
		}
	})
	t.Run("empty ciphertext", func(t *testing.T) {
		bad := sealed
		bad.Ciphertext = nil
		if _, err := ring.Open(TOTPRecordKindCredential, testTOTPAccountA, testTOTPRowA, bad); !errors.Is(err, ErrTOTPAuthentication) {
			t.Fatalf("Open: err = %v, want ErrTOTPAuthentication", err)
		}
	})
	t.Run("swapped account", func(t *testing.T) {
		if _, err := ring.Open(TOTPRecordKindCredential, testTOTPAccountB, testTOTPRowA, sealed); !errors.Is(err, ErrTOTPAuthentication) {
			t.Fatalf("Open: err = %v, want ErrTOTPAuthentication", err)
		}
	})
	t.Run("swapped row", func(t *testing.T) {
		if _, err := ring.Open(TOTPRecordKindCredential, testTOTPAccountA, testTOTPRowB, sealed); !errors.Is(err, ErrTOTPAuthentication) {
			t.Fatalf("Open: err = %v, want ErrTOTPAuthentication", err)
		}
	})
	t.Run("swapped record kind", func(t *testing.T) {
		if _, err := ring.Open(TOTPRecordKindEnrollment, testTOTPAccountA, testTOTPRowA, sealed); !errors.Is(err, ErrTOTPAuthentication) {
			t.Fatalf("Open: err = %v, want ErrTOTPAuthentication", err)
		}
	})
	t.Run("swapped version", func(t *testing.T) {
		bad := sealed
		bad.Version = totpFormatVersion + 1
		if _, err := ring.Open(TOTPRecordKindCredential, testTOTPAccountA, testTOTPRowA, bad); !errors.Is(err, ErrTOTPAuthentication) {
			t.Fatalf("Open: err = %v, want ErrTOTPAuthentication", err)
		}
	})
}

func TestTOTPKeyRing_Seal_NonceFailure(t *testing.T) {
	ring, ringErr := NewTOTPKeyRing(testTOTPKeySeqText, "", sequentialReader())
	if ringErr != nil {
		t.Fatalf("NewTOTPKeyRing: %v", ringErr)
	}

	empty, emptyErr := NewTOTPKeyRing(testTOTPKeySeqText, "", bytes.NewReader(nil))
	if emptyErr != nil {
		t.Fatalf("NewTOTPKeyRing: %v", emptyErr)
	}
	if _, sealErr := empty.Seal(TOTPRecordKindCredential, testTOTPAccountA, testTOTPRowA, rfc6238SHA1Secret); !errors.Is(sealErr, io.EOF) {
		t.Fatalf("Seal with empty entropy: err = %v, want io.EOF", sealErr)
	}

	short, shortErr := NewTOTPKeyRing(testTOTPKeySeqText, "", bytes.NewReader([]byte{1, 2, 3}))
	if shortErr != nil {
		t.Fatalf("NewTOTPKeyRing: %v", shortErr)
	}
	if _, sealErr := short.Seal(TOTPRecordKindCredential, testTOTPAccountA, testTOTPRowA, rfc6238SHA1Secret); !errors.Is(sealErr, io.ErrUnexpectedEOF) {
		t.Fatalf("Seal with short entropy: err = %v, want io.ErrUnexpectedEOF", sealErr)
	}

	if _, sealErr := ring.Seal(TOTPRecordKindCredential, testTOTPAccountA, testTOTPRowA, rfc6238SHA1Secret); sealErr != nil {
		t.Fatalf("Seal with working entropy: %v", sealErr)
	}
}

func TestTOTPKeyRing_ActiveVersusPrevious(t *testing.T) {
	older, err := NewTOTPKeyRing(testTOTPKeyOnesText, "", sequentialReader())
	if err != nil {
		t.Fatalf("NewTOTPKeyRing(older): %v", err)
	}
	sealedUnderOld, err := older.Seal(TOTPRecordKindCredential, testTOTPAccountA, testTOTPRowA, rfc6238SHA1Secret)
	if err != nil {
		t.Fatalf("Seal under older key: %v", err)
	}
	if sealedUnderOld.KeyID != testTOTPKeyOnesID {
		t.Fatalf("Seal KeyID = %q, want %q", sealedUnderOld.KeyID, testTOTPKeyOnesID)
	}

	rotated, err := NewTOTPKeyRing(testTOTPKeySeqText, testTOTPKeyOnesText, sequentialReader())
	if err != nil {
		t.Fatalf("NewTOTPKeyRing(rotated): %v", err)
	}
	if rotated.ActiveKeyID() == sealedUnderOld.KeyID {
		t.Fatal("test fixture bug: rotated ring's active key must differ from the older sealed key")
	}

	got, err := rotated.Open(TOTPRecordKindCredential, testTOTPAccountA, testTOTPRowA, sealedUnderOld)
	if err != nil {
		t.Fatalf("Open a value sealed under the now-previous key: %v", err)
	}
	if got != rfc6238SHA1Secret {
		t.Fatalf("Open = %x, want %x", got, rfc6238SHA1Secret)
	}

	sealedUnderNew, err := rotated.Seal(TOTPRecordKindCredential, testTOTPAccountA, testTOTPRowA, rfc6238SHA1Secret)
	if err != nil {
		t.Fatalf("Seal under rotated ring: %v", err)
	}
	if sealedUnderNew.KeyID != rotated.ActiveKeyID() {
		t.Fatalf("Seal KeyID = %q, want the active key %q, not the previous key", sealedUnderNew.KeyID, rotated.ActiveKeyID())
	}
}
