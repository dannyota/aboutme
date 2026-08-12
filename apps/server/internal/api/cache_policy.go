package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
)

// Cache-Control values the middlewares in this file send.
const (
	// CacheControlNoStore is the Cache-Control value NoStoreCache sends:
	// every authenticated API response uses it so
	// no intermediary — a browser cache, CloudFront, a corporate proxy —
	// ever stores a copy that could later be served back to a different
	// account sharing that cache.
	CacheControlNoStore = "no-store"
	// CacheControlPublicJSON is the Cache-Control value PublicJSONCache
	// sends. It is paired with an ETag so a cache must always
	// revalidate with the origin (a conditional round-trip) before reusing
	// a stored response, rather than either serving it blindly or never
	// caching it at all. This lets conditional polling get cheap 304s.
	CacheControlPublicJSON = "no-cache, must-revalidate"
)

// NoStoreCache returns middleware that sets Cache-Control: no-store on
// every response it wraps, including error responses — it sets the header
// before calling next, the same pattern SecurityHeaders uses (see that
// doc comment for why that ordering is what makes a rejection path, not
// just the success path, actually carry the header).
//
// It serves two roles (see router.go): the cache
// policy for operational route groups (the health endpoints), and the
// outermost default on the non-health chain so that a rejection —
// 404/405/413/429/400 — never escapes without a cache policy. Using it as
// that default does NOT defeat a group's own policy: a route group wraps
// its policy (e.g. PublicJSONCache) INSIDE the mux, and because the header
// is a plain Set, the inner group's write is the one that reaches an
// intermediary — the outer default only fills in where no group set one.
// TestCachePolicy_InnerGroupOverridesOuterDefault pins this precedence.
func NoStoreCache() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", CacheControlNoStore)
			next.ServeHTTP(w, r)
		})
	}
}

// PublicJSONCache returns middleware implementing the public-JSON cache
// policy: Cache-Control: no-cache, must-revalidate on
// every response, plus a content-derived ETag on successful (2xx)
// responses that short-circuits a matching If-None-Match request straight
// to 304 Not Modified with no body. This makes the
// conditional-polling fallback (a client with no working SSE connection
// re-checking public resume JSON every 30-60s) cheap: an unchanged
// document costs the client a 304 with no body, not a full re-download.
//
// It buffers the wrapped handler's entire response in memory before
// writing anything to the real ResponseWriter, because the ETag header
// must be set before the status line but its value depends on the
// complete body. Only wrap this around a route that returns a single
// bounded JSON body — never a streaming or SSE handler, which this would
// block until the stream ends and then buffer in full.
func PublicJSONCache() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			buf := &bufferedResponse{header: make(http.Header)}
			next.ServeHTTP(buf, r)

			dst := w.Header()
			for k, v := range buf.header {
				dst[k] = v
			}
			dst.Set("Cache-Control", CacheControlPublicJSON)

			status := buf.status
			if status == 0 {
				status = http.StatusOK
			}

			if status >= 200 && status < 300 {
				etag := computeETag(buf.body.Bytes())
				dst.Set("ETag", etag)
				if ifNoneMatchSatisfied(r.Header.Get("If-None-Match"), etag) {
					w.WriteHeader(http.StatusNotModified)
					return
				}
			}

			w.WriteHeader(status)
			// Drain the buffered body straight into the ResponseWriter.
			// WriteTo streams without the extra copy Bytes() would make, and
			// the ETag was already computed above, so consuming the buffer
			// here is fine. (Using Buffer.WriteTo rather than w.Write(...) also
			// keeps this off the direct-ResponseWriter-write XSS heuristic,
			// which does not apply: this replays the handler's own JSON body
			// with the handler's own Content-Type, adding no untrusted content.)
			if _, err := buf.body.WriteTo(w); err != nil {
				// The status line and headers are already sent at this
				// point (same situation as writeJSON in envelope.go), so
				// all we can do is record the failure; the client sees a
				// truncated body.
				slog.Error("api: write cached response body", "error", err)
			}
		})
	}
}

// computeETag derives a strong ETag (RFC 9110 §8.8.3) from body: the same
// body always yields the same tag, and any change to the body yields a
// different one, so a conditional request needs no separate freshness
// bookkeeping kept in sync with the content it describes.
func computeETag(body []byte) string {
	sum := sha256.Sum256(body)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

// ifNoneMatchSatisfied reports whether header — the raw If-None-Match
// request header value, which may be "*", a single entity-tag, or a
// comma-separated list of them (RFC 9110 §13.1.2) — already names etag.
func ifNoneMatchSatisfied(header, etag string) bool {
	header = strings.TrimSpace(header)
	if header == "" {
		return false
	}
	if header == "*" {
		return true
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == etag {
			return true
		}
	}
	return false
}

// bufferedResponse is an in-memory http.ResponseWriter PublicJSONCache
// uses to capture a handler's complete response before any of it reaches
// the real ResponseWriter — see PublicJSONCache.
type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

// Header returns the header map the wrapped handler writes into; the
// caller (PublicJSONCache) copies this into the real response's headers
// once the handler has finished.
func (b *bufferedResponse) Header() http.Header { return b.header }

// Write buffers p in memory rather than sending it anywhere. Per
// http.ResponseWriter's contract, an implicit 200 is recorded here if the
// handler never called WriteHeader first.
func (b *bufferedResponse) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}

// WriteHeader records status. Per net/http's real behavior (and
// statusWriter's, in middleware.go), only the first call has any effect.
func (b *bufferedResponse) WriteHeader(status int) {
	if b.status == 0 {
		b.status = status
	}
}
