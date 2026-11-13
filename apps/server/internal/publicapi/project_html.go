package publicapi

// The project list and project page handlers
// (docs/design/projects-contract.md "Routes and dispatch", "Render
// envelope"; docs/design/ui/project-pages.md).

import (
	"context"
	"errors"
	"net/http"

	"danny.vn/aboutme/apps/server/internal/previewcard"
	"danny.vn/aboutme/apps/server/internal/publiccache"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/publicstate"
)

// projectFormatVersion names the cached project page format. It rises
// whenever either page's request or validation changes, so no cached page
// of an older format is served.
const projectFormatVersion = 2

// projectPageCSP is the resume page's policy with style-src 'self' alone:
// the project pages load their CSS only from versioned stylesheets and carry
// none inline. The gate for a project address keeps publicformat.BaseCSP.
const projectPageCSP = "default-src 'none'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'none'; " +
	"script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; manifest-src 'self'; " +
	"media-src 'none'; worker-src 'none'"

// NewProjectListHandler creates the handler for /{slug}/projects. It shares
// the resume page's dependencies.
func NewProjectListHandler(dependencies HTMLDependencies) (http.Handler, error) {
	return newProjectHandler(dependencies, false)
}

// NewProjectHandler creates the handler for /{slug}/projects/{project-slug}.
// It shares the resume page's dependencies.
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
		// No view is observed on a project page (ADR 0022). The pass check
		// runs after admission and before the pages are built, the
		// availability decision and any public cache lookup: without a pass
		// every address of a gated resume serves the same gate after the same
		// work, so no one can probe which projects exist, and cached project
		// bytes are never served without a pass.
		if snapshot.SignInToView && !hasValidPass(request, dependencies.ViewPassKey, snapshot.ResumeID, snapshot.ViewPassEpoch, dependencies.Clock) {
			gatePage := "projects"
			if page {
				gatePage += "/" + projectSlug
			}
			//nolint:contextcheck // The lease context is derived from request.Context and adds revocation cancellation.
			serveGate(lease.Context(), w, request, dependencies, snapshot, gatePage)
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

// renderProjectPage builds, renders and validates one project page. On
// failure it returns the closed reason and, for rejected HTML, the rule, so
// the caller logs no slug or text.
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

// publicHTMLVariant separates the cached HTML of a sign-in-to-view resume
// from a discoverable and a nondiscoverable one. Only pass holders receive
// the sign-in variant, and the resume page's carries the join-invite marker
// (docs/design/viewer-analytics/sign-in-to-view.md "Join invite"), so it is
// never shared with an ordinary resume's entry.
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
