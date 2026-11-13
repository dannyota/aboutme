package resume_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/resume"
)

func validDocForTest(t *testing.T) schema.Resume {
	t.Helper()
	parts := splitFixture(t, "full.json")
	doc, err := resume.DecodeParts(parts.PersonalDetails, parts.Content, parts.Customization, parts.SchemaVersion)
	if err != nil {
		t.Fatalf("DecodeParts(full.json): %v", err)
	}
	return doc
}

func TestResumeSchemaIDMatchesCurrentEmbeddedSchema(t *testing.T) {
	var raw struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(schema.RawSchema, &raw); err != nil {
		t.Fatalf("unmarshal current embedded schema: %v", err)
	}
	if got := resume.ResumeSchemaIDForTest(); got != raw.ID {
		t.Fatalf("registered schema ID = %q, embedded $id = %q", got, raw.ID)
	}
}

func TestD1a_FormatAssertion_MatchesAjvPosture(t *testing.T) {
	doc := validDocForTest(t)
	doc.PersonalDetails.Details[0].ID = "not-a-uuid"

	err := resume.ValidateForStore(doc)
	if err == nil {
		t.Fatal("expected ValidateForStore to reject an invalid uuid format, got nil (format assertion is not enabled)")
	}
	var ve *resume.ValidationError
	if !errors.As(err, &ve) {
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

func TestD1a_FormatAssertion_URI(t *testing.T) {
	doc := validDocForTest(t)
	work := doc.Content["work"]
	// jsonschema/v6's uri format uses net/url.Parse, which tolerates an unencoded space, so only the newline isolates format.
	badLink := "https://a.example.com/\nnewline"
	work.WorkEntries[0].EmployerLink = &badLink
	doc.Content["work"] = work

	err := resume.ValidateForStore(doc)
	if err == nil {
		t.Fatal("expected ValidateForStore to reject an invalid uri format, got nil")
	}
	var ve *resume.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *resume.ValidationError, got %T: %v", err, err)
	}
	found := false
	for _, issue := range ve.Issues {
		if strings.Contains(issue, "uri") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected an issue mentioning the uri format violation, got: %v", ve.Issues)
	}
}

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

var storeFixtureExpectations = map[string]bool{
	"invalid-duplicate-detail-id.json":                false,
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

func TestValidateForStore_IssuesSortedPathFirstAcrossLayers(t *testing.T) {
	doc := validDocForTest(t)

	dupID := "00000000-0000-0000-0000-000000000001"
	aaa := schema.NewWorkSection(nil, nil, []schema.WorkEntry{
		{ID: dupID},
		{ID: dupID},
	})

	tooLong := strings.Repeat("a", 161)
	zzz := schema.NewWorkSection(nil, nil, []schema.WorkEntry{
		{ID: "00000000-0000-0000-0000-000000000002", JobTitle: &tooLong},
	})

	doc.Content = map[string]schema.Section{"aaa": aaa, "zzz": zzz}
	doc.Customization.Layout.Sections.Main = []string{"aaa", "zzz"}
	doc.Customization.Layout.Sections.Sidebar = []string{}

	err := resume.ValidateForStore(doc)
	if err == nil {
		t.Fatal("expected an error")
	}
	var ve *resume.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("expected *resume.ValidationError, got %T", err)
	}
	if len(ve.Issues) < 2 {
		t.Fatalf("expected at least 2 issues, got %d: %v", len(ve.Issues), ve.Issues)
	}
	firstIssue := ve.Issues[0]
	if !strings.Contains(firstIssue, "content.aaa") {
		t.Fatalf("expected the first (path-first-sorted) issue to be about content.aaa (the store-layer duplicate-entry-id issue), got: %v", ve.Issues)
	}
	lastIssue := ve.Issues[len(ve.Issues)-1]
	if !strings.Contains(lastIssue, "zzz") {
		t.Fatalf("expected the last issue to be about content.zzz (the schema-layer maxLength issue), got: %v", ve.Issues)
	}
}

func TestValidateForStore_MatchingIssue(t *testing.T) {
	cases := []struct {
		file      string
		substring string
	}{
		{"invalid-duplicate-detail-id.json", "duplicate-detail-id"},
		{"invalid-duplicate-entry-id.json", "duplicate-entry-id"},
		{"invalid-layout-duplicate-across-arrays.json", "layout-exactly-once"},
		{"invalid-layout-missing-content-key.json", "layout-missing-content-key"},
		{"invalid-layout-orphan-content-key.json", "layout-orphan-content-key"},
		{"invalid-missing-dates-start.json", "start"},
		{"invalid-oversize-richtext-bytes.json", "rich-text-byte-length"},
		{"invalid-personal-detail-url-scheme.json", "personal-detail-url-scheme"},
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

	decodeCases := []struct {
		file      string
		substring string
	}{
		{"invalid-hostile-sectiontype-constructor.json", "constructor"},
		{"invalid-hostile-sectiontype-hasownproperty.json", "hasOwnProperty"},
		{"invalid-hostile-sectiontype-proto.json", "__proto__"},
	}
	for _, tc := range decodeCases {
		t.Run(tc.file, func(t *testing.T) {
			_, decodeErr := loadFullDocument(t, filepath.Join(dir, tc.file))
			if decodeErr == nil {
				t.Fatalf("expected a decode error for %s", tc.file)
			}
			if !strings.Contains(decodeErr.Error(), tc.substring) {
				t.Errorf("%s: expected decode error to contain %q, got: %v", tc.file, tc.substring, decodeErr)
			}
		})
	}
}

func TestValidateForStore_SingleChokePoint(t *testing.T) {
	doc := validDocForTest(t)

	skill := doc.Content["skill"]
	badLevel := int64(99)
	skill.SkillEntries[0].Level = &badLevel
	doc.Content["skill"] = skill

	work := doc.Content["work"]
	dupID := doc.Content["education"].EducationEntries[0].ID
	work.WorkEntries[0].ID = dupID
	doc.Content["work"] = work

	err := resume.ValidateForStore(doc)
	if err == nil {
		t.Fatal("expected an error")
	}
	var ve *resume.ValidationError
	if !errors.As(err, &ve) {
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
