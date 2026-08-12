// released_test.go is hand-written (scripts/generate.mjs never touches it,
// unlike the released.go it tests, and unlike everything under gen/go/v1).
// It is the Go half of AC-DOC-012's "released versions are immutable and
// retained" proof; the TypeScript half is test/released-versions.test.ts.
//
// What only this file can check: that the retained v1 package actually
// compiles and is importable from outside itself, that schema.RawSchema (the
// CURRENT contract the server validates against) is the same bytes as the
// released current version rather than a drifted copy, and that a version
// nobody has released fails closed instead of resolving to something
// plausible.
package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"

	schemav1 "github.com/dannyota/aboutme/packages/schema/gen/go/v1"
	schemav2 "github.com/dannyota/aboutme/packages/schema/gen/go/v2"
)

func TestReleasedVersions_ContainsExactlyVersionsOneAndTwo(t *testing.T) {
	got := ReleasedVersions()
	want := []int{1, 2}
	if len(got) != len(want) {
		t.Fatalf("ReleasedVersions() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ReleasedVersions() = %v, want %v", got, want)
		}
	}
	if CurrentVersion != 2 {
		t.Fatalf("CurrentVersion = %d, want 2", CurrentVersion)
	}
}

func TestWireVersionDeclarations_ReturnFreshSlices(t *testing.T) {
	for name, get := range map[string]func() []int{
		"accepted": AcceptedVersions,
		"emitted":  EmittedVersions,
	} {
		first := get()
		if !slices.Equal(first, []int{1, 2}) {
			t.Fatalf("%s versions = %v, want [1 2]", name, first)
		}
		first[0] = 99
		if got := get(); !slices.Equal(got, []int{1, 2}) {
			t.Fatalf("%s versions escaped by reference: %v", name, got)
		}
	}
}

func TestReleasedVersions_ReturnsAFreshSlice(t *testing.T) {
	first := ReleasedVersions()
	first[0] = 99
	if second := ReleasedVersions(); second[0] != 1 {
		t.Fatalf("ReleasedVersions()[0] = %d after a caller mutated an earlier result, want 1", second[0])
	}
}

func TestReleasedSchemaFor_V2ByteEqualsItsImmutableFile(t *testing.T) {
	got, err := ReleasedSchemaFor(2)
	if err != nil {
		t.Fatalf("ReleasedSchemaFor(2): %v", err)
	}
	path := filepath.Join("..", "..", got.Schema)
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if !bytes.Equal(got.RawSchema, want) || !bytes.Equal(schemav2.RawSchema, want) {
		t.Fatal("released v2 bytes differ from its immutable file")
	}
}

func TestReleasedSchemaFor_V1ByteEqualsItsImmutableFile(t *testing.T) {
	got, err := ReleasedSchemaFor(1)
	if err != nil {
		t.Fatalf("ReleasedSchemaFor(1): %v", err)
	}
	if got.Schema != "resume.v1.schema.json" {
		t.Fatalf("ReleasedSchemaFor(1).Schema = %q, want %q", got.Schema, "resume.v1.schema.json")
	}

	path := filepath.Join("..", "..", got.Schema)
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if !bytes.Equal(got.RawSchema, want) {
		t.Fatalf("ReleasedSchemaFor(1).RawSchema (%d bytes) does not byte-equal %s (%d bytes)",
			len(got.RawSchema), path, len(want))
	}
	if !bytes.Equal(schemav1.RawSchema, want) {
		t.Fatalf("schemav1.RawSchema (%d bytes) does not byte-equal %s (%d bytes)",
			len(schemav1.RawSchema), path, len(want))
	}
}

// RawSchema is what the server's validator compiles. If it ever stopped
// matching the current RELEASED version, the server would be enforcing a
// contract no released schema file describes -- exactly the drift the
// immutable-snapshot policy exists to prevent.
func TestRawSchema_DerivesFromTheCurrentReleasedVersion(t *testing.T) {
	current, err := ReleasedSchemaFor(CurrentVersion)
	if err != nil {
		t.Fatalf("ReleasedSchemaFor(CurrentVersion=%d): %v", CurrentVersion, err)
	}
	if !bytes.Equal(RawSchema, current.RawSchema) {
		t.Fatalf("RawSchema (%d bytes) does not byte-equal the v%d released schema (%d bytes)",
			len(RawSchema), CurrentVersion, len(current.RawSchema))
	}
}

func TestReleasedSchemaFor_UnknownVersionFailsClosed(t *testing.T) {
	for _, version := range []int{0, 3, -1, math.MinInt, math.MaxInt} {
		got, err := ReleasedSchemaFor(version)
		if err == nil {
			t.Fatalf("ReleasedSchemaFor(%d) returned %+v and no error; unknown versions must fail closed", version, got)
		}
		if !errors.Is(err, ErrUnknownSchemaVersion) {
			t.Fatalf("ReleasedSchemaFor(%d) error = %v, want it to wrap ErrUnknownSchemaVersion", version, err)
		}
		if got.RawSchema != nil || got.Version != 0 {
			t.Fatalf("ReleasedSchemaFor(%d) returned a non-zero value %+v alongside its error", version, got)
		}
	}
}

func TestReleasedSchemaFor_RawSchemaIsADefensiveCopy(t *testing.T) {
	first, err := ReleasedSchemaFor(1)
	if err != nil {
		t.Fatalf("ReleasedSchemaFor(1): %v", err)
	}
	if len(first.RawSchema) == 0 {
		t.Fatal("ReleasedSchemaFor(1).RawSchema is empty")
	}
	first.RawSchema[0] = 'x'

	second, err := ReleasedSchemaFor(1)
	if err != nil {
		t.Fatalf("ReleasedSchemaFor(1): %v", err)
	}
	if second.RawSchema[0] == 'x' {
		t.Fatal("ReleasedSchemaFor hands out the registry's own backing array; a caller can mutate an immutable released schema")
	}
	if !bytes.Equal(second.RawSchema, schemav1.RawSchema) {
		t.Fatal("mutating a returned RawSchema corrupted the retained v1 package's bytes")
	}
}

// The retained v1 types have no production caller yet -- they exist so a
// future v2 has a real v1 shape to convert from (design spec §3,
// "Wire-version compatibility"). Referencing them here is what keeps them
// compiling and importable rather than silently rotting.
func TestRetainedV1Types_AreImportableAndRoundTrip(t *testing.T) {
	doc := schemav1.Resume{
		SchemaVersion: 1,
		Content: map[string]schemav1.Section{
			"work": schemav1.Section(`{"sectionType":"work","entries":[]}`),
		},
	}
	if _, err := json.Marshal(doc); err != nil {
		t.Fatalf("marshalling a retained v1 document: %v", err)
	}

	var entry schemav1.WorkEntry
	if err := json.Unmarshal([]byte(`{"id":"","jobTitle":"Engineer"}`), &entry); err != nil {
		t.Fatalf("unmarshalling a retained v1 work entry: %v", err)
	}
	if entry.JobTitle == nil || *entry.JobTitle != "Engineer" {
		t.Fatalf("retained v1 WorkEntry.JobTitle = %v, want \"Engineer\"", entry.JobTitle)
	}
	if schemav1.Work != "work" {
		t.Fatalf("retained v1 SectionType constant Work = %q, want %q", schemav1.Work, "work")
	}
}
