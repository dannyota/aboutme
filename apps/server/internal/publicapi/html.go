package publicapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/directrender"
	"github.com/dannyota/aboutme/apps/server/internal/previewcard"
	"github.com/dannyota/aboutme/apps/server/internal/previewmeta"
	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/publicformat"
	"github.com/dannyota/aboutme/apps/server/internal/publicpage"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
)

// htmlFormatVersion names the cached page format. It rises whenever the page
// head changes, so no cached page with an older head is served.
const htmlFormatVersion = 3

// HTMLDependencies contains the dependencies for public HTML responses.
type HTMLDependencies struct {
	Reader         *publicresume.Reader
	Cache          *publiccache.Cache
	Renderer       *directrender.Client
	PublicOrigin   publicresume.PublicOrigin
	AppDigest      string
	RendererDigest string
	// Logger receives one closed, content-free line per 503. Nil disables it.
	Logger *slog.Logger
	// Cards turns on stored preview cards: the page names the current card
	// version. Nil keeps the og.png share image.
	Cards PreviewCards
	// Views records crawler and link-preview fetches. Nil records nothing.
	Views ViewObserver
	// ViewPassKey seals and opens the __Host-view-pass cookie
	// (docs/design/viewer-analytics/sign-in-to-view.md "Pass cookie"). A nil
	// or empty key never validates a pass.
	ViewPassKey []byte
	// Clock lets tests inject a deterministic time for pass expiry. Nil uses
	// time.Now.
	Clock func() time.Time
	// GateProviders is the fixed subset of ["google","linkedin"] the gate
	// offers, computed once at startup from provider enablement
	// (docs/design/viewer-analytics/sign-in-to-view.md "Gate"; ADR 0016).
	GateProviders []string
	// JoinInviteTarget is "/register" or "/login": the join invite's link
	// for a sign-in-to-view resume's page
	// (docs/design/viewer-analytics/sign-in-to-view.md "Join invite").
	JoinInviteTarget string
}

// NewHTMLHandler creates the handler for public resume HTML pages.
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
		defer lease.Release()
		observeHTMLView(dependencies.Views, request, snapshot.ResumeID)

		// The pass check runs after admission and before any public cache
		// lookup, so cached resume bytes are never served without a pass
		// (docs/design/viewer-analytics/sign-in-to-view.md "Gated routes";
		// AC-VIEW-003).
		if snapshot.SignInToView && !hasValidPass(request, dependencies.ViewPassKey, snapshot.ResumeID, snapshot.ViewPassEpoch, dependencies.Clock) {
			//nolint:contextcheck // The lease context is derived from request.Context and adds revocation cancellation.
			serveGate(lease.Context(), w, request, dependencies, snapshot)
			return
		}

		variant := publiccache.Variant("nondiscoverable")
		switch {
		case snapshot.SignInToView:
			// A distinct variant, never shared with an ordinary
			// nondiscoverable resume's cache entry, since this body carries
			// the join-invite marker (docs/design/viewer-analytics/
			// sign-in-to-view.md "Join invite").
			variant = "sign-in"
		case snapshot.DiscoveryEnabled:
			variant = "discoverable"
		}
		key := publiccache.Key{
			RouteClass:     "resume",
			Representation: publicstate.RepresentationHTML,
			Variant:        variant,
			ResumeID:       snapshot.ResumeID,
			Generation:     snapshot.Revision,
			FormatVersion:  htmlFormatVersion,
			AppDigest:      dependencies.AppDigest,
			RendererDigest: dependencies.RendererDigest,
		}
		if cached, ok := dependencies.Cache.Get(key); ok {
			withPrivateCacheControl(SelectedResponse{Status: cached.Status, Header: cached.Header, Body: cached.Body}, snapshot.SignInToView).ServeHTTP(w, request)
			return
		}

		jsonLD, err := publicformat.JSONLD(snapshot.Public, dependencies.PublicOrigin, snapshot.DiscoveryEnabled)
		if err != nil {
			logHTMLUnavailable(dependencies.Logger, request, "jsonld_failed", "")
			serveHTMLError(w, request, http.StatusServiceUnavailable)
			return
		}
		page := expectedPublicPage(snapshot.Public, snapshot.PublicTitle, snapshot.FaviconEmoji)
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
		withPrivateCacheControl(response, snapshot.SignInToView).ServeHTTP(w, request)
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

// logHTMLUnavailable records why a public page returned 503 with closed values
// only: a reason and, for rejected HTML, the rule name. It never logs the slug
// or any resume content.
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

// publicPage is the head the renderer must emit for the owner's stored page
// settings: the exact <title> text, the exact favicon href or "" for none, and
// the preview text (docs/design/link-previews.md).
type publicPage struct {
	Title       string
	FaviconHref string
	Preview     previewmeta.Meta
	// JoinInvite is "/register" or "/login" for a sign-in-to-view resume's
	// page, and "" for every other public resume
	// (docs/design/viewer-analytics/sign-in-to-view.md "Join invite").
	JoinInvite string
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
