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

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
}

// TestRouter_RegisterExtraRoutes_GetsStandardMiddlewareChain proves New's
// variadic register extension point. A caller-supplied func(*http.ServeMux)
// can attach routes New itself knows nothing about
// (internal/auth's Google/LinkedIn/GitHub start/callback handlers, wired
// from cmd/server/main.go — internal/auth imports internal/api, so the
// reverse import would cycle, hence this inversion). The registered route
// must actually respond AND go through the same middleware chain as
// /healthz and /readyz: X-Request-Id is set on the response only if
// RequestID wrapped it, so asserting that header here is what proves the
// extra route isn't served by some bypass path that skips the standard
// chain.
func TestRouter_RegisterExtraRoutes_GetsStandardMiddlewareChain(t *testing.T) {
	t.Parallel()

	register := func(mux *http.ServeMux) {
		mux.Handle("/probe", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		}))
	}
	handler := api.New(testLogger(), fakePinger{}, api.Options{}, register)

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/probe", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, want %d (the registered handler)", rec.Code, http.StatusTeapot)
	}
	if rec.Header().Get(api.RequestIDHeader) == "" {
		t.Error("response missing X-Request-Id header — the registered route must go through the standard middleware chain")
	}
}

// TestRouter_RegisterExtraRoutes_MultipleFuncsAllApply proves every
// register func New receives is actually applied, not just the first —
// main.go may pass more than one provider or service RegisterRoutes function.
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
	handler := api.New(testLogger(), fakePinger{}, api.Options{}, first, second)

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

	handler := api.New(testLogger(), fakePinger{}, api.Options{})

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

	handler := api.New(testLogger(), fakePinger{}, api.Options{})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestRouter_Readyz_Returns503AgainstRealUnreachableStore wires the router
// with a real store.Pool pointed at an unreachable address (nothing is
// listening on 127.0.0.1:1), reproducing an actual database outage without
// requiring a live Postgres instance in the test environment.
func TestRouter_Readyz_Returns503AgainstRealUnreachableStore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pool, err := store.NewPool(ctx, "postgres://user:pass@127.0.0.1:1/aboutme?connect_timeout=1")
	if err != nil {
		t.Fatalf("store.NewPool() unexpected error: %v", err)
	}
	defer pool.Close(ctx)

	handler := api.New(testLogger(), pool, api.Options{ReadyTimeout: 500 * time.Millisecond})

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

	handler := api.New(testLogger(), pool, api.Options{})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d — liveness must survive a DB outage", rec.Code, http.StatusOK)
	}
}

// TestRouter_BodyLimitAppliesToAllRoutes proves Options.BodyLimitBytes is
// honored on the non-health chain. It deliberately targets a non-health
// path: /healthz and /readyz use their own small, fixed
// api.HealthBodyLimitBytes cap instead. See
// TestRouter_HealthEndpoints_BodyCapIndependentOfOptions for that
// decoupling.
func TestRouter_BodyLimitAppliesToAllRoutes(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{BodyLimitBytes: 10})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/anything", bytes.NewReader(bytes.Repeat([]byte("a"), 20)))
	req.ContentLength = 20
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

// decodeErrorEnvelope unmarshals rec's body as {"error":{"code","message"}}
// and fails the test if it isn't shaped that way — every non-2xx response
// from this router must use the standard envelope, never a stdlib default
// plain-text body.
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

	handler := api.New(testLogger(), fakePinger{}, api.Options{})

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/does-not-exist", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
	decodeErrorEnvelope(t, rec)
}

// TestRouter_UnknownAPIRoute_Returns404WithErrorEnvelope proves an unknown path
// under the API's own /api/v1/* namespace still gets the JSON envelope, not the stdlib
// ServeMux's default plain-text 404.
func TestRouter_UnknownAPIRoute_Returns404WithErrorEnvelope(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{})

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

	handler := api.New(testLogger(), fakePinger{}, api.Options{})

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

// TestRouter_Healthz_HeadRequest_ReturnsOKWithEmptyBody verifies that HEAD
// satisfies the GET route registered for /healthz and that the response
// body is genuinely empty on the wire. This deliberately uses a real
// httptest.Server rather than the httptest.NewRecorder() pattern used
// elsewhere in this file: net/http's HEAD body suppression lives in the
// real server's connection-writing path, which httptest.ResponseRecorder —
// constructed with no request of its own — never exercises, so asserting
// an empty body against a Recorder would prove nothing. The route relies on
// transport-level suppression, so the test observes the real transport.
func TestRouter_Healthz_HeadRequest_ReturnsOKWithEmptyBody(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{})
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

// TestRouter_Readyz_HeadRequest_MatchesGetStatus checks that HEAD /readyz
// runs the same handler as GET /readyz and reports the same status under
// the same (healthy) pinger condition.
func TestRouter_Readyz_HeadRequest_MatchesGetStatus(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{})

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

// TestRouter_Healthz_PostStillReturns405 guards against the method-matching
// relaxation added to let HEAD satisfy a GET route accidentally loosening
// enforcement for genuinely disallowed methods.
func TestRouter_Healthz_PostStillReturns405(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{})

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

// TestRouter_PercentEncodedHealthPath_DoesNotBypassRateLimit proves
// isHealthPath compares the exact, unencoded canonical path. net/http's own
// ServeMux still resolves
// a percent-encoded variant of /healthz or /readyz (e.g. "/%68ealthz") to
// the same registered handler, since mux matching happens on the decoded
// path — but the health/non-health DISPATCH decision in router.go must not
// make the same mistake, or a percent-encoded health path would silently
// take the RateLimit-exempt branch.
func TestRouter_PercentEncodedHealthPath_DoesNotBypassRateLimit(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/%68ealthz", "/%72eadyz"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			handler := api.New(testLogger(), fakePinger{}, api.Options{})

			var sawTooManyRequests bool
			for i := 0; i < api.DefaultRateLimitRequests+50; i++ {
				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				switch rec.Code {
				case http.StatusOK:
					// Still under the (non-exempt) budget; ServeMux still
					// resolves this to the health handler by decoded path.
				case http.StatusTooManyRequests:
					sawTooManyRequests = true
				default:
					t.Fatalf("request %d to %s: status = %d, want %d or %d",
						i, path, rec.Code, http.StatusOK, http.StatusTooManyRequests)
				}
			}
			if !sawTooManyRequests {
				t.Errorf("flooding %s (a percent-encoded health path) never returned 429 — "+
					"it must not take the RateLimit-exempt branch", path)
			}
		})
	}
}

// TestRouter_HealthEndpoints_BodyCapIndependentOfOptions proves health
// endpoints reject an over-cap body using api.HealthBodyLimitBytes, never
// Options.BodyLimitBytes — even when the latter is configured far larger.
func TestRouter_HealthEndpoints_BodyCapIndependentOfOptions(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{BodyLimitBytes: 10 * 1024 * 1024})

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

// countingReader wraps r and records how many bytes have actually been
// read through it, so a test can assert an upper bound on real I/O rather
// than only on the final outcome.
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

// TestRouter_HealthEndpoint_BoundedBodyRead proves a 256 KB body to /healthz is
// rejected/aborted after at most api.HealthBodyLimitBytes+1 bytes are
// actually read into memory — not the whole 256 KB — regardless of
// Options.BodyLimitBytes. The request deliberately omits a declared
// Content-Length so BodyLimit cannot short-circuit on that alone and must
// actually bound the read via http.MaxBytesReader.
func TestRouter_HealthEndpoint_BoundedBodyRead(t *testing.T) {
	t.Parallel()

	handler := api.New(testLogger(), fakePinger{}, api.Options{})

	const oversizedBody = 256 * 1024
	cr := &countingReader{r: bytes.NewReader(bytes.Repeat([]byte("a"), oversizedBody))}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", io.NopCloser(cr))
	req.ContentLength = -1 // undeclared: forces the streaming (not fast-path) branch

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

// countingPinger is a non-blocking DBPinger that always returns err (nil
// for success) and counts how many times Ping actually ran, so a test can
// assert the readiness cache collapses repeated requests into a bounded
// number of real pings.
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

// blockingCountingPinger is a DBPinger whose Ping blocks until release is
// closed (or ctx is done), and counts how many times Ping actually started
// — used to prove the readiness cache's single-flight behavior under real
// concurrency: N callers arriving while a ping is in flight must collapse
// into exactly one real Ping call, not N.
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

// TestRouter_Readyz_ConcurrentFloodPerformsExactlyOnePing proves concurrent
// requests collapse into one real database ping while it is in flight. This
// proves the single-flight half of
// the readiness cache, not just its post-completion TTL reuse.
func TestRouter_Readyz_ConcurrentFloodPerformsExactlyOnePing(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	pinger := &blockingCountingPinger{release: release}
	clock := testutil.NewClockAtEpoch()

	handler := api.New(testLogger(), pinger, api.Options{
		Clock:        clock.Now,
		ReadyTimeout: 5 * time.Second,
	})

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

	// Wait until the one real ping has started (and is now blocked on
	// <-release) before releasing it: from this point on, every other
	// goroutine that reaches the cache is guaranteed to observe an
	// in-flight ping (inflight is set, under lock, strictly before Ping is
	// called), never a nil one — deterministic, not a timing guess.
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

// TestRouter_Readyz_CachesResultUntilTTLThenPingsAgain proves the TTL half of
// the readiness cache. Repeated requests within the TTL reuse the last result,
// and a request after TTL expiry performs a fresh one.
func TestRouter_Readyz_CachesResultUntilTTLThenPingsAgain(t *testing.T) {
	t.Parallel()

	pinger := &countingPinger{}
	clock := testutil.NewClockAtEpoch()

	handler := api.New(testLogger(), pinger, api.Options{
		Clock:    clock.Now,
		ReadyTTL: time.Second,
	})
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

	clock.Advance(time.Second) // exactly at the TTL boundary: now stale

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	if rec.Code != http.StatusOK {
		t.Fatalf("request after TTL expiry: status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := pinger.callCount(); got != 2 {
		t.Errorf("after TTL expiry: real Ping() calls = %d, want 2 (a fresh ping)", got)
	}
}

// TestRouter_Readyz_FailingPingIsAlsoCachedForTTL proves a failing ping
// must be cached for the TTL exactly like a succeeding one, so a sustained
// flood against an already-down database does not retrigger a fresh
// connection attempt on every single request (a thundering herd against
// the very dependency that is already failing).
func TestRouter_Readyz_FailingPingIsAlsoCachedForTTL(t *testing.T) {
	t.Parallel()

	pinger := &countingPinger{err: errors.New("db unreachable")}
	clock := testutil.NewClockAtEpoch()

	handler := api.New(testLogger(), pinger, api.Options{
		Clock:    clock.Now,
		ReadyTTL: time.Second,
	})
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
