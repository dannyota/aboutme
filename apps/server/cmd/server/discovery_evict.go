package main

import (
	"github.com/dannyota/aboutme/apps/server/internal/publicapi"
	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/realtime"
)

// evictDiscoveryOnDelete drops the cached sitemap and llms.txt when a resume
// row is deleted. The command that deletes a reported account runs in another
// process and leaves the durable discovery generation alone, so the generation
// in the discovery cache key does not change; the database's committed-change
// notification is how this process learns of the deletion. next may be nil.
func evictDiscoveryOnDelete(cache *publiccache.Cache, next func(realtime.Change)) func(realtime.Change) {
	return func(change realtime.Change) {
		if change.Deleted {
			cache.RemoveRouteClass(publicapi.DiscoveryRouteClass)
		}
		if next != nil {
			next(change)
		}
	}
}
