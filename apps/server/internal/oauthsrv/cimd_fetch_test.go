package oauthsrv

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/dannyota/aboutme/apps/server/internal/config"
)

func cimdTestNow() time.Time {
	return time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
}

func cimdTestConfig() config.CIMDConfig {
	return config.CIMDConfig{Enabled: true, MaxURLBytes: 256, MaxBodyBytes: 5120, ConnectTimeout: 2 * time.Second, CompleteTimeout: 5 * time.Second,
		ConcurrentFetches: 4, AddressRequests: 30, AddressWindow: time.Hour, URLRequests: 6, URLWindow: time.Minute, GlobalRequests: 600, GlobalWindow: time.Hour,
		CacheFloor: 5 * time.Minute, CacheCeiling: time.Hour, MaxCacheEntries: 1000, MaxCacheBytes: 2 * 1024 * 1024}
}

func cimdTLSResolver(t *testing.T, handler http.Handler, clock func() time.Time) (*CIMDResolver, *httptest.Server) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	f, err := newCIMDResolver(cimdTestConfig(), "https://aboutme.vn", clock, 10000, func(a netip.Addr) bool { return a.IsLoopback() }, roots)
	if err != nil {
		t.Fatal(err)
	}
	// The trusted harness override preserves example.com certificate checks.
	f.cfg.DevStubAddr = server.Listener.Addr().String()
	return f, server
}

func cimdFixture(path string) string {
	return `{"client_id":"https://example.com` + path + `","client_name":"Fixture","redirect_uris":["http://localhost/callback"],"token_endpoint_auth_method":"none"}`
}

func TestCIMDFetchBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		valid   bool
	}{
		{"valid", func(w http.ResponseWriter, r *http.Request) {
			if r.Host != "example.com" || r.TLS.ServerName != "example.com" || r.Header.Get("Accept") != "application/json" || r.Header.Get("User-Agent") != "aboutme-cimd/1" || r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Accept-Encoding") != "" {
				t.Error("metadata fetch headers or TLS identity differ")
			}
			w.Header().Set("Content-Type", "application/client+json; charset=utf-8")
			if _, err := fmt.Fprint(w, cimdFixture(r.URL.Path)); err != nil {
				return
			}
		}, true},
		{"redirect", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://example.com/other", http.StatusFound)
		}, false},
		{"status", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }, false},
		{"type", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			if _, err := fmt.Fprint(w, cimdFixture(r.URL.Path)); err != nil {
				return
			}
		}, false},
		{"oversized stream", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if err := http.NewResponseController(w).Flush(); err != nil {
				return
			}
			if _, err := fmt.Fprint(w, strings.Repeat(" ", 5121)); err != nil {
				return
			}
		}, false},
		{"oversized length", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Length", "5121")
		}, false},
		{"compression", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Encoding", "gzip")
			if _, err := fmt.Fprint(w, cimdFixture(r.URL.Path)); err != nil {
				return
			}
		}, false},
		{"duplicate encoding", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Add("Content-Encoding", "identity")
			w.Header().Add("Content-Encoding", "gzip")
			if _, err := fmt.Fprint(w, cimdFixture(r.URL.Path)); err != nil {
				return
			}
		}, false},
		{"duplicate type", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Content-Type", "application/json")
			w.Header().Add("Content-Type", "text/html")
			if _, err := fmt.Fprint(w, cimdFixture(r.URL.Path)); err != nil {
				return
			}
		}, false},
		{"slow body", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if err := http.NewResponseController(w).Flush(); err != nil {
				return
			}
			<-r.Context().Done()
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _ := cimdTLSResolver(t, tc.handler, cimdTestNow)
			if tc.name == "slow body" {
				f.cfg.CompleteTimeout = 100 * time.Millisecond
			}
			_, err := f.Resolve(context.Background(), "https://example.com/metadata", netip.MustParseAddr("8.8.8.8"))
			if (err == nil) != tc.valid {
				t.Fatalf("fetch error = %v", err)
			}
			if err != nil && len(f.cache) != 0 {
				t.Fatal("failure cached")
			}
		})
	}
}

func TestCIMDTLSFailure(t *testing.T) {
	t.Parallel()
	f, _ := cimdTLSResolver(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { t.Error("TLS failure reached HTTP"); w.WriteHeader(200) }), cimdTestNow)
	trustedRoots := f.roots
	f.roots = x509.NewCertPool()
	if _, err := f.Resolve(context.Background(), "https://example.com/metadata", netip.MustParseAddr("8.8.8.8")); err == nil {
		t.Fatal("untrusted TLS accepted")
	}
	f.roots = trustedRoots
	if _, err := f.Resolve(context.Background(), "https://different.example/metadata", netip.MustParseAddr("8.8.8.8")); err == nil {
		t.Fatal("wrong TLS certificate name accepted")
	}
}

func TestCIMDCoalescedWaitersShareOneFetch(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{})
	release := make(chan struct{})
	waiterEntered := make(chan struct{})
	var clocks, calls atomic.Int32
	f, _ := cimdTLSResolver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(entered)
		<-release
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprint(w, cimdFixture(r.URL.Path)); err != nil {
			return
		}
	}), func() time.Time {
		if clocks.Add(1) == 2 {
			close(waiterEntered)
		}
		return cimdTestNow()
	})
	results := make(chan error, 2)
	resolve := func() {
		_, err := f.Resolve(context.Background(), "https://example.com/metadata", netip.MustParseAddr("8.8.8.8"))
		results <- err
	}
	go resolve()
	<-entered
	go resolve()
	<-waiterEntered
	close(release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("waiter consumed another fetch admission")
	}
}

func TestCIMDSharedFailureIsRemovedAndRestartRefetches(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	waiterEntered := make(chan struct{})
	var clocks, calls atomic.Int32
	f, _ := cimdTLSResolver(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		entered <- struct{}{}
		<-release
		w.WriteHeader(503)
	}), func() time.Time {
		if clocks.Add(1) == 2 {
			close(waiterEntered)
		}
		return cimdTestNow()
	})
	results := make(chan error, 2)
	resolve := func() {
		_, err := f.Resolve(context.Background(), "https://example.com/metadata", netip.MustParseAddr("8.8.8.8"))
		results <- err
	}
	go resolve()
	<-entered
	go resolve()
	<-waiterEntered
	close(release)
	for range 2 {
		if err := <-results; !errors.Is(err, errCIMDInvalid) {
			t.Fatal("waiter did not share the closed failure")
		}
	}
	if calls.Load() != 1 || len(f.flights) != 0 || len(f.cache) != 0 {
		t.Fatal("shared failure retained fetch or cache state")
	}
	if _, err := f.Resolve(context.Background(), "https://example.com/metadata", netip.MustParseAddr("8.8.8.8")); !errors.Is(err, errCIMDInvalid) || calls.Load() != 2 {
		t.Fatal("failed document was cached")
	}

	var successfulCalls atomic.Int32
	warm, _ := cimdTLSResolver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		successfulCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprint(w, cimdFixture(r.URL.Path)); err != nil {
			return
		}
	}), cimdTestNow)
	for range 2 {
		if _, err := warm.Resolve(context.Background(), "https://example.com/metadata", netip.MustParseAddr("8.8.8.8")); err != nil {
			t.Fatal(err)
		}
	}
	restarted, err := newCIMDResolver(warm.cfg, "https://aboutme.vn", cimdTestNow, 10000, warm.policy, warm.roots)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Resolve(context.Background(), "https://example.com/metadata", netip.MustParseAddr("8.8.8.8")); err != nil || successfulCalls.Load() != 2 {
		t.Fatal("restart reused process-local metadata")
	}
}

func TestCIMDSmallCacheEntriesStillObeyCountCap(t *testing.T) {
	t.Parallel()
	f, err := NewCIMDResolver(cimdTestConfig(), "https://aboutme.vn", cimdTestNow, 10000)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 1100 {
		f.cacheMetadata(CIMDMetadata{id: fmt.Sprintf("https://client.example/%d", i), host: "client.example", name: "Fixture", redirects: []string{"http://localhost/callback"}}, cimdTestNow().Add(time.Hour))
	}
	if len(f.cache) != 1000 || f.cacheBytes > 2*1024*1024 {
		t.Fatal("small metadata escaped the count cap")
	}
}

func TestCIMDCacheMemoryCountsMaximumMetadata(t *testing.T) {
	t.Parallel()
	f, err := NewCIMDResolver(cimdTestConfig(), "https://aboutme.vn", cimdTestNow, 10000)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 2000 {
		m := CIMDMetadata{id: fmt.Sprintf("https://client.example/%d", i), host: "client.example", name: strings.Repeat("😀", 64), redirects: make([]string, 5)}
		for j := range m.redirects {
			m.redirects[j] = "https://client.example/" + strings.Repeat("a", 490)
		}
		f.cacheMetadata(m, cimdTestNow().Add(time.Hour))
		if f.cacheBytes > f.cfg.MaxCacheBytes || len(f.cache) > f.cfg.MaxCacheEntries {
			t.Fatal("cache exceeded retained-memory or entry cap")
		}
	}
	if len(f.cache) >= 1000 {
		t.Fatal("maximum metadata ignored memory accounting")
	}
}

func TestCIMDCacheDoesNotRetainAuthorizationQueryBuffer(t *testing.T) {
	t.Parallel()
	f, _ := cimdTLSResolver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprint(w, cimdFixture(r.URL.Path)); err != nil {
			return
		}
	}), cimdTestNow)
	parent := strings.Repeat("x", 1024*1024) + "https://example.com/metadata"
	raw := parent[1024*1024:]
	metadata, err := f.Resolve(context.Background(), raw, netip.MustParseAddr("8.8.8.8"))
	if err != nil {
		t.Fatal(err)
	}
	if unsafe.StringData(metadata.id) == unsafe.StringData(raw) || unsafe.StringData(metadata.host) == unsafe.StringData(raw[8:]) {
		t.Fatal("cache retained the request's larger backing buffer")
	}
}

func TestCIMDFetchRateAdmissionPrecedesDNS(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	f, err := NewCIMDResolver(cimdTestConfig(), "https://aboutme.vn", func() time.Time { return now }, 10000)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	f.lookup = func(context.Context, string) ([]netip.Addr, error) { calls++; return nil, errCIMDInvalid }
	for range 6 {
		if _, resolveErr := f.Resolve(context.Background(), "https://client.example/metadata", netip.MustParseAddr("8.8.8.8")); resolveErr == nil {
			t.Fatal("failed DNS lookup succeeded")
		}
	}
	_, err = f.Resolve(context.Background(), "https://client.example/metadata", netip.MustParseAddr("8.8.8.8"))
	var limited *CIMDRateError
	if !errors.As(err, &limited) || limited.RetryAfter < 1 || calls != 6 {
		t.Fatal("rate refusal reached DNS or lacked Retry-After")
	}
}

func TestCIMDFetchAddressAndGlobalBudgets(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		limit   int
		address func(int) netip.Addr
	}{
		{"IPv4 address", 30, func(int) netip.Addr { return netip.MustParseAddr("8.8.8.8") }},
		{"IPv6 subnet", 30, func(i int) netip.Addr { return netip.MustParseAddr(fmt.Sprintf("2606:4700::%x", i+1)) }},
		{"global", 600, func(i int) netip.Addr { return netip.AddrFrom4([4]byte{8, 8, byte(i / 250), byte(i%250 + 1)}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
			f, err := NewCIMDResolver(cimdTestConfig(), "https://aboutme.vn", func() time.Time { return now }, 10000)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			f.lookup = func(context.Context, string) ([]netip.Addr, error) { calls++; return nil, errCIMDInvalid }
			for i := range tc.limit {
				_, resolveErr := f.Resolve(context.Background(), fmt.Sprintf("https://client.example/%d", i), tc.address(i))
				if !errors.Is(resolveErr, errCIMDInvalid) {
					t.Fatalf("fetch attempt %d: %v", i, resolveErr)
				}
			}
			_, err = f.Resolve(context.Background(), "https://client.example/excess", tc.address(tc.limit))
			var limited *CIMDRateError
			if !errors.As(err, &limited) || limited.RetryAfter != int(time.Hour/time.Second)/tc.limit || calls != tc.limit {
				t.Fatal("fetch budget did not refuse before DNS")
			}
		})
	}
}

func TestCIMDDNSRebindingCannotReplacePinnedAddress(t *testing.T) {
	t.Parallel()
	f, server := cimdTLSResolver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprint(w, cimdFixture(r.URL.Path)); err != nil {
			return
		}
	}), cimdTestNow)
	f.cfg.DevStubAddr = ""
	f.policy = cimdAddressAllowed
	lookups := 0
	f.lookup = func(context.Context, string) ([]netip.Addr, error) {
		lookups++
		if lookups > 1 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}
	f.dialAddress = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "8.8.8.8:443" {
			t.Error("dial did not receive the approved literal")
		}
		return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
	}
	if _, err := f.Resolve(context.Background(), "https://example.com/metadata", netip.MustParseAddr("8.8.8.8")); err != nil || lookups != 1 {
		t.Fatalf("pinned fetch: %v, lookups %d", err, lookups)
	}
	approved := map[netip.Addr]bool{netip.MustParseAddr("8.8.8.8"): true}
	for _, raw := range []string{"example.com:443", "127.0.0.1:443", "8.8.8.8:80", "192.31.196.1:443"} {
		if err := f.checkConnectAddress(raw, 443, approved); err == nil {
			t.Fatal("unsafe connect address accepted")
		}
	}
	approved[netip.MustParseAddr("192.31.196.1")] = true
	if err := f.checkConnectAddress("192.31.196.1:443", 443, approved); err == nil {
		t.Fatal("connect skipped production address policy")
	}
}

func TestCIMDMixedDNSFailsBeforeConnect(t *testing.T) {
	t.Parallel()
	for _, fixture := range cimdAddressFixtures() {
		if fixture.allowed {
			continue
		}
		for _, public := range []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("2606:4700::1111")} {
			for _, answers := range [][]netip.Addr{{public, fixture.address}, {fixture.address, public}} {
				f := cimdPolicyResolver(t)
				var lookups, dials int
				f.lookup = func(context.Context, string) ([]netip.Addr, error) { lookups++; return answers, nil }
				f.dialAddress = func(context.Context, string, string) (net.Conn, error) { dials++; return nil, errCIMDInvalid }
				if _, err := f.Resolve(t.Context(), "https://example.com/metadata", public); err == nil || lookups != 1 || dials != 0 {
					t.Fatalf("mixed answer %v: error %v, lookups %d, dials %d", answers, err, lookups, dials)
				}
			}
		}
	}
}

func cimdPolicyResolver(t *testing.T) *CIMDResolver {
	t.Helper()
	f, err := NewCIMDResolver(cimdTestConfig(), "https://aboutme.vn", cimdTestNow, 10000)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestCIMDAddressFixturesAtDNSAndConnect(t *testing.T) {
	t.Parallel()
	for _, fixture := range cimdAddressFixtures() {
		f := cimdPolicyResolver(t)
		var lookups int
		var dialed []string
		f.lookup = func(context.Context, string) ([]netip.Addr, error) {
			lookups++
			return []netip.Addr{fixture.address}, nil
		}
		f.dialAddress = func(_ context.Context, _ string, address string) (net.Conn, error) {
			dialed = append(dialed, address)
			return nil, errCIMDInvalid
		}
		if _, err := f.Resolve(t.Context(), "https://example.com/metadata", netip.MustParseAddr("8.8.8.8")); err == nil || lookups != 1 {
			t.Fatalf("DNS fixture %s: error %v, lookups %d", fixture.address, err, lookups)
		}
		if fixture.allowed {
			if len(dialed) != 1 || dialed[0] != netip.AddrPortFrom(fixture.address.Unmap(), 443).String() {
				t.Fatalf("allowed fixture %s dialed %v", fixture.address, dialed)
			}
		} else if len(dialed) != 0 {
			t.Fatalf("refused fixture %s dialed %v", fixture.address, dialed)
		}
		// Membership alone cannot bypass the connect-time address policy.
		approved := map[netip.Addr]bool{fixture.address.Unmap(): true}
		if err := f.checkConnectAddress(netip.AddrPortFrom(fixture.address, 443).String(), 443, approved); (err == nil) != fixture.allowed {
			t.Fatalf("connect fixture %s: %v", fixture.address, err)
		}
		dialed = nil
		_, _ = f.connect(t.Context(), "tcp", netip.AddrPortFrom(fixture.address, 443).String(), 443, approved)
		if fixture.allowed && (len(dialed) != 1 || dialed[0] != netip.AddrPortFrom(fixture.address.Unmap(), 443).String()) || !fixture.allowed && len(dialed) != 0 {
			t.Fatalf("connect fixture %s dialed %v", fixture.address, dialed)
		}
	}
}

func TestCIMDInvalidCacheEntryReleasesLock(t *testing.T) {
	t.Parallel()
	f := cimdPolicyResolver(t)
	f.cache["https://example.com/metadata"] = f.lru.PushFront("invalid")
	func() {
		defer func() {
			if recover() == nil {
				t.Error("invalid cache entry did not panic")
			}
		}()
		_, _ = f.Resolve(t.Context(), "https://example.com/metadata", netip.MustParseAddr("8.8.8.8"))
	}()
	if !f.mu.TryLock() {
		t.Fatal("invalid cache entry retained the resolver lock")
	}
	f.mu.Unlock()
}

func TestCIMDCoalescingCancellationAndSaturation(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	var calls atomic.Int32
	f, _ := cimdTLSResolver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprint(w, cimdFixture(r.URL.Path)); err != nil {
			return
		}
	}), cimdTestNow)
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 5)
	address := netip.MustParseAddr("8.8.8.8")
	go func() { _, err := f.Resolve(ctx, "https://example.com/a", address); result <- err }()
	<-entered
	for _, path := range []string{"/b", "/c", "/d"} {
		go func() { _, err := f.Resolve(context.Background(), "https://example.com"+path, address); result <- err }()
		<-entered
	}
	_, err := f.Resolve(context.Background(), "https://example.com/e", address)
	var limited *CIMDRateError
	if !errors.As(err, &limited) || limited.RetryAfter != 5 {
		t.Fatalf("saturation = %v", err)
	}
	cancel()
	if err := <-result; err == nil {
		t.Fatal("canceled waiter succeeded")
	}
	f.mu.Lock()
	flights := len(f.flights)
	f.mu.Unlock()
	if flights != 4 || calls.Load() != 4 {
		t.Fatal("waiter cancellation stopped shared fetch")
	}
}

func TestCIMDExpiryAndLRUEviction(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	var clock atomic.Int64
	clock.Store(now.UnixNano())
	var calls atomic.Int32
	var fail atomic.Bool
	f, _ := cimdTLSResolver(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if fail.Load() {
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := fmt.Fprint(w, cimdFixture(r.URL.Path)); err != nil {
			return
		}
	}), func() time.Time { return time.Unix(0, clock.Load()) })
	f.cfg.MaxCacheEntries = 2
	address := netip.MustParseAddr("8.8.8.8")
	resolve := func(path string) error {
		_, err := f.Resolve(context.Background(), "https://example.com"+path, address)
		return err
	}
	for _, path := range []string{"/a", "/b", "/a", "/c"} {
		if err := resolve(path); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 3 {
		t.Fatal("cache hit fetched")
	}
	if err := resolve("/b"); err != nil || calls.Load() != 4 {
		t.Fatal("LRU eviction did not refetch")
	}
	clock.Store(now.Add(5 * time.Minute).UnixNano())
	fail.Store(true)
	if err := resolve("/b"); err == nil {
		t.Fatal("expired entry served stale")
	}
	if err := resolve("/b"); err == nil || calls.Load() != 6 {
		t.Fatal("failed refetch was cached")
	}
}

func TestCIMDRefusedMissesPreserveOtherAddressesGlobalBudget(t *testing.T) {
	t.Parallel()
	for _, bucket := range []string{"address", "URL"} {
		t.Run(bucket, func(t *testing.T) {
			f := cimdPolicyResolver(t)
			calls := 0
			f.lookup = func(context.Context, string) ([]netip.Addr, error) { calls++; return nil, errCIMDInvalid }
			first := netip.MustParseAddr("8.8.8.8")
			limit := 30
			if bucket == "URL" {
				limit = 6
			}
			urlFor := func(i int) string {
				if bucket == "URL" {
					return "https://example.com/metadata"
				}
				return fmt.Sprintf("https://example.com/%d", i)
			}
			for i := range limit {
				if _, err := f.Resolve(t.Context(), urlFor(i), first); !errors.Is(err, errCIMDInvalid) {
					t.Fatal("admitted miss failed before DNS", err)
				}
			}
			for i := range 600 {
				address := first
				if bucket == "URL" {
					address = netip.AddrFrom4([4]byte{8, 9, byte(i / 250), byte(i%250 + 1)})
				}
				_, err := f.Resolve(t.Context(), urlFor(limit+i), address)
				var limited *CIMDRateError
				if !errors.As(err, &limited) {
					t.Fatal("exhausted bucket admitted a miss")
				}
			}
			if _, err := f.Resolve(t.Context(), "https://example.com/other", netip.MustParseAddr("1.1.1.1")); !errors.Is(err, errCIMDInvalid) || calls != limit+1 {
				t.Fatal("refused misses spent another address's global fetch budget", err)
			}
		})
	}
}

func TestCIMDCombinedAdmissionKeepsMaximumRetryAfter(t *testing.T) {
	t.Parallel()
	cfg := cimdTestConfig()
	cfg.GlobalRequests, cfg.GlobalWindow = 1, 3*time.Hour
	f, err := NewCIMDResolver(cfg, "https://aboutme.vn", cimdTestNow, 10000)
	if err != nil {
		t.Fatal(err)
	}
	now := cimdTestNow()
	for range cfg.AddressRequests {
		if ok, _ := f.addressRate.Admit(now, registerAddressKey(netip.MustParseAddr("8.8.8.8"))); !ok {
			t.Fatal("address setup refused")
		}
	}
	if ok, _ := f.globalRate.Admit(now, "global"); !ok {
		t.Fatal("global setup refused")
	}
	_, err = f.Resolve(t.Context(), "https://example.com/metadata", netip.MustParseAddr("8.8.8.8"))
	var limited *CIMDRateError
	if !errors.As(err, &limited) || limited.RetryAfter != int((3*time.Hour).Seconds()) {
		t.Fatal("combined admission lost the largest Retry-After", err)
	}
}
