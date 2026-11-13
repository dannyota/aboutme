package printsnapshot

import (
	"encoding/json"
	"regexp"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/publicroots"
)

const (
	CardKind              = "card"
	MaxCardTextCharacters = 160
)

var cardAccentPattern = regexp.MustCompile(`^#[0-9a-f]{6}$`)

type CardEnvelope struct {
	Version  int         `json:"version"`
	Kind     string      `json:"kind"`
	ResumeID string      `json:"resumeId"`
	Card     CardContent `json:"card"`
}

type CardContent struct {
	LayoutVersion int        `json:"layoutVersion"`
	Lng           string     `json:"lng"`
	Slug          string     `json:"slug"`
	Name          *string    `json:"name"`
	Headline      *string    `json:"headline"`
	Photo         *CardPhoto `json:"photo"`
	Accent        string     `json:"accent"`
}

type CardPhoto struct {
	URL  string                        `json:"url"`
	Crop *publicresume.PublicPhotoCrop `json:"crop"`
}

type CardInput struct {
	LayoutVersion int
	Lng           string
	Slug          string
	Name          string
	Headline      string
	HasPhoto      bool
	Crop          *publicresume.PublicPhotoCrop
	Accent        string
}

func CardLanguage(lng string) string { return projectLanguage(&lng) }

func NewCardEnvelope(resumeID uuid.UUID, input CardInput, photo []byte, contentType string) (CardEnvelope, error) {
	photoURL, err := inlinePhoto(input.HasPhoto, photo, contentType)
	if err != nil {
		return CardEnvelope{}, err
	}
	content := CardContent{
		LayoutVersion: input.LayoutVersion, Lng: input.Lng, Slug: input.Slug, Accent: input.Accent,
	}
	if input.Name != "" {
		name := input.Name
		content.Name = &name
		if input.Headline != "" {
			headline := input.Headline
			content.Headline = &headline
		}
	}
	if input.HasPhoto {
		content.Photo = &CardPhoto{URL: photoURL}
		if input.Crop != nil {
			crop := *input.Crop
			content.Photo.Crop = &crop
		}
	}
	envelope := CardEnvelope{Version: 1, Kind: CardKind, ResumeID: resumeID.String(), Card: content}
	if _, err := MarshalCard(envelope); err != nil {
		return CardEnvelope{}, err
	}
	return envelope, nil
}

func MarshalCard(source CardEnvelope) ([]byte, error) {
	card := source.Card
	if source.Version != 1 || source.Kind != CardKind || !canonicalUUID(source.ResumeID) ||
		card.LayoutVersion < 1 || !canonicalLanguage(card.Lng) || !publicroots.ValidSlug(card.Slug) ||
		!cardAccentPattern.MatchString(card.Accent) || !validCardText(card.Name) || !validCardText(card.Headline) ||
		(card.Name == nil && card.Headline != nil) {
		return nil, errInvalid
	}
	if card.Photo != nil {
		photo := &publicresume.PublicPhoto{URL: card.Photo.URL, Crop: card.Photo.Crop}
		if err := validateCrop(photo); err != nil {
			return nil, err
		}
		if err := validateDataPhoto(photo); err != nil {
			return nil, err
		}
	}
	encoded, err := json.Marshal(source)
	if err != nil || len(encoded) > MaxEnvelopeBytes {
		return nil, errInvalid
	}
	return encoded, nil
}

func validCardText(value *string) bool {
	if value == nil {
		return true
	}
	text := *value
	if text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > MaxCardTextCharacters {
		return false
	}
	first, _ := utf8.DecodeRuneInString(text)
	last, _ := utf8.DecodeLastRuneInString(text)
	if unicode.IsSpace(first) || unicode.IsSpace(last) {
		return false
	}
	for _, character := range text {
		if unicode.Is(unicode.Cc, character) {
			return false
		}
	}
	return true
}
