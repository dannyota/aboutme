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

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

func allowHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

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

func TestRateLimit_UntrustedPeerForgedHeader_DoesNotBypassLimit(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 2,
		Window:   time.Minute,
		Clock:    clock.Now,
	})(allowHandler())

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

func TestRateLimit_TrustedPeer_KeysByCanonicalHeader(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests:       1,
		Window:         time.Minute,
		Clock:          clock.Now,
		TrustedProxies: api.LoopbackTrustedProxies(),
	})(allowHandler())

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, rateLimitedReq("127.0.0.1:9000", "203.0.113.1"))
	if rec1.Code != http.StatusOK {
		t.Fatalf("first client: status = %d, want %d", rec1.Code, http.StatusOK)
	}

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, rateLimitedReq("127.0.0.1:9000", "203.0.113.2"))
	if rec2.Code != http.StatusOK {
		t.Fatalf("second (different) client via same proxy: status = %d, want %d; "+
			"distinct %s values must not share a bucket", rec2.Code, http.StatusOK, api.TrustedClientIPHeader)
	}

	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, rateLimitedReq("127.0.0.1:9000", "203.0.113.1"))
	if rec3.Code != http.StatusTooManyRequests {
		t.Fatalf("first client again: status = %d, want %d", rec3.Code, http.StatusTooManyRequests)
	}
}

func TestRateLimit_MissingOrMalformedCanonicalHeader_Rejected(t *testing.T) {
	t.Parallel()

	cases := []string{
		"",
		"not-an-ip",
		"999.999.999.999",
		"1.2.3.4, 5.6.7.8",
		"203.0.113.5:8080",
		"[::1]:8080",
		strings.Repeat("9", 10_000),
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

func TestRateLimit_UntrustedPeer_UnparseableRemoteAddr_Rejected(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 1,
		Window:   time.Minute,
		Clock:    clock.Now,
	})(allowHandler())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, rateLimitedReq("not-an-address", ""))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

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
	viewer.RemoteAddr = "127.0.0.1:443"
	viewer.Header.Set(api.TrustedClientIPHeader, "198.51.100.42")
	viewer.Header.Set("X-Forwarded-For", "203.0.113.66, 70.132.0.1")

	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, viewer)
	if rec1.Code != http.StatusOK {
		t.Fatalf("viewer request 1: status = %d, want %d", rec1.Code, http.StatusOK)
	}

	otherViewer := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	otherViewer.RemoteAddr = "127.0.0.1:443"
	otherViewer.Header.Set(api.TrustedClientIPHeader, "198.51.100.99")
	otherViewer.Header.Set("X-Forwarded-For", "203.0.113.66, 70.132.0.1")

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, otherViewer)
	if rec2.Code != http.StatusOK {
		t.Fatalf("second viewer behind the same edge: status = %d, want %d; "+
			"must not share a bucket with the first viewer", rec2.Code, http.StatusOK)
	}

	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, viewer)
	if rec3.Code != http.StatusTooManyRequests {
		t.Fatalf("first viewer again: status = %d, want %d", rec3.Code, http.StatusTooManyRequests)
	}
}

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

	clock.Advance(time.Minute)

	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, rateLimitedReq("203.0.113.5:1234", ""))
	if rec3.Code != http.StatusOK {
		t.Fatalf("request after advancing the clock past Window: status = %d, want %d", rec3.Code, http.StatusOK)
	}
}

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

	clock.Advance(time.Nanosecond)
	recAt := httptest.NewRecorder()
	handler.ServeHTTP(recAt, req())
	if recAt.Code != http.StatusOK {
		t.Fatalf("exactly at Window elapsed: status = %d, want %d (fully refilled)", recAt.Code, http.StatusOK)
	}
}

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

	clock.Set(testutil.Epoch.Add(-30 * time.Second))
	recRolledBack := httptest.NewRecorder()
	handler.ServeHTTP(recRolledBack, req())
	if recRolledBack.Code != http.StatusTooManyRequests {
		t.Fatalf("immediately after rollback: status = %d, want %d (no tokens were consumed to fabricate one)",
			recRolledBack.Code, http.StatusTooManyRequests)
	}

	clock.Set(testutil.Epoch.Add(2 * time.Minute))
	recAfter := httptest.NewRecorder()
	handler.ServeHTTP(recAfter, req())
	if recAfter.Code != http.StatusOK {
		t.Fatalf("after clock resumes forward past Window: status = %d, want %d; "+
			"a prior backward step must not freeze refill permanently", recAfter.Code, http.StatusOK)
	}
}

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

	clock.Advance(time.Minute)

	recB1 := httptest.NewRecorder()
	handler.ServeHTTP(recB1, rateLimitedReq("203.0.113.20:1", ""))
	if recB1.Code != http.StatusOK {
		t.Fatalf("key B, request 1 (after A expired): status = %d, want %d", recB1.Code, http.StatusOK)
	}
}

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

func TestRateLimit_KeyStore_FullOfActiveEntries_OverflowsInsteadOfEvicting(t *testing.T) {
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

	recB1 := httptest.NewRecorder()
	handler.ServeHTTP(recB1, rateLimitedReq("203.0.113.20:1", ""))
	if recB1.Code != http.StatusOK {
		t.Fatalf("key B, request 1 (via overflow): status = %d, want %d", recB1.Code, http.StatusOK)
	}

	recA2 := httptest.NewRecorder()
	handler.ServeHTTP(recA2, rateLimitedReq("203.0.113.10:1", ""))
	if recA2.Code != http.StatusTooManyRequests {
		t.Fatalf("key A, request 2: status = %d, want %d; "+
			"an active entry must never be evicted to admit a new key", recA2.Code, http.StatusTooManyRequests)
	}
}

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

	for i := 0; i < maxKeys; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, rateLimitedReq("203.0.113."+strconv.Itoa(i)+":1", ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("filling store, key %d: status = %d, want %d", i, rec.Code, http.StatusOK)
		}
	}

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

func TestIPKeyFunc_TrustedPeerMissingHeader_Fails(t *testing.T) {
	t.Parallel()

	_, ok := api.IPKeyFunc(rateLimitedReq("127.0.0.1:9000", ""), api.LoopbackTrustedProxies())
	if ok {
		t.Fatal("IPKeyFunc ok = true for a trusted peer with no canonical header, want false")
	}
}

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

func TestClientIP_TrustedProxyMissingHeader_FailsClosed(t *testing.T) {
	t.Parallel()

	_, ok := api.ClientIP(rateLimitedReq("127.0.0.1:9000", ""), api.LoopbackTrustedProxies())
	if ok {
		t.Error("ClientIP ok = true for a trusted peer with no canonical header, want false (fail closed)")
	}
}

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

	sameAcctDifferentIP := rateLimitedReq("198.51.100.1:1", "")
	sameAcctDifferentIP = sameAcctDifferentIP.WithContext(api.WithAccountID(sameAcctDifferentIP.Context(), "acct-alice"))
	if got, ok := api.AccountKeyFunc(sameAcctDifferentIP, nil); got != a || !ok {
		t.Errorf("AccountKeyFunc for the same account from a different IP = (%q, %v), want (%q, true)", got, ok, a)
	}
}

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

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, reqFor("acct-alice", "203.0.113.2:1"))
	if rec2.Code != http.StatusOK {
		t.Fatalf("alice from IP 2: status = %d, want %d", rec2.Code, http.StatusOK)
	}

	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, reqFor("acct-bob", "203.0.113.1:1"))
	if rec3.Code != http.StatusOK {
		t.Fatalf("bob from IP 1: status = %d, want %d", rec3.Code, http.StatusOK)
	}

	rec4 := httptest.NewRecorder()
	handler.ServeHTTP(rec4, reqFor("acct-alice", "203.0.113.1:1"))
	if rec4.Code != http.StatusTooManyRequests {
		t.Fatalf("alice from IP 1 again: status = %d, want %d", rec4.Code, http.StatusTooManyRequests)
	}
}

func TestCompositeKeyFunc_ComponentFailurePropagates(t *testing.T) {
	t.Parallel()

	key := api.CompositeKeyFunc(api.AccountKeyFunc, api.IPKeyFunc)

	req := rateLimitedReq("127.0.0.1:9000", "")
	req = req.WithContext(api.WithAccountID(req.Context(), "acct-alice"))

	if _, ok := key(req, api.LoopbackTrustedProxies()); ok {
		t.Fatal("CompositeKeyFunc ok = true when a component KeyFunc failed, want false")
	}
}

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

	req := rateLimitedReq("127.0.0.1:9000", "")
	req = req.WithContext(api.WithAccountID(req.Context(), "acct-alice"))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestRateLimit_RejectedRequestsDoNotOweTokens(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 2,
		Window:   time.Minute,
		Clock:    clock.Now,
	})(allowHandler())

	req := func() *http.Request { return rateLimitedReq("203.0.113.5:1234", "") }

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req())
		if rec.Code != http.StatusOK {
			t.Fatalf("admitted request %d: status = %d, want %d", i, rec.Code, http.StatusOK)
		}
	}

	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req())
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("rejected request %d: status = %d, want %d", i, rec.Code, http.StatusTooManyRequests)
		}
	}

	clock.Advance(time.Minute)

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

func TestRateLimit_SaturatedStore_ScanIsAmortizedNotPerRequest(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 1,
		Window:   time.Millisecond,
		Clock:    clock.Now,
		MaxKeys:  1,
	})(allowHandler())

	recA := httptest.NewRecorder()
	handler.ServeHTTP(recA, rateLimitedReq("203.0.113.10:1", ""))
	if recA.Code != http.StatusOK {
		t.Fatalf("key A: status = %d, want %d", recA.Code, http.StatusOK)
	}

	recX1 := httptest.NewRecorder()
	handler.ServeHTTP(recX1, rateLimitedReq("203.0.113.20:1", ""))
	if recX1.Code != http.StatusOK {
		t.Fatalf("key X1 (first overflow use): status = %d, want %d", recX1.Code, http.StatusOK)
	}

	clock.Advance(time.Millisecond)

	recX2 := httptest.NewRecorder()
	handler.ServeHTTP(recX2, rateLimitedReq("203.0.113.30:1", ""))
	if recX2.Code != http.StatusOK {
		t.Fatalf("key X2: status = %d, want %d", recX2.Code, http.StatusOK)
	}

	recX3 := httptest.NewRecorder()
	handler.ServeHTTP(recX3, rateLimitedReq("203.0.113.40:1", ""))
	if recX3.Code != http.StatusTooManyRequests {
		t.Fatalf("key X3: status = %d, want %d; a saturated store must not re-scan "+
			"(and so reclaim a slot) on every single request", recX3.Code, http.StatusTooManyRequests)
	}
}

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

func TestRateLimit_ConcurrentDistinctKeys_AtomicAdmission_MaxKeysOne(t *testing.T) {
	t.Parallel()

	const trials = 30
	const distinctKeys = 50

	for trial := 0; trial < trials; trial++ {
		clock := testutil.NewClockAtEpoch()
		handler := api.RateLimit(api.RateLimiterConfig{
			Requests: 1,
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
				<-start
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, rateLimitedReq("203.0.113."+strconv.Itoa(i)+":1", ""))
				if rec.Code == http.StatusOK {
					admitted.Add(1)
				}
			}(i)
		}
		close(start)
		wg.Wait()

		if got := admitted.Load(); got != 2 {
			t.Fatalf("trial %d: admitted = %d of %d concurrent distinct keys at MaxKeys=1, want exactly 2 "+
				"(one private entry + one shared overflow token); non-atomic admission lets "+
				"each key evict the previous one and grant itself a fresh private bucket instead",
				trial, got, distinctKeys)
		}
	}
}

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

		clock.Advance(time.Minute)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req())
		if rec.Code != http.StatusOK {
			t.Fatalf("round %d: post-Window admission after %d concurrent rejections: status = %d, want %d "+
				"(concurrent rejections must never create phantom debt)", round, concurrentRejections, rec.Code, http.StatusOK)
		}
	}
}

func TestRateLimit_ClockRollback_NoEarlyTokenGranted_Regression(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 1,
		Window:   60 * time.Second,
		Clock:    clock.Now,
	})(allowHandler())

	req := func() *http.Request { return rateLimitedReq("203.0.113.5:1234", "") }

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	if rec.Code != http.StatusOK {
		t.Fatalf("t=0 admission: status = %d, want %d", rec.Code, http.StatusOK)
	}

	clock.Set(testutil.Epoch.Add(100 * time.Second))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	if rec.Code != http.StatusOK {
		t.Fatalf("t=100s admission: status = %d, want %d", rec.Code, http.StatusOK)
	}

	clock.Set(testutil.Epoch.Add(70 * time.Second))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("t=70s (rolled back): status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}

	clock.Set(testutil.Epoch.Add(130 * time.Second))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("t=130s (30s before the real due time): status = %d, want %d; "+
			"a clock rollback must never grant a token early", rec.Code, http.StatusTooManyRequests)
	}

	clock.Set(testutil.Epoch.Add(160 * time.Second))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	if rec.Code != http.StatusOK {
		t.Fatalf("t=160s (the real due time): status = %d, want %d; recovery must still work", rec.Code, http.StatusOK)
	}
}

func TestRateLimit_TrustedProxiesMismatch_LogsWarningOnceNotPerRequest(t *testing.T) {
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

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, rateLimitedReq("203.0.113.5:1234", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("second request (same instant): status = %d, want %d", rec.Code, http.StatusOK)
	}
	if count := strings.Count(buf.String(), "trust boundary misconfigured"); count != 1 {
		t.Fatalf("still within the throttle interval: logged %d times, want 1 (no additional line yet)", count)
	}

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

func TestRateLimit_InvalidClientIP_ChargesSendingPeersBucket(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests:       2,
		Window:         time.Minute,
		Clock:          clock.Now,
		TrustedProxies: api.LoopbackTrustedProxies(),
	})(allowHandler())

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

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd request (peer budget exhausted): status = %d, want %d; "+
			"an unbounded stream of malformed headers must eventually be metered, not stay free forever",
			rec.Code, http.StatusTooManyRequests)
	}
	if retryAfter := rec.Header().Get("Retry-After"); retryAfter == "" {
		t.Error("429 for the exhausted peer bucket is missing Retry-After")
	}
}

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

	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, rateLimitedReq("127.0.0.2:9000", ""))
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("peer 2, request 1: status = %d, want %d (must not share peer 1's exhausted budget)",
			rec2.Code, http.StatusBadRequest)
	}
}

func TestRateLimit_ClockRollback_DoesNotSuppressEvictionSweep(t *testing.T) {
	t.Parallel()

	clock := testutil.NewClockAtEpoch()
	handler := api.RateLimit(api.RateLimiterConfig{
		Requests: 1,
		Window:   time.Second,
		Clock:    clock.Now,
		MaxKeys:  2,
	})(allowHandler())

	send := func(ip string) int {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, rateLimitedReq(ip+":1", ""))
		return rec.Code
	}

	if got := send("203.0.113.1"); got != http.StatusOK {
		t.Fatalf("A at Epoch: status = %d, want %d", got, http.StatusOK)
	}
	if got := send("203.0.113.2"); got != http.StatusOK {
		t.Fatalf("B at Epoch: status = %d, want %d", got, http.StatusOK)
	}
	if got := send("203.0.113.3"); got != http.StatusOK {
		t.Fatalf("C at Epoch (overflow token): status = %d, want %d", got, http.StatusOK)
	}

	clock.Advance(5 * time.Second)
	if got := send("203.0.113.2"); got != http.StatusOK {
		t.Fatalf("B at Epoch+5s: status = %d, want %d", got, http.StatusOK)
	}

	clock.Set(testutil.Epoch.Add(5*time.Second - time.Hour))
	if got := send("203.0.113.4"); got != http.StatusOK {
		t.Fatalf("D after a -1h clock step: status = %d, want %d; a backward clock step must not "+
			"suppress a due eviction sweep and starve a new key of a reclaimable slot", got, http.StatusOK)
	}
}

func TestPasswordRateBoundedLimiter_AllowsWithinBudgetAndRejectsOver(t *testing.T) {
	t.Parallel()

	l := api.NewBoundedRateLimiter(api.RateLimiterConfig{
		Requests: 2,
		Window:   time.Minute,
	})
	now := testutil.Epoch

	for i := 0; i < 2; i++ {
		allowed, retry := l.Admit(now, "203.0.113.5")
		if !allowed || retry != 0 {
			t.Fatalf("request %d: (allowed=%v, retry=%d), want (true, 0)", i, allowed, retry)
		}
	}
	allowed, retry := l.Admit(now, "203.0.113.5")
	if allowed || retry < 1 {
		t.Fatalf("over-budget: (allowed=%v, retry=%d), want (false, >= 1)", allowed, retry)
	}

	l2 := api.NewBoundedRateLimiter(api.RateLimiterConfig{Requests: 1, Window: time.Minute})
	if allowed, _ := l2.Admit(now, "k"); !allowed {
		t.Fatal("initial admission denied")
	}
	if allowed, _ := l2.Admit(now, "k"); allowed {
		t.Fatal("over-budget admission allowed")
	}
	if allowed, _ := l2.Admit(now.Add(time.Minute), "k"); !allowed {
		t.Fatal("admission after a full Window denied; a rejected request must not owe a token")
	}
}

func TestPasswordRateBoundedLimiter_DefaultsToTenThousandKeys(t *testing.T) {
	t.Parallel()

	l := api.NewBoundedRateLimiter(api.RateLimiterConfig{
		Requests: 1,
		Window:   time.Minute,
	})
	now := testutil.Epoch

	for i := 0; i < 10000; i++ {
		if allowed, _ := l.Admit(now, "k:"+strconv.Itoa(i)); !allowed {
			t.Fatalf("distinct key %d denied while the store had room", i)
		}
	}

	if allowed, retry := l.Admit(now, "overflow-1"); !allowed {
		t.Fatalf("first overflow key denied (allowed=%v, retry=%d); want one shared overflow grant", allowed, retry)
	}
	if allowed, retry := l.Admit(now, "overflow-2"); allowed {
		t.Fatal("second overflow key allowed via the already-spent overflow bucket")
	} else if retry < 1 {
		t.Fatalf("rejected overflow admission retry = %d, want >= 1", retry)
	}

	if allowed, _ := l.Admit(now, "k:0"); allowed {
		t.Fatal("k:0 admitted again after its single token was spent")
	}
}

func TestBoundedRateLimiterCheckDoesNotConsumeTokens(t *testing.T) {
	t.Parallel()
	limiter := api.NewBoundedRateLimiter(api.RateLimiterConfig{Requests: 1, Window: time.Minute, MaxKeys: 1})
	now := testutil.Epoch
	for range 10 {
		if allowed, retry := limiter.Check(now, "tracked"); !allowed || retry != 0 {
			t.Fatal("checking a fresh bucket spent its token")
		}
	}
	if allowed, _ := limiter.Admit(now, "tracked"); !allowed {
		t.Fatal("checks consumed the tracked budget")
	}
	for range 10 {
		if allowed, retry := limiter.Check(now.Add(-time.Minute), "tracked"); allowed || retry != 60 {
			t.Fatal("check ignored debt or a clock rollback")
		}
		if allowed, retry := limiter.Check(now, "overflow"); !allowed || retry != 0 {
			t.Fatal("checking overflow spent its token")
		}
	}
	if allowed, _ := limiter.Admit(now, "overflow"); !allowed {
		t.Fatal("checks consumed the overflow budget")
	}
	if allowed, retry := limiter.Check(now, "another overflow"); allowed || retry != 60 {
		t.Fatal("check bypassed shared overflow debt")
	}
	if allowed, _ := limiter.Check(now.Add(time.Minute), "tracked"); !allowed {
		t.Fatal("check left debt after refill")
	}
	if allowed, _ := limiter.Admit(now.Add(time.Minute), "tracked"); !allowed {
		t.Fatal("refill check spent its token")
	}
}
