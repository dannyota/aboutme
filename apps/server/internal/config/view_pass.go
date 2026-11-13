package config

import (
	"encoding/base64"
	"errors"
	"strings"
)

const viewPassKeyEncodedBytes = 43

func loadSignInToViewFlag(raw string) (bool, error) {
	switch strings.TrimSpace(raw) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, errors.New("config: SIGN_IN_TO_VIEW_ENABLED must be true or false")
	}
}

func loadViewPassKey(raw string) (string, error) {
	if len(raw) != viewPassKeyEncodedBytes {
		return "", errors.New("config: VIEW_PASS_KEY must be base64url encoding exactly 32 bytes")
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != raw {
		return "", errors.New("config: VIEW_PASS_KEY must be base64url encoding exactly 32 bytes")
	}
	return raw, nil
}
