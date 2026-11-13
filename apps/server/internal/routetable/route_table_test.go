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

const caddyBinEnv = "CADDY_BIN"

const stubHeader = "X-Stub-Backend"

const bodyDigestETag = `"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"`

func caddyBinary(t *testing.T) string {
	t.Helper()
	bin := os.Getenv(caddyBinEnv)
	if bin == "" {
		t.Skipf("%s not set; skipping live Caddy route-table integration test (set %s=caddy or an absolute path to a caddy 2.11.x binary)", caddyBinEnv, caddyBinEnv)
	}
	return bin
}

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

func tcpPort(t *testing.T, addr net.Addr) int {
	t.Helper()

	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		t.Fatalf("address %v is not a *net.TCPAddr", addr)
	}
	return tcpAddr.Port
}

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
			t.Logf("stub backend %s: writing response body: %v", name, err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

type caddyfileReplacement struct {
	old   string
	new   string
	count int
}

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

func startCaddy(t *testing.T, bin, configPath string) {
	t.Helper()

	stateDir := t.TempDir()
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
		if err := cmd.Wait(); err != nil {
			t.Logf("caddy process exited: %v", err)
		}
		if t.Failed() {
			t.Logf("caddy process output:\n%s", out.String())
		}
	})
}

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

type wantBackend int

const (
	wantGo wantBackend = iota
	wantWeb
	wantDenied
)

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

	cases := []routeCase{
		{name: "api", method: http.MethodGet, path: "/api", want: wantGo},
		{name: "api_v1_wildcard", method: http.MethodGet, path: "/api/v1/nope", want: wantGo},
		{name: "projects_list", method: http.MethodGet, path: "/api/v1/projects", want: wantGo},
		{name: "projects_create", method: http.MethodPost, path: "/api/v1/projects", want: wantGo},
		{name: "projects_read", method: http.MethodGet, path: "/api/v1/projects/018f5b6a-9a3e-7c21-8b1e-000000000001", want: wantGo},
		{name: "projects_replace", method: http.MethodPut, path: "/api/v1/projects/018f5b6a-9a3e-7c21-8b1e-000000000001", want: wantGo},
		{name: "projects_delete", method: http.MethodDelete, path: "/api/v1/projects/018f5b6a-9a3e-7c21-8b1e-000000000001", want: wantGo},
		{name: "sitemap_xml", method: http.MethodGet, path: "/sitemap.xml", want: wantGo},
		{name: "robots_txt", method: http.MethodGet, path: "/robots.txt", want: wantGo},
		{name: "llms_txt", method: http.MethodGet, path: "/llms.txt", want: wantGo},
		{name: "root_level_md_slug", method: http.MethodGet, path: "/someone.md", want: wantGo},
		{name: "healthz", method: http.MethodGet, path: "/healthz", want: wantGo},
		{name: "readyz", method: http.MethodGet, path: "/readyz", want: wantGo},
		{name: "healthz_subpath", method: http.MethodGet, path: "/healthz/nope", want: wantGo},
		{name: "healthz_markdown", method: http.MethodGet, path: "/healthz.md", want: wantGo},
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

		{name: "root", method: http.MethodGet, path: "/", want: wantWeb},
		{name: "app", method: http.MethodGet, path: "/app", want: wantWeb},
		{name: "app_subpath", method: http.MethodGet, path: "/app/resumes/x", want: wantWeb},
		{name: "nuxt_assets", method: http.MethodGet, path: "/_nuxt/app.js", want: wantWeb},
		{name: "login", method: http.MethodGet, path: "/login", want: wantWeb},
		{name: "login_markdown", method: http.MethodGet, path: "/login.md", want: wantWeb},
		{name: "authorize_consent_page", method: http.MethodGet, path: "/authorize", want: wantWeb},
		{name: "authorize_query", method: http.MethodGet, path: "/authorize?client_id=x", want: wantWeb},
		{name: "authorize_markdown", method: http.MethodGet, path: "/authorize.md", want: wantWeb},
		{name: "privacy_page", method: http.MethodGet, path: "/privacy", want: wantWeb},
		{name: "privacy_markdown", method: http.MethodGet, path: "/privacy.md", want: wantWeb},
		{name: "terms_page", method: http.MethodGet, path: "/terms", want: wantWeb},
		{name: "terms_markdown", method: http.MethodGet, path: "/terms.md", want: wantWeb},
		{name: "privacy_prefixed_slug", method: http.MethodGet, path: "/privacy-policy", want: wantGo},
		{name: "templates_gallery", method: http.MethodGet, path: "/templates", want: wantWeb},
		{name: "templates_page", method: http.MethodGet, path: "/templates/ats-plain", want: wantWeb},
		{name: "templates_markdown", method: http.MethodGet, path: "/templates.md", want: wantWeb},
		{name: "templates_prefixed_slug", method: http.MethodGet, path: "/templates-by-ada", want: wantGo},
		{name: "verify_page", method: http.MethodGet, path: "/verify", want: wantWeb},
		{name: "verify_markdown", method: http.MethodGet, path: "/verify.md", want: wantWeb},
		{name: "verify_prefixed_slug", method: http.MethodGet, path: "/verify-me", want: wantGo},
		{name: "guide_mcp_page", method: http.MethodGet, path: "/guide/mcp", want: wantWeb},
		{name: "guide_mcp_markdown", method: http.MethodGet, path: "/guide/mcp.md", want: wantWeb},
		{name: "guide_root_page", method: http.MethodGet, path: "/guide", want: wantWeb},
		{name: "guide_prefixed_slug", method: http.MethodGet, path: "/guide-me", want: wantGo},
		{name: "guides_slug", method: http.MethodGet, path: "/guides", want: wantGo},
		{name: "mcp_endpoint_still_go", method: http.MethodGet, path: "/mcp", want: wantGo},
		{name: "showcase_page", method: http.MethodGet, path: "/showcase", want: wantWeb},
		{name: "showcase_markdown", method: http.MethodGet, path: "/showcase.md", want: wantWeb},
		{name: "showcase_prefixed_slug", method: http.MethodGet, path: "/showcase-me", want: wantGo},
		{name: "showcases_slug", method: http.MethodGet, path: "/showcases", want: wantGo},
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
	cases = append(cases, projectRouteParityCases(t)...)

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
