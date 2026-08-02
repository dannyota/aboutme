// Package config_test exercises env-var parsing and validation for the
// server configuration.
package config_test

import (
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/config"
)

// env builds a lookup function for config.Load from a map, so tests never
// touch real process environment variables.
func env(vars map[string]string) func(string) string {
	return func(key string) string {
		return vars[key]
	}
}

func TestLoad_ValidConfig(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"PORT":          "9090",
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"LOG_LEVEL":     "debug",
		"ENV":           "dev",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	// Config.TrustedProxyCIDRs is a slice, so Config is not comparable with
	// == — compare each field explicitly instead.
	if got.Port != 9090 {
		t.Errorf("Port = %d, want %d", got.Port, 9090)
	}
	if got.ListenHost != "127.0.0.1" {
		t.Errorf("ListenHost = %q, want %q (default)", got.ListenHost, "127.0.0.1")
	}
	if got.DatabaseURL != "postgres://user:pass@localhost:5432/aboutme" {
		t.Errorf("DatabaseURL = %q, want %q", got.DatabaseURL, "postgres://user:pass@localhost:5432/aboutme")
	}
	if got.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", got.LogLevel, "debug")
	}
	if got.Env != "dev" {
		t.Errorf("Env = %q, want %q", got.Env, "dev")
	}
	if got.PublicOrigin != "https://aboutme.vn" {
		t.Errorf("PublicOrigin = %q, want %q", got.PublicOrigin, "https://aboutme.vn")
	}
	if got.TrustedProxyCIDRs != nil {
		t.Errorf("TrustedProxyCIDRs = %v, want nil (default outside prod/staging)", got.TrustedProxyCIDRs)
	}
}

// TestLoad_ValidConfig_StagingRequiresTrustBoundary is the staging
// counterpart of TestLoad_ValidConfig: staging now carries the same
// client-IP trust-boundary strictness as prod (re-review of security
// review finding #2 — staging exists to validate the real deployment
// topology before it reaches prod, so a lenient staging boundary would let
// a misconfiguration reach prod undetected), so a fully valid staging
// config must supply TRUSTED_PROXY_CIDRS and parse it exactly like prod
// does.
func TestLoad_ValidConfig_StagingRequiresTrustBoundary(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":        "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":       "https://aboutme.vn",
		"ENV":                 "staging",
		"LISTEN_HOST":         "127.0.0.1",
		"TRUSTED_PROXY_CIDRS": "127.0.0.1/32,::1/128",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.Env != "staging" {
		t.Errorf("Env = %q, want %q", got.Env, "staging")
	}
	want := []netip.Prefix{
		netip.MustParsePrefix("127.0.0.1/32"),
		netip.MustParsePrefix("::1/128"),
	}
	if !slices.Equal(got.TrustedProxyCIDRs, want) {
		t.Errorf("TrustedProxyCIDRs = %v, want %v", got.TrustedProxyCIDRs, want)
	}
}

func TestLoad_PortDefaultsTo8080(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"LOG_LEVEL":     "info",
		"ENV":           "dev",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.Port != 8080 {
		t.Errorf("Port = %d, want 8080", got.Port)
	}
}

func TestLoad_LogLevelDefaultsToInfo(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"ENV":           "dev",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", got.LogLevel, "info")
	}
}

func TestLoad_LogLevelCaseInsensitive(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"LOG_LEVEL":     "WARN",
		"ENV":           "dev",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.LogLevel != "warn" {
		t.Errorf("LogLevel = %q, want %q", got.LogLevel, "warn")
	}
}

func TestLoad_MissingRequiredVars(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		vars    map[string]string
		wantErr string
	}{
		{
			name: "missing DATABASE_URL",
			vars: map[string]string{
				"ENV": "dev",
			},
			wantErr: "DATABASE_URL",
		},
		{
			name: "missing ENV",
			vars: map[string]string{
				"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN": "https://aboutme.vn",
			},
			wantErr: "ENV",
		},
		{
			name: "missing PUBLIC_ORIGIN",
			vars: map[string]string{
				"DATABASE_URL": "postgres://user:pass@localhost:5432/aboutme",
				"ENV":          "dev",
			},
			wantErr: "PUBLIC_ORIGIN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(env(tt.vars))
			if err == nil {
				t.Fatalf("Load() error = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Load() error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestLoad_InvalidValues(t *testing.T) {
	t.Parallel()

	base := map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"LOG_LEVEL":     "info",
		"ENV":           "dev",
	}

	tests := []struct {
		name    string
		key     string
		value   string
		wantErr string
	}{
		{name: "port not numeric", key: "PORT", value: "abc", wantErr: "PORT"},
		{name: "port zero", key: "PORT", value: "0", wantErr: "PORT"},
		{name: "port negative", key: "PORT", value: "-1", wantErr: "PORT"},
		{name: "port too large", key: "PORT", value: "70000", wantErr: "PORT"},
		{name: "log level invalid", key: "LOG_LEVEL", value: "verbose", wantErr: "LOG_LEVEL"},
		{name: "env invalid", key: "ENV", value: "production", wantErr: "ENV"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vars := make(map[string]string, len(base))
			for k, v := range base {
				vars[k] = v
			}
			vars[tt.key] = tt.value

			_, err := config.Load(env(vars))
			if err == nil {
				t.Fatalf("Load() error = nil, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Load() error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestLoad_ValidEnvValues(t *testing.T) {
	t.Parallel()

	for _, envValue := range []string{"dev", "staging", "prod"} {
		envValue := envValue
		t.Run(envValue, func(t *testing.T) {
			t.Parallel()

			vars := map[string]string{
				"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN": "https://aboutme.vn",
				"ENV":           envValue,
			}
			if envValue == "prod" || envValue == "staging" {
				// prod and staging both require TRUSTED_PROXY_CIDRS (fail
				// closed — see TestLoad_TrustedProxyCIDRs_RequiredInProd and
				// TestLoad_TrustedProxyCIDRs_RequiredInStaging); LISTEN_HOST
				// is left at its loopback default.
				vars["TRUSTED_PROXY_CIDRS"] = "127.0.0.1/32,::1/128"
			}

			got, err := config.Load(env(vars))
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
			if got.Env != envValue {
				t.Errorf("Env = %q, want %q", got.Env, envValue)
			}
		})
	}
}

func TestLoad_ListenHostDefaultsToLoopback(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"ENV":           "dev",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.ListenHost != "127.0.0.1" {
		t.Errorf("ListenHost = %q, want %q", got.ListenHost, "127.0.0.1")
	}
}

func TestLoad_ListenHostCustomValueAcceptedOutsideProd(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"ENV":           "dev",
		"LISTEN_HOST":   "0.0.0.0",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.ListenHost != "0.0.0.0" {
		t.Errorf("ListenHost = %q, want %q", got.ListenHost, "0.0.0.0")
	}
}

func TestLoad_ListenHostInvalidValue(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"ENV":           "dev",
		"LISTEN_HOST":   "not-an-ip",
	}))
	if err == nil {
		t.Fatal("Load() error = nil, want error for invalid LISTEN_HOST")
	}
	if !strings.Contains(err.Error(), "LISTEN_HOST") {
		t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "LISTEN_HOST")
	}
}

// TestLoad_ListenHostNonLoopbackRejectedInProd guards the specific
// regression the security review found: main.go used to listen on all
// interfaces unconditionally, contrary to design spec §6's production
// requirement that port 8080 never be reachable around Caddy's
// origin-secret boundary.
func TestLoad_ListenHostNonLoopbackRejectedInProd(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{
		"DATABASE_URL":        "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":       "https://aboutme.vn",
		"ENV":                 "prod",
		"LISTEN_HOST":         "0.0.0.0",
		"TRUSTED_PROXY_CIDRS": "127.0.0.1/32",
	}))
	if err == nil {
		t.Fatal("Load() error = nil, want error for non-loopback LISTEN_HOST in prod")
	}
	if !strings.Contains(err.Error(), "LISTEN_HOST") {
		t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "LISTEN_HOST")
	}
}

func TestLoad_ListenHostLoopbackAcceptedInProd(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":        "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":       "https://aboutme.vn",
		"ENV":                 "prod",
		"LISTEN_HOST":         "127.0.0.1",
		"TRUSTED_PROXY_CIDRS": "127.0.0.1/32",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.ListenHost != "127.0.0.1" {
		t.Errorf("ListenHost = %q, want %q", got.ListenHost, "127.0.0.1")
	}
}

// TestLoad_ListenHostNonLoopbackRejectedInStaging is the staging
// counterpart of TestLoad_ListenHostNonLoopbackRejectedInProd: staging now
// carries the same production-strictness client-IP trust boundary as prod
// (re-review of security review finding #2), so it must reject a
// non-loopback LISTEN_HOST too.
func TestLoad_ListenHostNonLoopbackRejectedInStaging(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{
		"DATABASE_URL":        "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":       "https://aboutme.vn",
		"ENV":                 "staging",
		"LISTEN_HOST":         "0.0.0.0",
		"TRUSTED_PROXY_CIDRS": "127.0.0.1/32",
	}))
	if err == nil {
		t.Fatal("Load() error = nil, want error for non-loopback LISTEN_HOST in staging")
	}
	if !strings.Contains(err.Error(), "LISTEN_HOST") {
		t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "LISTEN_HOST")
	}
}

func TestLoad_ListenHostLoopbackAcceptedInStaging(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":        "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":       "https://aboutme.vn",
		"ENV":                 "staging",
		"LISTEN_HOST":         "127.0.0.1",
		"TRUSTED_PROXY_CIDRS": "127.0.0.1/32",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.ListenHost != "127.0.0.1" {
		t.Errorf("ListenHost = %q, want %q", got.ListenHost, "127.0.0.1")
	}
}

// TestLoad_TrustedProxyCIDRs_RequiredInProd guards the other half of the
// security review's finding 2: production must fail closed when its
// client-IP trust configuration is absent, never silently trust no one
// (which collapses every real client into one bucket) or everyone (a
// spoofing bypass).
func TestLoad_TrustedProxyCIDRs_RequiredInProd(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"ENV":           "prod",
	}))
	if err == nil {
		t.Fatal("Load() error = nil, want error: TRUSTED_PROXY_CIDRS is required when ENV=prod")
	}
	if !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
		t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "TRUSTED_PROXY_CIDRS")
	}
}

// TestLoad_TrustedProxyCIDRs_RequiredInStaging is the staging counterpart
// of TestLoad_TrustedProxyCIDRs_RequiredInProd: staging now fails closed on
// an absent client-IP trust boundary too (re-review of security review
// finding #2 — staging exists to validate the real deployment topology
// before it reaches prod).
func TestLoad_TrustedProxyCIDRs_RequiredInStaging(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"ENV":           "staging",
	}))
	if err == nil {
		t.Fatal("Load() error = nil, want error: TRUSTED_PROXY_CIDRS is required when ENV=staging")
	}
	if !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
		t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "TRUSTED_PROXY_CIDRS")
	}
}

func TestLoad_TrustedProxyCIDRs_OptionalOutsideProd(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"ENV":           "dev",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.TrustedProxyCIDRs != nil {
		t.Errorf("TrustedProxyCIDRs = %v, want nil", got.TrustedProxyCIDRs)
	}
}

func TestLoad_TrustedProxyCIDRs_ParsesCommaSeparatedList(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":        "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":       "https://aboutme.vn",
		"ENV":                 "prod",
		"LISTEN_HOST":         "127.0.0.1",
		"TRUSTED_PROXY_CIDRS": " 127.0.0.1/32 , ::1/128 ",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}

	want := []netip.Prefix{
		netip.MustParsePrefix("127.0.0.1/32"),
		netip.MustParsePrefix("::1/128"),
	}
	if !slices.Equal(got.TrustedProxyCIDRs, want) {
		t.Errorf("TrustedProxyCIDRs = %v, want %v", got.TrustedProxyCIDRs, want)
	}
}

// TestLoad_TrustedProxyCIDRs_RejectsTrustEveryone guards the static half of
// security review finding #2: a /0 CIDR passes ordinary CIDR syntax
// validation but trusts every possible peer, silently defeating the
// client-IP trust boundary rather than merely narrowing it to a wrong
// range. Load must reject it outright rather than starting successfully
// with a value this implausible for any deployment topology.
func TestLoad_TrustedProxyCIDRs_RejectsTrustEveryone(t *testing.T) {
	t.Parallel()

	for _, cidr := range []string{"0.0.0.0/0", "::/0"} {
		t.Run(cidr, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(env(map[string]string{
				"DATABASE_URL":        "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN":       "https://aboutme.vn",
				"ENV":                 "dev",
				"TRUSTED_PROXY_CIDRS": cidr,
			}))
			if err == nil {
				t.Fatalf("Load() error = nil, want error rejecting %q as an implausible trust-everyone CIDR", cidr)
			}
			if !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
				t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "TRUSTED_PROXY_CIDRS")
			}
		})
	}
}

// TestLoad_TrustedProxyCIDRs_RejectsBroaderThanMinimumPrefix is the
// re-review's regression test for security review finding #2's minor
// residual (M5): the old check only special-cased a literal /0, which a
// deployment could trivially evade by splitting the whole IPv4 space into
// two /1 prefixes that individually pass ordinary CIDR syntax. Load must
// instead enforce a minimum prefix length per address family (narrower
// than /8 for IPv4, /48 for IPv6 is rejected outright) so every configured
// prefix is checked on its own, closing the split-evasion case along with
// any other implausibly broad single CIDR.
func TestLoad_TrustedProxyCIDRs_RejectsBroaderThanMinimumPrefix(t *testing.T) {
	t.Parallel()

	cases := []string{
		"0.0.0.0/1",             // half of IPv4 alone
		"128.0.0.0/1",           // the other half alone
		"0.0.0.0/1,128.0.0.0/1", // split-evasion: together cover all of IPv4, but each must still be rejected on its own
		"10.0.0.0/7",            // one bit broader than the IPv4 minimum (/8)
		"::/47",                 // one bit broader than the IPv6 minimum (/48)
	}
	for _, cidr := range cases {
		t.Run(cidr, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(env(map[string]string{
				"DATABASE_URL":        "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN":       "https://aboutme.vn",
				"ENV":                 "dev",
				"TRUSTED_PROXY_CIDRS": cidr,
			}))
			if err == nil {
				t.Fatalf("Load() error = nil, want error rejecting %q as broader than the minimum allowed prefix", cidr)
			}
			if !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
				t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "TRUSTED_PROXY_CIDRS")
			}
		})
	}
}

// TestLoad_TrustedProxyCIDRs_NarrowPrefixesStillAccepted proves the minimum-
// prefix rejection above is specifically about implausibly broad CIDRs, not
// about broad prefixes in general: a deployment legitimately trusting a
// large-but-bounded internal range (e.g. all of RFC 1918 10.0.0.0/8, or the
// IPv6 minimum /48) must still be accepted.
func TestLoad_TrustedProxyCIDRs_NarrowPrefixesStillAccepted(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":        "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":       "https://aboutme.vn",
		"ENV":                 "dev",
		"TRUSTED_PROXY_CIDRS": "10.0.0.0/8, 2001:db8::/48",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	want := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("2001:db8::/48"),
	}
	if !slices.Equal(got.TrustedProxyCIDRs, want) {
		t.Errorf("TrustedProxyCIDRs = %v, want %v", got.TrustedProxyCIDRs, want)
	}
}

// TestLoad_TrustedProxyCIDRs_RejectsIPv4MappedPrefix is the regression test
// for re-review minor M-D: an IPv4-in-IPv6 mapped prefix passed the /48 IPv6
// minimum-prefix bound yet can never match a real peer, because
// api.resolveClientIP Unmap()s every peer address before testing trust — so
// such a prefix silently trusts nobody. Load must reject it (naming the v4
// form) rather than accept a value that reads as configured but is inert.
func TestLoad_TrustedProxyCIDRs_RejectsIPv4MappedPrefix(t *testing.T) {
	t.Parallel()

	cases := []string{
		"::ffff:0.0.0.0/104",     // every IPv4 address, expressed 4-in-6; passed the old /48 bound
		"::ffff:203.0.113.0/120", // a specific v4-mapped /24, still inert against unmapped peers
		"::ffff:10.90.0.0/124",   // the compose /28 written in mapped form
	}
	for _, cidr := range cases {
		t.Run(cidr, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(env(map[string]string{
				"DATABASE_URL":        "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN":       "https://aboutme.vn",
				"ENV":                 "dev",
				"TRUSTED_PROXY_CIDRS": cidr,
			}))
			if err == nil {
				t.Fatalf("Load() error = nil, want error rejecting %q as an IPv4-in-IPv6 mapped prefix", cidr)
			}
			if !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
				t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "TRUSTED_PROXY_CIDRS")
			}
			if !strings.Contains(err.Error(), "IPv4-in-IPv6") {
				t.Errorf("Load() error = %q, want it to explain the IPv4-in-IPv6 mapping so the "+
					"operator knows to use the plain IPv4 form", err.Error())
			}
		})
	}
}

func TestLoad_TrustedProxyCIDRs_InvalidCIDR(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{
		"DATABASE_URL":        "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":       "https://aboutme.vn",
		"ENV":                 "dev",
		"TRUSTED_PROXY_CIDRS": "not-a-cidr",
	}))
	if err == nil {
		t.Fatal("Load() error = nil, want error for invalid TRUSTED_PROXY_CIDRS")
	}
	if !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
		t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "TRUSTED_PROXY_CIDRS")
	}
}
