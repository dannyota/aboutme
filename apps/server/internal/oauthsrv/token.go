package oauthsrv

import (
	"crypto/sha256"
	"errors"
	"io"
)

// ErrTokenInvalid is the closed error for every malformed access- or
// refresh-token spelling and for a kind outside the closed set. It never
// carries the presented value.
var ErrTokenInvalid = errors.New("oauth token invalid")

// TokenKind is the closed set of token kinds M3 stores.
type TokenKind string

// The token kinds. The values are the stored kind column's spellings.
const (
	TokenKindAccess  TokenKind = "access"
	TokenKindRefresh TokenKind = "refresh"
)

const (
	// accessTokenPrefix and refreshTokenPrefix are the M3 token prefixes.
	accessTokenPrefix  = "amat_"
	refreshTokenPrefix = "amrt_"
	// tokenPrefixChars is the shared prefix width; tokenChars is the only
	// valid total token length.
	tokenPrefixChars = len(accessTokenPrefix)
	tokenChars       = tokenPrefixChars + secretBodyChars
)

// NewToken returns a fresh token of the given kind and the SHA-256 digest that
// is its only stored form. It reads exactly 32 bytes from entropy — after
// checking the kind, so an unknown kind consumes none — and returns no token
// at all when the reader is short.
func NewToken(kind TokenKind, entropy io.Reader) (raw string, digest [32]byte, err error) {
	prefix, ok := tokenPrefixFor(kind)
	if !ok {
		return "", [32]byte{}, ErrTokenInvalid
	}
	body, err := readSecretBody(entropy)
	if err != nil {
		return "", [32]byte{}, err
	}
	raw = prefix + body
	return raw, sha256.Sum256([]byte(raw)), nil
}

// ParseToken checks a presented token against the exact M3 spelling — a known
// 5-byte prefix followed by 43 canonical, unpadded base64url characters — and
// returns its kind and digest. Shape decoding happens before hashing, so no
// malformed spelling reaches a store lookup.
//
// The digest covers the prefix, which domain-separates the two kinds: the same
// entropy spelled as the other kind yields a different digest and so can never
// match a stored row of the kind it was not issued as.
func ParseToken(raw string) (kind TokenKind, digest [32]byte, err error) {
	if len(raw) != tokenChars {
		return "", [32]byte{}, ErrTokenInvalid
	}
	kind, ok := tokenKindFor(raw[:tokenPrefixChars])
	if !ok {
		return "", [32]byte{}, ErrTokenInvalid
	}
	if !isCanonicalSecretBody(raw[tokenPrefixChars:]) {
		return "", [32]byte{}, ErrTokenInvalid
	}
	return kind, sha256.Sum256([]byte(raw)), nil
}

// tokenPrefixFor maps a kind to its M3 prefix. The second result is false for
// any kind outside the closed set.
func tokenPrefixFor(kind TokenKind) (string, bool) {
	switch kind {
	case TokenKindAccess:
		return accessTokenPrefix, true
	case TokenKindRefresh:
		return refreshTokenPrefix, true
	default:
		return "", false
	}
}

// tokenKindFor maps an M3 prefix to its kind. The second result is false for
// any other prefix.
func tokenKindFor(prefix string) (TokenKind, bool) {
	switch prefix {
	case accessTokenPrefix:
		return TokenKindAccess, true
	case refreshTokenPrefix:
		return TokenKindRefresh, true
	default:
		return "", false
	}
}
