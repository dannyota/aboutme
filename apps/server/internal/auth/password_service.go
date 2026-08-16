package auth

// PasswordService implements the seven Phase PA password operations and their
// D4 transactional fences. It holds the exact dependencies the handlers need:
// the store pool/queries, the session manager (issue), the password policy and
// hasher, the encrypted outbox, and the D6 rate policies. It never opens a
// short transaction around Argon2id hashing or HIBP lookups; those run before
// the transaction, and only the user/credential/session lock-recheck-commit
// work runs inside it.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/accountemail"
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

// PasswordService is the Phase PA password HTTP service. See the package
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

// ---- register ----

func (s *PasswordService) register(ctx context.Context, name, email, rawPassword, clientIP string) error {
	now := s.clock()

	canonicalEmail, err := accountemail.Canonicalize(email)
	if err != nil {
		return errPasswordEmailInvalid
	}
	name, err = normalizeRegistrationName(name)
	if err != nil {
		return errPasswordNameInvalid
	}

	// Email and IP budgets are shared by register and forgot (D6).
	if d := s.limits.AdmitRegisterOrForgotEmail(now, canonicalEmail); !d.Allowed {
		return &passwordRateLimitedError{retryAfterSeconds: d.RetryAfterSeconds}
	}
	if d := s.limits.AdmitRegisterOrForgotIP(now, clientAddrFromString(clientIP)); !d.Allowed {
		return &passwordRateLimitedError{retryAfterSeconds: d.RetryAfterSeconds}
	}

	// Expensive password policy/hash runs regardless of email ownership so an
	// owned and an unowned email are indistinguishable in cost.
	check, err := s.policy.CheckNew(ctx, rawPassword)
	if err != nil {
		if errors.Is(err, password.ErrBreachUnavailable) {
			return errPasswordUnavailable
		}
		return err // length/common/breached: mapped by the handler
	}
	encodedHash, err := s.hasher.Hash(ctx, check.Normalized)
	if err != nil {
		return errPasswordUnavailable
	}

	token, err := password.NewToken(s.entropy)
	if err != nil {
		return errPasswordUnavailable
	}
	jobID, err := uuid.NewV7FromReader(s.entropy)
	if err != nil {
		return errPasswordUnavailable
	}
	expiresAt := now.Add(passwordRegistrationTTL)
	payload := newEmailPayload(canonicalEmail, verifyEmailLink(token.Raw))

	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		// Owned email: discard the prepared material, no writes, generic 202.
		if _, uerr := qtx.GetUserByCanonicalEmail(ctx, canonicalEmail); uerr == nil {
			return nil
		} else if !errors.Is(uerr, pgx.ErrNoRows) {
			return uerr
		}
		// Unowned email: replace any prior registration (cascading its jobs).
		if prior, rerr := qtx.GetPasswordRegistrationByEmailForUpdate(ctx, canonicalEmail); rerr == nil {
			if _, derr := qtx.DeletePasswordRegistration(ctx, prior.ID); derr != nil {
				return derr
			}
		} else if !errors.Is(rerr, pgx.ErrNoRows) {
			return rerr
		}
		reg, cerr := qtx.CreatePasswordRegistration(ctx, store.CreatePasswordRegistrationParams{
			Email:       canonicalEmail,
			Name:        name,
			EncodedHash: []byte(encodedHash),
			TokenDigest: token.Digest[:],
			CreatedAt:   now,
			ExpiresAt:   expiresAt,
		})
		if cerr != nil {
			if isUniqueViolation(cerr) {
				return errPasswordRegistrationRace
			}
			return cerr
		}
		return s.outbox.EnqueueTx(ctx, qtx, authmail.EnqueueRequest{
			JobID:          jobID,
			Kind:           authmail.KindVerify,
			RegistrationID: &reg.ID,
			TokenDigest:    &token.Digest,
			Payload:        payload,
			ExpiresAt:      expiresAt,
		})
	})
	if err != nil {
		if errors.Is(err, errPasswordRegistrationRace) {
			return nil // a concurrent registration won; still the generic 202
		}
		return errPasswordUnavailable
	}
	return nil
}

// ---- verify ----

func (s *PasswordService) verify(ctx context.Context, rawToken, clientIP string) error {
	now := s.clock()

	digest, err := password.DigestToken(rawToken)
	if err != nil {
		return errPasswordTokenShape
	}
	if d := s.limits.AdmitVerifyOrResetIP(now, clientAddrFromString(clientIP)); !d.Allowed {
		return &passwordRateLimitedError{retryAfterSeconds: d.RetryAfterSeconds}
	}

	reg, err := s.q.GetPasswordRegistrationByDigest(ctx, digest[:])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errPasswordTokenInvalid
		}
		return errPasswordUnavailable
	}
	if now.After(reg.ExpiresAt) {
		return errPasswordTokenInvalid
	}

	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		live, lerr := qtx.GetPasswordRegistrationForUpdate(ctx, reg.ID)
		if lerr != nil {
			if errors.Is(lerr, pgx.ErrNoRows) {
				return errPasswordTokenInvalid
			}
			return lerr
		}
		if now.After(live.ExpiresAt) {
			return errPasswordTokenInvalid
		}
		if s.verifyRegistrationLockProbe != nil {
			s.verifyRegistrationLockProbe()
		}
		// Email now owned (e.g. a provider signup won the race): consume the
		// registration without creating a password.
		if _, uerr := qtx.GetUserByCanonicalEmail(ctx, live.Email); uerr == nil {
			_, derr := qtx.DeletePasswordRegistration(ctx, live.ID)
			return derr
		} else if !errors.Is(uerr, pgx.ErrNoRows) {
			return uerr
		}
		user, cerr := qtx.CreateUser(ctx, store.CreateUserParams{Email: live.Email, Name: live.Name})
		if cerr != nil {
			if isUniqueViolation(cerr) {
				return errPasswordEmailOwnedRace
			}
			return cerr
		}
		if _, uerr := qtx.UpsertPasswordCredential(ctx, store.UpsertPasswordCredentialParams{
			UserID:      user.ID,
			EncodedHash: live.EncodedHash,
			CreatedAt:   now,
			ChangedAt:   now,
		}); uerr != nil {
			return uerr
		}
		_, derr := qtx.DeletePasswordRegistration(ctx, live.ID)
		return derr
	})
	if err != nil {
		if errors.Is(err, errPasswordEmailOwnedRace) {
			return s.consumeRegistrationForOwnedEmail(ctx, reg.ID)
		}
		if errors.Is(err, errPasswordTokenInvalid) {
			return errPasswordTokenInvalid
		}
		return errPasswordUnavailable
	}
	return nil
}

// errPasswordEmailOwnedRace marks a verify whose user insert lost a
// unique-email race; the registration is consumed without a password.
var errPasswordEmailOwnedRace = errors.New("auth: password email owned race")

// consumeRegistrationForOwnedEmail deletes the registration after a provider
// signup won the email race. A concurrent winner may already have consumed it;
// that is still the success outcome.
func (s *PasswordService) consumeRegistrationForOwnedEmail(ctx context.Context, registrationID uuid.UUID) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		live, lerr := qtx.GetPasswordRegistrationForUpdate(ctx, registrationID)
		if lerr != nil {
			if errors.Is(lerr, pgx.ErrNoRows) {
				return nil
			}
			return lerr
		}
		_, derr := qtx.DeletePasswordRegistration(ctx, live.ID)
		return derr
	})
	if err != nil {
		return errPasswordUnavailable
	}
	return nil
}

// ---- login ----

func (s *PasswordService) login(ctx context.Context, email, rawPassword, ua, clientIP string) (string, error) {
	now := s.clock()

	canonicalEmail, err := accountemail.Canonicalize(email)
	if err != nil {
		return "", errPasswordEmailInvalid
	}
	normalized, err := password.Normalize(rawPassword)
	if err != nil {
		return "", errPasswordAuthFailed
	}
	if d := s.limits.AdmitLoginIP(now, clientAddrFromString(clientIP)); !d.Allowed {
		return "", &passwordRateLimitedError{retryAfterSeconds: d.RetryAfterSeconds}
	}

	// Credential snapshot (or the dummy path for unknown/provider-only accounts).
	var user store.User
	var snapshotHash []byte
	var needsRehash bool
	uerr := s.lookupLoginUser(ctx, canonicalEmail, &user, &snapshotHash)
	if uerr != nil {
		return "", uerr
	}

	matched := false
	if snapshotHash != nil {
		res, verr := s.hasher.Verify(ctx, string(snapshotHash), normalized)
		switch {
		case verr == nil:
			matched, needsRehash = res.Match, res.NeedsRehash
		case errors.Is(verr, password.ErrHashInvalid):
			_ = s.hasher.VerifyDummy(ctx, normalized) //nolint:errcheck // corrupt hash: pay the cost so no account oracle opens, and the dummy's own error is irrelevant
		default:
			return "", errPasswordUnavailable
		}
	} else {
		_ = s.hasher.VerifyDummy(ctx, normalized) //nolint:errcheck // unknown account: pay the same verify cost and ignore the dummy's error to avoid an oracle
	}
	if !matched {
		return s.recordLoginFailure(now, canonicalEmail)
	}

	if s.loginPreTxProbe != nil {
		s.loginPreTxProbe()
	}

	issued, err := s.issueLoginSession(ctx, user, snapshotHash, normalized, ua, clientIP, needsRehash, now)
	if err != nil {
		if errors.Is(err, errPasswordCredentialChanged) {
			// Re-read and re-verify once, outside the transaction.
			cred, cerr := s.q.GetPasswordCredential(ctx, user.ID)
			if cerr != nil {
				if errors.Is(cerr, pgx.ErrNoRows) {
					return s.recordLoginFailure(now, canonicalEmail)
				}
				return "", errPasswordUnavailable
			}
			res, verr := s.hasher.Verify(ctx, string(cred.EncodedHash), normalized)
			if verr != nil {
				if !errors.Is(verr, password.ErrHashInvalid) {
					return "", errPasswordUnavailable
				}
				return s.recordLoginFailure(now, canonicalEmail)
			}
			if !res.Match {
				return s.recordLoginFailure(now, canonicalEmail)
			}
			issued, err = s.issueLoginSession(ctx, user, cred.EncodedHash, normalized, ua, clientIP, res.NeedsRehash, now)
		}
		if err != nil {
			if errors.Is(err, errPasswordAuthFailed) {
				return s.recordLoginFailure(now, canonicalEmail)
			}
			return "", errPasswordUnavailable
		}
	}

	s.limits.ClearLoginSuccess(canonicalEmail)
	return issued.RawToken, nil
}

// lookupLoginUser fills user and snapshotHash (nil for unknown/provider-only
// accounts). It never returns a password-package error; the caller runs the
// dummy verify.
func (s *PasswordService) lookupLoginUser(ctx context.Context, canonicalEmail string, user *store.User, snapshotHash *[]byte) error {
	u, err := s.q.GetUserByCanonicalEmail(ctx, canonicalEmail)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // unknown account
		}
		return errPasswordUnavailable
	}
	*user = u
	cred, cerr := s.q.GetPasswordCredential(ctx, u.ID)
	if cerr == nil {
		*snapshotHash = cred.EncodedHash
		return nil
	}
	if errors.Is(cerr, pgx.ErrNoRows) {
		return nil // provider-only account
	}
	return errPasswordUnavailable
}

// issueLoginSession locks the user, rechecks the credential against the
// snapshot, optionally commits a prepared rehash, and issues the session — all
// in one transaction. It returns errPasswordCredentialChanged when the snapshot
// no longer matches so the caller re-verifies outside the transaction.
func (s *PasswordService) issueLoginSession(ctx context.Context, user store.User, snapshotHash []byte, normalized, ua, ip string, needsRehash bool, now time.Time) (SessionIssue, error) {
	rehash, err := s.prepareRehash(ctx, normalized, needsRehash)
	if err != nil {
		return SessionIssue{}, err
	}

	var issued SessionIssue
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		if _, lerr := qtx.GetUserForUpdate(ctx, user.ID); lerr != nil {
			if errors.Is(lerr, pgx.ErrNoRows) {
				return errPasswordAuthFailed
			}
			return lerr
		}
		if s.userLockProbe != nil {
			s.userLockProbe()
		}
		cred, cerr := qtx.GetPasswordCredentialForUpdate(ctx, user.ID)
		if cerr != nil {
			if errors.Is(cerr, pgx.ErrNoRows) {
				return errPasswordCredentialChanged
			}
			return cerr
		}
		if !bytes.Equal(cred.EncodedHash, snapshotHash) {
			return errPasswordCredentialChanged
		}
		if rehash != "" {
			if _, uerr := qtx.UpsertPasswordCredential(ctx, store.UpsertPasswordCredentialParams{
				UserID:      user.ID,
				EncodedHash: []byte(rehash),
				CreatedAt:   cred.CreatedAt,
				ChangedAt:   now,
			}); uerr != nil {
				return uerr
			}
		}
		var ierr error
		issued, ierr = s.sessions.IssueTx(ctx, qtx, user, ua, ip)
		return ierr
	})
	if err != nil {
		switch {
		case errors.Is(err, errPasswordCredentialChanged):
			return SessionIssue{}, errPasswordCredentialChanged
		case errors.Is(err, errPasswordAuthFailed):
			return SessionIssue{}, errPasswordAuthFailed
		default:
			return SessionIssue{}, errPasswordUnavailable
		}
	}
	return issued, nil
}

// prepareRehash derives a fresh encoding for normalized only when the verified
// snapshot needs it, outside any transaction.
func (s *PasswordService) prepareRehash(ctx context.Context, normalized string, needsRehash bool) (string, error) {
	if !needsRehash {
		return "", nil
	}
	rehash, err := s.hasher.Hash(ctx, normalized)
	if err != nil {
		return "", errPasswordUnavailable
	}
	return rehash, nil
}

// ---- forgot ----

func (s *PasswordService) forgot(ctx context.Context, email, clientIP string) error {
	now := s.clock()

	canonicalEmail, err := accountemail.Canonicalize(email)
	if err != nil {
		return errPasswordEmailInvalid
	}
	if d := s.limits.AdmitRegisterOrForgotEmail(now, canonicalEmail); !d.Allowed {
		return &passwordRateLimitedError{retryAfterSeconds: d.RetryAfterSeconds}
	}
	if d := s.limits.AdmitRegisterOrForgotIP(now, clientAddrFromString(clientIP)); !d.Allowed {
		return &passwordRateLimitedError{retryAfterSeconds: d.RetryAfterSeconds}
	}

	// Prepare the token and payload before the ownership lookup so the two
	// account states are indistinguishable.
	token, err := password.NewToken(s.entropy)
	if err != nil {
		return errPasswordUnavailable
	}
	jobID, err := uuid.NewV7FromReader(s.entropy)
	if err != nil {
		return errPasswordUnavailable
	}
	expiresAt := now.Add(passwordResetTokenTTL)
	payload := newEmailPayload(canonicalEmail, resetEmailLink(token.Raw))

	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		user, uerr := qtx.GetUserByCanonicalEmail(ctx, canonicalEmail)
		if uerr != nil {
			if errors.Is(uerr, pgx.ErrNoRows) {
				return nil // unknown: no-op
			}
			return uerr
		}
		if _, cerr := qtx.GetPasswordCredentialForUpdate(ctx, user.ID); cerr != nil {
			if errors.Is(cerr, pgx.ErrNoRows) {
				return nil // provider-only: no-op
			}
			return cerr
		}
		if prior, rerr := qtx.GetPasswordResetTokenByUserForUpdate(ctx, user.ID); rerr == nil {
			if _, derr := qtx.DeletePasswordResetToken(ctx, prior.ID); derr != nil {
				return derr
			}
		} else if !errors.Is(rerr, pgx.ErrNoRows) {
			return rerr
		}
		rt, cerr := qtx.CreatePasswordResetToken(ctx, store.CreatePasswordResetTokenParams{
			UserID:      user.ID,
			TokenDigest: token.Digest[:],
			CreatedAt:   now,
			ExpiresAt:   expiresAt,
		})
		if cerr != nil {
			return cerr
		}
		return s.outbox.EnqueueTx(ctx, qtx, authmail.EnqueueRequest{
			JobID:        jobID,
			Kind:         authmail.KindReset,
			ResetTokenID: &rt.ID,
			TokenDigest:  &token.Digest,
			Payload:      payload,
			ExpiresAt:    expiresAt,
		})
	})
	if err != nil {
		return errPasswordUnavailable
	}
	return nil
}

// ---- reset ----

func (s *PasswordService) reset(ctx context.Context, rawToken, rawPassword, clientIP string) error {
	now := s.clock()

	digest, err := password.DigestToken(rawToken)
	if err != nil {
		return errPasswordTokenShape
	}
	if d := s.limits.AdmitVerifyOrResetIP(now, clientAddrFromString(clientIP)); !d.Allowed {
		return &passwordRateLimitedError{retryAfterSeconds: d.RetryAfterSeconds}
	}

	rt, err := s.q.GetPasswordResetTokenByDigest(ctx, digest[:])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errPasswordTokenInvalid
		}
		return errPasswordUnavailable
	}
	if now.After(rt.ExpiresAt) {
		return errPasswordTokenInvalid
	}

	// Policy/hash/notification preparation runs before the transaction.
	check, err := s.policy.CheckNew(ctx, rawPassword)
	if err != nil {
		if errors.Is(err, password.ErrBreachUnavailable) {
			return errPasswordUnavailable
		}
		return err
	}
	encodedHash, err := s.hasher.Hash(ctx, check.Normalized)
	if err != nil {
		return errPasswordUnavailable
	}
	jobID, err := uuid.NewV7FromReader(s.entropy)
	if err != nil {
		return errPasswordUnavailable
	}

	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		user, uerr := qtx.GetUserForUpdate(ctx, rt.UserID)
		if uerr != nil {
			return uerr
		}
		if s.userLockProbe != nil {
			s.userLockProbe()
		}
		cred, cerr := qtx.GetPasswordCredentialForUpdate(ctx, rt.UserID)
		if cerr != nil {
			return cerr
		}
		live, lerr := qtx.GetPasswordResetTokenForUpdate(ctx, rt.ID)
		if lerr != nil {
			if errors.Is(lerr, pgx.ErrNoRows) {
				return errPasswordTokenInvalid
			}
			return lerr
		}
		if now.After(live.ExpiresAt) {
			return errPasswordTokenInvalid
		}
		if _, uerr := qtx.UpsertPasswordCredential(ctx, store.UpsertPasswordCredentialParams{
			UserID:      rt.UserID,
			EncodedHash: []byte(encodedHash),
			CreatedAt:   cred.CreatedAt,
			ChangedAt:   now,
		}); uerr != nil {
			return uerr
		}
		if _, derr := qtx.DeletePasswordResetToken(ctx, live.ID); derr != nil {
			return derr
		}
		if _, rerr := qtx.RevokeAllSessions(ctx, store.RevokeAllSessionsParams{UserID: rt.UserID, RevokedAt: &now}); rerr != nil {
			return rerr
		}
		return s.outbox.EnqueueTx(ctx, qtx, authmail.EnqueueRequest{
			JobID:     jobID,
			Kind:      authmail.KindPasswordChanged,
			UserID:    &rt.UserID,
			Payload:   newEmailPayload(user.Email, ""),
			ExpiresAt: now.Add(passwordNotificationTTL),
		})
	})
	if err != nil {
		if errors.Is(err, errPasswordTokenInvalid) {
			return errPasswordTokenInvalid
		}
		return errPasswordUnavailable
	}
	return nil
}

// ---- reauth ----

func (s *PasswordService) reauth(ctx context.Context, sess store.Session, rawPassword, clientIP string) error {
	now := s.clock()

	if d := s.limits.AdmitAccountMutation(now, sess.UserID, clientAddrFromString(clientIP)); !d.Allowed {
		return &passwordRateLimitedError{retryAfterSeconds: d.RetryAfterSeconds}
	}
	normalized, err := password.Normalize(rawPassword)
	if err != nil {
		return errPasswordReauthFailed
	}

	cred, err := s.q.GetPasswordCredential(ctx, sess.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			_ = s.hasher.VerifyDummy(ctx, normalized) //nolint:errcheck // no credential: pay the same verify cost and ignore the dummy's error to avoid an oracle
			return errPasswordReauthFailed
		}
		return errPasswordUnavailable
	}
	res, err := s.hasher.Verify(ctx, string(cred.EncodedHash), normalized)
	if err != nil {
		if errors.Is(err, password.ErrHashInvalid) {
			return errPasswordReauthFailed
		}
		return errPasswordUnavailable
	}
	if !res.Match {
		return errPasswordReauthFailed
	}

	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		if _, uerr := qtx.GetUserForUpdate(ctx, sess.UserID); uerr != nil {
			return uerr
		}
		if s.userLockProbe != nil {
			s.userLockProbe()
		}
		current, cerr := qtx.GetPasswordCredentialForUpdate(ctx, sess.UserID)
		if cerr != nil {
			if errors.Is(cerr, pgx.ErrNoRows) {
				return errPasswordReauthFailed
			}
			return cerr
		}
		if !bytes.Equal(current.EncodedHash, cred.EncodedHash) {
			return errPasswordReauthFailed
		}
		live, serr := qtx.GetSessionByIDForUpdate(ctx, sess.ID)
		if serr != nil {
			if errors.Is(serr, pgx.ErrNoRows) {
				return errPasswordReauthFailed
			}
			return serr
		}
		if live.RevokedAt != nil {
			return errPasswordReauthFailed
		}
		return qtx.TouchReauthenticatedAt(ctx, store.TouchReauthenticatedAtParams{
			ID:                sess.ID,
			ReauthenticatedAt: now,
		})
	})
	if err != nil {
		if errors.Is(err, errPasswordReauthFailed) {
			return errPasswordReauthFailed
		}
		return errPasswordUnavailable
	}
	return nil
}

// ---- add/change (PUT /me/password) ----

func (s *PasswordService) change(ctx context.Context, sess store.Session, rawPassword, clientIP string) (string, error) {
	now := s.clock()

	if d := s.limits.AdmitAccountMutation(now, sess.UserID, clientAddrFromString(clientIP)); !d.Allowed {
		return "", &passwordRateLimitedError{retryAfterSeconds: d.RetryAfterSeconds}
	}
	check, err := s.policy.CheckNew(ctx, rawPassword)
	if err != nil {
		if errors.Is(err, password.ErrBreachUnavailable) {
			return "", errPasswordUnavailable
		}
		return "", err
	}
	encodedHash, err := s.hasher.Hash(ctx, check.Normalized)
	if err != nil {
		return "", errPasswordUnavailable
	}
	jobID, err := uuid.NewV7FromReader(s.entropy)
	if err != nil {
		return "", errPasswordUnavailable
	}

	var newRaw string
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		user, uerr := qtx.GetUserForUpdate(ctx, sess.UserID)
		if uerr != nil {
			return uerr
		}
		if s.userLockProbe != nil {
			s.userLockProbe()
		}
		current, cerr := qtx.GetPasswordCredentialForUpdate(ctx, sess.UserID)
		if cerr != nil {
			if errors.Is(cerr, pgx.ErrNoRows) {
				current = store.PasswordCredential{CreatedAt: now, ChangedAt: now}
			} else {
				return cerr
			}
		}
		live, serr := qtx.GetSessionByIDForUpdate(ctx, sess.ID)
		if serr != nil {
			if errors.Is(serr, pgx.ErrNoRows) {
				return errPasswordReauthRequired
			}
			return serr
		}
		if live.RevokedAt != nil {
			return errPasswordReauthRequired
		}
		if _, uerr := qtx.UpsertPasswordCredential(ctx, store.UpsertPasswordCredentialParams{
			UserID:      sess.UserID,
			EncodedHash: []byte(encodedHash),
			CreatedAt:   current.CreatedAt,
			ChangedAt:   now,
		}); uerr != nil {
			return uerr
		}
		if _, rerr := qtx.RevokeAllSessions(ctx, store.RevokeAllSessionsParams{UserID: sess.UserID, RevokedAt: &now}); rerr != nil {
			return rerr
		}
		newRaw, err = s.createFreshSessionTx(ctx, qtx, user, live, now)
		if err != nil {
			return err
		}
		return s.outbox.EnqueueTx(ctx, qtx, authmail.EnqueueRequest{
			JobID:     jobID,
			Kind:      authmail.KindPasswordChanged,
			UserID:    &sess.UserID,
			Payload:   newEmailPayload(user.Email, ""),
			ExpiresAt: now.Add(passwordNotificationTTL),
		})
	})
	if err != nil {
		if errors.Is(err, errPasswordReauthRequired) {
			return "", errPasswordReauthRequired
		}
		return "", errPasswordUnavailable
	}
	return newRaw, nil
}

// createFreshSessionTx mints the forced-replacement session after a credential
// change: fresh token and CSRF material with rotated_from NULL, preserving the
// old current session's absolute expiry, user agent, and IP, and setting a new
// reauthenticated_at. It is not an ADR 0015 rotation lineage.
func (s *PasswordService) createFreshSessionTx(ctx context.Context, qtx *store.Queries, user store.User, old store.Session, now time.Time) (string, error) {
	raw, err := randomSessionToken()
	if err != nil {
		return "", errPasswordUnavailable
	}
	csrf, err := randomCSRFSecret()
	if err != nil {
		return "", errPasswordUnavailable
	}
	if _, err := qtx.CreateSession(ctx, store.CreateSessionParams{
		UserID:            user.ID,
		TokenHash:         hashSessionToken(raw),
		CSRFSecret:        csrf,
		CreatedAt:         now,
		LastSeenAt:        now,
		ReauthenticatedAt: now,
		AbsoluteExpiresAt: old.AbsoluteExpiresAt,
		UA:                old.UA,
		IP:                old.IP,
	}); err != nil {
		return "", fmt.Errorf("auth: change password: create fresh session: %w", err)
	}
	return raw, nil
}

// verifyEmailLink and resetEmailLink build the D8 fragment links. The origin is
// the canonical production origin, matching authmail's link validation; it is
// not the configured PublicOrigin.
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
