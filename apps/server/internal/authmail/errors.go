package authmail

import "errors"

// Closed sentinels. None of them carries a key, nonce, plaintext, email,
// token, ciphertext, or raw AEAD error, so they are safe to surface.
var (
	// ErrInvalidKind rejects a kind outside the closed Kind set.
	ErrInvalidKind = errors.New("authmail: invalid email kind")
	// ErrInvalidEmail rejects a recipient that fails the structural check.
	ErrInvalidEmail = errors.New("authmail: invalid email address")
	// ErrInvalidLink rejects a link that does not match the kind's shape.
	ErrInvalidLink = errors.New("authmail: invalid link")
	// ErrUnknownVersion rejects a payload version or field set the kind does
	// not use.
	ErrUnknownVersion = errors.New("authmail: unknown payload version")
	// ErrStrictJSON rejects payload JSON or fields outside the closed shape.
	ErrStrictJSON = errors.New("authmail: payload is not strict JSON")
	// ErrInvalidKeyID rejects a key ID outside 1–64 printable ASCII bytes.
	ErrInvalidKeyID = errors.New("authmail: invalid key id")
	// ErrUnknownKey reports a key ID the ring does not hold.
	ErrUnknownKey = errors.New("authmail: unknown key id")
	// ErrKeyRing rejects an invalid key ring configuration.
	ErrKeyRing = errors.New("authmail: invalid key ring")
	// ErrNonce reports that the nonce source could not supply 12 bytes.
	ErrNonce = errors.New("authmail: nonce entropy failure")
	// ErrCiphertext rejects stored ciphertext outside 1–4,112 bytes.
	ErrCiphertext = errors.New("authmail: invalid ciphertext")
	// ErrAuthentication reports any AES-256-GCM failure, including tampering
	// and a wrong key.
	ErrAuthentication = errors.New("authmail: authentication failed")
	// ErrOversizedPayload rejects serialized plaintext over 4,096 bytes.
	ErrOversizedPayload = errors.New("authmail: payload too large")
	// ErrScope rejects scope IDs or a token digest that do not match the
	// kind.
	ErrScope = errors.New("authmail: invalid job scope")
	// ErrExpiry rejects a job expiry outside the kind's lifetime.
	ErrExpiry = errors.New("authmail: invalid expiry")
	// ErrJobID rejects a nil job ID.
	ErrJobID = errors.New("authmail: invalid job id")
	// ErrOutbox rejects a nil outbox dependency or transaction.
	ErrOutbox = errors.New("authmail: invalid outbox")
)
