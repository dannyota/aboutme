package directrender

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/dannyota/aboutme/apps/server/internal/previewmeta"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

// PublicRenderMode identifies the renderer mode used for public pages.
const PublicRenderMode = "continuous"

// GateRenderMode identifies the renderer mode used for the sign-in-to-view
// gate page (docs/design/viewer-analytics/sign-in-to-view.md "Gate").
const GateRenderMode = "gate"

// PublicRenderRequest contains all renderer inputs for one public resume.
type PublicRenderRequest struct {
	PublicResume     publicresume.PublicResume `json:"publicResume"`
	Mode             string                    `json:"mode"`
	CanonicalOrigin  string                    `json:"canonicalOrigin"`
	DiscoveryEnabled bool                      `json:"discoveryEnabled"`
	// PageTitle and FaviconHref are the exact head values the public HTML
	// validator expects; FaviconHref is "" when the owner set no icon.
	PageTitle   string `json:"pageTitle"`
	FaviconHref string `json:"faviconHref"`
	// Preview is the link-preview text the page head carries; the validator
	// accepts exactly these values (docs/design/link-previews.md).
	Preview previewmeta.Meta `json:"preview"`
	// JoinInvite is "/register" or "/login" for a sign-in-to-view resume's
	// page, and omitted for every other public resume
	// (docs/design/viewer-analytics/sign-in-to-view.md "Join invite"). It
	// authorizes nothing; it only names the link the page's join invite uses.
	JoinInvite string `json:"joinInvite,omitempty"`
}

// GateRenderRequest contains the renderer inputs for the sign-in-to-view
// gate page. It carries no resume content
// (docs/design/viewer-analytics/sign-in-to-view.md "Gate"; AC-VIEW-002).
type GateRenderRequest struct {
	Mode            string           `json:"mode"`
	CanonicalOrigin string           `json:"canonicalOrigin"`
	Slug            string           `json:"slug"`
	Lng             string           `json:"lng"`
	PageTitle       string           `json:"pageTitle"`
	FaviconHref     string           `json:"faviconHref"`
	Preview         previewmeta.Meta `json:"preview"`
	// Providers is a subset of ["google","linkedin"] in that order; it may
	// be empty.
	Providers []string `json:"providers"`
	// Message is "none", the canceled outcome, or the failed outcome
	// (docs/design/viewer-analytics/sign-in-to-view.md "Gate"); see
	// gateMessage for the exact wire values.
	Message string `json:"message"`
}

// Result is a validated renderer response.
type Result struct{ HTML []byte }

// RenderOrigin is the configured internal origin for direct rendering.
type RenderOrigin struct{ value string }

// Client calls the internal renderer with a fixed public-render contract.
type Client struct {
	origin RenderOrigin
	http   *http.Client
}

// ErrRenderUnavailable classifies all direct-render dependency failures.
var ErrRenderUnavailable = errors.New("direct render unavailable")

// RenderStatusError reports an unexpected renderer HTTP status.
type RenderStatusError struct{ Status int }

func (e *RenderStatusError) Error() string { return fmt.Sprintf("direct render status %d", e.Status) }

// RenderResponseTooLargeError reports a renderer response above the byte limit.
type RenderResponseTooLargeError struct{ Limit int64 }

func (e *RenderResponseTooLargeError) Error() string {
	return fmt.Sprintf("direct render response exceeds %d bytes", e.Limit)
}

// New creates a direct-render client for origin.
func New(origin RenderOrigin, client *http.Client) *Client {
	if client == nil {
		client = http.DefaultClient
	}
	return &Client{origin: origin, http: &http.Client{
		Transport: client.Transport,
		Timeout:   client.Timeout,
		Jar:       nil,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

// Probe verifies that a request can be rendered.
func (c *Client) Probe(ctx context.Context, request PublicRenderRequest) error {
	_, err := c.Render(ctx, request)
	return err
}
