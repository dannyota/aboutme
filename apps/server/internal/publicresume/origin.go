package publicresume

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

type PublicOrigin struct{ value string }

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
	if !canonicalPort(port) {
		return PublicOrigin{}, errors.New("public origin port must be canonical")
	}
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
	if !canonicalHost(hostname) {
		return PublicOrigin{}, errors.New("public origin host must be canonical")
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

func canonicalPort(port string) bool {
	if port == "" {
		return true
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 0 && n <= 65535 && strconv.Itoa(n) == port
}

func canonicalHost(hostname string) bool {
	if addr, err := netip.ParseAddr(hostname); err == nil {
		return addr.Zone() == "" && !addr.Is4In6() && addr.String() == hostname
	}
	labels := strings.Split(strings.TrimSuffix(hostname, "."), ".")
	last := labels[len(labels)-1]
	if last == "" {
		return true
	}
	if strings.HasPrefix(last, "0x") {
		return false
	}
	return strings.Trim(last, "0123456789") != ""
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
