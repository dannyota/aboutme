package verify

import (
	"context"
	"time"
)

const CacheTTL = 24 * time.Hour

type Cache interface {
	Get(ctx context.Context, digest string) (ev Evidence, found bool, err error)
	Put(ctx context.Context, ev Evidence) error
}

type MemoryCache struct{ m map[string]Evidence }

func NewMemoryCache() *MemoryCache { return &MemoryCache{m: map[string]Evidence{}} }

func (c *MemoryCache) Get(_ context.Context, digest string) (Evidence, bool, error) {
	ev, ok := c.m[digest]
	return ev, ok, nil
}

func (c *MemoryCache) Put(_ context.Context, ev Evidence) error {
	c.m[ev.Digest] = ev
	return nil
}
