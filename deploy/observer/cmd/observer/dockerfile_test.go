package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDockerfileBuildsOnlyForAKnownOwner(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	declared := slices.Index(lines, `ARG OWNER=""`)
	check := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, `RUN case "$OWNER" in `) })
	build := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "-X main.owner=${OWNER}") })
	if declared < 0 || check < declared || build < check {
		t.Fatalf("Dockerfile must declare OWNER, check it, then link it into main.owner: lines %d, %d, %d", declared, check, build)
	}
	script := strings.TrimPrefix(lines[check], "RUN ")
	for value, accepted := range map[string]bool{
		"dannyota": true, "dntmakers": true, "": false, "someone": false, "Dannyota": false, "dannyota dntmakers": false,
	} {
		cmd := exec.CommandContext(t.Context(), "sh", "-c", script)
		cmd.Env = append(os.Environ(), "OWNER="+value)
		if out, runErr := cmd.CombinedOutput(); (runErr == nil) != accepted {
			t.Errorf("OWNER=%q: accepted %v, want %v: %s", value, runErr == nil, accepted, out)
		}
	}
}
