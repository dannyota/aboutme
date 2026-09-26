package printsnapshot

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

func validCardInput() CardInput {
	return CardInput{
		LayoutVersion: 1, Lng: "vi", Slug: "nguyen-van-a", Name: "Nguyễn Văn A", Headline: "Kỹ sư phần mềm",
		HasPhoto: true, Crop: &publicresume.PublicPhotoCrop{X: 0.1, Y: 0.2, Width: 0.5, Height: 0.5}, Accent: "#1d4ed8",
	}
}

// The card envelope is closed: exactly these keys at each level, so no
// contact, section, or document field can reach the card page.
func TestCardEnvelopeHasExactlyTheClosedKeys(t *testing.T) {
	t.Parallel()
	resumeID := uuid.MustParse("20000000-0000-4000-8000-000000000001")
	envelope, err := NewCardEnvelope(resumeID, validCardInput(), testPNG(t), "image/png")
	if err != nil {
		t.Fatalf("NewCardEnvelope() error = %v", err)
	}
	encoded, err := MarshalCard(envelope)
	if err != nil {
		t.Fatalf("MarshalCard() error = %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if got := sortedKeys(decoded); !reflect.DeepEqual(got, []string{"card", "kind", "resumeId", "version"}) {
		t.Fatalf("envelope keys = %v", got)
	}
	var card map[string]json.RawMessage
	if err := json.Unmarshal(decoded["card"], &card); err != nil {
		t.Fatal(err)
	}
	if got := sortedKeys(card); !reflect.DeepEqual(got, []string{"accent", "headline", "layoutVersion", "lng", "name", "photo", "slug"}) {
		t.Fatalf("card keys = %v", got)
	}
	var photo map[string]json.RawMessage
	if err := json.Unmarshal(card["photo"], &photo); err != nil {
		t.Fatal(err)
	}
	if got := sortedKeys(photo); !reflect.DeepEqual(got, []string{"crop", "url"}) {
		t.Fatalf("photo keys = %v", got)
	}
	if string(decoded["kind"]) != `"card"` || string(decoded["resumeId"]) != `"`+resumeID.String()+`"` ||
		!strings.HasPrefix(envelope.Card.Photo.URL, "data:image/png;base64,") {
		t.Fatalf("envelope = %s", encoded)
	}
}

func TestCardEnvelopeLeavesOffAbsentFieldsAsNull(t *testing.T) {
	t.Parallel()
	input := validCardInput()
	input.Name, input.Headline, input.HasPhoto, input.Crop = "", "Kỹ sư", false, nil
	envelope, err := NewCardEnvelope(uuid.New(), input, nil, "")
	if err != nil {
		t.Fatalf("NewCardEnvelope() error = %v", err)
	}
	encoded, err := MarshalCard(envelope)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"name":null`, `"headline":null`, `"photo":null`} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("envelope %s lacks %s", encoded, want)
		}
	}

	input = validCardInput()
	input.Crop = nil
	envelope, err = NewCardEnvelope(uuid.New(), input, testJPEG(t), "image/jpeg")
	if err != nil {
		t.Fatalf("NewCardEnvelope(no crop) error = %v", err)
	}
	if encoded, err = MarshalCard(envelope); err != nil || !strings.Contains(string(encoded), `"crop":null`) {
		t.Fatalf("envelope without crop = %s, %v", encoded, err)
	}
}

func TestCardEnvelopeRejectsInvalidContent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*CardEnvelope)
	}{
		{"version", func(e *CardEnvelope) { e.Version = 2 }},
		{"kind", func(e *CardEnvelope) { e.Kind = "resume" }},
		{"resume id", func(e *CardEnvelope) { e.ResumeID = uuid.Nil.String() }},
		{"layout version", func(e *CardEnvelope) { e.Card.LayoutVersion = 0 }},
		{"language", func(e *CardEnvelope) { e.Card.Lng = "VI" }},
		{"slug", func(e *CardEnvelope) { e.Card.Slug = "app" }},
		{"uppercase accent", func(e *CardEnvelope) { e.Card.Accent = "#1D4ED8" }},
		{"accent", func(e *CardEnvelope) { e.Card.Accent = "blue" }},
		{"empty name", func(e *CardEnvelope) { e.Card.Name = stringPointer("") }},
		{"padded name", func(e *CardEnvelope) { e.Card.Name = stringPointer(" Ada") }},
		{"control name", func(e *CardEnvelope) { e.Card.Name = stringPointer("Ada\nLovelace") }},
		{"long headline", func(e *CardEnvelope) { e.Card.Headline = stringPointer(strings.Repeat("ạ", 161)) }},
		{"headline without name", func(e *CardEnvelope) { e.Card.Name = nil }},
		{"remote photo", func(e *CardEnvelope) { e.Card.Photo.URL = "https://example.com/a.png" }},
		{"crop", func(e *CardEnvelope) { e.Card.Photo.Crop = &publicresume.PublicPhotoCrop{Width: 2, Height: 1} }},
	}
	valid, err := NewCardEnvelope(uuid.New(), validCardInput(), testPNG(t), "image/png")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			envelope := valid
			card := valid.Card
			photo := *valid.Card.Photo
			card.Photo = &photo
			envelope.Card = card
			test.mutate(&envelope)
			if _, err := MarshalCard(envelope); err == nil {
				t.Fatalf("MarshalCard() accepted %s", test.name)
			}
		})
	}
	if _, err := NewCardEnvelope(uuid.New(), validCardInput(), nil, ""); err == nil {
		t.Fatal("NewCardEnvelope() accepted a photo flag without photo bytes")
	}
}

func TestCardLanguageMatchesThePrintProjection(t *testing.T) {
	t.Parallel()
	for input, want := range map[string]string{"vi": "vi", "en-GB": "en-GB", "": "und", "not a tag!": "und"} {
		if got := CardLanguage(input); got != want {
			t.Errorf("CardLanguage(%q) = %q, want %q", input, got, want)
		}
	}
}

func sortedKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
