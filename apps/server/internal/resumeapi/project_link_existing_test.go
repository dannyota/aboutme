package resumeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/resume"
)

// fullFixtureWorkEntryID and fullFixtureCustomEntryID name entries of the
// full schema fixture's work and custom sections.
const (
	fullFixtureWorkEntryID   = "dd89bd8a-ba7d-4bec-9c43-f1b296c56fac"
	fullFixtureCustomEntryID = "6f2b6ec0-dc76-4c6e-a44f-6d9e2141b44e"
)

// unlinkedProjectEntry is a project entry with every text field, a hidden
// flag and no link.
func unlinkedProjectEntry() schema.ProjectEntry {
	start, end := int64(4), int64(11)
	hidden := true
	return schema.ProjectEntry{
		ID:          uuid.NewString(),
		Title:       stringPointer("Note G Interpreter <beta>"),
		Subtitle:    stringPointer("Author & maintainer"),
		Link:        stringPointer("https://github.com/ada/note-g"),
		Dates:       &schema.DateRange{Start: schema.YearMonth{Y: 2021, M: &start}, End: &schema.YearMonth{Y: 2022, M: &end}},
		Description: stringPointer("<p>An interpreter for <strong>Note G</strong> &amp; its tables.</p>"),
		IsHidden:    &hidden,
	}
}

// existingEntryDocument returns the full fixture whose project section holds
// a linked entry, target, and the fixture's own entry, in that order.
func existingEntryDocument(t *testing.T, target schema.ProjectEntry, linkedTo uuid.UUID) schema.Resume {
	t.Helper()
	doc := loadFullDocument(t)
	doc.PersonalDetails.Photo = nil
	section := doc.Content["project"]
	section.ProjectEntries = append([]schema.ProjectEntry{linkedEntry(linkedTo, "Linked"), target}, section.ProjectEntries...)
	doc.Content["project"] = section
	return doc
}

func canonicalDocument(t *testing.T, doc schema.Resume) json.RawMessage {
	t.Helper()
	raw, err := resume.AssembleCanonical(doc)
	if err != nil {
		t.Fatalf("assemble document: %v", err)
	}
	return raw
}

// withEntryLink returns a fresh copy of raw whose entryID links projectID.
func withEntryLink(t *testing.T, raw json.RawMessage, entryID string, projectID uuid.UUID) schema.Resume {
	t.Helper()
	doc, err := strictDecodeCurrentDocument(raw)
	if err != nil {
		t.Fatalf("decode document: %v", err)
	}
	for key, section := range doc.Content {
		for index := range section.ProjectEntries {
			if section.ProjectEntries[index].ID == entryID {
				section.ProjectEntries[index].ProjectID = stringPointer(projectID.String())
				doc.Content[key] = section
				return doc
			}
		}
	}
	t.Fatalf("document has no project entry %s", entryID)
	return schema.Resume{}
}

func TestLinkExistingEntry_SetsOnlyTheProjectID(t *testing.T) {
	t.Parallel()
	target := unlinkedProjectEntry()
	raw := canonicalDocument(t, existingEntryDocument(t, target, uuid.New()))
	projectID := uuid.New()

	linked, err := linkExistingEntry(raw, target.ID, projectID)
	if err != nil {
		t.Fatalf("link existing entry: %v", err)
	}
	got, err := strictDecodeCurrentDocument(linked)
	if err != nil {
		t.Fatalf("decode linked document: %v", err)
	}
	want := withEntryLink(t, raw, target.ID, projectID)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("linked document differs beyond the entry's projectId:\n got=%+v\nwant=%+v", got.Content["project"], want.Content["project"])
	}
	if gotBytes, wantBytes := canonicalDocument(t, got), canonicalDocument(t, want); !bytes.Equal(gotBytes, wantBytes) {
		t.Fatalf("linked document bytes differ:\n got=%s\nwant=%s", gotBytes, wantBytes)
	}
	if entries := got.Content["project"].ProjectEntries; len(entries) != 3 || entries[1].ID != target.ID {
		t.Fatalf("project entries = %+v, want the target kept second of three", entries)
	}
}

func TestLinkExistingEntry_RefusesEntriesItCannotLink(t *testing.T) {
	t.Parallel()
	target := unlinkedProjectEntry()
	other := uuid.New()
	doc := existingEntryDocument(t, target, other)
	linkedID := doc.Content["project"].ProjectEntries[0].ID
	raw := canonicalDocument(t, doc)
	for _, tc := range []struct {
		name    string
		entryID string
		status  int
		code    string
		path    string
	}{
		{"unknown entry", uuid.NewString(), http.StatusNotFound, "entry_not_found", ""},
		{"already linked entry", linkedID, http.StatusConflict, "project_already_linked", ""},
		{"work entry", fullFixtureWorkEntryID, http.StatusUnprocessableEntity, "document_invalid", "entryId"},
		{"custom entry", fullFixtureCustomEntryID, http.StatusUnprocessableEntity, "document_invalid", "entryId"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			changed, err := linkExistingEntry(raw, tc.entryID, uuid.New())
			if err == nil {
				t.Fatalf("link = %s, want %s", changed, tc.code)
			}
			mapped := mapMutationError(err)
			if mapped.Status != tc.status || mapped.Code != tc.code {
				t.Fatalf("error = %d %s (%v), want %d %s", mapped.Status, mapped.Code, err, tc.status, tc.code)
			}
			var validation *resume.ValidationError
			if tc.path != "" && (!errors.As(err, &validation) || len(validation.Structured) != 1 || validation.Structured[0].Path != tc.path) {
				t.Fatalf("validation = %+v, want one issue at %s", validation, tc.path)
			}
		})
	}
}

func decodeProjectLinkBody(t *testing.T, resumeID, body string) (projectLinkPrepared, *clientError) {
	t.Helper()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", resumeID)
	input, err := decodeProjectLink(req)
	if err != nil {
		return projectLinkPrepared{}, mapMutationError(err)
	}
	prepared, ok := input.Value.(projectLinkPrepared)
	if !ok {
		t.Fatalf("decoded value = %T, want projectLinkPrepared", input.Value)
	}
	return prepared, nil
}

func TestDecodeProjectLink_OptionalEntryID(t *testing.T) {
	t.Parallel()
	resumeID, projectID, entryID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	inserted, failure := decodeProjectLinkBody(t, resumeID, `{"projectId":"`+projectID+`"}`)
	if failure != nil || inserted.ProjectID.String() != projectID || inserted.EntryID != "" {
		t.Fatalf("insert = %+v %v, want the project and no entry", inserted, failure)
	}
	existing, failure := decodeProjectLinkBody(t, resumeID, `{"projectId":"`+projectID+`","entryId":"`+entryID+`"}`)
	if failure != nil || existing.ProjectID.String() != projectID || existing.EntryID != entryID {
		t.Fatalf("existing = %+v %v, want the project and entry %s", existing, failure, entryID)
	}
	for name, body := range map[string]string{
		"entry without project": `{"entryId":"` + entryID + `"}`,
		"uppercase entry id":    `{"projectId":"` + projectID + `","entryId":"` + strings.ToUpper(entryID) + `"}`,
		"braced entry id":       `{"projectId":"` + projectID + `","entryId":"{` + entryID + `}"}`,
		"not a uuid":            `{"projectId":"` + projectID + `","entryId":"atlas"}`,
		"empty entry id":        `{"projectId":"` + projectID + `","entryId":""}`,
		"number entry id":       `{"projectId":"` + projectID + `","entryId":5}`,
	} {
		if prepared, failure := decodeProjectLinkBody(t, resumeID, body); failure == nil ||
			failure.Status != http.StatusBadRequest || failure.Code != "request_invalid" {
			t.Errorf("%s = %+v %+v, want 400 request_invalid", name, prepared, failure)
		}
	}
}

// A link that names an existing entry shares the insert's operation
// identity, so one idempotency key cannot both insert and link an entry for
// the same project.
func TestProjectLinkTargets_ExistingEntryKeepsTheLinkIdentity(t *testing.T) {
	t.Parallel()
	resumeID, projectID, entryID := uuid.New(), uuid.New(), uuid.NewString()
	targets := func(prepared projectLinkPrepared) []string {
		t.Helper()
		got, err := projectLinkTargets(boundedInput{Value: prepared})
		if err != nil {
			t.Fatalf("targets: %v", err)
		}
		return got
	}
	insert := targets(projectLinkPrepared{ResumeID: resumeID, ProjectID: projectID})
	existing := targets(projectLinkPrepared{ResumeID: resumeID, ProjectID: projectID, EntryID: entryID})
	refresh := targets(projectLinkPrepared{ResumeID: resumeID, EntryID: entryID})
	if !reflect.DeepEqual(existing, insert) {
		t.Fatalf("existing-entry targets = %v, want the insert's %v", existing, insert)
	}
	if want := []string{"resume_id", resumeID.String(), "entry_id", entryID}; !reflect.DeepEqual(refresh, want) {
		t.Fatalf("refresh targets = %v, want %v", refresh, want)
	}
}
