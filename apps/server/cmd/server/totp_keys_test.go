package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const testTOTPActiveKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func TestRunTOTPKeyReencrypt_RejectsArguments(t *testing.T) {
	t.Parallel()
	if err := runTOTPKeyReencrypt([]string{"--dry-run"}); err == nil {
		t.Fatal("runTOTPKeyReencrypt([\"--dry-run\"]) error = nil, want a usage rejection")
	}
}

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
	err := dispatch([]string{totpKeyReencryptCommandName, "unexpected-arg"})
	if err == nil || !strings.Contains(err.Error(), "usage: server totp-key-reencrypt") {
		t.Fatalf("dispatch() error = %v, want runTOTPKeyReencrypt's usage rejection", err)
	}
}
