package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// testRateSecret returns a fixed nonzero 32-byte HMAC key.
func testRateSecret() [32]byte {
	var k [32]byte
	for i := range k {
		k[i] = byte(i + 1)
	}
	return k
}

func newTestRatePolicies(t *testing.T) *PasswordRatePolicies {
	t.Helper()
	p, err := NewPasswordRatePolicies(testRateSecret())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// recordingAdmission records the key an admission limiter receives.
type recordingAdmission struct {
	record func(key string)
}

func (r *recordingAdmission) Admit(_ time.Time, key string) RateDecision {
	r.record(key)
	return RateDecision{Allowed: true}
}

// recordingFailure records the email key a failure limiter receives.
type recordingFailure struct {
	record func(key [32]byte)
}

func (r *recordingFailure) State(_ time.Time, _ [32]byte) FailureState { return FailureState{} }
func (r *recordingFailure) RecordFailure(_ time.Time, key [32]byte) FailureState {
	r.record(key)
	return FailureState{}
}
func (r *recordingFailure) ClearSuccess(_ [32]byte) {}

// exhaustAdmission drives one policy method to its limit and proves the
// limit+1 call is denied with a positive ceiling-rounded retry.
func exhaustAdmission(t *testing.T, limit int, admit func() RateDecision) {
	t.Helper()
	for i := 0; i < limit; i++ {
		d := admit()
		if !d.Allowed || d.RetryAfterSeconds != 0 {
			t.Fatalf("request %d: %+v; want allowed with retry 0", i+1, d)
		}
	}
	d := admit()
	if d.Allowed || d.RetryAfterSeconds < 1 {
		t.Fatalf("request %d: %+v; want denied with positive ceiling-rounded retry", limit+1, d)
	}
}

func TestPasswordRateBudgetParity(t *testing.T) {
	t.Parallel()

	// Every store is capped at the ADR 0018 default of 10,000 active keys plus
	// one overflow bucket.
	if passwordRateMaxKeys != api.DefaultRateLimitMaxKeys {
		t.Errorf("passwordRateMaxKeys = %d, api default = %d; want equal",
			passwordRateMaxKeys, api.DefaultRateLimitMaxKeys)
	}
	if passwordRateMaxKeys != 10_000 {
		t.Errorf("passwordRateMaxKeys = %d, want 10000", passwordRateMaxKeys)
	}

	// The exact D6 budget rows (budgets.md).
	if loginAdmissionLimit != 30 {
		t.Errorf("loginAdmissionLimit = %d, want 30", loginAdmissionLimit)
	}
	if loginAdmissionWindow != time.Minute {
		t.Errorf("loginAdmissionWindow = %v, want 1m", loginAdmissionWindow)
	}
	if loginFailuresLimit != 10 {
		t.Errorf("loginFailuresLimit = %d, want 10", loginFailuresLimit)
	}
	if loginFailuresWindow != 15*time.Minute {
		t.Errorf("loginFailuresWindow = %v, want 15m", loginFailuresWindow)
	}
	if registerForgotEmailLimit != 5 {
		t.Errorf("registerForgotEmailLimit = %d, want 5", registerForgotEmailLimit)
	}
	if registerForgotIPLimit != 20 {
		t.Errorf("registerForgotIPLimit = %d, want 20", registerForgotIPLimit)
	}
	if verifyResetIPLimit != 10 {
		t.Errorf("verifyResetIPLimit = %d, want 10", verifyResetIPLimit)
	}
	if accountMutationLimit != 10 {
		t.Errorf("accountMutationLimit = %d, want 10", accountMutationLimit)
	}
}

func TestPasswordRateLoginIPBudget(t *testing.T) {
	t.Parallel()
	p := newTestRatePolicies(t)
	ip := netip.MustParseAddr("203.0.113.10")
	now := testutil.Epoch

	exhaustAdmission(t, loginAdmissionLimit, func() RateDecision {
		return p.AdmitLoginIP(now, ip)
	})

	// A different IP has its own budget.
	if d := p.AdmitLoginIP(now, netip.MustParseAddr("203.0.113.11")); !d.Allowed {
		t.Fatalf("different IP denied: %+v; want independent budget", d)
	}
}

func TestPasswordRateRegisterOrForgotIPBudget(t *testing.T) {
	t.Parallel()
	p := newTestRatePolicies(t)
	ip := netip.MustParseAddr("203.0.113.20")
	now := testutil.Epoch

	exhaustAdmission(t, registerForgotIPLimit, func() RateDecision {
		return p.AdmitRegisterOrForgotIP(now, ip)
	})

	// Register and forgot share this per-IP budget: exhausting one route's
	// admission denies the other route for the same IP.
	if d := p.AdmitRegisterOrForgotIP(now, ip); d.Allowed {
		t.Fatal("second route allowed after shared per-IP budget exhausted")
	}
}

func TestPasswordRateRegisterOrForgotEmailBudget(t *testing.T) {
	t.Parallel()
	p := newTestRatePolicies(t)
	now := testutil.Epoch
	const email = "alice@example.com"

	exhaustAdmission(t, registerForgotEmailLimit, func() RateDecision {
		return p.AdmitRegisterOrForgotEmail(now, email)
	})

	// A different email is independent.
	if d := p.AdmitRegisterOrForgotEmail(now, "bob@example.com"); !d.Allowed {
		t.Fatalf("different email denied: %+v; want independent budget", d)
	}
	// Canonical case equivalence: the same mailbox spelled differently shares
	// the same budget.
	if d := p.AdmitRegisterOrForgotEmail(now, "Alice@Example.com"); d.Allowed {
		t.Fatal("mixed-case spelling allowed after lowercase budget exhausted; want shared bucket")
	}
}

func TestPasswordRateVerifyOrResetIPBudget(t *testing.T) {
	t.Parallel()
	p := newTestRatePolicies(t)
	ip := netip.MustParseAddr("203.0.113.30")
	now := testutil.Epoch

	exhaustAdmission(t, verifyResetIPLimit, func() RateDecision {
		return p.AdmitVerifyOrResetIP(now, ip)
	})
}

func TestPasswordRateAccountMutationBudget(t *testing.T) {
	t.Parallel()
	p := newTestRatePolicies(t)
	now := testutil.Epoch
	userA := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	userB := uuid.MustParse("550e8400-e29b-41d4-a716-446655440001")
	ip := netip.MustParseAddr("203.0.113.40")
	otherIP := netip.MustParseAddr("203.0.113.41")

	exhaustAdmission(t, accountMutationLimit, func() RateDecision {
		return p.AdmitAccountMutation(now, userA, ip)
	})

	// Same account from a different IP is an independent (account, IP) bucket.
	if d := p.AdmitAccountMutation(now, userA, otherIP); !d.Allowed {
		t.Fatalf("same account, different IP denied: %+v; want independent budget", d)
	}
	// Different account from the same IP is also independent.
	if d := p.AdmitAccountMutation(now, userB, ip); !d.Allowed {
		t.Fatalf("different account, same IP denied: %+v; want independent budget", d)
	}
}

func TestPasswordRateAdmissionRefillsAfterWindow(t *testing.T) {
	t.Parallel()
	p := newTestRatePolicies(t)
	now := testutil.Epoch
	const email = "alice@example.com"

	exhaustAdmission(t, registerForgotEmailLimit, func() RateDecision {
		return p.AdmitRegisterOrForgotEmail(now, email)
	})

	// The hourly budget refills continuously, so a full hour later the whole
	// budget is available again (rolling expiry).
	full := now.Add(time.Hour)
	for i := 0; i < registerForgotEmailLimit; i++ {
		if d := p.AdmitRegisterOrForgotEmail(full, email); !d.Allowed {
			t.Fatalf("refilled request %d denied: %+v; want the budget fully restored after one window", i+1, d)
		}
	}
	if d := p.AdmitRegisterOrForgotEmail(full, email); d.Allowed {
		t.Fatal("request beyond a full window's budget allowed")
	}
}

func TestPasswordEmailHMACFixedSizeAndDistinctSecrets(t *testing.T) {
	t.Parallel()
	const email = "alice@example.com"
	secretA := [32]byte{1}
	secretB := [32]byte{2}

	a := emailDigest(secretA, email)
	b := emailDigest(secretB, email)
	if len(a) != 32 || len(b) != 32 {
		t.Fatalf("digest sizes = %d, %d; want 32", len(a), len(b))
	}
	if a == b {
		t.Fatal("distinct secrets produced an identical digest for the same email")
	}
}

func TestPasswordEmailHMACDomainSeparation(t *testing.T) {
	t.Parallel()
	secret := [32]byte{7}
	const email = "alice@example.com"

	digest := emailDigest(secret, email)

	// A bare HMAC of the email without the domain-separation prefix must differ,
	// proving the prefix actually shapes the input so the email digest can never
	// collide with another use of the same secret over the raw email.
	mac := hmac.New(sha256.New, secret[:])
	mac.Write([]byte(email))
	var bare [32]byte
	copy(bare[:], mac.Sum(nil))
	if digest == bare {
		t.Fatal("domain separator had no effect; digest equals bare HMAC(email)")
	}
}

func TestPasswordEmailHMACCanonicalCaseEquivalence(t *testing.T) {
	t.Parallel()
	p := newTestRatePolicies(t)

	mixed, okMixed := p.canonicalEmailDigest("Alice.Raw@Example.COM")
	lower, okLower := p.canonicalEmailDigest("alice.raw@example.com")
	if !okMixed || !okLower {
		t.Fatalf("canonicalize ok = (%v, %v); want true, true", okMixed, okLower)
	}
	if mixed != lower {
		t.Fatal("mixed-case and lowercase email derived different digests")
	}
	if other, ok := p.canonicalEmailDigest("mallory@example.com"); ok && mixed == other {
		t.Fatal("distinct emails derived the same digest")
	}
}

func TestPasswordEmailHMACInvalidSecretRejected(t *testing.T) {
	t.Parallel()
	if _, err := NewPasswordRatePolicies([32]byte{}); err == nil {
		t.Fatal("zero HMAC key accepted; want error")
	}
	if _, err := NewPasswordRatePolicies([32]byte{1}); err != nil {
		t.Fatalf("nonzero HMAC key rejected: %v", err)
	}
}

func TestPasswordEmailHMACNoRawEmailInKeys(t *testing.T) {
	t.Parallel()
	p := newTestRatePolicies(t)
	const rawEmail = "Alice.Raw@Example.COM"

	var admissionKey string
	p.registerOrForgotEmail = &recordingAdmission{record: func(key string) { admissionKey = key }}
	p.AdmitRegisterOrForgotEmail(testutil.Epoch, rawEmail)
	if len(admissionKey) != 32 {
		t.Fatalf("admission key length = %d, want 32 (the HMAC digest)", len(admissionKey))
	}
	if strings.Contains(admissionKey, rawEmail) || strings.Contains(admissionKey, "alice.raw@example.com") {
		t.Fatalf("admission key %q leaks raw email bytes", admissionKey)
	}

	var failureKey [32]byte
	p.failures = &recordingFailure{record: func(key [32]byte) { failureKey = key }}
	p.RecordLoginFailure(testutil.Epoch, rawEmail)
	if failureKey == ([32]byte{}) {
		t.Fatal("failure limiter received a zero key")
	}
	if bytes.Contains(failureKey[:], []byte("alice")) || bytes.Contains(failureKey[:], []byte("example")) {
		t.Fatal("failure key contains raw email bytes")
	}
}

func TestPasswordRateInvalidEmailFailsClosed(t *testing.T) {
	t.Parallel()
	p := newTestRatePolicies(t)
	now := testutil.Epoch

	d := p.AdmitRegisterOrForgotEmail(now, "not an email")
	if d.Allowed || d.RetryAfterSeconds < 1 {
		t.Fatalf("AdmitRegisterOrForgotEmail(invalid) = %+v; want denied with positive retry", d)
	}
	s := p.RecordLoginFailure(now, "not an email")
	if !s.Exhausted || s.RetryAfterSeconds < 1 {
		t.Fatalf("RecordLoginFailure(invalid) = %+v; want exhausted with positive retry", s)
	}
	// The malformed input must not have consumed any real limiter budget.
	if d := p.AdmitRegisterOrForgotEmail(now, "alice@example.com"); !d.Allowed {
		t.Fatalf("valid email denied after malformed input: %+v", d)
	}
}

func TestPasswordFailureRecordThreshold(t *testing.T) {
	t.Parallel()
	p := newTestRatePolicies(t)
	now := testutil.Epoch
	const email = "alice@example.com"

	for i := 1; i <= 9; i++ {
		s := p.RecordLoginFailure(now, email)
		if s.Exhausted || s.RetryAfterSeconds != 0 {
			t.Fatalf("failure %d: %+v; want unexhausted with retry 0", i, s)
		}
	}
	s := p.RecordLoginFailure(now, email)
	if !s.Exhausted || s.RetryAfterSeconds != 900 {
		t.Fatalf("failure 10: %+v; want exhausted with retry 900", s)
	}
	// An 11th failure at the same instant stays exhausted with the full window
	// remaining (the window is not extended).
	s = p.RecordLoginFailure(now, email)
	if !s.Exhausted || s.RetryAfterSeconds != 900 {
		t.Fatalf("failure 11: %+v; want exhausted with retry 900", s)
	}
}

func TestPasswordFailureStateObservesWithoutMutation(t *testing.T) {
	t.Parallel()
	p := newTestRatePolicies(t)
	now := testutil.Epoch
	const email = "alice@example.com"

	for i := 0; i < 10; i++ {
		p.RecordLoginFailure(now, email)
	}
	before := p.LoginFailureState(now, email)
	after := p.LoginFailureState(now, email)
	if before != after {
		t.Fatalf("State mutated the bucket: %+v then %+v", before, after)
	}
	if !after.Exhausted || after.RetryAfterSeconds != 900 {
		t.Fatalf("State = %+v; want exhausted with retry 900", after)
	}
	// Observing again after another record still sees the accumulated debt.
	p.RecordLoginFailure(now, email)
	if s := p.LoginFailureState(now, email); !s.Exhausted {
		t.Fatal("State after an extra failure lost exhaustion")
	}
}

func TestPasswordFailureClearSuccessOnlyThatEmail(t *testing.T) {
	t.Parallel()
	p := newTestRatePolicies(t)
	now := testutil.Epoch

	for i := 0; i < 10; i++ {
		p.RecordLoginFailure(now, "alice@example.com")
		p.RecordLoginFailure(now, "bob@example.com")
	}
	p.ClearLoginSuccess("alice@example.com")

	if s := p.LoginFailureState(now, "alice@example.com"); s.Exhausted {
		t.Fatalf("alice still exhausted after success clear: %+v", s)
	}
	if s := p.LoginFailureState(now, "bob@example.com"); !s.Exhausted {
		t.Fatalf("bob was cleared too: %+v; ClearSuccess must remove only that email key", s)
	}
	// Alice's next failure starts a fresh window at count 1.
	if s := p.RecordLoginFailure(now, "alice@example.com"); s.Exhausted {
		t.Fatalf("first post-clear failure exhausted: %+v; want a fresh window", s)
	}
}

func TestPasswordFailureLaterFailuresDoNotExtendWindow(t *testing.T) {
	t.Parallel()
	p := newTestRatePolicies(t)
	start := testutil.Epoch
	const email = "alice@example.com"

	for i := 0; i < 10; i++ {
		p.RecordLoginFailure(start, email) // all at the same instant: window opens at start
	}

	// A failure ten minutes in must not push the expiry out: five minutes remain.
	s := p.RecordLoginFailure(start.Add(10*time.Minute), email)
	if !s.Exhausted || s.RetryAfterSeconds != 300 {
		t.Fatalf("failure at +10m: %+v; want exhausted with retry 300", s)
	}
	// At exactly 15 minutes the fixed window has closed.
	if s := p.LoginFailureState(start.Add(15*time.Minute), email); s.Exhausted {
		t.Fatalf("State at +15m: %+v; want unexhausted (window closed)", s)
	}
	// A failure after expiry opens a brand-new window.
	after := start.Add(16 * time.Minute)
	if s := p.RecordLoginFailure(after, email); s.Exhausted {
		t.Fatalf("first failure after expiry exhausted: %+v; want a new window", s)
	}
	// Re-exhausting the new window reports the fresh window's full retry.
	for i := 0; i < 9; i++ {
		p.RecordLoginFailure(after, email)
	}
	if s := p.LoginFailureState(after, email); !s.Exhausted || s.RetryAfterSeconds != 900 {
		t.Fatalf("second window = %+v; want exhausted with retry 900", s)
	}
}

func TestPasswordFailureClockRollbackFailsClosed(t *testing.T) {
	t.Parallel()
	p := newTestRatePolicies(t)
	const email = "alice@example.com"
	peak := testutil.Epoch.Add(100 * time.Second)

	for i := 0; i < 10; i++ {
		p.RecordLoginFailure(peak, email) // window opens at peak
	}

	// A backward clock step must not make the open window look expired.
	rolledBack := testutil.Epoch.Add(50 * time.Second)
	s := p.LoginFailureState(rolledBack, email)
	if !s.Exhausted || s.RetryAfterSeconds != 900 {
		t.Fatalf("State after rollback = %+v; want exhausted with retry 900 (fail closed)", s)
	}
}

func TestPasswordFailureConcurrentRecordExact(t *testing.T) {
	t.Parallel()
	p := newTestRatePolicies(t)
	now := testutil.Epoch
	const email = "alice@example.com"

	const attempts = 100
	var unexhausted, exhausted atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s := p.RecordLoginFailure(now, email); s.Exhausted {
				exhausted.Add(1)
			} else {
				unexhausted.Add(1)
			}
		}()
	}
	wg.Wait()

	// Exactly failures 1..9 report unexhausted; the tenth and the remaining
	// ninety report exhausted — no increment is lost or double-counted.
	if got := unexhausted.Load(); got != loginFailuresLimit-1 {
		t.Fatalf("unexhausted = %d, want %d", got, loginFailuresLimit-1)
	}
	if got := exhausted.Load(); got != attempts-(loginFailuresLimit-1) {
		t.Fatalf("exhausted = %d, want %d", got, attempts-(loginFailuresLimit-1))
	}
}

func TestPasswordFailureConcurrentSuccessAndFailure(t *testing.T) {
	t.Parallel()
	p := newTestRatePolicies(t)
	now := testutil.Epoch
	const email = "alice@example.com"

	// Nine failures leave the bucket one short of exhaustion, then a tenth
	// failure races a success. The mutex is the linearization point: if the
	// failure lands first the success clears it; if the success lands first the
	// failure opens a fresh window. Either way the bucket must not report
	// exhausted, and no later call can resurrect cleared debt.
	for i := 0; i < 9; i++ {
		p.RecordLoginFailure(now, email)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); p.RecordLoginFailure(now, email) }()
	go func() { defer wg.Done(); p.ClearLoginSuccess(email) }()
	wg.Wait()

	if s := p.LoginFailureState(now, email); s.Exhausted {
		t.Fatalf("bucket exhausted after concurrent success/failure: %+v", s)
	}
}

func TestPasswordFailureConcurrentDistinctKeys(t *testing.T) {
	t.Parallel()
	f := newFailureLimiter(loginFailuresLimit, loginFailuresWindow, 64)
	now := testutil.Epoch
	secret := testRateSecret()

	const goroutines = 100
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := emailDigest(secret, fmt.Sprintf("user%d@example.com", i))
			f.RecordFailure(now, key)
			f.State(now, key)
		}(i)
	}
	wg.Wait()
}

func TestPasswordFailureLimiterMaxKeysAndOverflow(t *testing.T) {
	t.Parallel()
	f := newFailureLimiter(loginFailuresLimit, loginFailuresWindow, 2)
	now := testutil.Epoch
	secret := testRateSecret()
	keyA := emailDigest(secret, "a@example.com")
	keyB := emailDigest(secret, "b@example.com")
	keyC := emailDigest(secret, "c@example.com")
	keyD := emailDigest(secret, "d@example.com")

	// A protected key records nine failures (just short of exhaustion) and B
	// fills the second slot: the store is now full of active entries.
	for i := 0; i < 9; i++ {
		f.RecordFailure(now, keyA)
	}
	f.RecordFailure(now, keyB)

	// C arrives at a saturated store: it shares the single overflow bucket.
	for i := 0; i < 10; i++ {
		s := f.RecordFailure(now, keyC)
		if i < 9 && s.Exhausted {
			t.Fatalf("overflow failure %d exhausted early: %+v", i+1, s)
		}
		if i == 9 && !s.Exhausted {
			t.Fatalf("overflow failure 10 not exhausted: %+v", s)
		}
	}

	// The protected key was never evicted and keeps its own debt.
	if s := f.State(now, keyA); s.Exhausted {
		t.Fatalf("protected key A exhausted by the flood: %+v", s)
	}
	// State for an untracked key at saturation reflects the shared overflow.
	if s := f.State(now, keyC); !s.Exhausted {
		t.Fatalf("State for overflow key C = %+v; want exhausted (shared bucket)", s)
	}
	// A further overflow key finds the shared bucket already exhausted.
	if s := f.RecordFailure(now, keyD); !s.Exhausted {
		t.Fatalf("overflow key D not exhausted: %+v", s)
	}
}

func TestPasswordFailureLimiterExpiredReclamation(t *testing.T) {
	t.Parallel()
	f := newFailureLimiter(loginFailuresLimit, loginFailuresWindow, 1)
	now := testutil.Epoch
	secret := testRateSecret()
	keyA := emailDigest(secret, "a@example.com")
	keyB := emailDigest(secret, "b@example.com")

	f.RecordFailure(now, keyA) // takes the store's single slot

	// Sixteen minutes later A's fixed window has closed. Admitting B must
	// reclaim A's expired slot rather than send B to overflow.
	later := now.Add(16 * time.Minute)
	s := f.RecordFailure(later, keyB)
	if s.Exhausted {
		t.Fatalf("B exhausted after reclamation: %+v; want its own fresh slot", s)
	}
	if f.State(later, keyB).Exhausted {
		t.Fatal("B is not tracked after reclamation")
	}
	// A's closed-window debt is gone.
	if f.State(later, keyA).Exhausted {
		t.Fatal("A still exhausted after its window closed")
	}
}

func TestPasswordRateIntegrationFakeVerificationStillRuns(t *testing.T) {
	t.Parallel()
	p := newTestRatePolicies(t)
	now := testutil.Epoch
	const email = "alice@example.com"

	var verifies atomic.Int64
	verify := func(raw string) bool {
		verifies.Add(1)
		return raw == "correct-password"
	}

	// Ten wrong attempts: each verifies once and then records a failure. The
	// tenth records exhaustion, but the verification call still happened.
	for i := 0; i < 10; i++ {
		if verify("wrong-password") {
			t.Fatal("wrong password verified")
		}
		s := p.RecordLoginFailure(now, email)
		if i < 9 && s.Exhausted {
			t.Fatalf("wrong attempt %d exhausted early: %+v", i+1, s)
		}
		if i == 9 && !s.Exhausted {
			t.Fatalf("tenth wrong attempt not exhausted: %+v", s)
		}
	}
	if got := verifies.Load(); got != 10 {
		t.Fatalf("verifications after 10 wrong attempts = %d, want 10 "+
			"(exhaustion never blocks the verification call)", got)
	}

	// A correct attempt still verifies once, clears debt, and succeeds.
	if !verify("correct-password") {
		t.Fatal("correct password did not verify")
	}
	p.ClearLoginSuccess(email)
	if s := p.LoginFailureState(now, email); s.Exhausted {
		t.Fatalf("after success clear: %+v; want unexhausted", s)
	}
	if got := verifies.Load(); got != 11 {
		t.Fatalf("verifications = %d, want 11", got)
	}
}

func TestPasswordRateLoginIPRejectionPrecedesVerification(t *testing.T) {
	t.Parallel()
	p := newTestRatePolicies(t)
	ip := netip.MustParseAddr("203.0.113.10")
	now := testutil.Epoch

	exhaustAdmission(t, loginAdmissionLimit, func() RateDecision {
		return p.AdmitLoginIP(now, ip)
	})
	// A denied login admission is independent of failure state, so the handler
	// can 429 before any verification/body work.
	if s := p.LoginFailureState(now, "alice@example.com"); s.Exhausted {
		t.Fatalf("failure state unexpectedly exhausted by IP admission: %+v", s)
	}
}
