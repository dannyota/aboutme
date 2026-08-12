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
// counterpart of TestLoad_ValidConfig: staging carries the same client-IP
// trust-boundary strictness as prod. A valid staging config must supply
// TRUSTED_PROXY_CIDRS and parse it exactly like prod does.
func TestLoad_ValidConfig_StagingRequiresTrustBoundary(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":           "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":          "https://aboutme.vn",
		"ENV":                    "staging",
		"LISTEN_HOST":            "127.0.0.1",
		"TRUSTED_PROXY_CIDRS":    "127.0.0.1/32,::1/128",
		"GOOGLE_CLIENT_ID":       "client-id",
		"GOOGLE_CLIENT_SECRET":   "client-secret",
		"GITHUB_CLIENT_ID":       "github-client-id",
		"GITHUB_CLIENT_SECRET":   "github-client-secret",
		"LINKEDIN_CLIENT_ID":     "client-id",
		"LINKEDIN_CLIENT_SECRET": "client-secret",
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
				// TestLoad_TrustedProxyCIDRs_RequiredInStaging),
				// GOOGLE_CLIENT_ID/SECRET (see
				// TestLoad_GoogleCredentials_RequiredInProd/Staging),
				// GITHUB_CLIENT_ID/SECRET (see
				// TestLoad_GitHubCredentials_RequiredInProd/Staging), and
				// LINKEDIN_CLIENT_ID/SECRET (see
				// TestLoad_LinkedInCredentials_RequiredInProd/Staging);
				// LISTEN_HOST is left at its loopback default.
				vars["TRUSTED_PROXY_CIDRS"] = "127.0.0.1/32,::1/128"
				vars["GOOGLE_CLIENT_ID"] = "client-id"
				vars["GOOGLE_CLIENT_SECRET"] = "client-secret"
				vars["GITHUB_CLIENT_ID"] = "github-client-id"
				vars["GITHUB_CLIENT_SECRET"] = "github-client-secret"
				vars["LINKEDIN_CLIENT_ID"] = "client-id"
				vars["LINKEDIN_CLIENT_SECRET"] = "client-secret"
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

// TestLoad_ListenHostNonLoopbackRejectedInProd proves production cannot
// expose port 8080 around Caddy's origin-secret boundary.
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
		"DATABASE_URL":           "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":          "https://aboutme.vn",
		"ENV":                    "prod",
		"LISTEN_HOST":            "127.0.0.1",
		"TRUSTED_PROXY_CIDRS":    "127.0.0.1/32",
		"GOOGLE_CLIENT_ID":       "client-id",
		"GOOGLE_CLIENT_SECRET":   "client-secret",
		"GITHUB_CLIENT_ID":       "github-client-id",
		"GITHUB_CLIENT_SECRET":   "github-client-secret",
		"LINKEDIN_CLIENT_ID":     "client-id",
		"LINKEDIN_CLIENT_SECRET": "client-secret",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.ListenHost != "127.0.0.1" {
		t.Errorf("ListenHost = %q, want %q", got.ListenHost, "127.0.0.1")
	}
}

// TestLoad_ListenHostNonLoopbackRejectedInStaging is the staging
// counterpart of TestLoad_ListenHostNonLoopbackRejectedInProd. Staging uses
// the same client-IP trust boundary as prod, so it also rejects a non-loopback
// LISTEN_HOST.
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
		"DATABASE_URL":           "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":          "https://aboutme.vn",
		"ENV":                    "staging",
		"LISTEN_HOST":            "127.0.0.1",
		"TRUSTED_PROXY_CIDRS":    "127.0.0.1/32",
		"GOOGLE_CLIENT_ID":       "client-id",
		"GOOGLE_CLIENT_SECRET":   "client-secret",
		"GITHUB_CLIENT_ID":       "github-client-id",
		"GITHUB_CLIENT_SECRET":   "github-client-secret",
		"LINKEDIN_CLIENT_ID":     "client-id",
		"LINKEDIN_CLIENT_SECRET": "client-secret",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.ListenHost != "127.0.0.1" {
		t.Errorf("ListenHost = %q, want %q", got.ListenHost, "127.0.0.1")
	}
}

// TestLoad_TrustedProxyCIDRs_RequiredInProd proves production fails closed when
// its client-IP trust configuration is absent rather than silently trusting no one
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
// of TestLoad_TrustedProxyCIDRs_RequiredInProd: staging fails closed on
// an absent client-IP trust boundary too. Staging validates the real
// deployment topology before it reaches prod.
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
		"DATABASE_URL":           "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":          "https://aboutme.vn",
		"ENV":                    "prod",
		"LISTEN_HOST":            "127.0.0.1",
		"TRUSTED_PROXY_CIDRS":    " 127.0.0.1/32 , ::1/128 ",
		"GOOGLE_CLIENT_ID":       "client-id",
		"GOOGLE_CLIENT_SECRET":   "client-secret",
		"GITHUB_CLIENT_ID":       "github-client-id",
		"GITHUB_CLIENT_SECRET":   "github-client-secret",
		"LINKEDIN_CLIENT_ID":     "client-id",
		"LINKEDIN_CLIENT_SECRET": "client-secret",
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

// TestLoad_TrustedProxyCIDRs_RejectsTrustEveryone proves a /0 CIDR passes
// ordinary CIDR syntax validation but trusts every possible peer, defeating the
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

// TestLoad_TrustedProxyCIDRs_RejectsBroaderThanMinimumPrefix proves a literal
// /0 check is insufficient because the whole IPv4 space can be split into
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

// TestLoad_TrustedProxyCIDRs_NarrowPrefixesStillAccepted proves the minimum
// prefix rejection is specifically about implausibly broad CIDRs, not
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

// TestLoad_TrustedProxyCIDRs_RejectsIPv4MappedPrefix proves an IPv4-in-IPv6
// mapped prefix can pass the /48 IPv6 minimum-prefix bound yet can never match
// a real peer, because
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

// ---- GOOGLE_CLIENT_ID / GOOGLE_CLIENT_SECRET --------------------------

func TestLoad_GoogleCredentials_ValidConfig(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":         "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":        "https://aboutme.vn",
		"ENV":                  "dev",
		"GOOGLE_CLIENT_ID":     "test-client-id",
		"GOOGLE_CLIENT_SECRET": "test-client-secret",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.GoogleClientID != "test-client-id" {
		t.Errorf("GoogleClientID = %q, want %q", got.GoogleClientID, "test-client-id")
	}
	if got.GoogleClientSecret != "test-client-secret" {
		t.Errorf("GoogleClientSecret = %q, want %q", got.GoogleClientSecret, "test-client-secret")
	}
}

// TestLoad_GoogleCredentials_OptionalInDev proves a developer working on an
// unrelated feature is never forced to obtain real Google OAuth
// credentials just to start the server in dev — the same
// optional-outside-prod/staging shape TRUSTED_PROXY_CIDRS already has.
func TestLoad_GoogleCredentials_OptionalInDev(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"ENV":           "dev",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.GoogleClientID != "" {
		t.Errorf("GoogleClientID = %q, want empty (optional in dev)", got.GoogleClientID)
	}
	if got.GoogleClientSecret != "" {
		t.Errorf("GoogleClientSecret = %q, want empty (optional in dev)", got.GoogleClientSecret)
	}
}

// TestLoad_GoogleCredentials_RequiredInProd guards the fail-closed half: a
// production server cannot offer "Sign in with Google" without real
// credentials, so Load must refuse to start rather than silently booting
// with an empty client id/secret that would only fail later, per-request,
// against the real Google endpoint.
func TestLoad_GoogleCredentials_RequiredInProd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		vars map[string]string
	}{
		{
			name: "missing client id",
			vars: map[string]string{"GOOGLE_CLIENT_SECRET": "secret"},
		},
		{
			name: "missing client secret",
			vars: map[string]string{"GOOGLE_CLIENT_ID": "client-id"},
		},
		{
			name: "missing both",
			vars: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vars := map[string]string{
				"DATABASE_URL":        "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN":       "https://aboutme.vn",
				"ENV":                 "prod",
				"LISTEN_HOST":         "127.0.0.1",
				"TRUSTED_PROXY_CIDRS": "127.0.0.1/32",
			}
			for k, v := range tt.vars {
				vars[k] = v
			}

			_, err := config.Load(env(vars))
			if err == nil {
				t.Fatal("Load() error = nil, want error: GOOGLE_CLIENT_ID/SECRET required when ENV=prod")
			}
			if !strings.Contains(err.Error(), "GOOGLE_CLIENT_ID") && !strings.Contains(err.Error(), "GOOGLE_CLIENT_SECRET") {
				t.Errorf("Load() error = %q, want it to name GOOGLE_CLIENT_ID or GOOGLE_CLIENT_SECRET", err.Error())
			}
		})
	}
}

// TestLoad_GoogleCredentials_RequiredInStaging is the staging counterpart
// of TestLoad_GoogleCredentials_RequiredInProd — staging shares prod's
// strictness so a misconfiguration is caught before it reaches prod.
func TestLoad_GoogleCredentials_RequiredInStaging(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{
		"DATABASE_URL":        "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":       "https://aboutme.vn",
		"ENV":                 "staging",
		"LISTEN_HOST":         "127.0.0.1",
		"TRUSTED_PROXY_CIDRS": "127.0.0.1/32",
	}))
	if err == nil {
		t.Fatal("Load() error = nil, want error: GOOGLE_CLIENT_ID/SECRET required when ENV=staging")
	}
	if !strings.Contains(err.Error(), "GOOGLE_CLIENT_ID") {
		t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "GOOGLE_CLIENT_ID")
	}
}

// ---- GITHUB_CLIENT_ID / GITHUB_CLIENT_SECRET --------------------------
//
// GitHub login is plain OAuth2, not OIDC -- no discovery, no
// issuer, no nonce -- but its client credentials carry the identical
// fail-closed requirement as Google's: same optional-in-dev,
// required-in-prod/staging shape, mirrored test-for-test below.

func TestLoad_GitHubCredentials_ValidConfig(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":         "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":        "https://aboutme.vn",
		"ENV":                  "dev",
		"GITHUB_CLIENT_ID":     "test-github-client-id",
		"GITHUB_CLIENT_SECRET": "test-github-client-secret",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.GitHubClientID != "test-github-client-id" {
		t.Errorf("GitHubClientID = %q, want %q", got.GitHubClientID, "test-github-client-id")
	}
	if got.GitHubClientSecret != "test-github-client-secret" {
		t.Errorf("GitHubClientSecret = %q, want %q", got.GitHubClientSecret, "test-github-client-secret")
	}
}

// TestLoad_GitHubCredentials_OptionalInDev proves a developer working on an
// unrelated feature is never forced to obtain real GitHub OAuth
// credentials just to start the server in dev — the same
// optional-outside-prod/staging shape GOOGLE_CLIENT_ID/SECRET already has.
func TestLoad_GitHubCredentials_OptionalInDev(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"ENV":           "dev",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.GitHubClientID != "" {
		t.Errorf("GitHubClientID = %q, want empty (optional in dev)", got.GitHubClientID)
	}
	if got.GitHubClientSecret != "" {
		t.Errorf("GitHubClientSecret = %q, want empty (optional in dev)", got.GitHubClientSecret)
	}
}

// TestLoad_GitHubCredentials_RequiredInProd guards the fail-closed half: a
// production server cannot offer "Sign in with GitHub" without real
// credentials, so Load must refuse to start rather than silently booting
// with an empty client id/secret that would only fail later, per-request,
// against the real GitHub endpoint. Every case also supplies valid Google
// credentials, so the failure asserted here is unambiguously GitHub's own
// (not Google's check firing first). LinkedIn credentials are NOT needed
// here: Load checks GitHub before LinkedIn (see Load's own call order), so
// a missing GitHub credential is always reported first regardless of
// LinkedIn's state.
func TestLoad_GitHubCredentials_RequiredInProd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		vars map[string]string
	}{
		{
			name: "missing client id",
			vars: map[string]string{"GITHUB_CLIENT_SECRET": "secret"},
		},
		{
			name: "missing client secret",
			vars: map[string]string{"GITHUB_CLIENT_ID": "client-id"},
		},
		{
			name: "missing both",
			vars: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vars := map[string]string{
				"DATABASE_URL":         "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN":        "https://aboutme.vn",
				"ENV":                  "prod",
				"LISTEN_HOST":          "127.0.0.1",
				"TRUSTED_PROXY_CIDRS":  "127.0.0.1/32",
				"GOOGLE_CLIENT_ID":     "client-id",
				"GOOGLE_CLIENT_SECRET": "client-secret",
			}
			for k, v := range tt.vars {
				vars[k] = v
			}

			_, err := config.Load(env(vars))
			if err == nil {
				t.Fatal("Load() error = nil, want error: GITHUB_CLIENT_ID/SECRET required when ENV=prod")
			}
			if !strings.Contains(err.Error(), "GITHUB_CLIENT_ID") && !strings.Contains(err.Error(), "GITHUB_CLIENT_SECRET") {
				t.Errorf("Load() error = %q, want it to name GITHUB_CLIENT_ID or GITHUB_CLIENT_SECRET", err.Error())
			}
		})
	}
}

// TestLoad_GitHubCredentials_RequiredInStaging is the staging counterpart
// of TestLoad_GitHubCredentials_RequiredInProd — staging shares prod's
// strictness so a misconfiguration is caught before it reaches prod.
func TestLoad_GitHubCredentials_RequiredInStaging(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{
		"DATABASE_URL":         "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":        "https://aboutme.vn",
		"ENV":                  "staging",
		"LISTEN_HOST":          "127.0.0.1",
		"TRUSTED_PROXY_CIDRS":  "127.0.0.1/32",
		"GOOGLE_CLIENT_ID":     "client-id",
		"GOOGLE_CLIENT_SECRET": "client-secret",
	}))
	if err == nil {
		t.Fatal("Load() error = nil, want error: GITHUB_CLIENT_ID/SECRET required when ENV=staging")
	}
	if !strings.Contains(err.Error(), "GITHUB_CLIENT_ID") {
		t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "GITHUB_CLIENT_ID")
	}
}

// ---- LINKEDIN_CLIENT_ID / LINKEDIN_CLIENT_SECRET ----------------------

func TestLoad_LinkedInCredentials_ValidConfig(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":           "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":          "https://aboutme.vn",
		"ENV":                    "dev",
		"LINKEDIN_CLIENT_ID":     "test-client-id",
		"LINKEDIN_CLIENT_SECRET": "test-client-secret",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.LinkedInClientID != "test-client-id" {
		t.Errorf("LinkedInClientID = %q, want %q", got.LinkedInClientID, "test-client-id")
	}
	if got.LinkedInClientSecret != "test-client-secret" {
		t.Errorf("LinkedInClientSecret = %q, want %q", got.LinkedInClientSecret, "test-client-secret")
	}
}

// TestLoad_LinkedInCredentials_OptionalInDev mirrors
// TestLoad_GoogleCredentials_OptionalInDev: a developer working on an
// unrelated feature is never forced to obtain real LinkedIn OAuth
// credentials just to start the server in dev.
func TestLoad_LinkedInCredentials_OptionalInDev(t *testing.T) {
	t.Parallel()

	got, err := config.Load(env(map[string]string{
		"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN": "https://aboutme.vn",
		"ENV":           "dev",
	}))
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if got.LinkedInClientID != "" {
		t.Errorf("LinkedInClientID = %q, want empty (optional in dev)", got.LinkedInClientID)
	}
	if got.LinkedInClientSecret != "" {
		t.Errorf("LinkedInClientSecret = %q, want empty (optional in dev)", got.LinkedInClientSecret)
	}
}

// TestLoad_LinkedInCredentials_RequiredInProd mirrors
// TestLoad_GoogleCredentials_RequiredInProd: a production server cannot
// offer "Sign in with LinkedIn" without real credentials, so Load must
// refuse to start rather than silently booting with an empty client
// id/secret that would only fail later, per-request, against the real
// LinkedIn endpoint. Google AND GitHub credentials are both set (valid)
// here: Load checks Google, then GitHub, then LinkedIn (Load's own call
// order), so both earlier checks must already pass or their own error
// would mask the LINKEDIN_CLIENT_* failure this test is actually
// targeting.
func TestLoad_LinkedInCredentials_RequiredInProd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		vars map[string]string
	}{
		{
			name: "missing client id",
			vars: map[string]string{"LINKEDIN_CLIENT_SECRET": "secret"},
		},
		{
			name: "missing client secret",
			vars: map[string]string{"LINKEDIN_CLIENT_ID": "client-id"},
		},
		{
			name: "missing both",
			vars: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vars := map[string]string{
				"DATABASE_URL":         "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN":        "https://aboutme.vn",
				"ENV":                  "prod",
				"LISTEN_HOST":          "127.0.0.1",
				"TRUSTED_PROXY_CIDRS":  "127.0.0.1/32",
				"GOOGLE_CLIENT_ID":     "google-client-id",
				"GOOGLE_CLIENT_SECRET": "google-client-secret",
				"GITHUB_CLIENT_ID":     "github-client-id",
				"GITHUB_CLIENT_SECRET": "github-client-secret",
			}
			for k, v := range tt.vars {
				vars[k] = v
			}

			_, err := config.Load(env(vars))
			if err == nil {
				t.Fatal("Load() error = nil, want error: LINKEDIN_CLIENT_ID/SECRET required when ENV=prod")
			}
			if !strings.Contains(err.Error(), "LINKEDIN_CLIENT_ID") && !strings.Contains(err.Error(), "LINKEDIN_CLIENT_SECRET") {
				t.Errorf("Load() error = %q, want it to name LINKEDIN_CLIENT_ID or LINKEDIN_CLIENT_SECRET", err.Error())
			}
		})
	}
}

// TestLoad_LinkedInCredentials_RequiredInStaging is the staging
// counterpart of TestLoad_LinkedInCredentials_RequiredInProd — staging
// shares prod's strictness so a misconfiguration is caught before it
// reaches prod. Google AND GitHub credentials are both set (valid) for
// the same reason as TestLoad_LinkedInCredentials_RequiredInProd above.
func TestLoad_LinkedInCredentials_RequiredInStaging(t *testing.T) {
	t.Parallel()

	_, err := config.Load(env(map[string]string{
		"DATABASE_URL":         "postgres://user:pass@localhost:5432/aboutme",
		"PUBLIC_ORIGIN":        "https://aboutme.vn",
		"ENV":                  "staging",
		"LISTEN_HOST":          "127.0.0.1",
		"TRUSTED_PROXY_CIDRS":  "127.0.0.1/32",
		"GOOGLE_CLIENT_ID":     "google-client-id",
		"GOOGLE_CLIENT_SECRET": "google-client-secret",
		"GITHUB_CLIENT_ID":     "github-client-id",
		"GITHUB_CLIENT_SECRET": "github-client-secret",
	}))
	if err == nil {
		t.Fatal("Load() error = nil, want error: LINKEDIN_CLIENT_ID/SECRET required when ENV=staging")
	}
	if !strings.Contains(err.Error(), "LINKEDIN_CLIENT_ID") {
		t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "LINKEDIN_CLIENT_ID")
	}
}

// ---- PUBLIC_ORIGIN format validation -----------------------------------

// TestLoad_PublicOrigin_ValidFormats proves the format check accepts every
// shape a real deployment's PUBLIC_ORIGIN legitimately takes: with and
// without an explicit port, https and http (dev).
func TestLoad_PublicOrigin_ValidFormats(t *testing.T) {
	t.Parallel()

	for _, origin := range []string{
		"https://aboutme.vn",
		"https://aboutme.vn:8443",
		"http://localhost:8080",
		"http://localhost",
	} {
		t.Run(origin, func(t *testing.T) {
			t.Parallel()

			got, err := config.Load(env(map[string]string{
				"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN": origin,
				"ENV":           "dev",
			}))
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
			if got.PublicOrigin != origin {
				t.Errorf("PublicOrigin = %q, want %q", got.PublicOrigin, origin)
			}
		})
	}
}

// TestLoad_PublicOrigin_InvalidFormats guards fail-fast format validation.
// PUBLIC_ORIGIN must parse as scheme://host[:port] only — no path, trailing slash,
// query, no fragment — since it becomes the base of every absolute OAuth
// redirect/callback URL this server builds (an unnoticed trailing slash or
// path would silently double up or corrupt every one of them).
func TestLoad_PublicOrigin_InvalidFormats(t *testing.T) {
	t.Parallel()

	cases := []string{
		"https://aboutme.vn/",         // trailing slash
		"https://aboutme.vn/api",      // path
		"https://aboutme.vn?x=1",      // query
		"https://aboutme.vn#frag",     // fragment
		"aboutme.vn",                  // no scheme
		"ftp://aboutme.vn",            // unsupported scheme
		"https://",                    // no host
		"not a url at all : : spaces", // unparseable
	}
	for _, origin := range cases {
		t.Run(origin, func(t *testing.T) {
			t.Parallel()

			_, err := config.Load(env(map[string]string{
				"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN": origin,
				"ENV":           "dev",
			}))
			if err == nil {
				t.Fatalf("Load() error = nil, want error rejecting PUBLIC_ORIGIN %q", origin)
			}
			if !strings.Contains(err.Error(), "PUBLIC_ORIGIN") {
				t.Errorf("Load() error = %q, want it to contain %q", err.Error(), "PUBLIC_ORIGIN")
			}
		})
	}
}

// TestLoad_PublicOrigin_Normalization proves the configured transformations
// run before exact comparison with each request's Origin header
// (auth.originAllowed, csrf.go) and before it is used to build every OAuth
// redirect/callback URL. A browser sends a
// normalized Origin (lowercase host, default port omitted), so mixed-case
// hosts and explicit default ports must normalize before comparison. The
// table proves: (1) scheme+host are lowercased, (2) a default port for the
// scheme (":80" on http, ":443" on https) is stripped, (3) a non-default port
// is preserved unchanged, (4)
// an origin already in canonical form round-trips byte-identical.
func TestLoad_PublicOrigin_Normalization(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"already canonical, unchanged", "https://aboutme.vn", "https://aboutme.vn"},
		{"uppercase host lowercased", "https://ABOUTME.vn", "https://aboutme.vn"},
		{"mixed-case scheme and host lowercased", "Https://Aboutme.VN", "https://aboutme.vn"},
		{"https default port 443 stripped", "https://aboutme.vn:443", "https://aboutme.vn"},
		{"http default port 80 stripped", "http://aboutme.vn:80", "http://aboutme.vn"},
		{"https on port 80 is NOT the default for https, preserved", "https://aboutme.vn:80", "https://aboutme.vn:80"},
		{"http on port 443 is NOT the default for http, preserved", "http://aboutme.vn:443", "http://aboutme.vn:443"},
		{"non-default port preserved", "https://aboutme.vn:8443", "https://aboutme.vn:8443"},
		{"uppercase host with non-default port", "https://ABOUTME.vn:8443", "https://aboutme.vn:8443"},
		{"localhost with default http port stripped", "http://localhost:80", "http://localhost"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := config.Load(env(map[string]string{
				"DATABASE_URL":  "postgres://user:pass@localhost:5432/aboutme",
				"PUBLIC_ORIGIN": tc.input,
				"ENV":           "dev",
			}))
			if err != nil {
				t.Fatalf("Load() unexpected error: %v", err)
			}
			if got.PublicOrigin != tc.want {
				t.Errorf("PublicOrigin = %q, want %q (input %q)", got.PublicOrigin, tc.want, tc.input)
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
