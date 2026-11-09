package secondfactor

// TOTP enrollment and replacement: starting an enrollment, proving its
// secret to install or replace the active credential, and creating the
// account's first factor policy handle. Both mutations follow the canonical
// lock order in totp_service.go.

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/authmail"
	"danny.vn/aboutme/apps/server/internal/store"
)

// errTOTPPolicyHandleExhausted reports that every policy-handle candidate
// collided. It creates no policy, credential, recovery code, epoch change,
// session, grant revocation, or mail ("Enrollment and replacement API").
var errTOTPPolicyHandleExhausted = errors.New("secondfactor: totp policy handle candidates exhausted")

// cleanupTOTPEnrollments makes the bounded best-effort expiry call before an
// enrollment is created. A failure writes one fixed warning and never fails
// the request.
func (s *Service) cleanupTOTPEnrollments(ctx context.Context) {
	if _, err := s.q.CleanupExpiredTOTPEnrollments(ctx, cleanupLimit); err != nil && s.logger != nil {
		s.logger.Warn("totp enrollment cleanup failed")
	}
}

// StartTOTPEnrollment begins TOTP enrollment or replacement. Under the user
// lock it supersedes any existing enrollment, seals a fresh secret under the
// active key, and stores it with a ten-minute expiry. It changes no active
// credential, recovery code, enforcement, epoch, session, grant, or mail, so
// abandoning replacement cannot lock out the user
// ("Enrollment and replacement API", AC-AUTH-026, AC-AUTH-027).
func (s *Service) StartTOTPEnrollment(ctx context.Context, current store.Session) (auth.TOTPEnrollmentStart, error) {
	if !s.totpEnabled {
		return auth.TOTPEnrollmentStart{}, auth.ErrSecondFactorEnrollmentDisabled
	}
	if s.totpRing == nil {
		return auth.TOTPEnrollmentStart{}, errTOTPRingUnconfigured
	}
	s.cleanupTOTPEnrollments(ctx)
	secret, err := GenerateTOTPSecret(s.entropy)
	if err != nil {
		return auth.TOTPEnrollmentStart{}, err
	}
	id, err := uuid.NewV7FromReader(s.entropy)
	if err != nil {
		return auth.TOTPEnrollmentStart{}, err
	}
	token, err := s.random(totpEnrollmentTokenBytes)
	if err != nil {
		return auth.TOTPEnrollmentStart{}, err
	}
	digest := sha256.Sum256(token)
	var result auth.TOTPEnrollmentStart
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		now := s.now()
		user, sess, policy, lockErr := lockAccount(ctx, qtx, current, now)
		if lockErr != nil {
			return lockErr
		}
		if reauthErr := auth.RequireRecentSecondFactorReauth(user, policy, sess, now); reauthErr != nil {
			return reauthErr
		}
		if _, delErr := qtx.DeleteTOTPEnrollmentForUser(ctx, user.ID); delErr != nil {
			return fmt.Errorf("supersede totp enrollment: %w", delErr)
		}
		sealed, sealErr := s.totpRing.Seal(TOTPRecordKindEnrollment, user.ID, id, secret)
		if sealErr != nil {
			return sealErr
		}
		uri, uriErr := BuildTOTPProvisioningURI(s.totpIssuer, user.Email, secret)
		if uriErr != nil {
			return uriErr
		}
		expiresAt := now.Add(totpEnrollmentLifetime)
		if _, createErr := qtx.CreateTOTPEnrollment(ctx, store.CreateTOTPEnrollmentParams{
			ID: id, TokenDigest: digest[:], UserID: user.ID, SessionID: sess.ID, AuthEpoch: user.AuthEpoch,
			Issuer: s.totpIssuer, KeyID: sealed.KeyID, Nonce: sealed.Nonce[:], Ciphertext: sealed.Ciphertext,
			CreatedAt: now, ExpiresAt: expiresAt,
		}); createErr != nil {
			return fmt.Errorf("create totp enrollment: %w", createErr)
		}
		result = auth.TOTPEnrollmentStart{
			EnrollmentID: base64.RawURLEncoding.EncodeToString(token), Secret: secret.GroupedDisplay(),
			ProvisioningURI: uri, ExpiresAt: expiresAt,
		}
		return nil
	})
	if err != nil {
		return auth.TOTPEnrollmentStart{}, err
	}
	return result, nil
}

// CompleteTOTPEnrollment proves the enrollment's secret and installs or
// replaces the active credential. It locks user, session, policy, credential,
// then enrollment, in that order; validates the enrollment binding before
// rechecking the enrollment flag, so a disabled completion still consumes a
// matching enrollment; and generates a fresh policy handle with up to three
// candidates only for the account's first factor
// ("Enrollment and replacement API", AC-AUTH-026, AC-AUTH-027).
func (s *Service) CompleteTOTPEnrollment(ctx context.Context, current store.Session, credential auth.SecondFactorCredential) (auth.TOTPEnrollmentComplete, error) {
	completion, ok := credential.(*totpEnrollmentCompletion)
	if !ok {
		return auth.TOTPEnrollmentComplete{}, auth.ErrSecondFactorRequestInvalid
	}
	if s.totpRing == nil {
		return auth.TOTPEnrollmentComplete{}, errTOTPRingUnconfigured
	}
	digest := sha256.Sum256([]byte(completion.rawToken))
	var (
		result  auth.TOTPEnrollmentComplete
		outcome error
	)
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		now := s.now()
		user, sess, policy, lockErr := lockAccount(ctx, qtx, current, now)
		if lockErr != nil {
			return lockErr
		}
		existingCred, credErr := lockTOTPCredential(ctx, qtx, user.ID)
		if credErr != nil {
			return credErr
		}
		enr, enrErr := qtx.GetTOTPEnrollmentForUpdate(ctx, digest[:])
		if errors.Is(enrErr, pgx.ErrNoRows) {
			outcome = auth.ErrTOTPEnrollmentInvalid
			return nil
		}
		if enrErr != nil {
			return fmt.Errorf("lock totp enrollment: %w", enrErr)
		}
		if enr.UserID != user.ID || enr.SessionID != sess.ID || enr.AuthEpoch != user.AuthEpoch || !enr.ExpiresAt.After(now) {
			outcome = auth.ErrTOTPEnrollmentInvalid
			return nil
		}
		if !s.totpEnabled {
			if _, delErr := qtx.DeleteTOTPEnrollmentByID(ctx, enr.ID); delErr != nil {
				return fmt.Errorf("discard totp enrollment: %w", delErr)
			}
			outcome = auth.ErrSecondFactorEnrollmentDisabled
			return nil
		}
		if reauthErr := auth.RequireRecentSecondFactorReauth(user, policy, sess, now); reauthErr != nil {
			return reauthErr
		}
		secret, openErr := s.totpRing.Open(TOTPRecordKindEnrollment, user.ID, enr.ID, sealedFromEnrollmentRow(enr))
		if openErr != nil {
			s.logTOTPKeyFailure(openErr, TOTPRecordKindEnrollment, enr.ID)
			return openErr
		}
		step, matched, verifyErr := VerifyTOTPCode(secret, completion.code, now)
		if verifyErr != nil {
			return verifyErr
		}
		if !matched {
			outcome = auth.ErrSecondFactorVerificationFailed
			return nil
		}
		first := policy == nil
		if first {
			// The account's shared policy identity only needs to exist; its
			// value is not read again in this transaction
			// ("First TOTP creates the shared policy identity").
			if _, handleErr := s.createTOTPPolicyHandle(ctx, tx, user.ID, now); handleErr != nil {
				// Reported through outcome after commit, the same pattern
				// CompletePasskeyRegistration uses, so the ceremony-equivalent
				// locks stay released without retrying the whole request.
				outcome = handleErr
				return nil //nolint:nilerr // the failure is returned through outcome after commit
			}
		}
		if _, delErr := qtx.DeleteTOTPEnrollmentByID(ctx, enr.ID); delErr != nil {
			return fmt.Errorf("delete totp enrollment: %w", delErr)
		}
		var credentialID uuid.UUID
		if existingCred != nil {
			credentialID = existingCred.ID
		} else {
			newID, idErr := uuid.NewV7FromReader(s.entropy)
			if idErr != nil {
				return idErr
			}
			credentialID = newID
		}
		sealed2, sealErr := s.totpRing.Seal(TOTPRecordKindCredential, user.ID, credentialID, secret)
		if sealErr != nil {
			return sealErr
		}
		if existingCred == nil {
			if _, instErr := qtx.InstallTOTPCredential(ctx, store.InstallTOTPCredentialParams{
				ID: credentialID, UserID: user.ID, KeyID: sealed2.KeyID, Nonce: sealed2.Nonce[:],
				Ciphertext: sealed2.Ciphertext, LastUsedStep: totpStepColumn(step), CreatedAt: now,
			}); instErr != nil {
				return fmt.Errorf("install totp credential: %w", instErr)
			}
		} else if _, replErr := qtx.ReplaceTOTPCredential(ctx, store.ReplaceTOTPCredentialParams{
			UserID: user.ID, KeyID: sealed2.KeyID, Nonce: sealed2.Nonce[:], Ciphertext: sealed2.Ciphertext,
			LastUsedStep: totpStepColumn(step), UpdatedAt: now,
		}); replErr != nil {
			return fmt.Errorf("replace totp credential: %w", replErr)
		}
		proof, kind, codes := sess.SecondFactorVerifiedAt, authmail.KindTOTPReplaced, []string(nil)
		switch {
		case first:
			values, display, genErr := generateRecoveryCodes(s.entropy)
			if genErr != nil {
				return genErr
			}
			if insertErr := insertRecoveryCodes(ctx, qtx, user.ID, values, now); insertErr != nil {
				return insertErr
			}
			kind, proof, codes = authmail.KindSecondFactorEnabled, &now, display
		case existingCred == nil:
			kind = authmail.KindTOTPAdded
		}
		issue, commitErr := s.commitFactorChange(ctx, qtx, user, sess, proof, kind, now)
		if commitErr != nil {
			return commitErr
		}
		result = auth.TOTPEnrollmentComplete{RecoveryCodes: codes, Session: issue}
		return nil
	})
	if err != nil {
		return auth.TOTPEnrollmentComplete{}, err
	}
	if outcome != nil {
		return auth.TOTPEnrollmentComplete{}, outcome
	}
	return result, nil
}

// createTOTPPolicyHandle generates a fresh random WebAuthn user handle and
// creates the account's first factor policy, retrying a unique collision
// inside its own savepoint for up to three candidates total. Three
// collisions exhaust the candidates and roll back to the caller, so nothing
// is created ("Enrollment and replacement API": "When no policy exists...",
// AC-AUTH-027).
func (s *Service) createTOTPPolicyHandle(ctx context.Context, tx pgx.Tx, userID uuid.UUID, now time.Time) (*store.SecondFactorPolicy, error) {
	for attempt := 0; attempt < totpHandleCandidates; attempt++ {
		handle, err := s.random(userHandleBytes)
		if err != nil {
			return nil, err
		}
		var policy store.SecondFactorPolicy
		writeErr := pgx.BeginFunc(ctx, tx, func(sp pgx.Tx) error {
			var createErr error
			policy, createErr = s.q.WithTx(sp).CreateSecondFactorPolicy(ctx, store.CreateSecondFactorPolicyParams{
				UserID: userID, WebauthnUserHandle: handle, EnabledAt: now,
			})
			return createErr
		})
		if writeErr == nil {
			return &policy, nil
		}
		var pgErr *pgconn.PgError
		if !errors.As(writeErr, &pgErr) || pgErr.Code != totpUniqueViolationSQLState {
			return nil, fmt.Errorf("create totp policy: %w", writeErr)
		}
	}
	return nil, errTOTPPolicyHandleExhausted
}
