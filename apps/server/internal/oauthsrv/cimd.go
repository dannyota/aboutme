package oauthsrv

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var errCIMDInvalid = errors.New("oauth client metadata invalid")

// CIMDMetadata retains only validated display and redirect values. Its fields
// stay private and each resolver result owns its redirect slice.
type CIMDMetadata struct {
	id        string
	host      string
	name      string
	redirects []string
}

func parseCIMDURL(raw, publicHost string) (*url.URL, error) {
	if len(raw) < 9 || len(raw) > 256 || !strings.HasPrefix(raw, "https://") {
		return nil, errCIMDInvalid
	}
	for _, c := range []byte(raw) {
		if c < 0x21 || c > 0x7e || c == '#' || c == '\\' {
			return nil, errCIMDInvalid
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Host != u.Hostname() {
		return nil, errCIMDInvalid
	}
	host := u.Hostname()
	if host == "" || host != strings.ToLower(host) || strings.HasSuffix(host, ".") || !strings.Contains(host, ".") {
		return nil, errCIMDInvalid
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return nil, errCIMDInvalid
	}
	allNumeric := true
	for _, label := range strings.Split(host, ".") {
		if len(label) < 1 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return nil, errCIMDInvalid
		}
		for _, c := range []byte(label) {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return nil, errCIMDInvalid
			}
		}
		_, decimalErr := strconv.ParseUint(label, 10, 64)
		_, hexErr := strconv.ParseUint(strings.TrimPrefix(label, "0x"), 16, 64)
		if decimalErr != nil && (!strings.HasPrefix(label, "0x") || hexErr != nil) {
			allNumeric = false
		}
	}
	if allNumeric || host == publicHost || strings.HasSuffix(host, "."+publicHost) {
		return nil, errCIMDInvalid
	}
	for _, suffix := range []string{".localhost", ".local", ".internal", ".home.arpa", ".onion"} {
		if strings.HasSuffix(host, suffix) {
			return nil, errCIMDInvalid
		}
	}
	path := u.EscapedPath()
	if len(path) < 2 || path[0] != '/' {
		return nil, errCIMDInvalid
	}
	for _, rawSegment := range strings.Split(path, "/") {
		decoded, err := url.PathUnescape(rawSegment)
		if err != nil {
			return nil, errCIMDInvalid
		}
		for _, segment := range strings.Split(decoded, "/") {
			if segment == "." || segment == ".." {
				return nil, errCIMDInvalid
			}
		}
	}
	return u, nil
}

const cimdRefusedRanges = `
0.0.0.0/8 10.0.0.0/8 100.64.0.0/10 127.0.0.0/8 169.254.0.0/16
172.16.0.0/12 192.0.0.0/24 192.0.2.0/24 192.31.196.0/24 192.52.193.0/24
192.88.99.0/24 192.168.0.0/16 192.175.48.0/24 198.18.0.0/15 198.51.100.0/24
203.0.113.0/24 224.0.0.0/4 240.0.0.0/4
::/128 ::1/128 64:ff9b::/96 64:ff9b:1::/48 100::/64 100:0:0:1::/64
2001::/23 2001:db8::/32 2002::/16 2620:4f:8000::/48 3fff::/20 5f00::/16
fc00::/7 fe80::/10 fec0::/10 ff00::/8`

var cimdRefusedPrefixes = func() []netip.Prefix {
	var result []netip.Prefix
	for _, raw := range strings.Fields(cimdRefusedRanges) {
		result = append(result, netip.MustParsePrefix(raw))
	}
	return result
}()

func cimdAddressAllowed(addr netip.Addr) bool {
	if !addr.IsValid() || addr.Zone() != "" {
		return false
	}
	addr = addr.Unmap()
	if addr.Is6() && !netip.MustParsePrefix("2000::/3").Contains(addr) {
		return false
	}
	for _, prefix := range cimdRefusedPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

// checkCIMDJSON checks ignored nested values before fields are discarded.
func checkCIMDJSON(d *json.Decoder) error {
	t, err := d.Token()
	if err != nil {
		return errCIMDInvalid
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return errCIMDInvalid
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return errCIMDInvalid
			}
			seen[name] = true
			if err := checkCIMDJSON(d); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim('}') {
			return errCIMDInvalid
		}
	case '[':
		for d.More() {
			if err := checkCIMDJSON(d); err != nil {
				return err
			}
		}
		end, err := d.Token()
		if err != nil || end != json.Delim(']') {
			return errCIMDInvalid
		}
	default:
		return errCIMDInvalid
	}
	return nil
}

func validateCIMDDocument(raw []byte, id, host string) (CIMDMetadata, error) {
	bad := CIMDMetadata{}
	if len(raw) > 5120 || !utf8.Valid(raw) || len(bytes.TrimSpace(raw)) == 0 || bytes.TrimSpace(raw)[0] != '{' {
		return bad, errCIMDInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := checkCIMDJSON(d); err != nil {
		return bad, errCIMDInvalid
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return bad, errCIMDInvalid
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return bad, errCIMDInvalid
	}
	for _, key := range []string{"client_secret", "client_secret_expires_at", "jwks", "jwks_uri"} {
		if _, ok := fields[key]; ok {
			return bad, errCIMDInvalid
		}
	}
	text := func(key string, required bool) (string, error) {
		value, ok := fields[key]
		if !ok && !required {
			return "", nil
		}
		var result string
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &result) != nil {
			return "", errCIMDInvalid
		}
		return result, nil
	}
	list := func(key string, required bool) ([]string, error) {
		value, ok := fields[key]
		if !ok && !required {
			return nil, nil
		}
		var members []json.RawMessage
		if !ok || json.Unmarshal(value, &members) != nil || members == nil {
			return nil, errCIMDInvalid
		}
		result := make([]string, 0, len(members))
		for _, member := range members {
			var s string
			if bytes.Equal(bytes.TrimSpace(member), []byte("null")) || json.Unmarshal(member, &s) != nil {
				return nil, errCIMDInvalid
			}
			result = append(result, s)
		}
		return result, nil
	}
	clientID, err := text("client_id", true)
	if err != nil || clientID != id {
		return bad, errCIMDInvalid
	}
	name, err := text("client_name", true)
	if err != nil {
		return bad, errCIMDInvalid
	}
	name, err = ValidateClientName(name)
	if err != nil {
		return bad, errCIMDInvalid
	}
	method, err := text("token_endpoint_auth_method", true)
	if err != nil || method != "none" {
		return bad, errCIMDInvalid
	}
	redirects, err := list("redirect_uris", true)
	if err != nil || len(redirects) < 1 || len(redirects) > 5 {
		return bad, errCIMDInvalid
	}
	for _, raw := range redirects {
		if !validCIMDRedirect(raw, host) {
			return bad, errCIMDInvalid
		}
	}
	grants, err := list("grant_types", false)
	if err != nil || grants != nil && !slices.Contains(grants, "authorization_code") {
		return bad, errCIMDInvalid
	}
	responses, err := list("response_types", false)
	if err != nil || responses != nil && !validResponseTypesSet(responses) {
		return bad, errCIMDInvalid
	}
	scope, err := text("scope", false)
	if err != nil {
		return bad, errCIMDInvalid
	}
	if _, ok := fields["scope"]; ok {
		if _, scopeErr := ParseScopes(scope); scopeErr != nil {
			return bad, errCIMDInvalid
		}
	}
	application, err := text("application_type", false)
	if err != nil {
		return bad, errCIMDInvalid
	}
	if _, ok := fields["application_type"]; ok {
		if application != "native" && application != "web" {
			return bad, errCIMDInvalid
		}
		for _, raw := range redirects {
			if application == "native" && !nativeLoopbackRedirect(raw) || application == "web" && !webRedirect(raw) {
				return bad, errCIMDInvalid
			}
		}
	}
	return CIMDMetadata{id: id, host: host, name: name, redirects: redirects}, nil
}

func validCIMDRedirect(raw, host string) bool {
	if ValidateRedirectURI(raw) != nil {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme == "https" {
		return strings.EqualFold(u.Hostname(), host) && !strings.HasSuffix(u.Hostname(), ".")
	}
	if strings.HasSuffix(u.Host, ":") {
		return false
	}
	if port := u.Port(); port != "" {
		for _, c := range []byte(port) {
			if c < '0' || c > '9' {
				return false
			}
		}
		p, err := strconv.ParseUint(port, 10, 16)
		if err != nil || p == 0 {
			return false
		}
	}
	return true
}

func cimdRedirectWithoutPort(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Port() == "" {
		return raw
	}
	// Remove only the literal port; reserialization could alter path spelling.
	start := len(u.Scheme) + 3
	return raw[:start] + strings.TrimSuffix(u.Host, ":"+u.Port()) + raw[start+len(u.Host):]
}

func (m CIMDMetadata) matchesRedirect(raw string) bool {
	if !validCIMDRedirect(raw, m.host) {
		return false
	}
	for _, entry := range m.redirects {
		if nativeLoopbackRedirect(raw) {
			if cimdRedirectWithoutPort(raw) == cimdRedirectWithoutPort(entry) {
				return true
			}
		} else if raw == entry {
			return true
		}
	}
	return false
}

func cimdCacheLifetime(header string, floor, ceiling time.Duration) time.Duration {
	values := map[string][]string{}
	for _, raw := range strings.Split(header, ",") {
		key, value, _ := strings.Cut(strings.TrimSpace(raw), "=")
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "private" || key == "no-store" || key == "no-cache" {
			return floor
		}
		values[key] = append(values[key], strings.TrimSpace(value))
	}
	selected, ok := values["s-maxage"]
	if !ok {
		selected = values["max-age"]
	}
	if len(selected) != 1 {
		return floor
	}
	raw := selected[0]
	if strings.HasPrefix(raw, `"`) || strings.HasSuffix(raw, `"`) {
		if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
			return floor
		}
		raw = raw[1 : len(raw)-1]
	}
	if raw == "" {
		return floor
	}
	for _, c := range []byte(raw) {
		if c < '0' || c > '9' {
			return floor
		}
	}
	seconds, err := strconv.ParseUint(raw, 10, 63)
	if err != nil {
		return floor
	}
	limit := int64(ceiling / time.Second)
	if limit < 0 {
		return floor
	}
	if seconds > uint64(limit) {
		return ceiling
	}
	duration := time.Duration(seconds) * time.Second
	if duration < floor {
		return floor
	}
	return duration
}
