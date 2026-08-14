package publiccache

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
)

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
