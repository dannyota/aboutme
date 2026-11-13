package api

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type TrustedProxies []netip.Prefix

func LoopbackTrustedProxies() TrustedProxies {
	return TrustedProxies{
		netip.MustParsePrefix("127.0.0.1/32"),
		netip.MustParsePrefix("::1/128"),
	}
}

func (tp TrustedProxies) trusts(remoteAddr string) bool {
	addr, ok := peerAddr(remoteAddr)
	if !ok {
		return false
	}
	for _, prefix := range tp {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func (tp TrustedProxies) TrustsPeer(r *http.Request) bool {
	return tp.trusts(r.RemoteAddr)
}

const maxAddrLen = 45

func peerAddr(remoteAddr string) (netip.Addr, bool) {
	host := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = h
	}
	host = strings.TrimSpace(host)
	if host == "" || len(host) > maxAddrLen {
		return netip.Addr{}, false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

const TrustedClientIPHeader = "X-Real-IP"

func canonicalHeaderIP(r *http.Request) (netip.Addr, bool) {
	values := r.Header.Values(TrustedClientIPHeader)
	if len(values) != 1 {
		return netip.Addr{}, false
	}

	raw := strings.TrimSpace(values[0])
	if raw == "" || len(raw) > maxAddrLen {
		return netip.Addr{}, false
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func resolveClientIP(r *http.Request, trusted TrustedProxies) (netip.Addr, bool) {
	if trusted.trusts(r.RemoteAddr) {
		return canonicalHeaderIP(r)
	}
	return peerAddr(r.RemoteAddr)
}

func ClientIP(r *http.Request, trusted TrustedProxies) (string, bool) {
	addr, ok := resolveClientIP(r, trusted)
	if !ok {
		return "", false
	}
	return addr.String(), true
}

func requestIsHTTPS(r *http.Request, trusted TrustedProxies) bool {
	if r.TLS != nil {
		return true
	}
	if !trusted.trusts(r.RemoteAddr) {
		return false
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
