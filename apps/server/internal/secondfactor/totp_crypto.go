package secondfactor

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/google/uuid"
)

const (
	totpKeyBytes         = 32
	totpKeyEncodedBytes  = 43
	totpKeyIDPrefix      = "tk1_"
	totpKeyIDDigestBytes = 16
	totpKeyIDDomain      = "aboutme.totp.key-id.v1"
	totpAADDomain        = "aboutme.totp.v1"
	totpFormatVersion    = 1
	totpNonceBytes       = 12
	totpTagBytes         = 16
	totpCiphertextBytes  = totpSecretBytes + totpTagBytes
)

var ErrTOTPKeyRing = errors.New("secondfactor: totp key ring is invalid")

var ErrTOTPUnknownKey = errors.New("secondfactor: totp key id is not in the ring")

var ErrTOTPAuthentication = errors.New("secondfactor: totp authenticated decryption failed")

type TOTPKey [totpKeyBytes]byte

func DecodeTOTPKey(text string) (TOTPKey, error) {
	var key TOTPKey
	if len(text) != totpKeyEncodedBytes {
		return key, ErrTOTPKeyRing
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(text)
	if err != nil || len(decoded) != totpKeyBytes || base64.RawURLEncoding.EncodeToString(decoded) != text {
		return key, ErrTOTPKeyRing
	}
	copy(key[:], decoded)
	return key, nil
}

func DeriveTOTPKeyID(key TOTPKey) string {
	h := sha256.New()
	h.Write([]byte(totpKeyIDDomain))
	h.Write([]byte{0})
	h.Write(key[:])
	sum := h.Sum(nil)
	return totpKeyIDPrefix + base64.RawURLEncoding.EncodeToString(sum[:totpKeyIDDigestBytes])
}

type TOTPRecordKind string

const (
	TOTPRecordKindCredential TOTPRecordKind = "credential"
	TOTPRecordKindEnrollment TOTPRecordKind = "enrollment"
)

type SealedTOTPSecret struct {
	KeyID      string
	Nonce      [totpNonceBytes]byte
	Ciphertext []byte
	Version    int
}

type TOTPKeyRing struct {
	activeID string
	keys     map[string]TOTPKey
	entropy  io.Reader
}

func NewTOTPKeyRing(activeKey, previousKey string, entropy io.Reader) (*TOTPKeyRing, error) {
	if entropy == nil {
		return nil, ErrTOTPKeyRing
	}
	active, err := DecodeTOTPKey(activeKey)
	if err != nil {
		return nil, err
	}
	activeID := DeriveTOTPKeyID(active)
	keys := map[string]TOTPKey{activeID: active}
	if previousKey != "" {
		previous, previousErr := DecodeTOTPKey(previousKey)
		if previousErr != nil {
			return nil, previousErr
		}
		previousID := DeriveTOTPKeyID(previous)
		if previousID == activeID {
			return nil, ErrTOTPKeyRing
		}
		keys[previousID] = previous
	}
	return &TOTPKeyRing{activeID: activeID, keys: keys, entropy: entropy}, nil
}

func (k *TOTPKeyRing) ActiveKeyID() string {
	return k.activeID
}

func (k *TOTPKeyRing) KnownKeyID(id string) bool {
	_, ok := k.keys[id]
	return ok
}

func (k *TOTPKeyRing) Seal(kind TOTPRecordKind, accountID, rowID uuid.UUID, secret TOTPSecret) (SealedTOTPSecret, error) {
	var nonce [totpNonceBytes]byte
	if _, err := io.ReadFull(k.entropy, nonce[:]); err != nil {
		return SealedTOTPSecret{}, fmt.Errorf("secondfactor: totp seal nonce: %w", err)
	}
	active := k.keys[k.activeID]
	ct, err := sealTOTPAEAD(active, nonce[:], secret[:], totpAAD(kind, accountID, rowID, totpFormatVersion, k.activeID))
	if err != nil {
		return SealedTOTPSecret{}, err
	}
	return SealedTOTPSecret{KeyID: k.activeID, Nonce: nonce, Ciphertext: ct, Version: totpFormatVersion}, nil
}

func (k *TOTPKeyRing) Open(kind TOTPRecordKind, accountID, rowID uuid.UUID, sealed SealedTOTPSecret) (TOTPSecret, error) {
	if len(sealed.Ciphertext) != totpCiphertextBytes {
		return TOTPSecret{}, ErrTOTPAuthentication
	}
	key, ok := k.keys[sealed.KeyID]
	if !ok {
		return TOTPSecret{}, ErrTOTPUnknownKey
	}
	pt, err := openTOTPAEAD(key, sealed.Nonce[:], sealed.Ciphertext, totpAAD(kind, accountID, rowID, sealed.Version, sealed.KeyID))
	if err != nil {
		return TOTPSecret{}, err
	}
	var secret TOTPSecret
	copy(secret[:], pt)
	return secret, nil
}

func totpAAD(kind TOTPRecordKind, accountID, rowID uuid.UUID, version int, keyID string) []byte {
	versionText := strconv.Itoa(version)
	b := make([]byte, 0, len(totpAADDomain)+1+len(kind)+1+16+1+16+1+len(versionText)+1+len(keyID))
	b = append(b, totpAADDomain...)
	b = append(b, 0x00)
	b = append(b, kind...)
	b = append(b, 0x00)
	b = append(b, accountID[:]...)
	b = append(b, 0x00)
	b = append(b, rowID[:]...)
	b = append(b, 0x00)
	b = append(b, versionText...)
	b = append(b, 0x00)
	b = append(b, keyID...)
	return b
}

func sealTOTPAEAD(key TOTPKey, nonce, plaintext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, ErrTOTPAuthentication
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrTOTPAuthentication
	}
	return gcm.Seal(nil, nonce, plaintext, aad), nil
}

func openTOTPAEAD(key TOTPKey, nonce, ciphertext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, ErrTOTPAuthentication
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrTOTPAuthentication
	}
	pt, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrTOTPAuthentication
	}
	return pt, nil
}
