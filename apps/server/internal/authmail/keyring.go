package authmail

import (
	"io"

	"github.com/google/uuid"
)

// KeyRing holds exactly one active key and at most one previous 32-byte key
// (D3). Seal always uses the active key; Open decrypts with whichever key ID
// the blob names, so rotation keeps previously sealed jobs readable.
type KeyRing struct {
	activeID string
	keys     map[string][32]byte
	nonce    io.Reader
}

// NewKeyRing validates the ring configuration and copies the key map so later
// caller mutation cannot change it. It rejects a nil nonce source, an empty or
// unknown active ID, a non-printable or over-length key ID, and fewer than one
// or more than two configured keys. Duplicate IDs cannot be expressed by the
// map type; the active ID must be a member of the map.
func NewKeyRing(activeID string, keys map[string][32]byte, nonce io.Reader) (*KeyRing, error) {
	if nonce == nil {
		return nil, ErrKeyRing
	}
	if err := validateKeyID(activeID); err != nil {
		return nil, ErrKeyRing
	}
	if len(keys) == 0 || len(keys) > 2 {
		return nil, ErrKeyRing
	}
	if _, ok := keys[activeID]; !ok {
		return nil, ErrKeyRing
	}
	copied := make(map[string][32]byte, len(keys))
	for id, k := range keys {
		if err := validateKeyID(id); err != nil {
			return nil, ErrKeyRing
		}
		copied[id] = k
	}
	return &KeyRing{activeID: activeID, keys: copied, nonce: nonce}, nil
}

// Seal validates the payload, serializes it to strict JSON, and encrypts it
// under the active key with a fresh 12-byte nonce and the D3 AAD.
func (k *KeyRing) Seal(jobID uuid.UUID, kind Kind, p Payload) (Sealed, error) {
	if err := validatePayload(kind, p); err != nil {
		return Sealed{}, err
	}
	plaintext, err := marshalPayload(p)
	if err != nil {
		return Sealed{}, err
	}
	if len(plaintext) > maxPlaintextBytes {
		return Sealed{}, ErrOversizedPayload
	}
	key, ok := k.keys[k.activeID]
	if !ok {
		return Sealed{}, ErrUnknownKey
	}
	var nonce [12]byte
	if _, err := io.ReadFull(k.nonce, nonce[:]); err != nil {
		return Sealed{}, ErrNonce
	}
	ct, err := sealAEAD(key, nonce[:], plaintext, aad(jobID, kind, k.activeID))
	if err != nil {
		return Sealed{}, err
	}
	return Sealed{KeyID: k.activeID, Nonce: nonce, Ciphertext: ct}, nil
}

// Open validates every dynamic bound of the sealed blob (key ID, ciphertext
// length) before AEAD work, then decrypts and strictly decodes the payload.
// The nonce length is fixed at 12 bytes by the Sealed type. Unknown payload
// version, strict-JSON violations, and invalid email/link are rejected after
// decryption but before any plaintext is returned.
func (k *KeyRing) Open(jobID uuid.UUID, kind Kind, s Sealed) (Payload, error) {
	if err := validateKind(kind); err != nil {
		return Payload{}, err
	}
	if err := validateKeyID(s.KeyID); err != nil {
		return Payload{}, err
	}
	if len(s.Ciphertext) < 1 || len(s.Ciphertext) > maxCiphertextBytes {
		return Payload{}, ErrCiphertext
	}
	key, ok := k.keys[s.KeyID]
	if !ok {
		return Payload{}, ErrUnknownKey
	}
	pt, err := openAEAD(key, s.Nonce[:], s.Ciphertext, aad(jobID, kind, s.KeyID))
	if err != nil {
		return Payload{}, err
	}
	p, err := decodePayloadStrict(pt)
	if err != nil {
		return Payload{}, err
	}
	if err := validatePayload(kind, p); err != nil {
		return Payload{}, err
	}
	return p, nil
}
