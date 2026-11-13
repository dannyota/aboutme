package publicapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/previewcard"
	"danny.vn/aboutme/apps/server/internal/previewmeta"
	"danny.vn/aboutme/apps/server/internal/publiccache"
	"danny.vn/aboutme/apps/server/internal/publicformat"
	"danny.vn/aboutme/apps/server/internal/publicpage"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/publicstate"
)

const htmlFormatVersion = 4

type HTMLDependencies struct {
	Reader           *publicresume.Reader
	Cache            *publiccache.Cache
	Renderer         *directrender.Client
	PublicOrigin     publicresume.PublicOrigin
	AppDigest        string
	RendererDigest   string
	Logger           *slog.Logger
	Cards            PreviewCards
	Views            ViewObserver
	ViewPassKey      []byte
	Clock            func() time.Time
	GateProviders    []string
	JoinInviteTarget string
}

func NewHTMLHandler(dependencies HTMLDependencies) (http.Handler, error) {
	if dependencies.Reader == nil || dependencies.Cache == nil || dependencies.Renderer == nil || dependencies.PublicOrigin.String() == "" || dependencies.AppDigest == "" || dependencies.RendererDigest == "" {
		return nil, ErrUnavailableDependencies
	}
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if !publicHTMLGetOrHead(w, request) {
			return
		}
		slug := strings.TrimPrefix(request.URL.Path, "/")
		snapshot, lease, err := dependencies.Reader.ReadResume(request.Context(), slug, publicstate.RepresentationHTML)
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
		observeHTMLView(dependencies.Views, request, snapshot.ResumeID)

		if snapshot.SignInToView && !hasValidPass(request, dependencies.ViewPassKey, snapshot.ResumeID, snapshot.ViewPassEpoch, dependencies.Clock) {
			//nolint:contextcheck // The lease context is derived from request.Context and adds revocation cancellation.
			serveGate(lease.Context(), w, request, dependencies, snapshot, "", lease)
			return
		}

		key := publiccache.Key{
			RouteClass:     "resume",
			Representation: publicstate.RepresentationHTML,
			Variant:        publicHTMLVariant(snapshot),
			ResumeID:       snapshot.ResumeID,
			Generation:     snapshot.Revision,
			FormatVersion:  htmlFormatVersion,
			AppDigest:      dependencies.AppDigest,
			RendererDigest: dependencies.RendererDigest,
		}
		if cached, ok := dependencies.Cache.Get(key); ok {
			serveLeasedResponse(w, request, lease, dependencies.Logger, withPrivateCacheControl(SelectedResponse{Status: cached.Status, Header: cached.Header, Body: cached.Body}, snapshot.SignInToView))
			return
		}

		jsonLD, err := publicformat.JSONLD(snapshot.Public, dependencies.PublicOrigin, snapshot.DiscoveryEnabled)
		if err != nil {
			logHTMLUnavailable(dependencies.Logger, request, "jsonld_failed", "")
			serveHTMLError(w, request, http.StatusServiceUnavailable)
			return
		}
		page := withEntryPoints(expectedPublicPage(snapshot.Public, snapshot.PublicTitle, snapshot.FaviconEmoji), snapshot.Public.Slug, snapshot.ProjectLinks, dependencies.PublicOrigin)
		if snapshot.SignInToView {
			page.JoinInvite = dependencies.JoinInviteTarget
		}
		if dependencies.Cards != nil {
			version, versionErr := dependencies.Cards.Version(snapshot)
			if versionErr != nil {
				logHTMLUnavailable(dependencies.Logger, request, "card_version_failed", "")
				serveHTMLError(w, request, http.StatusServiceUnavailable)
				return
			}
			page.Preview.ImageURL = dependencies.PublicOrigin.Resolve(previewcard.Path(snapshot.Public.Slug, version))
		}
		//nolint:contextcheck // The lease context is derived from request.Context and adds revocation cancellation.
		result, err := dependencies.Renderer.Render(lease.Context(), publicRenderRequest(snapshot.Public, snapshot.DiscoveryEnabled, page, dependencies.PublicOrigin))
		if err != nil {
			logHTMLUnavailable(dependencies.Logger, request, "render_failed", "")
			serveHTMLError(w, request, http.StatusServiceUnavailable)
			return
		}
		if rule := publicHTMLRejectionForPage(result.HTML, snapshot.Public, page, dependencies.PublicOrigin, jsonLD, snapshot.DiscoveryEnabled); rule != "" {
			logHTMLUnavailable(dependencies.Logger, request, "html_rejected", rule)
			serveHTMLError(w, request, http.StatusServiceUnavailable)
			return
		}
		extra := make(http.Header)
		extra.Set("Content-Security-Policy", jsonLD.CSP)
		if !snapshot.DiscoveryEnabled {
			extra.Set("X-Robots-Tag", "noindex, noarchive")
		}
		response, err := NewSelectedResponse(http.StatusOK, "text/html; charset=utf-8", "no-cache, must-revalidate", result.HTML, extra)
		if err != nil {
			logHTMLUnavailable(dependencies.Logger, request, "response_failed", "")
			serveHTMLError(w, request, http.StatusServiceUnavailable)
			return
		}
		dependencies.Cache.Put(key, publiccache.Value{Status: response.Status, Header: response.Header, Body: response.Body})
		serveLeasedResponse(w, request, lease, dependencies.Logger, withPrivateCacheControl(response, snapshot.SignInToView))
	}), nil
}

func publicHTMLGetOrHead(w http.ResponseWriter, request *http.Request) bool {
	if request.Method == http.MethodGet || request.Method == http.MethodHead {
		return true
	}
	w.Header().Set("Allow", "GET, HEAD")
	serveHTMLError(w, request, http.StatusMethodNotAllowed)
	return false
}

func logHTMLUnavailable(logger *slog.Logger, request *http.Request, reason, rule string) {
	if logger == nil {
		return
	}
	attrs := []any{"reason", reason}
	if rule != "" {
		attrs = append(attrs, "rule", rule)
	}
	logger.WarnContext(request.Context(), "publicapi: html unavailable", attrs...)
}

func serveHTMLError(w http.ResponseWriter, request *http.Request, status int) {
	label := map[int]string{
		http.StatusBadRequest:         "Bad request",
		http.StatusNotFound:           "Not found",
		http.StatusMethodNotAllowed:   "Method not allowed",
		http.StatusServiceUnavailable: "Temporarily unavailable",
	}[status]
	body := []byte(fmt.Sprintf("<!doctype html>\n<html lang=\"en\">\n  <head>\n    <meta charset=\"utf-8\" />\n    <title>%s</title>\n  </head>\n  <body>\n    <a href=\"#main\">Skip to content</a>\n    <main id=\"main\"><h1>%s</h1></main>\n  </body>\n</html>\n", label, label))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, must-revalidate")
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
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

type publicPage struct {
	Title        string
	FaviconHref  string
	Preview      previewmeta.Meta
	JoinInvite   string
	ProjectsURL  string
	ProjectLinks []directrender.EntryProjectLink
}

func expectedPublicPage(resume publicresume.PublicResume, publicTitle, faviconEmoji *string) publicPage {
	page := publicPage{
		Title:   publicpage.EffectiveTitle(publicTitle, resume.Document.PersonalDetails.FullName),
		Preview: previewmeta.For(resume, publicTitle),
	}
	if faviconEmoji != nil && *faviconEmoji != "" {
		page.FaviconHref = publicpage.FaviconHref(*faviconEmoji)
	}
	return page
}
