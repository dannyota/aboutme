package auth

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const googleIssuer = "https://accounts.google.com"

func googleScopes() []string {
	return []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail}
}

type googleClaims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
}

type googleProviderConfig struct {
	clientID     string
	clientSecret string

	cache oidcProviderCache
}

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

func (s *Service) googleOAuth2Config(endpoint oauth2.Endpoint, redirectURL string) oauth2.Config {
	return oauth2.Config{
		ClientID:     s.google.clientID,
		ClientSecret: s.google.clientSecret,
		Endpoint:     endpoint,
		RedirectURL:  redirectURL,
		Scopes:       googleScopes(),
	}
}

func (s *Service) googleRedirectURL() string {
	return s.publicOrigin + GoogleCallbackPath
}
