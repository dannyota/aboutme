package main

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

const (
	feedModeServing     = "serving"
	feedModeMaintenance = "maintenance"
)

var (
	feedBufferPool = buffer.NewPool()
	publicSlugPath = regexp.MustCompile(`^/[a-z0-9]+(?:-[a-z0-9]+)*(?:\.md)?$`)
)

func init() {
	caddy.RegisterModule(CrowdSecFeedEncoder{})
}

// CrowdSecFeedEncoder emits only the bounded fields CrowdSec needs. It reads
// the socket address and route shape from Caddy's request object, then discards
// that object and every other access-log field.
type CrowdSecFeedEncoder struct {
	Mode            string `json:"mode,omitempty"`
	zapcore.Encoder `json:"-"`
	request         crowdSecFeedRequest
}

type crowdSecFeedRequest struct {
	sourceIP string
	method   string
	uri      string
	valid    bool
}

type crowdSecFeedRecord struct {
	Timestamp  string `json:"ts"`
	SourceIP   string `json:"source_ip"`
	Method     string `json:"method"`
	Status     int    `json:"status"`
	RouteClass string `json:"route_class"`
}

func (CrowdSecFeedEncoder) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "caddy.logging.encoders.crowdsec_feed",
		New: func() caddy.Module { return new(CrowdSecFeedEncoder) },
	}
}

func (e *CrowdSecFeedEncoder) Provision(caddy.Context) error {
	if !validFeedMode(e.Mode) {
		return fmt.Errorf("invalid feed mode")
	}
	e.Encoder = discardEncoder()
	return nil
}

func (e *CrowdSecFeedEncoder) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	d.Next()
	if !d.NextArg() {
		return d.ArgErr()
	}
	e.Mode = d.Val()
	if !validFeedMode(e.Mode) {
		return d.Errf("unknown CrowdSec feed mode %q", e.Mode)
	}
	if d.NextArg() || d.NextBlock(0) {
		return d.ArgErr()
	}
	return nil
}

func newCrowdSecFeedEncoder(mode string) *CrowdSecFeedEncoder {
	return &CrowdSecFeedEncoder{Mode: mode, Encoder: discardEncoder()}
}

func discardEncoder() zapcore.Encoder {
	return zapcore.NewJSONEncoder(zapcore.EncoderConfig{})
}

func (e *CrowdSecFeedEncoder) AddObject(key string, marshaler zapcore.ObjectMarshaler) error {
	if key != "request" {
		return nil
	}
	collector := new(crowdSecRequestCollector)
	if err := marshaler.MarshalLogObject(collector); err != nil {
		e.request = crowdSecFeedRequest{}
		return nil
	}
	collector.request.valid = collector.sourceIPCount == 1 && collector.methodCount == 1 && collector.uriCount == 1
	e.request = collector.request
	return nil
}

func (e *CrowdSecFeedEncoder) Clone() zapcore.Encoder {
	clone := *e
	clone.Encoder = e.Encoder.Clone()
	return &clone
}

func (e *CrowdSecFeedEncoder) EncodeEntry(entry zapcore.Entry, fields []zapcore.Field) (*buffer.Buffer, error) {
	out := feedBufferPool.Get()
	status, ok := feedStatus(fields)
	timestamp := entry.Time.UTC()
	if !ok || !validFeedMode(e.Mode) || !e.request.valid || entry.Time.IsZero() || timestamp.Year() < 0 || timestamp.Year() > 9999 {
		return out, nil
	}
	ip, err := netip.ParseAddr(e.request.sourceIP)
	if err != nil || ip.Zone() != "" || e.request.method == "" {
		return out, nil
	}
	routeClass, ok := feedRouteClass(e.Mode, e.request.uri)
	if !ok {
		return out, nil
	}
	record := crowdSecFeedRecord{
		Timestamp:  timestamp.Format(time.RFC3339Nano),
		SourceIP:   ip.Unmap().String(),
		Method:     feedMethod(e.request.method),
		Status:     status,
		RouteClass: routeClass,
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		return out, nil
	}
	_, _ = out.Write(encoded)
	out.AppendByte('\n')
	return out, nil
}

func validFeedMode(mode string) bool {
	return mode == feedModeServing || mode == feedModeMaintenance
}

func feedStatus(fields []zapcore.Field) (int, bool) {
	var status int
	found := false
	for _, field := range fields {
		if field.Key != "status" {
			continue
		}
		if found || field.Type != zapcore.Int64Type || field.Integer < 100 || field.Integer > 599 {
			return 0, false
		}
		status = int(field.Integer)
		found = true
	}
	return status, found
}

func feedMethod(method string) string {
	switch method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
		return method
	default:
		return "OTHER"
	}
}

func feedRouteClass(mode, requestURI string) (string, bool) {
	parsed, err := url.ParseRequestURI(requestURI)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") {
		return "", false
	}
	if mode == feedModeMaintenance {
		return "maintenance", true
	}
	path := parsed.Path
	switch {
	case pathShape(path, "/.well-known", false),
		pathShape(path, "/healthz", true),
		pathShape(path, "/readyz", true),
		pathShape(path, "/robots.txt", false),
		pathShape(path, "/sitemap.xml", false),
		pathShape(path, "/llms.txt", false),
		pathShape(path, "/internal-render", true),
		pathShape(path, "/print", true):
		return "system", true
	case pathShape(path, "/_nuxt", false):
		return "static", true
	case pathShape(path, "/app", false),
		pathShape(path, "/authorize", true),
		pathShape(path, "/oauth", true),
		pathShape(path, "/login", true),
		pathShape(path, "/register", true),
		pathShape(path, "/forgot-password", true),
		pathShape(path, "/reset-password", true),
		pathShape(path, "/verify", true),
		pathShape(path, "/verify-email", true):
		return "auth", true
	case pathShape(path, "/api", false):
		return "api", true
	case pathShape(path, "/mcp", false):
		return "mcp", true
	case path == "/",
		validPublicSlugPath(path) && !reservedPublicSlugPath(path),
		pathShape(path, "/guide", true),
		pathShape(path, "/privacy", true),
		pathShape(path, "/terms", true),
		pathShape(path, "/showcase", true),
		pathShape(path, "/templates", true):
		return "public", true
	default:
		return "unknown", true
	}
}

func pathShape(path, root string, markdown bool) bool {
	return path == root || strings.HasPrefix(path, root+"/") || markdown && path == root+".md"
}

func validPublicSlugPath(path string) bool {
	if !publicSlugPath.MatchString(path) {
		return false
	}
	length := len(path)
	if strings.HasSuffix(path, ".md") {
		return length >= 8 && length <= 34
	}
	return length >= 5 && length <= 31
}

func reservedPublicSlugPath(path string) bool {
	path = strings.TrimSuffix(path, ".md")
	return path == "/admin" || path == "/people"
}

// crowdSecRequestCollector is a strict allowlist. Nested objects, headers,
// ports, hosts, TLS data, bodies, and every non-string value are ignored.
type crowdSecRequestCollector struct {
	request       crowdSecFeedRequest
	sourceIPCount int
	methodCount   int
	uriCount      int
}

func (c *crowdSecRequestCollector) AddString(key, value string) {
	switch key {
	case "remote_ip":
		c.sourceIPCount++
		c.request.sourceIP = value
	case "method":
		c.methodCount++
		c.request.method = value
	case "uri":
		c.uriCount++
		c.request.uri = value
	}
}

func (*crowdSecRequestCollector) AddArray(string, zapcore.ArrayMarshaler) error   { return nil }
func (*crowdSecRequestCollector) AddObject(string, zapcore.ObjectMarshaler) error { return nil }
func (*crowdSecRequestCollector) AddBinary(string, []byte)                        {}
func (*crowdSecRequestCollector) AddByteString(string, []byte)                    {}
func (*crowdSecRequestCollector) AddBool(string, bool)                            {}
func (*crowdSecRequestCollector) AddComplex128(string, complex128)                {}
func (*crowdSecRequestCollector) AddComplex64(string, complex64)                  {}
func (*crowdSecRequestCollector) AddDuration(string, time.Duration)               {}
func (*crowdSecRequestCollector) AddFloat64(string, float64)                      {}
func (*crowdSecRequestCollector) AddFloat32(string, float32)                      {}
func (*crowdSecRequestCollector) AddInt(string, int)                              {}
func (*crowdSecRequestCollector) AddInt64(string, int64)                          {}
func (*crowdSecRequestCollector) AddInt32(string, int32)                          {}
func (*crowdSecRequestCollector) AddInt16(string, int16)                          {}
func (*crowdSecRequestCollector) AddInt8(string, int8)                            {}
func (*crowdSecRequestCollector) AddTime(string, time.Time)                       {}
func (*crowdSecRequestCollector) AddUint(string, uint)                            {}
func (*crowdSecRequestCollector) AddUint64(string, uint64)                        {}
func (*crowdSecRequestCollector) AddUint32(string, uint32)                        {}
func (*crowdSecRequestCollector) AddUint16(string, uint16)                        {}
func (*crowdSecRequestCollector) AddUint8(string, uint8)                          {}
func (*crowdSecRequestCollector) AddUintptr(string, uintptr)                      {}
func (*crowdSecRequestCollector) AddReflected(string, any) error                  { return nil }
func (*crowdSecRequestCollector) OpenNamespace(string)                            {}

var (
	_ caddy.Module          = (*CrowdSecFeedEncoder)(nil)
	_ caddy.Provisioner     = (*CrowdSecFeedEncoder)(nil)
	_ caddyfile.Unmarshaler = (*CrowdSecFeedEncoder)(nil)
	_ zapcore.Encoder       = (*CrowdSecFeedEncoder)(nil)
	_ zapcore.ObjectEncoder = (*crowdSecRequestCollector)(nil)
)
