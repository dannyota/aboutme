package showcase

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/dannyota/aboutme/apps/server/internal/previewcard"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

// Derived holds the three values Go keeps current on every showcase row.
type Derived struct {
	ReviewKey   string
	CardVersion string
	// TemplateID is nil when no preset matches ("Custom design").
	TemplateID *string
}

// reviewKeyInput is exactly what a review key covers. It leaves out the card
// layout version and the accent, so a layout release or a color change does not
// send a listing back to review (docs/design/showcase.md "Review").
type reviewKeyInput struct {
	Slug           string                        `json:"slug"`
	Language       string                        `json:"language"`
	Name           string                        `json:"name"`
	Headline       string                        `json:"headline"`
	PhotoKeyDigest string                        `json:"photoKeyDigest"`
	Crop           *publicresume.PublicPhotoCrop `json:"crop"`
}

// ReviewKey is the first 16 hex digits of SHA-256 over the preview card
// inputs' slug, language, scrubbed name, headline, photo storage key digest,
// and crop.
func ReviewKey(card previewcard.Card) (string, error) {
	encoded, err := json.Marshal(reviewKeyInput{
		Slug: card.Slug, Language: card.Lng, Name: card.Name, Headline: card.Headline,
		PhotoKeyDigest: card.PhotoKeyDigest, Crop: card.Crop,
	})
	if err != nil {
		return "", fmt.Errorf("showcase: encode review key input: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])[:previewcard.VersionLength], nil
}

// Derive computes the review key, card version, and template of an admitted
// public snapshot. It reads the same card inputs the preview card builder
// reads, so the key follows exactly what the card shows.
func Derive(snapshot publicresume.Snapshot) (Derived, error) {
	card, err := previewcard.FromSnapshot(snapshot)
	if err != nil {
		return Derived{}, err
	}
	version, err := card.Version()
	if err != nil {
		return Derived{}, err
	}
	key, err := ReviewKey(card)
	if err != nil {
		return Derived{}, err
	}
	template, err := TemplateID(snapshot.Public.Document.Customization)
	if err != nil {
		return Derived{}, err
	}
	return Derived{ReviewKey: key, CardVersion: version, TemplateID: template}, nil
}
