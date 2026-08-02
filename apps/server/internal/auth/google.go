package auth

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// googleIssuer is the real Google OIDC discovery issuer (design spec §3's
// OAuth table). Tests point Service at a different issuer instead (an
// oidctest.Provider's URL) via NewServiceForTest/googleIssuerOverride, so
// both production and tests run the exact same discovery/exchange/verify
// code path -- only the issuer URL differs.
const googleIssuer = "https://accounts.google.com"

// googleScopes are the OAuth2 scopes requested for Google login: "openid"
// is required for the OIDC flow itself; "email" is what design spec §3
// requires this provider check (sub claim + email_verified == true);
// "profile" (fix-round ruling b1) requests the optional "name" claim so
// resolveGoogleUser's email-local-part fallback is the exception, not the
// default, for a real Google login -- Google still doesn't guarantee the
// claim is present even with this scope granted, and oidctest's Claims has
// no Name field at all, so every test still exercises the fallback.
var googleScopes = []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail}

// googleClaims is the subset of a Google ID token's claims this package
// reads. Name is optional (unlike Email/EmailVerified, it is not a claim
// this package requires); see resolveGoogleUser for the fallback when
// it's absent.
type googleClaims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
}

// googleProviderConfig holds Google's OAuth2 client credentials and the
// lazily-discovered, cached *oidc.Provider backing them, via the shared
// oidcProviderCache (provider_cache.go) -- a distinct type (rather than
// fields directly on Service) so the cache's mutex only ever guards the
// one thing it protects.
type googleProviderConfig struct {
	clientID     string
	clientSecret string

	cache oidcProviderCache
}

// googleProvider returns the discovered Google OIDC provider, discovering
// (and caching) it on first use rather than at Service construction time:
// NewService takes no context and must not perform network I/O of its
// own (the same lazy-dial reasoning as store.NewPool not dialing the
// database eagerly), so discovery happens the first time /start or
// /callback actually needs it. See oidcProviderCache.discover for the
// caching/concurrency contract (and the mutex-across-network-call hazard
// it fixes).
func (s *Service) googleProvider(ctx context.Context) (*oidc.Provider, error) {
	issuer := googleIssuer
	if s.googleIssuerOverride != "" {
		issuer = s.googleIssuerOverride
	}

	p, err := s.google.cache.discover(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("auth: discover google oidc provider: %w", err)
	}
	return p, nil
}

// googleOAuth2Config builds the oauth2.Config for a single request, from
// the discovered provider's endpoint and this Service's redirect URL.
// Built fresh per call (cheap, no I/O) rather than cached, since
// endpoint/redirectURL are already immutable once discovery has run.
func (s *Service) googleOAuth2Config(endpoint oauth2.Endpoint, redirectURL string) oauth2.Config {
	return oauth2.Config{
		ClientID:     s.google.clientID,
		ClientSecret: s.google.clientSecret,
		Endpoint:     endpoint,
		RedirectURL:  redirectURL,
		Scopes:       googleScopes,
	}
}

// googleRedirectURL is the absolute callback URL registered with Google
// and sent as this flow's redirect_uri -- must be byte-identical between
// the /start authorize request and the /callback token exchange, per
// OAuth2's redirect_uri-must-match requirement.
func (s *Service) googleRedirectURL() string {
	return s.publicOrigin + GoogleCallbackPath
}
