package auth

// Password login: credential verification, the snapshot re-check inside the
// session-issuing transaction, the deferred password rehash, and the pending
// login an enrolled account receives instead of a session. See
// docs/design/second-factor-authentication.md.

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/accountemail"
	"github.com/dannyota/aboutme/apps/server/internal/password"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// ---- login ----

// passwordLoginInput is one password login request. ReturnPath is already
// validated. PreviousPending is the browser's pending cookie, if any.
type passwordLoginInput struct {
	Email           string
	Password        string
	UA              string
	ClientIP        string
	ReturnPath      string
	PreviousPending string
}

// passwordLoginOutcome carries exactly one of a session token for an
// unenrolled account or a pending token for an enrolled account.
type passwordLoginOutcome struct {
	SessionRaw string
	PendingRaw string
}

// errPasswordSecondFactorPending tells a caller of login that the account is
// enrolled and received a pending login instead of a session.
var errPasswordSecondFactorPending = errors.New("auth: password login requires a second factor")

// login returns the session token for an unenrolled account. For an enrolled
// account it returns errPasswordSecondFactorPending; the HTTP handler uses
// loginWithSecondFactor to deliver the pending cookie.
func (s *PasswordService) login(ctx context.Context, email, rawPassword, ua, clientIP string) (string, error) {
	out, err := s.loginWithSecondFactor(ctx, passwordLoginInput{
		Email: email, Password: rawPassword, UA: ua, ClientIP: clientIP, ReturnPath: defaultLoginReturnPath,
	})
	if err != nil {
		return "", err
	}
	if out.PendingRaw != "" {
		return "", errPasswordSecondFactorPending
	}
	return out.SessionRaw, nil
}

// loginWithSecondFactor verifies the primary credential. An unenrolled account
// receives a session. An enrolled account receives a pending login bound to
// the validated return path and no session.
func (s *PasswordService) loginWithSecondFactor(ctx context.Context, in passwordLoginInput) (passwordLoginOutcome, error) {
	now := s.clock()
	email, rawPassword, ua, clientIP := in.Email, in.Password, in.UA, in.ClientIP

	canonicalEmail, err := accountemail.Canonicalize(email)
	if err != nil {
		return passwordLoginOutcome{}, errPasswordEmailInvalid
	}
	normalized, err := password.Normalize(rawPassword)
	if err != nil {
		return passwordLoginOutcome{}, errPasswordAuthFailed
	}
	if d := s.limits.AdmitLoginIP(now, clientAddrFromString(clientIP)); !d.Allowed {
		return passwordLoginOutcome{}, &passwordRateLimitedError{retryAfterSeconds: d.RetryAfterSeconds}
	}

	// Credential snapshot (or the dummy path for unknown/provider-only accounts).
	var user store.User
	var snapshotHash []byte
	var needsRehash bool
	uerr := s.lookupLoginUser(ctx, canonicalEmail, &user, &snapshotHash)
	if uerr != nil {
		return passwordLoginOutcome{}, uerr
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
			return passwordLoginOutcome{}, errPasswordUnavailable
		}
	} else {
		_ = s.hasher.VerifyDummy(ctx, normalized) //nolint:errcheck // unknown account: pay the same verify cost and ignore the dummy's error to avoid an oracle
	}
	if !matched {
		return passwordLoginOutcome{}, s.recordLoginFailure(now, canonicalEmail)
	}

	if s.loginPreTxProbe != nil {
		s.loginPreTxProbe()
	}

	issued, enrolled, err := s.issueLoginSession(ctx, user, snapshotHash, normalized, ua, clientIP, needsRehash, now)
	if err != nil {
		if errors.Is(err, errPasswordCredentialChanged) {
			// Re-read and re-verify once, outside the transaction.
			cred, cerr := s.q.GetPasswordCredential(ctx, user.ID)
			if cerr != nil {
				if errors.Is(cerr, pgx.ErrNoRows) {
					return passwordLoginOutcome{}, s.recordLoginFailure(now, canonicalEmail)
				}
				return passwordLoginOutcome{}, errPasswordUnavailable
			}
			res, verr := s.hasher.Verify(ctx, string(cred.EncodedHash), normalized)
			if verr != nil {
				if !errors.Is(verr, password.ErrHashInvalid) {
					return passwordLoginOutcome{}, errPasswordUnavailable
				}
				return passwordLoginOutcome{}, s.recordLoginFailure(now, canonicalEmail)
			}
			if !res.Match {
				return passwordLoginOutcome{}, s.recordLoginFailure(now, canonicalEmail)
			}
			issued, enrolled, err = s.issueLoginSession(ctx, user, cred.EncodedHash, normalized, ua, clientIP, res.NeedsRehash, now)
		}
		if err != nil {
			if errors.Is(err, errPasswordAuthFailed) {
				return passwordLoginOutcome{}, s.recordLoginFailure(now, canonicalEmail)
			}
			return passwordLoginOutcome{}, errPasswordUnavailable
		}
	}

	s.limits.ClearLoginSuccess(canonicalEmail)
	if !enrolled {
		return passwordLoginOutcome{SessionRaw: issued.RawToken}, nil
	}
	pending, err := s.pending.Create(ctx, PendingAuthenticationRequest{
		UserID:            user.ID,
		Purpose:           PendingAuthenticationPurposeLogin,
		PrimaryVerifiedAt: now,
		ReturnPath:        in.ReturnPath,
		PreviousRawToken:  in.PreviousPending,
	})
	if err != nil {
		return passwordLoginOutcome{}, errPasswordUnavailable
	}
	return passwordLoginOutcome{PendingRaw: pending.RawToken}, nil
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
// snapshot, optionally commits a prepared rehash, and then either issues the
// session or, for an enrolled account, reports enrolled without issuing one,
// all in one transaction. It returns errPasswordCredentialChanged when the
// snapshot no longer matches so the caller re-verifies outside the
// transaction.
func (s *PasswordService) issueLoginSession(ctx context.Context, user store.User, snapshotHash []byte, normalized, ua, ip string, needsRehash bool, now time.Time) (SessionIssue, bool, error) {
	rehash, err := s.prepareRehash(ctx, normalized, needsRehash)
	if err != nil {
		return SessionIssue{}, false, err
	}

	var (
		issued   SessionIssue
		enrolled bool
	)
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
		policy, perr := lockSecondFactorPolicy(ctx, qtx, user.ID)
		if perr != nil {
			return perr
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
		if policy != nil {
			enrolled = true
			return nil
		}
		var ierr error
		issued, ierr = s.sessions.IssueTx(ctx, qtx, user, ua, ip)
		return ierr
	})
	if err != nil {
		switch {
		case errors.Is(err, errPasswordCredentialChanged):
			return SessionIssue{}, false, errPasswordCredentialChanged
		case errors.Is(err, errPasswordAuthFailed):
			return SessionIssue{}, false, errPasswordAuthFailed
		default:
			return SessionIssue{}, false, errPasswordUnavailable
		}
	}
	return issued, enrolled, nil
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
