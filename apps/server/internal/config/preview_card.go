package config

import (
	"errors"
	"strings"
)

func loadPreviewCardFlag(raw string) (bool, error) {
	switch strings.TrimSpace(raw) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, errors.New("config: PREVIEW_CARD_ENABLED must be true or false")
	}
}
