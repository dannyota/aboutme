package resumeapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

func TestEntryCanonicalTargetsIncludeCanonicalBodyAndPathEntryIDs(t *testing.T) {
	t.Parallel()

	const resumeID = "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f"
	const entryID = "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60"
	want := []string{"resume_id", resumeID, "section_key", "work", "entry_id", entryID}

	upsert := httptest.NewRequestWithContext(context.Background(), http.MethodPatch, "/", strings.NewReader(`{"entry":{"id":"`+entryID+`"}}`))
	upsert.Header.Set("Content-Type", "application/json")
	upsert.SetPathValue("id", resumeID)
	upsert.SetPathValue("sectionKey", "work")
	upsertInput, err := decodeEntryUpsert(upsert)
	if err != nil {
		t.Fatalf("decode upsert: %v", err)
	}
	upsertTargets, err := entryCanonicalTargets(upsertInput)
	if err != nil {
		t.Fatalf("upsert targets: %v", err)
	}
	if !reflect.DeepEqual(upsertTargets, want) {
		t.Fatalf("upsert targets = %v, want %v", upsertTargets, want)
	}

	deleteRequest := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "/", nil)
	deleteRequest.SetPathValue("id", resumeID)
	deleteRequest.SetPathValue("sectionKey", "work")
	deleteRequest.SetPathValue("entryId", strings.ReplaceAll(entryID, "-", ""))
	deleteInput, err := decodeEntryDelete(deleteRequest)
	if err != nil {
		t.Fatalf("decode delete: %v", err)
	}
	deleteTargets, err := entryCanonicalTargets(deleteInput)
	if err != nil {
		t.Fatalf("delete targets: %v", err)
	}
	if !reflect.DeepEqual(deleteTargets, want) {
		t.Fatalf("delete targets = %v, want canonical %v", deleteTargets, want)
	}
}

func entryTestDocument(t *testing.T) json.RawMessage {
	t.Helper()
	doc := loadMinimalDocument(t)
	doc.Content = map[string]schema.Section{
		"work": schema.NewWorkSection(nil, nil, []schema.WorkEntry{
			{ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60"},
			{ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61"},
		}),
		"skill": schema.NewSkillSection(nil, nil, []schema.SkillEntry{
			{ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e62"},
		}),
	}
	doc.Customization.Layout.Sections.Main = []string{"work"}
	doc.Customization.Layout.Sections.Sidebar = []string{"skill"}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal entry test document: %v", err)
	}
	return raw
}

func decodeEntryTestDocument(t *testing.T, raw json.RawMessage) schema.Resume {
	t.Helper()
	var doc schema.Resume
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode changed document: %v", err)
	}
	return doc
}

func TestEntryUpsertAppendsAndReplacesInPlace(t *testing.T) {
	t.Parallel()
	raw := entryTestDocument(t)

	appended, err := applyEntryUpsert(raw, "work", json.RawMessage(`{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e63","jobTitle":"new"}`))
	if err != nil {
		t.Fatalf("append entry: %v", err)
	}
	doc := decodeEntryTestDocument(t, appended)
	if got := doc.Content["work"].WorkEntries; len(got) != 3 || got[2].ID != "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e63" {
		t.Fatalf("appended entries = %#v", got)
	}

	replaced, err := applyEntryUpsert(appended, "work", json.RawMessage(`{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60","jobTitle":"replacement"}`))
	if err != nil {
		t.Fatalf("replace entry: %v", err)
	}
	doc = decodeEntryTestDocument(t, replaced)
	got := doc.Content["work"].WorkEntries
	if len(got) != 3 || got[0].JobTitle == nil || *got[0].JobTitle != "replacement" || got[1].ID != "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61" {
		t.Fatalf("replaced entries = %#v", got)
	}
}

func TestEntryUpsertPreservesDraftAndRejectsWrongShapeCollisionAndLimit(t *testing.T) {
	t.Parallel()
	raw := entryTestDocument(t)

	draft, err := applyEntryUpsert(raw, "work", json.RawMessage(`{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e63"}`))
	if err != nil {
		t.Fatalf("draft entry: %v", err)
	}
	if got := decodeEntryTestDocument(t, draft).Content["work"].WorkEntries[2]; got.ID != "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e63" || got.JobTitle != nil {
		t.Fatalf("draft entry = %#v", got)
	}

	for name, test := range map[string]struct {
		entry     string
		wantIssue string
	}{
		"wrong shape":             {`{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e64","degree":"BSc"}`, "work entry"},
		"other section collision": {`{"id":"01890f47-7e8a-7b2a-8d70-9a1f2c3d4e62"}`, "whole resume"},
	} {
		t.Run(name, func(t *testing.T) {
			_, applyErr := applyEntryUpsert(raw, "work", json.RawMessage(test.entry))
			if applyErr == nil || !strings.Contains(applyErr.Error(), test.wantIssue) {
				t.Fatalf("error = %v, want issue containing %q", applyErr, test.wantIssue)
			}
		})
	}

	doc := decodeEntryTestDocument(t, raw)
	entries := make([]schema.WorkEntry, 64)
	// Use stable valid UUIDs without sharing ids across sections.
	for i := range entries {
		entries[i].ID = fmt.Sprintf("10000000-0000-4000-8000-%012d", i)
	}
	doc.Content["work"] = schema.NewWorkSection(nil, nil, entries)
	overRaw, marshalErr := json.Marshal(doc)
	if marshalErr != nil {
		t.Fatalf("marshal at-limit document: %v", marshalErr)
	}
	_, err = applyEntryUpsert(overRaw, "work", json.RawMessage(`{"id":"20000000-0000-4000-8000-000000000001"}`))
	if err == nil || !strings.Contains(err.Error(), "64") {
		t.Fatalf("65th entry error = %v, want limit issue", err)
	}
}

func TestEntryDeleteRemovesOnlyTargetAndKeepsEmptySection(t *testing.T) {
	t.Parallel()
	raw := entryTestDocument(t)
	changed, err := applyEntryDelete(raw, "work", "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60")
	if err != nil {
		t.Fatalf("delete entry: %v", err)
	}
	doc := decodeEntryTestDocument(t, changed)
	if got := doc.Content["work"].WorkEntries; len(got) != 1 || got[0].ID != "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61" {
		t.Fatalf("remaining entries = %#v", got)
	}

	one := doc
	one.Content["work"] = schema.NewWorkSection(nil, nil, []schema.WorkEntry{{ID: "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61"}})
	oneRaw, marshalErr := json.Marshal(one)
	if marshalErr != nil {
		t.Fatalf("marshal one-entry document: %v", marshalErr)
	}
	empty, err := applyEntryDelete(oneRaw, "work", "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e61")
	if err != nil {
		t.Fatalf("delete last entry: %v", err)
	}
	if entries := decodeEntryTestDocument(t, empty).Content["work"].WorkEntries; entries == nil || len(entries) != 0 {
		t.Fatalf("emptied section entries = %#v, want present empty array", entries)
	}

	if _, err := applyEntryDelete(raw, "work", "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e99"); err == nil {
		t.Fatal("unknown entry delete succeeded")
	}
}
