package main

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestEvidenceUsesTheClosedAllowlist(t *testing.T) {
	run := testArtifacts(t, runScope)
	if err := writeEvidence(run, workflowEvidence(modeProduction, reconcileCreated, revocationRevoked, true)); err != nil {
		t.Fatal(err)
	}
	data, err := run.read(evidenceName)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if decodeErr := json.Unmarshal(data, &fields); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	want := []string{"count_delta", "create_reconciliation", "language", "mode", "post_revocation_401", "revocation", "sdk_version", "source_unchanged", "stage", "target_private", "tool_count", "transport", "version"}
	if !reflect.DeepEqual(keys, want) || len(data) > maxEvidenceBytes {
		t.Fatalf("evidence keys = %v size = %d", keys, len(data))
	}
	for _, forbidden := range []string{"token", "http://", "https://", "aboutme.vn", "localhost", "@", fakeSourceID, "resume_id", "digest"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("evidence contains %q", forbidden)
		}
	}
}

func TestEvidenceRejectsOpenLabelsAndMixedShapes(t *testing.T) {
	run := testArtifacts(t, runScope)
	valid := workflowEvidence(modeLocal, reconcileReplayed, revocationRevoked, true)
	for name, mutate := range map[string]func(*runEvidence){
		"mode":            func(e *runEvidence) { e.Mode = "staging" },
		"stage":           func(e *runEvidence) { e.Stage = "other" },
		"reconciliation":  func(e *runEvidence) { e.CreateReconciliation = "a free-form note" },
		"revocation":      func(e *runEvidence) { e.Revocation = "maybe" },
		"sdk":             func(e *runEvidence) { e.SDKVersion = "v2" },
		"tool count":      func(e *runEvidence) { e.ToolCount = 16 },
		"language":        func(e *runEvidence) { e.Language = "en" },
		"count delta":     func(e *runEvidence) { e.CountDelta = 2 },
		"source changed":  func(e *runEvidence) { e.SourceUnchanged = false },
		"recovery mixing": func(e *runEvidence) { e.Stage = stageRevocationOnly },
	} {
		value := valid
		mutate(&value)
		if err := writeEvidence(run, value); !errors.Is(err, errEvidence) {
			t.Fatalf("%s accepted", name)
		}
	}
	if err := writeEvidence(run, recoveryEvidence(modeProduction, revocationUnconfirmed)); err != nil {
		t.Fatalf("recovery evidence rejected: %v", err)
	}
}

func TestEvidenceMatchesTheOwnerWorkflowValidator(t *testing.T) {
	script, err := os.ReadFile("../../../../scripts/mcp-owner-workflow.sh")
	if err != nil {
		t.Fatalf("read owner workflow script: %v", err)
	}
	done := regexp.MustCompile(`sdk_version: "([^"]+)",\s+transport: "([^"]+)", tool_count: (\d+), language: "([^"]+)"`).FindSubmatch(script)
	if done == nil {
		t.Fatal("owner workflow script has no completed-evidence shape")
	}
	want := workflowEvidence(modeLocal, reconcileReplayed, revocationRevoked, true)
	got := []string{string(done[1]), string(done[2]), string(done[3]), string(done[4])}
	if expected := []string{want.SDKVersion, want.Transport, strconv.Itoa(want.ToolCount), want.Language}; !reflect.DeepEqual(got, expected) {
		t.Fatalf("validator expects %v, runner writes %v", got, expected)
	}
}
