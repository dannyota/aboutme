package auth

// Provider HTTP calls use one client with a fixed timeout. GitHub response
// bodies also have a separate byte limit at the read site.

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// providerHTTPTimeout bounds discovery, JWKS, token, and profile requests.
const providerHTTPTimeout = 10 * time.Second

// maxProviderResponseBytes bounds each GitHub JSON response before decoding.
const maxProviderResponseBytes = 1 << 20 // 1 MiB

// providerHTTPClient is safe for concurrent use by all provider requests.
var providerHTTPClient = &http.Client{Timeout: providerHTTPTimeout}

// localProviderHTTPClient returns a bounded client that can dial only an
// IPv4 or IPv6 loopback listener. It disables proxy use so a local provider
// request can never carry credentials through an ambient HTTP proxy.
func localProviderHTTPClient() *http.Client {
	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		panic("http.DefaultTransport must be an *http.Transport")
	}
	transport := defaultTransport.Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: providerHTTPTimeout}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("auth: local provider address: %w", err)
		}
		if host == "localhost" {
			host = "127.0.0.1"
		} else {
			ip := net.ParseIP(host)
			if ip == nil || !ip.IsLoopback() {
				return nil, fmt.Errorf("auth: local provider host %q is not loopback", host)
			}
		}
		return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
	}
	return &http.Client{Timeout: providerHTTPTimeout, Transport: transport}
}

func withLocalProviderHTTPClient(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, localProviderHTTPClient())
}

func (s *Service) withGitHubProviderHTTPClient(ctx context.Context) context.Context {
	if s.githubLocalOAuth && s.githubEndpointOverride == "" {
		return withLocalProviderHTTPClient(ctx)
	}
	return withProviderHTTPClient(ctx)
}

func validateLocalOIDCProvider(p *oidc.Provider, publicOrigin string, provider Provider) error {
	if err := validateLocalOIDCEndpoint(p.Endpoint(), publicOrigin, provider); err != nil {
		return err
	}
	var metadata struct {
		JWKSURL string `json:"jwks_uri"`
	}
	if err := p.Claims(&metadata); err != nil {
		return fmt.Errorf("discovery metadata: %w", err)
	}
	return validateLocalBackchannelURL(metadata.JWKSURL, "/"+string(provider)+"/jwks.json")
}

func validateLocalOIDCEndpoint(endpoint oauth2.Endpoint, publicOrigin string, provider Provider) error {
	wantAuthorize := publicOrigin + "/__uat/oauth/" + string(provider) + "/authorize"
	if endpoint.AuthURL != wantAuthorize {
		return fmt.Errorf("authorization URL must equal %s", wantAuthorize)
	}
	if err := validateLocalBackchannelURL(endpoint.TokenURL, "/"+string(provider)+"/token"); err != nil {
		return fmt.Errorf("token URL: %w", err)
	}
	return nil
}

func validateLocalBackchannelURL(raw, wantPath string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https")
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return fmt.Errorf("URL must not contain user info, query, or fragment")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("URL must include a valid explicit port")
	}
	host := u.Hostname()
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("host must be loopback")
		}
	}
	if u.EscapedPath() != wantPath {
		return fmt.Errorf("path must equal %s", wantPath)
	}
	return nil
}

// withProviderHTTPClient configures oauth2 and go-oidc calls to use the bounded
// client. Callers must pass the returned context through the complete flow.
func withProviderHTTPClient(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, providerHTTPClient)
}
