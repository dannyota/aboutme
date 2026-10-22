package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type feedRequest struct {
	remoteIP string
	method   string
	uri      string
}

func (r feedRequest) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("remote_ip", r.remoteIP)
	enc.AddString("client_ip", "198.51.100.77")
	enc.AddString("remote_port", "49152")
	enc.AddString("method", r.method)
	enc.AddString("host", "secret-host.example")
	enc.AddString("uri", r.uri)
	enc.AddString("secret", "fieldmarker-5c8d")
	return nil
}

func encodeFeed(t *testing.T, mode string, entry zapcore.Entry, request *feedRequest, fields ...zapcore.Field) string {
	t.Helper()
	enc := newCrowdSecFeedEncoder(mode)
	if request != nil {
		if err := enc.AddObject("request", realFeedRequest(*request)); err != nil {
			t.Fatal("feed-request-collection")
		}
	}
	encoded, err := enc.EncodeEntry(entry, fields)
	if err != nil {
		t.Fatal("feed-encoding")
	}
	defer encoded.Free()
	return encoded.String()
}

func realFeedRequest(request feedRequest) caddyhttp.LoggableHTTPRequest {
	parsed, err := url.ParseRequestURI(request.uri)
	if err != nil {
		parsed = &url.URL{Path: request.uri, RawPath: request.uri}
	}
	return caddyhttp.LoggableHTTPRequest{Request: &http.Request{
		Method: request.method, URL: parsed, RequestURI: request.uri,
		RemoteAddr: net.JoinHostPort(request.remoteIP, "49152"), Host: "secret-host.example",
		Header: http.Header{"X-Real-Ip": {"198.51.100.77"}, "X-Probe": {"headermarker-6f7a"}},
	}}
}

func TestCrowdSecFeedEncoderExactSchema(t *testing.T) {
	t.Parallel()

	entry := zapcore.Entry{Time: time.Date(2026, 10, 10, 8, 9, 10, 123456789, time.FixedZone("ICT", 7*60*60))}
	request := feedRequest{
		remoteIP: "2001:0db8:0000:0000:0000:0000:0000:0001",
		method:   "GET",
		uri:      "/resume-name?token=querymarker-1a2b",
	}
	line := encodeFeed(t, feedModeServing, entry, &request,
		zap.Int("status", 200),
		zap.String("user_id", "usermarker-3d4e"),
		zap.String("resp_headers", "headermarker-6f7a"),
	)
	assertUniqueFeedKeys(t, line)

	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatal("feed-json")
	}
	want := map[string]any{
		"ts":          "2026-10-10T01:09:10.123456789Z",
		"source_ip":   "2001:db8::1",
		"method":      "GET",
		"status":      float64(200),
		"route_class": "public",
		"probe_class": "none",
		"target_path": "",
	}
	if len(got) != len(want) {
		t.Fatal("feed-key-set")
	}
	for key, value := range want {
		if actual, exists := got[key]; !exists || actual != value {
			t.Error("feed-field-value")
		}
	}
	for _, leak := range []string{
		"resume-name", "querymarker-1a2b", "secret-host", "fieldmarker-5c8d",
		"usermarker-3d4e", "headermarker-6f7a", "198.51.100.77", "49152",
	} {
		if strings.Contains(line, leak) {
			t.Error("feed-forbidden-material")
		}
	}
}

func TestCrowdSecFeedEncoderRouteClasses(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"/":                             "public",
		"/.well-known/acme-challenge/x": "system",
		"/healthz.md":                   "system",
		"/internal-render/public":       "system",
		"/print.md":                     "system",
		"/print-secret":                 "public",
		"/internal-renderer":            "public",
		"/_nuxt/app.js":                 "static",
		"/app/resumes":                  "auth",
		"/oauth.md":                     "auth",
		"/verify-email/x":               "auth",
		"/api/v1/resumes":               "api",
		"/mcp":                          "mcp",
		"/guide/mcp":                    "public",
		"/resume-name.md":               "public",
		"/admin":                        "unknown",
		"/people":                       "unknown",
		"/not/a/route":                  "unknown",
		"/%72esume-name":                "public",
	}
	entry := zapcore.Entry{Time: time.Unix(1_700_000_000, 0)}
	for uri, want := range tests {
		uri, want := uri, want
		t.Run(uri, func(t *testing.T) {
			t.Parallel()
			line := encodeFeed(t, feedModeServing, entry, &feedRequest{
				remoteIP: "192.0.2.1",
				method:   "HEAD",
				uri:      uri,
			}, zap.Int("status", 204))
			var got struct {
				RouteClass string `json:"route_class"`
			}
			if err := json.Unmarshal([]byte(line), &got); err != nil {
				t.Fatal("feed-json")
			}
			if got.RouteClass != want {
				t.Error("feed-route-class")
			}
		})
	}

	line := encodeFeed(t, feedModeMaintenance, entry, &feedRequest{
		remoteIP: "192.0.2.1",
		method:   "GET",
		uri:      "/api/v1/private-marker",
	}, zap.Int("status", 503))
	if !strings.Contains(line, `"route_class":"maintenance"`) || strings.Contains(line, "private-marker") {
		t.Fatal("feed-maintenance-privacy")
	}
}

func TestCrowdSecFeedEncoderMethods(t *testing.T) {
	t.Parallel()

	entry := zapcore.Entry{Time: time.Unix(1_700_000_000, 0)}
	for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		line := encodeFeed(t, feedModeServing, entry, &feedRequest{
			remoteIP: "192.0.2.1",
			method:   method,
			uri:      "/api",
		}, zap.Int("status", 200))
		if !strings.Contains(line, `"method":"`+method+`"`) {
			t.Error("feed-allowed-method")
		}
	}
	line := encodeFeed(t, feedModeServing, entry, &feedRequest{
		remoteIP: "192.0.2.1",
		method:   "TRACE-secret-marker",
		uri:      "/api",
	}, zap.Int("status", 405))
	if !strings.Contains(line, `"method":"OTHER"`) || strings.Contains(line, "secret-marker") {
		t.Fatal("feed-other-method")
	}
}

func TestCrowdSecFeedEncoderDropsMalformedRecords(t *testing.T) {
	t.Parallel()

	validEntry := zapcore.Entry{Time: time.Unix(1_700_000_000, 0)}
	validRequest := feedRequest{remoteIP: "192.0.2.1", method: "GET", uri: "/api"}
	tests := []struct {
		name    string
		mode    string
		entry   zapcore.Entry
		request *feedRequest
		fields  []zapcore.Field
	}{
		{name: "mode", mode: "invalid", entry: validEntry, request: &validRequest, fields: []zapcore.Field{zap.Int("status", 200)}},
		{name: "timestamp", mode: feedModeServing, request: &validRequest, fields: []zapcore.Field{zap.Int("status", 200)}},
		{name: "request", mode: feedModeServing, entry: validEntry, fields: []zapcore.Field{zap.Int("status", 200)}},
		{name: "ip", mode: feedModeServing, entry: validEntry, request: &feedRequest{remoteIP: "192.0.2.1:443", method: "GET", uri: "/api"}, fields: []zapcore.Field{zap.Int("status", 200)}},
		{name: "ip zone", mode: feedModeServing, entry: validEntry, request: &feedRequest{remoteIP: "fe80::1%eth0", method: "GET", uri: "/api"}, fields: []zapcore.Field{zap.Int("status", 200)}},
		{name: "method", mode: feedModeServing, entry: validEntry, request: &feedRequest{remoteIP: "192.0.2.1", uri: "/api"}, fields: []zapcore.Field{zap.Int("status", 200)}},
		{name: "status missing", mode: feedModeServing, entry: validEntry, request: &validRequest},
		{name: "status low", mode: feedModeServing, entry: validEntry, request: &validRequest, fields: []zapcore.Field{zap.Int("status", 99)}},
		{name: "status high", mode: feedModeServing, entry: validEntry, request: &validRequest, fields: []zapcore.Field{zap.Int("status", 600)}},
		{name: "status type", mode: feedModeServing, entry: validEntry, request: &validRequest, fields: []zapcore.Field{zap.String("status", "200-secret")}},
		{name: "status repeated", mode: feedModeServing, entry: validEntry, request: &validRequest, fields: []zapcore.Field{zap.Int("status", 200), zap.Int("status", 201)}},
	}
	for _, tt := range tests {
		testCase := tt
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if line := encodeFeed(t, testCase.mode, testCase.entry, testCase.request, testCase.fields...); line != "" {
				t.Error("feed-malformed-base-record")
			}
		})
	}
}

func TestCrowdSecFeedEncoderUnmapsIPv4(t *testing.T) {
	t.Parallel()

	line := encodeFeed(t, feedModeServing, zapcore.Entry{Time: time.Unix(1_700_000_000, 0)}, &feedRequest{
		remoteIP: "::ffff:192.0.2.9",
		method:   "GET",
		uri:      "/api",
	}, zap.Int("status", 200))
	if !strings.Contains(line, `"source_ip":"192.0.2.9"`) || strings.Contains(line, "::ffff") {
		t.Fatal("feed-unmapped-ipv4")
	}
}

func TestCrowdSecFeedEncoderCloneKeepsRequest(t *testing.T) {
	t.Parallel()

	enc := newCrowdSecFeedEncoder(feedModeServing)
	if err := enc.AddObject("request", realFeedRequest(feedRequest{remoteIP: "192.0.2.4", method: "GET", uri: "/api"})); err != nil {
		t.Fatal("feed-request-collection")
	}
	clone := enc.Clone()
	encoded, err := clone.EncodeEntry(zapcore.Entry{Time: time.Unix(1_700_000_000, 0)}, []zapcore.Field{zap.Int("status", 201)})
	if err != nil {
		t.Fatal("feed-encoding")
	}
	defer encoded.Free()
	if !strings.Contains(encoded.String(), `"source_ip":"192.0.2.4"`) {
		t.Fatal("feed-clone-request")
	}
}

func TestCrowdSecFeedEncoderOriginalParsedURL(t *testing.T) {
	t.Parallel()
	request := realFeedRequest(feedRequest{remoteIP: "192.0.2.1", method: "GET", uri: "/unknown/original%2fmarker?token=discarded"})
	request.URL.User = url.User("discarded")
	request.URL.Fragment, request.URL.RawFragment = "discarded", "discarded"
	prepared := caddyhttp.PrepareRequest(request.Request, caddy.NewReplacer(), nil, nil)
	prepared.URL.Path, prepared.URL.RawPath = "/api/rewrite", ""
	prepared.RequestURI = "/api/rewrite"
	request.Request = prepared
	enc := newCrowdSecFeedEncoder(feedModeServing)
	if err := enc.AddObject("request", request); err != nil {
		t.Fatal("feed-request-collection")
	}
	prepared.URL.Path = "/app/another-rewrite"
	encoded, err := enc.EncodeEntry(zapcore.Entry{Time: time.Unix(1_700_000_000, 0)}, []zapcore.Field{zap.Int("status", 404)})
	if err != nil {
		t.Fatal("feed-encoding")
	}
	defer encoded.Free()
	var got map[string]any
	if err := json.Unmarshal(encoded.Bytes(), &got); err != nil {
		t.Fatal("feed-json")
	}
	if got["route_class"] != "unknown" || got["probe_class"] != "other" || got["target_path"] != "/unknown/original%2fmarker" {
		t.Error("feed-original-parsed-url")
	}
	if strings.Contains(encoded.String(), "discarded") || strings.Contains(encoded.String(), "rewrite") {
		t.Error("feed-original-url-privacy")
	}
}

func TestCrowdSecFeedEncoderInconsistentRawPath(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, path, raw string }{
		{"mismatch", "/unknown/sample", "/unknown/other"},
		{"malformed", "/unknown/sample", "/unknown/%zz"},
		{"unescaped", "/unknown/raw space", "/unknown/raw space"},
		{"mixed-utf8", "/unknown/caf\u00e9/a/b", "/unknown/caf\u00e9/a%2fb"},
		{"encoded-leading-slash", "/unknown/sample", "%2funknown/sample"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := realFeedRequest(feedRequest{remoteIP: "192.0.2.1", method: "GET", uri: "/unknown/sample"})
			request.URL.Path, request.URL.RawPath = tc.path, tc.raw
			enc := newCrowdSecFeedEncoder(feedModeServing)
			if err := enc.AddObject("request", request); err != nil {
				t.Fatal("feed-request-collection")
			}
			out, err := enc.EncodeEntry(zapcore.Entry{Time: time.Unix(1_700_000_000, 0)}, []zapcore.Field{zap.Int("status", 404)})
			if err != nil {
				t.Fatal("feed-encoding")
			}
			defer out.Free()
			var got map[string]any
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatal("feed-json")
			}
			if got["probe_class"] != "invalid" || got["target_path"] != "" {
				t.Error("feed-invalid-raw-path")
			}
		})
	}
}

func TestCrowdSecFeedEncoderUnicodePathWithoutRawPath(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, path, target string }{
		{"utf8", "/unknown/caf\u00e9", "/unknown/caf%C3%A9"},
		{"replacement-codepoint", "/unknown/\ufffd", "/unknown/%EF%BF%BD"},
		{"ascii-case-only", "/.well-\u212anown/sample", "/.well-%E2%84%AAnown/sample"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := realFeedRequest(feedRequest{remoteIP: "192.0.2.1", method: "GET", uri: "/unknown/sample"})
			request.URL.Path, request.URL.RawPath = tc.path, ""
			enc := newCrowdSecFeedEncoder(feedModeServing)
			if err := enc.AddObject("request", request); err != nil {
				t.Fatal("feed-request-collection")
			}
			out, err := enc.EncodeEntry(zapcore.Entry{Time: time.Unix(1_700_000_000, 0)}, []zapcore.Field{zap.Int("status", 404)})
			if err != nil {
				t.Fatal("feed-encoding")
			}
			defer out.Free()
			var got map[string]any
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatal("feed-json")
			}
			if got["route_class"] != "unknown" || got["probe_class"] != "other" || got["target_path"] != tc.target {
				t.Error("feed-unicode-path")
			}
		})
	}
}

func TestCrowdSecFeedEncoderAbsoluteForm(t *testing.T) {
	t.Parallel()
	assertFeedTarget(t, feedModeServing, "http://discarded.example/api?token=discarded", 200, "api", "none", "")
	assertFeedTarget(t, feedModeServing, "http://discarded.example/unknown/sample%2fdata?token=discarded", 404, "unknown", "other", "/unknown/sample%2fdata")
}

func TestCrowdSecFeedEncoderRoutePriority(t *testing.T) {
	t.Parallel()
	assertFeedTarget(t, feedModeServing, "/api/../healthz", 404, "system", "none", "")
	assertFeedTarget(t, feedModeServing, "/_nuxt/../api", 404, "static", "none", "")
	assertFeedTarget(t, feedModeServing, "/unknown/%252541pi/private", 404, "unknown", "other", "/unknown/%252541pi/private")
}

func assertFeedTarget(t *testing.T, mode, uri string, status int, route, probe, target string) {
	t.Helper()
	line := encodeFeed(t, mode, zapcore.Entry{Time: time.Unix(1_700_000_000, 123)}, &feedRequest{
		remoteIP: "192.0.2.1", method: "GET", uri: uri,
	}, zap.Int("status", status))
	assertUniqueFeedKeys(t, line)
	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatal("feed-json")
	}
	if len(got) != 7 {
		t.Error("feed-key-count")
	}
	for key, want := range map[string]any{
		"ts": "2023-11-14T22:13:20.000000123Z", "source_ip": "192.0.2.1",
		"method": "GET", "status": float64(status), "route_class": route,
		"probe_class": probe, "target_path": target,
	} {
		if actual, exists := got[key]; !exists || actual != want {
			t.Error("feed-contract-value")
		}
	}
}

func assertUniqueFeedKeys(t *testing.T, line string) {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(line))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		t.Fatal("feed-json-object")
	}
	seen := make(map[string]bool)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			t.Fatal("feed-duplicate-key")
		}
		seen[key] = true
		var value any
		if err := decoder.Decode(&value); err != nil {
			t.Fatal("feed-json-value")
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		t.Fatal("feed-json-end")
	}
}

func TestCrowdSecFeedEncoderTargetEligibility(t *testing.T) {
	t.Parallel()
	for _, status := range []int{100, 200, 301, 399, 400, 401, 403, 404, 405, 429, 500, 599} {
		t.Run(fmt.Sprintf("status-%d", status), func(t *testing.T) {
			t.Parallel()
			probe, target := "none", ""
			if status == 400 || status == 404 || status == 405 {
				probe, target = "other", "/unknown/sample"
			}
			assertFeedTarget(t, feedModeServing, "/unknown/sample?token=discarded", status, "unknown", probe, target)
			assertFeedTarget(t, feedModeMaintenance, "/unknown/sample", status, "maintenance", "none", "")
		})
	}
}

func TestCrowdSecFeedEncoderProtectedViews(t *testing.T) {
	t.Parallel()
	routes := []struct {
		name, path, class string
	}{
		{"root", "/", "public"},
		{"well-known", "/.well-known/acme-challenge/sample", "system"},
		{"health", "/healthz", "system"}, {"ready", "/readyz.md", "system"},
		{"robots", "/robots.txt", "system"}, {"sitemap", "/sitemap.xml", "system"},
		{"llms", "/llms.txt", "system"}, {"print", "/print.md", "system"},
		{"render", "/internal-render/public", "system"},
		{"static", "/_nuxt/app.js", "static"},
		{"app", "/app/resumes", "auth"}, {"authorize", "/authorize.md", "auth"},
		{"oauth", "/oauth/callback", "auth"}, {"login", "/login", "auth"},
		{"register", "/register", "auth"}, {"forgot", "/forgot-password", "auth"},
		{"reset", "/reset-password", "auth"}, {"verify", "/verify", "auth"},
		{"verify-email", "/verify-email/sample", "auth"},
		{"api", "/api/v1/resumes", "api"}, {"mcp", "/mcp/sample", "mcp"},
		{"resume", "/resume-name", "public"}, {"resume-markdown", "/resume-name.md", "public"},
		{"guide", "/guide/mcp", "public"}, {"privacy", "/privacy.md", "public"},
		{"terms", "/terms", "public"}, {"showcase", "/showcase", "public"},
		{"templates", "/templates/sample", "public"},
	}
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			t.Parallel()
			encoded := "/"
			for _, char := range []byte(route.path[1:]) {
				encoded += fmt.Sprintf("%%%02x", char)
			}
			views := []string{
				route.path, strings.ToUpper(route.path), encoded,
				strings.ReplaceAll(encoded, "%", "%25"),
				"/unrecognized/../" + route.path[1:],
				"/unrecognized/%2e%2E/" + route.path[1:],
				"/unrecognized/%252e%252E/" + route.path[1:],
				"/unrecognized\\..\\" + route.path[1:],
			}
			for i, view := range views {
				t.Run(fmt.Sprintf("view-%d", i), func(t *testing.T) {
					for _, status := range []int{400, 404, 405} {
						assertFeedTarget(t, feedModeServing, view, status, route.class, "none", "")
					}
				})
			}
		})
	}
}

func TestCrowdSecFeedEncoderProbeClasses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, path, probe string
	}{
		{"traversal", "/unknown/../admin", "traversal"},
		{"encoded-traversal", "/unknown/%2e%2E/admin", "traversal"},
		{"double-traversal", "/unknown/%252e%252e/admin", "traversal"},
		{"backslash-traversal", "/unknown%5c..%5cadmin", "traversal"},
		{"env", "/.env", "secret-config"}, {"env-suffix", "/.ENV.local", "secret-config"},
		{"config-json", "/unknown/config.json", "secret-config"},
		{"config-yml", "/unknown/config.yml", "secret-config"},
		{"config-yaml", "/unknown/config.yaml", "secret-config"},
		{"config-php", "/unknown/config.php", "secret-config"},
		{"wordpress-config", "/unknown/wp-config.php", "secret-config"},
		{"rsa", "/unknown/id_rsa", "secret-config"}, {"ed25519", "/unknown/id_ed25519", "secret-config"},
		{"passwd", "/etc/passwd", "secret-config"}, {"shadow", "/etc/shadow", "secret-config"},
		{"git", "/.git/config", "repository"}, {"svn", "/.svn/entries", "repository"},
		{"hg", "/.hg/store", "repository"}, {"encoded-git", "/%252egit/config", "repository"},
		{"shell", "/unknown/shell.php", "web-shell"}, {"cmd", "/unknown/cmd.php", "web-shell"},
		{"c99", "/unknown/c99.php", "web-shell"}, {"r57", "/unknown/r57.php", "web-shell"},
		{"wso", "/unknown/wso.php", "web-shell"}, {"webshell", "/unknown/webshell.php", "web-shell"},
		{"jsp", "/unknown/shell.jsp", "web-shell"}, {"aspx", "/unknown/shell.aspx", "web-shell"},
		{"wp-admin", "/wp-admin/index", "framework"}, {"wp-content", "/wp-content/index", "framework"},
		{"wp-includes", "/wp-includes/index", "framework"}, {"phpmyadmin", "/phpmyadmin/index", "framework"},
		{"phpunit", "/phpunit/index", "framework"}, {"vendor", "/vendor/index", "framework"},
		{"actuator", "/actuator/index", "framework"}, {"cgi-bin", "/cgi-bin/index", "framework"},
		{"wp-login", "/unknown/wp-login.php", "framework"}, {"xmlrpc", "/unknown/xmlrpc.php", "framework"},
		{"class-priority", "/.git/.env/shell.php", "secret-config"},
		{"component-boundary", "/unknown/not.env", "other"},
		{"prefix-boundary", "/unknown/wp-admin/index", "other"},
		{"other", "/unknown/sample", "other"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertFeedTarget(t, feedModeServing, tc.path, 404, "unknown", tc.probe, tc.path)
		})
	}
}

func TestCrowdSecFeedEncoderEscapedTargets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, uri, target, probe string
	}{
		{"escape-case", "/unknown/a%2fb%2Fc", "/unknown/a%2fb%2Fc", "other"},
		{"double-encoding", "/unknown/a%252Fb", "/unknown/a%252Fb", "other"},
		{"raw-utf8", "/unknown/caf\u00e9", "", "invalid"},
		{"escaped-utf8", "/unknown/caf%c3%a9", "/unknown/caf%c3%a9", "other"},
		{"encoded-question", "/unknown/a%3Fb?token=discarded", "/unknown/a%3Fb", "other"},
		{"query", "/unknown/sample?token=discarded", "/unknown/sample", "other"},
		{"boundary", "/" + strings.Repeat("x", 255) + "/" + strings.Repeat("y", 255), "/" + strings.Repeat("x", 255) + "/" + strings.Repeat("y", 255), "other"},
		{"oversized", "/" + strings.Repeat("x", 256) + "/" + strings.Repeat("y", 255), "", "oversized"},
		{"escaped-boundary", "/unknown/" + strings.Repeat("%41", 167) + "ab", "/unknown/" + strings.Repeat("%41", 167) + "ab", "other"},
		{"escaped-oversized", "/unknown/" + strings.Repeat("%41", 168), "", "oversized"},
		{"nul", "/unknown/%00", "", "invalid"}, {"newline", "/unknown/%0a", "", "invalid"},
		{"delete", "/unknown/%7f", "", "invalid"}, {"double-control", "/unknown/%250A", "", "invalid"},
		{"unicode-control", "/unknown/%C2%85", "", "invalid"},
		{"invalid-utf8", "/unknown/%ff", "", "invalid"},
		{"percent-short", "/unknown/%a", "", "invalid"},
		{"percent-nonhex", "/unknown/%zz", "", "invalid"},
		{"decoded-percent-short", "/unknown/%25a", "", "invalid"},
		{"authority-only", "http://example.invalid", "", "invalid"},
		{"opaque", "scheme:sample", "", "invalid"},
		{"asterisk", "*", "", "invalid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assertFeedTarget(t, feedModeServing, tc.uri, 404, "unknown", tc.probe, tc.target)
		})
	}
}

type feedFields []zapcore.Field

func (fields feedFields) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	for _, field := range fields {
		field.AddTo(enc)
	}
	return nil
}

func TestCrowdSecFeedEncoderRejectsDuplicateRequestFields(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"remote_ip", "method", "uri"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			enc := newCrowdSecFeedEncoder(feedModeServing)
			if err := enc.AddObject("request", feedFields{
				zap.String("remote_ip", "192.0.2.1"), zap.String("method", "GET"),
				zap.String("uri", "/unknown/sample"), zap.String(key, "discarded"),
			}); err != nil {
				t.Fatal("feed-request-collection")
			}
			out, err := enc.EncodeEntry(zapcore.Entry{Time: time.Unix(1_700_000_000, 0)}, []zapcore.Field{zap.Int("status", 404)})
			if err != nil {
				t.Fatal("feed-encoding")
			}
			defer out.Free()
			if out.Len() != 0 {
				t.Error("feed-duplicate-base-field")
			}
		})
	}
}
