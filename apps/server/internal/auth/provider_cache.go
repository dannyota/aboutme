package auth

import (
	"context"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
)

type oidcProviderCache struct {
	mu       sync.Mutex
	provider *oidc.Provider
}

func (c *oidcProviderCache) discover(ctx context.Context, issuer string) (*oidc.Provider, error) {
	c.mu.Lock()
	p := c.provider
	c.mu.Unlock()
	if p != nil {
		return p, nil
	}

	discovered, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.provider == nil {
		c.provider = discovered
	}
	return c.provider, nil
}
