package publicresume

import (
	"errors"
	"net/netip"
	"strconv"
	"strings"
)

type PublicOrigin struct{ value string }

func ParsePublicOrigin(raw, environment string) (PublicOrigin, error) {
	if raw == "" || len(raw) > 512 {
		return PublicOrigin{}, errors.New("public origin is empty or too long")
	}
	scheme, authority, ok := strings.Cut(raw, "://")
	if !ok || (scheme != "https" && scheme != "http") {
		return PublicOrigin{}, errors.New("public origin must start with https:// or http://")
	}
	host, port, ok := splitAuthority(authority)
	if !ok || !canonicalPort(scheme, port) {
		return PublicOrigin{}, errors.New("public origin must already be in canonical form")
	}
	if scheme == "http" && (!isDevelopment(environment) || !loopback(host)) {
		return PublicOrigin{}, errors.New("an HTTP public origin must be a development loopback")
	}
	return PublicOrigin{value: raw}, nil
}

func splitAuthority(authority string) (host, port string, ok bool) {
	if rest, bracketed := strings.CutPrefix(authority, "["); bracketed {
		literal, after, closed := strings.Cut(rest, "]")
		addr, err := netip.ParseAddr(literal)
		if !closed || err != nil || !addr.Is6() || addr.Is4In6() || addr.Zone() != "" || addr.String() != literal {
			return "", "", false
		}
		if after != "" && !strings.HasPrefix(after, ":") {
			return "", "", false
		}
		return literal, after, true
	}
	host, digits, hasPort := strings.Cut(authority, ":")
	if hasPort {
		port = ":" + digits
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return host, port, addr.Is4() && addr.String() == host
	}
	return host, port, domainName(host)
}

func canonicalPort(scheme, port string) bool {
	if port == "" {
		return true
	}
	digits := strings.TrimPrefix(port, ":")
	n, err := strconv.Atoi(digits)
	if err != nil || n < 1 || n > 65535 || strconv.Itoa(n) != digits {
		return false
	}
	return (scheme != "https" || n != 443) && (scheme != "http" || n != 80)
}

func domainName(host string) bool {
	labels := strings.Split(strings.TrimSuffix(host, "."), ".")
	for _, label := range labels {
		if label == "" || strings.Trim(label, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
			return false
		}
	}
	last := labels[len(labels)-1]
	return !strings.HasPrefix(last, "0x") && strings.Trim(last, "0123456789") != ""
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.IsLoopback()
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
