package publicformat

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The sitemap's template pages must match the presets the gallery serves.
func TestTemplateIDsMatchTheSchemaPresets(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "..", "packages", "schema", "templates", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	presets := make([]string, 0, len(paths))
	for _, path := range paths {
		if info, statErr := os.Stat(path); statErr != nil || !info.Mode().IsRegular() {
			t.Fatalf("preset %s is not a regular file: %v", path, statErr)
		}
		presets = append(presets, strings.TrimSuffix(filepath.Base(path), ".json"))
	}
	slices.Sort(presets)
	if !slices.Equal(templateIDs, presets) {
		t.Fatalf("templateIDs = %v, want the presets %v", templateIDs, presets)
	}
}
