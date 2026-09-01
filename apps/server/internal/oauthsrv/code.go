// Package oauthsrv implements the first-party OAuth 2.1 authorization server
// frozen in docs/plans/phase-pm/decisions.md. This file holds the M2
// authorization-code primitives; the package's other files hold the M3 token
// spellings, PKCE verification, the M1 registration grammar, and the closed
// scope set.
package oauthsrv

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

// ErrCodeInvalid is the closed error for every malformed authorization-code
// spelling. It never carries the presented value.
var ErrCodeInvalid = errors.New("oauth code invalid")

const (
	// secretEntropyBytes is the entropy behind every code and token: 32
	// random bytes (M2, M3).
	secretEntropyBytes = 32
	// secretBodyChars is secretEntropyBytes rendered as unpadded base64url.
	secretBodyChars = 43
)

// NewCode returns a fresh authorization code and the SHA-256 digest that is
// its only stored form. It reads exactly 32 bytes from entropy and returns no
// code at all when the reader is short.
func NewCode(entropy io.Reader) (raw string, digest [32]byte, err error) {
	body, err := readSecretBody(entropy)
	if err != nil {
		return "", [32]byte{}, err
	}
	return body, sha256.Sum256([]byte(body)), nil
}

// ParseCode checks a presented code against the exact M2 spelling — 43
// canonical, unpadded base64url characters, no prefix — and returns its
// digest. Shape decoding happens before hashing, so no malformed spelling
// reaches a store lookup.
func ParseCode(raw string) (digest [32]byte, err error) {
	if len(raw) != secretBodyChars {
		return [32]byte{}, ErrCodeInvalid
	}
	if !isCanonicalSecretBody(raw) {
		return [32]byte{}, ErrCodeInvalid
	}
	return sha256.Sum256([]byte(raw)), nil
}

// readSecretBody reads exactly secretEntropyBytes from entropy and returns
// their unpadded base64url spelling.
func readSecretBody(entropy io.Reader) (string, error) {
	var buf [secretEntropyBytes]byte
	if _, err := io.ReadFull(entropy, buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// isCanonicalSecretBody reports whether body is the one canonical unpadded
// base64url spelling of secretEntropyBytes bytes. Strict decoding rejects
// padding, the standard alphabet, and non-zero trailing bits, so two spellings
// of one value can never both parse.
func isCanonicalSecretBody(body string) bool {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(body)
	return err == nil && len(decoded) == secretEntropyBytes
}
