package secondfactor

// TOTP pending verification: completing a pending authentication with a
// code, the per-account failure budget and cool-down, and lazy re-encryption
// of a previous-key credential. Verification follows the canonical lock order
// in totp_service.go.

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

// completeTOTPPending locks the policy and credential in beforePending,
// before the pending row, in the canonical order, and reads cooldown_until
// there so a live cool-down short-circuits with no decryption and no row
// change. A matched, unused step resets the failure budget when due, lazily
// re-encrypts a previous-key row, advances the step, and finishes the
// pending authentication in the same transaction
// (docs/design/totp-second-factor-contract.md#per-account-totp-failure-budget;
// AC-AUTH-028; AC-SEC-008).
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
					// The step matched but was not strictly greater than
					// last_used_step: a replay, counted as one failure.
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

// resetTOTPBudgetIfDue clears the failure budget after a valid code only
// when last_failed_at is null or at least 24 hours old, so a fresh attack
// window cannot reset an active escalation
// (docs/design/totp-second-factor-contract.md#per-account-totp-failure-budget;
// AC-AUTH-028).
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

// reencryptTOTPCredentialIfStale reseals a previous-key row under the active
// key with a fresh nonce in the same transaction, reusing the row lock
// already held; it is a no-op when the row is already sealed under the
// active key (docs/design/totp-key-management.md#rotation; AC-SEC-008).
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

// failTOTPAttempt records one credential failure, enqueues the capped
// cool-down-start mail when this failure just opened a new cool-down, and
// reports one pending failure through the shared exhaustion path
// (docs/design/totp-second-factor-contract.md#per-account-totp-failure-budget;
// AC-AUTH-028).
func (s *Service) failTOTPAttempt(ctx context.Context, qtx *store.Queries, user store.User, now time.Time) error {
	updated, err := qtx.RecordTOTPCredentialFailure(ctx, store.RecordTOTPCredentialFailureParams{UserID: user.ID, Now: now})
	if err != nil {
		return fmt.Errorf("record totp failure: %w", err)
	}
	// RecordTOTPCredentialFailure's SQL only assigns a fresh cooldown_until
	// when the new failed_attempts is a multiple of five; every other
	// failure keeps the prior value (its ELSE branch), which can still be
	// non-nil and stale once that earlier cool-down has elapsed. Testing
	// CooldownUntil != nil alone would then wrongly re-claim the mail window
	// on every failure after the first cool-down ends, so this checks the
	// same multiple-of-five guard the query uses instead.
	if updated.FailedAttempts%5 == 0 {
		if mailErr := s.enqueueCappedAttemptMail(ctx, qtx, user, now); mailErr != nil {
			return mailErr
		}
	}
	return s.failAttempt(user)
}

// totpRetryAfterSeconds bounds the ceiling of the remaining seconds until
// cooldownUntil to the contract's 86,400-second cap, never rounding down
// below the true remaining time.
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
