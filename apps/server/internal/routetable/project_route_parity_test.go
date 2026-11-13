package routetable

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// routeCase is one request the route-table test drives through Caddy.
type routeCase struct {
	name   string
	method string
	path   string
	want   wantBackend
}

// projectRouteParityCases reads deploy/caddy/testdata/project-route-parity.json,
// the route parity fixture shared with the Go route classifier and the
// request feed, and turns each row into a GET with its expected owner: go,
// nuxt (the Nuxt catch-all), or 404 from Caddy itself.
func projectRouteParityCases(t *testing.T) []routeCase {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() error = %v", err)
	}
	path := findUpward(t, wd, filepath.Join("deploy", "caddy", "testdata", "project-route-parity.json"))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var fixture struct {
		Cases []struct {
			Path  string `json:"path"`
			Owner string `json:"owner"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil || len(fixture.Cases) == 0 {
		t.Fatalf("decoding %s: %v", path, err)
	}
	owners := map[string]wantBackend{"go": wantGo, "nuxt": wantWeb, "404": wantDenied}
	cases := make([]routeCase, 0, len(fixture.Cases))
	for _, row := range fixture.Cases {
		want, ok := owners[row.Owner]
		if !ok {
			t.Fatalf("%s: owner %q for %s, want go, nuxt or 404", path, row.Owner, row.Path)
		}
		cases = append(cases, routeCase{name: "project_parity" + row.Path, method: http.MethodGet, path: row.Path, want: want})
	}
	return cases
}
