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
	if strings.Contains(host, ":") {
		hostport = "[" + host + "]"
	}
	if port != "" {
		hostport += ":" + port
	}
	return scheme + "://" + hostport, nil
}

func requiresProductionTrustBoundary(env string) bool {
	return env == "prod" || env == "staging"
}

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
