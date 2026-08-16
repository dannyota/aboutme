// Package authmail implements the Phase PA encrypted transactional email
// outbox: closed payload types, the D3 AES-256-GCM key ring, and the
// transaction-scoped enqueue primitive. It imports no crypto dependency beyond
// the standard library and never holds or logs plaintext, keys, nonces, or
// raw tokens.
package authmail

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

// Kind names the three closed transactional-email types. It is the single
// authority for the auth_email_jobs.kind values (D3).
type Kind string

const (
	KindVerify          Kind = "verify"
	KindReset           Kind = "reset"
	KindPasswordChanged Kind = "password_changed"
)

// payloadVersion is the only supported payload version. Any other version is
// rejected on both seal and open.
const payloadVersion = 1

// canonicalLinkOrigin is the hard-coded origin used to build verification and
// reset links (ruling: a clearly-named placeholder, not a sender identity).
// Link validation checks only the prefix; full D1 canonicalization and token
// construction are the caller's job.
const canonicalLinkOrigin = "https://aboutme.vn"

const (
	verifyLinkPrefix = canonicalLinkOrigin + "/verify-email#token="
	resetLinkPrefix  = canonicalLinkOrigin + "/reset-password#token="
)

// Payload is the closed outbox plaintext. Link is empty (and omitted) for
// password-changed; for verify/reset it is the canonical origin plus the D8
// fragment route and token.
type Payload struct {
	Version int    `json:"version"`
	To      string `json:"to"`
	Link    string `json:"link,omitempty"`
}

// Sealed is an encrypted payload plus the metadata the database stores beside
// it. Nonce is exactly 12 bytes by type; Ciphertext is 1–4,112 bytes.
type Sealed struct {
	KeyID      string
	Nonce      [12]byte
	Ciphertext []byte
}

// Closed sentinels. None of them carries a key, nonce, plaintext, email,
// token, ciphertext, or raw AEAD error, so they are safe to surface.
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

func validateKind(k Kind) error {
	switch k {
	case KindVerify, KindReset, KindPasswordChanged:
		return nil
	default:
		return ErrInvalidKind
	}
}

// validateEmail performs the minimal structural check (ruling: full D1
// canonicalization is the caller's job): non-empty, exactly one '@', non-empty
// local and domain parts, and no whitespace or control bytes.
func validateEmail(to string) error {
	if to == "" {
		return ErrInvalidEmail
	}
	at := strings.IndexByte(to, '@')
	if at <= 0 || at == len(to)-1 {
		return ErrInvalidEmail
	}
	if strings.IndexByte(to[at+1:], '@') != -1 {
		return ErrInvalidEmail
	}
	for i := 0; i < len(to); i++ {
		if c := to[i]; c <= 0x20 || c == 0x7f {
			return ErrInvalidEmail
		}
	}
	return nil
}

// validateLink enforces the per-kind link shape (ruling): verify/reset use the
// canonical origin plus the D8 fragment route with a non-empty token;
// password-changed has no link.
func validateLink(kind Kind, link string) error {
	switch kind {
	case KindVerify:
		if !strings.HasPrefix(link, verifyLinkPrefix) || len(link) == len(verifyLinkPrefix) {
			return ErrInvalidLink
		}
		return nil
	case KindReset:
		if !strings.HasPrefix(link, resetLinkPrefix) || len(link) == len(resetLinkPrefix) {
			return ErrInvalidLink
		}
		return nil
	case KindPasswordChanged:
		if link != "" {
			return ErrInvalidLink
		}
		return nil
	default:
		return ErrInvalidKind
	}
}

func validatePayload(kind Kind, p Payload) error {
	if err := validateKind(kind); err != nil {
		return err
	}
	if p.Version != payloadVersion {
		return ErrUnknownVersion
	}
	if err := validateEmail(p.To); err != nil {
		return err
	}
	return validateLink(kind, p.Link)
}

// validateKeyID matches the database key_id check: 1–64 printable ASCII bytes
// (0x20–0x7e).
func validateKeyID(id string) error {
	if len(id) < 1 || len(id) > 64 {
		return ErrInvalidKeyID
	}
	for i := 0; i < len(id); i++ {
		if id[i] < 0x20 || id[i] > 0x7e {
			return ErrInvalidKeyID
		}
	}
	return nil
}

func marshalPayload(p Payload) ([]byte, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, ErrStrictJSON
	}
	return b, nil
}

// decodePayloadStrict parses exactly one JSON object with no duplicate keys,
// no unknown fields, no trailing content, and exact scalar types.
func decodePayloadStrict(data []byte) (Payload, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()

	tok, err := dec.Token()
	if err != nil {
		return Payload{}, ErrStrictJSON
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return Payload{}, ErrStrictJSON
	}

	var p Payload
	seen := make(map[string]bool, 3)
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return Payload{}, ErrStrictJSON
		}
		key, ok := keyTok.(string)
		if !ok {
			return Payload{}, ErrStrictJSON
		}
		if seen[key] {
			return Payload{}, ErrStrictJSON
		}
		seen[key] = true

		switch key {
		case "version":
			var v int
			if err := dec.Decode(&v); err != nil {
				return Payload{}, ErrStrictJSON
			}
			p.Version = v
		case "to":
			var s string
			if err := dec.Decode(&s); err != nil {
				return Payload{}, ErrStrictJSON
			}
			p.To = s
		case "link":
			var s string
			if err := dec.Decode(&s); err != nil {
				return Payload{}, ErrStrictJSON
			}
			p.Link = s
		default:
			return Payload{}, ErrStrictJSON
		}
	}

	tok, err = dec.Token()
	if err != nil {
		return Payload{}, ErrStrictJSON
	}
	if d, ok := tok.(json.Delim); !ok || d != '}' {
		return Payload{}, ErrStrictJSON
	}

	if _, err := dec.Token(); err != io.EOF {
		return Payload{}, ErrStrictJSON
	}

	if !seen["version"] || !seen["to"] {
		return Payload{}, ErrStrictJSON
	}

	return p, nil
}
