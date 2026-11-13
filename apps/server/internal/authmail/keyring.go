package authmail

import (
	"io"

	"github.com/google/uuid"
)

type KeyRing struct {
	activeID string
	keys     map[string][32]byte
	nonce    io.Reader
}

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

func (k *KeyRing) Seal(jobID uuid.UUID, kind Kind, scope JobScope, p Payload) (Sealed, error) {
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
	if _, nonceErr := io.ReadFull(k.nonce, nonce[:]); nonceErr != nil {
		return Sealed{}, ErrNonce
	}
	ct, err := sealAEAD(key, nonce[:], plaintext, aad(jobID, kind, k.activeID, scope))
	if err != nil {
		return Sealed{}, err
	}
	return Sealed{KeyID: k.activeID, Nonce: nonce, Ciphertext: ct}, nil
}

func (k *KeyRing) Open(jobID uuid.UUID, kind Kind, scope JobScope, s Sealed) (Payload, error) {
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
	pt, err := openAEAD(key, s.Nonce[:], s.Ciphertext, aad(jobID, kind, s.KeyID, scope))
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
