package publicapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// projectRouteParityPath is the route parity fixture shared with the Caddy
// route-table test and the request feed.
var projectRouteParityPath = filepath.Join("..", "..", "..", "..", "deploy", "caddy", "testdata", "project-route-parity.json")

// TestClassifyPublicRoute_ProjectRouteParity proves the Go classifier
// recognizes exactly the paths Caddy sends to Go for the project shapes, and
// tells the list from a project page by depth.
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

// TestClassifyPublicRoute_ProjectShapesRejectEscapes proves a percent sign
// anywhere in a project path classifies as no route, as for the resume page.
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
