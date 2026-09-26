package printsnapshot

import (
	"encoding/json"
	"regexp"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicroots"
)

const (
	// CardKind is the "kind" value that tells the print renderer to draw a
	// link-preview card instead of a resume document.
	CardKind = "card"
	// MaxCardTextCharacters bounds the card name and headline, the schema
	// bound of the fields they come from.
	MaxCardTextCharacters = 160
)

var cardAccentPattern = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// CardEnvelope is the closed payload redeemed by the print renderer for one
// link-preview card. It carries no contact, section, or document field, so
// no email, phone, or address can reach the image
// (docs/design/link-previews.md, "Preview card").
type CardEnvelope struct {
	Version  int         `json:"version"`
	Kind     string      `json:"kind"`
	ResumeID string      `json:"resumeId"`
	Card     CardContent `json:"card"`
}

// CardContent is everything the card shows. Name and Headline are nil when
// the card leaves them off; Headline is nil whenever Name is.
type CardContent struct {
	LayoutVersion int        `json:"layoutVersion"`
	Lng           string     `json:"lng"`
	Slug          string     `json:"slug"`
	Name          *string    `json:"name"`
	Headline      *string    `json:"headline"`
	Photo         *CardPhoto `json:"photo"`
	Accent        string     `json:"accent"`
}

// CardPhoto is the normalized photo as an inline data URL, and its crop or
// nil for the whole image.
type CardPhoto struct {
	URL  string                        `json:"url"`
	Crop *publicresume.PublicPhotoCrop `json:"crop"`
}

// CardInput is the card content before the photo bytes are inlined.
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

// CardLanguage is the canonical card language for a public resume language:
// the same projection the resume print envelope uses.
func CardLanguage(lng string) string { return projectLanguage(&lng) }

// NewCardEnvelope builds and validates one card envelope. photo and
// contentType are the normalized photo when input.HasPhoto is set, and empty
// otherwise.
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

// MarshalCard validates and emits the exact frozen JSON bytes digested by
// the render queue.
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

// validCardText accepts nil or normalized text: non-empty, at most
// MaxCardTextCharacters, with no control characters and no leading or
// trailing space.
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
