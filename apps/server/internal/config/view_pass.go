package config

// Sign in to view (docs/design/viewer-analytics/sign-in-to-view.md
// "Setting", "Pass cookie"; ADR 0022). SIGN_IN_TO_VIEW_ENABLED lets owners
// turn the per-resume switch on; SIGN_IN_TO_VIEW_LINKEDIN_ENABLED additionally
// gates whether the gate offers LinkedIn. VIEW_PASS_KEY seals the pass
// cookie.

import (
	"encoding/base64"
	"errors"
	"strings"
)

// viewPassKeyEncodedBytes is the unpadded base64url length of 32 bytes,
// matching the TOTP key ring encoding (see totpKeyEncodedBytes).
const viewPassKeyEncodedBytes = 43

// loadSignInToViewFlag parses SIGN_IN_TO_VIEW_ENABLED. Blank or "false"
// keeps the switch unavailable to owners (the default); "true" lets a
// publish request turn it on. It never lifts the gate on an already-gated
// resume.
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

// loadSignInToViewLinkedInFlag parses SIGN_IN_TO_VIEW_LINKEDIN_ENABLED.
// Blank or "false" keeps LinkedIn off the gate (the default) even when
// LinkedIn account login is enabled; "true" lets the gate also offer
// LinkedIn, subject to LinkedIn account login also being enabled.
func loadSignInToViewLinkedInFlag(raw string) (bool, error) {
	switch strings.TrimSpace(raw) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, errors.New("config: SIGN_IN_TO_VIEW_LINKEDIN_ENABLED must be true or false")
	}
}

// loadViewPassKey requires VIEW_PASS_KEY, the canonical 43-character
// unpadded base64url encoding of 32 bytes, mirroring loadTOTPKeyRing's
// encoding. It is required whether or not sign in to view is enabled,
// because rotating it must remain possible without also flipping the
// feature flag, and a later Load call that turns the flag on must not
// silently start sealing passes with an absent key. The raw value and its
// decoded bytes never appear in a returned error or elsewhere in this
// package's output.
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
