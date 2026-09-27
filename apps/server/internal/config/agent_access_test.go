package config_test

import (
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/config"
)

// The registration egress-range list follows
// docs/design/mcp-client-compatibility.md, "Registration rate from shared
// egress": deployment configuration, empty by default.
func TestLoad_OAuthRegisterEgressCIDRs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, raw string
		want      []netip.Prefix
	}{
		{name: "unset is none", raw: "", want: nil},
		{name: "blank is none", raw: "  ", want: nil},
		{name: "one IPv4 range", raw: "160.79.104.0/21", want: []netip.Prefix{netip.MustParsePrefix("160.79.104.0/21")}},
		{name: "IPv4 and IPv6 with spaces", raw: " 160.79.104.0/21 , 2001:db8::/32 ", want: []netip.Prefix{
			netip.MustParsePrefix("160.79.104.0/21"), netip.MustParsePrefix("2001:db8::/32"),
		}},
		{name: "narrowest IPv4 allowed", raw: "198.51.0.0/16", want: []netip.Prefix{netip.MustParsePrefix("198.51.0.0/16")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vars := validDevEnv()
			vars["OAUTH_REGISTER_EGRESS_CIDRS"] = tc.raw
			got, err := config.Load(env(vars))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if !slices.Equal(got.AgentAccess.OAuthRegisterEgressRanges, tc.want) {
				t.Fatalf("OAuthRegisterEgressRanges = %v, want %v", got.AgentAccess.OAuthRegisterEgressRanges, tc.want)
			}
		})
	}
}

func TestLoad_OAuthRegisterEgressCIDRsRejections(t *testing.T) {
	t.Parallel()
	seventeen := make([]string, 17)
	for i := range seventeen {
		seventeen[i] = netip.AddrFrom4([4]byte{10, byte(i), 0, 0}).String() + "/16"
	}
	for _, tc := range []struct{ name, raw string }{
		{name: "unparseable field", raw: "160.79.104.0/21,not-a-cidr-sentinel"},
		{name: "bare address", raw: "160.79.104.1"},
		{name: "empty field", raw: "160.79.104.0/21,,2001:db8::/32"},
		{name: "non-canonical prefix", raw: "160.79.104.1/21"},
		{name: "IPv4-mapped IPv6 prefix", raw: "::ffff:160.79.104.0/117"},
		{name: "IPv4 broader than /16", raw: "160.79.0.0/15"},
		{name: "IPv6 broader than /32", raw: "2001:d00::/24"},
		{name: "more than 16 entries", raw: strings.Join(seventeen, ",")},
		{name: "overlapping entries", raw: "160.79.104.0/21,160.79.105.0/24"},
		{name: "duplicate entries", raw: "160.79.104.0/21,160.79.104.0/21"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vars := validDevEnv()
			vars["OAUTH_REGISTER_EGRESS_CIDRS"] = tc.raw
			_, err := config.Load(env(vars))
			if err == nil {
				t.Fatal("Load() error = nil, want OAUTH_REGISTER_EGRESS_CIDRS rejection")
			}
			if !strings.Contains(err.Error(), "OAUTH_REGISTER_EGRESS_CIDRS") {
				t.Fatalf("Load() error = %q, want the variable name", err)
			}
			if strings.Contains(err.Error(), "sentinel") {
				t.Fatalf("Load() error = %q echoes an unparseable field", err)
			}
		})
	}
}

func TestConfig_ValidateAgentAccessRequiresFrozenRegisterBudgets(t *testing.T) {
	t.Parallel()
	vars := validDevEnv()
	vars["MCP_ENABLED"] = "true"
	vars["OAUTH_REGISTER_EGRESS_CIDRS"] = "160.79.104.0/21"
	loaded, err := config.Load(env(vars))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := loaded.ValidateAgentAccess(); err != nil {
		t.Fatalf("ValidateAgentAccess() error = %v for the loaded config", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*config.AgentAccessConfig)
	}{
		{"range budget", func(a *config.AgentAccessConfig) { a.OAuthRegisterRangeRequests = 1_000 }},
		{"global budget", func(a *config.AgentAccessConfig) { a.OAuthRegisterGlobalRequests = 10_000 }},
		{"zero global budget", func(a *config.AgentAccessConfig) { a.OAuthRegisterGlobalRequests = 0 }},
		{"non-canonical range", func(a *config.AgentAccessConfig) {
			a.OAuthRegisterEgressRanges = []netip.Prefix{netip.MustParsePrefix("160.79.104.1/21")}
		}},
		{"overlapping ranges", func(a *config.AgentAccessConfig) {
			a.OAuthRegisterEgressRanges = []netip.Prefix{
				netip.MustParsePrefix("160.79.104.0/21"), netip.MustParsePrefix("160.79.104.0/22"),
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := loaded
			cfg.AgentAccess.OAuthRegisterEgressRanges = slices.Clone(loaded.AgentAccess.OAuthRegisterEgressRanges)
			tc.mutate(&cfg.AgentAccess)
			if err := cfg.ValidateAgentAccess(); err == nil {
				t.Fatal("ValidateAgentAccess() error = nil")
			}
		})
	}
}
