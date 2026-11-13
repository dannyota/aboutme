package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/authmail"
	"danny.vn/aboutme/apps/server/internal/password"
	"danny.vn/aboutme/apps/server/internal/store"
)

func (s *PasswordService) reauth(ctx context.Context, sess store.Session, rawPassword, clientIP string) error {
	_, err := s.reauthWithSecondFactor(ctx, sess, rawPassword, clientIP, "")
	return err
}

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
