package publicapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/api"
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
	PrintQueue     RenderQueue
	TrustedProxies api.TrustedProxies
	Clock          func() time.Time
	Live           http.Handler
	// Logger receives closed, content-free diagnostics. Nil disables them.
	Logger *slog.Logger
	// Cards turns on stored preview cards (ADR 0014). Nil keeps the og.png
	// share image, so the server can deploy before the web card renderer.
	Cards PreviewCards
	// Views records crawler and link-preview fetches of resume pages. Nil
	// records nothing.
	Views ViewObserver
	// ViewPassKey seals and opens the __Host-view-pass cookie
	// (docs/design/viewer-analytics/sign-in-to-view.md "Pass cookie").
	ViewPassKey []byte
	// GateProviders is the fixed subset of ["google","linkedin"] the gate
	// offers (design "Gate"; ADR 0016).
	GateProviders []string
	// JoinInviteTarget is "/register" or "/login": the join invite's link
	// for a sign-in-to-view resume's page (design "Join invite").
	JoinInviteTarget string
	// Showcase serves the community showcase listing (docs/design/showcase.md).
	// Nil leaves the route answering 404, so the server can deploy before it
	// is wired.
	Showcase ShowcaseListing
}

var _ store.PublicReadQueries = (*store.Queries)(nil)

// Service is the public-route boundary consumed by api.New.
type Service struct {
	json     http.Handler
	photo    http.Handler
	pdf      http.Handler
	png      http.Handler
	card     http.Handler
	html     http.Handler
	markdown http.Handler
	sitemap  http.Handler
	robots   http.Handler
	llms     http.Handler
	live     http.Handler
	showcase http.Handler
}

// NewService creates the public-route dispatcher from its dependencies.
func NewService(dependencies ServiceDependencies) (*Service, error) {
	if dependencies.Reader == nil || dependencies.DiscoveryStore == nil || dependencies.Cache == nil || dependencies.Renderer == nil || dependencies.PublicOrigin.String() == "" || dependencies.AppDigest == "" || dependencies.RendererDigest == "" {
		return nil, ErrUnavailableDependencies
	}
	html, err := NewHTMLHandler(HTMLDependencies{
		Reader: dependencies.Reader, Cache: dependencies.Cache, Renderer: dependencies.Renderer, PublicOrigin: dependencies.PublicOrigin,
		AppDigest: dependencies.AppDigest, RendererDigest: dependencies.RendererDigest, Logger: dependencies.Logger, Cards: dependencies.Cards, Views: dependencies.Views,
		ViewPassKey: dependencies.ViewPassKey, Clock: dependencies.Clock, GateProviders: dependencies.GateProviders, JoinInviteTarget: dependencies.JoinInviteTarget,
	})
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
	if dependencies.Showcase != nil {
		service.showcase = newShowcaseHandler(dependencies.Showcase, dependencies.TrustedProxies, dependencies.Clock)
	}
	service.json = service.newJSONHandler(dependencies.Reader, dependencies.Cache, dependencies.AppDigest, dependencies.ViewPassKey, dependencies.Clock)
	service.photo = service.newPhotoHandler(dependencies.Reader, dependencies.Cache, dependencies.AppDigest, dependencies.ViewPassKey, dependencies.Clock)
	artifacts, artifactErr := newArtifactHandlers(ArtifactDependencies{
		Reader: dependencies.Reader, Cache: dependencies.Cache, Queue: dependencies.PrintQueue,
		AppDigest: dependencies.AppDigest, RendererDigest: dependencies.RendererDigest,
		TrustedProxies: dependencies.TrustedProxies, Clock: dependencies.Clock, Cards: dependencies.Cards,
		ViewPassKey: dependencies.ViewPassKey,
	})
	if artifactErr != nil {
		return nil, artifactErr
	}
	service.pdf, service.png, service.card = artifacts.pdf, artifacts.png, artifacts.card
	return service, nil
}

func (s *Service) newJSONHandler(reader *publicresume.Reader, cache *publiccache.Cache, appDigest string, viewPassKey []byte, clock func() time.Time) http.Handler {
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
		// The pass check runs after admission and before any public cache
		// lookup (docs/design/viewer-analytics/sign-in-to-view.md "Gated
		// routes"; AC-VIEW-003).
		if snapshot.SignInToView && !hasValidPass(request, viewPassKey, snapshot.ResumeID, snapshot.ViewPassEpoch, clock) {
			servePublicJSONError(w, request, http.StatusNotFound)
			return
		}
		key := publiccache.Key{RouteClass: "resume", Representation: publicstate.RepresentationJSON, Variant: "default", ResumeID: snapshot.ResumeID, Generation: snapshot.Revision, FormatVersion: jsonFormatVersion, AppDigest: appDigest}
		if cached, ok := cache.Get(key); ok {
			withPrivateCacheControl(withDiscoveryRobots(SelectedResponse{Status: cached.Status, Header: cached.Header, Body: cached.Body}, snapshot.DiscoveryEnabled), snapshot.SignInToView).ServeHTTP(w, request)
			return
		}
		response, err := NewPublicJSON(snapshot.Public)
		if err != nil {
			servePublicJSONError(w, request, http.StatusServiceUnavailable)
			return
		}
		cache.Put(key, publiccache.Value{Status: response.Status, Header: response.Header, Body: response.Body})
		withPrivateCacheControl(withDiscoveryRobots(response, snapshot.DiscoveryEnabled), snapshot.SignInToView).ServeHTTP(w, request)
	})
}

func (s *Service) newPhotoHandler(reader *publicresume.Reader, cache *publiccache.Cache, appDigest string, viewPassKey []byte, clock func() time.Time) http.Handler {
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
		if snapshot.SignInToView && !hasValidPass(request, viewPassKey, snapshot.ResumeID, snapshot.ViewPassEpoch, clock) {
			servePublicJSONError(w, request, http.StatusNotFound)
			return
		}
		key := publiccache.Key{RouteClass: "resume", Representation: publicstate.RepresentationPhoto, Variant: "default", ResumeID: snapshot.ResumeID, Generation: snapshot.Revision, FormatVersion: photoFormatVersion, AppDigest: appDigest}
		if cached, ok := cache.Get(key); ok {
			withPrivateCacheControl(withDiscoveryRobots(SelectedResponse{Status: cached.Status, Header: cached.Header, Body: cached.Body}, snapshot.DiscoveryEnabled), snapshot.SignInToView).ServeHTTP(w, request)
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
		withPrivateCacheControl(withDiscoveryRobots(response, snapshot.DiscoveryEnabled), snapshot.SignInToView).ServeHTTP(w, request)
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
