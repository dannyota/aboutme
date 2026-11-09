package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

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
	if !validLogLevel(level) {
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
	if !validEnv(env) {
		return "", fmt.Errorf("config: ENV: invalid value %q: must be one of dev, staging, prod", raw)
	}
	return env, nil
}

// loadPublicOrigin validates raw as the server's public origin. Required,
// like Env: there is no safe default (dev/staging/prod each serve from a
// different origin), so Load fails fast rather than silently guessing.
//
// OAuth redirect and callback URLs concatenate PublicOrigin with a path.
// Raw must therefore parse as scheme://host[:port] with no userinfo, path
// (including a bare trailing slash, which parses as Path "/"), query, or
// fragment. An unnoticed trailing slash or path would corrupt every absolute
// URL this server builds from it, so this fails fast at startup rather
// than surfacing as a broken redirect later.
//
// After validation, scheme and host are lowercased, and a scheme's default
// port (":80" for http, ":443" for https) is stripped. This avoids common
// mismatches because csrf.go compares PublicOrigin with the Origin header by
// exact string equality, and OAuth URLs concatenate it verbatim. Operators
// must still supply the browser-serialized form for representations this
// function does not canonicalize, including internationalized domain names
// and equivalent IPv6 spellings.
func loadPublicOrigin(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("config: PUBLIC_ORIGIN is required")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("config: PUBLIC_ORIGIN: invalid value %q: %w", raw, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("config: PUBLIC_ORIGIN: invalid value %q: scheme must be http or https", raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("config: PUBLIC_ORIGIN: invalid value %q: must include a host", raw)
	}
	if u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("config: PUBLIC_ORIGIN: invalid value %q: must be scheme://host[:port] only "+
			"— no userinfo, path, trailing slash, query, or fragment", raw)
	}

	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (scheme == "http" && port == "80") || (scheme == "https" && port == "443") {
		port = ""
	}
	hostport := host
	if strings.Contains(host, ":") { // IPv6 literal: re-add the brackets url.Hostname() strips.
		hostport = "[" + host + "]"
	}
	if port != "" {
		hostport += ":" + port
	}
	return scheme + "://" + hostport, nil
}

// requiresProductionTrustBoundary reports whether env must satisfy
// production's client-IP trust-boundary strictness: a loopback LISTEN_HOST
// (loadListenHost) and a required, non-empty TRUSTED_PROXY_CIDRS
// (loadTrustedProxyCIDRs). Both "prod" and "staging" require it.
func requiresProductionTrustBoundary(env string) bool {
	return env == "prod" || env == "staging"
}

// loadListenHost validates raw as an IP address to bind, defaulting to
// defaultListenHost when unset. When env requires production trust-boundary
// strictness (see requiresProductionTrustBoundary) it additionally requires
// the result to be a loopback address: binding anything
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
