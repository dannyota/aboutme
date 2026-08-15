package directrender

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

// PublicRenderMode identifies the renderer mode used for public pages.
const PublicRenderMode = "continuous"

// PublicRenderRequest contains all renderer inputs for one public resume.
type PublicRenderRequest struct {
	PublicResume     publicresume.PublicResume `json:"publicResume"`
	Mode             string                    `json:"mode"`
	CanonicalOrigin  string                    `json:"canonicalOrigin"`
	DiscoveryEnabled bool                      `json:"discoveryEnabled"`
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
