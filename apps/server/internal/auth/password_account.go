package auth

// Session-authenticated password operations: reauthentication (confirming
// the current password without changing it) and password change (replacing
// the credential and forcing a fresh session). Both lock the user, then the
// caller's session, then the factor policy, as
// docs/design/second-factor-authentication.md orders.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/password"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// ---- reauth ----

// reauth confirms the password for sess. For an enrolled account it creates a
// pending reauthentication and drops its token; the HTTP handler uses
// reauthWithSecondFactor to deliver the pending cookie.
func (s *PasswordService) reauth(ctx context.Context, sess store.Session, rawPassword, clientIP string) error {
	_, err := s.reauthWithSecondFactor(ctx, sess, rawPassword, clientIP, "")
	return err
}

// reauthWithSecondFactor confirms the password for the concrete session sess.
// For an unenrolled account it refreshes that session's primary proof and
// returns "". For an enrolled account it changes no session and returns a
// pending reauthentication token bound to sess, which only factor completion
// can turn into fresh proofs. previousPending is the browser's pending cookie,
// if any.
func (s *PasswordService) reauthWithSecondFactor(ctx context.Context, sess store.Session, rawPassword, clientIP, previousPending string) (string, error) {
	now := s.clock()

	if d := s.limits.AdmitAccountMutation(now, sess.UserID, clientAddrFromString(clientIP)); !d.Allowed {
		return "", &passwordRateLimitedError{retryAfterSeconds: d.RetryAfterSeconds}
	}
	normalized, err := password.Normalize(rawPassword)
	if err != nil {
		return "", errPasswordReauthFailed
	}

	cred, err := s.q.GetPasswordCredential(ctx, sess.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			_ = s.hasher.VerifyDummy(ctx, normalized) //nolint:errcheck // no credential: pay the same verify cost and ignore the dummy's error to avoid an oracle
			return "", errPasswordReauthFailed
		}
		return "", errPasswordUnavailable
	}
	res, err := s.hasher.Verify(ctx, string(cred.EncodedHash), normalized)
	if err != nil {
		if errors.Is(err, password.ErrHashInvalid) {
			return "", errPasswordReauthFailed
		}
		return "", errPasswordUnavailable
	}
	if !res.Match {
		return "", errPasswordReauthFailed
	}

	var enrolled bool
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		user, uerr := lockAccountUser(ctx, qtx, sess.UserID)
		if uerr != nil {
			return uerr
		}
		if s.userLockProbe != nil {
			s.userLockProbe()
		}
		if _, serr := lockCallerSession(ctx, qtx, user, sess.ID, now); serr != nil {
			return serr
		}
		policy, perr := lockSecondFactorPolicy(ctx, qtx, user.ID)
		if perr != nil {
			return perr
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
		if policy != nil {
			// Primary proof alone refreshes nothing for an enrolled account.
			enrolled = true
			return nil
		}
		return qtx.TouchReauthenticatedAt(ctx, store.TouchReauthenticatedAtParams{
			ID:                sess.ID,
			ReauthenticatedAt: now,
		})
	})
	if err != nil {
		if errors.Is(err, errPasswordReauthFailed) || errors.Is(err, ErrSessionInvalid) {
			return "", errPasswordReauthFailed
		}
		return "", errPasswordUnavailable
	}
	if !enrolled {
		return "", nil
	}
	pending, err := s.pending.Create(ctx, PendingAuthenticationRequest{
		UserID:            sess.UserID,
		Purpose:           PendingAuthenticationPurposeReauth,
		PrimaryVerifiedAt: now,
		ReturnPath:        settingsSessionsPath,
		SessionID:         &sess.ID,
		PreviousRawToken:  previousPending,
	})
	if err != nil {
		if errors.Is(err, ErrPendingAuthenticationRequired) {
			return "", errPasswordReauthFailed
		}
		return "", errPasswordUnavailable
	}
	return pending.RawToken, nil
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
		user, uerr := lockAccountUser(ctx, qtx, sess.UserID)
		if uerr != nil {
			return uerr
		}
		if s.userLockProbe != nil {
			s.userLockProbe()
		}
		// Add or change is a sensitive action: the concrete session, its
		// epoch, and both recent proofs of an enrolled account are rechecked
		// under the user lock before any write.
		live, gateErr := lockSensitiveSession(ctx, qtx, user, sess.ID, now)
		if gateErr != nil {
			return gateErr
		}
		current, cerr := qtx.GetPasswordCredentialForUpdate(ctx, sess.UserID)
		if cerr != nil {
			if errors.Is(cerr, pgx.ErrNoRows) {
				current = store.PasswordCredential{CreatedAt: now, ChangedAt: now}
			} else {
				return cerr
			}
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
		if errors.Is(err, errPasswordReauthRequired) || errors.Is(err, ErrSessionInvalid) || errors.Is(err, ErrReauthRequired) {
			return "", errPasswordReauthRequired
		}
		return "", errPasswordUnavailable
	}
	return newRaw, nil
}

// createFreshSessionTx mints the forced-replacement session after a credential
// change: fresh token and CSRF material with rotated_from NULL, preserving the
// old current session's absolute expiry, user agent, IP, and factor-proof time,
// copying the locked user's authentication epoch, and setting a new
// reauthenticated_at. It is not an ADR 0015 rotation lineage. A password change
// does not change the epoch, so the factor proof stays valid; see
// docs/design/second-factor-authentication.md.
func (s *PasswordService) createFreshSessionTx(ctx context.Context, qtx *store.Queries, user store.User, old store.Session, now time.Time) (string, error) {
	raw, err := randomSessionToken()
	if err != nil {
		return "", errPasswordUnavailable
	}
	csrf, err := randomCSRFSecret()
	if err != nil {
		return "", errPasswordUnavailable
	}
	if _, createErr := qtx.CreateSession(ctx, store.CreateSessionParams{
		UserID:                 user.ID,
		TokenHash:              hashSessionToken(raw),
		CSRFSecret:             csrf,
		CreatedAt:              now,
		LastSeenAt:             now,
		ReauthenticatedAt:      now,
		AbsoluteExpiresAt:      old.AbsoluteExpiresAt,
		UA:                     old.UA,
		IP:                     old.IP,
		AuthEpoch:              user.AuthEpoch,
		SecondFactorVerifiedAt: old.SecondFactorVerifiedAt,
	}); createErr != nil {
		return "", fmt.Errorf("auth: change password: create fresh session: %w", createErr)
	}
	return raw, nil
}
