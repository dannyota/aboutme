package secondfactor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/store"
)

func (s *Service) completeTOTPPending(ctx context.Context, pendingToken string, sessionID *uuid.UUID, credential *totpCredential, client auth.SecondFactorClient) (*auth.SessionIssue, error) {
	if s.totpRing == nil {
		return nil, errTOTPRingUnconfigured
	}
	var cred *store.TotpCredential
	before := func(ctx context.Context, qtx *store.Queries, user store.User, _ *store.Session) error {
		if _, err := lockPolicy(ctx, qtx, user.ID); err != nil {
			return err
		}
		locked, err := lockTOTPCredential(ctx, qtx, user.ID)
		if err != nil {
			return err
		}
		if locked == nil {
			return auth.ErrSecondFactorNotFound
		}
		now := s.now()
		if locked.CooldownUntil != nil && locked.CooldownUntil.After(now) {
			return auth.NewTOTPCoolingDownError(totpRetryAfterSeconds(*locked.CooldownUntil, now))
		}
		cred = locked
		return nil
	}
	var issue *auth.SessionIssue
	_, err := s.pending.WithLivePending(ctx, pendingToken, sessionID, before,
		func(ctx context.Context, qtx *store.Queries, locked auth.PendingAuthenticationLocked) error {
			now := s.now()
			secret, openErr := s.totpRing.Open(TOTPRecordKindCredential, locked.User.ID, cred.ID, sealedFromCredentialRow(*cred))
			if openErr != nil {
				s.logTOTPKeyFailure(openErr, TOTPRecordKindCredential, cred.ID)
				return openErr
			}
			step, matched, verifyErr := VerifyTOTPCode(secret, credential.code, now)
			if verifyErr != nil {
				return verifyErr
			}
			if matched {
				advanced, advErr := qtx.AdvanceTOTPCredentialStep(ctx, store.AdvanceTOTPCredentialStepParams{
					Step: totpStepColumn(step), Now: now, UserID: locked.User.ID,
				})
				switch {
				case advErr == nil:
					if resetErr := s.resetTOTPBudgetIfDue(ctx, qtx, locked.User.ID, advanced, now); resetErr != nil {
						return resetErr
					}
					if reencErr := s.reencryptTOTPCredentialIfStale(ctx, qtx, *cred, secret, now); reencErr != nil {
						return reencErr
					}
					var finishErr error
					issue, finishErr = s.finishPending(ctx, qtx, locked, client, now)
					return finishErr
				case errors.Is(advErr, pgx.ErrNoRows):
				default:
					return fmt.Errorf("advance totp step: %w", advErr)
				}
			}
			return s.failTOTPAttempt(ctx, qtx, locked.User, now)
		})
	if err != nil {
		return nil, err
	}
	return issue, nil
}

func (s *Service) resetTOTPBudgetIfDue(ctx context.Context, qtx *store.Queries, userID uuid.UUID, cred store.TotpCredential, now time.Time) error {
	if cred.FailedAttempts == 0 && cred.CooldownUntil == nil && cred.LastFailedAt == nil {
		return nil
	}
	if cred.LastFailedAt != nil && now.Sub(*cred.LastFailedAt) < totpFailureResetWindow {
		return nil
	}
	if _, err := qtx.ResetTOTPCredentialFailureBudget(ctx, store.ResetTOTPCredentialFailureBudgetParams{UserID: userID, Now: now}); err != nil {
		return fmt.Errorf("reset totp failure budget: %w", err)
	}
	return nil
}

func (s *Service) reencryptTOTPCredentialIfStale(ctx context.Context, qtx *store.Queries, cred store.TotpCredential, secret TOTPSecret, now time.Time) error {
	if cred.KeyID == s.totpRing.ActiveKeyID() {
		return nil
	}
	resealed, err := s.totpRing.Seal(TOTPRecordKindCredential, cred.UserID, cred.ID, secret)
	if err != nil {
		return err
	}
	if _, err = qtx.ReencryptTOTPCredential(ctx, store.ReencryptTOTPCredentialParams{
		NewKeyID: resealed.KeyID, Nonce: resealed.Nonce[:], Ciphertext: resealed.Ciphertext,
		Now: now, ID: cred.ID, OldKeyID: cred.KeyID,
	}); err != nil {
		return fmt.Errorf("lazily reencrypt totp credential: %w", err)
	}
	return nil
}

func (s *Service) failTOTPAttempt(ctx context.Context, qtx *store.Queries, user store.User, now time.Time) error {
	updated, err := qtx.RecordTOTPCredentialFailure(ctx, store.RecordTOTPCredentialFailureParams{UserID: user.ID, Now: now})
	if err != nil {
		return fmt.Errorf("record totp failure: %w", err)
	}
	if updated.FailedAttempts%5 == 0 {
		if mailErr := s.enqueueCappedAttemptMail(ctx, qtx, user, now); mailErr != nil {
			return mailErr
		}
	}
	return s.failAttempt(user)
}

func totpRetryAfterSeconds(cooldownUntil, now time.Time) int {
	remaining := cooldownUntil.Sub(now)
	secs := int((remaining + time.Second - 1) / time.Second)
	if secs < 1 {
		secs = 1
	}
	if secs > totpMaxRetryAfterSeconds {
		secs = totpMaxRetryAfterSeconds
	}
	return secs
}
