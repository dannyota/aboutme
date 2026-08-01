package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Middleware wraps an http.Handler with additional behavior.
type Middleware func(http.Handler) http.Handler

// RequestIDHeader is the response header carrying the per-request ID.
const RequestIDHeader = "X-Request-Id"

type contextKey int

const requestIDContextKey contextKey = iota

// RequestID assigns every request a fresh, server-generated ID, exposes it
// on the response via RequestIDHeader, and stores it in the request context
// for downstream handlers and the Logging middleware. Any client-supplied
// X-Request-Id is ignored so a caller cannot inject arbitrary values into
// server logs.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set(RequestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDContextKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext returns the request ID stored by RequestID, or ""
// if none is present.
func RequestIDFromContext(ctx context.Context) string {
	id, ok := ctx.Value(requestIDContextKey).(string)
	if !ok {
		// No request ID in context (e.g. called outside the RequestID
		// middleware, such as in a unit test): return "" rather than panic.
		return ""
	}
	return id
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is exceptionally unlikely; fall back to a
		// timestamp so requests still get a (non-unique-guaranteed) ID
		// instead of an empty one.
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// Logging returns middleware that emits one structured log entry per
// request via logger, after the request completes.
func Logging(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(sw, r)

			logger.Info("http_request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", RequestIDFromContext(r.Context()),
			)
		})
	}
}

// statusWriter records the status code passed to WriteHeader so it can be
// logged after the handler returns. It implements Unwrap() http.ResponseWriter
// so http.ResponseController (used by streaming handlers, e.g. SSE, to
// Flush or SetWriteDeadline) can see through this wrapper to the underlying
// writer's real capabilities — a naked type assertion like
// w.(http.Flusher) cannot do this and will incorrectly report the
// capability as missing. See http.ResponseController's docs.
type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

// WriteHeader exists so Logging can capture the status code actually sent on
// the wire, by recording it here before delegating to the wrapped
// ResponseWriter. Together with Unwrap below, this makes statusWriter
// capability-preserving for streaming handlers (e.g. SSE, which need
// http.Flusher/http.ResponseController to see through the wrapper): any
// future change to this method must keep delegating to ResponseWriter rather
// than substituting a different writer, or that capability lookup breaks.
func (sw *statusWriter) WriteHeader(status int) {
	// net/http only honors the first WriteHeader call for a given request;
	// later calls are superfluous no-ops (logged as a warning by the
	// server, but the status actually sent on the wire never changes). Only
	// the first call may update what gets logged, so the log always
	// matches what the client received.
	if !sw.wroteHeader {
		sw.status = status
		sw.wroteHeader = true
	}
	sw.ResponseWriter.WriteHeader(status)
}

// Unwrap returns the wrapped ResponseWriter so http.ResponseController can
// reach its Flusher/Hijacker/etc. capabilities through this wrapper.
func (sw *statusWriter) Unwrap() http.ResponseWriter {
	return sw.ResponseWriter
}

// BodyLimit returns middleware that rejects requests whose body exceeds
// maxBytes with 413 Request Entity Too Large and the standard error
// envelope. Requests are rejected before the downstream handler runs,
// either immediately from a declared Content-Length or after reading up to
// maxBytes+1 bytes when Content-Length is absent or understated.
func BodyLimit(maxBytes int64) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > maxBytes {
				writeBodyTooLarge(w, maxBytes)
				return
			}

			limited := http.MaxBytesReader(w, r.Body, maxBytes)
			body, err := io.ReadAll(limited)
			if err != nil {
				var tooLarge *http.MaxBytesError
				if errors.As(err, &tooLarge) {
					writeBodyTooLarge(w, maxBytes)
					return
				}
				WriteError(w, http.StatusBadRequest, "bad_request", "failed to read request body")
				return
			}

			r.Body = io.NopCloser(bytes.NewReader(body))
			r.ContentLength = int64(len(body))
			next.ServeHTTP(w, r)
		})
	}
}

func writeBodyTooLarge(w http.ResponseWriter, maxBytes int64) {
	WriteError(w, http.StatusRequestEntityTooLarge, "body_too_large",
		fmt.Sprintf("request body exceeds the %d byte limit", maxBytes))
}
