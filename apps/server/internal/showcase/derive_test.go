package showcase

import (
	"testing"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/previewcard"
)

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
	if derived.TemplateID != nil {
		t.Fatalf("template = %q, want nil for an unstyled document", *derived.TemplateID)
	}
}

// AC-SHOW-006: a color change moves the card version, so a tile fetched before
// the edit stops matching, and nothing else about the row is stored.
func TestDeriveCardVersionFollowsTheAccent(t *testing.T) {
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
	if after.CardVersion == before.CardVersion {
		t.Fatal("a color change left the card version unchanged")
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
