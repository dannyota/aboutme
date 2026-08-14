package publicapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/publicformat"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

type DiscoveryDependencies struct {
	Store        store.PublicDiscoveryQueries
	Coordinator  *publicstate.Coordinator
	Cache        *publiccache.Cache
	PublicOrigin publicresume.PublicOrigin
	AppDigest    string
}

var _ store.PublicDiscoveryQueries = (*store.Queries)(nil)

type discoveryEncoder func(publicresume.PublicOrigin, []string) ([]byte, error)

func NewSitemapHandler(dependencies DiscoveryDependencies) (http.Handler, error) {
	return newDiscoveryHandler(dependencies, publicstate.RepresentationSitemap, publicformat.SitemapFormatVersion, "application/xml; charset=utf-8", publicformat.Sitemap)
}

func NewLLMSHandler(dependencies DiscoveryDependencies) (http.Handler, error) {
	return newDiscoveryHandler(dependencies, publicstate.RepresentationLLMS, publicformat.LLMSFormatVersion, "text/plain; charset=utf-8", publicformat.LLMS)
}

func NewRobotsHandler(dependencies DiscoveryDependencies) (http.Handler, error) {
	if dependencies.Cache == nil || dependencies.PublicOrigin.String() == "" || dependencies.AppDigest == "" {
		return nil, ErrUnavailableDependencies
	}
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if !publicGetOrHead(w, request) {
			return
		}
		key := publiccache.Key{RouteClass: "discovery", Representation: publicstate.RepresentationRobots, Variant: "default", FormatVersion: publicformat.RobotsFormatVersion, AppDigest: dependencies.AppDigest}
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

func newDiscoveryHandler(dependencies DiscoveryDependencies, representation publicstate.Representation, version int, contentType string, encoder discoveryEncoder) (http.Handler, error) {
	if dependencies.Store == nil || dependencies.Coordinator == nil || dependencies.Cache == nil || dependencies.PublicOrigin.String() == "" || dependencies.AppDigest == "" {
		return nil, ErrUnavailableDependencies
	}
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if !publicGetOrHead(w, request) {
			return
		}
		generation, slugs, lease, err := admitDiscovery(request.Context(), dependencies.Store, dependencies.Coordinator, representation)
		if err != nil {
			serveTextError(w, request, http.StatusServiceUnavailable)
			return
		}
		defer lease.Release()
		key := publiccache.Key{RouteClass: "discovery", Representation: representation, Variant: "default", Generation: generation, FormatVersion: version, AppDigest: dependencies.AppDigest}
		if cached, ok := dependencies.Cache.Get(key); ok {
			SelectedResponse{Status: cached.Status, Header: cached.Header, Body: cached.Body}.ServeHTTP(w, request)
			return
		}
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
		dependencies.Cache.Put(key, publiccache.Value{Status: response.Status, Header: response.Header, Body: response.Body})
		response.ServeHTTP(w, request)
	}), nil
}

func admitDiscovery(ctx context.Context, queries store.PublicDiscoveryQueries, coordinator *publicstate.Coordinator, representation publicstate.Representation) (int64, []string, *publicstate.Lease, error) {
	for attempt := 0; attempt < 2; attempt++ {
		snapshot, err := queries.GetPublicDiscoverySnapshot(ctx)
		if err != nil || snapshot.DiscoveryGeneration <= 0 {
			return 0, nil, nil, errors.New("public discovery unavailable")
		}
		lease, err := coordinator.AcquireDiscovery(ctx, snapshot.DiscoveryGeneration, representation)
		if err == nil {
			return snapshot.DiscoveryGeneration, snapshot.Slugs, lease, nil
		}
		var mismatch *publicstate.GenerationMismatchError
		if !errors.As(err, &mismatch) || attempt != 0 {
			return 0, nil, nil, errors.New("public discovery unavailable")
		}
	}
	return 0, nil, nil, errors.New("public discovery unavailable")
}
