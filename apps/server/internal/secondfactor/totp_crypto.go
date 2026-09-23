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

// TOTP key-ring and sealing constants from
// docs/design/totp-key-management.md "Key ring" and "Sealing" (AC-SEC-008).
const (
	totpKeyBytes         = 32
	totpKeyEncodedBytes  = 43 // canonical unpadded base64url length of 32 bytes
	totpKeyIDPrefix      = "tk1_"
	totpKeyIDDigestBytes = 16
	totpKeyIDDomain      = "aboutme.totp.key-id.v1"
	totpAADDomain        = "aboutme.totp.v1"
	totpFormatVersion    = 1
	totpNonceBytes       = 12
	totpTagBytes         = 16
	totpCiphertextBytes  = totpSecretBytes + totpTagBytes // 36
)

// ErrTOTPKeyRing is returned when the configured active or previous key is
// missing, malformed, or derives the same key ID as the other key.
var ErrTOTPKeyRing = errors.New("secondfactor: totp key ring is invalid")

// ErrTOTPUnknownKey is returned when a sealed value names a key ID the ring
// does not hold. It exposes no key, record, or ciphertext value.
var ErrTOTPUnknownKey = errors.New("secondfactor: totp key id is not in the ring")

// ErrTOTPAuthentication is returned for any sealed-value shape failure or
// authenticated-decryption failure: the wrong ciphertext length, a tampered
// ciphertext, nonce, or associated data, or a mismatched key. It exposes no
// key, record, or ciphertext value.
var ErrTOTPAuthentication = errors.New("secondfactor: totp authenticated decryption failed")

// TOTPKey is one 32-byte AES-256-GCM sealing key.
type TOTPKey [totpKeyBytes]byte

// DecodeTOTPKey parses the canonical 43-character unpadded base64url key
// encoding from "Key ring". It rejects the wrong length, padding, and any
// encoding that does not round-trip to the same text.
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

// DeriveTOTPKeyID derives the 26-byte key ID for key: ASCII "tk1_" followed
// by the canonical unpadded base64url of the first 16 bytes of SHA-256 over
// ASCII "aboutme.totp.key-id.v1", one zero byte, and the 32 key bytes ("Key
// ring"). The ID is derived, never configured, and binds each stored ID to
// exactly one key value.
func DeriveTOTPKeyID(key TOTPKey) string {
	h := sha256.New()
	h.Write([]byte(totpKeyIDDomain))
	h.Write([]byte{0})
	h.Write(key[:])
	sum := h.Sum(nil)
	return totpKeyIDPrefix + base64.RawURLEncoding.EncodeToString(sum[:totpKeyIDDigestBytes])
}

// TOTPRecordKind names the stored row a sealed secret is bound to: a TOTP
// credential or an enrollment ("Sealing").
type TOTPRecordKind string

// The closed set of TOTP record kinds.
const (
	TOTPRecordKindCredential TOTPRecordKind = "credential"
	TOTPRecordKindEnrollment TOTPRecordKind = "enrollment"
)

// SealedTOTPSecret is one AES-256-GCM-protected TOTP secret ready for
// storage: its key ID, nonce, ciphertext, and format version.
type SealedTOTPSecret struct {
	KeyID      string
	Nonce      [totpNonceBytes]byte
	Ciphertext []byte
	Version    int
}

// TOTPKeyRing holds exactly one active key and at most one previous 32-byte
// key ("Key ring"). Seal always uses the active key; Open decrypts with
// whichever key ID the sealed value names, so a row sealed under the previous
// key stays readable until it is re-encrypted.
type TOTPKeyRing struct {
	activeID string
	keys     map[string]TOTPKey
	entropy  io.Reader
}

// NewTOTPKeyRing decodes and validates the active key and the optional
// previous key (empty for none), derives their key IDs, and returns the
// ring. Construction fails when the active key is missing or malformed, the
// previous key is present but malformed, or both keys derive the same ID
// ("Key ring").
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

// ActiveKeyID returns the ring's active key ID, the ID every Seal call uses.
func (k *TOTPKeyRing) ActiveKeyID() string {
	return k.activeID
}

// KnownKeyID reports whether id is the active or previous key ID in the
// ring, without decrypting anything ("Key failures").
func (k *TOTPKeyRing) KnownKeyID(id string) bool {
	_, ok := k.keys[id]
	return ok
}

// Seal encrypts secret under the active key with a fresh 12-byte nonce and
// the domain-separated associated data binding kind, accountID, and rowID
// ("Sealing"). The caller supplies rowID; Seal never creates one.
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

// Open validates the sealed value's shape, looks up its key ID, and decrypts
// and authenticates it against the same associated data Seal used. Moving
// ciphertext between accounts, rows, record kinds, format versions, or keys
// fails authentication ("Sealing").
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

// totpAAD builds the exact "Sealing" associated data:
//
//	aboutme.totp.v1 \x00 <kind> \x00 <account-id-bytes> \x00 <row-id-bytes> \x00 <version> \x00 <key-id>
//
// The raw 16-byte account and row UUIDs and the ASCII decimal format version
// bind the ciphertext to a single row, so it cannot be relabeled, reparented,
// or copied to another account, row, kind, version, or key.
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

// sealTOTPAEAD encrypts and authenticates plaintext with AES-256-GCM. It
// never returns a raw AEAD error: key construction failures here are
// unreachable for a [32]byte key.
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

// openTOTPAEAD decrypts and authenticates ciphertext. Any authentication
// failure, tampered ciphertext, nonce, or associated data, a truncated tag,
// or the wrong key collapses to the closed ErrTOTPAuthentication sentinel;
// the raw AEAD error never escapes.
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
