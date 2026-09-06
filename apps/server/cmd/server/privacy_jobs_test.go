package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestParsePrivacyCommand(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"idempotency-expiry-sweep", "media-deletion-sweep", "media-orphan-sweep", "privacy-retention-sweep"} {
		command, err := parsePrivacyCommand([]string{name})
		if err != nil || command.name != name || command.dryRun {
			t.Fatalf("command %s: parsed=%+v err=%v", name, command, err)
		}
	}
	command, err := parsePrivacyCommand([]string{"media-orphan-sweep", "--dry-run"})
	if err != nil || !command.dryRun {
		t.Fatal("orphan dry run rejected")
	}
	for _, args := range [][]string{
		nil, {"untrusted-input"}, {"media-orphan-sweep", "--unknown"},
		{"media-deletion-sweep", "--dry-run"},
		{"media-orphan-sweep", "--dry-run", "--dry-run"},
	} {
		if _, err := parsePrivacyCommand(args); err == nil {
			t.Fatalf("accepted unsupported invocation: %v", args)
		}
	}
}

func TestPrivacyCommandStartupFailureUsesFixedOutput(t *testing.T) {
	t.Parallel()
	var output, diagnostics bytes.Buffer
	err := executePrivacyCommand(t.Context(), privacyCommand{name: "idempotency-expiry-sweep"},
		func(name string) string {
			if name != "DATABASE_URL" {
				t.Fatalf("unexpected configuration dependency: %s", name)
			}
			return "not-a-database-private-value"
		}, &output, &diagnostics)
	if err == nil || strings.Contains(err.Error()+output.String()+diagnostics.String(), "not-a-database-private-value") {
		t.Fatal("startup failure did not preserve fixed diagnostics")
	}
	var report map[string]any
	if decodeErr := json.Unmarshal(output.Bytes(), &report); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if len(report) != 4 || report["success"] != false || report["result"] != nil || report["job"] != "idempotency-expiry-sweep" {
		t.Fatal("startup failure result does not match the fixed contract")
	}
}

func TestPrivacyCommandCanceledBeforeConfiguration(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	var output, diagnostics bytes.Buffer
	err := executePrivacyCommand(ctx, privacyCommand{name: "media-orphan-sweep", dryRun: true},
		func(string) string { t.Fatal("canceled command read configuration"); return "" }, &output, &diagnostics)
	if err == nil || !strings.Contains(output.String(), `"success":false`) {
		t.Fatal("canceled command reported success")
	}
}
