package resumeapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"testing"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
)

func TestWireVersion_PersonalDetailsAcceptProjectPersistEmit(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	doc := created.Doc
	doc.Customization.Font.Family = schema.AtkinsonHyperlegibleNext
	revision, err := h.resumes.SaveDocument(h.ctx, h.userID, created.ID, doc, created.Revision)
	if err != nil {
		t.Fatalf("seed v2-only font: %v", err)
	}
	preflight := resumeRequest(t, h, http.MethodGet, apiResumePath+"/"+created.ID.String(), "", 0, uuid.Nil, "1")
	if preflight.status != http.StatusOK {
		t.Fatalf("preflight v1 read = %d %s", preflight.status, preflight.body)
	}
	preflightResource := decodeResumeResource(t, preflight)

	v1Fixture, err := os.ReadFile("../../../../packages/schema/fixtures/v1/minimal.json")
	if err != nil {
		t.Fatalf("read v1 fixture: %v", err)
	}
	var v1Document map[string]json.RawMessage
	if decodeErr := json.Unmarshal(v1Fixture, &v1Document); decodeErr != nil {
		t.Fatalf("decode v1 fixture: %v", decodeErr)
	}
	var v1PersonalDetails map[string]json.RawMessage
	if decodeErr := json.Unmarshal(v1Document["personalDetails"], &v1PersonalDetails); decodeErr != nil {
		t.Fatalf("decode v1 personal-details fixture: %v", decodeErr)
	}
	fullName := json.RawMessage(`"Old client name"`)
	v1PersonalDetails["fullName"] = fullName
	v1PersonalDetails["details"] = json.RawMessage(`[]`)
	body, err := json.Marshal(v1PersonalDetails)
	if err != nil {
		t.Fatalf("marshal v1 personal-details: %v", err)
	}

	path := apiResumePath + "/" + created.ID.String() + "/personal-details"
	response := resumeRequest(t, h, http.MethodPatch, path, string(body), revision, uuid.New(), "1")
	if response.status != http.StatusOK || response.header.Get(wireVersionHeader) != "1" ||
		response.header.Get("ETag") != `"r3"` {
		t.Fatalf("v1 write = status %d schema=%q etag=%q body=%s",
			response.status, response.header.Get(wireVersionHeader), response.header.Get("ETag"), response.body)
	}
	resource := decodeResumeResource(t, response)
	var emitted map[string]json.RawMessage
	if decodeErr := json.Unmarshal(resource.Document, &emitted); decodeErr != nil {
		t.Fatalf("decode v1 response document: %v", decodeErr)
	}
	if string(emitted["schemaVersion"]) != "1" {
		t.Fatalf("v1 response schemaVersion = %s, want 1", emitted["schemaVersion"])
	}
	var emittedPersonal map[string]json.RawMessage
	if decodeErr := json.Unmarshal(emitted["personalDetails"], &emittedPersonal); decodeErr != nil {
		t.Fatalf("decode v1 emitted personalDetails: %v", decodeErr)
	}
	if !bytes.Equal(emittedPersonal["fullName"], fullName) || string(emittedPersonal["details"]) != "[]" {
		t.Fatalf("v1 emitted personalDetails = %s, want representable fields unchanged", emitted["personalDetails"])
	}
	var expectedDocument map[string]json.RawMessage
	if decodeErr := json.Unmarshal(preflightResource.Document, &expectedDocument); decodeErr != nil {
		t.Fatalf("decode preflight v1 document: %v", decodeErr)
	}
	expectedDocument["personalDetails"] = body
	expectedRaw, err := json.Marshal(expectedDocument)
	if err != nil {
		t.Fatalf("encode expected v1 response: %v", err)
	}
	var expectedValue, emittedValue any
	if decodeErr := json.Unmarshal(expectedRaw, &expectedValue); decodeErr != nil {
		t.Fatalf("decode expected v1 value: %v", decodeErr)
	}
	if decodeErr := json.Unmarshal(resource.Document, &emittedValue); decodeErr != nil {
		t.Fatalf("decode emitted v1 value: %v", decodeErr)
	}
	if !reflect.DeepEqual(emittedValue, expectedValue) {
		t.Fatalf("v1 representable fields changed:\nexpected=%s\nemitted=%s", expectedRaw, resource.Document)
	}

	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("load stored current row: %v", err)
	}
	if stored.StoredSchemaVersion != docmigrate.CurrentVersion || stored.Doc.SchemaVersion != int64(docmigrate.CurrentVersion) ||
		stored.Doc.Customization.Font.Family != schema.AtkinsonHyperlegibleNext || stored.Revision != revision+1 {
		t.Fatalf("stored row schema=%d doc=%d font=%q revision=%d",
			stored.StoredSchemaVersion, stored.Doc.SchemaVersion, stored.Doc.Customization.Font.Family, stored.Revision)
	}
	if stored.Doc.PersonalDetails.FullName == nil || *stored.Doc.PersonalDetails.FullName != "Old client name" ||
		stored.Doc.PersonalDetails.Details == nil || len(stored.Doc.PersonalDetails.Details) != 0 {
		t.Fatalf("stored v2 personalDetails = %#v", stored.Doc.PersonalDetails)
	}
	assertStoredPartsEqualDocument(t, h, created.ID, stored.Doc)

	before := snapshotStoredResumeRow(t, h, created.ID)
	readV1 := resumeRequest(t, h, http.MethodGet, apiResumePath+"/"+created.ID.String(), "", 0, uuid.Nil, "1")
	if readV1.status != http.StatusOK || readV1.header.Get(wireVersionHeader) != "1" {
		t.Fatalf("v1 read = %d schema=%q body=%s", readV1.status, readV1.header.Get(wireVersionHeader), readV1.body)
	}
	after := snapshotStoredResumeRow(t, h, created.ID)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("v1 GET wrote row:\nbefore=%+v\nafter=%+v", before, after)
	}
	readResource := decodeResumeResource(t, readV1)
	var readDocument map[string]any
	if err := json.Unmarshal(readResource.Document, &readDocument); err != nil {
		t.Fatalf("decode v1 read document: %v", err)
	}
	customization, ok := readDocument["customization"].(map[string]any)
	if !ok {
		t.Fatalf("v1 customization = %T, want object", readDocument["customization"])
	}
	fontObject, ok := customization["font"].(map[string]any)
	if !ok {
		t.Fatalf("v1 font = %T, want object", customization["font"])
	}
	font := fontObject["family"]
	if font != "Inter" {
		t.Fatalf("v1 fallback font = %v, want Inter", font)
	}
}

func TestWireVersion_PersonalDetailsFailsClosedAndCurrentIdentity(t *testing.T) {
	for _, test := range []struct {
		name    string
		version string
	}{
		{name: "absent"},
		{name: "explicit current", version: "2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			created := h.createResume(t)
			path := apiResumePath + "/" + created.ID.String() + "/personal-details"
			response := resumeRequest(t, h, http.MethodPatch, path,
				`{"fullName":"Identity","details":[]}`, created.Revision, uuid.New(), test.version)
			if response.status != http.StatusOK || response.header.Get(wireVersionHeader) != "2" {
				t.Fatalf("current identity write = %d schema=%q body=%s",
					response.status, response.header.Get(wireVersionHeader), response.body)
			}
			stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
			if err != nil {
				t.Fatalf("reload identity write: %v", err)
			}
			assertStoredPartsEqualDocument(t, h, created.ID, stored.Doc)
		})
	}

	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	before := snapshotStoredResumeRow(t, h, created.ID)
	rejected := resumeRequest(t, h, http.MethodPatch,
		apiResumePath+"/"+created.ID.String()+"/personal-details",
		`{"fullName":"Never written"}`, created.Revision, uuid.New(), "999")
	assertResumeTestError(t, rejected, http.StatusBadRequest, "unsupported_schema_version")
	if after := snapshotStoredResumeRow(t, h, created.ID); !reflect.DeepEqual(after, before) {
		t.Fatalf("unsupported version wrote row:\nbefore=%+v\nafter=%+v", before, after)
	}
}

func TestWireVersion_ExplicitFallbackFontTargetsCurrentFont(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	doc := created.Doc
	doc.Customization.Font.Family = schema.AtkinsonHyperlegibleNext
	revision, err := h.resumes.SaveDocument(h.ctx, h.userID, created.ID, doc, created.Revision)
	if err != nil {
		t.Fatalf("seed v2-only font: %v", err)
	}

	response := resumeRequest(t, h, http.MethodPatch,
		apiResumePath+"/"+created.ID.String()+"/customization",
		`{"deltas":[{"op":"set","path":"font.family","value":"Inter"}]}`,
		revision, uuid.New(), "1")
	if response.status != http.StatusOK {
		t.Fatalf("v1 explicit fallback font = %d %s, want 200", response.status, response.body)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("reload explicit fallback font: %v", err)
	}
	if stored.Doc.Customization.Font.Family != schema.Inter {
		t.Fatalf("stored font = %q, want explicitly targeted %q", stored.Doc.Customization.Font.Family, schema.Inter)
	}
}

// TestWireVersion_AcceptProjectPersistEmit is Task 9's granular old-client
// proof. It drives the real entry route after that route lands in W3.
func TestWireVersion_AcceptProjectPersistEmit(t *testing.T) {
	h := newResumeAPITestHarness(t)
	doc := loadMinimalDocument(t)
	doc.Customization.Font.Family = schema.AtkinsonHyperlegibleNext
	doc.Content = map[string]schema.Section{
		"work": schema.NewWorkSection(nil, nil, []schema.WorkEntry{}),
	}
	doc.Customization.Layout.Sections.Main = []string{"work"}
	doc.Customization.Layout.Sections.Sidebar = []string{}
	created, err := h.resumes.Create(h.ctx, h.userID, "old client entry", doc)
	if err != nil {
		t.Fatalf("create entry fixture: %v", err)
	}
	entryID := "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60"
	body := `{"entry":{"id":"` + entryID + `","jobTitle":"V1 engineer"}}`
	response := resumeRequest(t, h, http.MethodPatch,
		apiResumePath+"/"+created.ID.String()+"/entries/work", body, created.Revision, uuid.New(), "1")
	if response.status != http.StatusOK || response.header.Get(wireVersionHeader) != "1" {
		t.Fatalf("v1 entry write = %d schema=%q body=%s",
			response.status, response.header.Get(wireVersionHeader), response.body)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("reload v1 entry write: %v", err)
	}
	if stored.StoredSchemaVersion != docmigrate.CurrentVersion || stored.Doc.SchemaVersion != int64(docmigrate.CurrentVersion) {
		t.Fatalf("stored versions row=%d doc=%d, want current %d",
			stored.StoredSchemaVersion, stored.Doc.SchemaVersion, docmigrate.CurrentVersion)
	}
	if stored.Doc.Customization.Font.Family != schema.AtkinsonHyperlegibleNext {
		t.Fatalf("non-targeted v2 font = %q, want %q",
			stored.Doc.Customization.Font.Family, schema.AtkinsonHyperlegibleNext)
	}
	entries := stored.Doc.Content["work"].WorkEntries
	if len(entries) != 1 || entries[0].ID != entryID || entries[0].JobTitle == nil || *entries[0].JobTitle != "V1 engineer" {
		t.Fatalf("stored current entry = %#v", entries)
	}
	assertStoredPartsEqualDocument(t, h, created.ID, stored.Doc)
	resource := decodeResumeResource(t, response)
	var emitted map[string]any
	if err := json.Unmarshal(resource.Document, &emitted); err != nil {
		t.Fatalf("decode v1 entry response: %v", err)
	}
	if emitted["schemaVersion"] != float64(1) {
		t.Fatalf("entry response schemaVersion = %v, want 1", emitted["schemaVersion"])
	}
}

type wireStoredRow struct {
	PersonalDetails string
	Content         string
	Customization   string
	SchemaVersion   int32
	Revision        int64
	UpdatedAt       string
}

func snapshotStoredResumeRow(t *testing.T, h *resumeAPITestHarness, id uuid.UUID) wireStoredRow {
	t.Helper()
	var row wireStoredRow
	if err := h.pool.QueryRow(h.ctx, `
		SELECT personal_details::text, content::text, customization::text,
		       schema_version, revision, updated_at::text
		FROM resumes WHERE id = $1`, id).Scan(
		&row.PersonalDetails, &row.Content, &row.Customization,
		&row.SchemaVersion, &row.Revision, &row.UpdatedAt,
	); err != nil {
		t.Fatalf("snapshot stored resume: %v", err)
	}
	return row
}

func assertStoredPartsEqualDocument(t *testing.T, h *resumeAPITestHarness, id uuid.UUID, document schema.Resume) {
	t.Helper()
	stored := snapshotStoredResumeRow(t, h, id)
	wantPersonal := string(mustResumeTestJSON(t, document.PersonalDetails))
	wantContent := string(mustResumeTestJSON(t, document.Content))
	wantCustomization := string(mustResumeTestJSON(t, document.Customization))
	for name, pair := range map[string][2]string{
		"personal_details": {stored.PersonalDetails, wantPersonal},
		"content":          {stored.Content, wantContent},
		"customization":    {stored.Customization, wantCustomization},
	} {
		var got, want any
		if err := json.Unmarshal([]byte(pair[0]), &got); err != nil {
			t.Fatalf("decode stored %s: %v", name, err)
		}
		if err := json.Unmarshal([]byte(pair[1]), &want); err != nil {
			t.Fatalf("decode wanted %s: %v", name, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("stored %s differs:\ngot=%s\nwant=%s", name, pair[0], pair[1])
		}
	}
}
