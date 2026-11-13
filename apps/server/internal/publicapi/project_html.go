package publicapi

import (
	"context"
	"errors"
	"net/http"

	"danny.vn/aboutme/apps/server/internal/previewcard"
	"danny.vn/aboutme/apps/server/internal/publiccache"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/publicstate"
)

const projectFormatVersion = 2

const projectPageCSP = "default-src 'none'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'none'; " +
	"script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; manifest-src 'self'; " +
	"media-src 'none'; worker-src 'none'"

func NewProjectListHandler(dependencies HTMLDependencies) (http.Handler, error) {
	return newProjectHandler(dependencies, false)
}

func NewProjectHandler(dependencies HTMLDependencies) (http.Handler, error) {
	return newProjectHandler(dependencies, true)
}

func newProjectHandler(dependencies HTMLDependencies, page bool) (http.Handler, error) {
	if dependencies.Reader == nil || dependencies.Cache == nil || dependencies.Renderer == nil || dependencies.PublicOrigin.String() == "" || dependencies.AppDigest == "" || dependencies.RendererDigest == "" {
		return nil, ErrUnavailableDependencies
	}
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if !publicHTMLGetOrHead(w, request) {
			return
		}
		slug, projectSlug := projectRouteSlugs(request.URL.Path)
		if page != (projectSlug != "") {
			serveHTMLError(w, request, http.StatusNotFound)
			return
		}
		snapshot, read, lease, err := dependencies.Reader.ReadProjects(request.Context(), slug, projectSlug)
		if err != nil {
			if lease != nil {
				lease.Release()
			}
			if errors.Is(err, publicresume.ErrNotFound) {
				serveHTMLError(w, request, http.StatusNotFound)
				return
			}
			logHTMLUnavailable(dependencies.Logger, request, "read_failed", "")
			serveHTMLError(w, request, http.StatusServiceUnavailable)
			return
		}
		defer releaseAfterFlush(w, lease)
		if snapshot.SignInToView && !hasValidPass(request, dependencies.ViewPassKey, snapshot.ResumeID, snapshot.ViewPassEpoch, dependencies.Clock) {
			gatePage := "projects"
			if page {
				gatePage += "/" + projectSlug
			}
			//nolint:contextcheck // The lease context is derived from request.Context and adds revocation cancellation.
			serveGate(lease.Context(), w, request, dependencies, snapshot, gatePage, lease)
			return
		}
		pages, err := read.Pages()
		if err != nil {
			logHTMLUnavailable(dependencies.Logger, request, "pages_failed", "")
			serveHTMLError(w, request, http.StatusServiceUnavailable)
			return
		}
		if !pages.Shown() {
			serveHTMLError(w, request, http.StatusNotFound)
			return
		}
		routeClass := publiccache.RouteClass("project-list")
		if page {
			routeClass = "project"
		}
		key := publiccache.Key{
			RouteClass: routeClass, Representation: publicstate.RepresentationHTML, Variant: publicHTMLVariant(snapshot),
			ResumeID: snapshot.ResumeID, Generation: snapshot.Revision, FormatVersion: projectFormatVersion,
			AppDigest: dependencies.AppDigest, RendererDigest: dependencies.RendererDigest, Subject: projectSlug,
		}
		if cached, ok := dependencies.Cache.Get(key); ok {
			serveLeasedResponse(w, request, lease, dependencies.Logger, withPrivateCacheControl(SelectedResponse{Status: cached.Status, Header: cached.Header, Body: cached.Body}, snapshot.SignInToView))
			return
		}
		input := projectPageInput{snapshot: snapshot, pages: pages, origin: dependencies.PublicOrigin}
		//nolint:contextcheck // The lease context is derived from request.Context and adds revocation cancellation.
		body, reason, rule := renderProjectPage(lease.Context(), dependencies, input, page)
		if reason != "" {
			logHTMLUnavailable(dependencies.Logger, request, reason, rule)
			serveHTMLError(w, request, http.StatusServiceUnavailable)
			return
		}
		extra := make(http.Header)
		extra.Set("Content-Security-Policy", projectPageCSP)
		if !snapshot.DiscoveryEnabled {
			extra.Set("X-Robots-Tag", "noindex, noarchive")
		}
		response, err := NewSelectedResponse(http.StatusOK, "text/html; charset=utf-8", "no-cache, must-revalidate", body, extra)
		if err != nil {
			logHTMLUnavailable(dependencies.Logger, request, "response_failed", "")
			serveHTMLError(w, request, http.StatusServiceUnavailable)
			return
		}
		dependencies.Cache.Put(key, publiccache.Value{Status: response.Status, Header: response.Header, Body: response.Body})
		serveLeasedResponse(w, request, lease, dependencies.Logger, withPrivateCacheControl(response, snapshot.SignInToView))
	}), nil
}

func renderProjectPage(ctx context.Context, dependencies HTMLDependencies, input projectPageInput, page bool) (body []byte, reason, rule string) {
	if dependencies.Cards != nil {
		version, err := dependencies.Cards.Version(input.snapshot)
		if err != nil {
			return nil, "card_version_failed", ""
		}
		input.imageURL = dependencies.PublicOrigin.Resolve(previewcard.Path(input.snapshot.Public.Slug, version))
	}
	if !page {
		request, err := buildProjectListRequest(input)
		if err != nil {
			return nil, "request_failed", ""
		}
		result, err := dependencies.Renderer.RenderProjectList(ctx, request)
		if err != nil {
			return nil, "render_failed", ""
		}
		if rule := projectListHTMLRejection(result.HTML, request, dependencies.PublicOrigin); rule != "" {
			return nil, "html_rejected", rule
		}
		return result.HTML, "", ""
	}
	request, err := buildProjectRequest(input)
	if err != nil {
		return nil, "request_failed", ""
	}
	result, err := dependencies.Renderer.RenderProject(ctx, request)
	if err != nil {
		return nil, "render_failed", ""
	}
	if rule := projectHTMLRejection(result.HTML, request, dependencies.PublicOrigin); rule != "" {
		return nil, "html_rejected", rule
	}
	return result.HTML, "", ""
}

func publicHTMLVariant(snapshot publicresume.Snapshot) publiccache.Variant {
	switch {
	case snapshot.SignInToView:
		return "sign-in"
	case snapshot.DiscoveryEnabled:
		return "discoverable"
	default:
		return "nondiscoverable"
	}
}
