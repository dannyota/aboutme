package authmail

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// EnqueueRequest is the caller's full description of a job to enqueue. Exactly
// one scope FK must be set, matching the kind (D3). TokenDigest is required
// for verify/reset and must be absent for password_changed; authmail only ever
// receives the 32-byte digest, never a raw token.
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

// Outbox seals payloads and inserts pending jobs through the caller's
// transaction-scoped queries.
type Outbox struct {
	ring  *KeyRing
	clock func() time.Time
}

// NewOutbox rejects a nil ring or clock.
func NewOutbox(ring *KeyRing, clock func() time.Time) (*Outbox, error) {
	if ring == nil || clock == nil {
		return nil, ErrOutbox
	}
	return &Outbox{ring: ring, clock: clock}, nil
}

// EnqueueTx validates the request and expiry, seals the payload, and inserts a
// pending job through qtx. It never opens or commits its own transaction: the
// insert participates in the caller's transaction and any error is returned
// before the caller commits.
func (o *Outbox) EnqueueTx(ctx context.Context, qtx *store.Queries, req EnqueueRequest) error {
	if qtx == nil {
		return ErrOutbox
	}
	if err := validateRequest(req); err != nil {
		return err
	}
	now := o.clock()
	if err := validateExpiry(now, req.ExpiresAt); err != nil {
		return err
	}

	sealed, err := o.ring.Seal(req.JobID, req.Kind, req.Payload)
	if err != nil {
		return err
	}

	keyID := sealed.KeyID
	nonce := sealed.Nonce[:]
	next := now
	var digest []byte
	if req.TokenDigest != nil {
		d := *req.TokenDigest
		digest = d[:]
	}

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

// validateRequest enforces the D3 scope matrix and the non-nil job/scope IDs
// before any sealing or insert work.
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
	case KindPasswordChanged:
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

// validateExpiry enforces D3: a job must expire after creation and no later
// than 24 hours after creation.
func validateExpiry(now, expiresAt time.Time) error {
	if !expiresAt.After(now) {
		return ErrExpiry
	}
	if expiresAt.After(now.Add(24 * time.Hour)) {
		return ErrExpiry
	}
	return nil
}
