package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/accountemail"
	"danny.vn/aboutme/apps/server/internal/api"
)

const (
	passwordRateMaxKeys = 10_000

	loginAdmissionLimit  = 30
	loginAdmissionWindow = time.Minute

	loginFailuresLimit  = 10
	loginFailuresWindow = 15 * time.Minute

	registerForgotEmailLimit = 5
	registerForgotIPLimit    = 20

	verifyResetIPLimit = 10

	accountMutationLimit = 10
)

type FailureState struct {
	Exhausted         bool
	RetryAfterSeconds int
}

type RateDecision struct {
	Allowed           bool
	RetryAfterSeconds int
}

type admissionLimiter interface {
	Admit(time.Time, string) RateDecision
}

type PasswordFailureLimiter interface {
	State(now time.Time, emailKey [32]byte) FailureState
	RecordFailure(now time.Time, emailKey [32]byte) FailureState
	ClearSuccess(emailKey [32]byte)
}

type PasswordRatePolicies struct {
	emailHMACKey          [32]byte
	loginIP               admissionLimiter
	registerOrForgotIP    admissionLimiter
	registerOrForgotEmail admissionLimiter
	verifyOrResetIP       admissionLimiter
	accountMutation       admissionLimiter
	failures              PasswordFailureLimiter
}

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

func (p *PasswordRatePolicies) AdmitLoginIP(now time.Time, clientIP netip.Addr) RateDecision {
	return p.loginIP.Admit(now, ipBucketKey(clientIP))
}

func (p *PasswordRatePolicies) AdmitRegisterOrForgotIP(now time.Time, clientIP netip.Addr) RateDecision {
	return p.registerOrForgotIP.Admit(now, ipBucketKey(clientIP))
}

func (p *PasswordRatePolicies) AdmitRegisterOrForgotEmail(now time.Time, canonicalEmail string) RateDecision {
	digest, ok := p.canonicalEmailDigest(canonicalEmail)
	if !ok {
		return deniedDecision
	}
	return p.registerOrForgotEmail.Admit(now, string(digest[:]))
}

func (p *PasswordRatePolicies) AdmitVerifyOrResetIP(now time.Time, clientIP netip.Addr) RateDecision {
	return p.verifyOrResetIP.Admit(now, ipBucketKey(clientIP))
}

func (p *PasswordRatePolicies) AdmitAccountMutation(now time.Time, userID uuid.UUID, clientIP netip.Addr) RateDecision {
	return p.accountMutation.Admit(now, accountMutationBucketKey(userID, clientIP))
}

func (p *PasswordRatePolicies) LoginFailureState(now time.Time, canonicalEmail string) FailureState {
	digest, ok := p.canonicalEmailDigest(canonicalEmail)
	if !ok {
		return deniedFailureState
	}
	return p.failures.State(now, digest)
}

func (p *PasswordRatePolicies) RecordLoginFailure(now time.Time, canonicalEmail string) FailureState {
	digest, ok := p.canonicalEmailDigest(canonicalEmail)
	if !ok {
		return deniedFailureState
	}
	return p.failures.RecordFailure(now, digest)
}

func (p *PasswordRatePolicies) ClearLoginSuccess(canonicalEmail string) {
	digest, ok := p.canonicalEmailDigest(canonicalEmail)
	if !ok {
		return
	}
	p.failures.ClearSuccess(digest)
}

var (
	deniedDecision     = RateDecision{Allowed: false, RetryAfterSeconds: 1}
	deniedFailureState = FailureState{Exhausted: true, RetryAfterSeconds: 1}
)

func ipBucketKey(addr netip.Addr) string {
	return "ip:" + addr.Unmap().String()
}

func accountMutationBucketKey(userID uuid.UUID, clientIP netip.Addr) string {
	return "user:" + userID.String() + "|" + ipBucketKey(clientIP)
}

var passwordRateDomainSeparator = []byte("aboutme.password-rate.v1\x00")

func emailDigest(emailHMACKey [32]byte, canonicalEmail string) [32]byte {
	mac := hmac.New(sha256.New, emailHMACKey[:])
	mac.Write(passwordRateDomainSeparator)
	mac.Write([]byte(canonicalEmail))
	var digest [32]byte
	copy(digest[:], mac.Sum(nil))
	return digest
}

func (p *PasswordRatePolicies) canonicalEmailDigest(canonicalEmail string) ([32]byte, bool) {
	canonical, err := accountemail.Canonicalize(canonicalEmail)
	if err != nil {
		return [32]byte{}, false
	}
	return emailDigest(p.emailHMACKey, canonical), true
}

type boundedAdmission struct {
	inner *api.BoundedRateLimiter
}

func newAdmissionLimiter(requests int, window time.Duration) admissionLimiter {
	return &boundedAdmission{
		inner: api.NewBoundedRateLimiter(api.RateLimiterConfig{
			Requests: requests,
			Window:   window,
			MaxKeys:  passwordRateMaxKeys,
		}),
	}
}

func (b *boundedAdmission) Admit(now time.Time, key string) RateDecision {
	allowed, retry := b.inner.Admit(now, key)
	if allowed {
		return RateDecision{Allowed: true}
	}
	return RateDecision{Allowed: false, RetryAfterSeconds: retry}
}
