package password

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"io"
)

// ErrTokenInvalid is the closed error returned for every malformed bearer
// token spelling.
var ErrTokenInvalid = errors.New("password token invalid")

// tokenRawBytes is the exact entropy length of a bearer token before base64url
// encoding.
const tokenRawBytes = 32

// Token is a freshly generated bearer token: its raw 43-character unpadded
// base64url spelling and the SHA-256 digest that is the only stored form.
type Token struct {
	Raw    string
	Digest [32]byte
}

// NewToken reads exactly 32 random bytes from r and returns the token spelling
// plus its SHA-256 digest.
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

// DigestToken decodes a presented token into its canonical 32 bytes and returns
// their SHA-256 digest. It rejects any other length, padding, alphabet, or
// noncanonical base64url spelling before any comparison happens.
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

// EqualDigest reports whether two token digests are equal in constant time.
func EqualDigest(a, b [32]byte) bool {
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}
