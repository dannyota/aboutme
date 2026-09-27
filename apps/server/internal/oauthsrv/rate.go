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

	"github.com/dannyota/aboutme/apps/server/internal/api"
)

// RateConfig carries the frozen OAuth endpoint budgets from process config.
//
// Registration follows docs/design/mcp-client-compatibility.md, "Registration
// rate from shared egress": an address inside one of RegisterEgressRanges
// shares that range's RegisterRangeRequests budget, every other address keeps
// its own RegisterRequests budget, and RegisterGlobalRequests caps every
// admitted registration together. All three share RegisterWindow.
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

// registerGlobalKey is the one key of the global registration ceiling.
const registerGlobalKey = "global"

// RatePolicies composes OAuth endpoint admission over the ADR 0007 bounded
// limiter and one bounded fixed-window failed-grant store.
type RatePolicies struct {
	trusted        api.TrustedProxies
	egressRanges   []netip.Prefix
	register       *api.BoundedRateLimiter
	registerRange  *api.BoundedRateLimiter
	registerGlobal *api.BoundedRateLimiter
	token          *api.BoundedRateLimiter
	grants         *failedGrantLimiter
}

// NewRatePolicies validates and constructs one process-wide policy set.
func NewRatePolicies(cfg RateConfig) (*RatePolicies, error) {
	if cfg.RegisterRequests <= 0 || cfg.RegisterRangeRequests <= 0 || cfg.RegisterGlobalRequests <= 0 ||
		cfg.RegisterWindow <= 0 || cfg.TokenRequests <= 0 || cfg.TokenWindow <= 0 ||
		cfg.FailedGrantLimit <= 0 || cfg.FailedGrantWindow <= 0 || cfg.MaxKeys <= 0 {
		return nil, errors.New("oauth rate policies: invalid configuration")
	}
	for _, prefix := range cfg.RegisterEgressRanges {
		// Client addresses are unmapped before matching, so a mapped or
		// non-canonical prefix would not mean what it says.
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

// AdmitRegister enforces the registration budgets against the client address
// resolved once through the configured trust boundary; no other header is
// read. An address inside a configured egress range is admitted against that
// range's shared bucket, any other address against its own bucket. Only a
// request its own bucket admits is then charged to the global ceiling, so one
// address or range cannot drain the ceiling with refused requests. The
// Retry-After is that of the bucket that refused. An unresolvable address
// fails closed.
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
	limiter, key := p.register, "ip:"+addr.String()
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

// AdmitToken enforces the token-endpoint budget against the canonical client
// address accepted from the configured Caddy proxy boundary.
func (p *RatePolicies) AdmitToken(now time.Time, r *http.Request) (bool, int) {
	key, ok := api.IPKeyFunc(r, p.trusted)
	if !ok {
		return false, 1
	}
	return p.token.Admit(now, key)
}

// AdmitGrant reserves one failed-grant budget slot before token processing, so
// concurrent invalid grants cannot all pass an unchanged precheck.
func (p *RatePolicies) AdmitGrant(clientID uuid.UUID, now time.Time) (GrantAttempt, bool, int) {
	return p.grants.admit(clientID, now)
}

// FinishGrant resolves one reserved attempt. Invalid grants retain one failure;
// neutral outcomes release only their reservation; success clears a tracked
// client's window. Overflow success cannot clear other clients' shared debt.
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

func (l *failedGrantLimiter) admit(clientID uuid.UUID, now time.Time) (grantAttempt, bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now = l.nowLocked(now)
	bucket, tracked := l.entries[clientID]
	if !tracked {
		l.evictExpiredLocked(now)
		if len(l.entries) < l.maxKeys {
			bucket = &failedGrantBucket{}
			l.entries[clientID] = bucket
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
	return grantAttempt{clientID: clientID, leaseID: l.nextLease, overflow: !tracked}, true, 0
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
	for clientID, bucket := range l.entries {
		if l.expiredLocked(bucket, now) {
			delete(l.entries, clientID)
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
