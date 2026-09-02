package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Default values used when the corresponding Options field is zero.
const (
	// DefaultBodyLimitBytes matches the request-body budget (256 KB); see
	// docs/design/budgets.md. Applies to every route EXCEPT the health
	// endpoints — see HealthBodyLimitBytes.
	DefaultBodyLimitBytes int64 = 256 * 1024
	// HealthBodyLimitBytes caps request bodies on the health chain far
	// below DefaultBodyLimitBytes, independent of Options.BodyLimitBytes.
	// Health endpoints (see isHealthPath) never expect a body at all, and
	// — unlike every other route — they are deliberately exempt from
	// RateLimit (see healthChain's comment below), so nothing upstream
	// bounds how often a caller can make this server buffer a body in
	// memory before rejecting it. A small dedicated cap is what keeps that
	// exemption safe.
	HealthBodyLimitBytes int64 = 4 * 1024
	// DefaultReadyTimeout bounds how long each real database ping
	// performed on /readyz's behalf may take before it is treated as
	// unreachable.
	DefaultReadyTimeout = 3 * time.Second
	// DefaultReadyTTL is how long /readyz's cached readiness result —
	// success or failure — is reused before a fresh database ping is
	// attempted; see cachedPinger. One second bounds the amplification an
	// unbounded flood of /readyz requests could otherwise cause against
	// the connection pool. It remains short enough to reflect a recovery or
	// outage promptly.
	DefaultReadyTTL = time.Second
)

// Options configures the router built by New. The zero value uses the
// package defaults.
type Options struct {
	// BodyLimitBytes caps request body size on every route except the
	// health endpoints (see HealthBodyLimitBytes); requests over the limit
	// get 413. Defaults to DefaultBodyLimitBytes.
	BodyLimitBytes int64
	// ReadyTimeout bounds each real database ping performed on /readyz's
	// behalf. Defaults to DefaultReadyTimeout.
	ReadyTimeout time.Duration
	// ReadyTTL bounds how long /readyz's cached readiness result is reused
	// before a fresh database ping is attempted. Defaults to
	// DefaultReadyTTL.
	ReadyTTL time.Duration
	// TrustedProxies controls which peers RateLimit and SecurityHeaders
	// treat as a trusted reverse proxy able to assert this request's real
	// client IP and scheme — see TrustedProxies. The zero value (nil)
	// trusts no one, so callers must supply the deployment's real
	// trusted-proxy CIDRs explicitly (internal/config's
	// TRUSTED_PROXY_CIDRS, populated from main.go) rather than this package
	// assuming a topology. A loopback default is unsafe when a reverse proxy
	// reaches Go over a container network because it collapses all client
	// rate-limit keys to the proxy address. See docs/design/deployment.md.
	TrustedProxies TrustedProxies
	// Clock returns the current time, used by RateLimit's token buckets
	// and by /readyz's readiness cache (see cachedPinger). Defaults to
	// time.Now; tests inject a fake clock (e.g. testutil.Clock) so both
	// are deterministic and require no sleeping.
	Clock func() time.Time
}

// PublicRoutes is the public-route boundary. It receives recognized public
// paths before default request-body and rate middleware can read a viewer body.
type PublicRoutes interface {
	Recognizes(escapedPath string) bool
	ServeHTTP(http.ResponseWriter, *http.Request)
}

func (o Options) withDefaults() Options {
	if o.BodyLimitBytes <= 0 {
		o.BodyLimitBytes = DefaultBodyLimitBytes
	}
	if o.ReadyTimeout <= 0 {
		o.ReadyTimeout = DefaultReadyTimeout
	}
	if o.ReadyTTL <= 0 {
		o.ReadyTTL = DefaultReadyTTL
	}
	if o.Clock == nil {
		o.Clock = time.Now
	}
	return o
}

// New builds the server's top-level HTTP handler. It wires pinger into the
// readiness endpoint and applies the path-specific middleware chains below.
//
// Unmatched routes and disallowed methods use the standard error envelope.
// Registered handlers own their success representation, including JSON,
// binary media, and streams. Routes omit stdlib ServeMux's method-prefix syntax
// (e.g. "GET /healthz") so this package can produce enveloped 404 and 405
// responses via WriteError instead of ServeMux's plain-text defaults.
//
// register lets callers (the composition root in cmd/server/main.go)
// attach additional routes without this package importing the packages
// that define them — internal/auth imports internal/api (for its error
// envelope and TrustedProxies), so the reverse import would cycle. Each
// func receives the same mux built below, before the middleware chain is
// wrapped around it, so every extra route gets the same RequestID/
// SecurityHeaders/Logging/RateLimit/BodyLimit treatment as /healthz and
// /readyz's siblings.
func New(logger *slog.Logger, pinger DBPinger, opts Options, public PublicRoutes, register ...func(*http.ServeMux)) http.Handler {
	opts = opts.withDefaults()

	// Readyz never sees the raw pinger directly: cachedPinger memoizes it
	// behind a single-flight, short-TTL cache (see DefaultReadyTTL) so an
	// unbounded flood of /readyz requests costs the database at most one
	// round trip per TTL, regardless of request volume. See healthChain below
	// for why /readyz has no RateLimit ceiling.
	readyPinger := newCachedPinger(pinger, opts.ReadyTimeout, opts.ReadyTTL, opts.Clock)

	mux := http.NewServeMux()
	mux.Handle("/healthz", route(http.MethodGet, Healthz()))
	mux.Handle("/readyz", route(http.MethodGet, Readyz(readyPinger, opts.ReadyTimeout)))
	for _, r := range register {
		r(mux)
	}
	// "/" is a subtree pattern: it matches every path not claimed by a more
	// specific registration above, making it the catch-all 404 handler.
	// net/http's ServeMux dispatches by pattern specificity, not
	// registration order, so registering this after the register funcs
	// above does not let it shadow anything they added.
	mux.Handle("/", NotFound())

	// Health probes (see isHealthPath) get their own chain, deliberately
	// without RateLimit: they are infrastructure traffic (ECS/CloudFront
	// health checks, a local operator's curl), not external viewers, and
	// forcing them through the viewer-keyed limiter risks a false 429/400
	// that could restart-loop the service on infra whose peer address or
	// header shape the limiter's client-IP trust boundary doesn't recognize.
	// BodyLimit still applies because an oversized body is a resource risk.
	// It uses HealthBodyLimitBytes, not opts.BodyLimitBytes: a
	// caller that can never be turned away by RateLimit must not also get
	// the full request-body budget to buffer into memory on every request.
	// NoStoreCache wraps this whole chain outside BodyLimit, so a 413 rejection
	// carries the documented cache policy. /healthz and
	// /readyz are operational endpoints, never product data. No intermediary
	// may serve a stale result instead of checking live.
	healthChain := NoStoreCache()(BodyLimit(HealthBodyLimitBytes)(mux))

	// Ordinary routes: NoStoreCache (outermost — see below), then
	// RateLimit (so an over-limit client is rejected before the server
	// spends any I/O reading its body), then BodyLimit, then the mux.
	// Exact /mcp and photo-upload routes use routeOwnedBodyChain because their
	// handlers enforce distinct 4 MiB and streaming multipart limits. Escaped
	// and other near matches remain on the ordinary bounded chain.
	//
	// NoStoreCache is the DEFAULT cache policy here, not an incidental
	// side effect of where it happens to sit: it is outermost specifically
	// so every rejection this chain can produce — RateLimit's 429 and its
	// own 400 invalid_client_ip, BodyLimit's 413, the mux's 404/405 — all
	// carry Cache-Control: no-store, no-transform, not just a successful
	// response. A
	// public route group can substitute a different policy (for example,
	// PublicJSONCache for public JSON) by wrapping just that group's
	// handler INSIDE the mux: that inner middleware's Cache-Control write
	// happens after this outer one in the call chain and so overrides it
	// for that group specifically, the same pattern NoStoreCache itself
	// already uses to survive a downstream rejection.
	otherChain := NoStoreCache()(
		RateLimit(RateLimiterConfig{TrustedProxies: opts.TrustedProxies, Clock: opts.Clock})(
			BodyLimit(opts.BodyLimitBytes)(mux)))
	routeOwnedBodyChain := NoStoreCache()(
		RateLimit(RateLimiterConfig{TrustedProxies: opts.TrustedProxies, Clock: opts.Clock})(mux))

	// Middleware order (outer -> inner): RequestID, SecurityHeaders,
	// Logging, then the path-based dispatch above.
	//
	//   - RequestID stays outermost: every later layer,
	//     including a rejection, needs the ID already in the response and
	//     in context before it runs.
	//   - SecurityHeaders comes next, and specifically outside every layer
	//     that can itself write a response (RateLimit's 429, BodyLimit's
	//     413, the mux's 404/405). It only sets headers before calling
	//     next, so wrapping those rejection points — not just the
	//     success path — is what makes "every response carries these
	//     headers" actually true.
	//   - Logging wraps both chains so a 429, 413, or health-check result
	//     shows up in the request log with its real status and request
	//     ID, the same way 404/405 already do.
	//
	// Both RateLimit and SecurityHeaders are wired with opts.TrustedProxies
	// (see Options.TrustedProxies) rather than a value this package
	// invents: the caller (cmd/server/main.go) is the one place that knows
	// the deployment's actual topology, sourced from
	// internal/config.Config.TrustedProxyCIDRs.
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isHealthPath(r.URL.EscapedPath()) {
			healthChain.ServeHTTP(w, r)
			return
		}
		if public != nil && public.Recognizes(r.URL.EscapedPath()) {
			public.ServeHTTP(w, r)
			return
		}
		if isPhotoUploadPath(r.Method, r.URL.EscapedPath()) || r.URL.EscapedPath() == "/mcp" {
			routeOwnedBodyChain.ServeHTTP(w, r)
			return
		}
		otherChain.ServeHTTP(w, r)
	})
	handler = Logging(logger)(handler)
	handler = SecurityHeaders(opts.TrustedProxies)(handler)
	handler = RequestID(handler)
	return handler
}

// isHealthPath reports whether escapedPath — r.URL.EscapedPath(), NOT
// r.URL.Path — is one of the two operational health endpoints New
// registers, which — see the healthChain comment above — get their own
// middleware treatment distinct from every other route, most importantly
// exemption from RateLimit. EscapedPath() preserves percent-encoding (Path
// is already decoded by net/http), so a request to e.g. "/%68ealthz" —
// which net/http's own ServeMux still resolves to the registered
// "/healthz" handler, since mux matching happens on the decoded path —
// compares unequal here and falls through to the rate-limited chain
// instead of silently taking the RateLimit-exempt branch.
func isHealthPath(escapedPath string) bool {
	return escapedPath == "/healthz" || escapedPath == "/readyz"
}

// isPhotoUploadPath identifies the one streaming route that must bypass the
// ordinary buffering BodyLimit. It accepts only the canonical lowercase UUID
// spelling and an unescaped exact path, so malformed and near-match requests
// stay on the bounded JSON chain.
func isPhotoUploadPath(method, escapedPath string) bool {
	if method != http.MethodPost || strings.Contains(escapedPath, "%") {
		return false
	}
	parts := strings.Split(escapedPath, "/")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "api" || parts[2] != "v1" ||
		parts[3] != "resumes" || parts[5] != "photo" {
		return false
	}
	id, err := uuid.Parse(parts[4])
	return err == nil && id.String() == parts[4]
}

// route restricts handler to the given HTTP method, responding 405 Method
// Not Allowed with the standard error envelope (and an Allow header) for
// any other method on the same registered path. A HEAD request satisfies a
// GET route: net/http's real server already suppresses the response body
// it would otherwise send for HEAD (see (*http.response) body handling),
// matching http.ServeMux's own treatment of "GET /pattern" registrations
// and RFC 9110 §9.3.2 (HEAD is identical to GET but without a body), so the
// GET handler runs unchanged and no extra body-suppression logic is needed
// here.
func route(method string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !methodMatches(method, r.Method) {
			w.Header().Set("Allow", method)
			WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
				fmt.Sprintf("method not allowed on %s; use %s", r.URL.Path, method))
			return
		}
		handler(w, r)
	}
}

// methodMatches reports whether requestMethod satisfies a route registered
// for routeMethod. Every method matches itself; additionally, HEAD matches
// a GET route, since HEAD is defined as a bodyless GET (RFC 9110 §9.3.2).
func methodMatches(routeMethod, requestMethod string) bool {
	if requestMethod == routeMethod {
		return true
	}
	return routeMethod == http.MethodGet && requestMethod == http.MethodHead
}

// NotFound handles any request that doesn't match a registered route.
func NotFound() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, http.StatusNotFound, "not_found",
			fmt.Sprintf("no route for %s %s", r.Method, r.URL.Path))
	}
}
