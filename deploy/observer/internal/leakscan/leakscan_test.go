package leakscan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindPassesEveryExample(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "testdata", "examples", "*.json"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no examples: %v", err)
	}
	for _, p := range paths {
		b, readErr := os.ReadFile(p)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if found := Find(b); found != nil {
			t.Errorf("%s: %v", filepath.Base(p), found)
		}
	}
}

func TestFindCatchesEachKind(t *testing.T) {
	leaks := []string{
		`{"x":"123456789012"}`, `{"x":"arn:aws:ecs:x"}`, `{"x":"10.1.2.3"}`,
		`{"x":"fd00:9:8::7"}`, `{"x":"ap-southeast-1a"}`,
		`{"x":"0a1b2c3d4e5f60718293a4b5c6d7e8f9"}`, `{"x":"9d8c7b6a-5f4e-4d3c-8b2a-1f0e9d8c7b6a"}`,
		`{"x":"docker-pullable://registry/x"}`, `{"x":"https://evil.example/"}`,
		`{"x":"https://aboutme.vn.evil.example/"}`,
		`{"x":"https://github.com/someone/aboutme/commit/x"}`, `{"x":"https://github.com/dntmakers/aboutme2"}`,
		`{"x":"https://api.github.com/repos/someone/aboutme/attestations/x"}`,
		`{"x":"https://api.github.com/repositories/1318874703/attestations/x"}`,
	}
	for _, l := range leaks {
		if Find([]byte(l)) == nil {
			t.Errorf("Find passed %s", l)
		}
	}
}

func TestFindPassesBothOwners(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "examples", "verified.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"dannyota", "dntmakers"} {
		if found := Find([]byte(strings.ReplaceAll(string(b), "dannyota", owner))); found != nil {
			t.Errorf("%s: %v", owner, found)
		}
	}
}
