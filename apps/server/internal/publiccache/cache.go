package publiccache

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
)

type RouteClass string
type Variant string

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

type Cache struct {
	mu         sync.RWMutex
	entries    map[Key]cacheEntry
	maxEntries int
	ttl        time.Duration
	now        func() time.Time
	sequence   uint64
}

func New(maxEntries int, ttl time.Duration, now func() time.Time) (*Cache, error) {
	if maxEntries <= 0 || ttl <= 0 || ttl > time.Minute || now == nil {
		return nil, errors.New("invalid public cache configuration")
	}
	return &Cache{entries: make(map[Key]cacheEntry), maxEntries: maxEntries, ttl: ttl, now: now}, nil
}

func (c *Cache) Get(key Key) (Value, bool) {
	now := c.now()
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok || !now.Before(entry.ExpiresAt) {
		if ok {
			c.mu.Lock()
			if current, exists := c.entries[key]; exists && !now.Before(current.ExpiresAt) {
				delete(c.entries, key)
			}
			c.mu.Unlock()
		}
		return Value{}, false
	}
	return copyValue(entry.Value), true
}

func (c *Cache) Put(key Key, value Value) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sequence++
	if _, exists := c.entries[key]; !exists && len(c.entries) >= c.maxEntries {
		c.evictOldest()
	}
	c.entries[key] = cacheEntry{Value: copyValue(value), ExpiresAt: c.now().Add(c.ttl), Sequence: c.sequence}
}

func (c *Cache) Purge() {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, entry := range c.entries {
		if !now.Before(entry.ExpiresAt) {
			delete(c.entries, key)
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
		delete(c.entries, oldest)
	}
}

func copyValue(value Value) Value {
	return Value{Status: value.Status, Header: value.Header.Clone(), Body: append([]byte{}, value.Body...)}
}
