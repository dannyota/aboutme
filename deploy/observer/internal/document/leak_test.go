package document_test

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"

	"github.com/dannyota/aboutme/deploy/observer/internal/document"
	"github.com/dannyota/aboutme/deploy/observer/internal/platform"
	"github.com/dannyota/aboutme/deploy/observer/internal/platform/ecs"
	"github.com/dannyota/aboutme/deploy/observer/internal/platform/kubernetes"
	"github.com/dannyota/aboutme/deploy/observer/internal/verify"
	"github.com/dannyota/aboutme/deploy/observer/schema"
)

// The fixtures under testdata/leak fill every field the design says must
// never be written with a canary: most contain "canary", and these do not.
var plainCanaries = []string{
	"123456789012",                        // account ID
	"10.9.8.7", "10-9-8-7", "fd00:9:8::7", // addresses
	"ap-southeast-1b",                                                  // availability zone
	"c0ffee00c0ffee00",                                                 // task and container instance IDs
	"3f2a9c1e-7b4d", "0f1e2d3c-4b5a", "9d8c7b6a-5f4e", "1a2b3c4d-5e6f", // UUIDs
	"0a:1b:2c:3d", "subnet-", "ReplicaSet", "ExecuteCommandAgent", "ElasticNetworkInterface",
	"HEALTHY", "RUNNING", "ecs.cpu-architecture", "arn:", "task-definition", "role/",
}

const unknownMember = "unknownMemberCanary"

func leakFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "leak", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// withoutUnknownMember removes every unknownMemberCanary key, giving the
// fixture as the adapters' known AWS and Kubernetes shapes describe it.
func withoutUnknownMember(t *testing.T, raw []byte) []byte {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	var strip func(any)
	strip = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			delete(x, unknownMember)
			for _, c := range x {
				strip(c)
			}
		case []any:
			for _, c := range x {
				strip(c)
			}
		}
	}
	strip(v)
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// ecsImages runs the real ECS adapter and SDK against a fake endpoint
// serving the recorded-shape responses.
func ecsImages(t *testing.T, strip bool) []platform.Image {
	t.Helper()
	load := func(name string) []byte {
		b := leakFixture(t, name)
		if strip {
			b = withoutUnknownMember(t, b)
		}
		return b
	}
	lists := map[string][]byte{
		"aboutme-prod-app":         load("ecs-list-app.json"),
		"aboutme-prod-web":         load("ecs-list-web.json"),
		"aboutme-prod-maintenance": load("ecs-list-maintenance.json"),
	}
	describe := load("ecs-describe-tasks.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		switch target := r.Header.Get("X-Amz-Target"); {
		case strings.HasSuffix(target, ".ListTasks"):
			var req struct {
				ServiceName string `json:"serviceName"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_, _ = w.Write(lists[req.ServiceName]) //nolint:errcheck // test server
		case strings.HasSuffix(target, ".DescribeTasks"):
			_, _ = w.Write(describe) //nolint:errcheck // test server
		default:
			http.Error(w, "unexpected target "+target, http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	client := awsecs.New(awsecs.Options{
		Region:       "ap-southeast-1",
		BaseEndpoint: aws.String(srv.URL),
		Credentials:  credentials.NewStaticCredentialsProvider("canary-key-id", "canary-secret", ""),
		HTTPClient:   srv.Client(),
	})
	lister := ecs.New(client, ecs.Config{
		Cluster:            "canary-cluster-7f3a",
		AppService:         "aboutme-prod-app",
		WebService:         "aboutme-prod-web",
		MaintenanceService: "aboutme-prod-maintenance",
	})
	imgs, err := lister.Running(context.Background())
	if err != nil {
		t.Fatalf("ecs adapter: %v", err)
	}
	return imgs
}

// kubernetesImages runs the real Kubernetes adapter against a TLS API
// server serving the recorded-shape PodList.
func kubernetesImages(t *testing.T, strip bool) []platform.Image {
	t.Helper()
	body := leakFixture(t, "pod-list.json")
	if strip {
		body = withoutUnknownMember(t, body)
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body) //nolint:errcheck // test server
	}))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.crt")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	token := filepath.Join(dir, "token")
	if err := os.WriteFile(token, []byte("canary-token-1b2c"), 0o600); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	lister, err := kubernetes.New(kubernetes.Config{Namespace: "aboutme", Host: host, Port: port, TokenFile: token, CAFile: ca})
	if err != nil {
		t.Fatal(err)
	}
	imgs, err := lister.Running(context.Background())
	if err != nil {
		t.Fatalf("kubernetes adapter: %v", err)
	}
	return imgs
}

// build turns adapter output into document bytes, with every digest
// verified so every optional field and link is filled.
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

var (
	allowedHex = regexp.MustCompile(`sha256:[0-9a-f]{64}|\b[0-9a-f]{40}\b`)
	timestamp  = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z`)
	forbidden  = map[string]*regexp.Regexp{
		"12-digit number":   regexp.MustCompile(`\d{12}`),
		"arn":               regexp.MustCompile(`(?i)arn:`),
		"IPv4 address":      regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}\b`),
		"IPv6 address":      regexp.MustCompile(`(?i)\b[0-9a-f]{0,4}(:[0-9a-f]{0,4}){2,7}\b`),
		"availability zone": regexp.MustCompile(`\b[a-z]{2}(-gov)?-[a-z]+-\d[a-z]\b`),
		"32-hex ID":         regexp.MustCompile(`(?i)[0-9a-f]{32}`),
		"UUID":              regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`),
	}
	scheme      = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://`)
	urlPrefixes = []string{
		"https://aboutme.vn\"",
		"https://github.com/dannyota/aboutme\"",
		"https://github.com/dannyota/aboutme/",
		"https://api.github.com/repos/dannyota/aboutme/attestations/",
		"https://search.sigstore.dev/?logIndex=",
	}
)

// reporter is the part of testing.TB the leak checks use, so a test can
// record their failures without failing itself.
type reporter interface {
	Helper()
	Errorf(format string, args ...any)
}

// recorder counts failures for TestLeakChecksCatchLeaks.
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
	// Digests, commits, and timestamps are allowed values; strip them before
	// looking for identifiers hidden inside other strings.
	scrubbed := timestamp.ReplaceAllString(allowedHex.ReplaceAllString(s, "HEX"), "TIME")
	for name, re := range forbidden {
		if m := re.FindString(scrubbed); m != "" {
			t.Errorf("document holds a %s: %q", name, m)
		}
	}
	for _, loc := range scheme.FindAllStringIndex(s, -1) {
		rest := s[loc[0]:]
		if !slices.ContainsFunc(urlPrefixes, func(p string) bool { return strings.HasPrefix(rest, p) }) {
			t.Errorf("document holds a URL outside the allowed prefixes: %.80q", rest)
		}
	}
}

// schemaPaths lists every key path the JSON Schema allows, with [] for
// array items, by walking properties, items, $ref, and oneOf.
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

// TestDocumentLeaksNothing is the sanitizer's contract
// (docs/design/deployment-transparency/document.md, "Sanitizer").
func TestDocumentLeaksNothing(t *testing.T) {
	adapters := map[string]struct {
		images func(*testing.T, bool) []platform.Image
		info   platform.Info
	}{
		"ecs":        {ecsImages, platform.Info{Provider: "aws", Orchestrator: "ecs", Region: "ap-southeast-1"}},
		"kubernetes": {kubernetesImages, platform.Info{Provider: "greennode", Orchestrator: "kubernetes", Region: "HCM03"}},
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
			// A response member the adapters do not know changes nothing.
			if again := build(t, a.images(t, true), a.info); !bytes.Equal(b, again) {
				t.Errorf("an unknown platform field changed the document")
			}
		})
	}
}

// TestLeakChecksCatchLeaks proves the pattern checks above fail on each kind
// of identifier, so a passing TestDocumentLeaksNothing means something.
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
