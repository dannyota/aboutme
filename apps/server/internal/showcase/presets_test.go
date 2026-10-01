package showcase

import (
	"encoding/json"
	"regexp"
	"testing"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

var presetIDPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func objectAt(t *testing.T, parent map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := parent[key].(map[string]any)
	if !ok {
		t.Fatalf("customization lacks the object %q", key)
	}
	return value
}

// expectedMatchForm derives a preset's showcase match form straight from its
// template file, independently of the generator.
func expectedMatchForm(t *testing.T, file templateFile) string {
	t.Helper()
	raw, err := json.Marshal(file.Customization)
	if err != nil {
		t.Fatalf("encode %s: %v", file.ID, err)
	}
	var customization map[string]any
	if err = json.Unmarshal(raw, &customization); err != nil {
		t.Fatalf("decode %s: %v", file.ID, err)
	}
	for _, leaf := range []string{"pageFormat", "dateFormat", "colorScheme"} {
		delete(customization, leaf)
	}
	delete(objectAt(t, customization, "font"), "textAlign")
	delete(objectAt(t, customization, "header"), "photoPosition")
	layout := objectAt(t, customization, "layout")
	delete(layout, "placement")
	delete(layout, "sidebarSectionTypes")
	delete(layout, "sections")
	form, err := encodeCanonical(customization)
	if err != nil {
		t.Fatalf("canonical %s: %v", file.ID, err)
	}
	return form
}

// AC-SHOW-004: the generated table matches packages/schema/templates, preset
// for preset, so a template edit without a regeneration fails here.
func TestPresetTableMatchesTemplateFiles(t *testing.T) {
	t.Parallel()
	files := readTemplateFiles(t)
	if len(Presets) != len(files) {
		t.Fatalf("preset table holds %d presets, templates hold %d; run `make schema-gen`", len(Presets), len(files))
	}
	for index, file := range files {
		preset := Presets[index]
		if preset.ID != file.ID {
			t.Fatalf("preset %d is %q, want %q; run `make schema-gen`", index, preset.ID, file.ID)
		}
		got, err := canonicalForm([]byte(preset.Match))
		if err != nil {
			t.Fatalf("preset %s does not decode: %v", preset.ID, err)
		}
		if want := expectedMatchForm(t, file); got != want {
			t.Errorf("preset %s is stale; run `make schema-gen`\n got %s\nwant %s", preset.ID, got, want)
		}
		if !presetIDPattern.MatchString(preset.ID) || len(preset.ID) > 64 {
			t.Errorf("preset ID %q does not fit the template_id column", preset.ID)
		}
	}
	if len(Presets) != 20 {
		t.Errorf("preset table holds %d presets, want the 20 shipped templates", len(Presets))
	}
}

// AC-SHOW-004: no two presets share a match form, so every preset derives
// itself.
func TestPresetMatchFormsAreDistinct(t *testing.T) {
	t.Parallel()
	seen := map[string]string{}
	for _, preset := range Presets {
		form, err := canonicalForm([]byte(preset.Match))
		if err != nil {
			t.Fatalf("preset %s does not decode: %v", preset.ID, err)
		}
		if other, taken := seen[form]; taken {
			t.Errorf("presets %s and %s share one match form", other, preset.ID)
		}
		seen[form] = preset.ID
	}
}

func mustTemplateID(t *testing.T, customization schema.Customization) *string {
	t.Helper()
	id, err := TemplateID(customization)
	if err != nil {
		t.Fatalf("TemplateID() error = %v", err)
	}
	return id
}

// AC-SHOW-004: an applied preset derives its own ID, whatever section order
// the resume holds.
func TestTemplateIDMatchesEveryPreset(t *testing.T) {
	t.Parallel()
	for _, file := range readTemplateFiles(t) {
		for _, layout := range [][2][]string{
			{{}, {}},
			{{"work", "education"}, {"skills"}},
			{{"profile"}, {"language", "certificate"}},
		} {
			got := mustTemplateID(t, appliedCustomization(t, file, layout[0], layout[1]))
			if got == nil || *got != file.ID {
				t.Errorf("preset %s with sections %v derived %v", file.ID, layout, got)
			}
		}
	}
}

// AC-SHOW-004: the five leaves a template apply keeps or resets for the owner
// never change the match.
func TestTemplateIDIgnoresTheOwnerLeaves(t *testing.T) {
	t.Parallel()
	dark, justify, right := schema.Dark, schema.Justify, schema.Right
	variations := map[string]func(*schema.Customization){
		"page format":    func(c *schema.Customization) { c.PageFormat = schema.Letter },
		"date format":    func(c *schema.Customization) { c.DateFormat = schema.Yyyy },
		"color scheme":   func(c *schema.Customization) { c.ColorScheme = &dark },
		"text align":     func(c *schema.Customization) { c.Font.TextAlign = &justify },
		"photo position": func(c *schema.Customization) { c.Header.PhotoPosition = &right },
	}
	for _, file := range readTemplateFiles(t) {
		for name, vary := range variations {
			customization := appliedCustomization(t, file, []string{"work"}, []string{})
			if customization.Header == nil {
				t.Fatalf("preset %s has no header", file.ID)
			}
			vary(&customization)
			got := mustTemplateID(t, customization)
			if got == nil || *got != file.ID {
				t.Errorf("preset %s with a different %s derived %v, want %s", file.ID, name, got, file.ID)
			}
		}
	}
}

// AC-SHOW-004: one changed token is "Custom design".
func TestTemplateIDOneChangedTokenGivesCustom(t *testing.T) {
	t.Parallel()
	accent, otherSurface := "#abcdef", "#0a0b0c"
	surfaceTarget := schema.SurfaceTarget("experimental")
	changes := map[string]func(*schema.Customization){
		"base size":       func(c *schema.Customization) { c.Font.BaseSizePx++ },
		"primary color":   func(c *schema.Customization) { c.Colors.Primary = "#010203" },
		"background":      func(c *schema.Customization) { c.Colors.Background = "#fefefe" },
		"section gap":     func(c *schema.Customization) { c.Spacing.SectionGap++ },
		"line height":     func(c *schema.Customization) { c.Spacing.LineHeight += 0.05 },
		"heading rule":    func(c *schema.Customization) { c.Heading.ShowRule = !c.Heading.ShowRule },
		"column count":    func(c *schema.Customization) { c.Layout.Columns = 3 - c.Layout.Columns },
		"header icons":    func(c *schema.Customization) { c.Header.IconStyle = "experimental" },
		"header removed":  func(c *schema.Customization) { c.Header = nil },
		"accent added":    func(c *schema.Customization) { c.Colors.Accent = &accent },
		"surface target":  func(c *schema.Customization) { c.Layout.SurfaceTarget = &surfaceTarget },
		"page margin":     func(c *schema.Customization) { c.Spacing.PageMargin = &schema.PageMargin{X: 1.5, Y: 2.5} },
		"skill display":   func(c *schema.Customization) { c.SectionDisplay.Skill.Style = "experimental" },
		"language style":  func(c *schema.Customization) { c.SectionDisplay.Language.Style = "experimental" },
		"font family":     func(c *schema.Customization) { c.Font.Family = "experimental" },
		"heading style":   func(c *schema.Customization) { c.Heading.Style = "experimental" },
		"header align":    func(c *schema.Customization) { c.Header.Align = "experimental" },
		"details layout":  func(c *schema.Customization) { c.Header.DetailsLayout = "experimental" },
		"entry gap":       func(c *schema.Customization) { c.Spacing.EntryGap++ },
		"text color":      func(c *schema.Customization) { c.Colors.Text = "#020304" },
		"sidebar surface": func(c *schema.Customization) { c.Colors.Surface = &otherSurface },
	}
	for _, file := range readTemplateFiles(t) {
		for name, change := range changes {
			customization := appliedCustomization(t, file, []string{"work"}, []string{})
			if customization.Header == nil {
				t.Fatalf("preset %s has no header", file.ID)
			}
			change(&customization)
			if got := mustTemplateID(t, customization); got != nil && *got == file.ID {
				t.Errorf("preset %s with a changed %s still derived %s", file.ID, name, file.ID)
			}
		}
	}
	custom := appliedCustomization(t, readTemplateFiles(t)[0], []string{}, []string{})
	custom.Colors.Primary = "#010203"
	if got := mustTemplateID(t, custom); got != nil {
		t.Fatalf("a changed token derived %q, want nil (Custom design)", *got)
	}
}

// AC-SHOW-004: the default document, which no template produced, is custom.
func TestTemplateIDOfAnUnstyledDocumentIsCustom(t *testing.T) {
	t.Parallel()
	if got := mustTemplateID(t, baseCustomization()); got != nil {
		t.Fatalf("TemplateID() = %q, want nil", *got)
	}
}
