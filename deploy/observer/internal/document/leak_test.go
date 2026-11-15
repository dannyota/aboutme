package document_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"danny.vn/aboutme/deploy/observer/internal/document"
	"danny.vn/aboutme/deploy/observer/internal/leakscan"
	"danny.vn/aboutme/deploy/observer/internal/platform"
	"danny.vn/aboutme/deploy/observer/internal/platform/podman"
	"danny.vn/aboutme/deploy/observer/internal/verify"
	"danny.vn/aboutme/deploy/observer/schema"
)

var plainCanaries = []string{
	"123456789012",
	"10.9.8.7", "10-9-8-7", "fd00:9:8::7",
	"ap-southeast-1b",
	"c0ffee00c0ffee00",
	"3f2a9c1e-7b4d", "0f1e2d3c-4b5a", "9d8c7b6a-5f4e", "1a2b3c4d-5e6f",
	"0a:1b:2c:3d", "subnet-", "ReplicaSet", "ExecuteCommandAgent", "ElasticNetworkInterface",
	"HEALTHY", "RUNNING", "ecs.cpu-architecture", "arn:", "task-definition", "role/",
}

func podmanImages(t *testing.T, strip bool) []platform.Image {
	t.Helper()
	info := platform.Info{Provider: "greennode", Orchestrator: "podman", Region: "HCM03"}
	running := time.Date(2026, 10, 2, 3, 0, 0, 0, time.UTC)
	var imgs []platform.Image
	for i, component := range platform.Components {
		imgs = append(imgs, platform.Image{
			Component: component, Digest: "sha256:" + strings.Repeat(fmt.Sprintf("%02x", 0xa1+i), 32),
			Replicas: 1, RunningSince: running,
		})
	}
	claims := map[string]verify.Evidence{}
	if !strip {
		for _, img := range imgs {
			repo := platform.ImageRepository(img.Component)
			claims[img.Digest] = verify.Evidence{
				Digest: img.Digest,
				Provenance: verify.Provenance{
					Status: verify.Verified, CheckedAt: running, Subjects: []string{repo},
					Version: "v9.9.9", Commit: "c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00",
					SignerWorkflow: "https://github.com/dannyota/aboutme/.github/workflows/release-images.yml@refs/tags/v9.9.9",
					LogIndex:       123456789012,
					BuildURL:       "https://github.com/dannyota/aboutme/actions/runs/123456789012/attempts/1",
				},
				SBOM: verify.SBOM{Status: verify.Verified, CheckedAt: running, Subjects: []string{repo}},
			}
		}
	}
	reported, err := document.Build(document.Input{
		Environment: "production", Site: "https://aboutme.vn", Platform: info,
		ObservedAt: time.Now(), Images: imgs, Evidence: claims,
		Observer: document.BuildInfo{Version: "v9.9.9", Commit: "c0ffee00c0ffee00c0ffee00c0ffee00c0ffee00"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(reported)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "observation.json")
	if err = os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := podman.New(path).Running(context.Background())
	if err != nil {
		t.Fatalf("podman adapter: %v", err)
	}
	return out
}

func build(t *testing.T, imgs []platform.Image, info platform.Info) []byte {
	t.Helper()
	ev := map[string]verify.Evidence{}
	now := time.Date(2026, 10, 2, 3, 14, 5, 0, time.UTC)
	for _, img := range imgs {
		repo := platform.ImageRepository(img.Component)
		ev[img.Digest] = verify.Evidence{
			Digest: img.Digest,
			Provenance: verify.Provenance{
				Status: verify.Verified, CheckedAt: now, Subjects: []string{repo},
				Version: "v0.6.5", Commit: "3b1f0000000000000000000000000000000f9e07",
				SignerWorkflow: "https://github.com/dannyota/aboutme/.github/workflows/release-images.yml@refs/tags/v0.6.5",
				LogIndex:       2965228543,
				BuildURL:       "https://github.com/dannyota/aboutme/actions/runs/36218940587/attempts/1",
			},
			SBOM: verify.SBOM{Status: verify.Verified, CheckedAt: now, Subjects: []string{repo}},
		}
	}
	doc, err := document.Build(document.Input{
		Environment: "production",
		Site:        "https://aboutme.vn",
		Platform:    info,
		ObservedAt:  now,
		Images:      imgs,
		Evidence:    ev,
		Observer:    document.BuildInfo{Version: "v0.6.4", Commit: "5d0e00000000000000000000000000000000a771"},
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := document.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type reporter interface {
	Helper()
	Errorf(format string, args ...any)
}

type recorder struct{ failures int }

func (r *recorder) Helper() {}

func (r *recorder) Errorf(string, ...any) { r.failures++ }

func assertNoLeak(t reporter, b []byte) {
	t.Helper()
	s := string(b)
	if strings.Contains(strings.ToLower(s), "canary") {
		t.Errorf("a canary value reached the document:\n%s", s)
	}
	for _, c := range plainCanaries {
		if strings.Contains(s, c) {
			t.Errorf("canary %q reached the document", c)
		}
	}
	for _, f := range leakscan.Find(b) {
		t.Errorf("document %s", f)
	}
}

func schemaPaths(t *testing.T) map[string]bool {
	t.Helper()
	var root map[string]any
	if err := json.Unmarshal(schema.V1, &root); err != nil {
		t.Fatal(err)
	}
	defs, _ := root["$defs"].(map[string]any) //nolint:errcheck // absent defs fail below
	paths := map[string]bool{}
	var walk func(node map[string]any, prefix string, depth int)
	walk = func(node map[string]any, prefix string, depth int) {
		if depth > 20 {
			t.Fatal("schema nests too deeply")
		}
		if ref, ok := node["$ref"].(string); ok {
			def, ok := defs[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
			if !ok {
				t.Fatalf("unresolved %s", ref)
			}
			walk(def, prefix, depth+1)
		}
		if alts, ok := node["oneOf"].([]any); ok {
			for _, a := range alts {
				if m, ok := a.(map[string]any); ok {
					walk(m, prefix, depth+1)
				}
			}
		}
		if items, ok := node["items"].(map[string]any); ok {
			walk(items, prefix+"[]", depth+1)
		}
		if props, ok := node["properties"].(map[string]any); ok {
			for k, v := range props {
				p := k
				if prefix != "" {
					p = prefix + "." + k
				}
				paths[p] = true
				if m, ok := v.(map[string]any); ok {
					walk(m, p, depth+1)
				}
			}
		}
	}
	walk(root, "", 0)
	return paths
}

func documentPaths(t *testing.T, b []byte) []string {
	t.Helper()
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	var out []string
	var walk func(v any, prefix string)
	walk = func(v any, prefix string) {
		switch x := v.(type) {
		case map[string]any:
			for k, c := range x {
				p := k
				if prefix != "" {
					p = prefix + "." + k
				}
				out = append(out, p)
				walk(c, p)
			}
		case []any:
			for _, c := range x {
				walk(c, prefix+"[]")
			}
		}
	}
	walk(v, "")
	sort.Strings(out)
	return out
}

func TestDocumentLeaksNothing(t *testing.T) {
	adapters := map[string]struct {
		images func(*testing.T, bool) []platform.Image
		info   platform.Info
	}{
		"podman": {podmanImages, platform.Info{Provider: "greennode", Orchestrator: "podman", Region: "HCM03"}},
	}
	allowed := schemaPaths(t)
	for name, a := range adapters {
		t.Run(name, func(t *testing.T) {
			imgs := a.images(t, false)
			if len(imgs) != 4 {
				t.Fatalf("adapter returned %d images, want one per component: %+v", len(imgs), imgs)
			}
			b := build(t, imgs, a.info)
			if err := schema.Validate(b); err != nil {
				t.Fatal(err)
			}
			assertNoLeak(t, b)
			for _, p := range documentPaths(t, b) {
				if !allowed[p] {
					t.Errorf("key path %q is not in the schema allowlist", p)
				}
			}
			if again := build(t, a.images(t, true), a.info); !bytes.Equal(b, again) {
				t.Errorf("an unknown platform field changed the document")
			}
		})
	}
}

func TestLeakChecksCatchLeaks(t *testing.T) {
	leaks := []string{
		`{"x":"canary-cluster-7f3a"}`, `{"x":"123456789012"}`, `{"x":"arn:aws:ecs:x"}`,
		`{"x":"10.1.2.3"}`, `{"x":"fd00:9:8::7"}`, `{"x":"ap-southeast-1a"}`,
		`{"x":"0a1b2c3d4e5f60718293a4b5c6d7e8f9"}`, `{"x":"9d8c7b6a-5f4e-4d3c-8b2a-1f0e9d8c7b6a"}`,
		`{"x":"docker-pullable://registry/x"}`, `{"x":"https://evil.example/"}`,
	}
	for _, l := range leaks {
		var r recorder
		assertNoLeak(&r, []byte(l))
		if r.failures == 0 {
			t.Errorf("the leak checks passed %s", l)
		}
	}
}
