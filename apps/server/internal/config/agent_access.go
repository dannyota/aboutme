package config

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"
)

type AgentAccessConfig struct {
	CIMD                        CIMDConfig
	Enabled                     bool
	OAuthRegisterRequests       int
	OAuthRegisterEgressRanges   []netip.Prefix
	OAuthRegisterRangeRequests  int
	OAuthRegisterGlobalRequests int
	OAuthRegisterWindow         time.Duration
	OAuthTokenRequests          int
	OAuthTokenWindow            time.Duration
	OAuthFailedGrantLimit       int
	OAuthFailedGrantWindow      time.Duration
	MCPTokenRequests            int
	MCPTokenWindow              time.Duration
	MCPUserRequests             int
	MCPUserWindow               time.Duration
	MCPConcurrentPerUser        int
	OAuthLiveGrantLimit         int
	MCPBodyLimitBytes           int64
	MaxRateKeys                 int
}

const (
	oauthRegisterRequests       = 5
	oauthRegisterRangeRequests  = 120
	oauthRegisterGlobalRequests = 600
	oauthRegisterWindow         = time.Hour
	oauthTokenRequests          = 30
	oauthTokenWindow            = time.Minute
	oauthFailedGrantLimit       = 10
	oauthFailedGrantWindow      = 15 * time.Minute
	mcpTokenRequests            = 120
	mcpTokenWindow              = time.Minute
	mcpUserRequests             = 240
	mcpUserWindow               = time.Minute
	mcpConcurrentPerUser        = 4
	oauthLiveGrantLimit         = 10
	mcpBodyLimitBytes           = 4_194_304
	agentRateMaxKeys            = 10_000
)

const (
	maxRegisterEgressRanges         = 16
	minRegisterEgressPrefixBitsIPv4 = 16
	minRegisterEgressPrefixBitsIPv6 = 32
)

const registerEgressVar = "OAUTH_REGISTER_EGRESS_CIDRS"

func loadAgentAccessConfig(rawEnabled, rawEgress string) (AgentAccessConfig, error) {
	var enabled bool
	switch strings.TrimSpace(rawEnabled) {
	case "", "false":
	case "true":
		enabled = true
	default:
		return AgentAccessConfig{}, errors.New("config: MCP_ENABLED must be true or false")
	}
	egress, err := loadRegisterEgressRanges(rawEgress)
	if err != nil {
		return AgentAccessConfig{}, err
	}
	return AgentAccessConfig{
		Enabled:                     enabled,
		OAuthRegisterRequests:       oauthRegisterRequests,
		OAuthRegisterEgressRanges:   egress,
		OAuthRegisterRangeRequests:  oauthRegisterRangeRequests,
		OAuthRegisterGlobalRequests: oauthRegisterGlobalRequests,
		OAuthRegisterWindow:         oauthRegisterWindow,
		OAuthTokenRequests:          oauthTokenRequests,
		OAuthTokenWindow:            oauthTokenWindow,
		OAuthFailedGrantLimit:       oauthFailedGrantLimit,
		OAuthFailedGrantWindow:      oauthFailedGrantWindow,
		MCPTokenRequests:            mcpTokenRequests,
		MCPTokenWindow:              mcpTokenWindow,
		MCPUserRequests:             mcpUserRequests,
		MCPUserWindow:               mcpUserWindow,
		MCPConcurrentPerUser:        mcpConcurrentPerUser,
		OAuthLiveGrantLimit:         oauthLiveGrantLimit,
		MCPBodyLimitBytes:           mcpBodyLimitBytes,
		MaxRateKeys:                 agentRateMaxKeys,
	}, nil
}

func (c Config) ValidateAgentAccess() error {
	if err := c.validateCIMD(); err != nil {
		return err
	}
	if !c.AgentAccess.Enabled {
		return nil
	}
	a := c.AgentAccess
	if c.PublicOrigin == "" || a.OAuthRegisterRequests != oauthRegisterRequests || a.OAuthRegisterWindow != oauthRegisterWindow ||
		a.OAuthTokenRequests != oauthTokenRequests || a.OAuthTokenWindow != oauthTokenWindow ||
		a.OAuthFailedGrantLimit != oauthFailedGrantLimit || a.OAuthFailedGrantWindow != oauthFailedGrantWindow ||
		a.MCPTokenRequests != mcpTokenRequests || a.MCPTokenWindow != mcpTokenWindow ||
		a.MCPUserRequests != mcpUserRequests || a.MCPUserWindow != mcpUserWindow ||
		a.MCPConcurrentPerUser != mcpConcurrentPerUser || a.OAuthLiveGrantLimit != oauthLiveGrantLimit ||
		a.MCPBodyLimitBytes != mcpBodyLimitBytes || a.MaxRateKeys != agentRateMaxKeys ||
		a.OAuthRegisterRangeRequests != oauthRegisterRangeRequests ||
		a.OAuthRegisterGlobalRequests != oauthRegisterGlobalRequests {
		return errors.New("config: enabled MCP agent access configuration is incomplete")
	}
	return validateRegisterEgressRanges(a.OAuthRegisterEgressRanges)
}

func loadRegisterEgressRanges(raw string) ([]netip.Prefix, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	fields := strings.Split(raw, ",")
	if len(fields) > maxRegisterEgressRanges {
		return nil, fmt.Errorf("config: %s: at most %d entries", registerEgressVar, maxRegisterEgressRanges)
	}
	prefixes := make([]netip.Prefix, 0, len(fields))
	for i, field := range fields {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(field))
		if err != nil {
			return nil, fmt.Errorf("config: %s: entry %d is not a CIDR", registerEgressVar, i+1)
		}
		prefixes = append(prefixes, prefix)
	}
	if err := validateRegisterEgressRanges(prefixes); err != nil {
		return nil, err
	}
	return prefixes, nil
}

func validateRegisterEgressRanges(prefixes []netip.Prefix) error {
	if len(prefixes) > maxRegisterEgressRanges {
		return fmt.Errorf("config: %s: at most %d entries", registerEgressVar, maxRegisterEgressRanges)
	}
	for i, prefix := range prefixes {
		if !prefix.IsValid() {
			return fmt.Errorf("config: %s: entry %d is not a CIDR", registerEgressVar, i+1)
		}
		if prefix != prefix.Masked() {
			return fmt.Errorf("config: %s: %s has host bits set; write %s", registerEgressVar, prefix, prefix.Masked())
		}
		if prefix.Addr().Is4In6() {
			return fmt.Errorf("config: %s: %s is IPv4-mapped; write the plain IPv4 CIDR", registerEgressVar, prefix)
		}
		minBits := minRegisterEgressPrefixBitsIPv4
		if prefix.Addr().Is6() {
			minBits = minRegisterEgressPrefixBitsIPv6
		}
		if prefix.Bits() < minBits {
			return fmt.Errorf("config: %s: %s is broader than /%d", registerEgressVar, prefix, minBits)
		}
		for _, earlier := range prefixes[:i] {
			if prefix.Overlaps(earlier) {
				return fmt.Errorf("config: %s: %s overlaps %s", registerEgressVar, prefix, earlier)
			}
		}
	}
	return nil
}
