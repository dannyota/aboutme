package auth

// PasswordService implements the password-based register, verify, login,
// forgot, reset, reauth, and change operations, and their transactional
// fences. It holds the exact dependencies the handlers need: the store
// pool/queries, the session manager (issue), the password policy and hasher,
// the encrypted outbox, and the rate policies. It never opens a short
// transaction around Argon2id hashing or HIBP lookups; those run before the
// transaction, and only the user/credential/session lock-recheck-commit work
// runs inside it.

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/password"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// PasswordServiceOptions is the full dependency set for the password service.
// NewPasswordService fails on any nil or invalid dependency.
type PasswordServiceOptions struct {
	Pool           *store.Pool
	Queries        *store.Queries
	Sessions       *SessionManager
	Policy         *password.Policy
	Hasher         *password.Hasher
	Outbox         *authmail.Outbox
	Limits         *PasswordRatePolicies
	PublicOrigin   string
	Clock          func() time.Time
	Entropy        io.Reader
	Logger         *slog.Logger
	TrustedProxies api.TrustedProxies
}

// PasswordService is the password-based HTTP service. See the package
// comment for the operation/lock-order contract.
type PasswordService struct {
	pool           *store.Pool
	q              *store.Queries
	sessions       *SessionManager
	policy         *password.Policy
	hasher         *password.Hasher
	outbox         *authmail.Outbox
	limits         *PasswordRatePolicies
	publicOrigin   string
	clock          func() time.Time
	entropy        io.Reader
	logger         *slog.Logger
	trustedProxies api.TrustedProxies

	// Test-only probes, nil in production. See export_test.go.
	userLockProbe               func()
	loginPreTxProbe             func()
	verifyRegistrationLockProbe func()
}

// NewPasswordService validates every dependency and returns the service. It
// fails closed: a nil pool, queries, session manager, policy, hasher, outbox,
// rate policies, clock, or entropy source, or an empty PublicOrigin, is an
// error. Logger and TrustedProxies may be nil (dev defaults).
func NewPasswordService(opts PasswordServiceOptions) (*PasswordService, error) {
	switch {
	case opts.Pool == nil:
		return nil, errors.New("auth: password service: nil pool")
	case opts.Queries == nil:
		return nil, errors.New("auth: password service: nil queries")
	case opts.Sessions == nil:
		return nil, errors.New("auth: password service: nil session manager")
	case opts.Policy == nil:
		return nil, errors.New("auth: password service: nil policy")
	case opts.Hasher == nil:
		return nil, errors.New("auth: password service: nil hasher")
	case opts.Outbox == nil:
		return nil, errors.New("auth: password service: nil outbox")
	case opts.Limits == nil:
		return nil, errors.New("auth: password service: nil rate policies")
	case opts.PublicOrigin == "":
		return nil, errors.New("auth: password service: empty public origin")
	case opts.Clock == nil:
		return nil, errors.New("auth: password service: nil clock")
	case opts.Entropy == nil:
		return nil, errors.New("auth: password service: nil entropy")
	}
	return &PasswordService{
		pool:           opts.Pool,
		q:              opts.Queries,
		sessions:       opts.Sessions,
		policy:         opts.Policy,
		hasher:         opts.Hasher,
		outbox:         opts.Outbox,
		limits:         opts.Limits,
		publicOrigin:   opts.PublicOrigin,
		clock:          opts.Clock,
		entropy:        opts.Entropy,
		logger:         opts.Logger,
		trustedProxies: opts.TrustedProxies,
	}, nil
}

// RegisterRoutes attaches the seven password operations to mux. The independent
// PasswordService owns these routes; OAuth Service.RegisterRoutes is untouched.
func (s *PasswordService) RegisterRoutes(mux *http.ServeMux) {
	mux.Handle(PasswordRegisterPath, route(http.MethodPost, s.handleRegister))
	mux.Handle(PasswordVerifyPath, route(http.MethodPost, s.handleVerify))
	mux.Handle(PasswordLoginPath, route(http.MethodPost, s.handleLogin))
	mux.Handle(PasswordForgotPath, route(http.MethodPost, s.handleForgot))
	mux.Handle(PasswordResetPath, route(http.MethodPost, s.handleReset))
	mux.Handle(PasswordReauthPath, route(http.MethodPost, s.passwordSessionChain(s.handleReauth)))
	mux.Handle(PasswordMePath, route(http.MethodPut, s.passwordSessionChain(s.handleChange)))
}

// ---- shared transaction helpers ----

// clientAddrFromString returns the canonical client IP as a netip.Addr for rate
// keying. An empty/unparseable string returns the zero Addr, which keys to a
// shared "invalid IP" bucket: the router's outer limiter already rejects
// unresolvable client IPs before these handlers run, so this is defensive only.
func clientAddrFromString(ip string) netip.Addr {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return netip.Addr{}
	}
	return addr
}

// clientIPString resolves the request's real client IP through the trusted-proxy
// boundary, or "" when it cannot be determined.
func (s *PasswordService) clientIPString(r *http.Request) string {
	ip, ok := api.ClientIP(r, s.trustedProxies)
	if !ok {
		return ""
	}
	return ip
}

// errPasswordRateLimitedCarrier reports a rate-limit rejection together with the
// Retry-After seconds. Handlers unwrap it with errors.As.
type passwordRateLimitedError struct{ retryAfterSeconds int }

func (e *passwordRateLimitedError) Error() string { return errPasswordRateLimited.Error() }
func (e *passwordRateLimitedError) Unwrap() error { return errPasswordRateLimited }

// errPasswordCredentialChanged marks a login whose credential hash changed
// between the snapshot and the transaction. The caller re-verifies once outside
// the transaction and retries.
var errPasswordCredentialChanged = errors.New("auth: password credential changed")

// errPasswordRegistrationRace marks a register whose registration insert lost a
// unique-email race to a concurrent registration; the outcome is still the
// generic 202.
var errPasswordRegistrationRace = errors.New("auth: password registration raced")

// recordLoginFailure records one wrong-password failure and returns the 401 or
// 429 outcome.
func (s *PasswordService) recordLoginFailure(now time.Time, canonicalEmail string) (string, error) {
	state := s.limits.RecordLoginFailure(now, canonicalEmail)
	if state.Exhausted {
		return "", &passwordRateLimitedError{retryAfterSeconds: state.RetryAfterSeconds}
	}
	return "", errPasswordAuthFailed
}

// newEmailPayload builds the outbox plaintext for one email. Link is empty for
// password_changed; verify/reset carry the canonical fragment link.
func newEmailPayload(canonicalEmail string, link string) authmail.Payload {
	return authmail.Payload{Version: 1, To: canonicalEmail, Link: link}
}

// verifyEmailLink and resetEmailLink build the verification and reset
// fragment links. The origin is the canonical production origin, matching
// authmail's link validation; it is not the configured PublicOrigin.
func verifyEmailLink(rawToken string) string {
	return canonicalEmailLinkOrigin + "/verify-email#token=" + rawToken
}

func resetEmailLink(rawToken string) string {
	return canonicalEmailLinkOrigin + "/reset-password#token=" + rawToken
}

// canonicalEmailLinkOrigin is the hard-coded origin used in verification and
// reset links (authmail's canonicalLinkOrigin). It is a named placeholder, not
// a sender identity.
const canonicalEmailLinkOrigin = "https://aboutme.vn"
