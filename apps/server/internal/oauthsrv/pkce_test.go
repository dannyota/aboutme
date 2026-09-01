package oauthsrv

import (
	"crypto/sha256"
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

// RFC 7636 Appendix B ("Example for the S256 code_challenge_method"): the
// verifier octets encode to rfc7636Verifier and their SHA-256 digest encodes
// to rfc7636Challenge.
const (
	rfc7636Verifier  = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	rfc7636Challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
)

// pkceChallengeFor returns the S256 challenge string for verifier, computed
// independently of the implementation under test.
func pkceChallengeFor(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestVerifyS256RFC7636AppendixBVector(t *testing.T) {
	t.Parallel()

	if pkceChallengeFor(rfc7636Verifier) != rfc7636Challenge {
		t.Fatal("the Appendix B fixture is inconsistent: the derived challenge does not match the constant")
	}
	if !VerifyS256(rfc7636Challenge, rfc7636Verifier) {
		t.Error("VerifyS256 rejected the RFC 7636 Appendix B vector")
	}
}

func TestVerifyS256VerifierLengthBounds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		length int
		want   bool
	}{
		{"42 characters", 42, false},
		{"43 characters", 43, true},
		{"64 characters", 64, true},
		{"128 characters", 128, true},
		{"129 characters", 129, false},
		{"empty", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			verifier := strings.Repeat("a", tc.length)
			if got := VerifyS256(pkceChallengeFor(verifier), verifier); got != tc.want {
				t.Errorf("VerifyS256 with a %d-character verifier = %v, want %v", tc.length, got, tc.want)
			}
		})
	}
}

func TestVerifyS256VerifierAlphabet(t *testing.T) {
	t.Parallel()

	// Every case carries the challenge that matches its own verifier, so a
	// false result can only come from the RFC 7636 alphabet rule.
	cases := []struct {
		name     string
		verifier string
		want     bool
	}{
		{"unreserved alphabet", strings.Repeat("aZ9-._~", 6) + "a", true},
		{"plus", strings.Repeat("a", 42) + "+", false},
		{"slash", strings.Repeat("a", 42) + "/", false},
		{"equals", strings.Repeat("a", 42) + "=", false},
		{"space", strings.Repeat("a", 42) + " ", false},
		{"percent", strings.Repeat("a", 42) + "%", false},
		{"colon", strings.Repeat("a", 42) + ":", false},
		{"newline", strings.Repeat("a", 42) + "\n", false},
		{"null byte", strings.Repeat("a", 42) + "\x00", false},
		{"non-ascii", strings.Repeat("a", 41) + "\u00e9", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := len(tc.verifier); got < 43 || got > 128 {
				t.Fatalf("case verifier length = %d, want within 43..128 so length is not the reason", got)
			}
			if got := VerifyS256(pkceChallengeFor(tc.verifier), tc.verifier); got != tc.want {
				t.Errorf("VerifyS256 = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestVerifyS256ChallengeShape(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		challenge string
	}{
		{"empty", ""},
		{"one character short", rfc7636Challenge[:42]},
		{"one character long", rfc7636Challenge + "A"},
		{"padded", rfc7636Challenge[:42] + "="},
		{"standard base64 plus", "+" + rfc7636Challenge[1:]},
		{"standard base64 slash", "/" + rfc7636Challenge[1:]},
		// 'M' and 'N' differ only in the two bits past the 256th; a
		// non-strict decoder would accept both spellings of one digest.
		{"non-canonical trailing bits", rfc7636Challenge[:42] + "N"},
		{"verifier-shaped text", strings.Repeat("a", 43)},
		{"outside the base64url alphabet", strings.Repeat("a", 42) + "~"},
		{"leading space", " " + rfc7636Challenge[1:]},
		{"wrong digest", pkceChallengeFor("some-other-verifier-entirely-and-then-some")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if VerifyS256(tc.challenge, rfc7636Verifier) {
				t.Error("VerifyS256 accepted a challenge it must reject")
			}
		})
	}
}

func TestVerifyS256RejectsMismatchAtEitherEnd(t *testing.T) {
	t.Parallel()

	verifier := strings.Repeat("b", 64)
	challenge := pkceChallengeFor(verifier)
	if !VerifyS256(challenge, verifier) {
		t.Fatal("VerifyS256 rejected a self-consistent pair")
	}
	// Flip the first and the last decoded byte in turn; neither may pass.
	// Both replacement characters keep the spelling canonical, so rejection
	// comes from the digest mismatch alone.
	flipFirst := "B" + challenge[1:]
	lastChar := "Q"
	if strings.HasSuffix(challenge, lastChar) {
		lastChar = "A"
	}
	flipLast := challenge[:42] + lastChar
	if flipFirst == challenge || flipLast == challenge {
		t.Fatal("mutation produced the original challenge")
	}
	if VerifyS256(flipFirst, verifier) {
		t.Error("VerifyS256 accepted a challenge differing in its first byte")
	}
	if VerifyS256(flipLast, verifier) {
		t.Error("VerifyS256 accepted a challenge differing in its last byte")
	}
}

func TestVerifyS256UsesConstantTimeCompare(t *testing.T) {
	t.Parallel()

	// M4 requires the digest comparison to be constant time. Assert the
	// comparator in the source so a later edit cannot silently swap it for
	// bytes.Equal or ==.
	src, err := os.ReadFile("pkce.go")
	if err != nil {
		t.Fatalf("read pkce.go: %v", err)
	}
	if !strings.Contains(string(src), "subtle.ConstantTimeCompare") {
		t.Error("pkce.go does not compare digests with subtle.ConstantTimeCompare")
	}
}

func TestVerifyS256NeverPanicsOnHostileInput(t *testing.T) {
	t.Parallel()

	hostile := []string{"", oauthPrimSentinel, strings.Repeat("\x00", 200), strings.Repeat("\u00e9", 200), "\ufeff"}
	for _, challenge := range hostile {
		for _, verifier := range hostile {
			if VerifyS256(challenge, verifier) {
				t.Error("VerifyS256 accepted hostile input")
			}
		}
	}
}
