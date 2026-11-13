package api_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	schema "danny.vn/aboutme/packages/schema/gen/go"
)

func schemaFilePath() string {
	return filepath.Join("..", "..", "..", "..", "packages", "schema", "resume.schema.json")
}

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
			t.Fatalf("dispatch() does not recognize its own known constant %q; dispatch() and this list have drifted", sectionType)
		}
	}
}

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
		t.Fatalf("resume.schema.json declares %d sectionType(s) %v, but apps/server's dispatch recognizes %d %v; "+
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
				t.Fatalf("apps/server does not recognize sectionType %q declared by resume.schema.json; "+
					"dispatch() in this file needs a case added for it (design spec §3, Codegen fidelity)", want)
			}
		})
	}
}
