// Package authmail implements the encrypted transactional email
// outbox: closed payload types, the AES-256-GCM key ring, and the
// transaction-scoped enqueue primitive. It imports no crypto dependency beyond
// the standard library and never holds or logs plaintext, keys, nonces, or
// raw tokens.
package authmail

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/accountemail"
)

// Kind names a closed transactional-email type. It is the single authority for
// auth_email_jobs.kind values.
type Kind string

// Closed Kind values written to auth_email_jobs.kind.
const (
	// KindVerify is the registration verification email.
	KindVerify Kind = "verify"
	// KindReset is the password reset email.
	KindReset Kind = "reset"
	// KindPasswordChanged notifies that the account password changed.
	KindPasswordChanged Kind = "password_changed"
	// KindSecondFactorEnabled notifies that a second factor was enabled.
	KindSecondFactorEnabled Kind = "second_factor_enabled"
	// KindPasskeyAdded notifies that a passkey was added.
	KindPasskeyAdded Kind = "passkey_added"
	// KindPasskeyRemoved notifies that a passkey was removed.
	KindPasskeyRemoved Kind = "passkey_removed"
	// KindSecondFactorDisabled notifies that second-factor sign-in was
	// turned off.
	KindSecondFactorDisabled Kind = "second_factor_disabled"
	// KindRecoveryCodesRegenerated notifies that recovery codes were
	// replaced.
	KindRecoveryCodesRegenerated Kind = "recovery_codes_regenerated"
	// KindRecoveryCodeUsed notifies that a recovery code was used; its
	// payload carries the remaining count.
	KindRecoveryCodeUsed Kind = "recovery_code_used"
	// KindSecondFactorAttemptsExhausted notifies that second-factor attempts
	// ran out.
	KindSecondFactorAttemptsExhausted Kind = "second_factor_attempts_exhausted"
	// KindTOTPAdded notifies that an authenticator app was added.
	KindTOTPAdded Kind = "totp_added"
	// KindTOTPReplaced notifies that the authenticator app was replaced.
	KindTOTPReplaced Kind = "totp_replaced"
	// KindTOTPRemoved notifies that the authenticator app was removed.
	KindTOTPRemoved Kind = "totp_removed"
	// KindPrivacyNotice is the fixed account privacy notice.
	KindPrivacyNotice Kind = "privacy_notice"
)

// PrivacyNoticeID is the only accepted privacy notice campaign.
const PrivacyNoticeID = "crowdsec-postgres-paths-v1"

// payloadVersion remains the closed payload version for existing email jobs.
const payloadVersion = 1

const securityPayloadVersion = 2

// canonicalLinkOrigin is the hard-coded origin used to build verification and
// reset links: a clearly named placeholder, not a sender identity. Link
// validation checks only the prefix; full canonicalization and token
// construction are the caller's job.
const canonicalLinkOrigin = "https://aboutme.vn"

const (
	verifyLinkPrefix = canonicalLinkOrigin + "/verify-email#token="
	resetLinkPrefix  = canonicalLinkOrigin + "/reset-password#token="
)

// Payload is the closed outbox plaintext. Version 1 has a verification or
// reset link when required. Version 2 has an exact UTC event time and only
// recovery-code use carries a remaining count. Version 3 binds the fixed privacy
// notice to an account and ledger generation without message or link fields.
type Payload struct {
	// Version is the payload version: 1, 2, or 3, fixed by the kind.
	Version int `json:"version"`
	// To is the recipient address.
	To string `json:"to"`
	// Link is the version 1 verification or reset link.
	Link string `json:"link,omitempty"`
	// OccurredAt is the version 2 event time, RFC 3339 in whole UTC seconds.
	OccurredAt string `json:"occurredAt,omitempty"`
	// RemainingRecoveryCodes is the 0–9 count a recovery-code-used payload
	// carries.
	RemainingRecoveryCodes *int `json:"remainingRecoveryCodes,omitempty"`
	// UserID is the canonical account UUID a version 3 privacy notice binds.
	UserID string `json:"-"`
	// NoticeID is the version 3 privacy notice campaign, PrivacyNoticeID.
	NoticeID string `json:"-"`
	// Generation is the non-negative ledger generation a version 3 privacy
	// notice binds.
	Generation int32 `json:"-"`

	linkPresent                   bool
	occurredAtPresent             bool
	remainingRecoveryCodesPresent bool
	noticeFieldsPresent           bool
}

// Sealed is an encrypted payload plus the metadata the database stores beside
// it. Nonce is exactly 12 bytes by type; Ciphertext is 1–4,112 bytes.
type Sealed struct {
	// KeyID names the ring key that sealed the payload.
	KeyID string
	// Nonce is the fresh AES-256-GCM nonce.
	Nonce [12]byte
	// Ciphertext is the sealed payload with its 16-byte tag.
	Ciphertext []byte
}

func validateKind(k Kind) error {
	switch k {
	case KindVerify, KindReset, KindPasswordChanged,
		KindSecondFactorEnabled, KindPasskeyAdded, KindPasskeyRemoved,
		KindSecondFactorDisabled, KindRecoveryCodesRegenerated,
		KindRecoveryCodeUsed, KindSecondFactorAttemptsExhausted,
		KindTOTPAdded, KindTOTPReplaced, KindTOTPRemoved, KindPrivacyNotice:
		return nil
	default:
		return ErrInvalidKind
	}
}

// validateEmail performs the minimal structural check and leaves full
// account-email canonicalization to the caller: non-empty, exactly one '@',
// non-empty local and domain parts, and no whitespace or control bytes.
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

// validateLink enforces the per-kind link shape: verify/reset use the
// canonical origin plus the fragment route with a non-empty token;
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
	if err := validateEmail(p.To); err != nil {
		return err
	}
	if kind != KindPrivacyNotice && (p.noticeFieldsPresent || p.UserID != "" || p.NoticeID != "" || p.Generation != 0) {
		return ErrStrictJSON
	}
	switch kind {
	case KindPrivacyNotice:
		canonical, err := accountemail.Canonicalize(p.To)
		userID, uuidErr := uuid.Parse(p.UserID)
		if err != nil || canonical != p.To || uuidErr != nil || userID == uuid.Nil || userID.String() != p.UserID {
			return ErrStrictJSON
		}
		if p.Version != 3 || p.NoticeID != PrivacyNoticeID || p.Generation < 0 || p.linkPresent || p.Link != "" || p.occurredAtPresent || p.OccurredAt != "" || p.remainingRecoveryCodesPresent || p.RemainingRecoveryCodes != nil {
			return ErrStrictJSON
		}
		return nil
	case KindVerify, KindReset, KindPasswordChanged:
		if p.Version != payloadVersion || p.occurredAtPresent || p.remainingRecoveryCodesPresent || p.OccurredAt != "" || p.RemainingRecoveryCodes != nil {
			return ErrUnknownVersion
		}
		if kind == KindPasswordChanged && p.linkPresent {
			return ErrInvalidLink
		}
		return validateLink(kind, p.Link)
	default:
		if p.Version != securityPayloadVersion || p.linkPresent || p.Link != "" || !isWholeSecondUTC(p.OccurredAt) {
			return ErrUnknownVersion
		}
		if kind == KindRecoveryCodeUsed {
			if !p.remainingRecoveryCodesPresent && p.RemainingRecoveryCodes == nil {
				return ErrStrictJSON
			}
			if p.RemainingRecoveryCodes == nil || *p.RemainingRecoveryCodes < 0 || *p.RemainingRecoveryCodes > 9 {
				return ErrStrictJSON
			}
		} else if p.remainingRecoveryCodesPresent || p.RemainingRecoveryCodes != nil {
			return ErrStrictJSON
		}
		return nil
	}
}

func isWholeSecondUTC(value string) bool {
	parsed, err := time.Parse(time.RFC3339, value)
	return err == nil && parsed.Location() == time.UTC && parsed.Nanosecond() == 0 && parsed.Format(time.RFC3339) == value
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
	if p.Version == 3 {
		return json.Marshal(struct {
			Version    int    `json:"version"`
			To         string `json:"to"`
			UserID     string `json:"userId"`
			NoticeID   string `json:"noticeId"`
			Generation int32  `json:"generation"`
		}{p.Version, p.To, p.UserID, p.NoticeID, p.Generation})
	}
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
	seen := make(map[string]bool, 5)
	for dec.More() {
		keyTok, keyErr := dec.Token()
		if keyErr != nil {
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
			if decodeErr := dec.Decode(&v); decodeErr != nil {
				return Payload{}, ErrStrictJSON
			}
			p.Version = v
		case "to":
			var s string
			if decodeErr := dec.Decode(&s); decodeErr != nil {
				return Payload{}, ErrStrictJSON
			}
			p.To = s
		case "link":
			var s string
			if decodeErr := dec.Decode(&s); decodeErr != nil {
				return Payload{}, ErrStrictJSON
			}
			p.Link = s
			p.linkPresent = true
		case "userId", "noticeId", "generation":
			var raw json.RawMessage
			if decodeErr := dec.Decode(&raw); decodeErr != nil || bytes.Equal(raw, []byte("null")) {
				return Payload{}, ErrStrictJSON
			}
			var target any
			switch key {
			case "userId":
				target = &p.UserID
			case "noticeId":
				target = &p.NoticeID
			case "generation":
				target = &p.Generation
			}
			if decodeErr := json.Unmarshal(raw, target); decodeErr != nil {
				return Payload{}, ErrStrictJSON
			}
			p.noticeFieldsPresent = true
		case "occurredAt":
			var s string
			if decodeErr := dec.Decode(&s); decodeErr != nil {
				return Payload{}, ErrStrictJSON
			}
			p.OccurredAt = s
			p.occurredAtPresent = true
		case "remainingRecoveryCodes":
			var raw json.RawMessage
			if decodeErr := dec.Decode(&raw); decodeErr != nil || bytes.Equal(raw, []byte("null")) {
				return Payload{}, ErrStrictJSON
			}
			var n int
			if decodeErr := json.Unmarshal(raw, &n); decodeErr != nil {
				return Payload{}, ErrStrictJSON
			}
			p.RemainingRecoveryCodes = &n
			p.remainingRecoveryCodesPresent = true
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
	if p.Version == 3 && (!seen["userId"] || !seen["noticeId"] || !seen["generation"]) {
		return Payload{}, ErrStrictJSON
	}

	return p, nil
}
