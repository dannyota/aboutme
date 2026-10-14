package config

import (
	"errors"
	"net/netip"
	"time"
)

// CIMDConfig holds the complete metadata-fetch budgets from docs/design/budgets.md.
type CIMDConfig struct {
	Enabled           bool
	DevStubAddr       string
	MaxURLBytes       int
	MaxBodyBytes      int64
	ConnectTimeout    time.Duration
	CompleteTimeout   time.Duration
	ConcurrentFetches int
	AddressRequests   int
	AddressWindow     time.Duration
	URLRequests       int
	URLWindow         time.Duration
	GlobalRequests    int
	GlobalWindow      time.Duration
	CacheFloor        time.Duration
	CacheCeiling      time.Duration
	MaxCacheEntries   int
	MaxCacheBytes     int
}

func loadCIMDConfig(getenv func(string) string, environment string) (CIMDConfig, error) {
	c := CIMDConfig{
		MaxURLBytes: 256, MaxBodyBytes: 5120,
		ConnectTimeout: 2 * time.Second, CompleteTimeout: 5 * time.Second,
		ConcurrentFetches: 4, AddressRequests: 30, AddressWindow: time.Hour,
		URLRequests: 6, URLWindow: time.Minute, GlobalRequests: 600, GlobalWindow: time.Hour,
		CacheFloor: 5 * time.Minute, CacheCeiling: time.Hour,
		MaxCacheEntries: 1000, MaxCacheBytes: 2 * 1024 * 1024,
		DevStubAddr: getenv("OAUTH_CIMD_DEV_STUB_ADDR"),
	}
	switch getenv("OAUTH_CIMD_ENABLED") {
	case "", "false":
	case "true":
		c.Enabled = true
	default:
		return CIMDConfig{}, errors.New("config: OAUTH_CIMD_ENABLED must be true or false")
	}
	if err := validateCIMDStub(c.DevStubAddr, environment); err != nil {
		return CIMDConfig{}, err
	}
	return c, nil
}

func validateCIMDStub(raw, environment string) error {
	if raw == "" {
		return nil
	}
	a, err := netip.ParseAddrPort(raw)
	if environment != "dev" || err != nil || !a.Addr().IsLoopback() || a.Port() == 0 {
		return errors.New("config: OAUTH_CIMD_DEV_STUB_ADDR requires dev and a literal loopback address with a nonzero port")
	}
	return nil
}

func (c Config) validateCIMD() error {
	if err := validateCIMDStub(c.AgentAccess.CIMD.DevStubAddr, c.Env); err != nil {
		return err
	}
	if !c.AgentAccess.CIMD.Enabled {
		return nil
	}
	expected, err := loadCIMDConfig(func(key string) string {
		if key == "OAUTH_CIMD_ENABLED" {
			return "true"
		}
		if key == "OAUTH_CIMD_DEV_STUB_ADDR" {
			return c.AgentAccess.CIMD.DevStubAddr
		}
		return ""
	}, c.Env)
	if err != nil {
		return err
	}
	if c.AgentAccess.CIMD != expected {
		return errors.New("config: enabled CIMD configuration is incomplete")
	}
	return nil
}
