package publiccache

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
)

const maxCacheBodyBytes = 32 * 1024 * 1024

func TestCacheCopiesValuesAndExpires(t *testing.T) {
	now := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
	cache, err := New(2, time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	key := Key{RouteClass: "resume", Representation: publicstate.RepresentationJSON, ResumeID: uuid.New(), Generation: 3, FormatVersion: 1, AppDigest: "sha256:test"}
	value := Value{Status: http.StatusOK, Header: http.Header{"X-Test": {"one"}}, Body: []byte("body")}
	cache.Put(key, value)
	value.Header.Set("X-Test", "changed")
	value.Body[0] = 'B'
	got, ok := cache.Get(key)
	if !ok || got.Status != http.StatusOK || got.Header.Get("X-Test") != "one" || string(got.Body) != "body" {
		t.Fatalf("Get() = %#v, %v", got, ok)
	}
	got.Body[0] = 'B'
	got.Header.Set("X-Test", "changed")
	got, _ = cache.Get(key)
	if got.Header.Get("X-Test") != "one" || string(got.Body) != "body" {
		t.Fatalf("cache leaked mutation: %#v", got)
	}
	now = now.Add(time.Minute)
	if _, ok := cache.Get(key); ok {
		t.Fatal("expired entry remained visible")
	}
}

func TestCacheEvictsPurgesAndSeparatesEveryKeyDimension(t *testing.T) {
	now := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
	cache, err := New(2, time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	base := Key{RouteClass: "resume", Representation: publicstate.RepresentationJSON, Variant: "default", ResumeID: uuid.New(), Generation: 3, FormatVersion: 1, AppDigest: "sha256:app", RendererDigest: "sha256:renderer"}
	put := func(key Key, body string) {
		cache.Put(key, Value{Status: http.StatusOK, Header: http.Header{}, Body: []byte(body)})
	}
	put(base, "base")
	keys := []Key{
		func() Key { key := base; key.RouteClass = "photo"; return key }(),
		func() Key { key := base; key.Representation = publicstate.RepresentationPhoto; return key }(),
		func() Key { key := base; key.Variant = "noindex"; return key }(),
		func() Key { key := base; key.ResumeID = uuid.New(); return key }(),
		func() Key { key := base; key.Generation++; return key }(),
		func() Key { key := base; key.FormatVersion++; return key }(),
		func() Key { key := base; key.AppDigest = "sha256:other"; return key }(),
		func() Key { key := base; key.RendererDigest = "sha256:other"; return key }(),
	}
	for _, key := range keys {
		if _, ok := cache.Get(key); ok {
			t.Fatalf("different cache dimension matched %#v", key)
		}
	}
	put(keys[0], "second")
	put(keys[1], "third")
	if _, ok := cache.Get(base); ok {
		t.Fatal("oldest entry was not evicted")
	}
	if _, ok := cache.Get(keys[0]); !ok {
		t.Fatal("second entry was evicted")
	}
	now = now.Add(time.Minute)
	cache.Purge()
	if _, ok := cache.Get(keys[0]); ok {
		t.Fatal("Purge retained expired entry")
	}
}

func TestCacheRejectsBodyLargerThanAggregateLimit(t *testing.T) {
	cache, err := New(2, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	key := testKey()
	cache.Put(key, Value{Status: http.StatusOK, Body: make([]byte, maxCacheBodyBytes+1)})
	if _, ok := cache.Get(key); ok {
		t.Fatal("over-budget body remained cached")
	}
}

func TestCacheAcceptsExactAggregateLimit(t *testing.T) {
	cache, err := New(2, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	key := testKey()
	body := make([]byte, maxCacheBodyBytes)
	body[0], body[len(body)-1] = 'a', 'z'
	cache.Put(key, Value{Status: http.StatusOK, Body: body})
	got, ok := cache.Get(key)
	if !ok || len(got.Body) != maxCacheBodyBytes || got.Body[0] != 'a' || got.Body[len(got.Body)-1] != 'z' {
		t.Fatalf("Get() = %d bytes, %v", len(got.Body), ok)
	}
}

func TestCacheReplacementDoesNotRetainReplacedBodyBytes(t *testing.T) {
	cache, err := New(2, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	first := testKey()
	second := testKey()
	cache.Put(first, Value{Status: http.StatusOK, Body: make([]byte, 20*1024*1024)})
	cache.Put(first, Value{Status: http.StatusOK, Body: make([]byte, 20*1024*1024)})
	cache.Put(second, Value{Status: http.StatusOK, Body: make([]byte, 12*1024*1024)})
	if _, ok := cache.Get(first); !ok {
		t.Fatal("replacement entry was evicted")
	}
	if _, ok := cache.Get(second); !ok {
		t.Fatal("entry that fits after replacement was evicted")
	}
}

func TestCacheExpiryAndPurgeReleaseBodyBudget(t *testing.T) {
	now := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
	cache, err := New(2, time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	expired := testKey()
	cache.Put(expired, Value{Status: http.StatusOK, Body: make([]byte, maxCacheBodyBytes)})
	now = now.Add(time.Minute)
	if _, ok := cache.Get(expired); ok {
		t.Fatal("expired entry remained visible")
	}
	reused := testKey()
	cache.Put(reused, Value{Status: http.StatusOK, Body: make([]byte, maxCacheBodyBytes)})
	if _, ok := cache.Get(reused); !ok {
		t.Fatal("expired entry did not release body budget")
	}

	now = now.Add(time.Minute)
	cache.Purge()
	purged := testKey()
	cache.Put(purged, Value{Status: http.StatusOK, Body: make([]byte, maxCacheBodyBytes)})
	if _, ok := cache.Get(purged); !ok {
		t.Fatal("purged entry did not release body budget")
	}
}

func TestCacheEvictsOldestEntryToFitAggregateBodyLimit(t *testing.T) {
	cache, err := New(3, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	first, second, third := testKey(), testKey(), testKey()
	cache.Put(first, Value{Status: http.StatusOK, Body: make([]byte, 16*1024*1024)})
	cache.Put(second, Value{Status: http.StatusOK, Body: make([]byte, 16*1024*1024)})
	cache.Put(third, Value{Status: http.StatusOK, Body: []byte("x")})
	if _, ok := cache.Get(first); ok {
		t.Fatal("oldest entry remained after aggregate-body eviction")
	}
	if _, ok := cache.Get(second); !ok {
		t.Fatal("newer entry was evicted")
	}
	if _, ok := cache.Get(third); !ok {
		t.Fatal("new entry was not cached")
	}
}

func TestCacheConcurrentAccessPreservesBodyBudget(t *testing.T) {
	cache, err := New(16, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]Key, 8)
	for i := range keys {
		keys[i] = testKey()
	}
	var group sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			for iteration := 0; iteration < 100; iteration++ {
				key := keys[(worker+iteration)%len(keys)]
				cache.Put(key, Value{Status: http.StatusOK, Body: make([]byte, 1024+(worker+iteration)%1024)})
				if value, ok := cache.Get(key); ok && len(value.Body) == 0 {
					t.Error("cached non-empty body became empty")
				}
				if iteration%17 == 0 {
					cache.Purge()
				}
			}
		}(worker)
	}
	group.Wait()
}

func testKey() Key {
	return Key{RouteClass: "resume", Representation: publicstate.RepresentationJSON, ResumeID: uuid.New(), Generation: 3, FormatVersion: 1, AppDigest: "sha256:test"}
}
