package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
}

func TestRouter_RegisterExtraRoutes_GetsStandardMiddlewareChain(t *testing.T) {
	t.Parallel()

	register := func(mux *http.ServeMux) {
		mux.Handle("/probe", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		}))
	}
	handler := api.New(testLogger(), fakePinger{}, api.Options{}, nil, register)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/probe", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d (the registered handler)", rec.Code, http.StatusTeapot)
	}
	if rec.Header().Get(api.RequestIDHeader) == "" {
		t.Error("response missing X-Request-Id header; the registered route must go through the standard middleware chain")
	}
}

type publicRoutesStub struct {
	recognizes func(string) bool
	handler    http.Handler
}

func (s publicRoutesStub) Recognizes(path string) bool { return s.recognizes(path) }
func (s publicRoutesStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

type panicBody struct{}

func (panicBody) Read([]byte) (int, error) {
	panic("default body middleware read a public wrong-method body")
}

func TestPublicDispatchWrongMethodPrecedesDefaultBodyAndRateChain(t *testing.T) {
	t.Parallel()

	public := publicRoutesStub{
		recognizes: func(path string) bool {
			for _, publicPath := range []string{
				"/api/v1/public/resumes/ada-lovelace",
				"/api/v1/public/resumes/ada-lovelace/photo",
				"/ada-lovelace",
				"/ada-lovelace.md",
				"/sitemap.xml",
				"/robots.txt",
				"/llms.txt",
			} {
				if path == publicPath {
					return true
				}
			}
			return false
		},
		handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.WriteHeader(http.StatusOK)
		}),
	}
	handler := api.New(testLogger(), fakePinger{}, api.Options{BodyLimitBytes: 1}, public)
	for _, path := range []string{"/api/v1/public/resumes/ada-lovelace", "/api/v1/public/resumes/ada-lovelace/photo", "/ada-lovelace", "/ada-lovelace.md", "/sitemap.xml", "/robots.txt", "/llms.txt"} {
		for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch} {
			req := httptest.NewRequestWithContext(context.Background(), method, path, panicBody{})
			req.ContentLength = 1 << 20
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" {
				t.Errorf("%s %s response = %d Allow=%q, want 405 GET, HEAD", method, path, rec.Code, rec.Header().Get("Allow"))
			}
		}
	}
}

func TestRouter_RegisterExtraRoutes_MultipleFuncsAllApply(t *testing.T) {
	t.Parallel()

	first := func(mux *http.ServeMux) {
		mux.Handle("/probe-a", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
	}
	second := func(mux *http.ServeMux) {
		mux.Handle("/probe-b", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		}))
	}
	handler := api.New(testLogger(), fakePinger{}, api.Options{}, nil, first, second)

	for path, want := range map[string]int{"/probe-a": http.StatusOK, "/probe-b": http.StatusAccepted} {
		req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("%s status = %d, want %d", path, rec.Code, want)
		}
	}
}

func TestRouter_Healthz_ReturnsOK(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{}, nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Header().Get(api.RequestIDHeader) == "" {
		t.Error("response missing X-Request-Id header")
	}
}

func TestRouter_Readyz_ReturnsOKWhenDBReachable(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{}, nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRouter_Readyz_Returns503AgainstRealUnreachableStore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool, err := store.NewPool(ctx, "postgres://user:pass@127.0.0.1:1/aboutme?connect_timeout=1")
	if err != nil {
		t.Fatalf("store.NewPool() unexpected error: %v", err)
	}
	defer pool.Close(ctx)

	handler := api.New(testLogger(), pool, api.Options{ReadyTimeout: 500 * time.Millisecond}, nil)

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
		t.Fatalf("unmarshal response body: %v (body=%s)", err, rec.Body.String())
	}
	if body.Error.Code == "" {
		t.Error("error.code is empty, want a non-empty code")
	}
}

func TestRouter_Healthz_IgnoresDBOutage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool, err := store.NewPool(ctx, "postgres://user:pass@127.0.0.1:1/aboutme?connect_timeout=1")
	if err != nil {
		t.Fatalf("store.NewPool() unexpected error: %v", err)
	}
	defer pool.Close(ctx)

	handler := api.New(testLogger(), pool, api.Options{}, nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; liveness must survive a DB outage", rec.Code, http.StatusOK)
	}
}

func TestRouter_BodyLimitAppliesToAllRoutes(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{BodyLimitBytes: 10}, nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/anything", bytes.NewReader(bytes.Repeat([]byte("a"), 20)))
	req.ContentLength = 20
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestRouter_PhotoUploadAloneBypassesBufferingBodyLimit(t *testing.T) {
	t.Parallel()

	register := func(mux *http.ServeMux) {
		mux.Handle("/api/v1/resumes/{id}/photo", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
	}
	handler := api.New(testLogger(), fakePinger{}, api.Options{BodyLimitBytes: 10}, nil, register)
	canonicalID := "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f"

	for _, tc := range []struct {
		name       string
		method     string
		path       string
		wantStatus int
	}{
		{"exact POST", http.MethodPost, "/api/v1/resumes/" + canonicalID + "/photo", http.StatusNoContent},
		{"wrong method", http.MethodPatch, "/api/v1/resumes/" + canonicalID + "/photo", http.StatusRequestEntityTooLarge},
		{"noncanonical UUID", http.MethodPost, "/api/v1/resumes/" + strings.ToUpper(canonicalID) + "/photo", http.StatusRequestEntityTooLarge},
		{"missing id", http.MethodPost, "/api/v1/resumes//photo", http.StatusRequestEntityTooLarge},
		{"near suffix", http.MethodPost, "/api/v1/resumes/" + canonicalID + "/photos", http.StatusRequestEntityTooLarge},
		{"non-photo path", http.MethodPost, "/api/v1/resumes/" + canonicalID, http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequestWithContext(context.Background(), tc.method, tc.path, bytes.NewReader(bytes.Repeat([]byte("a"), 20)))
			req.ContentLength = 20
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}

func TestRouter_MCPAloneBypassesDefaultBodyLimit(t *testing.T) {
	t.Parallel()

	register := func(mux *http.ServeMux) {
		mux.Handle("/mcp", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
	}
	handler := api.New(testLogger(), fakePinger{}, api.Options{BodyLimitBytes: 10}, nil, register)

	for _, tc := range []struct {
		name, path string
		want       int
	}{
		{"exact MCP", "/mcp", http.StatusNoContent},
		{"subpath", "/mcp/other", http.StatusRequestEntityTooLarge},
		{"escaped root", "/%6dcp", http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, tc.path, bytes.NewReader(bytes.Repeat([]byte("a"), 20)))
			req.ContentLength = 20
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != tc.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.want)
			}
		})
	}
}

func decodeErrorEnvelope(t *testing.T, rec *httptest.ResponseRecorder) struct {
	Code    string
	Message string
} {
	t.Helper()

	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json (body=%s)", ct, rec.Body.String())
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response body as error envelope: %v (body=%s)", err, rec.Body.String())
	}
	if body.Error.Code == "" {
		t.Errorf("error.code is empty (body=%s)", rec.Body.String())
	}
	if body.Error.Message == "" {
		t.Errorf("error.message is empty (body=%s)", rec.Body.String())
	}
	return struct {
		Code    string
		Message string
	}{body.Error.Code, body.Error.Message}
}

func TestRouter_UnknownRoute_Returns404WithErrorEnvelope(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{}, nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/does-not-exist", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	decodeErrorEnvelope(t, rec)
}

func TestRouter_UnknownAPIRoute_Returns404WithErrorEnvelope(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{}, nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/does-not-exist", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	got := decodeErrorEnvelope(t, rec)
	if got.Code != "not_found" {
		t.Errorf("error.code = %q, want %q", got.Code, "not_found")
	}
}

func TestRouter_WrongMethodOnExistingRoute_Returns405WithErrorEnvelope(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{}, nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	got := decodeErrorEnvelope(t, rec)
	if got.Code != "method_not_allowed" {
		t.Errorf("error.code = %q, want %q", got.Code, "method_not_allowed")
	}
	if allow := rec.Header().Get("Allow"); allow != http.MethodGet {
		t.Errorf("Allow header = %q, want %q", allow, http.MethodGet)
	}
}

func TestRouter_Healthz_HeadRequest_ReturnsOKWithEmptyBody(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{}, nil)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodHead, srv.URL+"/healthz", nil)
	if err != nil {
		t.Fatalf("build HEAD request: %v", err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("HEAD /healthz: unexpected error: %v", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			t.Errorf("close response body: %v", cerr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("body = %q, want empty", body)
	}
}

func TestRouter_Readyz_HeadRequest_MatchesGetStatus(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{}, nil)

	getReq := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)

	headReq := httptest.NewRequestWithContext(context.Background(), http.MethodHead, "/readyz", nil)
	headRec := httptest.NewRecorder()
	handler.ServeHTTP(headRec, headReq)

	if headRec.Code != getRec.Code {
		t.Fatalf("HEAD status = %d, want same as GET status %d", headRec.Code, getRec.Code)
	}
	if headRec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", headRec.Code, http.StatusOK)
	}
}

func TestRouter_Healthz_PostStillReturns405(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{}, nil)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	got := decodeErrorEnvelope(t, rec)
	if got.Code != "method_not_allowed" {
		t.Errorf("error.code = %q, want %q", got.Code, "method_not_allowed")
	}
}

func TestRouter_PercentEncodedHealthPath_DoesNotBypassRateLimit(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/%68ealthz", "/%72eadyz"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			handler := api.New(testLogger(), fakePinger{}, api.Options{}, nil)

			var sawTooManyRequests bool
			for i := 0; i < api.DefaultRateLimitRequests+50; i++ {
				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				switch rec.Code {
				case http.StatusOK:
				case http.StatusTooManyRequests:
					sawTooManyRequests = true
				default:
					t.Fatalf("request %d to %s: status = %d, want %d or %d",
						i, path, rec.Code, http.StatusOK, http.StatusTooManyRequests)
				}
			}
			if !sawTooManyRequests {
				t.Errorf("flooding %s (a percent-encoded health path) never returned 429; "+
					"it must not take the RateLimit-exempt branch", path)
			}
		})
	}
}

func TestRouter_HealthEndpoints_BodyCapIndependentOfOptions(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{BodyLimitBytes: 10 * 1024 * 1024}, nil)

	body := bytes.Repeat([]byte("a"), int(api.HealthBodyLimitBytes)+1)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d (a 10 MiB Options.BodyLimitBytes must not apply to /healthz)",
			rec.Code, http.StatusRequestEntityTooLarge)
	}
}

type countingReader struct {
	r    io.Reader
	mu   sync.Mutex
	read int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.mu.Lock()
	c.read += int64(n)
	c.mu.Unlock()
	return n, err
}

func (c *countingReader) bytesRead() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.read
}

func TestRouter_HealthEndpoint_BoundedBodyRead(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{}, nil)

	const oversizedBody = 256 * 1024
	cr := &countingReader{r: bytes.NewReader(bytes.Repeat([]byte("a"), oversizedBody))}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", io.NopCloser(cr))
	req.ContentLength = -1

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	if got := cr.bytesRead(); got > api.HealthBodyLimitBytes+1 {
		t.Errorf("bytes actually read = %d, want at most %d (HealthBodyLimitBytes+1); "+
			"a 256 KB body must never be read in full just to reject it", got, api.HealthBodyLimitBytes+1)
	}
}

type countingPinger struct {
	err error

	mu    sync.Mutex
	calls int
}

func (p *countingPinger) Ping(ctx context.Context) error {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return p.err
}

func (p *countingPinger) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

type blockingCountingPinger struct {
	release <-chan struct{}
	err     error

	mu    sync.Mutex
	calls int
}

func (p *blockingCountingPinger) Ping(ctx context.Context) error {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()

	select {
	case <-p.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return p.err
}

func (p *blockingCountingPinger) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func TestRouter_Readyz_ConcurrentFloodPerformsExactlyOnePing(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	pinger := &blockingCountingPinger{release: release}
	clock := testutil.NewClockAtEpoch()

	handler := api.New(testLogger(), pinger, api.Options{
		Clock:        clock.Now,
		ReadyTimeout: 5 * time.Second,
	}, nil)

	const n = 25
	var wg sync.WaitGroup
	codes := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			codes[i] = rec.Code
		}(i)
	}

	for pinger.callCount() == 0 {
		runtime.Gosched()
	}
	close(release)
	wg.Wait()

	if got := pinger.callCount(); got != 1 {
		t.Errorf("real Ping() calls = %d, want exactly 1 for %d concurrent /readyz requests", got, n)
	}
	for i, code := range codes {
		if code != http.StatusOK {
			t.Errorf("request %d: status = %d, want %d", i, code, http.StatusOK)
		}
	}
}

func TestRouter_Readyz_CachesResultUntilTTLThenPingsAgain(t *testing.T) {
	t.Parallel()

	pinger := &countingPinger{}
	clock := testutil.NewClockAtEpoch()

	handler := api.New(testLogger(), pinger, api.Options{
		Clock:    clock.Now,
		ReadyTTL: time.Second,
	}, nil)
	req := func() *http.Request {
		return httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil)
	}

	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req())
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d (within TTL): status = %d, want %d", i, rec.Code, http.StatusOK)
		}
	}
	if got := pinger.callCount(); got != 1 {
		t.Fatalf("within TTL: real Ping() calls = %d across 5 requests, want 1", got)
	}

	clock.Advance(time.Second)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	if rec.Code != http.StatusOK {
		t.Fatalf("request after TTL expiry: status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := pinger.callCount(); got != 2 {
		t.Errorf("after TTL expiry: real Ping() calls = %d, want 2 (a fresh ping)", got)
	}
}

func TestRouter_Readyz_FailingPingIsAlsoCachedForTTL(t *testing.T) {
	t.Parallel()

	pinger := &countingPinger{err: errors.New("db unreachable")}
	clock := testutil.NewClockAtEpoch()

	handler := api.New(testLogger(), pinger, api.Options{
		Clock:    clock.Now,
		ReadyTTL: time.Second,
	}, nil)
	req := func() *http.Request {
		return httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil)
	}

	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req())
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("request %d: status = %d, want %d", i, rec.Code, http.StatusServiceUnavailable)
		}
	}
	if got := pinger.callCount(); got != 1 {
		t.Errorf("a failing ping must also be cached for the TTL (no thundering herd on a down DB): "+
			"real Ping() calls = %d across 5 requests, want 1", got)
	}

	clock.Advance(time.Second)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("request after TTL expiry: status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if got := pinger.callCount(); got != 2 {
		t.Errorf("after TTL expiry: real Ping() calls = %d, want 2 (recovery must be checked again)", got)
	}
}
