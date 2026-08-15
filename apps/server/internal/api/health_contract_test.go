// health_contract_test.go pins the /healthz and /readyz contract between
// docs/api/openapi.yaml and the real Go handlers. It parses the document,
// then drives api.New()'s real handlers over a real httptest.Server. A
// ResponseRecorder cannot prove an empty HEAD body; see TestRouter_Healthz_
// HeadRequest_ReturnsOKWithEmptyBody. The test fails if either
// side disagrees on status code, Content-Type, or response body shape for
// GET and HEAD on both endpoints.
package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/dannyota/aboutme/apps/server/internal/api"
)

// Schema component references the health responses must use, per
// docs/api/openapi.yaml's components — this test asserts the document
// points at these shared components rather than an inlined schema, and
// that the real handler body actually satisfies the referenced shape.
const (
	envelopeSchemaRef = "#/components/schemas/Envelope"
	errorSchemaRef    = "#/components/schemas/Error"
)

// oaDoc is the subset of the OpenAPI document this test needs: enough to
// look up, per path and method, which status codes are documented, which
// media type they use, and which component schema they reference.
type oaDoc struct {
	Paths map[string]oaPathItem `yaml:"paths"`
}

type oaPathItem struct {
	Get  *oaOperation `yaml:"get"`
	Head *oaOperation `yaml:"head"`
}

type oaOperation struct {
	Description string                `yaml:"description"`
	Responses   map[string]oaResponse `yaml:"responses"`
}

type oaResponse struct {
	Content map[string]oaMediaType `yaml:"content"`
}

type oaMediaType struct {
	Schema oaSchemaRef `yaml:"schema"`
}

type oaSchemaRef struct {
	Ref string `yaml:"$ref"`
}

// findUpward walks from startDir toward the filesystem root looking for a
// directory containing rel, returning the joined path at the first match.
// This is used instead of a hardcoded "../../../../docs/api/openapi.yaml"
// so the lookup doesn't silently break if this package ever moves relative
// to the repo root — it only needs docs/api/openapi.yaml to still exist
// somewhere above wherever this test runs from.
func findUpward(t *testing.T, startDir, rel string) string {
	t.Helper()

	dir := startDir
	for {
		candidate := filepath.Join(dir, rel)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("walked up from %s to the filesystem root without finding %s", startDir, rel)
		}
		dir = parent
	}
}

// loadHealthOpenAPIDoc locates and parses docs/api/openapi.yaml, walking up
// from the running test's package directory (go test's working directory
// is already the package directory; this only makes the ascent, rather
// than a fixed depth, do the work of finding the repo root).
func loadHealthOpenAPIDoc(t *testing.T) oaDoc {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd(): %v", err)
	}
	path := findUpward(t, wd, filepath.Join("docs", "api", "openapi.yaml"))

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	var doc oaDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	return doc
}

// operationFor returns the documented operation for method on path, or
// fails the test if either the path or that method isn't documented.
func operationFor(t *testing.T, doc oaDoc, path, method string) oaOperation {
	t.Helper()

	item, ok := doc.Paths[path]
	if !ok {
		t.Fatalf("openapi.yaml: path %q is not documented", path)
	}

	var op *oaOperation
	switch method {
	case http.MethodGet:
		op = item.Get
	case http.MethodHead:
		op = item.Head
	default:
		t.Fatalf("operationFor: unsupported method %q", method)
	}
	if op == nil {
		t.Fatalf("openapi.yaml: %s %s is not documented", method, path)
	}
	return *op
}

// documentedJSONResponse returns the "application/json" media type entry
// for status on op, failing the test if that status isn't documented at
// all, or is documented under a different media type such as text/plain.
func documentedJSONResponse(t *testing.T, op oaOperation, method, path string, status int) oaMediaType {
	t.Helper()

	statusStr := strconv.Itoa(status)
	resp, ok := op.Responses[statusStr]
	if !ok {
		t.Fatalf("openapi.yaml: %s %s has no %s response documented", method, path, statusStr)
	}
	media, ok := resp.Content["application/json"]
	if !ok {
		types := make([]string, 0, len(resp.Content))
		for ct := range resp.Content {
			types = append(types, ct)
		}
		t.Fatalf("openapi.yaml: %s %s %s response is not documented as application/json (documented content types: %v)",
			method, path, statusStr, types)
	}
	return media
}

// healthContractCase is one (path, method, backend condition) combination
// this test drives through both the OpenAPI document and the real router.
type healthContractCase struct {
	name       string
	path       string
	method     string
	pinger     api.DBPinger
	wantStatus int
}

func healthContractCases() []healthContractCase {
	down := fakePinger{err: errors.New("connection refused")}
	up := fakePinger{}
	return []healthContractCase{
		{"GET /healthz", "/healthz", http.MethodGet, up, http.StatusOK},
		{"HEAD /healthz", "/healthz", http.MethodHead, up, http.StatusOK},
		{"GET /readyz up", "/readyz", http.MethodGet, up, http.StatusOK},
		{"HEAD /readyz up", "/readyz", http.MethodHead, up, http.StatusOK},
		{"GET /readyz down", "/readyz", http.MethodGet, down, http.StatusServiceUnavailable},
		{"HEAD /readyz down", "/readyz", http.MethodHead, down, http.StatusServiceUnavailable},
	}
}

// TestHealthContract_OpenAPIAndHandlerAgree is the contract test itself.
// For every case it: (1) reads what docs/api/openapi.yaml documents for
// that method/status — media type and referenced schema — then (2) sends a
// real request of that method to api.New()'s router (wired exactly as
// production wires it) and checks the actual status code, Content-Type,
// and body shape match. A drift on either side — document or handler —
// fails this test.
func TestHealthContract_OpenAPIAndHandlerAgree(t *testing.T) {
	t.Parallel()

	doc := loadHealthOpenAPIDoc(t)

	for _, tc := range healthContractCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			op := operationFor(t, doc, tc.path, tc.method)
			media := documentedJSONResponse(t, op, tc.method, tc.path, tc.wantStatus)

			wantRef := envelopeSchemaRef
			if tc.wantStatus != http.StatusOK {
				wantRef = errorSchemaRef
			}
			if media.Schema.Ref != wantRef {
				t.Fatalf("openapi.yaml: %s %s %d schema = %q, want %q",
					tc.method, tc.path, tc.wantStatus, media.Schema.Ref, wantRef)
			}

			handler := api.New(testLogger(), tc.pinger, api.Options{}, nil)
			srv := httptest.NewServer(handler)
			defer srv.Close()

			req, err := http.NewRequestWithContext(context.Background(), tc.method, srv.URL+tc.path, nil)
			if err != nil {
				t.Fatalf("build %s %s request: %v", tc.method, tc.path, err)
			}
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatalf("%s %s: unexpected error: %v", tc.method, tc.path, err)
			}
			defer func() {
				if cerr := resp.Body.Close(); cerr != nil {
					t.Errorf("close response body: %v", cerr)
				}
			}()

			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("%s %s: status = %d, want %d (openapi.yaml documents %d)",
					tc.method, tc.path, resp.StatusCode, tc.wantStatus, tc.wantStatus)
			}

			ct := resp.Header.Get("Content-Type")
			base, _, err := mime.ParseMediaType(ct)
			if err != nil {
				t.Fatalf("%s %s: parse Content-Type %q: %v", tc.method, tc.path, ct, err)
			}
			if base != "application/json" {
				t.Fatalf("%s %s: Content-Type = %q, want application/json (openapi.yaml documents application/json)",
					tc.method, tc.path, ct)
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("%s %s: read body: %v", tc.method, tc.path, err)
			}

			if tc.method == http.MethodHead {
				if len(body) != 0 {
					t.Fatalf("%s %s: body = %q, want empty — openapi.yaml's HEAD description promises no response body",
						tc.method, tc.path, body)
				}
				// Normalize whitespace (the YAML block literal wraps prose
				// across lines) before searching, so the check isn't
				// sensitive to exactly where the source text happens to wrap.
				normalized := strings.Join(strings.Fields(strings.ToLower(op.Description)), " ")
				if !strings.Contains(normalized, "no response body") && !strings.Contains(normalized, "no body") {
					t.Errorf("openapi.yaml: HEAD %s description does not document that HEAD returns no body", tc.path)
				}
				return
			}

			assertBodyMatchesSchema(t, tc.method, tc.path, tc.wantStatus, body)
		})
	}
}

// assertBodyMatchesSchema checks body against the shape the referenced
// component schema requires: {"data":...} for a 200 (Envelope), or
// {"error":{"code":...,"message":...}} for a non-200 (Error) — the same
// two shapes envelope.go's WriteData/WriteError produce and
// docs/api/openapi.yaml's Envelope/Error components require.
func assertBodyMatchesSchema(t *testing.T, method, path string, status int, body []byte) {
	t.Helper()

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("%s %s: unmarshal body: %v (body=%s)", method, path, err, body)
	}

	if status == http.StatusOK {
		raw, ok := payload["data"]
		if !ok {
			t.Fatalf(`%s %s: body is missing the top-level "data" key the Envelope schema requires (body=%s)`,
				method, path, body)
		}
		var data struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			t.Fatalf("%s %s: unmarshal data: %v (body=%s)", method, path, err, body)
		}
		if data.Status == "" {
			t.Errorf("%s %s: data.status is empty (body=%s)", method, path, body)
		}
		return
	}

	raw, ok := payload["error"]
	if !ok {
		t.Fatalf(`%s %s: body is missing the top-level "error" key the Error schema requires (body=%s)`,
			method, path, body)
	}
	var errBody struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &errBody); err != nil {
		t.Fatalf("%s %s: unmarshal error: %v (body=%s)", method, path, err, body)
	}
	if errBody.Code == "" {
		t.Errorf("%s %s: error.code is empty (body=%s)", method, path, body)
	}
	if errBody.Message == "" {
		t.Errorf("%s %s: error.message is empty (body=%s)", method, path, body)
	}
}
