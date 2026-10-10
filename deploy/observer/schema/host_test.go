package schema

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestHostDocument validates the generator's output against the shared schema.
func TestHostDocument(t *testing.T) {
	work := t.TempDir()
	bin := filepath.Join(work, "bin")
	public := filepath.Join(work, "public")
	for _, dir := range []string{bin, public} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	stub := `#!/bin/sh
case "$2" in
  exists) [ "$3" != aboutme-maintenance ] ;;
  inspect) printf 'true sha256:%064d 2026-10-11T01:14:05.123456789Z\n' 7 ;;
  *) exit 99 ;;
esac
`
	if err := os.WriteFile(filepath.Join(bin, "podman"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "bash", "../../vn/host/deployment-document.sh", public, "HCM03")
	cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("host generator: %v: %s", err, output)
	}
	body, err := os.ReadFile(filepath.Join(public, "deployment.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(body); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["summary"] != "unverified" || doc["release"] != nil {
		t.Fatal("host document claims checked evidence")
	}
	platform, ok := doc["platform"].(map[string]any)
	if !ok {
		t.Fatal("host document has no platform object")
	}
	for _, mutate := range []func(){
		func() { platform["provider"] = "aws" },
		func() {
			platform["provider"] = "greennode"
			platform["reporter"] = "host"
		},
	} {
		mutate()
		changed, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		if Validate(changed) == nil {
			t.Fatal("accepted a wrong provider or an extra platform field")
		}
	}
}
