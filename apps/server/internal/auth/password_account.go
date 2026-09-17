package auth

// Session-authenticated password operations: reauthentication (confirming
// the current password without changing it) and password change (replacing
// the credential and forcing a fresh session).

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
