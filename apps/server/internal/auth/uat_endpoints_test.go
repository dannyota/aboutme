package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/oauth2"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/config"
)

type discoveryFixture struct {
	issuerURL    string
	authorizeURL string
	tokenURL     string
	jwksURL      string
}

func newDiscoveryServer(t *testing.T, provider string, mutate func(*discoveryFixture)) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	var hits atomic.Int64
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)

		fixture := discoveryFixture{
			issuerURL:    server.URL + "/" + provider,
			authorizeURL: "https://localhost:20443/__uat/oauth/" + provider + "/authorize",
			tokenURL:     server.URL + "/" + provider + "/token",
			jwksURL:      server.URL + "/" + provider + "/jwks.json",
		}
		if mutate != nil {
			mutate(&fixture)
		}
		if r.URL.Path != "/"+provider+"/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                fixture.issuerURL,
			"authorization_endpoint":                fixture.authorizeURL,
			"token_endpoint":                        fixture.tokenURL,
			"jwks_uri":                              fixture.jwksURL,
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"},
		}); err != nil {
			t.Errorf("encode discovery: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server, &hits
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNewService_RuntimeOIDCIssuerEndpoint(t *testing.T) {
	for _, provider := range []auth.Provider{auth.ProviderGoogle, auth.ProviderLinkedIn} {
		t.Run(string(provider), func(t *testing.T) {
			server, hits := newDiscoveryServer(t, string(provider), nil)
			cfg := config.Config{PublicOrigin: "https://localhost:20443"}
			if provider == auth.ProviderGoogle {
				cfg.GoogleOIDCIssuerURL = server.URL + "/google"
			} else {
				cfg.LinkedInOIDCIssuerURL = server.URL + "/linkedin"
			}
			svc, err := auth.NewService(testLogger(), cfg, nil)
			if err != nil {
				t.Fatal(err)
			}

			var intercepted string
			interceptClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				intercepted = r.URL.String()
				return nil, errors.New("unexpected caller transport")
			})}
			ctx := context.WithValue(context.Background(), oauth2.HTTPClient, interceptClient)
			endpoint, err := auth.OIDCProviderEndpointForTest(ctx, svc, provider)
			if intercepted != "" {
				t.Fatalf("discovery request = %q, want configured local issuer", intercepted)
			}
			if err != nil {
				t.Fatal(err)
			}
			wantAuthorize := "https://localhost:20443/__uat/oauth/" + string(provider) + "/authorize"
			if endpoint.AuthURL != wantAuthorize {
				t.Errorf("AuthURL = %q, want %q", endpoint.AuthURL, wantAuthorize)
			}
			wantToken := server.URL + "/" + string(provider) + "/token"
			if endpoint.TokenURL != wantToken {
				t.Errorf("TokenURL = %q, want %q", endpoint.TokenURL, wantToken)
			}
			if hits.Load() != 1 {
				t.Errorf("local discovery requests = %d, want 1", hits.Load())
			}
		})
	}
}

func TestNewService_RuntimeGitHubProviderEndpoints(t *testing.T) {
	svc, err := auth.NewService(testLogger(), config.Config{
		PublicOrigin:            "https://localhost:20443",
		GitHubOAuthAuthorizeURL: "https://localhost:20443/__uat/oauth/github/authorize",
		GitHubOAuthTokenURL:     "http://127.0.0.1:20442/github/token",
		GitHubAPIBaseURL:        "http://127.0.0.1:20442/github",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	authorizeURL, tokenURL, apiBaseURL := auth.GitHubProviderEndpointsForTest(svc)
	if authorizeURL != "https://localhost:20443/__uat/oauth/github/authorize" {
		t.Errorf("authorize URL = %q", authorizeURL)
	}
	if tokenURL != "http://127.0.0.1:20442/github/token" {
		t.Errorf("token URL = %q", tokenURL)
	}
	if apiBaseURL != "http://127.0.0.1:20442/github" {
		t.Errorf("API base URL = %q", apiBaseURL)
	}
}

func TestNewService_EmptyRuntimeEndpointsPreserveGitHubProductionConstants(t *testing.T) {
	svc, err := auth.NewService(testLogger(), config.Config{PublicOrigin: "https://aboutme.vn"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	authorizeURL, tokenURL, apiBaseURL := auth.GitHubProviderEndpointsForTest(svc)
	if authorizeURL != "https://github.com/login/oauth/authorize" {
		t.Errorf("authorize URL = %q", authorizeURL)
	}
	if tokenURL != "https://github.com/login/oauth/access_token" {
		t.Errorf("token URL = %q", tokenURL)
	}
	if apiBaseURL != "https://api.github.com" {
		t.Errorf("API base URL = %q", apiBaseURL)
	}
}

func TestRuntimeOIDCDiscoveryRejectsExternalEndpoints(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*discoveryFixture)
	}{
		{name: "authorization", mutate: func(f *discoveryFixture) {
			f.authorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
		}},
		{name: "token", mutate: func(f *discoveryFixture) {
			f.tokenURL = "https://oauth2.googleapis.com:443/token"
		}},
		{name: "jwks", mutate: func(f *discoveryFixture) {
			f.jwksURL = "https://www.googleapis.com:443/oauth2/v3/certs"
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server, hits := newDiscoveryServer(t, "google", tt.mutate)
			svc, err := auth.NewService(testLogger(), config.Config{
				PublicOrigin:        "https://localhost:20443",
				GoogleOIDCIssuerURL: server.URL + "/google",
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			interceptClient := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, errors.New("unexpected non-local discovery request")
			})}
			ctx := context.WithValue(context.Background(), oauth2.HTTPClient, interceptClient)
			_, err = auth.OIDCProviderEndpointForTest(ctx, svc, auth.ProviderGoogle)
			if err == nil {
				t.Fatal("discovery error = nil, want external endpoint rejection")
			}
			if !strings.Contains(err.Error(), "loopback") && !strings.Contains(err.Error(), "authorization") {
				t.Fatalf("discovery error = %q, want local endpoint rejection", err)
			}
			if hits.Load() != 1 {
				t.Fatalf("local discovery requests = %d, want exactly 1 and no endpoint request", hits.Load())
			}
		})
	}
}

func TestLocalProviderHTTPClientRejectsExternalEndpoint(t *testing.T) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://192.0.2.1:20442/google/token", nil)
	if err != nil {
		t.Fatalf("build external endpoint request: %v", err)
	}
	resp, err := auth.LocalProviderHTTPClientForTest().Do(req)
	if resp != nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("close unexpected response: %v", closeErr)
		}
	}
	if err == nil {
		t.Fatal("GET external endpoint error = nil, want loopback-only rejection")
	}
	if !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("GET external endpoint error = %q, want loopback rejection", err)
	}
}

func TestRuntimeGitHubHTTPClientRejectsExternalEndpoint(t *testing.T) {
	svc, err := auth.NewService(testLogger(), config.Config{
		PublicOrigin:            "https://localhost:20443",
		GitHubOAuthAuthorizeURL: "https://localhost:20443/__uat/oauth/github/authorize",
		GitHubOAuthTokenURL:     "http://127.0.0.1:20442/github/token",
		GitHubAPIBaseURL:        "http://127.0.0.1:20442/github",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	client := auth.GitHubProviderHTTPClientForTest(svc)
	if client == nil {
		t.Fatal("GitHub provider client = nil")
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://192.0.2.1:20442/github/token", nil)
	if err != nil {
		t.Fatalf("build external endpoint request: %v", err)
	}
	resp, err := client.Do(req)
	if resp != nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("close unexpected response: %v", closeErr)
		}
	}
	if err == nil {
		t.Fatal("GET external endpoint error = nil, want loopback-only rejection")
	}
	if !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("GET external endpoint error = %q, want loopback rejection", err)
	}
}
