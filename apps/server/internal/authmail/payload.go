package authmail

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/accountemail"
)

type Kind string

const (
	KindVerify                        Kind = "verify"
	KindReset                         Kind = "reset"
	KindPasswordChanged               Kind = "password_changed"
	KindSecondFactorEnabled           Kind = "second_factor_enabled"
	KindPasskeyAdded                  Kind = "passkey_added"
	KindPasskeyRemoved                Kind = "passkey_removed"
	KindSecondFactorDisabled          Kind = "second_factor_disabled"
	KindRecoveryCodesRegenerated      Kind = "recovery_codes_regenerated"
	KindRecoveryCodeUsed              Kind = "recovery_code_used"
	KindSecondFactorAttemptsExhausted Kind = "second_factor_attempts_exhausted"
	KindTOTPAdded                     Kind = "totp_added"
	KindTOTPReplaced                  Kind = "totp_replaced"
	KindTOTPRemoved                   Kind = "totp_removed"
	KindPrivacyNotice                 Kind = "privacy_notice"
)

const PrivacyNoticeID = "crowdsec-postgres-paths-v1"

const payloadVersion = 1

const securityPayloadVersion = 2

const canonicalLinkOrigin = "https://aboutme.vn"

const (
	verifyLinkPrefix = canonicalLinkOrigin + "/verify-email#token="
	resetLinkPrefix  = canonicalLinkOrigin + "/reset-password#token="
)

type Payload struct {
	Version                int    `json:"version"`
	To                     string `json:"to"`
	Link                   string `json:"link,omitempty"`
	OccurredAt             string `json:"occurredAt,omitempty"`
	RemainingRecoveryCodes *int   `json:"remainingRecoveryCodes,omitempty"`
	UserID                 string `json:"-"`
	NoticeID               string `json:"-"`
	Generation             int32  `json:"-"`

	linkPresent                   bool
	occurredAtPresent             bool
	remainingRecoveryCodesPresent bool
	noticeFieldsPresent           bool
}

type Sealed struct {
	KeyID      string
	Nonce      [12]byte
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
