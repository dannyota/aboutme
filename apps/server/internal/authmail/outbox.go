package authmail

import (
	"context"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/store"
)

type EnqueueRequest struct {
	JobID          uuid.UUID
	Kind           Kind
	RegistrationID *uuid.UUID
	ResetTokenID   *uuid.UUID
	UserID         *uuid.UUID
	TokenDigest    *[32]byte
	Payload        Payload
	ExpiresAt      time.Time
}

type Outbox struct {
	ring  *KeyRing
	clock func() time.Time
}

func NewOutbox(ring *KeyRing, clock func() time.Time) (*Outbox, error) {
	if ring == nil || clock == nil {
		return nil, ErrOutbox
	}
	return &Outbox{ring: ring, clock: clock}, nil
}

func (o *Outbox) EnqueueTx(ctx context.Context, qtx *store.Queries, req EnqueueRequest) error {
	if qtx == nil {
		return ErrOutbox
	}
	if err := validateRequest(req); err != nil {
		return err
	}
	if err := validatePayload(req.Kind, req.Payload); err != nil {
		return err
	}
	now := o.clock()
	if req.Kind == KindPrivacyNotice {
		if !req.ExpiresAt.IsZero() && !req.ExpiresAt.Equal(now.Add(24*time.Hour)) {
			return ErrExpiry
		}
		req.ExpiresAt = now.Add(24 * time.Hour)
	}
	if err := validateExpiry(req.Kind, req.Payload, now, req.ExpiresAt); err != nil {
		return err
	}

	var digest []byte
	if req.TokenDigest != nil {
		d := *req.TokenDigest
		digest = d[:]
	}
	scope := JobScope{UserID: req.UserID, RegistrationID: req.RegistrationID,
		ResetTokenID: req.ResetTokenID, TokenDigest: digest}
	sealed, err := o.ring.Seal(req.JobID, req.Kind, scope, req.Payload)
	if err != nil {
		return err
	}

	keyID := sealed.KeyID
	nonce := sealed.Nonce[:]
	next := now

	_, err = qtx.CreateAuthEmailJob(ctx, store.CreateAuthEmailJobParams{
		ID:             req.JobID,
		Kind:           string(req.Kind),
		State:          "pending",
		RegistrationID: req.RegistrationID,
		ResetTokenID:   req.ResetTokenID,
		UserID:         req.UserID,
		TokenDigest:    digest,
		KeyID:          &keyID,
		Nonce:          nonce,
		Ciphertext:     sealed.Ciphertext,
		CreatedAt:      now,
		ExpiresAt:      req.ExpiresAt,
		NextAttemptAt:  &next,
	})
	return err
}

func validateRequest(req EnqueueRequest) error {
	if req.JobID == uuid.Nil {
		return ErrJobID
	}
	if err := validateKind(req.Kind); err != nil {
		return err
	}
	switch req.Kind {
	case KindVerify:
		if req.RegistrationID == nil || *req.RegistrationID == uuid.Nil {
			return ErrScope
		}
		if req.ResetTokenID != nil || req.UserID != nil {
			return ErrScope
		}
		if req.TokenDigest == nil {
			return ErrScope
		}
	case KindReset:
		if req.ResetTokenID == nil || *req.ResetTokenID == uuid.Nil {
			return ErrScope
		}
		if req.RegistrationID != nil || req.UserID != nil {
			return ErrScope
		}
		if req.TokenDigest == nil {
			return ErrScope
		}
	case KindPasswordChanged, KindSecondFactorEnabled, KindPasskeyAdded,
		KindPasskeyRemoved, KindSecondFactorDisabled, KindRecoveryCodesRegenerated,
		KindRecoveryCodeUsed, KindSecondFactorAttemptsExhausted,
		KindTOTPAdded, KindTOTPReplaced, KindTOTPRemoved, KindPrivacyNotice:
		if req.UserID == nil || *req.UserID == uuid.Nil {
			return ErrScope
		}
		if req.RegistrationID != nil || req.ResetTokenID != nil {
			return ErrScope
		}
		if req.TokenDigest != nil {
			return ErrScope
		}
	default:
		return ErrInvalidKind
	}
	return nil
}

func validateExpiry(kind Kind, payload Payload, now, expiresAt time.Time) error {
	if !expiresAt.After(now) {
		return ErrExpiry
	}
	if expiresAt.After(now.Add(24 * time.Hour)) {
		return ErrExpiry
	}
	if kind == KindPrivacyNotice && !expiresAt.Equal(now.Add(24*time.Hour)) {
		return ErrExpiry
	}
	if isSecurityKind(kind) {
		occurredAt, err := time.Parse(time.RFC3339, payload.OccurredAt)
		if err != nil || !expiresAt.Equal(occurredAt.Add(24*time.Hour)) {
			return ErrExpiry
		}
	}
	return nil
}

func isSecurityKind(kind Kind) bool {
	switch kind {
	case KindSecondFactorEnabled, KindPasskeyAdded, KindPasskeyRemoved,
		KindSecondFactorDisabled, KindRecoveryCodesRegenerated,
		KindRecoveryCodeUsed, KindSecondFactorAttemptsExhausted,
		KindTOTPAdded, KindTOTPReplaced, KindTOTPRemoved:
		return true
	default:
		return false
	}
}
