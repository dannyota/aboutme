package secondfactor

// Passkey enrollment: the registration ceremony start, its completion under
// the user, session, and policy locks, and persistence of the verified
// credential with the first factor's policy and recovery codes.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/authmail"
	"danny.vn/aboutme/apps/server/internal/store"
)

var errDuplicateCredential = errors.New("secondfactor: credential already registered")

// StartPasskeyRegistration creates a registration ceremony for the current
// session. Before a policy exists it proposes a new random user handle and
// stores it in the ceremony; afterwards it reuses the stored handle.
func (s *Service) StartPasskeyRegistration(ctx context.Context, current store.Session) (auth.SecondFactorOptions, error) {
	if !s.enabled {
		return auth.SecondFactorOptions{}, auth.ErrSecondFactorEnrollmentDisabled
	}
	s.cleanup(ctx)
	var options auth.SecondFactorOptions
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
		active, err := qtx.ListWebAuthnCredentialsForUser(ctx, user.ID)
		if err != nil {
			return fmt.Errorf("list passkeys: %w", err)
		}
		if len(active) >= maxActivePasskeys {
			return auth.ErrSecondFactorLimitReached
		}
		var handle, proposed []byte
		if policy != nil {
			handle = policy.WebauthnUserHandle
		} else {
			if proposed, err = s.random(userHandleBytes); err != nil {
				return err
			}
			handle = proposed
		}
		token, challenge, err := s.createCeremony(ctx, qtx, user, purposeRegistration, &sess.ID, nil, proposed, now)
		if err != nil {
			return err
		}
		publicKey, err := s.rp.registrationOptions(challenge, handle, user.Email, user.Name, active)
		if err != nil {
			return fmt.Errorf("registration options: %w", err)
		}
		options = auth.SecondFactorOptions{CeremonyID: token, PublicKey: publicKey}
		return nil
	})
	if err != nil {
		return auth.SecondFactorOptions{}, err
	}
	return options, nil
}

// CompletePasskeyRegistration verifies and stores a passkey. It claims the
// ceremony under the user, session, and policy locks. First versus later
// enrollment follows policy existence: first creates the policy with the
// ceremony's proposed handle, the credential, and recovery codes; later reuses
// the stored handle. A claimed ceremony stays consumed when the completion is
// rejected, including while enrollment is disabled.
func (s *Service) CompletePasskeyRegistration(ctx context.Context, current store.Session, credential auth.SecondFactorCredential) (auth.SecondFactorRegistration, error) {
	reg, ok := credential.(*passkeyRegistration)
	if !ok {
		return auth.SecondFactorRegistration{}, auth.ErrSecondFactorRequestInvalid
	}
	values, display, err := generateRecoveryCodes(s.entropy)
	if err != nil {
		return auth.SecondFactorRegistration{}, err
	}
	var (
		result  auth.SecondFactorRegistration
		outcome error
	)
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		now := s.now()
		user, sess, policy, lockErr := lockAccount(ctx, qtx, current, now)
		if lockErr != nil {
			return lockErr
		}
		ceremony, claimErr := claimCeremony(ctx, qtx, reg.ceremonyDigest, user, purposeRegistration, &sess.ID, nil, now)
		if claimErr != nil {
			return claimErr
		}
		if !s.enabled {
			outcome = auth.ErrSecondFactorEnrollmentDisabled
			return nil
		}
		if reauthErr := auth.RequireRecentSecondFactorReauth(user, policy, sess, now); reauthErr != nil {
			return reauthErr
		}
		first := policy == nil
		handle := ceremony.ProposedUserHandle
		switch {
		case first != (handle != nil):
			outcome = auth.ErrSecondFactorChallengeInvalid
			return nil
		case !first:
			handle = policy.WebauthnUserHandle
		}
		active, listErr := qtx.ListWebAuthnCredentialsForUser(ctx, user.ID)
		if listErr != nil {
			return fmt.Errorf("list passkeys: %w", listErr)
		}
		if len(active) >= maxActivePasskeys {
			outcome = auth.ErrSecondFactorLimitReached
			return nil
		}
		verified, verifyErr := s.rp.verifyRegistration(reg, ceremony.ChallengeDigest, handle, user.Email, user.Name)
		if verifyErr != nil {
			// Commit the ceremony consumption and report the failure through
			// outcome, so a rejected registration cannot be retried.
			outcome = verifyErr
			return nil //nolint:nilerr // the failure is returned through outcome after commit
		}
		// The writes run in a savepoint so a duplicate credential ID rolls
		// back only them and the ceremony stays consumed.
		writeErr := pgx.BeginFunc(ctx, tx, func(sp pgx.Tx) error {
			var storeErr error
			result, storeErr = s.storePasskey(ctx, s.q.WithTx(sp), user, sess, handle, first, verified, values, display, now)
			return storeErr
		})
		if errors.Is(writeErr, errDuplicateCredential) {
			outcome = auth.ErrSecondFactorVerificationFailed
			return nil
		}
		return writeErr
	})
	if err != nil {
		return auth.SecondFactorRegistration{}, err
	}
	if outcome != nil {
		return auth.SecondFactorRegistration{}, outcome
	}
	return result, nil
}

// storePasskey writes a verified credential and commits the factor change.
// First enrollment sets the replacement session's factor proof to now; later
// enrollment copies the prior proof.
func (s *Service) storePasskey(ctx context.Context, qtx *store.Queries, user store.User, sess store.Session, handle []byte, first bool, verified verifiedPasskey, values []recoveryValue, display []string, now time.Time) (auth.SecondFactorRegistration, error) {
	if first {
		if _, err := qtx.CreateSecondFactorPolicy(ctx, store.CreateSecondFactorPolicyParams{UserID: user.ID, WebauthnUserHandle: handle, EnabledAt: now}); err != nil {
			return auth.SecondFactorRegistration{}, fmt.Errorf("create policy: %w", err)
		}
	}
	transports := verified.transports
	if transports == nil {
		transports = []string{}
	}
	stored, err := qtx.CreateWebAuthnCredential(ctx, store.CreateWebAuthnCredentialParams{
		UserID: user.ID, CredentialID: verified.credentialID, PublicKey: verified.publicKey,
		SignCount: int64(verified.signCount), BackupEligible: verified.backupEligible,
		BackupState: verified.backupState, Transports: transports, CreatedAt: now,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return auth.SecondFactorRegistration{}, errDuplicateCredential
		}
		return auth.SecondFactorRegistration{}, fmt.Errorf("create passkey: %w", err)
	}
	kind, proof := authmail.KindPasskeyAdded, sess.SecondFactorVerifiedAt
	var codes []string
	if first {
		if err = insertRecoveryCodes(ctx, qtx, user.ID, values, now); err != nil {
			return auth.SecondFactorRegistration{}, err
		}
		kind, proof, codes = authmail.KindSecondFactorEnabled, &now, display
	}
	issue, err := s.commitFactorChange(ctx, qtx, user, sess, proof, kind, now)
	if err != nil {
		return auth.SecondFactorRegistration{}, err
	}
	return auth.SecondFactorRegistration{Passkey: passkeyView(stored), RecoveryCodes: codes, Session: issue}, nil
}
