package secondfactor

// Passkey verification: the assertion ceremony start, the pending
// completion dispatch by method, assertion completion, and the
// non-increasing counter security event.

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/store"
)

// StartPasskeyAssertion creates an assertion ceremony for a live pending
// authentication. An account with no active passkey gets
// ErrSecondFactorNotFound; that rolls back, so no ceremony is created and the
// pending row and its failure count are unchanged.
func (s *Service) StartPasskeyAssertion(ctx context.Context, pendingToken string, sessionID *uuid.UUID) (auth.SecondFactorOptions, error) {
	s.cleanup(ctx)
	var options auth.SecondFactorOptions
	_, err := s.pending.WithLivePending(ctx, pendingToken, sessionID, lockPolicyBeforePending,
		func(ctx context.Context, qtx *store.Queries, locked auth.PendingAuthenticationLocked) error {
			active, err := qtx.ListWebAuthnCredentialsForUser(ctx, locked.User.ID)
			if err != nil {
				return fmt.Errorf("list passkeys: %w", err)
			}
			if len(active) == 0 {
				return auth.ErrSecondFactorNotFound
			}
			token, challenge, err := s.createCeremony(ctx, qtx, locked.User, purposeAssertion, nil, &locked.Pending.ID, nil, s.now())
			if err != nil {
				return err
			}
			publicKey, err := s.rp.assertionOptions(challenge, active)
			if err != nil {
				return err
			}
			options = auth.SecondFactorOptions{CeremonyID: token, PublicKey: publicKey}
			return nil
		})
	if err != nil {
		return auth.SecondFactorOptions{}, err
	}
	return options, nil
}

// CompletePending completes a live pending authentication with a passkey
// assertion or a recovery code.
func (s *Service) CompletePending(ctx context.Context, pendingToken string, sessionID *uuid.UUID, credential auth.SecondFactorCredential, client auth.SecondFactorClient) (*auth.SessionIssue, error) {
	switch c := credential.(type) {
	case *passkeyAssertion:
		return s.completeAssertion(ctx, pendingToken, sessionID, c, client)
	case *recoveryCredential:
		return s.completeRecovery(ctx, pendingToken, sessionID, c, client)
	case *totpCredential:
		return s.completeTOTPPending(ctx, pendingToken, sessionID, c, client)
	default:
		return nil, auth.ErrSecondFactorRequestInvalid
	}
}

// completeAssertion locks the user, bound session, policy, and the presented
// credential before the pending row, then claims the ceremony. A failed
// verification commits only the ceremony consumption, the counter security
// event, the counted attempt, and the exhaustion mail; it never writes the
// credential counter or a session. A failed event insert rolls every write
// back.
func (s *Service) completeAssertion(ctx context.Context, pendingToken string, sessionID *uuid.UUID, a *passkeyAssertion, client auth.SecondFactorClient) (*auth.SessionIssue, error) {
	var (
		policy *store.SecondFactorPolicy
		active []store.WebauthnCredential
		target *store.WebauthnCredential
		issue  *auth.SessionIssue
	)
	before := func(ctx context.Context, qtx *store.Queries, user store.User, _ *store.Session) error {
		var err error
		if policy, err = lockPolicy(ctx, qtx, user.ID); err != nil {
			return err
		}
		if active, err = qtx.ListWebAuthnCredentialsForUser(ctx, user.ID); err != nil {
			return fmt.Errorf("list passkeys: %w", err)
		}
		for _, candidate := range active {
			if !bytes.Equal(candidate.CredentialID, a.rawID) {
				continue
			}
			locked, lockErr := qtx.GetWebAuthnCredentialForUpdate(ctx, store.GetWebAuthnCredentialForUpdateParams{ID: candidate.ID, UserID: user.ID})
			if lockErr != nil {
				return fmt.Errorf("lock passkey: %w", lockErr)
			}
			target = &locked
		}
		return nil
	}
	_, err := s.pending.WithLivePending(ctx, pendingToken, sessionID, before,
		func(ctx context.Context, qtx *store.Queries, locked auth.PendingAuthenticationLocked) error {
			now := s.now()
			ceremony, err := claimCeremony(ctx, qtx, a.ceremonyDigest, locked.User, purposeAssertion, nil, &locked.Pending.ID, now)
			if err != nil {
				return err
			}
			if policy == nil || target == nil {
				return s.failAttempt(locked.User)
			}
			result, err := s.rp.verifyAssertion(a, ceremony.ChallengeDigest, policy.WebauthnUserHandle, active, *target)
			if err != nil {
				return s.failAttempt(locked.User)
			}
			if result.counterRegressed {
				if err = s.recordCounterEvent(ctx, qtx, *target, result.received, now); err != nil {
					return err
				}
				return s.failAttempt(locked.User)
			}
			if _, err = qtx.UpdateWebAuthnCredentialAfterAssertion(ctx, store.UpdateWebAuthnCredentialAfterAssertionParams{
				SignCount: int64(result.received), BackupEligible: target.BackupEligible,
				BackupState: result.backupState, LastUsedAt: now, ID: target.ID, UserID: locked.User.ID,
			}); err != nil {
				return fmt.Errorf("advance passkey counter: %w", err)
			}
			issue, err = s.finishPending(ctx, qtx, locked, client, now)
			return err
		})
	if err != nil {
		return nil, err
	}
	return issue, nil
}

// recordCounterEvent inserts the bounded non-increasing counter event. Logs
// name only the event ID and fixed kind.
func (s *Service) recordCounterEvent(ctx context.Context, qtx *store.Queries, credential store.WebauthnCredential, received uint32, now time.Time) error {
	if s.counterEventProbe != nil {
		if err := s.counterEventProbe(); err != nil {
			return fmt.Errorf("insert counter event: %w", err)
		}
	}
	event, err := qtx.InsertPasskeyCounterSecurityEvent(ctx, store.InsertPasskeyCounterSecurityEventParams{
		UserID: credential.UserID, PasskeyID: credential.ID, StoredCounter: credential.SignCount,
		ReceivedCounter: int64(received), OccurredAt: now,
	})
	if err != nil {
		return fmt.Errorf("insert counter event: %w", err)
	}
	if s.logger != nil {
		s.logger.Warn("authentication security event", "event_id", event.ID.String(), "kind", event.Kind)
	}
	return nil
}
