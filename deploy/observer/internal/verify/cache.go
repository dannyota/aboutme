package verify

import (
	"context"
	"time"
)

// CacheTTL is how long a cached result stands before the digest is checked
// again. Digests are immutable, so the cache only saves API calls.
const CacheTTL = 24 * time.Hour

// Cache stores Evidence by digest (verified/<digest>.json in the bucket).
// Get returns found=false, nil for a missing entry. An Unchecked result is
// never stored.
type Cache interface {
	Get(ctx context.Context, digest string) (ev Evidence, found bool, err error)
	Put(ctx context.Context, ev Evidence) error
}

// MemoryCache is a Cache in process memory, for tests and for runs that
// share a warm process.
type MemoryCache struct{ m map[string]Evidence }

// NewMemoryCache returns an empty MemoryCache.
func NewMemoryCache() *MemoryCache { return &MemoryCache{m: map[string]Evidence{}} }

// Get implements Cache.
func (c *MemoryCache) Get(_ context.Context, digest string) (Evidence, bool, error) {
	ev, ok := c.m[digest]
	return ev, ok, nil
}

// Put implements Cache.
func (c *MemoryCache) Put(_ context.Context, ev Evidence) error {
	c.m[ev.Digest] = ev
	return nil
}
