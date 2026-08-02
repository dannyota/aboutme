package auth

import (
	"context"
	"sync"

	"github.com/coreos/go-oidc/v3/oidc"
)

// oidcProviderCache lazily discovers and caches a single *oidc.Provider,
// shared by google.go and linkedin.go's own googleProviderConfig/
// linkedinProviderConfig so the discovery/caching pattern -- and its one
// concurrency hazard -- exists in exactly one place rather than being
// hand-copied into every new provider file. See discover's own comment
// for the hazard this type exists to avoid.
type oidcProviderCache struct {
	mu       sync.Mutex
	provider *oidc.Provider // nil until first successful discover
}

// discover returns the cached provider, discovering (and caching) it via
// issuer on first use. Discovery failure is not cached, so a transient
// network blip does not permanently break login until process restart --
// the next call simply retries.
//
// The oidc.NewProvider network call deliberately runs OUTSIDE c.mu: an
// earlier version of this pattern held the mutex for the call's entire
// duration, which meant every OTHER concurrent /start or /callback
// request across ANY purpose -- not just ones needing this same
// provider -- blocked behind a single slow or hung discovery attempt
// (Google/LinkedIn's discovery endpoint is a real, uncontrolled external
// dependency; a slow DNS lookup or a stalled TLS handshake would have
// stalled the whole process's login traffic, not just the one request
// that triggered discovery). This is a check-then-dial-then-recheck
// pattern instead: the mutex only ever guards the map read/write, never
// the I/O. The tradeoff is that two callers racing to discover
// concurrently before either has cached a result each independently
// dial (redundant work, but bounded -- it can only happen once per
// provider, ever, since every later caller sees the already-cached
// result) rather than one blocking on the other; the recheck under the
// lock after dialing means only the first writer's result is kept, and
// every caller -- including the "loser" of the race -- returns the same,
// single cached *oidc.Provider from then on.
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
