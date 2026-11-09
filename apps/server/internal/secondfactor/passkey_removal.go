package secondfactor

// Passkey removal and the shared factor-change commit that every factor
// enrollment, removal, and recovery-code regeneration finishes with.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/oauthsrv"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// RemovePasskey deletes one owned passkey. Final versus non-final comes only
// from ActiveFactorCount after the delete: final removal deletes the recovery
// codes and policy, clears the factor proof, and sends second_factor_disabled;
// otherwise the policy, codes, and proof stay and passkey_removed is sent.
func (s *Service) RemovePasskey(ctx context.Context, current store.Session, passkeyID uuid.UUID) (auth.SessionIssue, error) {
	var issue auth.SessionIssue
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		now := s.now()
		user, sess, policy, err := lockAccount(ctx, qtx, current, now)
		if err != nil {
			return err
		}
		if err = auth.RequireRecentSecondFactorReauth(user, policy, sess, now); err != nil {
			return err
		}
		if policy == nil {
			return auth.ErrSecondFactorNotFound
		}
		if _, err = qtx.GetWebAuthnCredentialForUpdate(ctx, store.GetWebAuthnCredentialForUpdateParams{ID: passkeyID, UserID: user.ID}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return auth.ErrSecondFactorNotFound
			}
			return fmt.Errorf("lock passkey: %w", err)
		}
		if _, err = qtx.DeleteWebAuthnCredentialForUser(ctx, store.DeleteWebAuthnCredentialForUserParams{ID: passkeyID, UserID: user.ID}); err != nil {
			return fmt.Errorf("delete passkey: %w", err)
		}
		remaining, err := s.ActiveFactorCount(ctx, qtx, user.ID)
		if err != nil {
			return err
		}
		kind, proof := authmail.KindPasskeyRemoved, sess.SecondFactorVerifiedAt
		if remaining == 0 {
			if _, err = qtx.DeleteSecondFactorRecoveryCodesForUser(ctx, user.ID); err != nil {
				return fmt.Errorf("delete recovery codes: %w", err)
			}
			if _, err = qtx.DeleteSecondFactorPolicyForUser(ctx, user.ID); err != nil {
				return fmt.Errorf("delete policy: %w", err)
			}
			kind, proof = authmail.KindSecondFactorDisabled, nil
		}
		issue, err = s.commitFactorChange(ctx, qtx, user, sess, proof, kind, now)
		return err
	})
	if err != nil {
		return auth.SessionIssue{}, err
	}
	return issue, nil
}

// commitFactorChange advances the epoch, replaces the current session and
// revokes every other one, then revokes every live agent grant and its token
// families, and enqueues the notification, all in the caller's transaction
// under the user lock. Sessions come before grants, as the lock order in
// docs/design/second-factor-authentication.md requires.
func (s *Service) commitFactorChange(ctx context.Context, qtx *store.Queries, user store.User, sess store.Session, factorProof *time.Time, kind authmail.Kind, now time.Time) (auth.SessionIssue, error) {
	if _, err := qtx.AdvanceUserAuthEpoch(ctx, user.ID); err != nil {
		return auth.SessionIssue{}, fmt.Errorf("advance epoch: %w", err)
	}
	issue, err := s.sessions.ReplaceAfterEpochChangeTx(ctx, qtx, user.ID, sess.ID, factorProof)
	if err != nil {
		return auth.SessionIssue{}, err
	}
	if err = oauthsrv.RevokeGrantsForEpochChangeTx(ctx, qtx, user.ID, now); err != nil {
		return auth.SessionIssue{}, err
	}
	if err = s.enqueueSecurityMail(ctx, qtx, user, kind, now, nil); err != nil {
		return auth.SessionIssue{}, err
	}
	return issue, nil
}
