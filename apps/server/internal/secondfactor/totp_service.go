package secondfactor

// TOTP lifecycle: enrollment, replacement, pending verification, removal,
// recovery composition, and the epoch and key-rotation effects that thread
// through them. It builds on the verified totp.go, totp_crypto.go, and
// totp_provisioning.go primitives and the v0.4.2 service in service.go and
// recovery.go. Every mutation follows the canonical lock order: user,
// current session when present, policy, credential, enrollment, then
// pending authentication when applicable
// (docs/design/totp-second-factor-contract.md#removal-recovery-and-races;
// AC-AUTH-027).

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

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// TOTP lifecycle bounds from docs/design/totp-second-factor-contract.md
// "PostgreSQL shape and bounds" and docs/design/budgets.md (AC-AUTH-026,
// AC-AUTH-027).
const (
	totpEnrollmentLifetime      = 10 * time.Minute
	totpEnrollmentTokenBytes    = 32
	totpEnrollmentTokenEncoded  = 43 // canonical unpadded base64url length of 32 bytes
	totpHandleCandidates        = 3
	totpFailureResetWindow      = 24 * time.Hour
	totpMaxRetryAfterSeconds    = 86400
	totpUniqueViolationSQLState = "23505"
)

// errTOTPRingUnconfigured reports that a TOTP method that needs the key ring
// ran without one configured. It maps to the default 503 unavailable
// response like any other dependency failure.
var errTOTPRingUnconfigured = errors.New("secondfactor: totp key ring not configured")

// errTOTPPolicyHandleExhausted reports that every policy-handle candidate
// collided. It creates no policy, credential, recovery code, epoch change,
// session, grant revocation, or mail ("Enrollment and replacement API").
var errTOTPPolicyHandleExhausted = errors.New("secondfactor: totp policy handle candidates exhausted")

// totpCredential is a pending-verification TOTP code awaiting decryption and
// comparison ("TOTP profile and code verification", AC-AUTH-026).
type totpCredential struct{ code string }

// SecondFactorMethod names the TOTP method.
func (*totpCredential) SecondFactorMethod() string { return auth.SecondFactorMethodTOTP }

// totpEnrollmentCompletion is a decoded PUT enrollment body: the raw
// enrollment token and the six-digit proof code
// ("Enrollment and replacement API", AC-AUTH-026).
type totpEnrollmentCompletion struct {
	rawToken string
	code     string
}

// SecondFactorMethod names the TOTP method.
func (*totpEnrollmentCompletion) SecondFactorMethod() string { return auth.SecondFactorMethodTOTP }

// TOTPCounter counts the active TOTP credential (zero or one), joining the
// shared active-factor count without changing any passkey route or stored
// credential (docs/design/second-factor-authentication.md#totp; AC-AUTH-027).
func TOTPCounter(ctx context.Context, qtx *store.Queries, userID uuid.UUID) (int, error) {
	n, err := qtx.CountTOTPCredentialsForUser(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("count totp credentials: %w", err)
	}
	return int(n), nil
}

// TOTPEnrollmentEnabled reports the TOTP enrollment flag.
func (s *Service) TOTPEnrollmentEnabled() bool { return s.totpEnabled }

// TOTPState reports whether the account has an active TOTP credential, for
// the shared /api/v1/me/second-factor response.
func (s *Service) TOTPState(ctx context.Context, userID uuid.UUID) (bool, error) {
	n, err := s.q.CountTOTPCredentialsForUser(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("secondfactor: totp state: %w", err)
	}
	return n > 0, nil
}

// DecodeTOTPCode strictly decodes a pending-verification body: exactly
// {"code": six ASCII digits}. Any other shape fails before cryptographic or
// database work ("TOTP profile and code verification", AC-AUTH-026).
func (s *Service) DecodeTOTPCode(body []byte) (auth.SecondFactorCredential, error) {
	members, err := strictObject(body, "code")
	if err != nil {
		return nil, err
	}
	code, err := stringMember(members["code"])
	if err != nil || !validTOTPCode(code) {
		return nil, errRequestInvalid
	}
	return &totpCredential{code: code}, nil
}

// DecodeTOTPEnrollmentCompletion strictly decodes a PUT enrollment body:
// exactly {"enrollmentId", "code"}. A malformed code is
// ErrSecondFactorRequestInvalid; a malformed enrollment ID is
// ErrTOTPEnrollmentInvalid, both decided before any database work
// ("Enrollment and replacement API", AC-AUTH-026).
func (s *Service) DecodeTOTPEnrollmentCompletion(body []byte) (auth.SecondFactorCredential, error) {
	members, err := strictObject(body, "enrollmentId", "code")
	if err != nil {
		return nil, err
	}
	code, err := stringMember(members["code"])
	if err != nil || !validTOTPCode(code) {
		return nil, errRequestInvalid
	}
	rawEnrollmentID, err := stringMember(members["enrollmentId"])
	if err != nil {
		return nil, auth.ErrTOTPEnrollmentInvalid
	}
	rawToken, err := decodeTOTPEnrollmentToken(rawEnrollmentID)
	if err != nil {
		return nil, auth.ErrTOTPEnrollmentInvalid
	}
	return &totpEnrollmentCompletion{rawToken: string(rawToken), code: code}, nil
}

// decodeTOTPEnrollmentToken parses the canonical 43-character unpadded
// base64url enrollment ID, rejecting the wrong length, padding, and any
// encoding that does not round-trip to the same text, mirroring
// DecodeTOTPKey in totp_crypto.go.
func decodeTOTPEnrollmentToken(raw string) ([]byte, error) {
	if len(raw) != totpEnrollmentTokenEncoded {
		return nil, auth.ErrTOTPEnrollmentInvalid
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil || len(decoded) != totpEnrollmentTokenBytes || base64.RawURLEncoding.EncodeToString(decoded) != raw {
		return nil, auth.ErrTOTPEnrollmentInvalid
	}
	return decoded, nil
}

// lockTOTPCredential locks and returns the account's TOTP credential, or nil
// when none exists, at the credential step of the canonical lock order.
func lockTOTPCredential(ctx context.Context, qtx *store.Queries, userID uuid.UUID) (*store.TotpCredential, error) {
	cred, err := qtx.GetTOTPCredentialForUpdate(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock totp credential: %w", err)
	}
	return &cred, nil
}

func sealedFromCredentialRow(row store.TotpCredential) SealedTOTPSecret {
	sealed := SealedTOTPSecret{KeyID: row.KeyID, Ciphertext: row.Ciphertext, Version: int(row.FormatVersion)}
	copy(sealed.Nonce[:], row.Nonce)
	return sealed
}

func sealedFromEnrollmentRow(row store.TotpEnrollment) SealedTOTPSecret {
	sealed := SealedTOTPSecret{KeyID: row.KeyID, Ciphertext: row.Ciphertext, Version: int(row.FormatVersion)}
	copy(sealed.Nonce[:], row.Nonce)
	return sealed
}

// logTOTPKeyFailure emits the shared totp_unavailable signal for a per-row
// key failure during verification or enrollment completion. The signal
// itself bounds every reason to at most once a minute per process, shared
// with the periodic key-health checker and the rotation runner
// (docs/design/totp-key-management.md#key-failures; AC-SEC-008).
func (s *Service) logTOTPKeyFailure(err error, kind TOTPRecordKind, rowID uuid.UUID) {
	switch {
	case errors.Is(err, ErrTOTPUnknownKey):
		s.totpSignal.Emit(TOTPUnavailableReasonUnknownKeyID)
	case errors.Is(err, ErrTOTPAuthentication):
		s.totpSignal.EmitDecryptFailure(kind, rowID)
	}
}

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

// totpStepColumn narrows a verified step to the bigint last_used_step
// column. VerifyTOTPCode derives step from a Unix-second timestamp divided
// by the 30-second period, thousands of orders of magnitude below the
// int64 range, so this conversion never overflows in practice.
func totpStepColumn(step uint64) int64 {
	return int64(step) //nolint:gosec // bounded by a Unix-second timestamp divided by the 30-second period
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

// RemoveTOTP deletes the active TOTP credential and any live enrollment.
// Final versus non-final comes only from ActiveFactorCount after the delete,
// the same shared count passkey removal uses
// ("Removal, recovery, and races", AC-AUTH-027).
func (s *Service) RemoveTOTP(ctx context.Context, current store.Session) (auth.SessionIssue, error) {
	var issue auth.SessionIssue
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		now := s.now()
		user, sess, policy, lockErr := lockAccount(ctx, qtx, current, now)
		if lockErr != nil {
			return lockErr
		}
		if reauthErr := auth.RequireRecentSecondFactorReauth(user, policy, sess, now); reauthErr != nil {
			return reauthErr
		}
		if policy == nil {
			return auth.ErrSecondFactorNotFound
		}
		cred, credErr := lockTOTPCredential(ctx, qtx, user.ID)
		if credErr != nil {
			return credErr
		}
		if cred == nil {
			return auth.ErrSecondFactorNotFound
		}
		if _, delErr := qtx.DeleteTOTPCredentialForUser(ctx, user.ID); delErr != nil {
			return fmt.Errorf("delete totp credential: %w", delErr)
		}
		if _, delErr := qtx.DeleteTOTPEnrollmentForUser(ctx, user.ID); delErr != nil {
			return fmt.Errorf("delete totp enrollment: %w", delErr)
		}
		remaining, countErr := s.ActiveFactorCount(ctx, qtx, user.ID)
		if countErr != nil {
			return countErr
		}
		kind, proof := authmail.KindTOTPRemoved, sess.SecondFactorVerifiedAt
		if remaining == 0 {
			if _, delErr := qtx.DeleteSecondFactorRecoveryCodesForUser(ctx, user.ID); delErr != nil {
				return fmt.Errorf("delete recovery codes: %w", delErr)
			}
			if _, delErr := qtx.DeleteSecondFactorPolicyForUser(ctx, user.ID); delErr != nil {
				return fmt.Errorf("delete policy: %w", delErr)
			}
			kind, proof = authmail.KindSecondFactorDisabled, nil
		}
		var changeErr error
		issue, changeErr = s.commitFactorChange(ctx, qtx, user, sess, proof, kind, now)
		return changeErr
	})
	if err != nil {
		return auth.SessionIssue{}, err
	}
	return issue, nil
}
