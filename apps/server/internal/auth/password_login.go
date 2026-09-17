package auth

// Password login: credential verification, the snapshot re-check inside the
// session-issuing transaction, and the deferred password rehash.

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
