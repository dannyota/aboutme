package auth

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/authmail"
	"danny.vn/aboutme/apps/server/internal/password"
	"danny.vn/aboutme/apps/server/internal/store"
)

type PasswordServiceOptions struct {
	Pool                 *store.Pool
	Queries              *store.Queries
	Sessions             *SessionManager
	Policy               *password.Policy
	Hasher               *password.Hasher
	Outbox               *authmail.Outbox
	Limits               *PasswordRatePolicies
	PublicOrigin         string
	Clock                func() time.Time
	Entropy              io.Reader
	Logger               *slog.Logger
	TrustedProxies       api.TrustedProxies
	RegistrationDisabled bool
	Pending              *PendingAuthenticationManager
}

type PasswordService struct {
	pool            *store.Pool
	q               *store.Queries
	sessions        *SessionManager
	pending         *PendingAuthenticationManager
	policy          *password.Policy
	hasher          *password.Hasher
	outbox          *authmail.Outbox
	limits          *PasswordRatePolicies
	publicOrigin    string
	clock           func() time.Time
	entropy         io.Reader
	logger          *slog.Logger
	trustedProxies  api.TrustedProxies
	registrationOff bool

	userLockProbe               func()
	loginPreTxProbe             func()
	verifyRegistrationLockProbe func()
}

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
	pending := opts.Pending
	if pending == nil {
		pending = NewPendingAuthenticationManager(opts.Pool, opts.Logger)
	}
	return &PasswordService{
		pool:            opts.Pool,
		q:               opts.Queries,
		sessions:        opts.Sessions,
		pending:         pending,
		policy:          opts.Policy,
		hasher:          opts.Hasher,
		outbox:          opts.Outbox,
		limits:          opts.Limits,
		publicOrigin:    opts.PublicOrigin,
		clock:           opts.Clock,
		entropy:         opts.Entropy,
		logger:          opts.Logger,
		trustedProxies:  opts.TrustedProxies,
		registrationOff: opts.RegistrationDisabled,
	}, nil
}

func (s *PasswordService) RegisterRoutes(mux *http.ServeMux) {
	if !s.registrationOff {
		mux.Handle(PasswordRegisterPath, route(http.MethodPost, s.handleRegister))
	}
	mux.Handle(PasswordVerifyPath, route(http.MethodPost, s.handleVerify))
	mux.Handle(PasswordLoginPath, route(http.MethodPost, s.handleLogin))
	mux.Handle(PasswordForgotPath, route(http.MethodPost, s.handleForgot))
	mux.Handle(PasswordResetPath, route(http.MethodPost, s.handleReset))
	mux.Handle(PasswordReauthPath, route(http.MethodPost, s.passwordSessionChain(s.handleReauth)))
	mux.Handle(PasswordMePath, route(http.MethodPut, s.passwordSessionChain(s.handleChange)))
}

func clientAddrFromString(ip string) netip.Addr {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return netip.Addr{}
	}
	return addr
}

func (s *PasswordService) clientIPString(r *http.Request) string {
	ip, ok := api.ClientIP(r, s.trustedProxies)
	if !ok {
		return ""
	}
	return ip
}

type passwordRateLimitedError struct{ retryAfterSeconds int }

func (e *passwordRateLimitedError) Error() string { return errPasswordRateLimited.Error() }
func (e *passwordRateLimitedError) Unwrap() error { return errPasswordRateLimited }

var errPasswordCredentialChanged = errors.New("auth: password credential changed")

var errPasswordRegistrationRace = errors.New("auth: password registration raced")

func (s *PasswordService) recordLoginFailure(now time.Time, canonicalEmail string) error {
	state := s.limits.RecordLoginFailure(now, canonicalEmail)
	if state.Exhausted {
		return &passwordRateLimitedError{retryAfterSeconds: state.RetryAfterSeconds}
	}
	return errPasswordAuthFailed
}

func newEmailPayload(canonicalEmail string, link string) authmail.Payload {
	return authmail.Payload{Version: 1, To: canonicalEmail, Link: link}
}

func verifyEmailLink(rawToken string) string {
	return canonicalEmailLinkOrigin + "/verify-email#token=" + rawToken
}

func resetEmailLink(rawToken string) string {
	return canonicalEmailLinkOrigin + "/reset-password#token=" + rawToken
}

const canonicalEmailLinkOrigin = "https://aboutme.vn"
