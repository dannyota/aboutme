package leakscan

import (
	"os"
	"path/filepath"
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
	}
	for _, l := range leaks {
		if Find([]byte(l)) == nil {
			t.Errorf("Find passed %s", l)
		}
	}
}
