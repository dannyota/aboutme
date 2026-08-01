package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/api"
)

func TestRequestID_SetsResponseHeaderAndContext(t *testing.T) {
	t.Parallel()

	var gotFromContext string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFromContext = api.RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	api.RequestID(next).ServeHTTP(rec, req)

	header := rec.Header().Get(api.RequestIDHeader)
	if header == "" {
		t.Fatal("response header X-Request-Id is empty, want a generated ID")
	}
	if gotFromContext != header {
		t.Errorf("context request ID = %q, want it to match response header %q", gotFromContext, header)
	}
}

func TestRequestID_GeneratesDistinctIDsPerRequest(t *testing.T) {
	t.Parallel()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := api.RequestID(next)

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))

	id1 := rec1.Header().Get(api.RequestIDHeader)
	id2 := rec2.Header().Get(api.RequestIDHeader)
	if id1 == "" || id2 == "" {
		t.Fatalf("expected non-empty request IDs, got %q and %q", id1, id2)
	}
	if id1 == id2 {
		t.Errorf("expected distinct request IDs, both were %q", id1)
	}
}

func TestLogging_EmitsStructuredRequestLog(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	handler := api.RequestID(api.Logging(logger)(next))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/widgets", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal log line: %v (line=%s)", err, buf.String())
	}

	if entry["method"] != http.MethodGet {
		t.Errorf("log method = %v, want %v", entry["method"], http.MethodGet)
	}
	if entry["path"] != "/widgets" {
		t.Errorf("log path = %v, want /widgets", entry["path"])
	}
	if status, ok := entry["status"].(float64); !ok || int(status) != http.StatusTeapot {
		t.Errorf("log status = %v, want %d", entry["status"], http.StatusTeapot)
	}
	if _, ok := entry["request_id"]; !ok {
		t.Error("log entry missing request_id field")
	}
	if _, ok := entry["duration_ms"]; !ok {
		t.Error("log entry missing duration_ms field")
	}
}

// TestLogging_RecordsOnlyFirstWriteHeaderCall guards against the logged
// status diverging from what was actually sent on the wire: net/http only
// honors the first WriteHeader call (later ones are superfluous no-ops), so
// the wrapper must not let a later call overwrite the recorded status.
func TestLogging_RecordsOnlyFirstWriteHeaderCall(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted) // first call: this is what is sent
		w.WriteHeader(http.StatusTeapot)   // superfluous: must not change the log
	})
	handler := api.RequestID(api.Logging(logger)(next))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal log line: %v (line=%s)", err, buf.String())
	}
	status, ok := entry["status"].(float64)
	if !ok || int(status) != http.StatusAccepted {
		t.Errorf("logged status = %v, want %d (the first WriteHeader call)", entry["status"], http.StatusAccepted)
	}
}

// TestLogging_PreservesFlushThroughResponseController proves the Logging
// wrapper doesn't break streaming responses: composed the same way api.New
// wires the middleware chain (RequestID -> Logging -> BodyLimit -> handler),
// a handler using http.ResponseController to flush must actually reach the
// underlying ResponseWriter. This matters for SSE endpoints (spec §2, §8),
// which flush events through this exact chain.
func TestLogging_PreservesFlushThroughResponseController(t *testing.T) {
	t.Parallel()

	flushed := make(chan struct{})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("event: ping\n\n")); err != nil {
			t.Errorf("Write: %v", err)
		}
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("Flush() via http.ResponseController: %v", err)
			return
		}
		close(flushed)
	})

	handler := api.RequestID(api.Logging(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))(
		api.BodyLimit(api.DefaultBodyLimitBytes)(next)))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	select {
	case <-flushed:
	default:
		t.Fatal("handler never reached the flush call")
	}
	if !rec.Flushed {
		t.Error("underlying ResponseRecorder.Flushed = false, want true — " +
			"Flush() did not reach it through the middleware chain")
	}
}

// TestLogging_NakedFlusherAssertionFailsDocumentingWhyResponseControllerIsRequired
// documents, rather than merely asserting, why handlers must use
// http.ResponseController instead of a direct `w.(http.Flusher)` type
// assertion: the Logging wrapper only exposes streaming capabilities via
// Unwrap(), which naked type assertions do not traverse.
func TestLogging_NakedFlusherAssertionFailsDocumentingWhyResponseControllerIsRequired(t *testing.T) {
	t.Parallel()

	var sawFlusher bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawFlusher = w.(http.Flusher)
	})
	handler := api.Logging(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)))(next)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil))

	if sawFlusher {
		t.Fatal("naked w.(http.Flusher) unexpectedly succeeded; " +
			"if this starts passing, http.ResponseController is no longer required and this test (and its comment) should be removed")
	}
}

func TestBodyLimit_AcceptsBodyAtLimit(t *testing.T) {
	t.Parallel()

	const limit = 256 * 1024
	body := bytes.Repeat([]byte("a"), limit)

	var gotLen int
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := new(bytes.Buffer)
		if _, err := b.ReadFrom(r.Body); err != nil {
			t.Errorf("read request body: %v", err)
		}
		gotLen = b.Len()
		w.WriteHeader(http.StatusOK)
	})
	handler := api.BodyLimit(limit)(next)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (body exactly at limit)", rec.Code, http.StatusOK)
	}
	if gotLen != limit {
		t.Errorf("handler received %d bytes, want %d", gotLen, limit)
	}
}

func TestBodyLimit_RejectsBodyOverLimit(t *testing.T) {
	t.Parallel()

	const limit = 256 * 1024
	const overLimit = limit + 1024 // 257 KB, per plan requirement

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("downstream handler must not run when body exceeds the limit")
	})
	handler := api.BodyLimit(limit)(next)

	body := bytes.Repeat([]byte("a"), overLimit)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}

	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal response body: %v (body=%s)", err, rec.Body.String())
	}
	if envelope.Error.Code == "" {
		t.Error("error.code is empty, want a non-empty error code")
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
		t.Errorf("Content-Type = %q, want application/json", rec.Header().Get("Content-Type"))
	}
}

func TestBodyLimit_RejectsByContentLengthWithoutReadingBody(t *testing.T) {
	t.Parallel()

	const limit = 256 * 1024
	const overLimit = limit + 1024

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("downstream handler must not run when Content-Length exceeds the limit")
	})
	handler := api.BodyLimit(limit)(next)

	// Content-Length lies about the body being large; a fast rejection path
	// should not need to read overLimit bytes to reject the request.
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", bytes.NewReader(nil))
	req.ContentLength = overLimit
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

// TestBodyLimit_RejectsStreamingBodyWithUnknownContentLength exercises the
// streaming read path (io.ReadAll + http.MaxBytesReader), not just the fast
// Content-Length check. ContentLength = -1 simulates a request with no (or
// a chunked, length-unknown) Content-Length header, which is exactly the
// case the fast path can't handle and must fall through to actually
// reading the body.
func TestBodyLimit_RejectsStreamingBodyWithUnknownContentLength(t *testing.T) {
	t.Parallel()

	const limit = 256 * 1024
	const overLimit = limit + 1024 // 257 KiB

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("downstream handler must not run when the streamed body exceeds the limit")
	})
	handler := api.BodyLimit(limit)(next)

	body := bytes.Repeat([]byte("a"), overLimit)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", bytes.NewReader(body))
	req.ContentLength = -1 // unknown length: forces the streaming read path
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

// TestBodyLimit_RejectsStreamingBodyWithUnderstatedContentLength covers a
// client that understates Content-Length (accidentally or maliciously): the
// declared length passes the fast Content-Length check, but the actual
// stream is oversized. Enforcement must come from what is actually read,
// not from trusting the declared header.
func TestBodyLimit_RejectsStreamingBodyWithUnderstatedContentLength(t *testing.T) {
	t.Parallel()

	const limit = 256 * 1024
	const overLimit = limit + 1024 // 257 KiB actually sent

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("downstream handler must not run when the streamed body exceeds the limit")
	})
	handler := api.BodyLimit(limit)(next)

	body := bytes.Repeat([]byte("a"), overLimit)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", bytes.NewReader(body))
	req.ContentLength = 10 // understated: far smaller than what's actually sent
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}
