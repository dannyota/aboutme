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

type Middleware func(http.Handler) http.Handler

const RequestIDHeader = "X-Request-Id"

type contextKey int

const requestIDContextKey contextKey = iota

func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set(RequestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDContextKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequestIDFromContext(ctx context.Context) string {
	id, ok := ctx.Value(requestIDContextKey).(string)
	if !ok {
		return ""
	}
	return id
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

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

type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (sw *statusWriter) WriteHeader(status int) {
	// net/http keeps the first final (non-1xx) WriteHeader status and logs later calls as superfluous (pkg.go.dev/net/http#ResponseWriter).
	if !sw.wroteHeader {
		sw.status = status
		sw.wroteHeader = true
	}
	sw.ResponseWriter.WriteHeader(status)
}

// http.ResponseController reaches Flusher and deadline support through Unwrap; a w.(http.Flusher) assertion does not.
func (sw *statusWriter) Unwrap() http.ResponseWriter {
	return sw.ResponseWriter
}

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
