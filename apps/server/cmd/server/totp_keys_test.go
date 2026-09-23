package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestTOTPIssuerFromPublicOrigin(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ origin, want string }{
		{"https://aboutme.vn", "aboutme.vn"},
		{"https://aboutme.example:8443", "aboutme.example"},
		{"http://localhost:20080", "localhost"},
	} {
		got, err := totpIssuerFromPublicOrigin(tc.origin)
		if err != nil {
			t.Fatalf("totpIssuerFromPublicOrigin(%q) error = %v", tc.origin, err)
		}
		if got != tc.want {
			t.Fatalf("totpIssuerFromPublicOrigin(%q) = %q, want %q", tc.origin, got, tc.want)
		}
	}
}

func TestTOTPIssuerFromPublicOrigin_RejectsAnUnparsableOrigin(t *testing.T) {
	t.Parallel()
	if _, err := totpIssuerFromPublicOrigin("://not-a-url"); err == nil {
		t.Fatal("totpIssuerFromPublicOrigin() error = nil, want a parse rejection")
	}
}

// TestRunTOTPKeyReencrypt_RejectsArguments proves the command takes no
// arguments (docs/design/totp-key-management.md "Rotation": the one-shot
// path is a bounded command with no user-supplied parameters).
func TestRunTOTPKeyReencrypt_RejectsArguments(t *testing.T) {
	t.Parallel()
	if err := runTOTPKeyReencrypt([]string{"--dry-run"}); err == nil {
		t.Fatal("runTOTPKeyReencrypt([\"--dry-run\"]) error = nil, want a usage rejection")
	}
}

// TestExecuteTOTPKeyReencrypt_ReportsConfigurationFailureWithoutTheRawKey
// proves a malformed TOTP_ACTIVE_KEY fails the command, writes one fixed
// JSON report naming only the job and success, and never echoes the raw
// value anywhere in its output
// (docs/design/totp-key-management.md "Rotation": "It reports counts and
// row IDs only, never keys, secrets, nonces, ciphertext, email, or account
// IDs").
func TestExecuteTOTPKeyReencrypt_ReportsConfigurationFailureWithoutTheRawKey(t *testing.T) {
	t.Parallel()
	const secretSentinel = "not-a-valid-totp-key-sentinel"
	getenv := func(key string) string {
		switch key {
		case "DATABASE_URL":
			return "postgres://user:pass@localhost:5432/aboutme"
		case "TOTP_ACTIVE_KEY":
			return secretSentinel
		default:
			return ""
		}
	}
	var stdout, stderr bytes.Buffer
	err := executeTOTPKeyReencrypt(context.Background(), getenv, &stdout, &stderr)
	if err == nil {
		t.Fatal("executeTOTPKeyReencrypt() error = nil, want a configuration rejection")
	}
	if strings.Contains(stdout.String(), secretSentinel) || strings.Contains(stderr.String(), secretSentinel) {
		t.Fatalf("output leaked the raw key: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	var report struct {
		Job     string `json:"job"`
		Success bool   `json:"success"`
	}
	if decErr := json.Unmarshal(stdout.Bytes(), &report); decErr != nil {
		t.Fatalf("decode report: %v (stdout=%q)", decErr, stdout.String())
	}
	if report.Job != totpKeyReencryptCommandName || report.Success {
		t.Fatalf("report = %+v, want job=%q success=false", report, totpKeyReencryptCommandName)
	}
}

// TestExecuteTOTPKeyReencrypt_ReportsMissingDatabaseURL proves the command
// fails closed, with the same fixed report shape, when the reduced
// environment omits DATABASE_URL.
func TestExecuteTOTPKeyReencrypt_ReportsMissingDatabaseURL(t *testing.T) {
	t.Parallel()
	getenv := func(key string) string {
		if key == "TOTP_ACTIVE_KEY" {
			return testTOTPActiveKey
		}
		return ""
	}
	var stdout, stderr bytes.Buffer
	err := executeTOTPKeyReencrypt(context.Background(), getenv, &stdout, &stderr)
	if err == nil {
		t.Fatal("executeTOTPKeyReencrypt() error = nil, want a missing DATABASE_URL rejection")
	}
	var report struct {
		Job     string `json:"job"`
		Success bool   `json:"success"`
	}
	if decErr := json.Unmarshal(stdout.Bytes(), &report); decErr != nil {
		t.Fatalf("decode report: %v (stdout=%q)", decErr, stdout.String())
	}
	if report.Success {
		t.Fatal("report.Success = true, want false")
	}
}

func TestDispatch_RoutesTOTPKeyReencryptCommand(t *testing.T) {
	t.Parallel()
	// No arguments beyond the command name is rejected by config loading
	// (no DATABASE_URL in the test process environment is not guaranteed),
	// so this proves only that dispatch reaches runTOTPKeyReencrypt's own
	// argument check rather than runCommand's privacy-command parser, which
	// would reject "totp-key-reencrypt" as an unknown privacy command with a
	// different message.
	err := dispatch([]string{totpKeyReencryptCommandName, "unexpected-arg"})
	if err == nil || !strings.Contains(err.Error(), "usage: server totp-key-reencrypt") {
		t.Fatalf("dispatch() error = %v, want runTOTPKeyReencrypt's usage rejection", err)
	}
}
