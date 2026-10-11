package oauthsrv

import (
	"net/url"
	"strings"
)

// IsCanonicalOrigin reports whether raw is a canonical HTTP(S) origin for
// OAuth and MCP configuration. It validates without normalizing the input.
func IsCanonicalOrigin(raw string) bool {
	if raw == "" || len(raw) > 512 {
		return false
	}
	for i := range raw {
		if raw[i] < 0x21 || raw[i] > 0x7e {
			return false
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Host == "" || u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return false
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := u.Port()
	if port != "" && (scheme != "http" || port != "80") && (scheme != "https" || port != "443") {
		host += ":" + port
	}
	return raw == scheme+"://"+host
}
