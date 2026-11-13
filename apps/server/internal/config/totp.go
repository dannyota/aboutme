package config

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

const (
	totpKeyEncodedBytes  = 43
	totpKeyDecodedBytes  = 32
	totpKeyIDPrefix      = "tk1_"
	totpKeyIDDigestBytes = 16
	totpKeyIDDomain      = "aboutme.totp.key-id.v1"
)

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
