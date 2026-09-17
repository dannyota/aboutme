package config

import (
	"fmt"
	"net/netip"
	"strings"
)

// minTrustedProxyPrefixBitsIPv4 and minTrustedProxyPrefixBitsIPv6 are the
// narrowest (i.e. numerically smallest bit count, so widest address range)
// prefix length TRUSTED_PROXY_CIDRS may configure per address family.
// Anything broader is too implausible to be a real deployment's actual
// reverse-proxy hop and silently approaches "trust everyone." Checking every
// prefix also rejects an address space split into individually broad ranges,
// such as "0.0.0.0/1,128.0.0.0/1".
const (
	minTrustedProxyPrefixBitsIPv4 = 8
	minTrustedProxyPrefixBitsIPv6 = 48
)

// loadTrustedProxyCIDRs parses raw as a comma-separated list of CIDRs. When
// env requires production trust-boundary strictness (see
// requiresProductionTrustBoundary) it must be non-empty: see
// Config.TrustedProxyCIDRs for why production has no safe default to fall
// back to. Outside that, an empty/unset raw returns (nil, nil) — trust no
// one, the same safe default api.TrustedProxies documents for its own zero
// value.
func loadTrustedProxyCIDRs(raw, env string) ([]netip.Prefix, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if requiresProductionTrustBoundary(env) {
			return nil, fmt.Errorf("config: TRUSTED_PROXY_CIDRS is required when ENV=%s: "+
				"production and staging must fail closed on their client-IP trust boundary "+
				"(design spec §6), never silently trust every peer or none", env)
		}
		return nil, nil
	}

	fields := strings.Split(raw, ",")
	cidrs := make([]netip.Prefix, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(field)
		if err != nil {
			return nil, fmt.Errorf("config: TRUSTED_PROXY_CIDRS: invalid value %q: "+
				"must be a comma-separated list of CIDRs: %w", raw, err)
		}
		// An IPv4-in-IPv6 mapped prefix (e.g. "::ffff:0.0.0.0/104") is
		// judged against the IPv6 minimum below, yet can never match a real
		// peer: api.resolveClientIP Unmap()s every peer address before
		// testing it, so a v4-mapped trusted prefix silently trusts nobody.
		// Reject it with a message pointing at the plain IPv4 form rather than
		// accept an inert value that appears configured.
		if prefix.Addr().Is4In6() {
			return nil, fmt.Errorf("config: TRUSTED_PROXY_CIDRS: invalid value %q: "+
				"%s is an IPv4-in-IPv6 mapped prefix, which never matches a real peer "+
				"(peer addresses are unmapped before the trust check); write it as a plain "+
				"IPv4 CIDR (the %s address, e.g. its IPv4 form) instead (design spec §6)",
				raw, field, prefix.Addr().Unmap().String())
		}
		// See the const doc comment above: a mismatched but non-trivial
		// (i.e. within these bounds) CIDR can't be caught here, since Go
		// has no way to know the real topology at config-load time; the
		// runtime mismatch warning in api.RateLimit is what catches that
		// case.
		minBits := minTrustedProxyPrefixBitsIPv4
		if !prefix.Addr().Is4() {
			minBits = minTrustedProxyPrefixBitsIPv6
		}
		if prefix.Bits() < minBits {
			return nil, fmt.Errorf("config: TRUSTED_PROXY_CIDRS: invalid value %q: "+
				"%s is broader than the minimum allowed /%d for its address family, which is "+
				"too broad to plausibly identify the deployment's actual reverse-proxy hop and "+
				"defeats the client-IP trust boundary (design spec §6); use the deployment's "+
				"actual proxy CIDR", raw, field, minBits)
		}
		cidrs = append(cidrs, prefix)
	}
	if len(cidrs) == 0 && requiresProductionTrustBoundary(env) {
		return nil, fmt.Errorf("config: TRUSTED_PROXY_CIDRS is required when ENV=%s", env)
	}
	return cidrs, nil
}
