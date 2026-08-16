package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"math"
	"net/netip"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/accountemail"
	"github.com/dannyota/aboutme/apps/server/internal/api"
)

// D6 password route rate budgets. These constants are the single source of the
// numeric values the policy tests assert against the exact budgets.md rows; the
// route handlers never re-derive them.
const (
	// passwordRateMaxKeys bounds every password rate store (ADR 0018): at most
	// this many active keys plus one shared overflow bucket.
	passwordRateMaxKeys = 10_000

	// loginAdmissionLimit is the login admission budget per client IP.
	loginAdmissionLimit = 30
	// loginAdmissionWindow is the interval loginAdmissionLimit applies over.
	loginAdmissionWindow = time.Minute

	// loginFailuresLimit is the number of wrong-password failures that open the
	// fixed exhausted window.
	loginFailuresLimit = 10
	// loginFailuresWindow is the fixed window opened by the first failure.
	loginFailuresWindow = 15 * time.Minute

	// registerForgotEmailLimit and registerForgotIPLimit are the shared
	// register/forgot budgets per canonical email and per client IP.
	registerForgotEmailLimit = 5
	registerForgotIPLimit    = 20

	// verifyResetIPLimit is the shared verify/reset budget per client IP.
	verifyResetIPLimit = 10

	// accountMutationLimit is the shared add/change/reauth budget per
	// (user ID, client IP) pair.
	accountMutationLimit = 10
)

// FailureState is the observed or recorded login-failure outcome for one
// canonical email. Exhausted=false always carries RetryAfterSeconds 0;
// Exhausted=true always carries a positive, ceiling-rounded
// RetryAfterSeconds.
type FailureState struct {
	Exhausted         bool
	RetryAfterSeconds int
}

// RateDecision is the admission outcome for one route request. Allowed=true
// always carries RetryAfterSeconds 0; Allowed=false always carries a positive,
// ceiling-rounded RetryAfterSeconds.
type RateDecision struct {
	Allowed           bool
	RetryAfterSeconds int
}

// admissionLimiter admits a single string key against a per-key budget.
// PasswordRatePolicies holds only this narrow interface so the failure limiter
// (a fixed-window counter) and the admission limiters (ADR 0018 token buckets)
// can be composed behind the same policy object.
type admissionLimiter interface {
	Admit(time.Time, string) RateDecision
}

// PasswordFailureLimiter tracks wrong-password failures per canonical-email
// HMAC digest. State observes without mutation; RecordFailure atomically
// increments the bucket; ClearSuccess removes only that email key.
type PasswordFailureLimiter interface {
	State(now time.Time, emailKey [32]byte) FailureState
	RecordFailure(now time.Time, emailKey [32]byte) FailureState
	ClearSuccess(emailKey [32]byte)
}

// PasswordRatePolicies holds the D6 route-admission and login-outcome rate
// policies. It is constructed by NewPasswordRatePolicies; handlers receive the
// policies as a whole and never assemble or name the individual stores.
type PasswordRatePolicies struct {
	emailHMACKey          [32]byte
	loginIP               admissionLimiter
	registerOrForgotIP    admissionLimiter
	registerOrForgotEmail admissionLimiter
	verifyOrResetIP       admissionLimiter
	accountMutation       admissionLimiter
	failures              PasswordFailureLimiter
}

// NewPasswordRatePolicies builds the five shared admission stores plus the
// failure store, each capped at passwordRateMaxKeys active keys plus one
// overflow bucket. A zero emailHMACKey is rejected: the runtime secret is
// exactly 32 bytes and distinct from the mail keys.
func NewPasswordRatePolicies(emailHMACKey [32]byte) (*PasswordRatePolicies, error) {
	if emailHMACKey == ([32]byte{}) {
		return nil, errors.New("password rate policies: zero HMAC key")
	}
	return &PasswordRatePolicies{
		emailHMACKey:          emailHMACKey,
		loginIP:               newAdmissionLimiter(loginAdmissionLimit, loginAdmissionWindow),
		registerOrForgotIP:    newAdmissionLimiter(registerForgotIPLimit, time.Hour),
		registerOrForgotEmail: newAdmissionLimiter(registerForgotEmailLimit, time.Hour),
		verifyOrResetIP:       newAdmissionLimiter(verifyResetIPLimit, time.Hour),
		accountMutation:       newAdmissionLimiter(accountMutationLimit, time.Hour),
		failures:              newFailureLimiter(loginFailuresLimit, loginFailuresWindow, passwordRateMaxKeys),
	}, nil
}

// AdmitLoginIP admits a password login start per client IP (30/minute).
func (p *PasswordRatePolicies) AdmitLoginIP(now time.Time, clientIP netip.Addr) RateDecision {
	return p.loginIP.Admit(now, ipBucketKey(clientIP))
}

// AdmitRegisterOrForgotIP admits a registration or forgot-password request per
// client IP (20/hour), shared by the two routes.
func (p *PasswordRatePolicies) AdmitRegisterOrForgotIP(now time.Time, clientIP netip.Addr) RateDecision {
	return p.registerOrForgotIP.Admit(now, ipBucketKey(clientIP))
}

// AdmitRegisterOrForgotEmail admits a registration or forgot-password request
// per canonical email (5/hour), shared by the two routes. The email is
// canonicalized through D1 and HMAC'd immediately; only the digest reaches the
// limiter.
func (p *PasswordRatePolicies) AdmitRegisterOrForgotEmail(now time.Time, canonicalEmail string) RateDecision {
	digest, ok := p.canonicalEmailDigest(canonicalEmail)
	if !ok {
		return deniedDecision
	}
	return p.registerOrForgotEmail.Admit(now, string(digest[:]))
}

// AdmitVerifyOrResetIP admits a verify-email or reset-password token
// consumption per client IP (10/hour), shared by the two routes.
func (p *PasswordRatePolicies) AdmitVerifyOrResetIP(now time.Time, clientIP netip.Addr) RateDecision {
	return p.verifyOrResetIP.Admit(now, ipBucketKey(clientIP))
}

// AdmitAccountMutation admits a password add, change, or reauth per
// (user ID, client IP) pair (10/hour), shared by the three routes.
func (p *PasswordRatePolicies) AdmitAccountMutation(now time.Time, userID uuid.UUID, clientIP netip.Addr) RateDecision {
	return p.accountMutation.Admit(now, accountMutationBucketKey(userID, clientIP))
}

// LoginFailureState observes, without mutating, whether canonicalEmail's
// failure window is exhausted.
func (p *PasswordRatePolicies) LoginFailureState(now time.Time, canonicalEmail string) FailureState {
	digest, ok := p.canonicalEmailDigest(canonicalEmail)
	if !ok {
		return deniedFailureState
	}
	return p.failures.State(now, digest)
}

// RecordLoginFailure atomically records one wrong-password failure for
// canonicalEmail and reports whether the fixed 15-minute window is exhausted.
// Failures one through nine report unexhausted; the tenth and later report
// exhausted with a positive ceiling-rounded retry.
func (p *PasswordRatePolicies) RecordLoginFailure(now time.Time, canonicalEmail string) FailureState {
	digest, ok := p.canonicalEmailDigest(canonicalEmail)
	if !ok {
		return deniedFailureState
	}
	return p.failures.RecordFailure(now, digest)
}

// ClearLoginSuccess removes only canonicalEmail's failure state after a
// correct password. It never touches any other email key.
func (p *PasswordRatePolicies) ClearLoginSuccess(canonicalEmail string) {
	digest, ok := p.canonicalEmailDigest(canonicalEmail)
	if !ok {
		return
	}
	p.failures.ClearSuccess(digest)
}

// deniedDecision and deniedFailureState are the fail-closed outcomes for a
// malformed canonical email. The route layer rejects such input before it
// reaches the policies, so these are defensive only; they keep the invariants
// "Allowed=false implies positive retry" and "Exhausted=true implies positive
// retry" while never letting a raw email into any key.
var (
	deniedDecision     = RateDecision{Allowed: false, RetryAfterSeconds: 1}
	deniedFailureState = FailureState{Exhausted: true, RetryAfterSeconds: 1}
)

// ipBucketKey normalizes a client IP for use as a per-IP admission key. The
// value is already canonical (api.ClientIP Unmap's); Unmap is applied again so
// an IPv4 address and its IPv4-in-IPv6 spelling always key identically.
func ipBucketKey(addr netip.Addr) string {
	return "ip:" + addr.Unmap().String()
}

// accountMutationBucketKey joins a user ID and a client IP into one composite
// admission key, so add/change/reauth share a per-(account, IP) budget.
func accountMutationBucketKey(userID uuid.UUID, clientIP netip.Addr) string {
	return "user:" + userID.String() + "|" + ipBucketKey(clientIP)
}

// passwordRateDomainSeparator is the domain-separation prefix that keeps the
// email HMAC distinct from every other use of the rate secret.
var passwordRateDomainSeparator = []byte("aboutme.password-rate.v1\x00")

// emailDigest returns the fixed 32-byte HMAC-SHA-256 digest of a canonical
// email under the rate secret. Only this digest ever enters a bucket, key,
// log, or metric; the canonical (lowercase) email itself is never stored or
// rendered by the policies.
func emailDigest(emailHMACKey [32]byte, canonicalEmail string) [32]byte {
	mac := hmac.New(sha256.New, emailHMACKey[:])
	mac.Write(passwordRateDomainSeparator)
	mac.Write([]byte(canonicalEmail))
	var digest [32]byte
	copy(digest[:], mac.Sum(nil))
	return digest
}

// canonicalEmailDigest canonicalizes through D1 and derives the HMAC digest.
// ok=false means the input was not a valid canonical account email; callers
// fail closed in that case.
func (p *PasswordRatePolicies) canonicalEmailDigest(canonicalEmail string) ([32]byte, bool) {
	canonical, err := accountemail.Canonicalize(canonicalEmail)
	if err != nil {
		return [32]byte{}, false
	}
	return emailDigest(p.emailHMACKey, canonical), true
}

// boundedAdmission adapts the exported ADR 0018 store to admissionLimiter.
type boundedAdmission struct {
	inner *api.BoundedRateLimiter
}

// newAdmissionLimiter builds one bounded per-key admission store capped at
// passwordRateMaxKeys active keys plus one overflow bucket.
func newAdmissionLimiter(requests int, window time.Duration) admissionLimiter {
	return &boundedAdmission{
		inner: api.NewBoundedRateLimiter(api.RateLimiterConfig{
			Requests: requests,
			Window:   window,
			MaxKeys:  passwordRateMaxKeys,
		}),
	}
}

// Admit forwards the key's admission decision through the bounded store,
// collapsing the returned retry duration into a RateDecision.
func (b *boundedAdmission) Admit(now time.Time, key string) RateDecision {
	allowed, retry := b.inner.Admit(now, key)
	if allowed {
		return RateDecision{Allowed: true}
	}
	return RateDecision{Allowed: false, RetryAfterSeconds: retry}
}

// failureSweepCooldown bounds how often the failure store scans the entries
// map for expired keys once saturated: at most once per second, matching the
// admission store's amortized-sweep behavior.
const failureSweepCooldown = time.Second

// failureBudget is one key's fixed-window failure counter. A zero windowStart
// means no window is open.
type failureBudget struct {
	count       int
	windowStart time.Time
}

// failureLimiter is the PasswordFailureLimiter implementation: a bounded,
// fixed-window failure counter. The first RecordFailure opens a window of
// fixed length; later failures increment the count but never extend the
// window. When the window closes, the count is debt-free until the next
// RecordFailure opens a fresh window. The store is capped at maxKeys active
// entries plus one shared overflow bucket; expired entries are reclaimed on
// admission when saturated, active entries are never evicted, and a clock
// rollback fails closed (see failureClock).
type failureLimiter struct {
	limit   int
	window  time.Duration
	maxKeys int

	mu          sync.Mutex
	clock       failureClock
	entries     map[[32]byte]*failureBudget
	overflow    failureBudget
	nextSweepAt time.Time
}

func newFailureLimiter(limit int, window time.Duration, maxKeys int) *failureLimiter {
	return &failureLimiter{
		limit:   limit,
		window:  window,
		maxKeys: maxKeys,
		entries: make(map[[32]byte]*failureBudget),
	}
}

// State reports the current failure state for emailKey without mutating
// anything. A key with no recorded state (or a closed window) reports
// unexhausted. If the store is saturated, an untracked key would share the
// overflow bucket on its next RecordFailure, so State reflects that bucket.
func (f *failureLimiter) State(now time.Time, emailKey [32]byte) FailureState {
	f.mu.Lock()
	defer f.mu.Unlock()
	now = f.clock.clamp(now)

	b, ok := f.entries[emailKey]
	if ok {
		return f.stateLocked(b, now)
	}
	if len(f.entries) >= f.maxKeys {
		return f.stateLocked(&f.overflow, now)
	}
	return FailureState{}
}

func (f *failureLimiter) stateLocked(b *failureBudget, now time.Time) FailureState {
	if b.count == 0 || b.windowStart.IsZero() || b.count < f.limit {
		return FailureState{}
	}
	remaining := f.window - now.Sub(b.windowStart)
	if remaining <= 0 {
		return FailureState{}
	}
	return FailureState{Exhausted: true, RetryAfterSeconds: ceilSeconds(remaining)}
}

// RecordFailure atomically increments emailKey's bucket, opening a fresh fixed
// window if none is open or the previous one has closed, and reports whether
// the window is exhausted. Exhaustion starts at failure limit (the tenth) and
// never resets until the window closes; later failures within the same window
// keep reporting exhausted with the time remaining in the original window.
func (f *failureLimiter) RecordFailure(now time.Time, emailKey [32]byte) FailureState {
	f.mu.Lock()
	defer f.mu.Unlock()
	now = f.clock.clamp(now)

	b, ok := f.entries[emailKey]
	if !ok {
		if len(f.entries) >= f.maxKeys && !now.Before(f.nextSweepAt) {
			f.evictExpiredLocked(now)
			f.nextSweepAt = now.Add(failureSweepCooldown)
		}
		if len(f.entries) < f.maxKeys {
			b = &failureBudget{}
			f.entries[emailKey] = b
		} else {
			// The store is full of active entries: share the single overflow
			// bucket rather than evicting a protected key.
			return f.recordLocked(&f.overflow, now)
		}
	}
	return f.recordLocked(b, now)
}

func (f *failureLimiter) recordLocked(b *failureBudget, now time.Time) FailureState {
	if b.windowStart.IsZero() || now.Sub(b.windowStart) >= f.window {
		b.windowStart = now
		b.count = 0
	}
	b.count++
	if b.count >= f.limit {
		remaining := f.window - now.Sub(b.windowStart)
		if remaining < 0 {
			remaining = 0
		}
		return FailureState{Exhausted: true, RetryAfterSeconds: ceilSeconds(remaining)}
	}
	return FailureState{}
}

// ClearSuccess removes only emailKey's entry. An overflow key has no private
// entry, so clearing it is a no-op: the shared overflow bucket is never
// cleared for one key's success.
func (f *failureLimiter) ClearSuccess(emailKey [32]byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.entries, emailKey)
}

// evictExpiredLocked drops every entry whose fixed window has closed (or that
// has no open window), freeing its slot for a new key. f.mu must be held by
// the caller.
func (f *failureLimiter) evictExpiredLocked(now time.Time) {
	for k, b := range f.entries {
		if b.count == 0 || b.windowStart.IsZero() || now.Sub(b.windowStart) >= f.window {
			delete(f.entries, k)
		}
	}
}

// failureClock turns a possibly-backward wall clock into a per-instance
// non-decreasing one, so a backward step cannot make an open failure window
// look expired (which would let a rollback clear an attacker's debt) or a
// closed one look fresh.
type failureClock struct {
	highWater time.Time
}

func (c *failureClock) clamp(now time.Time) time.Time {
	if now.Before(c.highWater) {
		return c.highWater
	}
	c.highWater = now
	return now
}

// ceilSeconds converts a duration to a whole-second retry count, always at
// least 1 so an exhausted caller is never told to retry immediately.
func ceilSeconds(d time.Duration) int {
	secs := int(math.Ceil(d.Seconds()))
	if secs < 1 {
		secs = 1
	}
	return secs
}
