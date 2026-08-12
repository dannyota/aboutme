package api_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// allowHandler is the downstream handler RateLimit wraps in these tests:
// it just proves next.ServeHTTP ran.
func allowHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

// rateLimitedReq builds a request with a given socket peer address
// (RemoteAddr) and, optionally, a claimed api.TrustedClientIPHeader value.
func rateLimitedReq(remoteAddr, claimedIP string) *http.Request {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	if claimedIP != "" {
		req.Header.Set(api.TrustedClientIPHeader, claimedIP)
	}
	return req
}

func TestRateLimit_AllowsWithinLimit(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 3,
		Window:   time.Minute,
		Clock:    clock.Now,
	})(allowHandler())

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, rateLimitedReq("203.0.113.5:1234", ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i, rec.Code, http.StatusOK)
		}
	}
}

func TestRateLimit_RejectsOverLimitWith429AndRetryAfter(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 2,
		Window:   time.Minute,
		Clock:    clock.Now,
	})(allowHandler())

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, rateLimitedReq("203.0.113.5:1234", ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i, rec.Code, http.StatusOK)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, rateLimitedReq("203.0.113.5:1234", ""))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}

	retryAfter := rec.Header().Get("Retry-After")
	secs, err := strconv.Atoi(retryAfter)
	if err != nil || secs < 1 {
		t.Fatalf("Retry-After = %q, want a positive integer number of seconds", retryAfter)
	}

	got := decodeErrorEnvelope(t, rec)
	if got.Code != "rate_limited" {
		t.Errorf("error.code = %q, want %q", got.Code, "rate_limited")
	}
}

// TestRateLimit_UntrustedPeerForgedHeader_DoesNotBypassLimit proves the
// core spoofing-resistance requirement: a request whose socket peer is not
// a trusted proxy must be keyed on that peer address, never on
// TrustedClientIPHeader — otherwise a direct client could send a fresh
// forged value on every request and get a fresh bucket every time,
// bypassing the limit entirely.
func TestRateLimit_UntrustedPeerForgedHeader_DoesNotBypassLimit(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 2,
		Window:   time.Minute,
		Clock:    clock.Now,
		// No TrustedProxies configured: this peer is never trusted, no
		// matter what it claims via TrustedClientIPHeader.
	})(allowHandler())

	// Same socket peer, a different forged claimed IP on every request. If
	// the header were honored, each request would look like a distinct
	// client and none would ever be rejected.
	forgedIPs := []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}
	var lastCode int
	for i, forged := range forgedIPs {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, rateLimitedReq("198.51.100.9:5555", forged))
		lastCode = rec.Code
		if i < 2 && rec.Code != http.StatusOK {
			t.Fatalf("request %d (forged %s=%s): status = %d, want %d",
				i, api.TrustedClientIPHeader, forged, rec.Code, http.StatusOK)
		}
	}
	if lastCode != http.StatusTooManyRequests {
		t.Fatalf("3rd request from the same untrusted peer: status = %d, want %d "+
			"(a forged %s must not bypass the limit)", lastCode, http.StatusTooManyRequests, api.TrustedClientIPHeader)
	}
}

// TestRateLimit_TrustedPeer_KeysByCanonicalHeader proves the other half of
// the requirement: when the socket peer IS a trusted proxy, distinct
// TrustedClientIPHeader values are treated as distinct clients (so many
// real users sharing one proxy aren't lumped into a single bucket), while
// the same value from that trusted proxy is still limited normally.
func TestRateLimit_TrustedPeer_KeysByCanonicalHeader(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests:       1,
		Window:         time.Minute,
		Clock:          clock.Now,
		TrustedProxies: api.LoopbackTrustedProxies(),
	})(allowHandler())

	// Two different real clients behind the trusted proxy: each gets its
	// own allowance.
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, rateLimitedReq("127.0.0.1:9000", "203.0.113.1"))
	if rec1.Code != http.StatusOK {
		t.Fatalf("first client: status = %d, want %d", rec1.Code, http.StatusOK)
	}

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, rateLimitedReq("127.0.0.1:9000", "203.0.113.2"))
	if rec2.Code != http.StatusOK {
		t.Fatalf("second (different) client via same proxy: status = %d, want %d — "+
			"distinct %s values must not share a bucket", rec2.Code, http.StatusOK, api.TrustedClientIPHeader)
	}

	// The first client again, still within the same window: now over its
	// own budget.
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, rateLimitedReq("127.0.0.1:9000", "203.0.113.1"))
	if rec3.Code != http.StatusTooManyRequests {
		t.Fatalf("first client again: status = %d, want %d", rec3.Code, http.StatusTooManyRequests)
	}
}

// TestRateLimit_MissingOrMalformedCanonicalHeader_Rejected proves a trusted
// proxy's request must carry exactly one syntactically valid bare-IP
// TrustedClientIPHeader value, or the request is rejected outright (400)
// rather than falling back to the socket peer address. A fallback would put
// every viewer behind that proxy in one shared bucket, creating a cross-tenant
// denial of service.
func TestRateLimit_MissingOrMalformedCanonicalHeader_Rejected(t *testing.T) {
	t.Parallel()

	cases := []string{
		"",                          // missing entirely
		"not-an-ip",                 // not an address at all
		"999.999.999.999",           // out-of-range octets
		"1.2.3.4, 5.6.7.8",          // a comma-separated list is not a single address
		"203.0.113.5:8080",          // host:port — the header must be a bare address, no port
		"[::1]:8080",                // bracketed IPv6 host:port form — same rule
		strings.Repeat("9", 10_000), // implausibly oversized
	}
	for i, claimed := range cases {
		t.Run(strconv.Itoa(i)+"_"+claimed[:min(len(claimed), 24)], func(t *testing.T) {
			t.Parallel()

			clock := testutil.NewClockAtEpoch()
			handler := api.RateLimit(api.RateLimiterConfig{
				Requests:       1,
				Window:         time.Minute,
				Clock:          clock.Now,
				TrustedProxies: api.LoopbackTrustedProxies(),
			})(allowHandler())

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, rateLimitedReq("127.0.0.1:9000", claimed))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("claimed=%q: status = %d, want %d", claimed, rec.Code, http.StatusBadRequest)
			}
			got := decodeErrorEnvelope(t, rec)
			if got.Code != "invalid_client_ip" {
				t.Errorf("claimed=%q: error.code = %q, want %q", claimed, got.Code, "invalid_client_ip")
			}
		})
	}
}

// TestRateLimit_MultipleCanonicalHeaderValues_Rejected proves more than one
// TrustedClientIPHeader value on a single request is rejected, not silently
// resolved by picking the first (or last) one — an ambiguous header must
// never be trusted.
func TestRateLimit_MultipleCanonicalHeaderValues_Rejected(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests:       1,
		Window:         time.Minute,
		Clock:          clock.Now,
		TrustedProxies: api.LoopbackTrustedProxies(),
	})(allowHandler())

	req := rateLimitedReq("127.0.0.1:9000", "")
	req.Header.Add(api.TrustedClientIPHeader, "203.0.113.1")
	req.Header.Add(api.TrustedClientIPHeader, "203.0.113.2")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	got := decodeErrorEnvelope(t, rec)
	if got.Code != "invalid_client_ip" {
		t.Errorf("error.code = %q, want %q", got.Code, "invalid_client_ip")
	}
}

// TestRateLimit_UntrustedPeer_UnparseableRemoteAddr_Rejected proves the
// fail-closed contract also covers the untrusted-peer path: an unparseable
// RemoteAddr must reject the request rather than being used verbatim as an
// unbounded, unvalidated key.
func TestRateLimit_UntrustedPeer_UnparseableRemoteAddr_Rejected(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 1,
		Window:   time.Minute,
		Clock:    clock.Now,
		// No TrustedProxies: every peer is untrusted, so RemoteAddr itself
		// is what gets parsed.
	})(allowHandler())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, rateLimitedReq("not-an-address", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// TestRateLimit_NormalizesIPv4MappedIPv6 proves clientIP normalizes via
// Unmap: an address and its IPv4-in-IPv6 form must key identically, per
// the required normalization through Unmap().String().
func TestRateLimit_NormalizesIPv4MappedIPv6(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests:       1,
		Window:         time.Minute,
		Clock:          clock.Now,
		TrustedProxies: api.LoopbackTrustedProxies(),
	})(allowHandler())

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, rateLimitedReq("127.0.0.1:9000", "203.0.113.7"))
	if rec1.Code != http.StatusOK {
		t.Fatalf("plain IPv4 request: status = %d, want %d", rec1.Code, http.StatusOK)
	}

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, rateLimitedReq("127.0.0.1:9000", "::ffff:203.0.113.7"))
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("IPv4-mapped-IPv6 form of the same address: status = %d, want %d "+
			"(must normalize to the same key as the plain IPv4 form)", rec2.Code, http.StatusTooManyRequests)
	}
}

// TestRateLimit_FullSimulatedChain_ForgedHeaderChangesNothing simulates
// the full deployment chain — an attacker-
// supplied X-Forwarded-For, a real viewer IP, and a request arriving from
// Caddy's trusted address carrying the single canonical header Caddy
// itself determined — and proves two things at once: the viewer is keyed
// correctly from the canonical header, and the raw X-Forwarded-For content
// (whatever wrote it, upstream of Caddy) has zero effect because this
// server never parses it.
func TestRateLimit_FullSimulatedChain_ForgedHeaderChangesNothing(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests:       1,
		Window:         time.Minute,
		Clock:          clock.Now,
		TrustedProxies: api.LoopbackTrustedProxies(),
	})(allowHandler())

	viewer := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	viewer.RemoteAddr = "127.0.0.1:443"                           // Caddy, the trusted hop
	viewer.Header.Set(api.TrustedClientIPHeader, "198.51.100.42") // what Caddy validated
	// A raw X-Forwarded-For must not override the validated viewer address.
	viewer.Header.Set("X-Forwarded-For", "203.0.113.66, 70.132.0.1")

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, viewer)
	if rec1.Code != http.StatusOK {
		t.Fatalf("viewer request 1: status = %d, want %d", rec1.Code, http.StatusOK)
	}

	// A second, distinct viewer behind the SAME edge/proxy (same
	// X-Forwarded-For content an attacker or CloudFront might send) must
	// get its own bucket, proving the shared edge address never became the
	// key.
	otherViewer := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	otherViewer.RemoteAddr = "127.0.0.1:443"
	otherViewer.Header.Set(api.TrustedClientIPHeader, "198.51.100.99")
	otherViewer.Header.Set("X-Forwarded-For", "203.0.113.66, 70.132.0.1") // identical XFF content

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, otherViewer)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second viewer behind the same edge: status = %d, want %d — "+
			"must not share a bucket with the first viewer", rec2.Code, http.StatusOK)
	}

	// The original viewer again, same instant: now over its own (size-1)
	// budget.
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, viewer)
	if rec3.Code != http.StatusTooManyRequests {
		t.Fatalf("first viewer again: status = %d, want %d", rec3.Code, http.StatusTooManyRequests)
	}
}

// TestRateLimit_ResetsOnInjectedClockWithoutSleeping proves the limiter's
// window expiry is driven entirely by the injected Clock, not real time:
// advancing the fake clock unblocks a rejected key with no test sleep.
func TestRateLimit_ResetsOnInjectedClockWithoutSleeping(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 1,
		Window:   time.Minute,
		Clock:    clock.Now,
	})(allowHandler())

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, rateLimitedReq("203.0.113.5:1234", ""))
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request: status = %d, want %d", rec1.Code, http.StatusOK)
	}

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, rateLimitedReq("203.0.113.5:1234", ""))
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request within the window: status = %d, want %d", rec2.Code, http.StatusTooManyRequests)
	}

	clock.Advance(time.Minute) // full refill; no real time elapses

	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, rateLimitedReq("203.0.113.5:1234", ""))
	if rec3.Code != http.StatusOK {
		t.Fatalf("request after advancing the clock past Window: status = %d, want %d", rec3.Code, http.StatusOK)
	}
}

// TestRateLimit_ExactBoundaryPlusMinusOneNanosecond proves refill lands
// exactly at capacity when precisely Window has elapsed, not a hair before
// or after: 1ns short of the boundary must still be denied, and exactly at
// the boundary must be allowed.
func TestRateLimit_ExactBoundaryPlusMinusOneNanosecond(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 1,
		Window:   time.Minute,
		Clock:    clock.Now,
	})(allowHandler())

	req := func() *http.Request { return rateLimitedReq("203.0.113.5:1234", "") }

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req())
	if rec1.Code != http.StatusOK {
		t.Fatalf("initial request: status = %d, want %d", rec1.Code, http.StatusOK)
	}

	clock.Advance(time.Minute - time.Nanosecond)
	recBefore := httptest.NewRecorder()
	handler.ServeHTTP(recBefore, req())
	if recBefore.Code != http.StatusTooManyRequests {
		t.Fatalf("1ns before Window elapsed: status = %d, want %d (not yet refilled)",
			recBefore.Code, http.StatusTooManyRequests)
	}

	clock.Advance(time.Nanosecond) // now exactly Window since the initial request
	recAt := httptest.NewRecorder()
	handler.ServeHTTP(recAt, req())
	if recAt.Code != http.StatusOK {
		t.Fatalf("exactly at Window elapsed: status = %d, want %d (fully refilled)", recAt.Code, http.StatusOK)
	}
}

// TestRateLimit_ClockRollback_DoesNotFreezeRefillForever proves the
// documented failure mode of a hand-rolled "only refill when elapsed > 0"
// bucket — a backward clock step permanently latching the bucket at a
// stale high-water mark — does not happen here: after the clock moves
// backward and then forward again to (at least) Window past the original
// consuming request, the bucket must be refilled, not still stuck denying.
func TestRateLimit_ClockRollback_DoesNotFreezeRefillForever(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 1,
		Window:   time.Minute,
		Clock:    clock.Now,
	})(allowHandler())

	req := func() *http.Request { return rateLimitedReq("203.0.113.5:1234", "") }

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req())
	if rec1.Code != http.StatusOK {
		t.Fatalf("initial request: status = %d, want %d", rec1.Code, http.StatusOK)
	}

	// Clock steps backward (e.g. an NTP correction) well before the
	// initial request's own timestamp.
	clock.Set(testutil.Epoch.Add(-30 * time.Second))
	recRolledBack := httptest.NewRecorder()
	handler.ServeHTTP(recRolledBack, req())
	if recRolledBack.Code != http.StatusTooManyRequests {
		t.Fatalf("immediately after rollback: status = %d, want %d (no tokens were consumed to fabricate one)",
			recRolledBack.Code, http.StatusTooManyRequests)
	}

	// Clock resumes moving forward and reaches (comfortably past) a full
	// Window after the ORIGINAL request.
	clock.Set(testutil.Epoch.Add(2 * time.Minute))
	recAfter := httptest.NewRecorder()
	handler.ServeHTTP(recAfter, req())
	if recAfter.Code != http.StatusOK {
		t.Fatalf("after clock resumes forward past Window: status = %d, want %d — "+
			"a prior backward step must not freeze refill permanently", recAfter.Code, http.StatusOK)
	}
}

// TestRateLimit_ConcurrentSameKey_ExactAccounting drives many goroutines
// at the same key concurrently (run with -race) and asserts exactly
// cfg.Requests of them are admitted — no more (a race letting two
// goroutines both observe "1 token left" and both consume it) and no
// fewer (a race that drops a token that should have been available).
func TestRateLimit_ConcurrentSameKey_ExactAccounting(t *testing.T) {
	t.Parallel()

	const requests = 50
	const attempts = 500

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: requests,
		Window:   time.Minute,
		Clock:    clock.Now,
	})(allowHandler())

	var admitted atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, rateLimitedReq("203.0.113.5:1234", ""))
			if rec.Code == http.StatusOK {
				admitted.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := admitted.Load(); got != requests {
		t.Errorf("admitted = %d, want exactly %d", got, requests)
	}
}

// TestRateLimit_KeyStore_ReclaimsOnlyExpiredEntries proves the store
// reclaims a slot from a key whose bucket is fully refilled (holds no
// state worth preserving) to admit a new key once MaxKeys is reached.
func TestRateLimit_KeyStore_ReclaimsOnlyExpiredEntries(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 1,
		Window:   time.Minute,
		Clock:    clock.Now,
		MaxKeys:  1,
	})(allowHandler())

	recA1 := httptest.NewRecorder()
	handler.ServeHTTP(recA1, rateLimitedReq("203.0.113.10:1", ""))
	if recA1.Code != http.StatusOK {
		t.Fatalf("key A, request 1: status = %d, want %d", recA1.Code, http.StatusOK)
	}

	// A's bucket is now fully refilled (a full Window has passed with no
	// further use): admitting a brand-new key B must be able to reclaim
	// A's slot.
	clock.Advance(time.Minute)

	recB1 := httptest.NewRecorder()
	handler.ServeHTTP(recB1, rateLimitedReq("203.0.113.20:1", ""))
	if recB1.Code != http.StatusOK {
		t.Fatalf("key B, request 1 (after A expired): status = %d, want %d", recB1.Code, http.StatusOK)
	}
}

// TestRateLimit_KeyStore_ReclaimsUnrefilledEntryAfterHardIdleExpiry proves
// the 24-hour idle bound is independent of token refill. A very slow bucket
// must not pin one of the bounded key slots forever after its client vanishes.
func TestRateLimit_KeyStore_ReclaimsUnrefilledEntryAfterHardIdleExpiry(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 1,
		Window:   48 * time.Hour,
		Clock:    clock.Now,
		MaxKeys:  1,
	})(allowHandler())
	send := func(ip string) int {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, rateLimitedReq(ip+":1", ""))
		return rec.Code
	}

	if got := send("203.0.113.10"); got != http.StatusOK {
		t.Fatalf("A at epoch: status = %d, want %d", got, http.StatusOK)
	}
	if got := send("203.0.113.20"); got != http.StatusOK {
		t.Fatalf("B via overflow at epoch: status = %d, want %d", got, http.StatusOK)
	}
	clock.Advance(24*time.Hour + time.Second)
	if got := send("203.0.113.30"); got != http.StatusOK {
		t.Fatalf("C after A's hard idle expiry: status = %d, want %d", got, http.StatusOK)
	}
}

// TestRateLimit_KeyStore_RejectionRefreshesIdleClock proves last activity is
// recorded for denied requests too. Otherwise a caller continuously probing
// an exhausted key would be granted a fresh private bucket at 24 hours.
func TestRateLimit_KeyStore_RejectionRefreshesIdleClock(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 1,
		Window:   48 * time.Hour,
		Clock:    clock.Now,
		MaxKeys:  1,
	})(allowHandler())
	send := func(ip string) int {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, rateLimitedReq(ip+":1", ""))
		return rec.Code
	}

	if got := send("203.0.113.10"); got != http.StatusOK {
		t.Fatalf("A at epoch: status = %d, want %d", got, http.StatusOK)
	}
	if got := send("203.0.113.20"); got != http.StatusOK {
		t.Fatalf("B via overflow at epoch: status = %d, want %d", got, http.StatusOK)
	}
	clock.Advance(23 * time.Hour)
	if got := send("203.0.113.10"); got != http.StatusTooManyRequests {
		t.Fatalf("A rejection at 23h: status = %d, want %d", got, http.StatusTooManyRequests)
	}
	clock.Advance(2 * time.Hour)
	if got := send("203.0.113.30"); got != http.StatusTooManyRequests {
		t.Fatalf("C two hours after A's rejection: status = %d, want %d; rejection must reset idle expiry", got, http.StatusTooManyRequests)
	}
}

func TestRateLimit_KeyStore_PeriodicAllowedAndRejectedActivityPreventsIdleExpiry(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 1,
		Window:   6 * time.Hour,
		Clock:    clock.Now,
		MaxKeys:  1,
	})(allowHandler())
	send := func(ip string) int {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, rateLimitedReq(ip+":1", ""))
		return recorder.Code
	}

	if got := send("203.0.113.10"); got != http.StatusOK {
		t.Fatalf("initial activity status = %d, want 200", got)
	}
	for elapsed := 6 * time.Hour; elapsed <= 30*time.Hour; elapsed += 6 * time.Hour {
		clock.Advance(6 * time.Hour)
		if got := send("203.0.113.10"); got != http.StatusOK {
			t.Fatalf("allowed activity at %v status = %d, want 200", elapsed, got)
		}
		if got := send("203.0.113.10"); got != http.StatusTooManyRequests {
			t.Fatalf("rejected activity at %v status = %d, want 429", elapsed, got)
		}
	}
	if got := send("203.0.113.20"); got != http.StatusOK {
		t.Fatalf("new key via overflow status = %d, want 200", got)
	}
	if got := send("203.0.113.10"); got != http.StatusTooManyRequests {
		t.Fatalf("active original key status = %d, want 429; it must not have been idle-evicted", got)
	}
}

func TestRateLimit_KeyStore_BackwardClockCannotExpireAnActiveEntry(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 1,
		Window:   48 * time.Hour,
		Clock:    clock.Now,
		MaxKeys:  1,
	})(allowHandler())
	send := func(ip string) int {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, rateLimitedReq(ip+":1", ""))
		return recorder.Code
	}

	if got := send("203.0.113.10"); got != http.StatusOK {
		t.Fatalf("initial status = %d, want 200", got)
	}
	clock.Advance(23 * time.Hour)
	if got := send("203.0.113.10"); got != http.StatusTooManyRequests {
		t.Fatalf("activity at 23h status = %d, want 429", got)
	}
	clock.Set(testutil.Epoch.Add(-24 * time.Hour))
	if got := send("203.0.113.20"); got != http.StatusOK {
		t.Fatalf("new key during rollback via overflow status = %d, want 200", got)
	}
	if got := send("203.0.113.10"); got != http.StatusTooManyRequests {
		t.Fatalf("original key during rollback status = %d, want 429; rollback must not create idle age", got)
	}
}

// TestRateLimit_KeyStore_FullOfActiveEntries_OverflowsInsteadOfEvicting proves
// that when the store is at MaxKeys and every entry is active, admitting a new
// key must NOT evict an arbitrary active entry — that would hand an
// attacker a fresh bucket via key churn. Key A's own budget must be
// completely undisturbed by B's admission.
func TestRateLimit_KeyStore_FullOfActiveEntries_OverflowsInsteadOfEvicting(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 1,
		Window:   time.Minute,
		Clock:    clock.Now,
		MaxKeys:  1,
	})(allowHandler())

	// Key A takes the store's one slot and consumes its one token — still
	// fully active (no time has passed).
	recA1 := httptest.NewRecorder()
	handler.ServeHTTP(recA1, rateLimitedReq("203.0.113.10:1", ""))
	if recA1.Code != http.StatusOK {
		t.Fatalf("key A, request 1: status = %d, want %d", recA1.Code, http.StatusOK)
	}

	// Key B arrives at the same instant: the store is full of an active
	// entry, so B must fall back to the shared overflow limiter rather
	// than evicting A.
	recB1 := httptest.NewRecorder()
	handler.ServeHTTP(recB1, rateLimitedReq("203.0.113.20:1", ""))
	if recB1.Code != http.StatusOK {
		t.Fatalf("key B, request 1 (via overflow): status = %d, want %d", recB1.Code, http.StatusOK)
	}

	// Key A again, same instant: if B had evicted A, this would be a fresh
	// bucket (200). It must still be 429 — A's own state was never
	// touched.
	recA2 := httptest.NewRecorder()
	handler.ServeHTTP(recA2, rateLimitedReq("203.0.113.10:1", ""))
	if recA2.Code != http.StatusTooManyRequests {
		t.Fatalf("key A, request 2: status = %d, want %d — "+
			"an active entry must never be evicted to admit a new key", recA2.Code, http.StatusTooManyRequests)
	}
}

// TestRateLimit_MaxKeyChurn_BoundedThroughputViaOverflow proves that with the
// store saturated by active entries, flooding in many additional distinct
// (e.g. forged) keys must
// not each get their own fresh bucket. Every key beyond the store's
// capacity shares one overflow bucket, so their combined admitted
// throughput is bounded at exactly one key's normal budget, however many
// distinct keys are involved.
func TestRateLimit_MaxKeyChurn_BoundedThroughputViaOverflow(t *testing.T) {
	t.Parallel()

	const maxKeys = 2
	const overflowBudget = 2

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: overflowBudget,
		Window:   time.Minute,
		Clock:    clock.Now,
		MaxKeys:  maxKeys,
	})(allowHandler())

	// Fill the store with maxKeys genuinely active entries (each uses only
	// one of its own overflowBudget tokens, so they stay active/non-
	// expired for the rest of the test).
	for i := 0; i < maxKeys; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, rateLimitedReq("203.0.113."+strconv.Itoa(i)+":1", ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("filling store, key %d: status = %d, want %d", i, rec.Code, http.StatusOK)
		}
	}

	// Flood in far more distinct keys than the store has room for. Every
	// one of them shares the overflow bucket, whose budget is
	// overflowBudget total — not overflowBudget per churned key.
	const churnedKeys = 50
	var admitted int
	for i := 0; i < churnedKeys; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, rateLimitedReq("198.51.100."+strconv.Itoa(i)+":1", ""))
		if rec.Code == http.StatusOK {
			admitted++
		}
	}

	if admitted != overflowBudget {
		t.Errorf("admitted %d of %d churned (distinct, never-before-seen) keys, want exactly %d "+
			"(unlimited key churn must not grant unlimited throughput)", admitted, churnedKeys, overflowBudget)
	}
}

// TestIPKeyFunc_DistinctIPsGetDistinctKeys is a focused unit check on the
// default KeyFunc in isolation from RateLimit's HTTP wiring.
func TestIPKeyFunc_DistinctIPsGetDistinctKeys(t *testing.T) {
	t.Parallel()

	a, aOK := api.IPKeyFunc(rateLimitedReq("203.0.113.1:1", ""), nil)
	b, bOK := api.IPKeyFunc(rateLimitedReq("203.0.113.2:1", ""), nil)
	if !aOK || !bOK {
		t.Fatalf("IPKeyFunc ok = (%v, %v), want (true, true)", aOK, bOK)
	}
	if a == b {
		t.Fatalf("IPKeyFunc for distinct IPs both = %q, want distinct keys", a)
	}
}

// TestIPKeyFunc_TrustedPeerMissingHeader_Fails proves IPKeyFunc's own
// contract in isolation: a trusted peer with no canonical header fails
// (ok=false) rather than returning some fallback key — see RateLimit's use
// of this to reject the request instead of admitting it under a shared key.
func TestIPKeyFunc_TrustedPeerMissingHeader_Fails(t *testing.T) {
	t.Parallel()

	_, ok := api.IPKeyFunc(rateLimitedReq("127.0.0.1:9000", ""), api.LoopbackTrustedProxies())
	if ok {
		t.Fatal("IPKeyFunc ok = true for a trusted peer with no canonical header, want false")
	}
}

// TestClientIP_UntrustedPeer_UsesRemoteAddr proves ClientIP resolves an
// untrusted request's raw socket peer address, bare (no port) — the shape
// internal/auth's SessionManager.Issue requires (it hard-fails on a
// "host:port" string).
func TestClientIP_UntrustedPeer_UsesRemoteAddr(t *testing.T) {
	t.Parallel()

	got, ok := api.ClientIP(rateLimitedReq("203.0.113.7:54321", ""), nil)
	if !ok {
		t.Fatal("ClientIP ok = false, want true for an untrusted request with a valid RemoteAddr")
	}
	if got != "203.0.113.7" {
		t.Errorf("ClientIP = %q, want %q (bare address, no port)", got, "203.0.113.7")
	}
}

// TestClientIP_TrustedProxy_UsesCanonicalHeader proves ClientIP takes the
// viewer address from TrustedClientIPHeader — never the trusted proxy's
// own RemoteAddr — matching resolveClientIP/IPKeyFunc's own trust
// decision exactly (this wrapper must not invent a different one).
func TestClientIP_TrustedProxy_UsesCanonicalHeader(t *testing.T) {
	t.Parallel()

	req := rateLimitedReq("127.0.0.1:9000", "198.51.100.42")
	got, ok := api.ClientIP(req, api.LoopbackTrustedProxies())
	if !ok {
		t.Fatal("ClientIP ok = false, want true for a trusted peer with a canonical header")
	}
	if got != "198.51.100.42" {
		t.Errorf("ClientIP = %q, want %q (the canonical header value, not the proxy's own RemoteAddr)", got, "198.51.100.42")
	}
}

// TestClientIP_TrustedProxyMissingHeader_FailsClosed proves ClientIP fails
// closed (ok=false) rather than falling back to the trusted proxy's own
// RemoteAddr when the canonical header is absent. A
// caller like session issuance must not silently record the proxy's own
// address as if it were the viewer's.
func TestClientIP_TrustedProxyMissingHeader_FailsClosed(t *testing.T) {
	t.Parallel()

	_, ok := api.ClientIP(rateLimitedReq("127.0.0.1:9000", ""), api.LoopbackTrustedProxies())
	if ok {
		t.Error("ClientIP ok = true for a trusted peer with no canonical header, want false (fail closed)")
	}
}

// TestAccountKeyFunc_DistinctAccountsGetDistinctKeys proves AccountKeyFunc
// reads the account ID auth middleware stores in
// context via WithAccountID.
func TestAccountKeyFunc_DistinctAccountsGetDistinctKeys(t *testing.T) {
	t.Parallel()

	reqFor := func(accountID string) *http.Request {
		r := rateLimitedReq("203.0.113.1:1", "")
		return r.WithContext(api.WithAccountID(r.Context(), accountID))
	}

	a, aOK := api.AccountKeyFunc(reqFor("acct-alice"), nil)
	b, bOK := api.AccountKeyFunc(reqFor("acct-bob"), nil)
	if !aOK || !bOK {
		t.Fatalf("AccountKeyFunc ok = (%v, %v), want (true, true)", aOK, bOK)
	}
	if a == b {
		t.Fatalf("AccountKeyFunc for distinct accounts both = %q, want distinct keys", a)
	}

	// Same account, same key, regardless of IP — AccountKeyFunc ignores it.
	sameAcctDifferentIP := rateLimitedReq("198.51.100.1:1", "")
	sameAcctDifferentIP = sameAcctDifferentIP.WithContext(api.WithAccountID(sameAcctDifferentIP.Context(), "acct-alice"))
	if got, ok := api.AccountKeyFunc(sameAcctDifferentIP, nil); got != a || !ok {
		t.Errorf("AccountKeyFunc for the same account from a different IP = (%q, %v), want (%q, true)", got, ok, a)
	}
}

// TestCompositeKeyFunc_BothDimensionsMustMatchForSameKey proves design
// the per-account-and-IP requirement: a composite key changes if either
// the account or the IP changes, so neither dimension alone determines the
// bucket.
func TestCompositeKeyFunc_BothDimensionsMustMatchForSameKey(t *testing.T) {
	t.Parallel()

	key := api.CompositeKeyFunc(api.AccountKeyFunc, api.IPKeyFunc)

	reqFor := func(accountID, remoteAddr string) *http.Request {
		r := rateLimitedReq(remoteAddr, "")
		return r.WithContext(api.WithAccountID(r.Context(), accountID))
	}

	keyOK := func(r *http.Request) string {
		t.Helper()
		got, ok := key(r, nil)
		if !ok {
			t.Fatalf("CompositeKeyFunc ok = false, want true")
		}
		return got
	}

	base := keyOK(reqFor("acct-alice", "203.0.113.1:1"))
	sameAcctDifferentIP := keyOK(reqFor("acct-alice", "203.0.113.2:1"))
	differentAcctSameIP := keyOK(reqFor("acct-bob", "203.0.113.1:1"))
	same := keyOK(reqFor("acct-alice", "203.0.113.1:1"))

	if base == sameAcctDifferentIP {
		t.Error("same account, different IP: keys must differ")
	}
	if base == differentAcctSameIP {
		t.Error("different account, same IP: keys must differ")
	}
	if base != same {
		t.Error("identical account and IP: keys must match")
	}
}

// TestRateLimit_CompositeKey_LimitsAccountAndIPIndependently proves
// RateLimit itself honors a configured composite KeyFunc end to end: the
// same account from a different IP (or a different account from the same
// IP) gets its own budget, not a shared one.
func TestRateLimit_CompositeKey_LimitsAccountAndIPIndependently(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 1,
		Window:   time.Minute,
		Clock:    clock.Now,
		Key:      api.CompositeKeyFunc(api.AccountKeyFunc, api.IPKeyFunc),
	})(allowHandler())

	reqFor := func(accountID, remoteAddr string) *http.Request {
		r := rateLimitedReq(remoteAddr, "")
		return r.WithContext(api.WithAccountID(r.Context(), accountID))
	}

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, reqFor("acct-alice", "203.0.113.1:1"))
	if rec1.Code != http.StatusOK {
		t.Fatalf("alice from IP 1, request 1: status = %d, want %d", rec1.Code, http.StatusOK)
	}

	// Same account, different IP: independent budget.
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, reqFor("acct-alice", "203.0.113.2:1"))
	if rec2.Code != http.StatusOK {
		t.Fatalf("alice from IP 2: status = %d, want %d", rec2.Code, http.StatusOK)
	}

	// Different account, same original IP: also independent.
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, reqFor("acct-bob", "203.0.113.1:1"))
	if rec3.Code != http.StatusOK {
		t.Fatalf("bob from IP 1: status = %d, want %d", rec3.Code, http.StatusOK)
	}

	// Alice from IP 1 again: this exact pair already used its budget.
	rec4 := httptest.NewRecorder()
	handler.ServeHTTP(rec4, reqFor("acct-alice", "203.0.113.1:1"))
	if rec4.Code != http.StatusTooManyRequests {
		t.Fatalf("alice from IP 1 again: status = %d, want %d", rec4.Code, http.StatusTooManyRequests)
	}
}

// TestCompositeKeyFunc_ComponentFailurePropagates proves CompositeKeyFunc
// fails as a whole when any one component KeyFunc does, rather than
// silently degrading to whichever components did succeed — an unresolvable
// client IP must still reject the request even when composed with an
// always-succeeding AccountKeyFunc.
func TestCompositeKeyFunc_ComponentFailurePropagates(t *testing.T) {
	t.Parallel()

	key := api.CompositeKeyFunc(api.AccountKeyFunc, api.IPKeyFunc)

	// A trusted peer with no canonical header: IPKeyFunc fails.
	req := rateLimitedReq("127.0.0.1:9000", "")
	req = req.WithContext(api.WithAccountID(req.Context(), "acct-alice"))

	if _, ok := key(req, api.LoopbackTrustedProxies()); ok {
		t.Fatal("CompositeKeyFunc ok = true when a component KeyFunc failed, want false")
	}
}

// TestRateLimit_CompositeKey_UnresolvableIP_Rejected proves the same
// propagation end to end through RateLimit: a composite (account, IP) key
// on a trusted-but-header-less request is rejected with 400, not silently
// admitted keyed on the account alone.
func TestRateLimit_CompositeKey_UnresolvableIP_Rejected(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests:       1,
		Window:         time.Minute,
		Clock:          clock.Now,
		TrustedProxies: api.LoopbackTrustedProxies(),
		Key:            api.CompositeKeyFunc(api.AccountKeyFunc, api.IPKeyFunc),
	})(allowHandler())

	req := rateLimitedReq("127.0.0.1:9000", "") // trusted peer, no header
	req = req.WithContext(api.WithAccountID(req.Context(), "acct-alice"))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// TestRateLimit_RejectedRequestsDoNotOweTokens proves a burst of
// rejected requests beyond the budget must not leave the bucket owing
// anything beyond what was actually admitted — after exactly one Window,
// exactly Requests admissions must succeed again, no fewer.
func TestRateLimit_RejectedRequestsDoNotOweTokens(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 2,
		Window:   time.Minute,
		Clock:    clock.Now,
	})(allowHandler())

	req := func() *http.Request { return rateLimitedReq("203.0.113.5:1234", "") }

	// Exhaust the budget.
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req())
		if rec.Code != http.StatusOK {
			t.Fatalf("admitted request %d: status = %d, want %d", i, rec.Code, http.StatusOK)
		}
	}

	// Several more requests, all rejected. None of these may create
	// "phantom debt" that delays refill beyond one normal Window.
	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req())
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("rejected request %d: status = %d, want %d", i, rec.Code, http.StatusTooManyRequests)
		}
	}

	clock.Advance(time.Minute) // exactly one Window past the admitted requests

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req())
		if rec.Code != http.StatusOK {
			t.Fatalf("post-refill request %d: status = %d, want %d "+
				"(a rejected request must never owe a token)", i, rec.Code, http.StatusOK)
		}
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd post-refill request: status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}

// TestRateLimit_SaturatedStore_ScanIsAmortizedNotPerRequest proves that once a
// saturated store's entry becomes expired, a flood of new distinct keys
// arriving faster than the store's internal sweep cooldown must not each
// pay a fresh O(MaxKeys) expired-entry scan — only the first is allowed to
// reclaim the newly-expired slot; the rest fall through to the shared
// overflow limiter exactly as if nothing had expired yet.
//
// This is proven with a deliberately tiny Window (far shorter than any
// plausible sweep cooldown) so an entry can expire almost immediately,
// combined with the shared overflow limiter's own budget as an observable
// probe: if a second flood request had instead reclaimed its own dedicated
// slot (i.e. the store re-scanned on every request), the overflow limiter's
// meanwhile-refilled token would be
// left untouched for a third request to also succeed. Amortized scanning
// instead routes the second AND third requests through the shared overflow
// limiter, so the third finds it already spent and is rejected.
func TestRateLimit_SaturatedStore_ScanIsAmortizedNotPerRequest(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 1,
		Window:   time.Millisecond,
		Clock:    clock.Now,
		MaxKeys:  1,
	})(allowHandler())

	// Key A takes the store's one slot.
	recA := httptest.NewRecorder()
	handler.ServeHTTP(recA, rateLimitedReq("203.0.113.10:1", ""))
	if recA.Code != http.StatusOK {
		t.Fatalf("key A: status = %d, want %d", recA.Code, http.StatusOK)
	}

	// Key X1: the store is full of an active entry (A), so this is the
	// first saturated encounter — a scan runs (finds nothing expired yet)
	// and starts the store's internal sweep cooldown. X1 falls back to the
	// shared overflow limiter, consuming its only token.
	recX1 := httptest.NewRecorder()
	handler.ServeHTTP(recX1, rateLimitedReq("203.0.113.20:1", ""))
	if recX1.Code != http.StatusOK {
		t.Fatalf("key X1 (first overflow use): status = %d, want %d", recX1.Code, http.StatusOK)
	}

	// A full Window passes: A is now expired (fully refilled), and — since
	// Window is far shorter than any plausible sweep cooldown — this is
	// still well inside the cooldown window started above.
	clock.Advance(time.Millisecond)

	// Key X2: if the store re-scanned here, it would evict A and hand X2 a
	// fresh dedicated slot, leaving the overflow limiter's meanwhile-
	// refilled token untouched. The amortized store instead skips the scan
	// and falls back to overflow again — which, by this same instant, has
	// also refilled (same rate/window as any per-key bucket) and so still
	// admits X2. This request alone can't distinguish the two cases; X3
	// below can.
	recX2 := httptest.NewRecorder()
	handler.ServeHTTP(recX2, rateLimitedReq("203.0.113.30:1", ""))
	if recX2.Code != http.StatusOK {
		t.Fatalf("key X2: status = %d, want %d", recX2.Code, http.StatusOK)
	}

	// Key X3, same instant: if X2 had reclaimed its own dedicated slot (no
	// amortization), the overflow limiter's refilled token would still be
	// sitting unused and X3 would also succeed. Because X2 actually went
	// through overflow (amortization skipped the scan), overflow's one
	// token is already spent and X3 must be rejected.
	recX3 := httptest.NewRecorder()
	handler.ServeHTTP(recX3, rateLimitedReq("203.0.113.40:1", ""))
	if recX3.Code != http.StatusTooManyRequests {
		t.Fatalf("key X3: status = %d, want %d — a saturated store must not re-scan "+
			"(and so reclaim a slot) on every single request", recX3.Code, http.StatusTooManyRequests)
	}
}

// TestRateLimit_ConcurrentDistinctKeys_NoRaceUnderSaturation drives many
// goroutines at many distinct keys concurrently, with MaxKeys small enough
// that the store saturates during the run — run with -race to catch any
// data race in the eviction-sweep/overflow bookkeeping under concurrent
// admission, not just concurrent same-key accounting.
func TestRateLimit_ConcurrentDistinctKeys_NoRaceUnderSaturation(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 2,
		Window:   time.Minute,
		Clock:    clock.Now,
		MaxKeys:  10,
	})(allowHandler())

	const goroutines = 200
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, rateLimitedReq("203.0.113."+strconv.Itoa(i%256)+":1", ""))
			if rec.Code != http.StatusOK && rec.Code != http.StatusTooManyRequests {
				t.Errorf("unexpected status %d", rec.Code)
			}
		}(i)
	}
	wg.Wait()
}

// TestRateLimit_ConcurrentDistinctKeys_AtomicAdmission_MaxKeysOne proves
// lookup, expiry, and first-token consumption form one critical section. A
// newly inserted entry must consume its first token before another key can
// observe it as fully refilled and evict it. Under MaxKeys=1, at most one of
// many concurrent distinct keys can occupy the dedicated slot; every other
// key shares the overflow bucket.
func TestRateLimit_ConcurrentDistinctKeys_AtomicAdmission_MaxKeysOne(t *testing.T) {
	t.Parallel()

	// The forbidden interleaving (insert a new entry, release the store lock,
	// THEN consume its first token — leaving a window where another
	// goroutine's admission can observe the still-unconsumed entry as
	// "expired" and evict it) depends on real OS thread scheduling landing
	// inside a very narrow gap. A single trial can miss it in a non-atomic
	// implementation. Many independent trials, each against a
	// fresh limiter (so no state bleeds between them), make detection reliable
	// without any sleep or scheduler-hint hack. The outcome below is a
	// mathematical guarantee when one critical section covers
	// lookup, the expiry decision, and the first reserve, so every trial must
	// pass identically.
	const trials = 30
	const distinctKeys = 50

	for trial := 0; trial < trials; trial++ {
		clock := testutil.NewClockAtEpoch()
		handler := api.RateLimit(api.RateLimiterConfig{
			Requests: 1, // burst 1: the shared overflow bucket also grants exactly one token
			Window:   time.Minute,
			Clock:    clock.Now,
			MaxKeys:  1,
		})(allowHandler())

		var wg sync.WaitGroup
		var admitted atomic.Int64
		start := make(chan struct{})
		for i := 0; i < distinctKeys; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start // maximize real concurrent contention on the store lock
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, rateLimitedReq("203.0.113."+strconv.Itoa(i)+":1", ""))
				if rec.Code == http.StatusOK {
					admitted.Add(1)
				}
			}(i)
		}
		close(start)
		wg.Wait()

		// Exactly one key can land the store's single dedicated slot, and
		// exactly one more can drain the shared overflow bucket's single
		// token — never one full private grant per distinct key.
		if got := admitted.Load(); got != 2 {
			t.Fatalf("trial %d: admitted = %d of %d concurrent distinct keys at MaxKeys=1, want exactly 2 "+
				"(one private entry + one shared overflow token) — non-atomic admission lets "+
				"each key evict the previous one and grant itself a fresh private bucket instead",
				trial, got, distinctKeys)
		}
	}
}

// TestRateLimit_ConcurrentRejections_DoNotOweTokens_Regression proves
// concurrent rejected reservations do not leave phantom token debt. A
// sequential test cannot observe this:
// x/time/rate's ReserveN always advances the bucket's internal
// bookkeeping, even for a reservation that will later be canceled, and
// when many concurrent reservations interleave, a later one can advance
// the bucket's lastEvent past an earlier one's timeToAct, so CancelAt
// restores nothing: the earlier "rejected" request leaves the bucket
// owing a token it never spent. Non-mutating denial (AllowN — see
// reserveNow) has no reservation to cancel and so cannot leave phantom
// debt, for any interleaving.
func TestRateLimit_ConcurrentRejections_DoNotOweTokens_Regression(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 1,
		Window:   time.Minute,
		Clock:    clock.Now,
	})(allowHandler())

	req := func() *http.Request { return rateLimitedReq("203.0.113.5:1234", "") }

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	if rec.Code != http.StatusOK {
		t.Fatalf("initial admission: status = %d, want %d", rec.Code, http.StatusOK)
	}

	const rounds = 40
	const concurrentRejections = 32
	for round := 0; round < rounds; round++ {
		var wg sync.WaitGroup
		for i := 0; i < concurrentRejections; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req())
				if rec.Code != http.StatusTooManyRequests {
					t.Errorf("round %d: concurrent request got status %d, want %d (budget was already spent)",
						round, rec.Code, http.StatusTooManyRequests)
				}
			}()
		}
		wg.Wait()

		clock.Advance(time.Minute) // exactly one Window past the last admission

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req())
		if rec.Code != http.StatusOK {
			t.Fatalf("round %d: post-Window admission after %d concurrent rejections: status = %d, want %d "+
				"(concurrent rejections must never create phantom debt)", round, concurrentRejections, rec.Code, http.StatusOK)
		}
	}
}

// TestRateLimit_ClockRollback_NoEarlyTokenGranted_Regression proves a backward
// clock step cannot manufacture a token ahead of schedule:
// two requests admitted with real capacity to spare, a clock rollback, a
// request whose wall-clock-relative due time has not yet arrived (must
// still be rejected even once the clock resumes forward past the rollback
// point but short of the real due time), and finally recovery at the real
// due time.
func TestRateLimit_ClockRollback_NoEarlyTokenGranted_Regression(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 1,
		Window:   60 * time.Second,
		Clock:    clock.Now,
	})(allowHandler())

	req := func() *http.Request { return rateLimitedReq("203.0.113.5:1234", "") }

	// t=0: admit the sole token.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	if rec.Code != http.StatusOK {
		t.Fatalf("t=0 admission: status = %d, want %d", rec.Code, http.StatusOK)
	}

	// t=100s: more than one Window has genuinely passed; admitted, and the
	// NEXT token is now correctly due at t=160s.
	clock.Set(testutil.Epoch.Add(100 * time.Second))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	if rec.Code != http.StatusOK {
		t.Fatalf("t=100s admission: status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Clock rolls back to t=70s (e.g. an NTP correction) and a request
	// lands there: no spare capacity at t=70s relative to the real t=100s
	// admission, so this must be rejected.
	clock.Set(testutil.Epoch.Add(70 * time.Second))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("t=70s (rolled back): status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}

	// Clock resumes forward to t=130s: still 30s short of the correct
	// t=160s due time. An implementation that lets the t=70s rollback
	// re-anchor the bucket's internal clock would admit here early.
	clock.Set(testutil.Epoch.Add(130 * time.Second))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("t=130s (30s before the real due time): status = %d, want %d — "+
			"a clock rollback must never grant a token early", rec.Code, http.StatusTooManyRequests)
	}

	// Recovery: at the correct t=160s due time, admission succeeds.
	clock.Set(testutil.Epoch.Add(160 * time.Second))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	if rec.Code != http.StatusOK {
		t.Fatalf("t=160s (the real due time): status = %d, want %d — recovery must still work", rec.Code, http.StatusOK)
	}
}

// TestRateLimit_TrustedProxiesMismatch_LogsWarningOnceNotPerRequest proves that when
// TrustedProxies is configured but doesn't match the request's real peer
// (e.g. TRUSTED_PROXY_CIDRS set to the wrong subnet), RateLimit must log a
// distinct, high-severity warning so an operator notices the
// misconfiguration — throttled to at most once per trustMismatchWarnInterval
// (see the const), never once per request. Five requests landing at the
// same clock instant (the case here) all fall inside the same interval, so
// this still asserts exactly one log line; see
// TestRateLimit_TrustedProxiesMismatch_ReLogsAfterInterval for proof the
// condition stays observable across a live incident rather than being
// silenced after the first request.
func TestRateLimit_TrustedProxiesMismatch_LogsWarningOnceNotPerRequest(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 100,
		Window:   time.Minute,
		Clock:    clock.Now,
		Logger:   logger,
		// Configured trust exists but does not include this peer's real
		// address — e.g. TRUSTED_PROXY_CIDRS was set to the wrong subnet.
		TrustedProxies: api.TrustedProxies{netip.MustParsePrefix("192.0.2.0/24")},
	})(allowHandler())

	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, rateLimitedReq("203.0.113.5:1234", ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want %d", i, rec.Code, http.StatusOK)
		}
	}

	logged := buf.String()
	if count := strings.Count(logged, "trust boundary misconfigured"); count != 1 {
		t.Errorf("mismatch warning logged %d times across 5 requests, want exactly 1 (must not spam per request); log=%s",
			count, logged)
	}
	if !strings.Contains(logged, "level=WARN") {
		t.Errorf("log = %q, want a WARN-level entry", logged)
	}
}

// TestRateLimit_TrustedProxiesMatch_NoWarningLogged is the negative
// counterpart: when the peer DOES match the configured TrustedProxies (the
// healthy, expected case), no mismatch warning is logged at all.
func TestRateLimit_TrustedProxiesMatch_NoWarningLogged(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests:       1,
		Window:         time.Minute,
		Clock:          clock.Now,
		Logger:         logger,
		TrustedProxies: api.LoopbackTrustedProxies(),
	})(allowHandler())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, rateLimitedReq("127.0.0.1:9000", "203.0.113.1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if logged := buf.String(); strings.Contains(logged, "trust boundary misconfigured") {
		t.Errorf("log unexpectedly contains a mismatch warning for a trusted peer: %s", logged)
	}
}

// TestRateLimit_TrustedProxiesMismatch_ReLogsAfterInterval proves a persistent trust-boundary
// mismatch must stay observable for as long as it persists, not fall
// silent after the very first request the way a sync.Once-guarded warning
// would. Once the injected clock advances past the throttle interval, a
// further mismatched request must log again.
func TestRateLimit_TrustedProxiesMismatch_ReLogsAfterInterval(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests:       100,
		Window:         time.Minute,
		Clock:          clock.Now,
		Logger:         logger,
		TrustedProxies: api.TrustedProxies{netip.MustParsePrefix("192.0.2.0/24")},
	})(allowHandler())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, rateLimitedReq("203.0.113.5:1234", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("first request: status = %d, want %d", rec.Code, http.StatusOK)
	}
	if count := strings.Count(buf.String(), "trust boundary misconfigured"); count != 1 {
		t.Fatalf("after the first mismatched request: logged %d times, want 1", count)
	}

	// Still within the throttle interval: no second line yet.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, rateLimitedReq("203.0.113.5:1234", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("second request (same instant): status = %d, want %d", rec.Code, http.StatusOK)
	}
	if count := strings.Count(buf.String(), "trust boundary misconfigured"); count != 1 {
		t.Fatalf("still within the throttle interval: logged %d times, want 1 (no additional line yet)", count)
	}

	// Advance past the throttle interval: the still-ongoing mismatch must
	// produce a fresh log line, proving the condition stays observable
	// rather than being silenced forever after the first occurrence.
	clock.Advance(time.Minute)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, rateLimitedReq("203.0.113.5:1234", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("third request (after the throttle interval): status = %d, want %d", rec.Code, http.StatusOK)
	}
	if count := strings.Count(buf.String(), "trust boundary misconfigured"); count != 2 {
		t.Errorf("after the throttle interval elapsed: logged %d times, want 2 "+
			"(a persistent mismatch must stay observable, not fall silent after the first request)", count)
	}
}

// TestRateLimit_InvalidClientIP_ChargesSendingPeersBucket proves the
// invalid_client_ip path charges the sending peer. A trusted peer repeatedly
// sending a missing or malformed canonical header must exhaust a bucket keyed
// on its raw socket address and receive 429. This bounds handler work, not log
// volume: RateLimit sits inside Logging, so a 429 is logged like a 400.
func TestRateLimit_InvalidClientIP_ChargesSendingPeersBucket(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests:       2,
		Window:         time.Minute,
		Clock:          clock.Now,
		TrustedProxies: api.LoopbackTrustedProxies(),
	})(allowHandler())

	// A trusted peer sending no canonical header: cfg.Key fails, so this
	// takes the invalid_client_ip path every time.
	req := func() *http.Request { return rateLimitedReq("127.0.0.1:9000", "") }

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req())
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("request %d (within the peer's budget): status = %d, want %d", i, rec.Code, http.StatusBadRequest)
		}
		got := decodeErrorEnvelope(t, rec)
		if got.Code != "invalid_client_ip" {
			t.Errorf("request %d: error.code = %q, want %q", i, got.Code, "invalid_client_ip")
		}
	}

	// The peer's own (small, Requests=2) budget is now exhausted: the next
	// malformed-header request must be metered like any other over-budget
	// request, not another free 400.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd request (peer budget exhausted): status = %d, want %d — "+
			"an unbounded stream of malformed headers must eventually be metered, not stay free forever",
			rec.Code, http.StatusTooManyRequests)
	}
	if retryAfter := rec.Header().Get("Retry-After"); retryAfter == "" {
		t.Error("429 for the exhausted peer bucket is missing Retry-After")
	}
}

// TestRateLimit_InvalidClientIP_DifferentPeersMeteredIndependently proves
// the peer-keyed charge on the 400 path (see the test above) is scoped per
// sending peer, not one shared bucket for the whole invalid_client_ip
// path — otherwise one misbehaving trusted proxy could exhaust every other
// trusted proxy's allowance for legitimately reporting the same failure
// mode.
func TestRateLimit_InvalidClientIP_DifferentPeersMeteredIndependently(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 1,
		Window:   time.Minute,
		Clock:    clock.Now,
		TrustedProxies: api.TrustedProxies{
			netip.MustParsePrefix("127.0.0.1/32"),
			netip.MustParsePrefix("127.0.0.2/32"),
		},
	})(allowHandler())

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, rateLimitedReq("127.0.0.1:9000", ""))
	if rec1.Code != http.StatusBadRequest {
		t.Fatalf("peer 1, request 1: status = %d, want %d", rec1.Code, http.StatusBadRequest)
	}

	// A different trusted peer, same instant: must get its own budget, not
	// share peer 1's already-spent one.
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, rateLimitedReq("127.0.0.2:9000", ""))
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("peer 2, request 1: status = %d, want %d (must not share peer 1's exhausted budget)",
			rec2.Code, http.StatusBadRequest)
	}
}

// TestRateLimit_ClockRollback_DoesNotSuppressEvictionSweep proves the
// saturated-store eviction sweep is gated on the injected clock
// (nextSweepAt), so a backward clock
// step must not suppress sweeps for the whole rollback span. The limiter
// clamps the sweep clock to a monotonic high-water mark, so a rollback
// can neither skip a due sweep nor let reclaimable capacity go unreclaimed.
//
// The scenario drives the clock forward through real requests to a high
// water mark at which one tracked entry is fully refilled (reclaimable),
// then rolls the clock back an hour and admits a brand-new key. The due sweep
// must reclaim the idle entry instead of sending the new key to the spent
// overflow bucket.
func TestRateLimit_ClockRollback_DoesNotSuppressEvictionSweep(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 1,
		Window:   time.Second, // fast refill so an idle entry becomes reclaimable quickly
		Clock:    clock.Now,
		MaxKeys:  2,
	})(allowHandler())

	send := func(ip string) int {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, rateLimitedReq(ip+":1", ""))
		return rec.Code
	}

	// Fill the store to MaxKeys with keys A and B, then admit C: the store
	// is saturated, so C trips the first sweep (setting nextSweepAt to
	// Epoch+cooldown) and shares the overflow bucket, spending its one token.
	if got := send("203.0.113.1"); got != http.StatusOK { // A
		t.Fatalf("A at Epoch: status = %d, want %d", got, http.StatusOK)
	}
	if got := send("203.0.113.2"); got != http.StatusOK { // B
		t.Fatalf("B at Epoch: status = %d, want %d", got, http.StatusOK)
	}
	if got := send("203.0.113.3"); got != http.StatusOK { // C -> overflow (its single token)
		t.Fatalf("C at Epoch (overflow token): status = %d, want %d", got, http.StatusOK)
	}

	// Advance the clock five windows and touch B. This moves the limiter's
	// high-water mark forward WITHOUT triggering a sweep (B is found
	// directly), and leaves A idle since Epoch — so A is now fully refilled
	// and reclaimable at this later time, while B is freshly spent.
	clock.Advance(5 * time.Second)
	if got := send("203.0.113.2"); got != http.StatusOK { // B again, refilled
		t.Fatalf("B at Epoch+5s: status = %d, want %d", got, http.StatusOK)
	}

	// The clock jumps backward an hour (e.g. an NTP correction). A new key D
	// arrives. The sweep IS due — A has been reclaimable for five windows of
	// real time — so D must get a private bucket. Comparing the rolled-back
	// clock directly with nextSweepAt would skip the sweep and send D to the
	// overflow bucket whose only token C already spent.
	clock.Set(testutil.Epoch.Add(5*time.Second - time.Hour))
	if got := send("203.0.113.4"); got != http.StatusOK { // D
		t.Fatalf("D after a -1h clock step: status = %d, want %d — a backward clock step must not "+
			"suppress a due eviction sweep and starve a new key of a reclaimable slot", got, http.StatusOK)
	}
}
