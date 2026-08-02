package auth

// provider_http.go bounds every OUTBOUND HTTP call this package makes to
// an OAuth/OIDC provider -- Google/LinkedIn's OIDC discovery and ID token
// verification (JWKS fetch), and all three providers' token exchange and
// (GitHub's) REST API calls. Security-relevant cheap-win fix: the server's
// own ReadHeaderTimeout (cmd/server/main.go) bounds INBOUND requests only;
// nothing previously bounded how long a slow, hung, or malicious provider
// endpoint could keep one of these OUTBOUND calls -- and the goroutine and
// connection serving the visitor's own request -- open, nor how large a
// response body this package would read into memory before decoding it.

import (
	"context"
	"net/http"
	"time"

	"golang.org/x/oauth2"
)

// providerHTTPTimeout bounds every outbound HTTP call this package makes
// to a provider: OIDC discovery, ID token verification's JWKS fetch, an
// authorization code's token exchange, and GitHub's /user and
// /user/emails REST calls. 10s is generous for any of these -- each is a
// single small request/response against a well-behaved provider -- while
// still bounding the worst case to something far short of the client's
// own patience for a stalled login attempt.
const providerHTTPTimeout = 10 * time.Second

// maxProviderResponseBytes bounds how much of a provider's HTTP response
// body githubAPIGet (github.go) will ever read before decoding it as
// JSON: GitHub's /user and /user/emails responses are both small, fixed-
// shape JSON documents, so 1 MiB is generous headroom, not a tight fit --
// this exists to stop an unbounded read (of an arbitrarily large body a
// misbehaving or compromised endpoint returns, e.g. under a test/dev
// githubEndpointOverride pointed at the wrong place) from exhausting
// memory, not to accommodate any response this package expects to
// legitimately see.
const maxProviderResponseBytes = 1 << 20 // 1 MiB

// providerHTTPClient is the single *http.Client every outbound provider
// call in this package shares, bounded by providerHTTPTimeout. Safe for
// concurrent use across every request this process ever handles --
// *http.Client's own documented contract -- so one package-level instance
// is correct, not merely convenient.
var providerHTTPClient = &http.Client{Timeout: providerHTTPTimeout}

// withProviderHTTPClient returns ctx carrying providerHTTPClient via the
// oauth2.HTTPClient context key. This ONE call covers every outbound
// provider call downstream of it, for both OIDC providers (Google/
// LinkedIn) and GitHub alike:
//
//   - golang.org/x/oauth2's own Config.Exchange and Config.Client both
//     resolve their *http.Client from this exact context key
//     (internal.ContextClient), falling back to http.DefaultClient (no
//     timeout at all) only when it is absent.
//   - coreos/go-oidc's oidc.ClientContext is documented as "sets the same
//     context key used by ... oauth2" -- so oidc.NewProvider (discovery)
//     and an *oidc.IDTokenVerifier's JWKS fetch (Verify) both pick up the
//     SAME bounded client from a context built this way, with no
//     go-oidc-specific call needed.
//
// Callers thread the returned context through the rest of the handler
// (Consume, Exchange, the discovered provider's Verifier, and, for
// GitHub, the oauth2.Config.Client used by githubAPIGet) rather than
// re-deriving it per call.
func withProviderHTTPClient(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, providerHTTPClient)
}
