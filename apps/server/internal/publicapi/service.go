package publicapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/dannyota/aboutme/apps/server/internal/directrender"
	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

const (
	jsonFormatVersion  = 1
	photoFormatVersion = 1
)

// ServiceDependencies are the public HTTP handlers' shared dependencies.
// Composition constructs one Reader and one Coordinator before calling this
// constructor, so every representation takes the same admission path.
type ServiceDependencies struct {
	Reader         *publicresume.Reader
	DiscoveryStore store.PublicDiscoveryQueries
	Coordinator    *publicstate.Coordinator
	Cache          *publiccache.Cache
	Renderer       *directrender.Client
	PublicOrigin   publicresume.PublicOrigin
	AppDigest      string
	RendererDigest string
	Live           http.Handler
}

var _ store.PublicReadQueries = (*store.Queries)(nil)

// Service is the public-route boundary consumed by api.New.
type Service struct {
	json     http.Handler
	photo    http.Handler
	html     http.Handler
	markdown http.Handler
	sitemap  http.Handler
	robots   http.Handler
	llms     http.Handler
	live     http.Handler
}

// NewService creates the public-route dispatcher from its dependencies.
func NewService(dependencies ServiceDependencies) (*Service, error) {
	if dependencies.Reader == nil || dependencies.DiscoveryStore == nil || dependencies.Cache == nil || dependencies.Renderer == nil || dependencies.PublicOrigin.String() == "" || dependencies.AppDigest == "" || dependencies.RendererDigest == "" {
		return nil, ErrUnavailableDependencies
	}
	html, err := NewHTMLHandler(HTMLDependencies{Reader: dependencies.Reader, Cache: dependencies.Cache, Renderer: dependencies.Renderer, PublicOrigin: dependencies.PublicOrigin, AppDigest: dependencies.AppDigest, RendererDigest: dependencies.RendererDigest})
	if err != nil {
		return nil, err
	}
	markdown, err := NewMarkdownHandler(MarkdownDependencies{Reader: dependencies.Reader, Cache: dependencies.Cache, AppDigest: dependencies.AppDigest})
	if err != nil {
		return nil, err
	}
	if dependencies.Coordinator == nil {
		return nil, ErrUnavailableDependencies
	}
	discovery := DiscoveryDependencies{Store: dependencies.DiscoveryStore, Coordinator: dependencies.Coordinator, Cache: dependencies.Cache, PublicOrigin: dependencies.PublicOrigin, AppDigest: dependencies.AppDigest}
	sitemap, err := NewSitemapHandler(discovery)
	if err != nil {
		return nil, err
	}
	robots, err := NewRobotsHandler(discovery)
	if err != nil {
		return nil, err
	}
	llms, err := NewLLMSHandler(discovery)
	if err != nil {
		return nil, err
	}
	service := &Service{html: html, markdown: markdown, sitemap: sitemap, robots: robots, llms: llms, live: dependencies.Live}
	service.json = service.newJSONHandler(dependencies.Reader, dependencies.Cache, dependencies.AppDigest)
	service.photo = service.newPhotoHandler(dependencies.Reader, dependencies.Cache, dependencies.AppDigest)
	return service, nil
}

func (s *Service) newJSONHandler(reader *publicresume.Reader, cache *publiccache.Cache, appDigest string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if !publicJSONGetOrHead(w, request) {
			return
		}
		snapshot, lease, err := reader.ReadResume(request.Context(), publicSlug(request.URL.EscapedPath()), publicstate.RepresentationJSON)
		if err != nil {
			if lease != nil {
				lease.Release()
			}
			if errors.Is(err, publicresume.ErrNotFound) {
				servePublicJSONError(w, request, http.StatusNotFound)
				return
			}
			servePublicJSONError(w, request, http.StatusServiceUnavailable)
			return
		}
		defer lease.Release()
		key := publiccache.Key{RouteClass: "resume", Representation: publicstate.RepresentationJSON, Variant: "default", ResumeID: snapshot.ResumeID, Generation: snapshot.Revision, FormatVersion: jsonFormatVersion, AppDigest: appDigest}
		if cached, ok := cache.Get(key); ok {
			SelectedResponse{Status: cached.Status, Header: cached.Header, Body: cached.Body}.ServeHTTP(w, request)
			return
		}
		response, err := NewPublicJSON(snapshot.Public)
		if err != nil {
			servePublicJSONError(w, request, http.StatusServiceUnavailable)
			return
		}
		cache.Put(key, publiccache.Value{Status: response.Status, Header: response.Header, Body: response.Body})
		response.ServeHTTP(w, request)
	})
}

func (s *Service) newPhotoHandler(reader *publicresume.Reader, cache *publiccache.Cache, appDigest string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if !publicJSONGetOrHead(w, request) {
			return
		}
		snapshot, lease, err := reader.ReadResume(request.Context(), publicSlug(request.URL.EscapedPath()), publicstate.RepresentationPhoto)
		if err != nil {
			if lease != nil {
				lease.Release()
			}
			if errors.Is(err, publicresume.ErrNotFound) {
				servePublicJSONError(w, request, http.StatusNotFound)
				return
			}
			servePublicJSONError(w, request, http.StatusServiceUnavailable)
			return
		}
		defer lease.Release()
		key := publiccache.Key{RouteClass: "resume", Representation: publicstate.RepresentationPhoto, Variant: "default", ResumeID: snapshot.ResumeID, Generation: snapshot.Revision, FormatVersion: photoFormatVersion, AppDigest: appDigest}
		if cached, ok := cache.Get(key); ok {
			SelectedResponse{Status: cached.Status, Header: cached.Header, Body: cached.Body}.ServeHTTP(w, request)
			return
		}
		//nolint:contextcheck // The lease context is derived from request.Context and adds revocation cancellation.
		body, contentType, err := reader.ReadPhoto(lease.Context(), snapshot)
		if err != nil {
			servePublicJSONError(w, request, http.StatusServiceUnavailable)
			return
		}
		response, err := NewPublicPhoto(body, contentType)
		if err != nil {
			servePublicJSONError(w, request, http.StatusServiceUnavailable)
			return
		}
		cache.Put(key, publiccache.Value{Status: response.Status, Header: response.Header, Body: response.Body})
		response.ServeHTTP(w, request)
	})
}

func publicJSONGetOrHead(w http.ResponseWriter, request *http.Request) bool {
	if request.Method == http.MethodGet || request.Method == http.MethodHead {
		return true
	}
	w.Header().Set("Allow", "GET, HEAD")
	servePublicJSONError(w, request, http.StatusMethodNotAllowed)
	return false
}

func servePublicJSONError(w http.ResponseWriter, request *http.Request, status int) {
	code, message := "temporarily_unavailable", "service temporarily unavailable"
	switch status {
	case http.StatusNotFound:
		code, message = "public_not_found", "public resume not found"
	case http.StatusMethodNotAllowed:
		code, message = "method_not_allowed", "method is not allowed"
	case http.StatusBadRequest:
		code, message = "request_invalid", "request is invalid"
	}
	body := []byte(`{"error":{"code":"` + code + `","message":"` + message + `"}}` + "\n")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	if status == http.StatusServiceUnavailable {
		w.Header().Set("Retry-After", "1")
	}
	w.WriteHeader(status)
	if request.Method != http.MethodHead {
		if _, err := w.Write(body); err != nil {
			return
		}
	}
}
