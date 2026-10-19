package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/dannyota/aboutme/apps/server/internal/authmail"
)

func privacyNoticeKeyRing(getenv func(string) string) (*authmail.KeyRing, error) {
	invalid := errors.New("privacy notice: encryption keys invalid")
	decode := func(value string) ([32]byte, error) {
		var key [32]byte
		decoded, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimSpace(value))
		if err != nil || len(decoded) != len(key) {
			return key, invalid
		}
		copy(key[:], decoded)
		return key, nil
	}
	activeID := strings.TrimSpace(getenv("AUTH_EMAIL_ACTIVE_KEY_ID"))
	active, err := decode(getenv("AUTH_EMAIL_ACTIVE_KEY"))
	if err != nil {
		return nil, invalid
	}
	keys := map[string][32]byte{activeID: active}
	previousID, previousRaw := strings.TrimSpace(getenv("AUTH_EMAIL_PREVIOUS_KEY_ID")), strings.TrimSpace(getenv("AUTH_EMAIL_PREVIOUS_KEY"))
	if (previousID == "") != (previousRaw == "") || (previousID != "" && previousID == activeID) {
		return nil, invalid
	}
	if previousID != "" {
		previous, previousErr := decode(previousRaw)
		if previousErr != nil {
			return nil, invalid
		}
		keys[previousID] = previous
	}
	ring, err := authmail.NewKeyRing(activeID, keys, rand.Reader)
	if err != nil {
		return nil, invalid
	}
	return ring, nil
}
