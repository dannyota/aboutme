package auth

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// googleIssuer is Google's OIDC discovery issuer. Tests override only the URL
// and exercise the same discovery, exchange, and verification path.
const googleIssuer = "https://accounts.google.com"

// googleScopes request the ID, verified email, and optional display name used
// by the login flow. The profile scope does not guarantee a name claim.
var googleScopes = []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail}

// googleClaims contains the required email state and optional display name.
type googleClaims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
}

// googleProviderConfig holds credentials and the lazy discovery cache.
type googleProviderConfig struct {
	clientID     string
	clientSecret string

	cache oidcProviderCache
}

// googleProvider discovers and caches Google's provider on first use.
func (s *Service) googleProvider(ctx context.Context) (*oidc.Provider, error) {
	issuer := s.googleIssuerURL
	local := s.googleLocalOIDC
	if s.googleIssuerOverride != "" {
		issuer = s.googleIssuerOverride
		local = false
	}
	if local {
		ctx = withLocalProviderHTTPClient(ctx)
	}

	p, err := s.google.cache.discover(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("auth: discover google oidc provider: %w", err)
	}
	if local {
		if err := validateLocalOIDCProvider(p, s.publicOrigin, ProviderGoogle); err != nil {
			return nil, fmt.Errorf("auth: validate google oidc provider: %w", err)
		}
	}
	return p, nil
}

// googleOAuth2Config builds a request-local configuration without I/O.
func (s *Service) googleOAuth2Config(endpoint oauth2.Endpoint, redirectURL string) oauth2.Config {
	return oauth2.Config{
		ClientID:     s.google.clientID,
		ClientSecret: s.google.clientSecret,
		Endpoint:     endpoint,
		RedirectURL:  redirectURL,
		Scopes:       googleScopes,
	}
}

// googleRedirectURL must match exactly at authorization and token exchange.
func (s *Service) googleRedirectURL() string {
	return s.publicOrigin + GoogleCallbackPath
}
