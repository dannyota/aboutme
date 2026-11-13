package secondfactor

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/authmail"
	"danny.vn/aboutme/apps/server/internal/store"
)

const (
	totpEnrollmentLifetime      = 10 * time.Minute
	totpEnrollmentTokenBytes    = 32
	totpEnrollmentTokenEncoded  = 43
	totpHandleCandidates        = 3
	totpFailureResetWindow      = 24 * time.Hour
	totpMaxRetryAfterSeconds    = 86400
	totpUniqueViolationSQLState = "23505"
)

var errTOTPRingUnconfigured = errors.New("secondfactor: totp key ring not configured")

type totpCredential struct{ code string }

func (*totpCredential) SecondFactorMethod() string { return auth.SecondFactorMethodTOTP }

type totpEnrollmentCompletion struct {
	rawToken string
	code     string
}

func (*totpEnrollmentCompletion) SecondFactorMethod() string { return auth.SecondFactorMethodTOTP }

func TOTPCounter(ctx context.Context, qtx *store.Queries, userID uuid.UUID) (int, error) {
	n, err := qtx.CountTOTPCredentialsForUser(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("count totp credentials: %w", err)
	}
	return int(n), nil
}

func (s *Service) TOTPEnrollmentEnabled() bool { return s.totpEnabled }

func (s *Service) TOTPState(ctx context.Context, userID uuid.UUID) (bool, error) {
	n, err := s.q.CountTOTPCredentialsForUser(ctx, userID)
	if err != nil {
		return false, fmt.Errorf("secondfactor: totp state: %w", err)
	}
	return n > 0, nil
}

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

func (s *Service) logTOTPKeyFailure(err error, kind TOTPRecordKind, rowID uuid.UUID) {
	switch {
	case errors.Is(err, ErrTOTPUnknownKey):
		s.totpSignal.Emit(TOTPUnavailableReasonUnknownKeyID)
	case errors.Is(err, ErrTOTPAuthentication):
		s.totpSignal.EmitDecryptFailure(kind, rowID)
	}
}

func totpStepColumn(step uint64) int64 {
	return int64(step) //nolint:gosec // bounded by a Unix-second timestamp divided by the 30-second period
}

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
