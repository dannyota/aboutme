package config

// PASSKEY_ENROLLMENT_ENABLED parsing and its relying-party origin check. See
// docs/design/second-factor-authentication.md and
// docs/design/passkey-second-factor-contract.md.

import (
	"errors"
	"net"
	"net/url"
	"strings"
)

// loadPasskeyEnrollmentFlag parses PASSKEY_ENROLLMENT_ENABLED. Blank or
// "false" leaves passkey enrollment off, the documented default; "true" turns
// it on. The error never echoes the raw value.
func loadPasskeyEnrollmentFlag(raw string) (bool, error) {
	switch strings.TrimSpace(raw) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, errors.New("config: PASSKEY_ENROLLMENT_ENABLED must be true or false")
	}
}

// validatePasskeyRelyingPartyOrigin fails an enabled passkey-enrollment flag
// whose PUBLIC_ORIGIN cannot produce a WebAuthn relying party: an https
// origin with a DNS-shaped host, or an http origin on exactly "localhost". It
// mirrors internal/secondfactor.NewRelyingParty's origin rule so an invalid
// origin fails fast here, before any database or WebAuthn dependency is
// built; the packages cannot share one function directly because
// internal/secondfactor imports internal/auth, which imports this package.
func validatePasskeyRelyingPartyOrigin(publicOrigin string) error {
	u, err := url.Parse(publicOrigin)
	if err != nil || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") || u.Host == "" {
		return errors.New("config: PASSKEY_ENROLLMENT_ENABLED requires a plain PUBLIC_ORIGIN")
	}
	host := u.Hostname()
	if host != strings.ToLower(host) || net.ParseIP(host) != nil || !validRelyingPartyHost(host) {
		return errors.New("config: PASSKEY_ENROLLMENT_ENABLED requires a PUBLIC_ORIGIN host that is a valid WebAuthn relying party ID")
	}
	if u.Scheme != "https" && (u.Scheme != "http" || host != "localhost") {
		return errors.New("config: PASSKEY_ENROLLMENT_ENABLED requires an https PUBLIC_ORIGIN, or http on localhost")
	}
	return nil
}

// validRelyingPartyHost reports whether host is "localhost" or a lowercase
// DNS name of plain ASCII letter/digit/hyphen labels, no label starting or
// ending with a hyphen, at least two labels, and at most 253 characters
// total — the same shape internal/secondfactor.NewRelyingParty requires.
func validRelyingPartyHost(host string) bool {
	if host == "localhost" {
		return true
	}
	labels := strings.Split(host, ".")
	if len(host) > 253 || len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}
