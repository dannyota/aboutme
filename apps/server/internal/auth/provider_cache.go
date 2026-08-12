package auth

import (
	"context"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
)

// oidcProviderCache lazily discovers and caches one OIDC provider.
type oidcProviderCache struct {
	mu       sync.Mutex
	provider *oidc.Provider // nil until first successful discover
}

// discover caches the first successful result and retries after failures. It
// never holds c.mu across network I/O. Concurrent first calls may issue
// redundant requests, but all return the first provider stored in the cache.
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
