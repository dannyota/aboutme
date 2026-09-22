package secondfactor

// Row locking and ceremony persistence shared by the factor mutations. Every
// helper runs in the caller's transaction and follows the lock order in
// docs/design/second-factor-authentication.md: user, session, factor policy,
// credential, then pending authentication or ceremony.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// lockAccount locks the user, then the concrete caller session, then the
// factor policy, and rechecks that the session is live, owned, and current.
func lockAccount(ctx context.Context, qtx *store.Queries, current store.Session, now time.Time) (store.User, store.Session, *store.SecondFactorPolicy, error) {
	user, err := qtx.GetUserForUpdate(ctx, current.UserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.User{}, store.Session{}, nil, auth.ErrSessionInvalid
		}
		return store.User{}, store.Session{}, nil, fmt.Errorf("lock user: %w", err)
	}
	sess, err := qtx.GetSessionByIDForUpdate(ctx, current.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.User{}, store.Session{}, nil, auth.ErrSessionInvalid
		}
		return store.User{}, store.Session{}, nil, fmt.Errorf("lock session: %w", err)
	}
	if sess.UserID != user.ID || sess.AuthEpoch != user.AuthEpoch || auth.RequireLiveSession(sess, now) != nil {
		return store.User{}, store.Session{}, nil, auth.ErrSessionInvalid
	}
	policy, err := lockPolicy(ctx, qtx, user.ID)
	if err != nil {
		return store.User{}, store.Session{}, nil, err
	}
	return user, sess, policy, nil
}

func lockPolicy(ctx context.Context, qtx *store.Queries, userID uuid.UUID) (*store.SecondFactorPolicy, error) {
	policy, err := qtx.GetSecondFactorPolicyForUpdate(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock policy: %w", err)
	}
	return &policy, nil
}

func lockPolicyBeforePending(ctx context.Context, qtx *store.Queries, user store.User, _ *store.Session) error {
	_, err := lockPolicy(ctx, qtx, user.ID)
	return err
}

// claimCeremony locks the ceremony and consumes it only when it is live and
// bound to this user, purpose, epoch, and binding. Unknown, foreign, expired,
// consumed, and wrong-purpose ceremonies are left unchanged.
func claimCeremony(ctx context.Context, qtx *store.Queries, digest []byte, user store.User, purpose string, sessionID, pendingID *uuid.UUID, now time.Time) (store.WebauthnCeremony, error) {
	ceremony, err := qtx.GetWebAuthnCeremonyByTokenDigestForUpdate(ctx, digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.WebauthnCeremony{}, auth.ErrSecondFactorChallengeInvalid
	}
	if err != nil {
		return store.WebauthnCeremony{}, fmt.Errorf("lock ceremony: %w", err)
	}
	if ceremony.UserID != user.ID || ceremony.Purpose != purpose || ceremony.AuthEpoch != user.AuthEpoch ||
		!sameID(ceremony.SessionID, sessionID) || !sameID(ceremony.PendingAuthenticationID, pendingID) ||
		ceremony.ConsumedAt != nil || !ceremony.ExpiresAt.After(now) {
		return store.WebauthnCeremony{}, auth.ErrSecondFactorChallengeInvalid
	}
	claimed, err := qtx.ClaimWebAuthnCeremony(ctx, store.ClaimWebAuthnCeremonyParams{ConsumedAt: now, TokenDigest: digest})
	if errors.Is(err, pgx.ErrNoRows) {
		return store.WebauthnCeremony{}, auth.ErrSecondFactorChallengeInvalid
	}
	if err != nil {
		return store.WebauthnCeremony{}, fmt.Errorf("claim ceremony: %w", err)
	}
	return claimed, nil
}

// createCeremony consumes the binding's prior live ceremony and inserts a new
// one. It returns the ceremony ID and the raw challenge; only digests are
// stored.
func (s *Service) createCeremony(ctx context.Context, qtx *store.Queries, user store.User, purpose string, sessionID, pendingID *uuid.UUID, proposed []byte, now time.Time) (string, []byte, error) {
	if _, err := qtx.ConsumeLiveWebAuthnCeremoniesForBinding(ctx, store.ConsumeLiveWebAuthnCeremoniesForBindingParams{
		ConsumedAt: now, UserID: user.ID, Purpose: purpose, SessionID: sessionID, PendingAuthenticationID: pendingID,
	}); err != nil {
		return "", nil, fmt.Errorf("consume prior ceremony: %w", err)
	}
	token, err := s.random(ceremonyTokenBytes)
	if err != nil {
		return "", nil, err
	}
	challenge, err := s.random(challengeBytes)
	if err != nil {
		return "", nil, err
	}
	tokenDigest, challengeDigest := sha256.Sum256(token), sha256.Sum256(challenge)
	if _, err = qtx.CreateWebAuthnCeremony(ctx, store.CreateWebAuthnCeremonyParams{
		TokenDigest: tokenDigest[:], ChallengeDigest: challengeDigest[:], UserID: user.ID, Purpose: purpose,
		AuthEpoch: user.AuthEpoch, SessionID: sessionID, PendingAuthenticationID: pendingID,
		ProposedUserHandle: proposed, CreatedAt: now, ExpiresAt: now.Add(ceremonyLifetime),
	}); err != nil {
		return "", nil, fmt.Errorf("create ceremony: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(token), challenge, nil
}

// cleanup makes the bounded best-effort expiry calls before a ceremony is
// created. A failure writes one fixed warning and never fails the request.
func (s *Service) cleanup(ctx context.Context) {
	if _, err := s.q.DeleteExpiredPendingAuthentications(ctx, cleanupLimit); err != nil && s.logger != nil {
		s.logger.Warn("pending authentication cleanup failed")
	}
	if _, err := s.q.DeleteExpiredWebAuthnCeremonies(ctx, cleanupLimit); err != nil && s.logger != nil {
		s.logger.Warn("webauthn ceremony cleanup failed")
	}
}
