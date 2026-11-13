package oauthsrv

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

var ErrCodeInvalid = errors.New("oauth code invalid")

const (
	secretEntropyBytes = 32
	secretBodyChars    = 43
)

func NewCode(entropy io.Reader) (raw string, digest [32]byte, err error) {
	body, err := readSecretBody(entropy)
	if err != nil {
		return "", [32]byte{}, err
	}
	return body, sha256.Sum256([]byte(body)), nil
}

func ParseCode(raw string) (digest [32]byte, err error) {
	if len(raw) != secretBodyChars {
		return [32]byte{}, ErrCodeInvalid
	}
	if !isCanonicalSecretBody(raw) {
		return [32]byte{}, ErrCodeInvalid
	}
	return sha256.Sum256([]byte(raw)), nil
}

func readSecretBody(entropy io.Reader) (string, error) {
	var buf [secretEntropyBytes]byte
	if _, err := io.ReadFull(entropy, buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

func isCanonicalSecretBody(body string) bool {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(body)
	return err == nil && len(decoded) == secretEntropyBytes
}
