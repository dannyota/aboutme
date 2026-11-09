package authmail

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"

	"github.com/google/uuid"
)

// aadPrefix is the authenticated-data domain separator.
const aadPrefix = "aboutme.auth-email.v2"

// maxPlaintextBytes caps the serialized closed JSON payload at 4,096 bytes.
const maxPlaintextBytes = 4096

// maxCiphertextBytes is maxPlaintextBytes plus the AES-256-GCM tag (16 bytes).
const maxCiphertextBytes = 4096 + 16

// JobScope binds the job's nullable account and token authority columns.
// Nil values represent SQL NULL and differ from present zero values.
type JobScope struct {
	// UserID identifies the account for user-scoped mail.
	UserID *uuid.UUID
	// RegistrationID identifies the pending registration for verification mail.
	RegistrationID *uuid.UUID
	// ResetTokenID identifies the reset token for reset mail.
	ResetTokenID *uuid.UUID
	// TokenDigest is the stored verify/reset token digest, or nil for user mail.
	TokenDigest []byte
}

// aad encodes the domain, job ID, kind, key ID, user ID, registration ID,
// reset token ID, and token digest in that order. UUIDs use 16 raw bytes;
// variable-width values have an eight-byte big-endian byte length. Nullable
// values start with 0 for absent or 1 followed by the encoded value for present.
func aad(jobID uuid.UUID, kind Kind, keyID string, scope JobScope) []byte {
	b := make([]byte, 0, 192)
	b = append(b, aadPrefix...)
	b = append(b, 0x00)
	b = append(b, jobID[:]...)
	b = binary.BigEndian.AppendUint64(b, uint64(len(kind)))
	b = append(b, kind...)
	b = binary.BigEndian.AppendUint64(b, uint64(len(keyID)))
	b = append(b, keyID...)
	for _, id := range []*uuid.UUID{scope.UserID, scope.RegistrationID, scope.ResetTokenID} {
		if id == nil {
			b = append(b, 0)
		} else {
			b = append(b, 1)
			b = append(b, id[:]...)
		}
	}
	if scope.TokenDigest == nil {
		b = append(b, 0)
	} else {
		b = append(b, 1)
		b = binary.BigEndian.AppendUint64(b, uint64(len(scope.TokenDigest)))
		b = append(b, scope.TokenDigest...)
	}
	return b
}

// sealAEAD encrypts and authenticates plaintext with AES-256-GCM, returning
// the nonce-prepended-and-tagged ciphertext. It never returns a raw AEAD
// error: key construction failures here are unreachable for a [32]byte key.
func sealAEAD(key [32]byte, nonce, plaintext, aadBytes []byte) ([]byte, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, ErrAuthentication
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrAuthentication
	}
	return gcm.Seal(nil, nonce, plaintext, aadBytes), nil
}

// openAEAD decrypts and authenticates ciphertext. Any authentication failure,
// including tampered ciphertext/nonce/AAD, truncated tag, or wrong key, collapses to
// the closed ErrAuthentication sentinel; the raw AEAD error never escapes.
func openAEAD(key [32]byte, nonce, ciphertext, aadBytes []byte) ([]byte, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, ErrAuthentication
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrAuthentication
	}
	pt, err := gcm.Open(nil, nonce, ciphertext, aadBytes)
	if err != nil {
		return nil, ErrAuthentication
	}
	return pt, nil
}
