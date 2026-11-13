package oauthsrv

import (
	"errors"
	"math"
	"net/http"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/api"
)

type RateConfig struct {
	TrustedProxies         api.TrustedProxies
	RegisterRequests       int
	RegisterEgressRanges   []netip.Prefix
	RegisterRangeRequests  int
	RegisterGlobalRequests int
	RegisterWindow         time.Duration
	TokenRequests          int
	TokenWindow            time.Duration
	FailedGrantLimit       int
	FailedGrantWindow      time.Duration
	MaxKeys                int
}

const registerGlobalKey = "global"

type RatePolicies struct {
	trusted        api.TrustedProxies
	egressRanges   []netip.Prefix
	register       *api.BoundedRateLimiter
	registerRange  *api.BoundedRateLimiter
	registerGlobal *api.BoundedRateLimiter
	token          *api.BoundedRateLimiter
	grants         *failedGrantLimiter
}

func NewRatePolicies(cfg RateConfig) (*RatePolicies, error) {
	if cfg.RegisterRequests <= 0 || cfg.RegisterRangeRequests <= 0 || cfg.RegisterGlobalRequests <= 0 ||
		cfg.RegisterWindow <= 0 || cfg.TokenRequests <= 0 || cfg.TokenWindow <= 0 ||
		cfg.FailedGrantLimit <= 0 || cfg.FailedGrantWindow <= 0 || cfg.MaxKeys <= 0 {
		return nil, errors.New("oauth rate policies: invalid configuration")
	}
	for _, prefix := range cfg.RegisterEgressRanges {
		if !prefix.IsValid() || prefix != prefix.Masked() || prefix.Addr().Is4In6() {
			return nil, errors.New("oauth rate policies: invalid registration egress range")
		}
	}
	bucket := func(requests int, window time.Duration) *api.BoundedRateLimiter {
		return api.NewBoundedRateLimiter(api.RateLimiterConfig{Requests: requests, Window: window, MaxKeys: cfg.MaxKeys})
	}
	return &RatePolicies{
		trusted:        cfg.TrustedProxies,
		egressRanges:   slices.Clone(cfg.RegisterEgressRanges),
		register:       bucket(cfg.RegisterRequests, cfg.RegisterWindow),
		registerRange:  bucket(cfg.RegisterRangeRequests, cfg.RegisterWindow),
		registerGlobal: bucket(cfg.RegisterGlobalRequests, cfg.RegisterWindow),
		token:          bucket(cfg.TokenRequests, cfg.TokenWindow),
		grants:         newFailedGrantLimiter(cfg.FailedGrantLimit, cfg.FailedGrantWindow, cfg.MaxKeys),
	}, nil
}

func (p *RatePolicies) AdmitRegister(now time.Time, r *http.Request) (bool, int) {
	raw, ok := api.ClientIP(r, p.trusted)
	if !ok {
		return false, 1
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return false, 1
	}
	addr = addr.Unmap()
	limiter, key := p.register, registerAddressKey(addr)
	for _, prefix := range p.egressRanges {
		if prefix.Contains(addr) {
			limiter, key = p.registerRange, "range:"+prefix.String()
			break
		}
	}
	if allowed, retry := limiter.Admit(now, key); !allowed {
		return false, retry
	}
	return p.registerGlobal.Admit(now, registerGlobalKey)
}

func registerAddressKey(addr netip.Addr) string {
	if addr.Is6() {
		if prefix, err := addr.Prefix(64); err == nil {
			return "ip6:" + prefix.String()
		}
	}
	return "ip:" + addr.String()
}

func (p *RatePolicies) AdmitToken(now time.Time, r *http.Request) (bool, int) {
	key, ok := api.IPKeyFunc(r, p.trusted)
	if !ok {
		return false, 1
	}
	return p.token.Admit(now, key)
}

func (p *RatePolicies) AdmitGrant(grantID uuid.UUID, now time.Time) (GrantAttempt, bool, int) {
	return p.grants.admit(grantID, now)
}

func (p *RatePolicies) FinishGrant(attempt grantAttempt, result grantAttemptResult) {
	p.grants.finish(attempt, result)
}

type failedGrantBucket struct {
	failures    int
	pending     map[uint64]struct{}
	windowStart time.Time
}

type failedGrantLimiter struct {
	limit   int
	window  time.Duration
	maxKeys int

	mu        sync.Mutex
	highTime  time.Time
	nextLease uint64
	entries   map[uuid.UUID]*failedGrantBucket
	overflow  failedGrantBucket
}

func newFailedGrantLimiter(limit int, window time.Duration, maxKeys int) *failedGrantLimiter {
	return &failedGrantLimiter{
		limit: limit, window: window, maxKeys: maxKeys,
		entries: make(map[uuid.UUID]*failedGrantBucket),
	}
}

func (l *failedGrantLimiter) nowLocked(now time.Time) time.Time {
	if now.Before(l.highTime) {
		return l.highTime
	}
	l.highTime = now
	return now
}

func (l *failedGrantLimiter) admit(grantID uuid.UUID, now time.Time) (grantAttempt, bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now = l.nowLocked(now)
	bucket, tracked := l.entries[grantID]
	if !tracked {
		l.evictExpiredLocked(now)
		if len(l.entries) < l.maxKeys {
			bucket = &failedGrantBucket{}
			l.entries[grantID] = bucket
			tracked = true
		} else {
			bucket = &l.overflow
		}
	}
	if l.expiredLocked(bucket, now) {
		*bucket = failedGrantBucket{}
	}
	if bucket.failures+len(bucket.pending) >= l.limit {
		return grantAttempt{}, false, ceilSeconds(bucket.windowStart.Add(l.window).Sub(now))
	}
	if bucket.windowStart.IsZero() {
		bucket.windowStart = now
	}
	if bucket.pending == nil {
		bucket.pending = make(map[uint64]struct{})
	}
	l.nextLease++
	if l.nextLease == 0 {
		l.nextLease++
	}
	bucket.pending[l.nextLease] = struct{}{}
	return grantAttempt{clientID: grantID, leaseID: l.nextLease, overflow: !tracked}, true, 0
}

func (l *failedGrantLimiter) finish(attempt grantAttempt, result grantAttemptResult) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var bucket *failedGrantBucket
	if attempt.overflow {
		bucket = &l.overflow
	} else {
		bucket = l.entries[attempt.clientID]
	}
	if bucket == nil || bucket.pending == nil {
		return
	}
	if _, ok := bucket.pending[attempt.leaseID]; !ok {
		return
	}
	delete(bucket.pending, attempt.leaseID)
	switch result {
	case grantAttemptFailure:
		if bucket.failures < l.limit {
			bucket.failures++
		}
	case grantAttemptSuccess:
		if !attempt.overflow {
			delete(l.entries, attempt.clientID)
			return
		}
	}
	if bucket.failures == 0 && len(bucket.pending) == 0 {
		if attempt.overflow {
			l.overflow = failedGrantBucket{}
		} else {
			delete(l.entries, attempt.clientID)
		}
	}
}

func (l *failedGrantLimiter) evictExpiredLocked(now time.Time) {
	for grantID, bucket := range l.entries {
		if l.expiredLocked(bucket, now) {
			delete(l.entries, grantID)
		}
	}
}

func (l *failedGrantLimiter) expiredLocked(bucket *failedGrantBucket, now time.Time) bool {
	return !bucket.windowStart.IsZero() && !now.Before(bucket.windowStart.Add(l.window))
}

func ceilSeconds(duration time.Duration) int {
	seconds := int(math.Ceil(duration.Seconds()))
	if seconds < 1 {
		return 1
	}
	return seconds
}
