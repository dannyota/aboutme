package config

// TOTP_ENROLLMENT_ENABLED and TOTP_ACTIVE_KEY/TOTP_PREVIOUS_KEY parsing. See
// docs/design/totp-key-management.md "Key ring" and
// docs/design/totp-second-factor-contract.md.

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

// totpKeyEncodedBytes, totpKeyDecodedBytes, and the key-ID derivation
// constants mirror internal/secondfactor.DecodeTOTPKey and DeriveTOTPKeyID
// exactly (docs/design/totp-key-management.md "Key ring"). The packages
// cannot share one function directly because internal/secondfactor imports
// internal/auth, which imports this package (see passkey.go for the same
// constraint).
const (
	totpKeyEncodedBytes  = 43 // canonical unpadded base64url length of 32 bytes
	totpKeyDecodedBytes  = 32
	totpKeyIDPrefix      = "tk1_"
	totpKeyIDDigestBytes = 16
	totpKeyIDDomain      = "aboutme.totp.key-id.v1"
)

// loadTOTPEnrollmentFlag parses TOTP_ENROLLMENT_ENABLED. Blank or "false"
// leaves TOTP enrollment off, the documented default; "true" turns it on.
// The error never echoes the raw value.
func loadTOTPEnrollmentFlag(raw string) (bool, error) {
	switch strings.TrimSpace(raw) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, errors.New("config: TOTP_ENROLLMENT_ENABLED must be true or false")
	}
}

// loadTOTPKeyRing requires TOTP_ACTIVE_KEY and accepts an absent or present
// TOTP_PREVIOUS_KEY, each the canonical 43-character unpadded base64url
// encoding of 32 bytes. It fails startup when either value does not decode to
// that exact shape or when both derive the same key ID, before any database
// or entropy dependency is built. Neither raw value nor its decoded bytes
// ever appears in a returned error. It accepts no key-ID variable: the ID is
// derived, never configured (docs/design/totp-key-management.md "Key ring").
func loadTOTPKeyRing(activeRaw, previousRaw string) (active, previous string, err error) {
	activeID, decErr := totpKeyIDFor(activeRaw)
	if decErr != nil {
		return "", "", errors.New("config: TOTP_ACTIVE_KEY must be base64url encoding exactly 32 bytes")
	}
	if previousRaw == "" {
		return activeRaw, "", nil
	}
	previousID, decErr := totpKeyIDFor(previousRaw)
	if decErr != nil {
		return "", "", errors.New("config: TOTP_PREVIOUS_KEY must be base64url encoding exactly 32 bytes")
	}
	if previousID == activeID {
		return "", "", errors.New("config: TOTP_PREVIOUS_KEY must not derive the same key id as TOTP_ACTIVE_KEY")
	}
	return activeRaw, previousRaw, nil
}

// totpKeyIDFor decodes text as a canonical TOTP key and derives its key ID,
// mirroring secondfactor.DecodeTOTPKey and DeriveTOTPKeyID (see
// loadTOTPKeyRing). It returns an error, never the decoded bytes, for a
// malformed value.
func totpKeyIDFor(text string) (string, error) {
	if len(text) != totpKeyEncodedBytes {
		return "", errors.New("invalid totp key encoding")
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(text)
	if err != nil || len(decoded) != totpKeyDecodedBytes || base64.RawURLEncoding.EncodeToString(decoded) != text {
		return "", errors.New("invalid totp key encoding")
	}
	h := sha256.New()
	h.Write([]byte(totpKeyIDDomain))
	h.Write([]byte{0})
	h.Write(decoded)
	sum := h.Sum(nil)
	return totpKeyIDPrefix + base64.RawURLEncoding.EncodeToString(sum[:totpKeyIDDigestBytes]), nil
}

// LoadTOTPReencryptJob reads the reduced configuration the one-shot
// `server totp-key-reencrypt` command needs: DATABASE_URL and the TOTP key
// ring only. The task definition that runs this command grants it no
// PUBLIC_ORIGIN, auth-email, OAuth, or password-rate values
// (docs/design/totp-key-management.md "Rotation": "The one-shot path ...
// uses the app execution and task roles and the same injection").
func LoadTOTPReencryptJob(getenv func(string) string) (Config, error) {
	if getenv == nil {
		return Config{}, errors.New("config: environment reader is required")
	}
	databaseURL := strings.TrimSpace(getenv("DATABASE_URL"))
	if databaseURL == "" {
		return Config{}, errors.New("config: DATABASE_URL is required")
	}
	active, previous, err := loadTOTPKeyRing(getenv("TOTP_ACTIVE_KEY"), getenv("TOTP_PREVIOUS_KEY"))
	if err != nil {
		return Config{}, err
	}
	return Config{DatabaseURL: databaseURL, TOTPActiveKey: active, TOTPPreviousKey: previous}, nil
}
