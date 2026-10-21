package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

func TestExportFixturesMatchesBenchmarkCorpusWithoutRuntime(t *testing.T) {
	t.Parallel()
	root := testRepositoryRoot(t)
	corpus, err := buildFixtureCorpus(root)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "corpus")
	settings, err := parseConfig([]string{"--repository-root", root, "--export-fixtures", output})
	if err != nil {
		t.Fatal(err)
	}
	// Invalid benchmark settings make an accidental fallthrough fail before measurement.
	settings.BrowserExecutable = "/missing-browser"
	settings.OutputDirectory = "/invalid-benchmark-output"
	settings.Probe = true
	if runErr := run(t.Context(), settings); runErr != nil {
		t.Fatal(runErr)
	}
	directoryInfo, err := os.Stat(output)
	if err != nil || !directoryInfo.IsDir() || directoryInfo.Mode().Perm() != 0o700 {
		t.Fatal("export directory is not private")
	}
	entries, err := os.ReadDir(output)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
		info, statErr := entry.Info()
		if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			t.Fatalf("exported file is not private and regular: %s", entry.Name())
		}
	}
	if !reflect.DeepEqual(names, []string{"full-photo.png", "full.json", "manifest.json", "maximum.json", "minimal.json"}) {
		t.Fatalf("export files = %v", names)
	}
	var manifest fixtureExportManifest
	if err := json.Unmarshal(readExportFile(t, output, "manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != int(schema.CurrentVersion) || len(manifest.Fixtures) != 3 {
		t.Fatalf("manifest schema/count = %d/%d", manifest.SchemaVersion, len(manifest.Fixtures))
	}
	for index, item := range corpus {
		row := manifest.Fixtures[index]
		data := readExportFile(t, output, item.Name+".json")
		if !bytes.Equal(data, item.Canonical) {
			t.Fatalf("%s differs from benchmark canonical bytes", item.Name)
		}
		digest := sha256.Sum256(data)
		if row.Name != item.Name || row.File != item.Name+".json" || row.SHA256 != hex.EncodeToString(digest[:]) || row.CanonicalBytes != len(data) {
			t.Fatalf("%s manifest digest or size differs", item.Name)
		}
		if row.SectionCount != item.Stats.SectionCount || row.EntryCount != item.Stats.EntryCount || row.HiddenEntryCount != item.Stats.HiddenEntryCount || row.LayoutSectionCount != item.Stats.LayoutSectionCount || row.LargestRichTextBytes != item.Stats.LargestRichTextBytes || row.PageFormat != item.Stats.PageFormat || row.FontFamily != item.Stats.FontFamily || row.InlinePhotoBytes != len(item.Photo) {
			t.Fatalf("%s manifest statistics differ", item.Name)
		}
		if item.Name == "full" {
			photo := readExportFile(t, output, "full-photo.png")
			photoDigest := sha256.Sum256(photo)
			if !bytes.Equal(photo, item.Photo) || row.Photo == nil || row.Photo.File != "full-photo.png" || row.Photo.Bytes != len(photo) || row.Photo.SHA256 != hex.EncodeToString(photoDigest[:]) || row.Photo.ContentType != "image/png" {
				t.Fatal("full normalized photo differs")
			}
		} else if row.Photo != nil {
			t.Fatalf("%s has unexpected photo", item.Name)
		}
	}
	maximum := manifest.Fixtures[2]
	if maximum.CanonicalBytes != 524288 || maximum.SectionCount != 24 || maximum.EntryCount != 1536 || maximum.HiddenEntryCount != 0 || maximum.LayoutSectionCount != 24 || maximum.LargestRichTextBytes != 16384 {
		t.Fatal("maximum corpus contract differs")
	}
	second := filepath.Join(t.TempDir(), "corpus")
	settings.ExportFixtures = second
	if runErr := run(t.Context(), settings); runErr != nil {
		t.Fatal(runErr)
	}
	for _, name := range names {
		if !bytes.Equal(readExportFile(t, output, name), readExportFile(t, second, name)) {
			t.Fatalf("%s is not deterministic", name)
		}
	}
}

func TestExportConfigRejectsBenchmarkFlagsAndUnsafePaths(t *testing.T) {
	t.Parallel()
	base := []string{"--repository-root", testRepositoryRoot(t)}
	output := filepath.Join(t.TempDir(), "corpus")
	for _, args := range [][]string{
		{"--export-fixtures", ""},
		{"--export-fixtures", "relative"},
		{"--export-fixtures", "/"},
		{"--export-fixtures", output, "--probe=false"},
		{"--export-fixtures", output, "--chromium-executable", "/browser"},
		{"--export-fixtures", output, "--output-directory", "/output"},
	} {
		if _, err := parseConfig(append(append([]string{}, base...), args...)); err == nil {
			t.Fatalf("accepted arguments %v", args)
		}
	}
}

func TestExportDirectoryRefusesOverwriteAndSymlinks(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	existing := filepath.Join(parent, "existing")
	if err := os.Mkdir(existing, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(existing, "minimal.json")
	if err := os.WriteFile(marker, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(existing, link); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(parent, "empty")
	if err := os.Mkdir(empty, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{existing, empty, link, filepath.Join(link, "nested"), filepath.Join(parent, "missing", "nested")} {
		if _, err := createExportDirectory(output); err == nil {
			t.Fatalf("accepted unsafe output %s", output)
		}
	}
	if got := string(readExportFile(t, existing, "minimal.json")); got != "preserve" {
		t.Fatal("existing data overwritten")
	}
	if _, err := os.Stat(filepath.Join(existing, "nested")); !os.IsNotExist(err) {
		t.Fatal("export followed parent symlink")
	}
}

func readExportFile(t *testing.T, directory, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(directory, name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
