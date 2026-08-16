package authmail

import (
	"crypto/aes"
	"crypto/cipher"

	"github.com/google/uuid"
)

// aadPrefix is the D3 authenticated-data domain separator.
const aadPrefix = "aboutme.auth-email.v1"

// maxPlaintextBytes caps the serialized payload (D3: closed JSON <= 4,096).
const maxPlaintextBytes = 4096

// maxCiphertextBytes is maxPlaintextBytes plus the AES-256-GCM tag (16 bytes).
const maxCiphertextBytes = 4096 + 16

// aad builds the exact D3 AAD:
//
//	aboutme.auth-email.v1 \x00 <job-id-bytes> \x00 <kind> \x00 <key-id>
//
// <job-id-bytes> are the 16 raw bytes of the UUID, binding the ciphertext to a
// single job so a blob cannot be relabeled, reparented, or copied to another
// job ID, kind, or key.
func aad(jobID uuid.UUID, kind Kind, keyID string) []byte {
	b := make([]byte, 0, len(aadPrefix)+1+16+1+len(kind)+1+len(keyID))
	b = append(b, aadPrefix...)
	b = append(b, 0x00)
	b = append(b, jobID[:]...)
	b = append(b, 0x00)
	b = append(b, kind...)
	b = append(b, 0x00)
	b = append(b, keyID...)
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

// openAEAD decrypts and authenticates ciphertext. Any authentication failure —
// tampered ciphertext/nonce/AAD, truncated tag, or wrong key — collapses to
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
