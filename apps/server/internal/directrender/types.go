package directrender

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

const PublicRenderMode = "continuous"

type PublicRenderRequest struct {
	PublicResume     publicresume.PublicResume `json:"publicResume"`
	Mode             string                    `json:"mode"`
	CanonicalOrigin  string                    `json:"canonicalOrigin"`
	DiscoveryEnabled bool                      `json:"discoveryEnabled"`
}

type Result struct{ HTML []byte }

type RenderOrigin struct{ value string }

type Client struct {
	origin RenderOrigin
	http   *http.Client
}

var ErrRenderUnavailable = errors.New("direct render unavailable")

type RenderStatusError struct{ Status int }

func (e *RenderStatusError) Error() string { return fmt.Sprintf("direct render status %d", e.Status) }

type RenderResponseTooLargeError struct{ Limit int64 }

func (e *RenderResponseTooLargeError) Error() string {
	return fmt.Sprintf("direct render response exceeds %d bytes", e.Limit)
}

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

func (c *Client) Probe(ctx context.Context, request PublicRenderRequest) error {
	_, err := c.Render(ctx, request)
	return err
}
