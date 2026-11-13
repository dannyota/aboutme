package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type projectRouteParityCase struct {
	Path  string `json:"path"`
	Owner string `json:"owner"`
	Feed  string `json:"feed"`
}

func readProjectRouteParity(t *testing.T) []projectRouteParityCase {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "project-route-parity.json"))
	if err != nil {
		t.Fatal("feed-parity-fixture")
	}
	var fixture struct {
		Cases []projectRouteParityCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || len(fixture.Cases) == 0 {
		t.Fatal("feed-parity-fixture")
	}
	return fixture.Cases
}

func TestCrowdSecFeedEncoderProjectRouteParity(t *testing.T) {
	t.Parallel()
	for _, tc := range readProjectRouteParity(t) {
		t.Run(tc.Path, func(t *testing.T) {
			t.Parallel()
			for _, status := range []int{200, 404} {
				probe, target := "none", ""
				if tc.Feed == "unknown" && status == 404 {
					probe, target = "other", tc.Path
				}
				assertFeedTarget(t, feedModeServing, tc.Path, status, tc.Feed, probe, target)
			}
		})
	}
}

func TestCrowdSecFeedEncoderProjectProtectedViews(t *testing.T) {
	t.Parallel()
	for _, path := range []string{
		"/ada-lovelace/projects",
		"/ada-lovelace/projects/analytical-engine",
		"/aaaa/projects/" + strings.Repeat("a", 40),
	} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			encoded := "/"
			for _, char := range []byte(path[1:]) {
				if char == '/' {
					encoded += "/"
					continue
				}
				encoded += fmt.Sprintf("%%%02x", char)
			}
			for i, view := range []string{
				path, strings.ToUpper(path), encoded,
				strings.ReplaceAll(encoded, "%", "%25"),
				"/unrecognized/../" + path[1:],
				"/unrecognized/%2e%2E/" + path[1:],
				"/unrecognized\\..\\" + path[1:],
				path + "/sample/..",
				strings.ReplaceAll(path, "/", "//"),
			} {
				t.Run(fmt.Sprintf("view-%d", i), func(t *testing.T) {
					for _, status := range []int{400, 404, 405} {
						assertFeedTarget(t, feedModeServing, view, status, "public", "none", "")
					}
				})
			}
		})
	}
}

func TestCrowdSecFeedEncoderProjectComponentBounds(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("a", 41)
	for _, path := range []string{
		"/aaaa/projects/" + long,
		"/aaaa/projects/" + strings.Repeat("a", 300),
		"/" + long + "/projects",
		"/" + long + "/projects/analytical-engine",
		"/aaaa/" + strings.Repeat("projects", 6) + "/analytical-engine",
		"/aaaa/projects/analytical-engine/" + long,
		"/aaaa/projects/" + long + "/../analytical-engine/" + long,
	} {
		t.Run(fmt.Sprint(len(path)), func(t *testing.T) {
			t.Parallel()
			assertFeedTarget(t, feedModeServing, path, 404, "unknown", feedProbeForTest(path), path)
		})
	}
	assertFeedTarget(t, feedModeServing, "/aaaa/projects/"+long+"/../analytical-engine", 404, "public", "none", "")
}

func feedProbeForTest(path string) string {
	if strings.Contains(path, "/../") {
		return "traversal"
	}
	return "other"
}
