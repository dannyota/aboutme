package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/config"
)

func TestLoadCIMDFlag(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "false", "true", "TRUE", "1", " true "} {
		t.Run(raw, func(t *testing.T) {
			vars := validDevEnv()
			vars["OAUTH_CIMD_ENABLED"] = raw
			cfg, err := config.Load(env(vars))
			valid := raw == "" || raw == "false" || raw == "true"
			if (err == nil) != valid {
				t.Fatalf("Load error = %v, valid = %v", err, valid)
			}
			if valid && cfg.AgentAccess.CIMD.Enabled != (raw == "true") {
				t.Fatal("CIMD startup flag differs")
			}
		})
	}
}

func TestLoadCIMDStubRestriction(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		environment, address string
		valid                bool
	}{
		{"dev", "127.0.0.1:20095", true},
		{"dev", "[::1]:20095", true},
		{"dev", "localhost:20095", false},
		{"dev", "8.8.8.8:443", false},
		{"dev", "127.0.0.1:0", false},
		{"dev", "127.0.0.1:65536", false},
		{"prod", "127.0.0.1:20095", false},
		{"staging", "127.0.0.1:20095", false},
	} {
		for _, flags := range []struct{ mcp, cimd string }{
			{"false", "false"}, {"false", "true"}, {"true", "false"}, {"true", "true"},
		} {
			t.Run(tc.environment+tc.address+flags.mcp+flags.cimd, func(t *testing.T) {
				vars := validDevEnv()
				vars["ENV"] = tc.environment
				vars["OAUTH_CIMD_DEV_STUB_ADDR"] = tc.address
				vars["MCP_ENABLED"] = flags.mcp
				vars["OAUTH_CIMD_ENABLED"] = flags.cimd
				_, loadErr := config.Load(env(vars))
				if (loadErr == nil) != tc.valid {
					t.Fatalf("Load error = %v, valid = %v", loadErr, tc.valid)
				}
				if loadErr != nil && !strings.Contains(loadErr.Error(), "OAUTH_CIMD_DEV_STUB_ADDR") {
					t.Fatalf("wrong rejection: %v", loadErr)
				}
			})
		}
	}
}

func TestCIMDConfigurationCannotBypassStartupValidation(t *testing.T) {
	t.Parallel()
	vars := validDevEnv()
	vars["OAUTH_CIMD_ENABLED"] = "true"
	loaded, err := config.Load(env(vars))
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*config.Config){
		func(c *config.Config) { c.Env = "prod"; c.AgentAccess.CIMD.DevStubAddr = "127.0.0.1:20095" },
		func(c *config.Config) { c.AgentAccess.CIMD.CompleteTimeout = time.Minute },
		func(c *config.Config) { c.AgentAccess.CIMD.MaxCacheBytes = 0 },
	} {
		cfg := loaded
		mutate(&cfg)
		if cfg.ValidateAgentAccess() == nil {
			t.Fatal("incomplete or unsafe CIMD configuration accepted")
		}
	}
}
