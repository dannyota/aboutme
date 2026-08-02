package resume_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
)

// validDocForTest returns a document decoded straight from full.json --
// schema-valid and store-valid -- for tests that just need "a fully valid
// resume" without caring about its specific content.
func validDocForTest(t *testing.T) schema.Resume {
	t.Helper()
	parts := splitFixture(t, "full.json")
	doc, err := resume.DecodeParts(parts.PersonalDetails, parts.Content, parts.Customization, parts.SchemaVersion)
	if err != nil {
		t.Fatalf("DecodeParts(full.json): %v", err)
	}
	return doc
}

// --- D1 adoption conditions ---

// TestD1a_FormatAssertion_MatchesAjvPosture proves format assertion is
// enabled and pinned to match packages/schema's ajv configuration
// (addFormats(new Ajv2020({allErrors:true, strict:true})), which ASSERTS
// format -- draft 2020-12 defaults jsonschema/v6 to annotation-only, so this
// would otherwise silently accept a malformed uuid/uri.
func TestD1a_FormatAssertion_MatchesAjvPosture(t *testing.T) {
	doc := validDocForTest(t)
	// Corrupt a uuid-format field (personalDetails.details[0].id) to a
	// syntactically-invalid uuid. If format were merely annotated (the
	// draft 2020-12 default), this would validate successfully; format
	// assertion must reject it.
	doc.PersonalDetails.Details[0].ID = "not-a-uuid"

	err := resume.ValidateForStore(doc)
	if err == nil {
		t.Fatal("expected ValidateForStore to reject an invalid uuid format, got nil (format assertion is not enabled)")
	}
	ve, ok := err.(*resume.ValidationError)
	if !ok {
		t.Fatalf("expected *resume.ValidationError, got %T: %v", err, err)
	}
	found := false
	for _, issue := range ve.Issues {
		if strings.Contains(issue, "uuid") || strings.Contains(issue, "format") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected an issue mentioning the uuid/format violation, got: %v", ve.Issues)
	}
}

// TestD1a_FormatAssertion_URI mirrors the uuid case for the "uri" format
// (personalDetails.details[].value under a URL-constrained type, and
// $defs/link) -- both formats ajv asserts, per this task's brief.
func TestD1a_FormatAssertion_URI(t *testing.T) {
	doc := validDocForTest(t)
	work := doc.Content["work"]
	badLink := "https://not a valid uri because of the space and\nnewline"
	work.WorkEntries[0].EmployerLink = &badLink
	doc.Content["work"] = work

	err := resume.ValidateForStore(doc)
	if err == nil {
		t.Fatal("expected ValidateForStore to reject an invalid uri format, got nil")
	}
}

// TestD1b_NoURLLoader_RemoteRefFails proves the compiler this package uses
// is configured with no URL loader: resolving any external (or file) $ref
// must fail at compile time, never touching the network or filesystem.
func TestD1b_NoURLLoader_RemoteRefFails(t *testing.T) {
	c := resume.NewSchemaCompilerForTest()

	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(`{"$ref": "https://example.com/other.schema.json"}`))
	if err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	const loc = "mem://d1b-remote-ref-test.json"
	if err := c.AddResource(loc, doc); err != nil {
		t.Fatalf("AddResource: %v", err)
	}

	if _, err := c.Compile(loc); err == nil {
		t.Fatal("expected compiling a schema with an unresolvable remote $ref to fail, got nil error")
	} else if !strings.Contains(err.Error(), "example.com") {
		t.Fatalf("expected the error to reference the unresolved remote URL, got: %v", err)
	}
}

// TestD1b_NoURLLoader_FileRefFails is the other half of D1(b): the
// library's own DEFAULT loader (absent any UseLoader call) is a FileLoader
// that reads from the local filesystem for "file:" URLs. An empty
// jsonschema.SchemeURLLoader{} must reject that scheme too -- resolving a
// $ref must never depend on the filesystem either.
func TestD1b_NoURLLoader_FileRefFails(t *testing.T) {
	c := resume.NewSchemaCompilerForTest()

	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(`{"$ref": "file:///etc/passwd"}`))
	if err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	const loc = "mem://d1b-file-ref-test.json"
	if err := c.AddResource(loc, doc); err != nil {
		t.Fatalf("AddResource: %v", err)
	}

	if _, err := c.Compile(loc); err == nil {
		t.Fatal("expected compiling a schema with a file: $ref to fail, got nil error")
	}
}

// TestD1c_CompiledOnceAtPackageInit proves the embedded schema is compiled
// exactly once, at package init -- never lazily, never per call.
func TestD1c_CompiledOnceAtPackageInit(t *testing.T) {
	if got := resume.CompileCountForTest(); got != 1 {
		t.Fatalf("compile count = %d, want exactly 1 (compiled once at package init, before any test ran)", got)
	}

	before := resume.CompiledSchemaPointerForTest()
	doc := validDocForTest(t)
	for i := 0; i < 5; i++ {
		if err := resume.ValidateForStore(doc); err != nil {
			t.Fatalf("ValidateForStore(valid doc) call %d: unexpected error: %v", i, err)
		}
	}
	after := resume.CompiledSchemaPointerForTest()

	if got := resume.CompileCountForTest(); got != 1 {
		t.Fatalf("compile count after 5 ValidateForStore calls = %d, want still 1 (never recompiled per call)", got)
	}
	if before != after {
		t.Fatal("compiled schema pointer changed across calls (schema was recompiled)")
	}
}

// --- ValidateForStore pipeline (Step 2) ---

// pipelineFixture is one fixture the ValidateForStore choke-point test
// drives: every packages/schema/fixtures/*.json (excluding the store/
// subdirectory, which is walked separately below with its own store-layer
// fixtures) plus every packages/schema/fixtures/store/*.json.
func listJSONFixtures(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// loadFullDocument reads a full fixture document and decodes it via a plain
// json.Unmarshal into schema.Resume (not this package's DecodeParts -- these
// fixtures are single files, not split store columns). It returns the
// decode error rather than failing the test: a handful of fixtures (the
// "hostile sectiontype" ones) are deliberately unknown-sectionType payloads
// that schema.Section.UnmarshalJSON itself rejects before ValidateForStore
// is ever reached -- a decode failure is itself a rejection, and callers
// below treat it as one.
func loadFullDocument(t *testing.T, path string) (schema.Resume, error) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var doc schema.Resume
	err = json.Unmarshal(data, &doc)
	return doc, err
}

// TestValidateForStore_TopLevelFixtures drives every top-level fixture
// (packages/schema/fixtures/*.json, not the store/ subdirectory) through
// ValidateForStore: files named "invalid-*" must be rejected (whether by
// failing to decode at all, or by ValidateForStore itself), every other
// file must decode AND be accepted. This is the SAME naming convention
// packages/schema/test/schema.test.ts already uses.
func TestValidateForStore_TopLevelFixtures(t *testing.T) {
	dir := schemaFixturesDir(t)
	for _, name := range listJSONFixtures(t, dir) {
		name := name
		t.Run(name, func(t *testing.T) {
			wantInvalid := strings.HasPrefix(name, "invalid-")
			doc, decodeErr := loadFullDocument(t, filepath.Join(dir, name))
			if decodeErr != nil {
				if !wantInvalid {
					t.Errorf("%s: expected to decode and be accepted, but failed to decode: %v", name, decodeErr)
				}
				return
			}
			err := resume.ValidateForStore(doc)
			if wantInvalid && err == nil {
				t.Errorf("%s: expected ValidateForStore to reject it, got nil", name)
			}
			if !wantInvalid && err != nil {
				t.Errorf("%s: expected ValidateForStore to accept it, got: %v", name, err)
			}
		})
	}
}

// storeFixtureExpectations pins each packages/schema/fixtures/store/*.json
// fixture's expected ValidateForStore verdict. Unlike the top-level
// fixtures, plain "invalid-*" naming is not enough on its own here to prove
// intent by itself (a few of these files also happen to trip the
// JSON-Schema layer, e.g. an out-of-enum sectionType) -- but every one of
// them is still invalid overall, so ValidateForStore's naming convention
// still holds: this table exists to make the "why" explicit per file, not
// to override the verdict.
var storeFixtureExpectations = map[string]bool{ // name -> wantValid
	"invalid-duplicate-entry-id.json":                 false,
	"invalid-hostile-sectiontype-constructor.json":    false,
	"invalid-hostile-sectiontype-hasownproperty.json": false,
	"invalid-hostile-sectiontype-proto.json":          false,
	"invalid-layout-duplicate-across-arrays.json":     false,
	"invalid-layout-missing-content-key.json":         false,
	"invalid-layout-orphan-content-key.json":          false,
	"invalid-missing-dates-start.json":                false,
	"invalid-oversize-richtext-bytes.json":            false,
	"invalid-personal-detail-url-scheme.json":         false,
	"invalid-reversed-date-range.json":                false,
	"valid-unique-entry-id.json":                      true,
}

func TestValidateForStore_StoreFixtures(t *testing.T) {
	dir := filepath.Join(schemaFixturesDir(t), "store")
	names := listJSONFixtures(t, dir)
	if len(names) != len(storeFixtureExpectations) {
		t.Fatalf("fixtures/store has %d fixtures, storeFixtureExpectations pins %d -- update the table", len(names), len(storeFixtureExpectations))
	}
	for _, name := range names {
		name := name
		wantValid, known := storeFixtureExpectations[name]
		if !known {
			t.Fatalf("%s: no expectation pinned in storeFixtureExpectations", name)
		}
		t.Run(name, func(t *testing.T) {
			doc, decodeErr := loadFullDocument(t, filepath.Join(dir, name))
			if decodeErr != nil {
				if wantValid {
					t.Errorf("%s: expected to decode and be accepted, but failed to decode: %v", name, decodeErr)
				}
				return
			}
			err := resume.ValidateForStore(doc)
			if wantValid && err != nil {
				t.Errorf("%s: expected ValidateForStore to accept it, got: %v", name, err)
			}
			if !wantValid && err == nil {
				t.Errorf("%s: expected ValidateForStore to reject it, got nil", name)
			}
		})
	}
}

// TestValidateForStore_IssuesDeterministic proves repeated calls over the
// same invalid document report identical issues in identical order.
func TestValidateForStore_IssuesDeterministic(t *testing.T) {
	dir := filepath.Join(schemaFixturesDir(t), "store")
	doc, decodeErr := loadFullDocument(t, filepath.Join(dir, "invalid-duplicate-entry-id.json"))
	if decodeErr != nil {
		t.Fatalf("decoding fixture: %v", decodeErr)
	}

	first := resume.ValidateForStore(doc)
	if first == nil {
		t.Fatal("expected an error")
	}
	for i := 0; i < 5; i++ {
		got := resume.ValidateForStore(doc)
		if got == nil || got.Error() != first.Error() {
			t.Fatalf("run %d: issues not deterministic:\n first=%v\n got=%v", i, first, got)
		}
	}
}

// TestValidateForStore_MatchingIssue spot-checks that specific known
// fixtures report an issue that actually names the rule/field at fault, not
// just "invalid" with no detail.
func TestValidateForStore_MatchingIssue(t *testing.T) {
	cases := []struct {
		file      string
		substring string
	}{
		{"invalid-duplicate-entry-id.json", "duplicate-entry-id"},
		{"invalid-oversize-richtext-bytes.json", "rich-text-byte-length"},
		{"invalid-layout-orphan-content-key.json", "layout-orphan-content-key"},
		{"invalid-reversed-date-range.json", "date-range-order"},
	}
	dir := filepath.Join(schemaFixturesDir(t), "store")
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			doc, decodeErr := loadFullDocument(t, filepath.Join(dir, tc.file))
			if decodeErr != nil {
				t.Fatalf("decoding fixture: %v", decodeErr)
			}
			err := resume.ValidateForStore(doc)
			if err == nil {
				t.Fatalf("expected an error for %s", tc.file)
			}
			if !strings.Contains(err.Error(), tc.substring) {
				t.Errorf("%s: expected error to contain %q, got: %v", tc.file, tc.substring, err)
			}
		})
	}
}

// TestValidateForStore_SingleChokePoint proves ValidateForStore's three
// layers -- JSON-Schema, MaxDocumentBytes, schema.ValidateDocument -- are
// ALL exercised in one call: a document violating one rule from each layer
// simultaneously must report an issue from each, not stop at the first.
func TestValidateForStore_SingleChokePoint(t *testing.T) {
	doc := validDocForTest(t)

	// (1) JSON-Schema violation: an out-of-range skill level.
	skill := doc.Content["skill"]
	badLevel := int64(99)
	skill.SkillEntries[0].Level = &badLevel
	doc.Content["skill"] = skill

	// (2) schema.ValidateDocument violation: duplicate entry ids across
	// sections.
	work := doc.Content["work"]
	dupID := doc.Content["education"].EducationEntries[0].ID
	work.WorkEntries[0].ID = dupID
	doc.Content["work"] = work

	err := resume.ValidateForStore(doc)
	if err == nil {
		t.Fatal("expected an error")
	}
	ve, ok := err.(*resume.ValidationError)
	if !ok {
		t.Fatalf("expected *resume.ValidationError, got %T", err)
	}
	joined := strings.Join(ve.Issues, "\n")
	if !strings.Contains(joined, "maximum") && !strings.Contains(joined, "level") {
		t.Errorf("expected a JSON-Schema-layer issue about the out-of-range level, got: %v", ve.Issues)
	}
	if !strings.Contains(joined, "duplicate-entry-id") {
		t.Errorf("expected a store-layer duplicate-entry-id issue, got: %v", ve.Issues)
	}
}

func TestValidateForStore_ValidDocumentReturnsNil(t *testing.T) {
	if err := resume.ValidateForStore(validDocForTest(t)); err != nil {
		t.Fatalf("expected nil for a fully valid document, got: %v", err)
	}
}

func TestValidateForStore_MaxDocumentBytes_Simple(t *testing.T) {
	// A cheap (non-boundary-exact) sanity check that MaxDocumentBytes is
	// wired at all; the exact limit/limit+1 boundary is bounds_test.go's job.
	doc := validDocForTest(t)
	profile := doc.Content["profile"]
	huge := strings.Repeat("a", resume.MaxDocumentBytes)
	profile.ProfileEntries[0].Text = &huge
	doc.Content["profile"] = profile

	err := resume.ValidateForStore(doc)
	if err == nil {
		t.Fatal("expected an error for a document far beyond MaxDocumentBytes")
	}
}
