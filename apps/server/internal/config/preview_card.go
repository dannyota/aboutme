package config

// PREVIEW_CARD_ENABLED parsing. See docs/design/link-previews.md and
// docs/adr/0055-stored-link-preview-card.md.

import (
	"errors"
	"strings"
)

// loadPreviewCardFlag parses PREVIEW_CARD_ENABLED. Blank or "false" keeps
// the og.png share image, the default until the web renderer draws the card
// envelope; "true" turns on stored preview cards. The error never echoes the
// raw value.
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
