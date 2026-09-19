package config

import (
	"errors"
	"strings"
)

// loadPasswordRegistrationFlag parses PASSWORD_REGISTRATION_ENABLED and returns
// whether email-and-password sign-up is disabled. Blank or "true" keeps it on,
// so existing environments are unchanged; "false" turns it off. The error
// never echoes the raw value.
func loadPasswordRegistrationFlag(raw string) (disabled bool, err error) {
	switch strings.TrimSpace(raw) {
	case "", "true":
		return false, nil
	case "false":
		return true, nil
	default:
		return false, errors.New("config: PASSWORD_REGISTRATION_ENABLED must be true or false")
	}
}
