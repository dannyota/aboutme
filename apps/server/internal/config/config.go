// Package config loads and validates the server's process configuration
// from environment variables.
package config

import (
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

// Config holds the server's validated runtime configuration.
type Config struct {
	// Port is the TCP port the HTTP server listens on.
	Port int
	// ListenHost is the network interface address the HTTP server binds,
	// e.g. "127.0.0.1" (loopback only) or "0.0.0.0" (all interfaces).
	// Defaults to "127.0.0.1" and, when Env is "prod" or "staging", must
	// resolve to a loopback address: production's only supported topology
	// (design spec §6) has Caddy as the sole process ever allowed to reach
	// this one directly, always over loopback, so port 8080 must never be
	// reachable any other way, and staging enforces the same boundary so a
	// misconfiguration is caught before it reaches prod. A compose/self-host
	// topology that instead reaches this process over a container network
	// must set LISTEN_HOST explicitly to an interface Caddy's container can
	// reach.
	ListenHost string
	// DatabaseURL is the Postgres connection string.
	DatabaseURL string
	// LogLevel is one of "debug", "info", "warn", "error".
	LogLevel string
	// Env is one of "dev", "staging", "prod".
	Env string
	// TrustedProxyCIDRs is the set of reverse-proxy hops this server
	// treats as able to assert a request's real client IP and scheme (see
	// api.TrustedProxies for the spoofing risk of getting this wrong).
	// Required (non-empty) when Env is "prod" or "staging": there is no safe
	// default in either direction (trusting everyone is a spoofing bypass;
	// trusting no one collapses every real client behind the actual proxy
	// into one shared bucket), so both production and staging fail closed —
	// refusing to start — rather than silently guessing; staging shares
	// prod's strictness so a mismatched boundary is caught before it reaches
	// prod. Optional in dev, defaulting to nil (trust no one), since a dev
	// topology's correct value (e.g. a podman-compose network's subnet)
	// isn't something this package can know in advance.
	TrustedProxyCIDRs []netip.Prefix
}

const (
	defaultPort       = 8080
	defaultListenHost = "127.0.0.1"
	defaultLogLevel   = "info"

	minPort = 1
	maxPort = 65535
)

var validLogLevels = map[string]bool{
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
}

var validEnvs = map[string]bool{
	"dev":     true,
	"staging": true,
	"prod":    true,
}

// Load reads and validates configuration using getenv to look up each
// variable. Passing os.Getenv reads the real process environment; tests can
// substitute a fake lookup so they never mutate global state.
//
// PORT defaults to 8080, LISTEN_HOST defaults to "127.0.0.1", and
// LOG_LEVEL defaults to "info" when unset. DATABASE_URL and ENV have no
// safe default and are required. TRUSTED_PROXY_CIDRS is required when
// ENV=prod or ENV=staging (see TrustedProxyCIDRs and
// requiresProductionTrustBoundary) and optional in dev. Load fails fast
// with a descriptive error naming the offending variable when any of these
// are missing or invalid.
func Load(getenv func(string) string) (Config, error) {
	port, err := loadPort(getenv("PORT"))
	if err != nil {
		return Config{}, err
	}

	databaseURL := strings.TrimSpace(getenv("DATABASE_URL"))
	if databaseURL == "" {
		return Config{}, fmt.Errorf("config: DATABASE_URL is required")
	}

	logLevel, err := loadLogLevel(getenv("LOG_LEVEL"))
	if err != nil {
		return Config{}, err
	}

	env, err := loadEnv(getenv("ENV"))
	if err != nil {
		return Config{}, err
	}

	listenHost, err := loadListenHost(getenv("LISTEN_HOST"), env)
	if err != nil {
		return Config{}, err
	}

	trustedProxyCIDRs, err := loadTrustedProxyCIDRs(getenv("TRUSTED_PROXY_CIDRS"), env)
	if err != nil {
		return Config{}, err
	}

	return Config{
		Port:              port,
		ListenHost:        listenHost,
		DatabaseURL:       databaseURL,
		LogLevel:          logLevel,
		Env:               env,
		TrustedProxyCIDRs: trustedProxyCIDRs,
	}, nil
}

// LoadEnv loads configuration from the real process environment.
func LoadEnv() (Config, error) {
	return Load(os.Getenv)
}

func loadPort(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultPort, nil
	}

	port, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("config: PORT: invalid value %q: must be an integer", raw)
	}
	if port < minPort || port > maxPort {
		return 0, fmt.Errorf("config: PORT: invalid value %d: must be between %d and %d", port, minPort, maxPort)
	}
	return port, nil
}

func loadLogLevel(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultLogLevel, nil
	}

	level := strings.ToLower(raw)
	if !validLogLevels[level] {
		return "", fmt.Errorf("config: LOG_LEVEL: invalid value %q: must be one of debug, info, warn, error", raw)
	}
	return level, nil
}

func loadEnv(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("config: ENV is required: must be one of dev, staging, prod")
	}

	env := strings.ToLower(raw)
	if !validEnvs[env] {
		return "", fmt.Errorf("config: ENV: invalid value %q: must be one of dev, staging, prod", raw)
	}
	return env, nil
}

// requiresProductionTrustBoundary reports whether env must satisfy
// production's client-IP trust-boundary strictness: a loopback LISTEN_HOST
// (loadListenHost) and a required, non-empty TRUSTED_PROXY_CIDRS
// (loadTrustedProxyCIDRs). Both "prod" and "staging" require it — staging
// exists specifically to validate the real deployment topology before it
// reaches prod, so a lenient staging boundary would let a misconfiguration
// reach prod undetected (re-review of security review finding #2).
func requiresProductionTrustBoundary(env string) bool {
	return env == "prod" || env == "staging"
}

// loadListenHost validates raw as an IP address to bind, defaulting to
// defaultListenHost when unset. When env requires production trust-boundary
// strictness (see requiresProductionTrustBoundary) it additionally requires
// the result to be a loopback address (design spec §6): binding anything
// else would make port 8080 reachable around Caddy's origin-secret boundary
// entirely.
func loadListenHost(raw, env string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = defaultListenHost
	}

	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return "", fmt.Errorf("config: LISTEN_HOST: invalid value %q: must be a valid IP address: %w", raw, err)
	}
	if requiresProductionTrustBoundary(env) && !addr.Unmap().IsLoopback() {
		return "", fmt.Errorf("config: LISTEN_HOST: invalid value %q: ENV=%s requires a loopback address "+
			"(design spec §6: Caddy is the only process ever allowed to reach this one directly)", raw, env)
	}
	return raw, nil
}

// minTrustedProxyPrefixBitsIPv4 and minTrustedProxyPrefixBitsIPv6 are the
// narrowest (i.e. numerically smallest bit count, so widest address range)
// prefix length TRUSTED_PROXY_CIDRS may configure per address family.
// Anything broader is too implausible to be a real deployment's actual
// reverse-proxy hop and silently approaches "trust everyone" (design spec
// §6, re-review of security review finding #2, minor M5). A single
// literal-/0 special case was evadable by splitting the whole address space
// into two half-width prefixes (e.g. "0.0.0.0/1,128.0.0.0/1") that each
// individually passed; validating every configured prefix against this
// bound on its own closes that split-evasion case along with any other
// implausibly broad single CIDR.
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
		// testing it, so a v4-mapped trusted prefix silently trusts nobody
		// (re-review of security review finding #2, minor M-D). Reject it
		// with a message pointing at the plain IPv4 form, rather than accept
		// a value that reads as configured but is inert — the exact
		// silent-misconfiguration this validation exists to prevent.
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
