package showcase

import (
	"testing"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/previewcard"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

const (
	photoHexOne = "0123456789abcdef"
	photoHexTwo = "fedcba9876543210"
)

func baseCard() previewcard.Card {
	return previewcard.Card{
		LayoutVersion: previewcard.LayoutVersion, Slug: "ada-lovelace", Lng: "en", Name: "Ada Lovelace",
		Headline: "Engineer", PhotoKeyDigest: photoHexOne, Accent: "#1d4ed8",
		Crop: &publicresume.PublicPhotoCrop{X: 0.1, Y: 0.2, Width: 0.5, Height: 0.6},
	}
}

func mustKey(t *testing.T, card previewcard.Card) string {
	t.Helper()
	key, err := ReviewKey(card)
	if err != nil {
		t.Fatalf("ReviewKey() error = %v", err)
	}
	if !ValidKey(key) {
		t.Fatalf("ReviewKey() = %q, want 16 lowercase hex digits", key)
	}
	return key
}

// AC-SHOW-006: the key leaves out the card layout version and the accent, so a
// layout release or a color change never sends a listing back to review, and
// covers the slug, language, name, headline, photo, and crop.
func TestReviewKeyStableAcrossAccentAndLayoutVersion(t *testing.T) {
	t.Parallel()
	base := baseCard()
	want := mustKey(t, base)
	moved := base
	moved.Accent = "#be123c"
	moved.LayoutVersion = base.LayoutVersion + 7
	if got := mustKey(t, moved); got != want {
		t.Fatalf("key after an accent and layout change = %s, want %s", got, want)
	}
	if again := mustKey(t, base); again != want {
		t.Fatalf("key is not deterministic: %s then %s", want, again)
	}
}

func TestReviewKeyChangesWithEveryReviewedInput(t *testing.T) {
	t.Parallel()
	base := baseCard()
	want := mustKey(t, base)
	changes := map[string]func(*previewcard.Card){
		"slug":           func(c *previewcard.Card) { c.Slug = "ada-lovelace-2" },
		"language":       func(c *previewcard.Card) { c.Lng = "vi" },
		"name":           func(c *previewcard.Card) { c.Name = "Grace Hopper" },
		"headline":       func(c *previewcard.Card) { c.Headline = "Admiral" },
		"empty headline": func(c *previewcard.Card) { c.Headline = "" },
		"photo":          func(c *previewcard.Card) { c.PhotoKeyDigest = photoHexTwo },
		"no photo":       func(c *previewcard.Card) { c.PhotoKeyDigest, c.Crop = "", nil },
		"crop x": func(c *previewcard.Card) {
			c.Crop = &publicresume.PublicPhotoCrop{X: 0.11, Y: 0.2, Width: 0.5, Height: 0.6}
		},
		"crop height": func(c *previewcard.Card) {
			c.Crop = &publicresume.PublicPhotoCrop{X: 0.1, Y: 0.2, Width: 0.5, Height: 0.7}
		},
		"crop removed":    func(c *previewcard.Card) { c.Crop = nil },
		"name and slug":   func(c *previewcard.Card) { c.Name, c.Slug = "Ada", "ada" },
		"swapped content": func(c *previewcard.Card) { c.Name, c.Headline = c.Headline, c.Name },
	}
	for name, change := range changes {
		card := base
		change(&card)
		if got := mustKey(t, card); got == want {
			t.Errorf("a changed %s left the key at %s", name, want)
		}
	}
}

// AC-SHOW-006: Derive reads the same card inputs as the preview card builder,
// so the card version it stores is the card builder's.
func TestDeriveFollowsTheCardBuilder(t *testing.T) {
	t.Parallel()
	spec := defaultSpec()
	spec.photoKey = "resumes/photo-key.png"
	spec.crop = &schema.PhotoCrop{X: 0.1, Y: 0.1, Width: 0.5, Height: 0.5}
	snapshot := snapshotOf(t, spec)

	derived, err := Derive(snapshot)
	if err != nil {
		t.Fatalf("Derive() error = %v", err)
	}
	card, err := previewcard.FromSnapshot(snapshot)
	if err != nil {
		t.Fatalf("FromSnapshot() error = %v", err)
	}
	version, err := card.Version()
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}
	built, err := previewcard.VersionOf(snapshot)
	if err != nil {
		t.Fatalf("VersionOf() error = %v", err)
	}
	if derived.CardVersion != version || derived.CardVersion != built {
		t.Fatalf("card version = %s, builder = %s and %s", derived.CardVersion, version, built)
	}
	if derived.ReviewKey != mustKey(t, card) {
		t.Fatalf("review key = %s, want %s from the card", derived.ReviewKey, mustKey(t, card))
	}
	if derived.TemplateID != nil {
		t.Fatalf("template = %q, want nil for an unstyled document", *derived.TemplateID)
	}
}

func TestDeriveKeyStableAcrossAccentButNotCardVersion(t *testing.T) {
	t.Parallel()
	spec := defaultSpec()
	before, err := Derive(snapshotOf(t, spec))
	if err != nil {
		t.Fatalf("Derive() error = %v", err)
	}
	spec.customization.Colors.Primary = "#be123c"
	after, err := Derive(snapshotOf(t, spec))
	if err != nil {
		t.Fatalf("Derive() error = %v", err)
	}
	if after.ReviewKey != before.ReviewKey {
		t.Fatalf("a color change moved the review key %s to %s", before.ReviewKey, after.ReviewKey)
	}
	if after.CardVersion == before.CardVersion {
		t.Fatal("a color change left the card version unchanged")
	}
}

func TestDeriveKeyChangesWithTheReviewedFields(t *testing.T) {
	t.Parallel()
	spec := defaultSpec()
	base, err := Derive(snapshotOf(t, spec))
	if err != nil {
		t.Fatalf("Derive() error = %v", err)
	}
	changes := map[string]func(*rowSpec){
		"slug":     func(s *rowSpec) { s.slug = "grace-hopper-1" },
		"language": func(s *rowSpec) { s.lng = "vi" },
		"name":     func(s *rowSpec) { s.name = "Grace Hopper" },
		"headline": func(s *rowSpec) { s.headline = "Admiral" },
		"photo":    func(s *rowSpec) { s.photoKey = "resumes/other.png" },
		"crop": func(s *rowSpec) {
			s.photoKey, s.crop = "resumes/other.png", &schema.PhotoCrop{X: 0.3, Y: 0.3, Width: 0.4, Height: 0.4}
		},
	}
	for name, change := range changes {
		changed := defaultSpec()
		changed.slug = spec.slug
		change(&changed)
		got, deriveErr := Derive(snapshotOf(t, changed))
		if deriveErr != nil {
			t.Fatalf("%s: Derive() error = %v", name, deriveErr)
		}
		if got.ReviewKey == base.ReviewKey {
			t.Errorf("a changed %s left the review key at %s", name, base.ReviewKey)
		}
	}
}

// AC-SHOW-004: Derive reports the preset an applied template matches.
func TestDeriveReportsThePresetTemplate(t *testing.T) {
	t.Parallel()
	files := readTemplateFiles(t)
	spec := defaultSpec()
	spec.customization = appliedCustomization(t, files[3], []string{}, []string{})
	derived, err := Derive(snapshotOf(t, spec))
	if err != nil {
		t.Fatalf("Derive() error = %v", err)
	}
	if derived.TemplateID == nil || *derived.TemplateID != files[3].ID {
		t.Fatalf("template = %v, want %s", derived.TemplateID, files[3].ID)
	}
}
