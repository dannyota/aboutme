package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap/buffer"
	"go.uber.org/zap/zapcore"
)

const (
	feedModeServing     = "serving"
	feedModeMaintenance = "maintenance"
	feedTargetLimit     = 512
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
	url      url.URL
	invalid  bool
	valid    bool
}

type crowdSecFeedRecord struct {
	Timestamp  string `json:"ts"`
	SourceIP   string `json:"source_ip"`
	Method     string `json:"method"`
	Status     int    `json:"status"`
	RouteClass string `json:"route_class"`
	ProbeClass string `json:"probe_class"`
	TargetPath string `json:"target_path"`
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
	e.request = crowdSecFeedRequest{}
	logged, ok := marshaler.(caddyhttp.LoggableHTTPRequest)
	if !ok || logged.Request == nil {
		return nil
	}
	request := logged.Request
	// Caddy snapshots the original parsed URL before its handler chain. Reading
	// the object preserves RawPath, which its generic marshaler discards.
	if original, exists := request.Context().Value(caddyhttp.OriginalRequestCtxKey).(http.Request); exists {
		request = &original
	}
	ip, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		return nil
	}
	e.request = crowdSecFeedRequest{sourceIP: ip, method: request.Method, valid: true, invalid: request.URL == nil}
	if request.URL != nil {
		e.request.url = url.URL{Path: request.URL.Path, RawPath: request.URL.RawPath}
		e.request.invalid = request.URL.Opaque != ""
	}
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
	routeClass, probeClass, targetPath := feedTarget(e.Mode, e.request.url, e.request.invalid, status)
	record := crowdSecFeedRecord{
		Timestamp:  timestamp.Format(time.RFC3339Nano),
		SourceIP:   ip.Unmap().String(),
		Method:     feedMethod(e.request.method),
		Status:     status,
		RouteClass: routeClass,
		ProbeClass: probeClass,
		TargetPath: targetPath,
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

func feedTarget(mode string, target url.URL, invalid bool, status int) (string, string, string) {
	if mode == feedModeMaintenance {
		return "maintenance", "none", ""
	}
	if invalid || !strings.HasPrefix(target.Path, "/") {
		if status == 400 || status == 404 || status == 405 {
			return "unknown", "invalid", ""
		}
		return "unknown", "none", ""
	}
	route, firstValid := feedPathClass(target.Path, false)
	secondRoute, secondValid := feedPathClass(target.Path, true)
	route = feedPriority(route, secondRoute)
	if route != "unknown" || status != 400 && status != 404 && status != 405 {
		return route, "none", ""
	}
	if !firstValid || !secondValid || !feedRawPathValid(target) {
		return route, "invalid", ""
	}
	// Escaping can only grow Path. Do not allocate escaped or decoded copies
	// of an oversized target; classification above uses fixed-size buffers.
	if len(target.Path) > feedTargetLimit || len(target.RawPath) > feedTargetLimit {
		return route, "oversized", ""
	}
	escaped := target.EscapedPath()
	if !strings.HasPrefix(escaped, "/") {
		return route, "invalid", ""
	}
	if len(escaped) > feedTargetLimit {
		return route, "oversized", ""
	}
	decoded, _ := url.PathUnescape(target.Path) // the scanner validated every triplet
	return route, feedProbeClass([]string{target.Path, decoded}), escaped
}

func feedPriority(first, second string) string {
	for _, class := range []string{"system", "static", "auth", "api", "mcp", "public"} {
		if first == class || second == class {
			return class
		}
	}
	return "unknown"
}

func feedRootClass(root string, leaf bool) string {
	path := "/" + root
	switch {
	case pathShape(path, "/.well-known", false),
		pathShape(path, "/healthz", leaf),
		pathShape(path, "/readyz", leaf),
		pathShape(path, "/robots.txt", false),
		pathShape(path, "/sitemap.xml", false),
		pathShape(path, "/llms.txt", false),
		pathShape(path, "/internal-render", leaf),
		pathShape(path, "/print", leaf):
		return "system"
	case pathShape(path, "/_nuxt", false):
		return "static"
	case pathShape(path, "/app", false),
		pathShape(path, "/authorize", leaf),
		pathShape(path, "/oauth", leaf),
		pathShape(path, "/login", leaf),
		pathShape(path, "/register", leaf),
		pathShape(path, "/forgot-password", leaf),
		pathShape(path, "/reset-password", leaf),
		pathShape(path, "/verify", leaf),
		pathShape(path, "/verify-email", leaf):
		return "auth"
	case pathShape(path, "/api", false):
		return "api"
	case pathShape(path, "/mcp", false):
		return "mcp"
	case path == "/" && leaf,
		leaf && validPublicSlugPath(path) && !reservedPublicSlugPath(path),
		pathShape(path, "/guide", leaf),
		pathShape(path, "/privacy", leaf),
		pathShape(path, "/terms", leaf),
		pathShape(path, "/showcase", leaf),
		pathShape(path, "/templates", leaf):
		return "public"
	default:
		return "unknown"
	}
}

// feedPathClass examines one decoded view without allocating a copy of a
// visitor-sized path. Only the first component and normalized depth matter
// for protected routes; a long component cannot match a protected root.
func feedPathClass(path string, decode bool) (string, bool) {
	reader := bufio.NewReaderSize(&feedPathReader{path: path, decode: decode}, feedTargetLimit)
	var component [34]byte
	length, depth, rawDepth := 0, 0, 0
	root, rawRoot := "", ""
	valid := true
	finish := func() {
		if length == 0 {
			return
		}
		value := "\x00"
		if length <= len(component) {
			value = string(component[:length])
		}
		if rawDepth == 0 {
			rawRoot = value
		}
		rawDepth++
		switch value {
		case ".":
		case "..":
			if depth > 0 {
				depth--
			}
			if depth == 0 {
				root = ""
			}
		default:
			if depth == 0 {
				root = value
			}
			depth++
		}
		length = 0
	}
	for {
		char, size, err := reader.ReadRune()
		if err != nil {
			valid = valid && err == io.EOF
			break
		}
		if char == utf8.RuneError && size == 1 || unicode.IsControl(char) {
			valid = false
		}
		if char == '/' || char == '\\' {
			finish()
			continue
		}
		if char >= 'A' && char <= 'Z' {
			char += 'a' - 'A'
		}
		if char > 127 {
			char = 0 // protected route roots and resume handles contain ASCII only
		}
		if length < len(component) {
			component[length] = byte(char)
		}
		length++
	}
	finish()
	return feedPriority(feedRootClass(rawRoot, valid && rawDepth <= 1), feedRootClass(root, valid && depth <= 1)), valid
}

type feedPathReader struct {
	path   string
	offset int
	decode bool
	failed bool
}

func (r *feedPathReader) Read(out []byte) (int, error) {
	for i := range out {
		if r.offset == len(r.path) {
			return i, io.EOF
		}
		value := r.path[r.offset]
		r.offset++
		if r.decode && value == '%' {
			if r.offset+1 >= len(r.path) {
				r.failed = true
				return i, io.ErrUnexpectedEOF
			}
			high, highOK := feedHex(r.path[r.offset])
			low, lowOK := feedHex(r.path[r.offset+1])
			if !highOK || !lowOK {
				r.failed = true
				return i, io.ErrUnexpectedEOF
			}
			value = high<<4 | low
			r.offset += 2
		}
		out[i] = value
	}
	return len(out), nil
}

func feedHex(value byte) (byte, bool) {
	switch {
	case value >= '0' && value <= '9':
		return value - '0', true
	case value >= 'a' && value <= 'f':
		return value - 'a' + 10, true
	case value >= 'A' && value <= 'F':
		return value - 'A' + 10, true
	default:
		return 0, false
	}
}

func feedRawPathValid(target url.URL) bool {
	if target.RawPath == "" {
		return true
	}
	if !strings.HasPrefix(target.RawPath, "/") {
		return false
	}
	for i := 0; i < len(target.RawPath); i++ {
		value := target.RawPath[i]
		if value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' ||
			strings.ContainsRune("-._~!$&'()*+,;=:@/[]%", rune(value)) {
			continue
		}
		return false
	}
	reader := feedPathReader{path: target.RawPath, decode: true}
	var one [1]byte
	for i := 0; ; i++ {
		n, err := reader.Read(one[:])
		if n == 0 {
			return err == io.EOF && !reader.failed && i == len(target.Path)
		}
		if i >= len(target.Path) || one[0] != target.Path[i] {
			return false
		}
	}
}

func feedProbeClass(views []string) string {
	components := make([][]string, 0, len(views))
	for _, view := range views {
		components = append(components, strings.Split(feedLowerASCII(strings.ReplaceAll(view, "\\", "/")), "/"))
	}
	for _, parts := range components {
		for _, part := range parts {
			if part == ".." {
				return "traversal"
			}
		}
	}
	for _, parts := range components {
		base := parts[len(parts)-1]
		for _, part := range parts {
			if part == ".env" || strings.HasPrefix(part, ".env.") {
				return "secret-config"
			}
		}
		if stringIn(base, "config.json", "config.yml", "config.yaml", "config.php", "wp-config.php", "id_rsa", "id_ed25519") ||
			strings.Join(parts, "/") == "/etc/passwd" || strings.Join(parts, "/") == "/etc/shadow" {
			return "secret-config"
		}
	}
	for _, parts := range components {
		for _, part := range parts {
			if stringIn(part, ".git", ".svn", ".hg") {
				return "repository"
			}
		}
	}
	for _, parts := range components {
		if stringIn(parts[len(parts)-1], "shell.php", "cmd.php", "c99.php", "r57.php", "wso.php", "webshell.php", "shell.jsp", "shell.aspx") {
			return "web-shell"
		}
	}
	for _, parts := range components {
		if len(parts) > 1 && stringIn(parts[1], "wp-admin", "wp-content", "wp-includes", "phpmyadmin", "phpunit", "vendor", "actuator", "cgi-bin") ||
			stringIn(parts[len(parts)-1], "wp-login.php", "xmlrpc.php") {
			return "framework"
		}
	}
	return "other"
}

func stringIn(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}

func feedLowerASCII(value string) string {
	lower := []byte(value)
	for i, char := range lower {
		if char >= 'A' && char <= 'Z' {
			lower[i] += 'a' - 'A'
		}
	}
	return string(lower)
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

var (
	_ caddy.Module          = (*CrowdSecFeedEncoder)(nil)
	_ caddy.Provisioner     = (*CrowdSecFeedEncoder)(nil)
	_ caddyfile.Unmarshaler = (*CrowdSecFeedEncoder)(nil)
	_ zapcore.Encoder       = (*CrowdSecFeedEncoder)(nil)
)
