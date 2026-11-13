package publicapi

// The WHATWG URL parser refuses links net/url accepts: ports above 65535, invalid numeric IPv4 hosts, IPv6 zones, forbidden host code points, IDNA failures.

import (
	"net/netip"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/idna"

	"danny.vn/aboutme/apps/server/internal/project"
)

func rendererLink(link string) bool {
	if !strings.HasPrefix(link, "https://") || utf8.RuneCountInString(link) > project.MaxLinkCharacters {
		return false
	}
	_, _, ok := whatwgHostPort(whatwgAuthority(link[len("https://"):]))
	return ok
}

func whatwgAuthority(rest string) string {
	rest = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, rest)
	rest = strings.TrimRightFunc(rest, func(r rune) bool { return r <= ' ' })
	rest = strings.TrimLeft(rest, "/\\")
	if end := strings.IndexAny(rest, "/\\?#"); end >= 0 {
		rest = rest[:end]
	}
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		rest = rest[at+1:]
	}
	return rest
}

func whatwgHostPort(hostPort string) (host, port string, ok bool) {
	rawHost, rawPort := hostPort, ""
	if strings.HasPrefix(hostPort, "[") {
		end := strings.Index(hostPort, "]")
		if end < 0 {
			return "", "", false
		}
		rest := hostPort[end+1:]
		if rest != "" && !strings.HasPrefix(rest, ":") {
			return "", "", false
		}
		rawHost, rawPort = hostPort[:end+1], strings.TrimPrefix(rest, ":")
	} else if colon := strings.Index(hostPort, ":"); colon >= 0 {
		rawHost, rawPort = hostPort[:colon], hostPort[colon+1:]
	}
	port, ok = whatwgPort(rawPort)
	if !ok {
		return "", "", false
	}
	host, ok = whatwgHost(rawHost)
	return host, port, ok
}

func whatwgPort(raw string) (string, bool) {
	if raw == "" {
		return "", true
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return "", false
		}
	}
	trimmed := strings.TrimLeft(raw, "0")
	if len(trimmed) > 5 {
		return "", false
	}
	value, err := strconv.Atoi("0" + trimmed)
	if err != nil || value > 65535 {
		return "", false
	}
	return strconv.Itoa(value), true
}

func whatwgHost(raw string) (string, bool) {
	if strings.HasPrefix(raw, "[") {
		inner := strings.TrimSuffix(strings.TrimPrefix(raw, "["), "]")
		addr, err := netip.ParseAddr(inner)
		if err != nil || !addr.Is6() || addr.Zone() != "" || strings.Contains(inner, "%") {
			return "", false
		}
		return "[" + addr.String() + "]", true
	}
	decoded := whatwgPercentDecode(raw)
	if !utf8.ValidString(decoded) {
		return "", false
	}
	profile := idna.New(idna.MapForLookup(), idna.StrictDomainName(false), idna.CheckHyphens(false),
		idna.CheckJoiners(true), idna.BidiRule(), idna.Transitional(false), idna.VerifyDNSLength(false))
	ascii, err := profile.ToASCII(decoded)
	if err != nil || ascii == "" {
		return "", false
	}
	for i := 0; i < len(ascii); i++ {
		if c := ascii[i]; c <= ' ' || c >= 0x7f || strings.IndexByte("#%/:<>?@[\\]^|", c) >= 0 {
			return "", false
		}
	}
	if !whatwgEndsInANumber(ascii) {
		return ascii, true
	}
	address, ok := whatwgIPv4(ascii)
	if !ok {
		return "", false
	}
	return address, true
}

func whatwgPercentDecode(raw string) string {
	var out strings.Builder
	for i := 0; i < len(raw); i++ {
		if raw[i] == '%' && i+2 < len(raw) {
			if value, err := strconv.ParseUint(raw[i+1:i+3], 16, 8); err == nil {
				out.WriteByte(byte(value))
				i += 2
				continue
			}
		}
		out.WriteByte(raw[i])
	}
	return out.String()
}

func whatwgEndsInANumber(domain string) bool {
	labels := strings.Split(domain, ".")
	if len(labels) > 1 && labels[len(labels)-1] == "" {
		labels = labels[:len(labels)-1]
	}
	last := labels[len(labels)-1]
	if last != "" && strings.Trim(last, "0123456789") == "" {
		return true
	}
	hex, found := strings.CutPrefix(strings.ToLower(last), "0x")
	return found && strings.Trim(hex, "0123456789abcdef") == ""
}

func whatwgIPv4(domain string) (string, bool) {
	parts := strings.Split(domain, ".")
	if len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) > 4 {
		return "", false
	}
	var address uint64
	for i, part := range parts {
		value, ok := whatwgIPv4Number(part)
		if !ok {
			return "", false
		}
		if i < len(parts)-1 {
			if value > 255 {
				return "", false
			}
			address = address<<8 | value
			continue
		}
		remaining := uint(5 - len(parts))
		if value >= 1<<(8*remaining) {
			return "", false
		}
		address = address<<(8*remaining) | value
	}
	return netip.AddrFrom4([4]byte{byte(address >> 24), byte(address >> 16), byte(address >> 8), byte(address)}).String(), true
}

func whatwgIPv4Number(part string) (uint64, bool) {
	if part == "" {
		return 0, false
	}
	base, digits := 10, part
	switch {
	case len(part) >= 2 && (part[:2] == "0x" || part[:2] == "0X"):
		base, digits = 16, part[2:]
	case len(part) >= 2 && part[0] == '0':
		base, digits = 8, part[1:]
	}
	if digits == "" {
		return 0, true
	}
	value, err := strconv.ParseUint(digits, base, 64)
	if err != nil || value > 0xffffffff {
		return 0, false
	}
	return value, true
}
