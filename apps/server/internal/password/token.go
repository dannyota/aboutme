package password

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
)

var ErrTokenInvalid = errors.New("password token invalid")

const tokenRawBytes = 32

type Token struct {
	Raw    string
	Digest [32]byte
}

func NewToken(r io.Reader) (Token, error) {
	raw := make([]byte, tokenRawBytes)
	if _, err := io.ReadFull(r, raw); err != nil {
		return Token{}, err
	}
	return Token{
		Raw:    base64.RawURLEncoding.EncodeToString(raw),
		Digest: sha256.Sum256(raw),
	}, nil
}

func DigestToken(raw string) ([32]byte, error) {
	var zero [32]byte
	if len(raw) != 43 {
		return zero, ErrTokenInvalid
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil {
		return zero, ErrTokenInvalid
	}
	if len(decoded) != tokenRawBytes {
		return zero, ErrTokenInvalid
	}
	return sha256.Sum256(decoded), nil
}

func EqualDigest(a, b [32]byte) bool {
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}
