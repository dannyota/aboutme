package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

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
		if err := enc.AddObject("request", *request); err != nil {
			t.Fatal(err)
		}
	}
	encoded, err := enc.EncodeEntry(entry, fields)
	if err != nil {
		t.Fatal(err)
	}
	defer encoded.Free()
	return encoded.String()
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

	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("decode feed record: %v", err)
	}
	want := map[string]any{
		"ts":          "2026-10-10T01:09:10.123456789Z",
		"source_ip":   "2001:db8::1",
		"method":      "GET",
		"status":      float64(200),
		"route_class": "public",
	}
	if len(got) != len(want) {
		t.Fatalf("feed keys = %#v, want exactly %#v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("feed %s = %#v, want %#v", key, got[key], value)
		}
	}
	for _, leak := range []string{
		"resume-name", "querymarker-1a2b", "secret-host", "fieldmarker-5c8d",
		"usermarker-3d4e", "headermarker-6f7a", "198.51.100.77", "49152",
	} {
		if strings.Contains(line, leak) {
			t.Errorf("feed record contains %q: %s", leak, line)
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
				t.Fatal(err)
			}
			if got.RouteClass != want {
				t.Errorf("route class = %q, want %q", got.RouteClass, want)
			}
		})
	}

	line := encodeFeed(t, feedModeMaintenance, entry, &feedRequest{
		remoteIP: "192.0.2.1",
		method:   "GET",
		uri:      "/api/v1/private-marker",
	}, zap.Int("status", 503))
	if !strings.Contains(line, `"route_class":"maintenance"`) || strings.Contains(line, "private-marker") {
		t.Fatalf("maintenance feed record = %s", line)
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
			t.Errorf("method %q record = %s", method, line)
		}
	}
	line := encodeFeed(t, feedModeServing, entry, &feedRequest{
		remoteIP: "192.0.2.1",
		method:   "TRACE-secret-marker",
		uri:      "/api",
	}, zap.Int("status", 405))
	if !strings.Contains(line, `"method":"OTHER"`) || strings.Contains(line, "secret-marker") {
		t.Fatalf("other method record = %s", line)
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
		{name: "uri", mode: feedModeServing, entry: validEntry, request: &feedRequest{remoteIP: "192.0.2.1", method: "GET", uri: "http://secret.example/api"}, fields: []zapcore.Field{zap.Int("status", 200)}},
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
				t.Errorf("malformed record was emitted: %s", line)
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
		t.Fatalf("mapped IPv4 record = %s", line)
	}
}

func TestCrowdSecFeedEncoderCloneKeepsRequest(t *testing.T) {
	t.Parallel()

	enc := newCrowdSecFeedEncoder(feedModeServing)
	if err := enc.AddObject("request", feedRequest{remoteIP: "192.0.2.4", method: "GET", uri: "/api"}); err != nil {
		t.Fatal(err)
	}
	clone := enc.Clone()
	encoded, err := clone.EncodeEntry(zapcore.Entry{Time: time.Unix(1_700_000_000, 0)}, []zapcore.Field{zap.Int("status", 201)})
	if err != nil {
		t.Fatal(err)
	}
	defer encoded.Free()
	if !strings.Contains(encoded.String(), `"source_ip":"192.0.2.4"`) {
		t.Fatalf("cloned encoder lost the request: %s", encoded.String())
	}
}
