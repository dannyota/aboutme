package previewcard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"

	"danny.vn/aboutme/apps/server/internal/previewmeta"
	"danny.vn/aboutme/apps/server/internal/printsnapshot"
	"danny.vn/aboutme/apps/server/internal/publicresume"
)

const (
	LayoutVersion  = 2
	MaxPNGBytes    = 524_288
	VersionLength  = 16
	fallbackAccent = "#000000"
)

var ErrInvalid = errors.New("previewcard: invalid card input")

var versionPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

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

func FromSnapshot(snapshot publicresume.Snapshot) (Card, error) {
	public := snapshot.Public
	person := public.Document.PersonalDetails
	accent, err := Accent(public.Document.Customization.Colors)
	if err != nil {
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

func (c Card) Version() (string, error) {
	encoded, err := json.Marshal(c)
	if err != nil {
		return "", ErrInvalid
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])[:VersionLength], nil
}

func (c Card) Input() printsnapshot.CardInput {
	return printsnapshot.CardInput{
		LayoutVersion: c.LayoutVersion, Lng: c.Lng, Slug: c.Slug, Name: c.Name, Headline: c.Headline,
		HasPhoto: c.PhotoKeyDigest != "", Crop: c.Crop, Accent: c.Accent,
	}
}

func ValidVersion(version string) bool { return versionPattern.MatchString(version) }

func Path(slug, version string) string {
	return "/api/v1/public/resumes/" + slug + "/og/" + version + ".png"
}

func VersionOf(snapshot publicresume.Snapshot) (string, error) {
	card, err := FromSnapshot(snapshot)
	if err != nil {
		return "", err
	}
	return card.Version()
}
