// Package publiccache stores bounded public responses by complete render keys.
package publiccache

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
)

const maxBodyBytes = 32 * 1024 * 1024

// RouteClass separates cache routes with distinct response contracts.
type RouteClass string

// Variant separates response variants for one representation.
type Variant string

// Key identifies one cacheable public response.
type Key struct {
	RouteClass     RouteClass
	Representation publicstate.Representation
	Variant        Variant
	ResumeID       uuid.UUID
	Generation     int64
	FormatVersion  int
	AppDigest      string
	RendererDigest string
}

// Value holds a cached response's immutable response data.
type Value struct {
	Status int
	Header http.Header
	Body   []byte
}
type cacheEntry struct {
	Value     Value
	ExpiresAt time.Time
	Sequence  uint64
}

// Cache is a bounded in-memory cache for public responses.
type Cache struct {
	mu         sync.RWMutex
	entries    map[Key]cacheEntry
	maxEntries int
	ttl        time.Duration
	now        func() time.Time
	sequence   uint64
	bodyBytes  int
}

// New creates a cache with a bounded entry count and lifetime.
func New(maxEntries int, ttl time.Duration, now func() time.Time) (*Cache, error) {
	if maxEntries <= 0 || ttl <= 0 || ttl > time.Minute || now == nil {
		return nil, errors.New("invalid public cache configuration")
	}
	return &Cache{entries: make(map[Key]cacheEntry), maxEntries: maxEntries, ttl: ttl, now: now}, nil
}

// Get returns a copy of the unexpired value for key.
func (c *Cache) Get(key Key) (Value, bool) {
	now := c.now()
	c.mu.RLock()
	entry, ok := c.entries[key]
	if ok && now.Before(entry.ExpiresAt) {
		value := copyValue(entry.Value)
		c.mu.RUnlock()
		return value, true
	}
	c.mu.RUnlock()
	if ok {
		c.mu.Lock()
		if current, exists := c.entries[key]; exists && !now.Before(current.ExpiresAt) {
			c.remove(key, current)
		}
		c.mu.Unlock()
	}
	return Value{}, false
}

// Put stores a copy of value and evicts the oldest entry when full.
func (c *Cache) Put(key Key, value Value) {
	if len(value.Body) > maxBodyBytes {
		return
	}
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.purgeExpired(now)
	if current, exists := c.entries[key]; exists {
		c.remove(key, current)
	}
	for len(c.entries) >= c.maxEntries || c.bodyBytes > maxBodyBytes-len(value.Body) {
		c.evictOldest()
	}
	c.sequence++
	c.entries[key] = cacheEntry{Value: copyValue(value), ExpiresAt: now.Add(c.ttl), Sequence: c.sequence}
	c.bodyBytes += len(value.Body)
}

// Purge removes expired entries.
func (c *Cache) Purge() {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.purgeExpired(now)
}

func (c *Cache) purgeExpired(now time.Time) {
	for key, entry := range c.entries {
		if !now.Before(entry.ExpiresAt) {
			c.remove(key, entry)
		}
	}
}

func (c *Cache) evictOldest() {
	var oldest Key
	var sequence uint64
	first := true
	for key, entry := range c.entries {
		if first || entry.Sequence < sequence {
			oldest, sequence, first = key, entry.Sequence, false
		}
	}
	if !first {
		c.remove(oldest, c.entries[oldest])
	}
}

func (c *Cache) remove(key Key, entry cacheEntry) {
	delete(c.entries, key)
	c.bodyBytes -= len(entry.Value.Body)
}

func copyValue(value Value) Value {
	return Value{Status: value.Status, Header: value.Header.Clone(), Body: append([]byte{}, value.Body...)}
}
