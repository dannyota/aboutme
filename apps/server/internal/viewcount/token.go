package viewcount

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
)

const (
	viewIDSize      = 16
	tokenPlainSize  = 16 + 8 + viewIDSize
	tokenNonceSize  = 12
	tokenSealedSize = tokenNonceSize + tokenPlainSize + 16
	maxTokenText    = 200
)

var errInvalidToken = errors.New("viewcount: invalid token")

type viewID [viewIDSize]byte

func (id viewID) text() string {
	return base64.RawURLEncoding.EncodeToString(id[:])
}

type tokenClaims struct {
	resumeID uuid.UUID
	issuedAt time.Time
	id       viewID
}

type sealer struct {
	aead cipher.AEAD
	rand io.Reader
}

func newSealer(random io.Reader) (*sealer, error) {
	key := make([]byte, 32)
	if _, err := io.ReadFull(random, key); err != nil {
		return nil, fmt.Errorf("viewcount: token key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("viewcount: token cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("viewcount: token aead: %w", err)
	}
	return &sealer{aead: aead, rand: random}, nil
}

func (s *sealer) seal(resumeID uuid.UUID, issuedAt time.Time) (string, viewID, error) {
	var id viewID
	if _, err := io.ReadFull(s.rand, id[:]); err != nil {
		return "", id, fmt.Errorf("viewcount: view id: %w", err)
	}
	nonce := make([]byte, tokenNonceSize, tokenSealedSize)
	if _, err := io.ReadFull(s.rand, nonce); err != nil {
		return "", id, fmt.Errorf("viewcount: token nonce: %w", err)
	}
	plain := make([]byte, 0, tokenPlainSize)
	plain = append(plain, resumeID[:]...)
	plain = binary.BigEndian.AppendUint64(plain, uint64(issuedAt.UnixMilli())) //nolint:gosec // Unix times after 1970 are positive.
	plain = append(plain, id[:]...)
	sealed := s.aead.Seal(nonce, nonce, plain, nil)
	return base64.RawURLEncoding.EncodeToString(sealed), id, nil
}

func (s *sealer) open(token string) (tokenClaims, error) {
	if len(token) > maxTokenText {
		return tokenClaims{}, errInvalidToken
	}
	sealed, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(sealed) != tokenSealedSize {
		return tokenClaims{}, errInvalidToken
	}
	plain, err := s.aead.Open(nil, sealed[:tokenNonceSize], sealed[tokenNonceSize:], nil)
	if err != nil || len(plain) != tokenPlainSize {
		return tokenClaims{}, errInvalidToken
	}
	var claims tokenClaims
	copy(claims.resumeID[:], plain[:16])
	millis := binary.BigEndian.Uint64(plain[16:24])
	claims.issuedAt = time.UnixMilli(int64(millis)) //nolint:gosec // seal wrote a positive Unix time.
	copy(claims.id[:], plain[24:])
	return claims, nil
}
