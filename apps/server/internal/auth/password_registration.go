package auth

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/accountemail"
	"danny.vn/aboutme/apps/server/internal/authmail"
	"danny.vn/aboutme/apps/server/internal/password"
	"danny.vn/aboutme/apps/server/internal/store"
)

func (s *PasswordService) register(ctx context.Context, name, email, rawPassword, clientIP string) error {
	now := s.clock()

	canonicalEmail, err := accountemail.Canonicalize(email)
	if err != nil {
		return errPasswordEmailInvalid
	}
	name, err = normalizeRegistrationName(name)
	if err != nil {
		return errPasswordNameInvalid
	}

	if d := s.limits.AdmitRegisterOrForgotEmail(now, canonicalEmail); !d.Allowed {
		return &passwordRateLimitedError{retryAfterSeconds: d.RetryAfterSeconds}
	}
	if d := s.limits.AdmitRegisterOrForgotIP(now, clientAddrFromString(clientIP)); !d.Allowed {
		return &passwordRateLimitedError{retryAfterSeconds: d.RetryAfterSeconds}
	}

	check, err := s.policy.CheckNew(ctx, rawPassword)
	if err != nil {
		if errors.Is(err, password.ErrBreachUnavailable) {
			return errPasswordUnavailable
		}
		return err
	}
	encodedHash, err := s.hasher.Hash(ctx, check.Normalized)
	if err != nil {
		return errPasswordUnavailable
	}

	token, err := password.NewToken(s.entropy)
	if err != nil {
		return errPasswordUnavailable
	}
	jobID, err := uuid.NewV7FromReader(s.entropy)
	if err != nil {
		return errPasswordUnavailable
	}
	expiresAt := now.Add(passwordRegistrationTTL)
	payload := newEmailPayload(canonicalEmail, verifyEmailLink(token.Raw))

	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		if lockErr := qtx.LockCanonicalAccountEmail(ctx, canonicalEmail); lockErr != nil {
			return lockErr
		}
		if prior, rerr := qtx.GetPasswordRegistrationByEmailForUpdate(ctx, canonicalEmail); rerr == nil {
			if _, uerr := qtx.GetUserByCanonicalEmail(ctx, canonicalEmail); uerr == nil {
				return nil
			} else if !errors.Is(uerr, pgx.ErrNoRows) {
				return uerr
			}
			if _, derr := qtx.DeletePasswordRegistration(ctx, prior.ID); derr != nil {
				return derr
			}
		} else if !errors.Is(rerr, pgx.ErrNoRows) {
			return rerr
		} else if _, uerr := qtx.GetUserByCanonicalEmail(ctx, canonicalEmail); uerr == nil {
			return nil
		} else if !errors.Is(uerr, pgx.ErrNoRows) {
			return uerr
		}
		reg, cerr := qtx.CreatePasswordRegistration(ctx, store.CreatePasswordRegistrationParams{
			Email:       canonicalEmail,
			Name:        name,
			EncodedHash: []byte(encodedHash),
			TokenDigest: token.Digest[:],
			CreatedAt:   now,
			ExpiresAt:   expiresAt,
		})
		if cerr != nil {
			if isUniqueViolation(cerr) {
				return errPasswordRegistrationRace
			}
			return cerr
		}
		return s.outbox.EnqueueTx(ctx, qtx, authmail.EnqueueRequest{
			JobID:          jobID,
			Kind:           authmail.KindVerify,
			RegistrationID: &reg.ID,
			TokenDigest:    &token.Digest,
			Payload:        payload,
			ExpiresAt:      expiresAt,
		})
	})
	if err != nil {
		if errors.Is(err, errPasswordRegistrationRace) {
			return nil
		}
		return errPasswordUnavailable
	}
	return nil
}

func (s *PasswordService) verify(ctx context.Context, rawToken, clientIP string) error {
	now := s.clock()

	digest, err := password.DigestToken(rawToken)
	if err != nil {
		return errPasswordTokenShape
	}
	if d := s.limits.AdmitVerifyOrResetIP(now, clientAddrFromString(clientIP)); !d.Allowed {
		return &passwordRateLimitedError{retryAfterSeconds: d.RetryAfterSeconds}
	}

	reg, err := s.q.GetPasswordRegistrationByDigest(ctx, digest[:])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errPasswordTokenInvalid
		}
		return errPasswordUnavailable
	}
	if now.After(reg.ExpiresAt) {
		return errPasswordTokenInvalid
	}

	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		if lockErr := qtx.LockCanonicalAccountEmail(ctx, reg.Email); lockErr != nil {
			return lockErr
		}
		live, lerr := qtx.GetPasswordRegistrationForUpdate(ctx, reg.ID)
		if lerr != nil {
			if errors.Is(lerr, pgx.ErrNoRows) {
				return errPasswordTokenInvalid
			}
			return lerr
		}
		if now.After(live.ExpiresAt) {
			return errPasswordTokenInvalid
		}
		if s.verifyRegistrationLockProbe != nil {
			s.verifyRegistrationLockProbe()
		}
		if _, uerr := qtx.GetUserByCanonicalEmail(ctx, live.Email); uerr == nil {
			_, derr := qtx.DeletePasswordRegistration(ctx, live.ID)
			return derr
		} else if !errors.Is(uerr, pgx.ErrNoRows) {
			return uerr
		}
		user, cerr := qtx.CreateUser(ctx, store.CreateUserParams{Email: live.Email, Name: live.Name})
		if cerr != nil {
			if isUniqueViolation(cerr) {
				return errPasswordEmailOwnedRace
			}
			return cerr
		}
		if _, uerr := qtx.UpsertPasswordCredential(ctx, store.UpsertPasswordCredentialParams{
			UserID:      user.ID,
			EncodedHash: live.EncodedHash,
			CreatedAt:   now,
			ChangedAt:   now,
		}); uerr != nil {
			return uerr
		}
		_, derr := qtx.DeletePasswordRegistration(ctx, live.ID)
		return derr
	})
	if err != nil {
		if errors.Is(err, errPasswordEmailOwnedRace) {
			return s.consumeRegistrationForOwnedEmail(ctx, reg.ID, reg.Email)
		}
		if errors.Is(err, errPasswordTokenInvalid) {
			return errPasswordTokenInvalid
		}
		return errPasswordUnavailable
	}
	return nil
}

var errPasswordEmailOwnedRace = errors.New("auth: password email owned race")

func (s *PasswordService) consumeRegistrationForOwnedEmail(ctx context.Context, registrationID uuid.UUID, canonicalEmail string) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		if lockErr := qtx.LockCanonicalAccountEmail(ctx, canonicalEmail); lockErr != nil {
			return lockErr
		}
		live, lerr := qtx.GetPasswordRegistrationForUpdate(ctx, registrationID)
		if lerr != nil {
			if errors.Is(lerr, pgx.ErrNoRows) {
				return nil
			}
			return lerr
		}
		_, derr := qtx.DeletePasswordRegistration(ctx, live.ID)
		return derr
	})
	if err != nil {
		return errPasswordUnavailable
	}
	return nil
}
