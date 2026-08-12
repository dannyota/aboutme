package resumeapi

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/sanitize"
)

func loadMinimalDocument(t *testing.T) schema.Resume {
	t.Helper()
	raw, err := os.ReadFile("../../../../packages/schema/fixtures/minimal.json")
	if err != nil {
		t.Fatalf("read minimal schema fixture: %v", err)
	}
	var doc schema.Resume
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode minimal schema fixture: %v", err)
	}
	return doc
}

// TestRichTextPaths_ExactlyMatchCurrentSchema makes a schema change break at
// the sanitizer boundary. The expected set is discovered from $defs whose
// entry properties directly reference richText, not copied from production.
func TestRichTextPaths_ExactlyMatchCurrentSchema(t *testing.T) {
	t.Parallel()

	var root map[string]any
	if err := json.Unmarshal(schema.RawSchema, &root); err != nil {
		t.Fatalf("decode embedded schema: %v", err)
	}
	defs, ok := root["$defs"].(map[string]any)
	if !ok {
		t.Fatal("embedded schema has no object $defs")
	}
	var discovered []string
	for definitionName, rawDefinition := range defs {
		if !strings.HasSuffix(definitionName, "Entry") {
			continue
		}
		definition, ok := rawDefinition.(map[string]any)
		if !ok {
			t.Fatalf("schema definition %q is not an object", definitionName)
		}
		for propertyName, rawProperty := range propertiesWithin(definition) {
			if !containsReference(rawProperty, "#/$defs/richText") {
				continue
			}
			sectionType := strings.TrimSuffix(definitionName, "Entry")
			sectionType = strings.ToLower(sectionType[:1]) + sectionType[1:]
			discovered = append(discovered, sectionType+"."+propertyName)
		}
	}
	sort.Strings(discovered)
	got := append([]string(nil), richTextPaths...)
	sort.Strings(got)
	if !reflect.DeepEqual(got, discovered) {
		t.Fatalf("richTextPaths = %v, schema declares %v", got, discovered)
	}
}

func propertiesWithin(value any) map[string]any {
	properties := make(map[string]any)
	var visit func(any)
	visit = func(current any) {
		switch current := current.(type) {
		case map[string]any:
			if rawProperties, ok := current["properties"].(map[string]any); ok {
				for name, property := range rawProperties {
					properties[name] = property
				}
			}
			for _, child := range current {
				visit(child)
			}
		case []any:
			for _, child := range current {
				visit(child)
			}
		}
	}
	visit(value)
	return properties
}

func containsReference(value any, reference string) bool {
	switch value := value.(type) {
	case map[string]any:
		if value["$ref"] == reference {
			return true
		}
		for _, child := range value {
			if containsReference(child, reference) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if containsReference(child, reference) {
				return true
			}
		}
	}
	return false
}

func TestSanitizeDocument_HostileBenignAbsentEmptyHiddenAndIdempotent(t *testing.T) {
	t.Parallel()

	hostile := `<script>alert(1)</script><p>safe</p>`
	benign := `<p><strong>kept</strong></p>`
	empty := ""
	hidden := true
	doc := loadMinimalDocument(t)
	doc.Content = map[string]schema.Section{
		"work": schema.NewWorkSection(nil, nil, []schema.WorkEntry{
			{ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60", Description: &hostile, IsHidden: &hidden},
			{ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61", Description: &benign},
			{ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e62", Description: &empty},
			{ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e63", Description: nil},
		}),
	}
	doc.Customization.Layout.Sections.Main = []string{"work"}
	doc.Customization.Layout.Sections.Sidebar = []string{}

	got := sanitizeDocument(doc)
	entries := got.Content["work"].WorkEntries
	if entries[0].Description == nil || *entries[0].Description != sanitize.RichText(hostile) {
		t.Fatalf("hostile rich text = %v, want sanitizer output %q", entries[0].Description, sanitize.RichText(hostile))
	}
	if entries[0].IsHidden == nil || !*entries[0].IsHidden {
		t.Fatal("hidden entry lost isHidden during sanitization")
	}
	if entries[1].Description == nil || *entries[1].Description != benign {
		t.Fatalf("benign rich text = %v, want unchanged %q", entries[1].Description, benign)
	}
	if entries[2].Description == nil || *entries[2].Description != "" {
		t.Fatalf("empty rich text = %v, want a present empty string", entries[2].Description)
	}
	if entries[3].Description != nil {
		t.Fatalf("absent rich text became present: %q", *entries[3].Description)
	}
	if twice := sanitizeDocument(got); !reflect.DeepEqual(twice, got) {
		t.Fatal("sanitizeDocument is not idempotent")
	}
}

func TestSanitizeDocument_AllSchemaRichTextFields(t *testing.T) {
	t.Parallel()

	hostile := `<script>alert(1)</script><p>safe</p>`
	want := sanitize.RichText(hostile)
	doc := loadMinimalDocument(t)
	customKey := "00000000-0000-0000-0000-000000000001"
	doc.Content = map[string]schema.Section{
		"profile":     schema.NewProfileSection(nil, nil, []schema.ProfileEntry{{ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60", Text: &hostile}}),
		"work":        schema.NewWorkSection(nil, nil, []schema.WorkEntry{{ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61", Description: &hostile}}),
		"education":   schema.NewEducationSection(nil, nil, []schema.EducationEntry{{ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e62", Description: &hostile}}),
		"skill":       schema.NewSkillSection(nil, nil, []schema.SkillEntry{{ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e63", InfoHTML: &hostile}}),
		"certificate": schema.NewCertificateSection(nil, nil, []schema.CertificateEntry{{ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e64", Description: &hostile}}),
		"project":     schema.NewProjectSection(nil, nil, []schema.ProjectEntry{{ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e65", Description: &hostile}}),
		customKey:     schema.NewCustomSection(nil, nil, []schema.CustomEntry{{ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e66", Description: &hostile}}),
	}

	got := sanitizeDocument(doc)
	values := map[string]*string{
		"profile.text":            got.Content["profile"].ProfileEntries[0].Text,
		"work.description":        got.Content["work"].WorkEntries[0].Description,
		"education.description":   got.Content["education"].EducationEntries[0].Description,
		"skill.infoHtml":          got.Content["skill"].SkillEntries[0].InfoHTML,
		"certificate.description": got.Content["certificate"].CertificateEntries[0].Description,
		"project.description":     got.Content["project"].ProjectEntries[0].Description,
		"custom.description":      got.Content[customKey].CustomEntries[0].Description,
	}
	for _, path := range richTextPaths {
		value := values[path]
		if value == nil || *value != want {
			t.Errorf("%s = %v, want %q", path, value, want)
		}
	}
}
