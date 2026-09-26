package previewcard

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/printsnapshot"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

// Values come from the resume renderer's clampAgainst in
// apps/web/app/components/resume/clampContrast.ts against white at 3:1.
func TestAccentClampsLikeTheResumeRenderer(t *testing.T) {
	t.Parallel()
	for source, want := range map[string]string{
		"#1d4ed8": "#1d4ed8", "#ffff00": "#9d9900", "#FFFF00": "#9d9900", "#aaaaaa": "#949494",
		"#00ff00": "#00ac00", "#ffd700": "#b68e00", "#ffffff": "#949494", "#0ea5e9": "#009de1",
		"#f59e0b": "#d58000", "#767676": "#767676",
	} {
		color := source
		if got, err := Accent(schema.Colors{Accent: &color, Primary: "#000000"}); err != nil || got != want {
			t.Errorf("Accent(%s) = %q, %v, want %q", source, got, err, want)
		}
	}
	if got, err := Accent(schema.Colors{Primary: "#FFD700"}); err != nil || got != "#b68e00" {
		t.Errorf("Accent(primary only) = %q, %v, want the clamped primary", got, err)
	}
	for _, invalid := range []string{"", "#fff", "red", "#gggggg", "#1d4ed8ff"} {
		if _, err := Accent(schema.Colors{Primary: invalid}); err == nil {
			t.Errorf("Accent(%q) accepted an invalid color", invalid)
		}
	}
}

func TestVersionIsStableAndCoversEveryInput(t *testing.T) {
	t.Parallel()
	base := Card{
		LayoutVersion: 1, Slug: "nguyen-van-a", Lng: "vi", Name: "Nguyễn Văn A", Headline: "Kỹ sư",
		PhotoKeyDigest: strings.Repeat("a", 64), Crop: &publicresume.PublicPhotoCrop{X: 0.1, Y: 0.1, Width: 0.5, Height: 0.5},
		Accent: "#1d4ed8",
	}
	version, err := base.Version()
	if err != nil || !ValidVersion(version) {
		t.Fatalf("Version() = %q, %v", version, err)
	}
	again, err := base.Version()
	if err != nil || again != version {
		t.Fatalf("Version() is not stable: %q then %q", version, again)
	}
	mutations := map[string]func(*Card){
		"layout":   func(c *Card) { c.LayoutVersion = 2 },
		"slug":     func(c *Card) { c.Slug = "nguyen-van-b" },
		"language": func(c *Card) { c.Lng = "en" },
		"name":     func(c *Card) { c.Name = "Nguyễn Văn B" },
		"headline": func(c *Card) { c.Headline = "" },
		"photo":    func(c *Card) { c.PhotoKeyDigest = strings.Repeat("b", 64) },
		"no photo": func(c *Card) { c.PhotoKeyDigest, c.Crop = "", nil },
		"crop":     func(c *Card) { c.Crop = &publicresume.PublicPhotoCrop{X: 0.2, Y: 0.1, Width: 0.5, Height: 0.5} },
		"accent":   func(c *Card) { c.Accent = "#000000" },
	}
	for name, mutate := range mutations {
		changed := base
		mutate(&changed)
		if got, err := changed.Version(); err != nil || got == version {
			t.Errorf("%s change kept version %q (%v)", name, got, err)
		}
	}
}

func TestPathAndVersionForm(t *testing.T) {
	t.Parallel()
	if got := Path("ada-lovelace", "0123456789abcdef"); got != "/api/v1/public/resumes/ada-lovelace/og/0123456789abcdef.png" {
		t.Fatalf("Path() = %q", got)
	}
	for version, want := range map[string]bool{
		"0123456789abcdef": true, "0123456789ABCDEF": false, "0123456789abcde": false, "0123456789abcdef0": false, "": false,
	} {
		if ValidVersion(version) != want {
			t.Errorf("ValidVersion(%q) = %v, want %v", version, !want, want)
		}
	}
}

// Sentinels sit in every contact type and in hidden content. The card
// inputs and the envelope Go sends must contain none of them.
func TestCardEnvelopeCarriesNoContactData(t *testing.T) {
	t.Parallel()
	contacts := []schema.PersonalDetail{
		{ID: "e", Type: schema.Email, Value: "sentinel-email@example.com"},
		{ID: "p", Type: schema.Phone, Value: "+84 900 000 111"},
		{ID: "l", Type: schema.Location, Value: "SENTINEL-LOCATION"},
		{ID: "c", Type: schema.TypeCustom, Value: "SENTINEL-CUSTOM"},
		{ID: "h", Type: schema.TypeCustom, Value: "SENTINEL-HIDDEN", IsHidden: true},
	}
	snapshot := testSnapshot(t, "Ada Lovelace", "Engineer", contacts)
	card, err := FromSnapshot(snapshot)
	if err != nil {
		t.Fatalf("FromSnapshot() error = %v", err)
	}
	envelope, err := printsnapshot.NewCardEnvelope(uuid.New(), card.Input(), nil, "")
	if err != nil {
		t.Fatalf("NewCardEnvelope() error = %v", err)
	}
	encoded, err := printsnapshot.MarshalCard(envelope)
	if err != nil {
		t.Fatal(err)
	}
	inputs, err := json.Marshal(card)
	if err != nil {
		t.Fatal(err)
	}
	for _, sentinel := range []string{"sentinel-email", "900 000 111", "SENTINEL-LOCATION", "SENTINEL-CUSTOM", "SENTINEL-HIDDEN"} {
		if strings.Contains(string(encoded), sentinel) || strings.Contains(string(inputs), sentinel) {
			t.Fatalf("card data leaks %s: %s", sentinel, encoded)
		}
	}
	if card.Name != "Ada Lovelace" || card.Headline != "Engineer" || card.Lng != "en" || card.Slug != "ada-lovelace" {
		t.Fatalf("card = %+v", card)
	}
}

func TestNameWithContactDataIsLeftOff(t *testing.T) {
	t.Parallel()
	contacts := []schema.PersonalDetail{{ID: "e", Type: schema.Email, Value: "ada@example.com"}}
	card, err := FromSnapshot(testSnapshot(t, "Ada ada@example.com", "Engineer", contacts))
	if err != nil {
		t.Fatal(err)
	}
	if card.Name != "" || card.Headline != "" {
		t.Fatalf("card name, headline = %q, %q, want both left off", card.Name, card.Headline)
	}
}
