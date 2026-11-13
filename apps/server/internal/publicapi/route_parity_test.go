package publicapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var projectRouteParityPath = filepath.Join("..", "..", "..", "..", "deploy", "caddy", "testdata", "project-route-parity.json")

func TestClassifyPublicRoute_ProjectRouteParity(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(projectRouteParityPath)
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Path  string `json:"path"`
			Owner string `json:"owner"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || len(fixture.Cases) == 0 {
		t.Fatalf("decode fixture: %v", err)
	}
	for _, tc := range fixture.Cases {
		want := publicRouteNone
		if tc.Owner == "go" {
			want = publicRouteProject
			if strings.Count(tc.Path, "/") == 2 {
				want = publicRouteProjectList
			}
		}
		if got := classifyPublicRoute(tc.Path); got != want {
			t.Errorf("classifyPublicRoute(%q) = %d, want %d (owner %s)", tc.Path, got, want, tc.Owner)
		}
	}
}

func TestClassifyPublicRoute_ProjectShapesRejectEscapes(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"/%61da-lovelace/projects",
		"/ada-lovelace/%70rojects",
		"/ada-lovelace/projects/%61nalytical",
		"/ada-lovelace/projects%2Fanalytical",
	} {
		if got := classifyPublicRoute(path); got != publicRouteNone {
			t.Errorf("classifyPublicRoute(%q) = %d, want none", path, got)
		}
	}
}
