package api

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// TrustedProxies is the set of reverse-proxy hops, by CIDR, whose
// TrustedClientIPHeader and X-Forwarded-Proto this server honors. A
// request is only treated as having arrived via a trusted proxy when its
// immediate socket peer (r.RemoteAddr) falls inside one of these ranges —
// unlike a header, RemoteAddr comes from the kernel's view of the actual
// TCP connection and cannot be forged by the client, so this is the one
// part of the trust decision an attacker cannot spoof.
//
// This must match the real deployment topology; see
// docs/design/deployment.md. Getting it wrong fails in different directions
// depending on which way it's wrong:
//   - Too broad (or defaulted to "trust everyone") lets any direct client
//     set its own TrustedClientIPHeader and pick any key it likes,
//     bypassing the limit entirely.
//   - Too narrow (or defaulted to "trust no one" when the topology
//     actually puts a proxy in front) makes every request appear to
//     originate from that proxy's own address, collapsing every distinct
//     real client into one shared bucket — a denial of service against
//     every legitimate client behind it, not just the misconfiguration's
//     author.
//
// Neither direction is a safe default, which is why this type's zero
// value (nil, trust no one) is only correct for a deployment with no
// proxy in front of Go at all — every other topology (see
// internal/config's TRUSTED_PROXY_CIDRS) must set this explicitly.
type TrustedProxies []netip.Prefix

// LoopbackTrustedProxies returns the trusted-proxy set for this project's
// production topology: Go bound to 127.0.0.1, reached
// only by Caddy over loopback (host networking, no ALB). It is NOT
// generally correct for podman-compose dev/self-host, where Caddy reaches
// Go as a separate container over the compose network rather than
// loopback — that topology's trusted CIDR is the compose network's
// subnet, supplied via TRUSTED_PROXY_CIDRS, not this function.
func LoopbackTrustedProxies() TrustedProxies {
	return TrustedProxies{
		netip.MustParsePrefix("127.0.0.1/32"),
		netip.MustParsePrefix("::1/128"),
	}
}

// trusts reports whether remoteAddr — an http.Request.RemoteAddr-shaped
// "host:port" string (or a bare host, as some hand-built test requests
// use) — falls inside tp.
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

// TrustsPeer reports whether r's socket peer is a trusted proxy, the same
// decision ClientIP makes. Callers that honor another proxy-set header use
// it instead of re-deriving the trust boundary.
func (tp TrustedProxies) TrustsPeer(r *http.Request) bool {
	return tp.trusts(r.RemoteAddr)
}

// maxAddrLen bounds how much of a claimed address string peerAddr will
// even attempt to parse: the longest valid textual IP address (IPv6 with
// an embedded IPv4 tail, e.g.
// "ffff:ffff:ffff:ffff:ffff:ffff:255.255.255.255") is 45 bytes, so
// anything longer is rejected outright rather than handed to
// netip.ParseAddr — a cheap guard against a claimed header value crafted
// to be needlessly expensive to reject.
const maxAddrLen = 45

// peerAddr extracts and validates the IP address portion of an
// http.Request.RemoteAddr- or header-value-shaped string: "host:port",
// "[ipv6]:port", or a bare host with no port (real RemoteAddr values
// always have one; TrustedClientIPHeader values and some hand-built test
// requests don't). The returned netip.Addr is Unmap()'d, so an IPv4
// address and its IPv4-in-IPv6 form ("203.0.113.5" vs
// "::ffff:203.0.113.5") always normalize to the same value — both for
// keying the rate limiter and for matching against TrustedProxies, which
// would otherwise silently fail to recognize a v4-mapped peer as
// loopback/in-range on a dual-stack listener.
//
// The result is a fixed-size value type, never a substring of remoteAddr:
// a caller that stores addr.String() in a long-lived map (e.g.
// rateLimiter.entries) retains only the bytes of the formatted address
// itself, never remoteAddr's (or a raw header's) backing array.
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

// TrustedClientIPHeader is the header Caddy sets, once and only once it
// has itself verified the CloudFront origin-secret, restricted forwarded
// headers to CloudFront's origin-facing ranges, and stripped every
// client-supplied forwarding header, to the single validated viewer
// address. Caddy — not Go — is the one place that reconciles a multi-hop
// X-Forwarded-For chain (e.g. CloudFront appending the viewer address, then a
// proxy in front of it
// appending its own) into one address; Go trusts this header's value only
// when the request's socket peer is in TrustedProxies (see clientIP) and
// never parses X-Forwarded-For itself.
const TrustedClientIPHeader = "X-Real-IP"

// canonicalHeaderIP resolves the single, strictly-parsed client IP a
// trusted proxy asserted via TrustedClientIPHeader. The deployment contract
// requires exactly one bare address — never a port or list — so this does not
// reuse peerAddr's host:port-tolerant
// parsing: a proxy asserting "203.0.113.5:8080" here is already violating
// the contract and must be rejected, not silently corrected the way a real
// RemoteAddr's own trailing port is.
func canonicalHeaderIP(r *http.Request) (netip.Addr, bool) {
	values := r.Header.Values(TrustedClientIPHeader)
	if len(values) != 1 {
		// Missing entirely, or repeated (RFC 9110 §5.3 allows a sender to
		// repeat a header field): an ambiguous count must never be
		// resolved by silently picking the first or last value.
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

// resolveClientIP returns r's client IP, and whether one could be
// determined at all. See TrustedClientIPHeader and TrustedProxies for the
// trust decision this depends on.
//
//   - If r arrived from a trusted proxy (see TrustedProxies), the ONLY
//     source ever consulted is TrustedClientIPHeader via canonicalHeaderIP
//     — never RemoteAddr, which at a trusted hop is the proxy's own
//     address, not the viewer's, and never X-Forwarded-For, which this
//     server does not parse at all. A trusted hop whose header is missing,
//     repeated, malformed, oversized, or port-bearing fails closed (false)
//     rather than falling back to RemoteAddr. A fallback would collapse
//     every viewer behind that proxy into one bucket keyed on the proxy's
//     own address. Forcing every viewer to share that bucket is worse than
//     rejecting the malformed request.
//   - If r did not arrive from a trusted proxy, RemoteAddr is the real
//     socket peer and is used directly via peerAddr; an unparseable
//     RemoteAddr also fails closed rather than being used as a raw,
//     unbounded string key.
func resolveClientIP(r *http.Request, trusted TrustedProxies) (netip.Addr, bool) {
	if trusted.trusts(r.RemoteAddr) {
		return canonicalHeaderIP(r)
	}
	return peerAddr(r.RemoteAddr)
}

// ClientIP returns r's resolved client IP as a bare address string (no
// port), and whether one could be determined at all — the same trust
// decision resolveClientIP/IPKeyFunc already make, exposed for a caller
// outside this package that needs the address itself rather than a
// rate-limit key derived from it. Session issuance must
// record the request's real, trust-boundary-resolved client IP — never a
// raw r.RemoteAddr, which at a trusted proxy hop is Caddy's own address,
// not the viewer's — and must never re-derive its own copy of this
// decision (see TrustedClientIPHeader/TrustedProxies for why getting it
// wrong is a spoofing bypass in one direction or a denial of service in
// the other).
func ClientIP(r *http.Request, trusted TrustedProxies) (string, bool) {
	addr, ok := resolveClientIP(r, trusted)
	if !ok {
		return "", false
	}
	return addr.String(), true
}

// requestIsHTTPS reports whether r arrived over HTTPS: either TLS
// terminated on this process directly, or r arrived via a trusted proxy
// (see TrustedProxies) asserting X-Forwarded-Proto: https. Caddy
// terminates TLS for CloudFront -> Caddy -> Go and always
// sets this header, so in production this is the path SecurityHeaders'
// HSTS decision actually takes.
func requestIsHTTPS(r *http.Request, trusted TrustedProxies) bool {
	if r.TLS != nil {
		return true
	}
	if !trusted.trusts(r.RemoteAddr) {
		return false
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
