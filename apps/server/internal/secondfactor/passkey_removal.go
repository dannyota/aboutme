package secondfactor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/authmail"
	"danny.vn/aboutme/apps/server/internal/oauthsrv"
	"danny.vn/aboutme/apps/server/internal/store"
)

func (s *Service) RemovePasskey(ctx context.Context, current store.Session, passkeyID uuid.UUID) (auth.SessionIssue, error) {
	var issue auth.SessionIssue
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		user, sess, policy, err := lockAccountRows(ctx, qtx, current)
		if err != nil {
			return err
		}
		_, passkeyErr := qtx.GetWebAuthnCredentialForUpdate(ctx, store.GetWebAuthnCredentialForUpdateParams{ID: passkeyID, UserID: user.ID})
		if passkeyErr != nil && !errors.Is(passkeyErr, pgx.ErrNoRows) {
			return fmt.Errorf("lock passkey: %w", passkeyErr)
		}
		now := s.now()
		if err = auth.RequireRecentSecondFactorReauth(user, policy, sess, now); err != nil {
			return err
		}
		if policy == nil || passkeyErr != nil {
			return auth.ErrSecondFactorNotFound
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
		if err != nil {
			return err
		}
		s.sessions.AccountSessionsEndedLocked(user.ID)
		return nil
	})
	if err != nil {
		return auth.SessionIssue{}, err
	}
	return issue, nil
}

func (s *Service) commitFactorChange(ctx context.Context, qtx *store.Queries, user store.User, sess store.Session, factorProof *time.Time, kind authmail.Kind, now time.Time) (auth.SessionIssue, error) {
	if _, err := qtx.AdvanceUserAuthEpoch(ctx, user.ID); err != nil {
		return auth.SessionIssue{}, fmt.Errorf("advance epoch: %w", err)
	}
	issue, err := s.sessions.ReplaceAfterEpochChangeTx(ctx, qtx, user.ID, sess.ID, factorProof, now)
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
