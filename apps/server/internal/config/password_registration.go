package config

import (
	"errors"
	"strings"
)

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
