package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultBodyLimitBytes int64 = 256 * 1024
	HealthBodyLimitBytes  int64 = 4 * 1024
	DefaultReadyTimeout         = 3 * time.Second
	DefaultReadyTTL             = time.Second
)

type Options struct {
	BodyLimitBytes int64
	ReadyTimeout   time.Duration
	ReadyTTL       time.Duration
	TrustedProxies TrustedProxies
	Clock          func() time.Time
}

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

func New(logger *slog.Logger, pinger DBPinger, opts Options, public PublicRoutes, register ...func(*http.ServeMux)) http.Handler {
	opts = opts.withDefaults()

	readyPinger := newCachedPinger(pinger, opts.ReadyTimeout, opts.ReadyTTL, opts.Clock)

	mux := http.NewServeMux()
	mux.Handle("/healthz", route(http.MethodGet, Healthz()))
	mux.Handle("/readyz", route(http.MethodGet, Readyz(readyPinger, opts.ReadyTimeout)))
	for _, r := range register {
		r(mux)
	}
	// net/http's ServeMux picks the most specific pattern, not the first registered, so "/" cannot shadow routes added above.
	mux.Handle("/", NotFound())

	healthChain := NoStoreCache()(BodyLimit(HealthBodyLimitBytes)(mux))

	otherChain := NoStoreCache()(
		RateLimit(RateLimiterConfig{TrustedProxies: opts.TrustedProxies, Clock: opts.Clock})(
			BodyLimit(opts.BodyLimitBytes)(mux)))
	routeOwnedBodyChain := NoStoreCache()(
		RateLimit(RateLimiterConfig{TrustedProxies: opts.TrustedProxies, Clock: opts.Clock})(mux))

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

// net/http's ServeMux matches the decoded path, so "/%68ealthz" reaches the /healthz handler; compare the escaped path.
func isHealthPath(escapedPath string) bool {
	return escapedPath == "/healthz" || escapedPath == "/readyz"
}

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

// net/http's server sends no body for HEAD, and RFC 9110 section 9.3.2 defines HEAD as GET without a body.
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

func methodMatches(routeMethod, requestMethod string) bool {
	if requestMethod == routeMethod {
		return true
	}
	return routeMethod == http.MethodGet && requestMethod == http.MethodHead
}

func NotFound() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteError(w, http.StatusNotFound, "not_found",
			fmt.Sprintf("no route for %s %s", r.Method, r.URL.Path))
	}
}
