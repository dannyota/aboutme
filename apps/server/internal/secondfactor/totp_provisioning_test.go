package secondfactor

import (
	"errors"
	"strings"
	"testing"
)

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

func TestBuildTOTPProvisioningURI_TooLong(t *testing.T) {
	secret := docExampleTOTPSecret(t)
	longEmail := strings.Repeat("@", 800)
	_, err := BuildTOTPProvisioningURI("aboutme.vn", longEmail, secret)
	if !errors.Is(err, ErrTOTPProvisioningURITooLong) {
		t.Fatalf("BuildTOTPProvisioningURI: err = %v, want ErrTOTPProvisioningURITooLong", err)
	}
}

func TestValidateTOTPIssuer_Hostile(t *testing.T) {
	longLabel := strings.Repeat("a", 64)
	label63 := strings.Repeat("a", 63)
	fourLabels := label63 + "." + label63 + "." + label63 + "." + label63

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

func TestValidateTOTPIssuer_Valid(t *testing.T) {
	for _, issuer := range []string{"aboutme.vn", "localhost", "a.b.c", "a-b.example.com"} {
		if err := ValidateTOTPIssuer(issuer); err != nil {
			t.Fatalf("ValidateTOTPIssuer(%q): %v", issuer, err)
		}
	}
}
