package oauthsrv

import (
	"crypto/sha256"
	"errors"
	"io"
)

var ErrTokenInvalid = errors.New("oauth token invalid")

type TokenKind string

const (
	TokenKindAccess  TokenKind = "access"
	TokenKindRefresh TokenKind = "refresh"
)

const (
	accessTokenPrefix  = "amat_"
	refreshTokenPrefix = "amrt_"
	tokenPrefixChars   = len(accessTokenPrefix)
	tokenChars         = tokenPrefixChars + secretBodyChars
)

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
