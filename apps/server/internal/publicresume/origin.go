package publicresume

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

// PublicOrigin is the single canonical viewer-facing origin.
type PublicOrigin struct{ value string }

// ParsePublicOrigin accepts the configured canonical origin only. The source
// value is never derived from a viewer request.
func ParsePublicOrigin(raw, environment string) (PublicOrigin, error) {
	if raw == "" || len(raw) > 512 {
		return PublicOrigin{}, errors.New("public origin is empty or too long")
	}
	for i := range raw {
		if raw[i] < 0x21 || raw[i] > 0x7e {
			return PublicOrigin{}, errors.New("public origin must be printable ASCII")
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.RawPath != "" || u.Host == "" {
		return PublicOrigin{}, errors.New("invalid public origin")
	}
	if strings.HasSuffix(raw, "/") {
		return PublicOrigin{}, errors.New("public origin must be canonical")
	}
	scheme := strings.ToLower(u.Scheme)
	hostname := strings.ToLower(u.Hostname())
	port := u.Port()
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	host := hostname
	if strings.Contains(hostname, ":") {
		host = "[" + hostname + "]"
	}
	if port != "" {
		host += ":" + port
	}
	if scheme != "https" && scheme != "http" {
		return PublicOrigin{}, errors.New("public origin must use HTTP(S)")
	}
	if scheme == "http" && !isDevelopment(environment) {
		return PublicOrigin{}, errors.New("public origin must use HTTPS")
	}
	if scheme == "http" {
		ip := net.ParseIP(hostname)
		if hostname != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return PublicOrigin{}, errors.New("development HTTP public origin must be loopback")
		}
	}
	return PublicOrigin{value: scheme + "://" + host}, nil
}

func isDevelopment(environment string) bool {
	return environment == "development" || environment == "dev" || environment == "test"
}

func (o PublicOrigin) String() string { return o.value }

func (o PublicOrigin) Resolve(path string) string {
	if !strings.HasPrefix(path, "/") {
		return ""
	}
	return o.value + path
}
