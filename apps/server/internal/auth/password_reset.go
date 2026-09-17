package auth

// Forgot-password and reset-password: issuing a reset token for an owned
// password account, and turning a verified reset token into a new password
// credential with every other session revoked.

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/accountemail"
	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/password"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

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
		preflight, uerr := qtx.GetUserByCanonicalEmail(ctx, canonicalEmail)
		if uerr != nil {
			if errors.Is(uerr, pgx.ErrNoRows) {
				return nil // unknown: no-op
			}
			return uerr
		}
		user, uerr := qtx.GetUserForUpdate(ctx, preflight.ID)
		if uerr != nil {
			if errors.Is(uerr, pgx.ErrNoRows) {
				return nil
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
