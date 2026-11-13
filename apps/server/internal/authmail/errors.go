package authmail

import "errors"

var (
	ErrInvalidKind      = errors.New("authmail: invalid email kind")
	ErrInvalidEmail     = errors.New("authmail: invalid email address")
	ErrInvalidLink      = errors.New("authmail: invalid link")
	ErrUnknownVersion   = errors.New("authmail: unknown payload version")
	ErrStrictJSON       = errors.New("authmail: payload is not strict JSON")
	ErrInvalidKeyID     = errors.New("authmail: invalid key id")
	ErrUnknownKey       = errors.New("authmail: unknown key id")
	ErrKeyRing          = errors.New("authmail: invalid key ring")
	ErrNonce            = errors.New("authmail: nonce entropy failure")
	ErrCiphertext       = errors.New("authmail: invalid ciphertext")
	ErrAuthentication   = errors.New("authmail: authentication failed")
	ErrOversizedPayload = errors.New("authmail: payload too large")
	ErrScope            = errors.New("authmail: invalid job scope")
	ErrExpiry           = errors.New("authmail: invalid expiry")
	ErrJobID            = errors.New("authmail: invalid job id")
	ErrOutbox           = errors.New("authmail: invalid outbox")
)
