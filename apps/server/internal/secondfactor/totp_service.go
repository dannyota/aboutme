package secondfactor

// TOTP lifecycle shared pieces: strict wire decoding, account state, removal,
// and the row helpers that enrollment (totp_enrollment.go) and pending
// verification (totp_verification.go) build on. It builds on the verified
// totp.go, totp_crypto.go, and totp_provisioning.go primitives and the v0.4.2
// service in service.go and recovery.go. Every mutation follows the canonical
// lock order: user, current session when present, policy, credential,
// enrollment, then pending authentication when applicable
// (docs/design/totp-second-factor-contract.md#removal-recovery-and-races;
// AC-AUTH-027).

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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

// totpStepColumn narrows a verified step to the bigint last_used_step
// column. VerifyTOTPCode derives step from a Unix-second timestamp divided
// by the 30-second period, thousands of orders of magnitude below the
// int64 range, so this conversion never overflows in practice.
func totpStepColumn(step uint64) int64 {
	return int64(step) //nolint:gosec // bounded by a Unix-second timestamp divided by the 30-second period
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
