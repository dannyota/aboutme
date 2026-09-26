// Package previewcard builds, stores, and schedules the stored link-preview
// card of a live resume: a 1200 by 630 PNG drawn from a closed card envelope
// that holds no contact data. See docs/design/link-previews.md, "Preview
// card" and "Build, storage, and serving", and
// docs/adr/0055-stored-link-preview-card.md.
package previewcard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/dannyota/aboutme/apps/server/internal/previewmeta"
	"github.com/dannyota/aboutme/apps/server/internal/printsnapshot"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

const (
	// LayoutVersion names the card layout. The web card component pins a
	// hash of its markup and CSS to the same number, so a layout change
	// raises it on both sides and every live card gets a new version.
	LayoutVersion = 1
	// MaxPNGBytes bounds one stored card.
	MaxPNGBytes = 524_288
	// VersionLength is the number of hex digits in a card version.
	VersionLength = 16
	// fallbackAccent stands in for a template color that is not hex.
	fallbackAccent = "#000000"
)

// ErrInvalid reports a snapshot that cannot produce a card.
var ErrInvalid = errors.New("previewcard: invalid card input")

var versionPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// Card is every input the card shows or depends on, derived from an admitted
// public snapshot. The photo is named by the digest of its private storage
// key, which never leaves Go.
type Card struct {
	LayoutVersion  int                           `json:"layoutVersion"`
	Slug           string                        `json:"slug"`
	Lng            string                        `json:"lng"`
	Name           string                        `json:"name"`
	Headline       string                        `json:"headline"`
	PhotoKeyDigest string                        `json:"photoKeyDigest"`
	Crop           *publicresume.PublicPhotoCrop `json:"crop"`
	Accent         string                        `json:"accent"`
}

// FromSnapshot derives the card inputs of one public snapshot.
func FromSnapshot(snapshot publicresume.Snapshot) (Card, error) {
	public := snapshot.Public
	person := public.Document.PersonalDetails
	accent, err := Accent(public.Document.Customization.Colors)
	if err != nil {
		// Stored colors are schema-validated hex; this keeps a card for a
		// document that somehow is not.
		accent = fallbackAccent
	}
	name, headline := previewmeta.CardText(person)
	card := Card{
		LayoutVersion: LayoutVersion, Slug: public.Slug, Lng: printsnapshot.CardLanguage(public.Lng),
		Name: name, Headline: headline, Accent: accent,
	}
	if person.Photo != nil {
		card.PhotoKeyDigest = snapshot.PhotoKeyDigest()
		if card.PhotoKeyDigest == "" {
			return Card{}, ErrInvalid
		}
		if person.Photo.Crop != nil {
			crop := *person.Photo.Crop
			card.Crop = &crop
		}
	}
	return card, nil
}

// Version is the first 16 hex digits of SHA-256 over the card inputs.
func (c Card) Version() (string, error) {
	encoded, err := json.Marshal(c)
	if err != nil {
		return "", ErrInvalid
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])[:VersionLength], nil
}

// Input is the card content the print envelope carries.
func (c Card) Input() printsnapshot.CardInput {
	return printsnapshot.CardInput{
		LayoutVersion: c.LayoutVersion, Lng: c.Lng, Slug: c.Slug, Name: c.Name, Headline: c.Headline,
		HasPhoto: c.PhotoKeyDigest != "", Crop: c.Crop, Accent: c.Accent,
	}
}

// ValidVersion reports whether version has the exact card version form.
func ValidVersion(version string) bool { return versionPattern.MatchString(version) }

// Path is the public path of one card version.
func Path(slug, version string) string {
	return "/api/v1/public/resumes/" + slug + "/og/" + version + ".png"
}

// VersionOf is the current card version of one public snapshot.
func VersionOf(snapshot publicresume.Snapshot) (string, error) {
	card, err := FromSnapshot(snapshot)
	if err != nil {
		return "", err
	}
	return card.Version()
}
