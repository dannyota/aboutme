// Package routetable closes AC-OPS-005 (design spec §2; docs/plans/
// traceability/ac-ops.md) with an automated test of the Caddy route table.
// Before this file, the only verification was UAT-P0-06's manual curl
// step — the one Phase 0 acceptance criterion with no automated coverage.
//
// # Design decision
//
// Two approaches were viable: (a) exercise deploy/caddy/Caddyfile through a
// real Caddy process and assert routing over live HTTP, or (b) run
// `caddy adapt`/`caddy validate` and assert against the adapted JSON
// without a live process. (b) was rejected: it proves the config parses
// and can describe its own matchers, but it cannot prove that a request
// for a given path is actually dispatched to a given upstream — that is a
// routing *decision* Caddy makes at request time, not a static property of
// the adapted JSON tree. A test that only re-parses the Caddyfile (or its
// JSON form) is one Caddyfile-syntax refactor away from asserting nothing
// real, which is exactly the "greps the Caddyfile text" failure mode the
// task called out. This file takes (a): a real `caddy` binary, fronting
// two tiny stub HTTP backends that each identify themselves in a response
// header, with every path class from UAT-P0-06 driven over real HTTP and
// the actual backend reached (or Caddy's own denial) observed directly.
//
// # Why the real Caddyfile, not a duplicated fixture
//
// Rather than maintaining a second, hand-written "trimmed" Caddyfile that
// could silently drift from deploy/caddy/Caddyfile (the file that actually
// ships), this test reads the real file and substitutes only its three
// environment-specific tokens — the listen port and the two upstream
// addresses — leaving every matcher, handle block, and directive
// byte-for-byte what production and dev actually run. Each substitution
// target must occur in the file exactly once; if it doesn't (because the
// Caddyfile's shape changed), the test fails loudly with the count it
// found, rather than silently matching zero occurrences and testing a
// config that no longer resembles the real one. That per-substitution
// count check is a second, independent line of drift protection beyond
// "the test reads the real file at all".
//
// # Gating
//
// This test drives a real subprocess and two real HTTP servers, so it is
// gated like apps/server/internal/store's live-database integration test:
// skipped unless an environment variable — here CADDY_BIN, naming a caddy
// binary (a bare name is resolved via PATH; see os/exec.Command) — is set.
// `go test ./...` and `make server-test` therefore stay fully hermetic;
// nothing here runs unless a caller opts in by setting CADDY_BIN.
//
// # What this does not (yet) cover
//
// The client-IP trust boundary (X-Real-IP stripping and reassertion) is
// AC-OPS-008/009's concern, with its own dedicated tests in
// apps/server/internal/api/ratelimit_test.go and config_test.go; this file
// only asserts *which backend* a path reaches, not the headers Caddy
// attaches on the way there.
package routetable

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// caddyBinEnv names the environment variable that gates this test and
// supplies the caddy executable to run. Unset means "skip" — mirroring
// TEST_DATABASE_URL in apps/server/internal/store/integration_test.go, the
// value itself is the resource locator, not a bare on/off flag.
const caddyBinEnv = "CADDY_BIN"

// stubHeader is the response header each stub backend sets to identify
// itself. It is the "Go's fingerprint" / "web's header set" signal the
// task describes, made unambiguous: rather than approximating the real
// server's or Nuxt's actual header set (which would drift as those
// services evolve, and would leave open the question of whether a test
// failure meant "misrouted" or "the real service's headers changed"),
// each stub asserts its identity directly and only that.
const stubHeader = "X-Stub-Backend"

const bodyDigestETag = `"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"`

// caddyBinary returns the caddy executable to run, or skips the test if
// CADDY_BIN is unset.
func caddyBinary(t *testing.T) string {
	t.Helper()
	bin := os.Getenv(caddyBinEnv)
	if bin == "" {
		t.Skipf("%s not set; skipping live Caddy route-table integration test (set %s=caddy or an absolute path to a caddy 2.11.x binary)", caddyBinEnv, caddyBinEnv)
	}
	return bin
}

// findUpward walks from startDir toward the filesystem root looking for a
// directory containing rel, returning the joined path at the first match.
// Same technique as apps/server/internal/api/health_contract_test.go's
// helper of the same name (unexported there, so duplicated here rather
// than shared across packages for one 15-line helper): this makes the
// lookup resilient to this package moving relative to the repo root — it
// only needs deploy/caddy/Caddyfile to exist somewhere above wherever the
// test runs from.
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

// findFreePort asks the OS for an unused TCP port on 127.0.0.1 by binding
// a listener and immediately closing it. There is an inherent, small
// TOCTOU window between the close and Caddy's own bind — the standard
// trade-off for handing a subprocess a port number in advance — but it is
// the same technique httptest.NewServer uses internally to pick its own
// port, and is not a source of the sleep-based flakiness the task warns
// against: readiness is still established by polling (waitForCaddyReady),
// never assumed from this call succeeding.
func findFreePort(t *testing.T) int {
	t.Helper()

	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	defer func() {
		if cerr := l.Close(); cerr != nil {
			t.Errorf("closing free-port probe listener: %v", cerr)
		}
	}()

	return tcpPort(t, l.Addr())
}

// tcpPort extracts the port number from addr, failing the test if addr is
// not a *net.TCPAddr (the errcheck linter's check-type-assertions setting
// requires the ",ok" form rather than a bare assertion at every call
// site, so every caller in this file goes through here instead of
// repeating the check-and-fail inline).
func tcpPort(t *testing.T, addr net.Addr) int {
	t.Helper()

	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		t.Fatalf("address %v is not a *net.TCPAddr", addr)
	}
	return tcpAddr.Port
}

// newStubBackend starts an httptest.Server that answers every request with
// 200 and a stubHeader identifying it as name ("go" or "web"), and
// registers its shutdown with t.Cleanup.
func newStubBackend(t *testing.T, name string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(stubHeader, name)
		if name == "go" && r.URL.Path == "/etag" {
			w.Header().Set("ETag", bodyDigestETag)
			if r.Header.Get("If-None-Match") == bodyDigestETag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.WriteHeader(http.StatusOK)
			if _, err := io.WriteString(w, strings.Repeat("body-digest-etag\n", 80)); err != nil {
				t.Logf("stub backend %s: writing ETag response: %v", name, err)
			}
			return
		}
		w.WriteHeader(http.StatusOK)
		if _, err := fmt.Fprintf(w, "%s-backend %s", name, r.URL.Path); err != nil {
			// The client already has the response headers/status by the
			// time a body write could fail; there is nothing left to
			// signal to it. Logging (rather than a swallowed error) keeps
			// this from being silent while not treating a client-side
			// disconnect mid-body as a fatal server bug.
			t.Logf("stub backend %s: writing response body: %v", name, err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// caddyfileReplacement is one required, exact substitution applied to the
// real Caddyfile before it is handed to a test Caddy process.
type caddyfileReplacement struct {
	old   string
	new   string
	count int
}

// adaptedCaddyfile reads the real deploy/caddy/Caddyfile and returns its
// content with exactly three tokens substituted: the site's listen port
// (production/dev use :80, which this test cannot bind without root — see
// the repo's "rootless podman cannot bind port 80" gotcha, which applies
// to any process, not just podman) and the two upstream addresses (Docker
// Compose service names, unresolvable outside the compose network) for
// the stub backends' loopback addresses. Every matcher, handle block, and
// directive besides those three tokens is whatever deploy/caddy/Caddyfile
// actually contains — this is the real route table, not a paraphrase of
// it.
func adaptedCaddyfile(t *testing.T, sitePort, goPort, webPort int) string {
	t.Helper()

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd(): %v", err)
	}
	caddyfilePath := findUpward(t, wd, filepath.Join("deploy", "caddy", "Caddyfile"))

	raw, err := os.ReadFile(caddyfilePath)
	if err != nil {
		t.Fatalf("reading %s: %v", caddyfilePath, err)
	}
	content := string(raw)
	fragmentPath := findUpward(t, wd, filepath.Join("deploy", "caddy", "public-roots.generated.caddy"))
	fragment, err := os.ReadFile(fragmentPath)
	if err != nil {
		t.Fatalf("reading %s: %v", fragmentPath, err)
	}
	const generatedMarker = "\t# ABOUTME_PUBLIC_ROOTS_GENERATED\n\timport /etc/caddy/generated/public-roots.generated.caddy"
	if got := strings.Count(content, generatedMarker); got != 1 {
		t.Fatalf("%s: generated marker count = %d, want 1", caddyfilePath, got)
	}
	content = strings.Replace(content, generatedMarker, strings.TrimSuffix(string(fragment), "\n"), 1)

	replacements := []caddyfileReplacement{
		{old: ":80 {", new: fmt.Sprintf(":%d {", sitePort), count: 1},
		{old: "reverse_proxy server:8080 {", new: fmt.Sprintf("reverse_proxy 127.0.0.1:%d {", goPort), count: 2},
		{old: "reverse_proxy web:3000", new: fmt.Sprintf("reverse_proxy 127.0.0.1:%d", webPort), count: 2},
	}
	for _, r := range replacements {
		if got := strings.Count(content, r.old); got != r.count {
			t.Fatalf("%s: found %d occurrence(s) of %q, want exactly %d — "+
				"the real Caddyfile's shape changed; update this test's substitution to match",
				caddyfilePath, got, r.old, r.count)
		}
		content = strings.ReplaceAll(content, r.old, r.new)
	}
	return content
}

// startCaddy launches bin as `caddy run --config configPath --adapter
// caddyfile`, captures its combined output for failure diagnostics, and
// registers its termination with t.Cleanup. It points Caddy's XDG config
// and data directories (instance UUID, TLS storage, admin autosave) at
// fresh subdirectories of t.TempDir() rather than the real user's
// ~/.config/caddy and ~/.local/share/caddy — verified by manual run that,
// left unset, a native `caddy run` writes there even with "admin off" and
// no TLS in the site block, which would leak test-process state into the
// developer's/CI runner's real home directory across every run.
func startCaddy(t *testing.T, bin, configPath string) {
	t.Helper()

	stateDir := t.TempDir()
	// context.Background(): this helper owns the process's whole lifecycle
	// itself via t.Cleanup's explicit Kill+Wait below, the same pattern
	// cmd/migrate/gen/main.go uses for its own exec.CommandContext call
	// (invoking `atlas`) — there is no shorter-lived request context to
	// thread through a test helper.
	cmd := exec.CommandContext(context.Background(), bin, "run", "--config", configPath, "--adapter", "caddyfile")
	cmd.Env = append(os.Environ(),
		"XDG_CONFIG_HOME="+filepath.Join(stateDir, "config"),
		"XDG_DATA_HOME="+filepath.Join(stateDir, "data"),
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting %s: %v (is CADDY_BIN=%q a valid caddy 2.11.x executable?)", bin, err, bin)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				t.Logf("killing caddy process: %v", err)
			}
		}
		// Wait() reporting "signal: killed" here is the expected result of
		// the Kill() above, not a failure — it is still called (and its
		// error inspected via Logf, never blank-discarded) to reap the
		// process and to have the output buffer fully populated before the
		// t.Failed() check below reads it.
		if err := cmd.Wait(); err != nil {
			t.Logf("caddy process exited: %v", err)
		}
		if t.Failed() {
			t.Logf("caddy process output:\n%s", out.String())
		}
	})
}

// waitForCaddyReady polls baseURL until it gets any HTTP response (success
// or not — a 404 still proves the listener is up and Caddy is routing) or
// timeout elapses. This is a bounded poll, not a fixed sleep-then-hope: it
// returns the instant the server responds, and fails the test with the
// last dial error if it never does.
func waitForCaddyReady(t *testing.T, baseURL string, timeout time.Duration) {
	t.Helper()

	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+"/", nil)
		if err != nil {
			t.Fatalf("building readiness probe request: %v", err)
		}
		resp, err := client.Do(req)
		if err == nil {
			if cerr := resp.Body.Close(); cerr != nil {
				t.Errorf("closing readiness probe response body: %v", cerr)
			}
			return
		}
		lastErr = err
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("caddy at %s did not become ready within %s: %v", baseURL, timeout, lastErr)
}

// wantBackend names which backend (or denial) a route class must reach.
type wantBackend int

const (
	wantGo wantBackend = iota
	wantWeb
	wantDenied
)

// TestRouteTable_CaddyRoutesEachPathClassToTheCorrectBackend drives every
// route class UAT-P0-06 / AC-OPS-005 describes through a real Caddy
// process fronting two stub backends, and asserts each one reaches the
// correct backend (or, for /print and /print/*, reaches neither).
func TestRouteTable_CaddyRoutesEachPathClassToTheCorrectBackend(t *testing.T) {
	bin := caddyBinary(t)

	goStub := newStubBackend(t, "go")
	webStub := newStubBackend(t, "web")

	goPort := tcpPort(t, goStub.Listener.Addr())
	webPort := tcpPort(t, webStub.Listener.Addr())
	sitePort := findFreePort(t)

	content := adaptedCaddyfile(t, sitePort, goPort, webPort)

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "Caddyfile")
	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("writing adapted Caddyfile to %s: %v", cfgPath, err)
	}

	startCaddy(t, bin, cfgPath)

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", sitePort)
	waitForCaddyReady(t, baseURL, 10*time.Second)

	cases := []struct {
		name   string
		method string
		path   string
		want   wantBackend
	}{
		// go-proxied: UAT-P0-06's `/api/v1/nope`, `/sitemap.xml`,
		// `/robots.txt`, `/llms.txt`, plus the root-level `/*.md` class
		// and the unversioned health endpoints, all under the @go
		// matcher in deploy/caddy/Caddyfile.
		{name: "api", method: http.MethodGet, path: "/api", want: wantGo},
		{name: "api_v1_wildcard", method: http.MethodGet, path: "/api/v1/nope", want: wantGo},
		{name: "sitemap_xml", method: http.MethodGet, path: "/sitemap.xml", want: wantGo},
		{name: "robots_txt", method: http.MethodGet, path: "/robots.txt", want: wantGo},
		{name: "llms_txt", method: http.MethodGet, path: "/llms.txt", want: wantGo},
		{name: "root_level_md_slug", method: http.MethodGet, path: "/someone.md", want: wantGo},
		{name: "healthz", method: http.MethodGet, path: "/healthz", want: wantGo},
		{name: "readyz", method: http.MethodGet, path: "/readyz", want: wantGo},
		{name: "healthz_subpath", method: http.MethodGet, path: "/healthz/nope", want: wantGo},
		{name: "healthz_markdown", method: http.MethodGet, path: "/healthz.md", want: wantGo},
		// v6 agent-access roots. `/oauth` and `/oauth.md` also satisfy the
		// public-slug grammar, so their fixed-root precedence is not
		// observable from the backend alone; `/authorize` below is the case
		// that distinguishes a fixed Nuxt root from a Go-served slug.
		{name: "well_known_exact", method: http.MethodGet, path: "/.well-known", want: wantGo},
		{name: "well_known_authorization_server", method: http.MethodGet, path: "/.well-known/oauth-authorization-server", want: wantGo},
		{name: "well_known_protected_resource", method: http.MethodGet, path: "/.well-known/oauth-protected-resource", want: wantGo},
		{name: "oauth_root", method: http.MethodGet, path: "/oauth", want: wantGo},
		{name: "oauth_authorize", method: http.MethodGet, path: "/oauth/authorize", want: wantGo},
		{name: "oauth_token", method: http.MethodPost, path: "/oauth/token", want: wantGo},
		{name: "oauth_register", method: http.MethodPost, path: "/oauth/register", want: wantGo},
		{name: "oauth_revoke", method: http.MethodPost, path: "/oauth/revoke", want: wantGo},
		{name: "oauth_markdown", method: http.MethodGet, path: "/oauth.md", want: wantGo},
		{name: "mcp_endpoint", method: http.MethodPost, path: "/mcp", want: wantGo},
		{name: "mcp_subpath", method: http.MethodPost, path: "/mcp/anything", want: wantGo},

		{name: "minimum_slug", method: http.MethodGet, path: "/a1-b", want: wantGo},
		{name: "maximum_slug", method: http.MethodGet, path: "/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", want: wantGo},
		{name: "minimum_markdown_slug", method: http.MethodGet, path: "/a1-b.md", want: wantGo},
		{name: "maximum_markdown_slug", method: http.MethodGet, path: "/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.md", want: wantGo},

		// web-proxied: UAT-P0-06's `/`, plus an arbitrary unmatched path
		// and a *nested* .md path — the Caddyfile's own comment says its
		// wildcard "does not cross a '/'", so a nested .md must fall
		// through to the catch-all, not the @go matcher. This is the
		// negative case that actually distinguishes "root-level slug
		// page" from "any file ending in .md".
		{name: "root", method: http.MethodGet, path: "/", want: wantWeb},
		{name: "app", method: http.MethodGet, path: "/app", want: wantWeb},
		{name: "app_subpath", method: http.MethodGet, path: "/app/resumes/x", want: wantWeb},
		{name: "nuxt_assets", method: http.MethodGet, path: "/_nuxt/app.js", want: wantWeb},
		{name: "login", method: http.MethodGet, path: "/login", want: wantWeb},
		{name: "login_markdown", method: http.MethodGet, path: "/login.md", want: wantWeb},
		// The consent page is a fixed Nuxt root whose name also satisfies
		// the public-slug grammar; without its registry row it would be
		// proxied to Go as a resume slug instead.
		{name: "authorize_consent_page", method: http.MethodGet, path: "/authorize", want: wantWeb},
		{name: "authorize_query", method: http.MethodGet, path: "/authorize?client_id=x", want: wantWeb},
		{name: "authorize_markdown", method: http.MethodGet, path: "/authorize.md", want: wantWeb},
		{name: "unmatched_editor_route", method: http.MethodGet, path: "/resume/editor/summary", want: wantWeb},
		{name: "nested_md_does_not_match_go", method: http.MethodGet, path: "/nested/path.md", want: wantWeb},
		{name: "too_short_slug", method: http.MethodGet, path: "/abc", want: wantWeb},
		{name: "too_long_slug", method: http.MethodGet, path: "/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", want: wantWeb},
		{name: "leading_hyphen", method: http.MethodGet, path: "/-abcd", want: wantWeb},
		{name: "trailing_hyphen", method: http.MethodGet, path: "/abcd-", want: wantWeb},
		{name: "double_hyphen", method: http.MethodGet, path: "/ab--cd", want: wantWeb},
		{name: "uppercase_slug", method: http.MethodGet, path: "/ABCD", want: wantWeb},
		{name: "underscore_slug", method: http.MethodGet, path: "/ab_cd", want: wantWeb},
		{name: "unregistered_extension", method: http.MethodGet, path: "/someone.txt", want: wantWeb},

		// denied: UAT-P0-06's `/print`, `/print/x` — Caddy answers
		// directly (respond 404), never proxying to either backend.
		{name: "print_denied", method: http.MethodGet, path: "/print", want: wantDenied},
		{name: "print_subpath_denied", method: http.MethodGet, path: "/print/x", want: wantDenied},
		{name: "print_markdown_denied", method: http.MethodGet, path: "/print.md", want: wantDenied},
		{name: "admin_denied", method: http.MethodGet, path: "/admin", want: wantDenied},
		{name: "admin_subpath_denied", method: http.MethodGet, path: "/admin/x", want: wantDenied},
		{name: "admin_markdown_denied", method: http.MethodGet, path: "/admin.md", want: wantDenied},
		{name: "people_denied", method: http.MethodGet, path: "/people", want: wantDenied},
		{name: "u_denied", method: http.MethodGet, path: "/u", want: wantDenied},
		{name: "internal_render_exact_denied", method: http.MethodGet, path: "/internal-render", want: wantDenied},
		{name: "internal_render_denied", method: http.MethodPost, path: "/internal-render/public", want: wantDenied},
		{name: "internal_render_markdown_denied", method: http.MethodGet, path: "/internal-render.md", want: wantDenied},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), tc.method, baseURL+tc.path, nil)
			if err != nil {
				t.Fatalf("GET %s: building request: %v", tc.path, err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("GET %s: %v", tc.path, err)
			}
			defer func() {
				if cerr := resp.Body.Close(); cerr != nil {
					t.Errorf("GET %s: closing response body: %v", tc.path, cerr)
				}
			}()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("GET %s: reading body: %v", tc.path, err)
			}

			via := resp.Header.Get("Via")
			backend := resp.Header.Get(stubHeader)

			switch tc.want {
			case wantGo:
				if backend != "go" {
					t.Errorf("GET %s: %s = %q, want %q — should reach the Go backend (got body %q)", tc.path, stubHeader, backend, "go", body)
				}
				if via == "" {
					t.Errorf("GET %s: Via header missing, want non-empty — a proxied response must carry Caddy's Via header", tc.path)
				}
			case wantWeb:
				if backend != "web" {
					t.Errorf("GET %s: %s = %q, want %q — should reach the web backend (got body %q)", tc.path, stubHeader, backend, "web", body)
				}
				if via == "" {
					t.Errorf("GET %s: Via header missing, want non-empty — a proxied response must carry Caddy's Via header", tc.path)
				}
			case wantDenied:
				if resp.StatusCode != http.StatusNotFound {
					t.Errorf("GET %s: status = %d, want %d — Caddy should deny this path directly", tc.path, resp.StatusCode, http.StatusNotFound)
				}
				if via != "" {
					t.Errorf("GET %s: Via = %q, want empty — a denied path must never reach a backend", tc.path, via)
				}
				if backend != "" {
					t.Errorf("GET %s: %s = %q, want empty — a denied path must never reach a backend", tc.path, stubHeader, backend)
				}
			}
		})
	}

	t.Run("encoded_body_digest_etags_preserve_conditional_parity", func(t *testing.T) {
		client := &http.Client{Transport: &http.Transport{DisableCompression: true}}
		for _, encoding := range []string{"identity", "gzip", "zstd"} {
			t.Run(encoding, func(t *testing.T) {
				request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+"/etag", nil)
				if err != nil {
					t.Fatalf("building %s ETag request: %v", encoding, err)
				}
				request.Header.Set("Accept-Encoding", encoding)
				response, err := client.Do(request)
				if err != nil {
					t.Fatalf("requesting %s ETag response: %v", encoding, err)
				}
				if closeErr := response.Body.Close(); closeErr != nil {
					t.Fatalf("closing %s ETag response: %v", encoding, closeErr)
				}
				if response.StatusCode != http.StatusOK {
					t.Fatalf("%s response status = %d, want 200", encoding, response.StatusCode)
				}
				etag := response.Header.Get("ETag")
				if encoding == "identity" {
					if etag != bodyDigestETag || response.Header.Get("Content-Encoding") != "" {
						t.Fatalf("identity ETag/encoding = %q/%q", etag, response.Header.Get("Content-Encoding"))
					}
				} else {
					wantETag := strings.TrimSuffix(bodyDigestETag, "\"") + "-" + encoding + "\""
					if etag != wantETag || response.Header.Get("Content-Encoding") != encoding {
						t.Fatalf("%s ETag/encoding = %q/%q", encoding, etag, response.Header.Get("Content-Encoding"))
					}
				}

				conditional, err := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+"/etag", nil)
				if err != nil {
					t.Fatalf("building %s conditional request: %v", encoding, err)
				}
				conditional.Header.Set("Accept-Encoding", encoding)
				conditional.Header.Set("If-None-Match", etag)
				conditionalResponse, err := client.Do(conditional)
				if err != nil {
					t.Fatalf("requesting %s conditional response: %v", encoding, err)
				}
				if err := conditionalResponse.Body.Close(); err != nil {
					t.Fatalf("closing %s conditional response: %v", encoding, err)
				}
				if conditionalResponse.StatusCode != http.StatusNotModified {
					t.Fatalf("%s conditional status = %d, want 304", encoding, conditionalResponse.StatusCode)
				}
			})
		}
	})
}
