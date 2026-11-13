package config

import (
	"fmt"
	"net/netip"
	"strings"
)

const (
	minTrustedProxyPrefixBitsIPv4 = 8
	minTrustedProxyPrefixBitsIPv6 = 48
)

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
		if prefix.Addr().Is4In6() {
			return nil, fmt.Errorf("config: TRUSTED_PROXY_CIDRS: invalid value %q: "+
				"%s is an IPv4-in-IPv6 mapped prefix, which never matches a real peer "+
				"(peer addresses are unmapped before the trust check); write it as a plain "+
				"IPv4 CIDR (the %s address, e.g. its IPv4 form) instead (design spec §6)",
				raw, field, prefix.Addr().Unmap().String())
		}
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
