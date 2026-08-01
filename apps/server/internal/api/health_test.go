package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/api"
)

func TestHealthz_AlwaysReturns200AndNeverTouchesDB(t *testing.T) {
	t.Parallel()

	// Healthz takes no dependencies at all — there is no pinger, database
	// handle, or other collaborator for it to call. That is what
	// guarantees liveness never touches the database: the handler simply
	// has nothing to touch it with.
	handler := api.Healthz()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.Data.Status != "ok" {
		t.Errorf("data.status = %q, want %q", body.Data.Status, "ok")
	}
}

type fakePinger struct {
	err   error
	delay time.Duration
}

func (f fakePinger) Ping(ctx context.Context) error {
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return f.err
}

func TestReadyz_Returns200WhenDatabaseIsReachable(t *testing.T) {
	t.Parallel()

	handler := api.Readyz(fakePinger{}, time.Second)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body struct {
		Data struct {
			Status string `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.Data.Status != "ready" {
		t.Errorf("data.status = %q, want %q", body.Data.Status, "ready")
	}
}

func TestReadyz_Returns503WithErrorEnvelopeWhenDatabaseUnreachable(t *testing.T) {
	t.Parallel()

	handler := api.Readyz(fakePinger{err: errors.New("connection refused")}, time.Second)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body: %v", err)
	}
	if body.Error.Code == "" {
		t.Error("error.code is empty, want a non-empty code")
	}
}

func TestReadyz_Returns503WhenPingExceedsTimeout(t *testing.T) {
	t.Parallel()

	handler := api.Readyz(fakePinger{delay: 200 * time.Millisecond}, 20*time.Millisecond)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	start := time.Now()
	handler.ServeHTTP(rec, req)
	elapsed := time.Since(start)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if elapsed > 150*time.Millisecond {
		t.Errorf("handler took %s, want it to respect the short timeout (~20ms)", elapsed)
	}
}
