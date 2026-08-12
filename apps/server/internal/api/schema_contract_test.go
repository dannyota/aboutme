// schema_contract_test.go checks the server side of cross-language schema
// conformance. It imports github.com/dannyota/aboutme/packages/schema/gen/go
// as a compile-time dependency, wired through the root go.work and the server's
// go.mod, and independently re-derives the sectionType
// list from resume.schema.json (deliberately not importing anything from
// packages/schema/scripts/generate.mjs or trusting conformance.test.ts's
// derivation — the point is to check the pipeline against the schema file
// itself, the same discipline conformance.test.ts already applies).
//
// It checks two failure modes:
//   - TestKnownSectionTypeConstantsCompile references every
//     schema.SectionType constant apps/server depends on by name. If
//     packages/schema ever renames or removes one, apps/server fails to
//     *compile* (go build ./... and go vet ./... both break), not just
//     fails a test.
//   - TestServerRecognizesEverySchemaSectionType reads resume.schema.json at
//     runtime and fails if the schema declares a sectionType this file's
//     dispatch() doesn't have a case for — the scenario a 9th, newly added
//     sectionType produces: a new schema.SectionType constant exists (it's
//     regenerated from the schema by packages/schema/scripts/generate.mjs),
//     but nothing yet forces dispatch() to add a case for it, so this test
//     fails loudly instead of the server silently ignoring the new type.
package api_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

// schemaFilePath resolves resume.schema.json relative to this source file's
// package directory (apps/server/internal/api), four levels up to the repo
// root (internal/api -> internal -> apps/server -> apps -> repo root) then
// into packages/schema. Valid both for local `go test` (cwd = this
// package's directory) and inside the container build, since
// deploy/server.Dockerfile's build context is the repo root and preserves
// this same relative layout (see deploy/server.Dockerfile, deploy/compose.yml).
func schemaFilePath() string {
	return filepath.Join("..", "..", "..", "..", "packages", "schema", "resume.schema.json")
}

// resumeSchemaSectionOneOf mirrors just enough of resume.schema.json's shape
// to read $defs.section.oneOf[].properties.sectionType.const — the same
// field packages/schema/scripts/generate.mjs's deriveSectionVariants and
// packages/schema/test/conformance.test.ts's sectionTypesFromSchema both
// read, kept as an independent third reader here rather than a shared
// import so this test doesn't trust either of those derivations.
type resumeSchemaSectionOneOf struct {
	Defs struct {
		Section struct {
			OneOf []struct {
				Properties struct {
					SectionType struct {
						Const string `json:"const"`
					} `json:"sectionType"`
				} `json:"properties"`
			} `json:"oneOf"`
		} `json:"section"`
	} `json:"$defs"`
}

// sectionTypesFromSchemaFile reads resume.schema.json off disk and returns
// every sectionType it currently declares, sorted for deterministic
// comparison.
func sectionTypesFromSchemaFile(t *testing.T) []string {
	t.Helper()

	path := schemaFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	var doc resumeSchemaSectionOneOf
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	if len(doc.Defs.Section.OneOf) == 0 {
		t.Fatalf("%s: $defs.section.oneOf is missing or empty", path)
	}

	types := make([]string, 0, len(doc.Defs.Section.OneOf))
	for i, branch := range doc.Defs.Section.OneOf {
		if branch.Properties.SectionType.Const == "" {
			t.Fatalf("%s: $defs.section.oneOf[%d] has no properties.sectionType.const", path, i)
		}
		types = append(types, branch.Properties.SectionType.Const)
	}
	sort.Strings(types)
	return types
}

// dispatch is the exhaustive switch a production consumer of schema.Section
// must keep in sync with schema.SectionType. Every case references a generated
// constant by name, never a
// server-local copy of the discriminator string. This keeps internal/resume
// from introducing a parallel discriminator model.
func dispatch(t schema.SectionType) (recognized bool) {
	switch t {
	case schema.Profile,
		schema.Work,
		schema.Education,
		schema.Skill,
		schema.Language,
		schema.Certificate,
		schema.Project,
		schema.SectionTypeCustom:
		return true
	default:
		return false
	}
}

// TestKnownSectionTypeConstantsCompile is the compile-time half of the
// tripwire. Every schema.SectionType constant apps/server currently relies
// on is named here explicitly; if packages/schema ever renames or removes
// one, this file fails to compile, breaking `go build ./...` and
// `go vet ./...`, not merely a test run.
func TestKnownSectionTypeConstantsCompile(t *testing.T) {
	t.Parallel()

	known := []schema.SectionType{
		schema.Profile,
		schema.Work,
		schema.Education,
		schema.Skill,
		schema.Language,
		schema.Certificate,
		schema.Project,
		schema.SectionTypeCustom,
	}
	for _, sectionType := range known {
		if !dispatch(sectionType) {
			t.Fatalf("dispatch() does not recognize its own known constant %q — dispatch() and this list have drifted", sectionType)
		}
	}
}

// TestServerRecognizesEverySchemaSectionType is the runtime half: it reads
// resume.schema.json directly and fails if the schema declares a
// sectionType dispatch() doesn't handle, or if dispatch()'s known set
// contains a sectionType the schema no longer declares. Either direction
// means apps/server and packages/schema have drifted — exactly the failure
// mode a ninth sectionType added to the schema without server-side wiring
// produces.
func TestServerRecognizesEverySchemaSectionType(t *testing.T) {
	t.Parallel()

	schemaTypes := sectionTypesFromSchemaFile(t)

	known := []schema.SectionType{
		schema.Profile,
		schema.Work,
		schema.Education,
		schema.Skill,
		schema.Language,
		schema.Certificate,
		schema.Project,
		schema.SectionTypeCustom,
	}
	knownStrings := make([]string, len(known))
	for i, k := range known {
		knownStrings[i] = string(k)
	}
	sort.Strings(knownStrings)

	if len(schemaTypes) != len(knownStrings) {
		t.Fatalf("resume.schema.json declares %d sectionType(s) %v, but apps/server's dispatch recognizes %d %v — "+
			"a sectionType was added to (or removed from) the schema without updating dispatch() in this file",
			len(schemaTypes), schemaTypes, len(knownStrings), knownStrings)
	}
	for i := range schemaTypes {
		if schemaTypes[i] != knownStrings[i] {
			t.Fatalf("resume.schema.json's sectionType set %v does not match apps/server's dispatch set %v",
				schemaTypes, knownStrings)
		}
	}

	for _, want := range schemaTypes {
		want := want
		t.Run(want, func(t *testing.T) {
			t.Parallel()
			if !dispatch(schema.SectionType(want)) {
				t.Fatalf("apps/server does not recognize sectionType %q declared by resume.schema.json — "+
					"dispatch() in this file needs a case added for it (design spec §3, Codegen fidelity)", want)
			}
		})
	}
}
