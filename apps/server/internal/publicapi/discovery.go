package publicapi

import (
	"context"
	"errors"
	"net/http"

	"danny.vn/aboutme/apps/server/internal/publiccache"
	"danny.vn/aboutme/apps/server/internal/publicformat"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/store"
)

// DiscoveryRouteClass is the cache route class of robots.txt.
const DiscoveryRouteClass publiccache.RouteClass = "discovery"

// DiscoveryDependencies contains dependencies for discovery documents.
type DiscoveryDependencies struct {
	// Store reads the discovery generation and the eligible slugs.
	Store store.PublicDiscoveryQueries
	// Coordinator admits sitemap and llms.txt requests under a discovery lease.
	Coordinator *publicstate.Coordinator
	// Cache holds robots.txt only; sitemap and llms.txt are never cached.
	Cache *publiccache.Cache
	// PublicOrigin is the canonical origin every listed URL resolves against.
	PublicOrigin publicresume.PublicOrigin
	// AppDigest identifies the application build in the robots.txt cache key.
	AppDigest string
}

var _ store.PublicDiscoveryQueries = (*store.Queries)(nil)

type discoveryEncoder func(publicresume.PublicOrigin, []string) ([]byte, error)

// NewSitemapHandler creates the sitemap handler.
func NewSitemapHandler(dependencies DiscoveryDependencies) (http.Handler, error) {
	return newDiscoveryHandler(dependencies, publicstate.RepresentationSitemap, "application/xml; charset=utf-8", publicformat.Sitemap)
}

// NewLLMSHandler creates the llms.txt handler.
func NewLLMSHandler(dependencies DiscoveryDependencies) (http.Handler, error) {
	return newDiscoveryHandler(dependencies, publicstate.RepresentationLLMS, "text/plain; charset=utf-8", publicformat.LLMS)
}

// NewRobotsHandler creates the robots.txt handler.
func NewRobotsHandler(dependencies DiscoveryDependencies) (http.Handler, error) {
	if dependencies.Cache == nil || dependencies.PublicOrigin.String() == "" || dependencies.AppDigest == "" {
		return nil, ErrUnavailableDependencies
	}
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if !publicGetOrHead(w, request) {
			return
		}
		key := publiccache.Key{RouteClass: DiscoveryRouteClass, Representation: publicstate.RepresentationRobots, Variant: "default", FormatVersion: publicformat.RobotsFormatVersion, AppDigest: dependencies.AppDigest}
		if cached, ok := dependencies.Cache.Get(key); ok {
			SelectedResponse{Status: cached.Status, Header: cached.Header, Body: cached.Body}.ServeHTTP(w, request)
			return
		}
		response, err := NewSelectedResponse(http.StatusOK, "text/plain; charset=utf-8", "no-cache, must-revalidate", publicformat.Robots(dependencies.PublicOrigin), nil)
		if err != nil {
			serveTextError(w, request, http.StatusServiceUnavailable)
			return
		}
		dependencies.Cache.Put(key, publiccache.Value{Status: response.Status, Header: response.Header, Body: response.Body})
		response.ServeHTTP(w, request)
	}), nil
}

func newDiscoveryHandler(dependencies DiscoveryDependencies, representation publicstate.Representation, contentType string, encoder discoveryEncoder) (http.Handler, error) {
	if dependencies.Store == nil || dependencies.Coordinator == nil || dependencies.PublicOrigin.String() == "" {
		return nil, ErrUnavailableDependencies
	}
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if !publicGetOrHead(w, request) {
			return
		}
		slugs, lease, err := admitDiscovery(request.Context(), dependencies.Store, dependencies.Coordinator, representation)
		if err != nil {
			serveTextError(w, request, http.StatusServiceUnavailable)
			return
		}
		defer lease.Release()
		// Encode the slugs this request read and never cache the bytes.
		// Operator account deletion removes slugs without a new discovery
		// generation, so a generation-keyed entry stored after its cache
		// eviction would keep listing them.
		body, err := encoder(dependencies.PublicOrigin, slugs)
		if err != nil {
			serveTextError(w, request, http.StatusServiceUnavailable)
			return
		}
		response, err := NewSelectedResponse(http.StatusOK, contentType, "no-cache, must-revalidate", body, nil)
		if err != nil {
			serveTextError(w, request, http.StatusServiceUnavailable)
			return
		}
		serveLeasedSelected(w, request, lease, response, serveTextError)
	}), nil
}

func admitDiscovery(ctx context.Context, queries store.PublicDiscoveryQueries, coordinator *publicstate.Coordinator, representation publicstate.Representation) ([]string, *publicstate.Lease, error) {
	for attempt := 0; attempt < 2; attempt++ {
		snapshot, err := queries.GetPublicDiscoverySnapshot(ctx)
		if err != nil || snapshot.DiscoveryGeneration <= 0 {
			return nil, nil, errors.New("public discovery unavailable")
		}
		lease, err := coordinator.AcquireDiscovery(ctx, snapshot.DiscoveryGeneration, representation)
		if err == nil {
			return snapshot.Slugs, lease, nil
		}
		var mismatch *publicstate.GenerationMismatchError
		if !errors.As(err, &mismatch) || attempt != 0 {
			return nil, nil, errors.New("public discovery unavailable")
		}
	}
	return nil, nil, errors.New("public discovery unavailable")
}
