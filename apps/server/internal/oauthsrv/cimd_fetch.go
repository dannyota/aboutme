package oauthsrv

import (
	"container/list"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/config"
)

// CIMDRateError carries only the closed admission failure and retry duration.
type CIMDRateError struct{ RetryAfter int }

func (e *CIMDRateError) Error() string { return "oauth client metadata rate limited" }

type cimdCacheEntry struct {
	metadata CIMDMetadata
	expires  time.Time
	bytes    int
}
type cimdFlight struct {
	done     chan struct{}
	metadata CIMDMetadata
	err      error
}

// CIMDResolver owns one process-local cache, bounded admission, and fetch set.
type CIMDResolver struct {
	cfg                              config.CIMDConfig
	publicHost                       string
	clock                            func() time.Time
	lookup                           func(context.Context, string) ([]netip.Addr, error)
	policy                           func(netip.Addr) bool
	roots                            *x509.CertPool
	dialAddress                      func(context.Context, string, string) (net.Conn, error)
	addressRate, urlRate, globalRate *api.BoundedRateLimiter
	mu                               sync.Mutex
	cache                            map[string]*list.Element
	lru                              list.List
	cacheBytes                       int
	cacheBaseBytes                   int
	flights                          map[string]*cimdFlight
}

// NewCIMDResolver uses system trust and the complete production address policy.
func NewCIMDResolver(cfg config.CIMDConfig, publicOrigin string, clock func() time.Time, maxKeys int) (*CIMDResolver, error) {
	return newCIMDResolver(cfg, publicOrigin, clock, maxKeys, cimdAddressAllowed, nil)
}

func newCIMDResolver(cfg config.CIMDConfig, publicOrigin string, clock func() time.Time, maxKeys int, policy func(netip.Addr) bool, roots *x509.CertPool) (*CIMDResolver, error) {
	u, err := url.Parse(publicOrigin)
	if err != nil || u.Hostname() == "" || clock == nil || policy == nil || maxKeys <= 0 || cfg.ConnectTimeout <= 0 || cfg.CompleteTimeout <= 0 || cfg.ConcurrentFetches <= 0 || cfg.MaxCacheEntries <= 0 || cfg.MaxCacheBytes < cfg.MaxCacheEntries*128 || cfg.MaxBodyBytes != 5120 || cfg.MaxURLBytes != 256 || cfg.CacheFloor <= 0 || cfg.CacheCeiling < cfg.CacheFloor || cfg.AddressRequests <= 0 || cfg.AddressWindow <= 0 || cfg.URLRequests <= 0 || cfg.URLWindow <= 0 || cfg.GlobalRequests <= 0 || cfg.GlobalWindow <= 0 {
		return nil, errCIMDInvalid
	}
	bucket := func(requests int, window time.Duration) *api.BoundedRateLimiter {
		return api.NewBoundedRateLimiter(api.RateLimiterConfig{Requests: requests, Window: window, MaxKeys: maxKeys})
	}
	return &CIMDResolver{
		cfg: cfg, publicHost: u.Hostname(), clock: clock, policy: policy, roots: roots,
		lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		},
		addressRate: bucket(cfg.AddressRequests, cfg.AddressWindow), urlRate: bucket(cfg.URLRequests, cfg.URLWindow), globalRate: bucket(cfg.GlobalRequests, cfg.GlobalWindow),
		cache: make(map[string]*list.Element, cfg.MaxCacheEntries), flights: map[string]*cimdFlight{},
		cacheBytes: cfg.MaxCacheEntries * 128, cacheBaseBytes: cfg.MaxCacheEntries * 128,
	}, nil
}

func cloneCIMD(m CIMDMetadata) CIMDMetadata {
	m.redirects = append([]string(nil), m.redirects...)
	return m
}

// Resolve never queues distinct misses. Shared fetch cancellation is independent
// of any one waiter, and its complete deadline starts before DNS.
func (f *CIMDResolver) Resolve(ctx context.Context, raw string, address netip.Addr) (CIMDMetadata, error) {
	if len(raw) > f.cfg.MaxURLBytes || ctx.Err() != nil {
		return CIMDMetadata{}, errCIMDInvalid
	}
	// Query decoding can return a substring of a much larger request buffer.
	// Own the bounded identifier before retaining its URL and hostname slices.
	raw = strings.Clone(raw)
	u, err := parseCIMDURL(raw, f.publicHost)
	if err != nil {
		return CIMDMetadata{}, errCIMDInvalid
	}
	f.mu.Lock()
	now := f.clock()
	if element := f.cache[raw]; element != nil {
		entry, ok := element.Value.(cimdCacheEntry)
		if !ok {
			panic("oauth client metadata cache type invalid")
		}
		if now.Before(entry.expires) {
			f.lru.MoveToFront(element)
			f.mu.Unlock()
			return cloneCIMD(entry.metadata), nil
		}
		f.removeEntry(element)
	}
	flight := f.flights[raw]
	if flight == nil {
		if len(f.flights) >= f.cfg.ConcurrentFetches {
			f.mu.Unlock()
			return CIMDMetadata{}, &CIMDRateError{RetryAfter: ceilSeconds(f.cfg.CompleteTimeout)}
		}
		if !address.IsValid() {
			f.mu.Unlock()
			return CIMDMetadata{}, errCIMDInvalid
		}
		digest := sha256.Sum256([]byte(raw))
		retry := 0
		for _, admission := range []struct {
			limiter *api.BoundedRateLimiter
			key     string
		}{
			{f.addressRate, registerAddressKey(address.Unmap())}, {f.urlRate, fmt.Sprintf("%x", digest)}, {f.globalRate, "global"},
		} {
			if ok, seconds := admission.limiter.Admit(now, admission.key); !ok && seconds > retry {
				retry = seconds
			}
		}
		if retry > 0 {
			f.mu.Unlock()
			return CIMDMetadata{}, &CIMDRateError{RetryAfter: retry}
		}
		flight = &cimdFlight{done: make(chan struct{})}
		f.flights[raw] = flight
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), f.cfg.CompleteTimeout)
		go func() {
			defer cancel()
			metadata, lifetime, received, fetchErr := f.fetch(fetchCtx, raw, u)
			f.mu.Lock()
			defer f.mu.Unlock()
			if fetchErr == nil {
				f.cacheMetadata(metadata, received.Add(lifetime))
			}
			flight.metadata, flight.err = metadata, fetchErr
			delete(f.flights, raw)
			close(flight.done)
		}()
	}
	f.mu.Unlock()
	select {
	case <-ctx.Done():
		return CIMDMetadata{}, errCIMDInvalid
	case <-flight.done:
		return cloneCIMD(flight.metadata), flight.err
	}
}

func (f *CIMDResolver) removeEntry(element *list.Element) {
	entry, ok := element.Value.(cimdCacheEntry)
	if !ok {
		panic("oauth client metadata cache type invalid")
	}
	delete(f.cache, entry.metadata.id)
	f.cacheBytes -= entry.bytes
	f.lru.Remove(element)
}

func (f *CIMDResolver) cacheMetadata(m CIMDMetadata, expires time.Time) {
	// Include string bytes, slice capacity, entry, list, map and allocator
	// overhead with a conservative per-entry reserve.
	accounted := 512 + len(m.id) + len(m.host) + len(m.name) + cap(m.redirects)*16
	for _, redirect := range m.redirects {
		accounted += len(redirect) + 16
	}
	if accounted > f.cfg.MaxCacheBytes-f.cacheBaseBytes {
		return
	}
	for len(f.cache) >= f.cfg.MaxCacheEntries || f.cacheBytes+accounted > f.cfg.MaxCacheBytes {
		f.removeEntry(f.lru.Back())
	}
	element := f.lru.PushFront(cimdCacheEntry{metadata: m, expires: expires, bytes: accounted})
	f.cache[m.id] = element
	f.cacheBytes += accounted
}

func (f *CIMDResolver) fetch(ctx context.Context, raw string, u *url.URL) (CIMDMetadata, time.Duration, time.Time, error) {
	bad := func() (CIMDMetadata, time.Duration, time.Time, error) {
		return CIMDMetadata{}, 0, time.Time{}, errCIMDInvalid
	}
	connectCtx, cancel := context.WithTimeout(ctx, f.cfg.ConnectTimeout)
	defer cancel()
	var addresses []netip.Addr
	port := uint16(443)
	if f.cfg.DevStubAddr != "" {
		stub, err := netip.ParseAddrPort(f.cfg.DevStubAddr)
		if err != nil || !stub.Addr().IsLoopback() || stub.Port() == 0 {
			return bad()
		}
		addresses = []netip.Addr{stub.Addr()}
		port = stub.Port()
	} else {
		resolved, err := f.lookup(connectCtx, u.Hostname())
		if err != nil || len(resolved) == 0 {
			return bad()
		}
		for _, address := range resolved {
			if !f.policy(address) {
				return bad()
			}
			addresses = append(addresses, address.Unmap())
		}
	}
	approved := make(map[netip.Addr]bool, len(addresses))
	for _, address := range addresses {
		approved[address] = true
	}
	dialer := &net.Dialer{ControlContext: func(_ context.Context, _ string, address string, _ syscall.RawConn) error {
		return f.checkConnectAddress(address, port, approved)
	}}
	transport := &http.Transport{
		Proxy: nil, DisableCompression: true, DisableKeepAlives: true, ForceAttemptHTTP2: false,
		TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{},
		DialTLSContext: func(_ context.Context, network, target string) (net.Conn, error) {
			if target != net.JoinHostPort(u.Hostname(), "443") {
				return nil, errCIMDInvalid
			}
			for _, address := range addresses {
				targetAddress := netip.AddrPortFrom(address, port).String()
				if err := f.checkConnectAddress(targetAddress, port, approved); err != nil {
					return nil, errCIMDInvalid
				}
				dial := dialer.DialContext
				if f.dialAddress != nil {
					dial = f.dialAddress
				}
				conn, err := dial(connectCtx, network, targetAddress)
				if err != nil {
					continue
				}
				tlsConn := tls.Client(conn, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname(), RootCAs: f.roots})
				if err := tlsConn.HandshakeContext(connectCtx); err != nil {
					if closeErr := conn.Close(); closeErr != nil {
						return nil, errCIMDInvalid
					}
					continue
				}
				return tlsConn, nil
			}
			return nil, errCIMDInvalid
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return bad()
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "aboutme-cimd/1")
	response, err := client.Do(req)
	if err != nil {
		return bad()
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			return
		}
	}()
	if response.StatusCode != http.StatusOK || response.ContentLength > f.cfg.MaxBodyBytes {
		return bad()
	}
	for _, encoding := range response.Header.Values("Content-Encoding") {
		if !strings.EqualFold(strings.TrimSpace(encoding), "identity") {
			return bad()
		}
	}
	if len(response.Header.Values("Content-Type")) != 1 {
		return bad()
	}
	kind, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || kind != "application/json" && !(strings.HasPrefix(kind, "application/") && len(kind) > len("application/+json") && strings.HasSuffix(kind, "+json")) {
		return bad()
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, f.cfg.MaxBodyBytes+1))
	if err != nil || int64(len(body)) > f.cfg.MaxBodyBytes || ctx.Err() != nil {
		return bad()
	}
	received := f.clock()
	metadata, err := validateCIMDDocument(body, raw, u.Hostname())
	if err != nil || ctx.Err() != nil {
		return bad()
	}
	return metadata, cimdCacheLifetime(strings.Join(response.Header.Values("Cache-Control"), ","), f.cfg.CacheFloor, f.cfg.CacheCeiling), received, nil
}

func (f *CIMDResolver) checkConnectAddress(raw string, port uint16, approved map[netip.Addr]bool) error {
	literal, err := netip.ParseAddrPort(raw)
	if err != nil || literal.Port() != port || !approved[literal.Addr().Unmap()] {
		return errCIMDInvalid
	}
	if f.cfg.DevStubAddr != "" {
		if !literal.Addr().IsLoopback() {
			return errCIMDInvalid
		}
	} else if !f.policy(literal.Addr()) {
		return errCIMDInvalid
	}
	return nil
}
