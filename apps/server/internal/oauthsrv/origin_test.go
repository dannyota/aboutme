package oauthsrv

import (
	"strings"
	"testing"
)

func TestIsCanonicalOrigin(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"https", "https://example.com", true},
		{"http", "http://example.com", true},
		{"localhost", "http://localhost", true},
		{"IPv4", "http://127.0.0.1", true},
		{"uppercase scheme", "HTTPS://example.com", false},
		{"uppercase host", "https://Example.COM", false},
		{"uppercase scheme and host", "HTTP://EXAMPLE.COM", false},
		{"default https port", "https://example.com:443", false},
		{"default http port", "http://example.com:80", false},
		{"explicit https port", "https://example.com:8443", true},
		{"explicit http port", "http://example.com:8080", true},
		{"https with http port", "https://example.com:80", true},
		{"http with https port", "http://example.com:443", true},
		{"zero port", "https://example.com:0", true},
		{"port above transport range", "https://example.com:65536", true},
		{"leading zero https port", "https://example.com:0443", true},
		{"leading zero http port", "http://example.com:080", true},
		{"empty port", "https://example.com:", false},
		{"nonnumeric port", "https://example.com:https", false},
		{"negative port", "https://example.com:-1", false},
		{"IPv6 loopback", "http://[::1]", true},
		{"IPv6 literal", "https://[2001:db8::1]", true},
		{"expanded IPv6 literal", "https://[2001:0db8:0:0:0:0:0:1]", true},
		{"IPv4 mapped IPv6 literal", "https://[::ffff:192.0.2.1]", true},
		{"uppercase IPv6 literal", "https://[2001:DB8::1]", false},
		{"IPv6 explicit port", "https://[2001:db8::1]:8443", true},
		{"IPv6 default https port", "https://[::1]:443", false},
		{"IPv6 default http port", "http://[::1]:80", false},
		{"IPv6 empty port", "https://[::1]:", false},
		{"IPv6 zone", "http://[fe80::1%25eth0]", false},
		{"unbracketed IPv6", "https://2001:db8::1", false},
		{"unclosed IPv6 bracket", "https://[::1", false},
		{"username", "https://user@example.com", false},
		{"password", "https://user:password@example.com", false},
		{"empty userinfo", "https://@example.com", false},
		{"encoded userinfo", "https://user%40name@example.com", false},
		{"path", "https://example.com/mcp", false},
		{"encoded path", "https://example.com/%2f", false},
		{"trailing slash", "https://example.com/", false},
		{"multiple trailing slashes", "https://example.com//", false},
		{"IPv6 trailing slash", "https://[::1]/", false},
		{"query", "https://example.com?key=value", false},
		{"empty query", "https://example.com?", false},
		{"fragment", "https://example.com#section", false},
		{"empty fragment", "https://example.com#", false},
		{"empty", "", false},
		{"missing scheme", "example.com", false},
		{"scheme relative", "//example.com", false},
		{"opaque URL", "https:example.com", false},
		{"unsupported scheme", "ftp://example.com", false},
		{"empty host", "https://", false},
		{"empty hostname with port", "https://:8443", false},
		{"leading space", " https://example.com", false},
		{"trailing space", "https://example.com ", false},
		{"embedded space", "https://exam ple.com", false},
		{"tab", "https://example.com\t", false},
		{"newline", "https://example.com\n", false},
		{"control byte", "https://example.com\x00", false},
		{"DEL byte", "https://example.com\x7f", false},
		{"non ASCII host", "https://exämple.com", false},
		{"punycode host", "https://xn--exmple-cua.com", true},
		{"trailing host dot", "https://example.com.", true},
		{"underscore host", "https://example_name", true},
		{"escaped host", "https://%65xample.com", false},
		{"backslash", "https://example.com\\path", false},
		{"512 bytes", "https://" + strings.Repeat("a", 504), true},
		{"513 bytes", "https://" + strings.Repeat("a", 505), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := IsCanonicalOrigin(tc.raw); got != tc.want {
				t.Errorf("IsCanonicalOrigin(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}
