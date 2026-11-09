package app

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/publicapi"
	"danny.vn/aboutme/apps/server/internal/publiccache"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/realtime"
)

func TestEvictDiscoveryOnDeleteDropsOnlyDiscoveryEntries(t *testing.T) {
	t.Parallel()
	cache, err := publiccache.New(8, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	sitemap := publiccache.Key{RouteClass: publicapi.DiscoveryRouteClass, Representation: publicstate.RepresentationSitemap, Variant: "default", Generation: 1}
	page := publiccache.Key{RouteClass: "resume", Representation: publicstate.RepresentationMarkdown, Variant: "default", Generation: 1}
	put := func() {
		cache.Put(sitemap, publiccache.Value{Status: 200, Body: []byte("sitemap")})
		cache.Put(page, publiccache.Value{Status: 200, Body: []byte("page")})
	}
	var forwarded []realtime.Change
	observe := EvictDiscoveryOnDelete(cache, func(c realtime.Change) { forwarded = append(forwarded, c) })

	put()
	observe(realtime.Change{ResumeID: uuid.New(), Revision: 2})
	if _, ok := cache.Get(sitemap); !ok {
		t.Fatal("an update evicted the sitemap")
	}
	observe(realtime.Change{ResumeID: uuid.New(), Revision: 3, Deleted: true})
	if _, ok := cache.Get(sitemap); ok {
		t.Fatal("a delete left the cached sitemap")
	}
	if _, ok := cache.Get(page); !ok {
		t.Fatal("a delete evicted a non-discovery entry")
	}
	if len(forwarded) != 2 {
		t.Fatalf("forwarded %d changes, want 2", len(forwarded))
	}
	EvictDiscoveryOnDelete(cache, nil)(realtime.Change{Deleted: true})
}
