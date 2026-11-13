package authmail

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"

	"github.com/google/uuid"
)

const aadPrefix = "aboutme.auth-email.v2"

const maxPlaintextBytes = 4096

const maxCiphertextBytes = 4096 + 16

type JobScope struct {
	UserID         *uuid.UUID
	RegistrationID *uuid.UUID
	ResetTokenID   *uuid.UUID
	TokenDigest    []byte
}

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
