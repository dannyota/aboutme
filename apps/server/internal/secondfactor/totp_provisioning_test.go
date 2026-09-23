package secondfactor

import (
	"errors"
	"strings"
	"testing"
)

// TestBuildTOTPProvisioningURI_ContractExample reproduces the exact
// provisioning URI docs/design/totp-second-factor-contract.md "Provisioning
// data" shows for issuer "aboutme.vn" and email "user@example.com".
func TestBuildTOTPProvisioningURI_ContractExample(t *testing.T) {
	secret := docExampleTOTPSecret(t)
	const want = "otpauth://totp/aboutme.vn:user%40example.com?secret=ABCDEFGHIJKLMNOPQRSTUVWXYZ234567&issuer=aboutme.vn&algorithm=SHA1&digits=6&period=30"
	got, err := BuildTOTPProvisioningURI("aboutme.vn", "user@example.com", secret)
	if err != nil {
		t.Fatalf("BuildTOTPProvisioningURI: %v", err)
	}
	if got != want {
		t.Fatalf("BuildTOTPProvisioningURI = %q, want %q", got, want)
	}
	if len(got) > totpMaxProvisioningURIBytes {
		t.Fatalf("URI is %d bytes, want at most %d", len(got), totpMaxProvisioningURIBytes)
	}
}

// TestBuildTOTPProvisioningURI_Localhost proves the bare single-label
// "localhost" issuer local HTTPS uses is accepted.
func TestBuildTOTPProvisioningURI_Localhost(t *testing.T) {
	secret := docExampleTOTPSecret(t)
	got, err := BuildTOTPProvisioningURI("localhost", "someone@example.com", secret)
	if err != nil {
		t.Fatalf("BuildTOTPProvisioningURI: %v", err)
	}
	if !strings.HasPrefix(got, "otpauth://totp/localhost:") {
		t.Fatalf("BuildTOTPProvisioningURI = %q, want the localhost label", got)
	}
}

// TestBuildTOTPProvisioningURI_SpaceEncoding proves a space in the email
// encodes as %20, never as +.
func TestBuildTOTPProvisioningURI_SpaceEncoding(t *testing.T) {
	secret := docExampleTOTPSecret(t)
	got, err := BuildTOTPProvisioningURI("aboutme.vn", "a b@example.com", secret)
	if err != nil {
		t.Fatalf("BuildTOTPProvisioningURI: %v", err)
	}
	if !strings.Contains(got, "a%20b%40example.com") {
		t.Fatalf("BuildTOTPProvisioningURI = %q, want a %%20-encoded space", got)
	}
	if strings.Contains(got, "+") {
		t.Fatalf("BuildTOTPProvisioningURI = %q, must never encode a space as +", got)
	}
}

// TestBuildTOTPProvisioningURI_TooLong proves an oversized escaped email
// makes the built URI exceed 2,048 bytes and fail closed instead of
// silently truncating.
func TestBuildTOTPProvisioningURI_TooLong(t *testing.T) {
	secret := docExampleTOTPSecret(t)
	longEmail := strings.Repeat("@", 800) // each '@' escapes to the 3-byte "%40"
	_, err := BuildTOTPProvisioningURI("aboutme.vn", longEmail, secret)
	if !errors.Is(err, ErrTOTPProvisioningURITooLong) {
		t.Fatalf("BuildTOTPProvisioningURI: err = %v, want ErrTOTPProvisioningURITooLong", err)
	}
}

// TestValidateTOTPIssuer_Hostile proves every non-canonical issuer shape is
// rejected: empty, over-length, uppercase, an IPv4 literal, an IPv6
// literal, a non-ASCII host, a trailing dot, a leading hyphen, and an
// over-length label.
func TestValidateTOTPIssuer_Hostile(t *testing.T) {
	longLabel := strings.Repeat("a", 64) // one label over the 63-byte bound
	label63 := strings.Repeat("a", 63)
	fourLabels := label63 + "." + label63 + "." + label63 + "." + label63 // 255 bytes total, each label within bound

	cases := map[string]string{
		"empty":           "",
		"uppercase":       "AboutMe.VN",
		"ipv4 literal":    "127.0.0.1",
		"ipv6 literal":    "::1",
		"non-ascii host":  "exämple.com",
		"trailing dot":    "aboutme.vn.",
		"leading hyphen":  "-aboutme.vn",
		"trailing hyphen": "aboutme-.vn",
		"label too long":  longLabel + ".vn",
		"host too long":   fourLabels,
	}
	for name, issuer := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateTOTPIssuer(issuer); !errors.Is(err, ErrTOTPInvalidIssuer) {
				t.Fatalf("ValidateTOTPIssuer(%q): err = %v, want ErrTOTPInvalidIssuer", issuer, err)
			}
		})
	}
}

// TestValidateTOTPIssuer_Valid proves ordinary lowercase hostnames and the
// bare "localhost" label are accepted.
func TestValidateTOTPIssuer_Valid(t *testing.T) {
	for _, issuer := range []string{"aboutme.vn", "localhost", "a.b.c", "a-b.example.com"} {
		if err := ValidateTOTPIssuer(issuer); err != nil {
			t.Fatalf("ValidateTOTPIssuer(%q): %v", issuer, err)
		}
	}
}
