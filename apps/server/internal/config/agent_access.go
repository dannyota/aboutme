package config

import (
	"errors"
	"strings"
	"time"
)

// AgentAccessConfig holds the frozen OAuth and MCP admission budgets. It is
// populated as one complete unit even when disabled so enabling the feature
// cannot expose a partially initialized limiter or body boundary.
type AgentAccessConfig struct {
	Enabled                bool
	OAuthRegisterRequests  int
	OAuthRegisterWindow    time.Duration
	OAuthTokenRequests     int
	OAuthTokenWindow       time.Duration
	OAuthFailedGrantLimit  int
	OAuthFailedGrantWindow time.Duration
	MCPTokenRequests       int
	MCPTokenWindow         time.Duration
	MCPUserRequests        int
	MCPUserWindow          time.Duration
	MCPConcurrentPerUser   int
	OAuthLiveGrantLimit    int
	MCPBodyLimitBytes      int64
	MaxRateKeys            int
}

const (
	oauthRegisterRequests  = 5
	oauthRegisterWindow    = time.Hour
	oauthTokenRequests     = 30
	oauthTokenWindow       = time.Minute
	oauthFailedGrantLimit  = 10
	oauthFailedGrantWindow = 15 * time.Minute
	mcpTokenRequests       = 120
	mcpTokenWindow         = time.Minute
	mcpUserRequests        = 240
	mcpUserWindow          = time.Minute
	mcpConcurrentPerUser   = 4
	oauthLiveGrantLimit    = 10
	mcpBodyLimitBytes      = 4_194_304
	agentRateMaxKeys       = 10_000
)

func loadAgentAccessConfig(rawEnabled string) (AgentAccessConfig, error) {
	var enabled bool
	switch strings.TrimSpace(rawEnabled) {
	case "", "false":
	case "true":
		enabled = true
	default:
		return AgentAccessConfig{}, errors.New("config: MCP_ENABLED must be true or false")
	}
	return AgentAccessConfig{
		Enabled:                enabled,
		OAuthRegisterRequests:  oauthRegisterRequests,
		OAuthRegisterWindow:    oauthRegisterWindow,
		OAuthTokenRequests:     oauthTokenRequests,
		OAuthTokenWindow:       oauthTokenWindow,
		OAuthFailedGrantLimit:  oauthFailedGrantLimit,
		OAuthFailedGrantWindow: oauthFailedGrantWindow,
		MCPTokenRequests:       mcpTokenRequests,
		MCPTokenWindow:         mcpTokenWindow,
		MCPUserRequests:        mcpUserRequests,
		MCPUserWindow:          mcpUserWindow,
		MCPConcurrentPerUser:   mcpConcurrentPerUser,
		OAuthLiveGrantLimit:    oauthLiveGrantLimit,
		MCPBodyLimitBytes:      mcpBodyLimitBytes,
		MaxRateKeys:            agentRateMaxKeys,
	}, nil
}

// ValidateAgentAccess rejects a manually assembled, partially enabled
// configuration. Load always supplies the complete frozen values, but the
// composition root also calls this method so tests and future constructors
// cannot bypass startup validation with a Config literal.
func (c Config) ValidateAgentAccess() error {
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
		a.MCPBodyLimitBytes != mcpBodyLimitBytes || a.MaxRateKeys != agentRateMaxKeys {
		return errors.New("config: enabled MCP agent access configuration is incomplete")
	}
	return nil
}
