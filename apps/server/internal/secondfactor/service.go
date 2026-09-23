package secondfactor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/oauthsrv"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// Service limits and lifetimes from docs/design/budgets.md.
const (
	maxActivePasskeys            = 5
	ceremonyLifetime             = 5 * time.Minute
	cleanupLimit           int32 = 200
	securityMailLifetime         = 24 * time.Hour
	securityPayloadVersion       = 2
	// sessionIdleTimeout matches the session idle timeout in
	// docs/design/security.md; proof updates never revive an idle session.
	sessionIdleTimeout = 30 * 24 * time.Hour
)

// Ceremony purposes stored in webauthn_ceremonies.purpose.
const (
	purposeRegistration = "registration"
	purposeAssertion    = "assertion"
)

var errDuplicateCredential = errors.New("secondfactor: credential already registered")

// FactorCounter counts one factor type's active credentials. It must be a
// plain read with no row lock; ActiveFactorCount supplies the user lock.
type FactorCounter func(ctx context.Context, qtx *store.Queries, userID uuid.UUID) (int, error)

// PasskeyCounter counts active passkeys.
func PasskeyCounter(ctx context.Context, qtx *store.Queries, userID uuid.UUID) (int, error) {
	credentials, err := qtx.ListWebAuthnCredentialsForUser(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("list passkeys: %w", err)
	}
	return len(credentials), nil
}

// Options is the service dependency set. Counters lists one counter per
// factor type; nil registers PasskeyCounter and TOTPCounter. Logger may be
// nil. TOTPKeyRing, TOTPIssuer, and TOTPSignal are required only when
// TOTPEnrollmentEnabled is true; a nil TOTPSignal defaults to a silent one
// (docs/design/totp-key-management.md).
type Options struct {
	Pool                  *store.Pool
	Pending               *auth.PendingAuthenticationManager
	Sessions              *auth.SessionManager
	Outbox                *authmail.Outbox
	RelyingParty          *RelyingParty
	EnrollmentEnabled     bool
	TOTPKeyRing           *TOTPKeyRing
	TOTPEnrollmentEnabled bool
	TOTPIssuer            string
	TOTPSignal            *TOTPUnavailableSignal
	Counters              []FactorCounter
	Clock                 func() time.Time
	Entropy               io.Reader
	Logger                *slog.Logger
}

// Service implements auth.SecondFactorService for passkeys and recovery
// codes. Every mutation follows the lock order in
// docs/design/second-factor-authentication.md.
type Service struct {
	pool        *store.Pool
	q           *store.Queries
	pending     *auth.PendingAuthenticationManager
	sessions    *auth.SessionManager
	outbox      *authmail.Outbox
	rp          *RelyingParty
	enabled     bool
	totpRing    *TOTPKeyRing
	totpEnabled bool
	totpIssuer  string
	totpSignal  *TOTPUnavailableSignal
	counters    []FactorCounter
	now         func() time.Time
	entropy     io.Reader
	logger      *slog.Logger

	// counterEventProbe is nil in production. Rollback tests set it to fail
	// the counter security-event write.
	counterEventProbe func() error
}

var _ auth.SecondFactorService = (*Service)(nil)
var _ auth.TOTPSecondFactorService = (*Service)(nil)

// New validates every dependency and returns the service. Enabling TOTP
// enrollment without a key ring or a valid issuer fails construction closed,
// the same way a malformed key ring fails startup
// (docs/design/totp-key-management.md#key-ring).
func New(opts Options) (*Service, error) {
	if opts.Pool == nil || opts.Pending == nil || opts.Sessions == nil || opts.Outbox == nil ||
		opts.RelyingParty == nil || opts.Clock == nil || opts.Entropy == nil {
		return nil, errors.New("secondfactor: missing dependency")
	}
	if opts.TOTPEnrollmentEnabled {
		if opts.TOTPKeyRing == nil {
			return nil, errors.New("secondfactor: totp enrollment enabled without a key ring")
		}
		if err := ValidateTOTPIssuer(opts.TOTPIssuer); err != nil {
			return nil, fmt.Errorf("secondfactor: totp issuer: %w", err)
		}
	}
	counters := opts.Counters
	if counters == nil {
		counters = []FactorCounter{PasskeyCounter, TOTPCounter}
	}
	signal := opts.TOTPSignal
	if signal == nil {
		signal = NewTOTPUnavailableSignal(opts.Clock, nil)
	}
	return &Service{
		pool: opts.Pool, q: store.New(opts.Pool), pending: opts.Pending, sessions: opts.Sessions,
		outbox: opts.Outbox, rp: opts.RelyingParty, enabled: opts.EnrollmentEnabled, counters: counters,
		totpRing: opts.TOTPKeyRing, totpEnabled: opts.TOTPEnrollmentEnabled, totpIssuer: opts.TOTPIssuer, totpSignal: signal,
		now: opts.Clock, entropy: opts.Entropy, logger: opts.Logger,
	}, nil
}

// PasskeyEnrollmentEnabled reports the passkey enrollment flag.
func (s *Service) PasskeyEnrollmentEnabled() bool {
	return s.enabled
}

// DecodePasskeyRegistration strictly decodes a registration completion.
func (s *Service) DecodePasskeyRegistration(body []byte) (auth.SecondFactorCredential, error) {
	reg, err := decodeRegistration(body)
	if err != nil {
		return nil, err
	}
	return reg, nil
}

// DecodePasskeyAssertion strictly decodes an assertion completion.
func (s *Service) DecodePasskeyAssertion(body []byte) (auth.SecondFactorCredential, error) {
	assertion, err := decodeAssertion(body)
	if err != nil {
		return nil, err
	}
	return assertion, nil
}

// ActiveFactorCount takes the user lock and sums every registered factor
// counter. It is the only authority for final versus non-final removal.
func (s *Service) ActiveFactorCount(ctx context.Context, qtx *store.Queries, userID uuid.UUID) (int, error) {
	if _, err := qtx.GetUserForUpdate(ctx, userID); err != nil {
		return 0, fmt.Errorf("lock user: %w", err)
	}
	return s.countActiveFactors(ctx, qtx, userID)
}

// countActiveFactors sums every registered counter. Counters are plain reads,
// so this also runs inside a read-only snapshot.
func (s *Service) countActiveFactors(ctx context.Context, qtx *store.Queries, userID uuid.UUID) (int, error) {
	total := 0
	for _, count := range s.counters {
		n, err := count(ctx, qtx, userID)
		if err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

// PendingMethods lists the pending methods in fixed order: passkey when an
// active passkey exists, then recovery while a code remains.
func (s *Service) PendingMethods(ctx context.Context, userID uuid.UUID) ([]string, error) {
	credentials, err := s.q.ListWebAuthnCredentialsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("secondfactor: list passkeys: %w", err)
	}
	totpCount, err := s.q.CountTOTPCredentialsForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("secondfactor: count totp credentials: %w", err)
	}
	codes, err := s.q.CountSecondFactorRecoveryCodes(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("secondfactor: count recovery codes: %w", err)
	}
	// Fixed order passkey, totp, then recovery
	// ("Release surface"; AC-AUTH-028). A cool-down or key failure still
	// lists totp: PendingMethods only reports existence, not availability.
	methods := make([]string, 0, 3)
	if len(credentials) > 0 {
		methods = append(methods, auth.SecondFactorMethodPasskey)
	}
	if totpCount > 0 {
		methods = append(methods, auth.SecondFactorMethodTOTP)
	}
	if codes > 0 {
		methods = append(methods, auth.SecondFactorMethodRecovery)
	}
	return methods, nil
}

// State reads enforcement, passkeys in (created_at, id) order, and the
// remaining recovery-code count from one read-only REPEATABLE READ snapshot
// that takes no row lock. Enforcement is derived from the active-factor
// counters and the code count in that snapshot: the policy row exists exactly
// while an active factor exists, and codes exist only under a policy, so the
// view is never torn by a concurrent enrollment or removal.
func (s *Service) State(ctx context.Context, userID uuid.UUID) (auth.SecondFactorState, error) {
	var state auth.SecondFactorState
	options := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	err := pgx.BeginTxFunc(ctx, s.pool, options, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		factors, err := s.countActiveFactors(ctx, qtx, userID)
		if err != nil {
			return err
		}
		credentials, err := qtx.ListWebAuthnCredentialsForUser(ctx, userID)
		if err != nil {
			return fmt.Errorf("list passkeys: %w", err)
		}
		codes, err := qtx.CountSecondFactorRecoveryCodes(ctx, userID)
		if err != nil {
			return fmt.Errorf("count recovery codes: %w", err)
		}
		state = auth.SecondFactorState{Enabled: factors > 0 || codes > 0, Passkeys: passkeyViews(credentials), RecoveryCodesRemaining: int(codes)}
		return nil
	})
	if err != nil {
		return auth.SecondFactorState{}, fmt.Errorf("secondfactor: state: %w", err)
	}
	return state, nil
}

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
