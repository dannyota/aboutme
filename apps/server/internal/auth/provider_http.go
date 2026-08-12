package auth

// Provider HTTP calls use one client with a fixed timeout. GitHub response
// bodies also have a separate byte limit at the read site.

import (
	"context"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

// providerHTTPTimeout bounds discovery, JWKS, token, and profile requests.
const providerHTTPTimeout = 10 * time.Second

// maxProviderResponseBytes bounds each GitHub JSON response before decoding.
const maxProviderResponseBytes = 1 << 20 // 1 MiB

// providerHTTPClient is safe for concurrent use by all provider requests.
var providerHTTPClient = &http.Client{Timeout: providerHTTPTimeout}

// withProviderHTTPClient configures oauth2 and go-oidc calls to use the bounded
// client. Callers must pass the returned context through the complete flow.
func withProviderHTTPClient(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, providerHTTPClient)
}
