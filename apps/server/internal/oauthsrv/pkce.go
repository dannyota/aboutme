package oauthsrv

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

// RFC 7636 section 4.1 bounds the code verifier to 43-128 characters.
const (
	pkceVerifierMinChars = 43
	pkceVerifierMaxChars = 128
)

func VerifyS256(challenge string, verifier string) bool {
	if !isPKCEVerifier(verifier) {
		return false
	}
	if len(challenge) != secretBodyChars {
		return false
	}
	want, err := base64.RawURLEncoding.Strict().DecodeString(challenge)
	if err != nil || len(want) != sha256.Size {
		return false
	}
	got := sha256.Sum256([]byte(verifier))
	return subtle.ConstantTimeCompare(got[:], want) == 1
}

func isPKCEVerifier(raw string) bool {
	if len(raw) < pkceVerifierMinChars || len(raw) > pkceVerifierMaxChars {
		return false
	}
	for i := 0; i < len(raw); i++ {
		switch c := raw[i]; {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '-', c == '.', c == '_', c == '~':
		default:
			return false
		}
	}
	return true
}
