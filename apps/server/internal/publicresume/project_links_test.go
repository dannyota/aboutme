package publicresume

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/store"
)

func linkedReaderRow(t *testing.T, slug string, ids map[string]string) store.Resume {
	t.Helper()
	row := validReaderRow(t, slug)
	project, hidden := uuid.NewString(), true
	entry := func(name string) schema.ProjectEntry {
		title := name
		return schema.ProjectEntry{ID: ids[name], Title: &title, ProjectID: &project}
	}
	unlinked, hiddenEntry := entry("unlinked"), entry("hidden")
	unlinked.ProjectID, hiddenEntry.IsHidden = nil, &hidden
	content := map[string]schema.Section{
		"work": {SectionType: schema.Project, ProjectEntries: []schema.ProjectEntry{entry("first"), unlinked, hiddenEntry, entry("second")}},
		"side": {SectionType: schema.Project, ProjectEntries: []schema.ProjectEntry{entry("aside")}},
	}
	var err error
	if row.Content, err = json.Marshal(content); err != nil {
		t.Fatal(err)
	}
	var customization schema.Customization
	customization.Layout.Sections.Main, customization.Layout.Sections.Sidebar = []string{"work"}, []string{"side"}
	if row.Customization, err = json.Marshal(customization); err != nil {
		t.Fatal(err)
	}
	return row
}

func linkIDs() map[string]string {
	ids := map[string]string{}
	for _, name := range []string{"first", "unlinked", "hidden", "second", "aside"} {
		ids[name] = uuid.NewString()
	}
	return ids
}

func linkColumn(t *testing.T, pairs ...string) json.RawMessage {
	t.Helper()
	links := []map[string]string{}
	for i := 0; i < len(pairs); i += 2 {
		links = append(links, map[string]string{"entryId": pairs[i], "projectSlug": pairs[i+1]})
	}
	raw, err := json.Marshal(links)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestReaderReadResume_ProjectLinksFollowLayoutAndEntryOrder(t *testing.T) {
	ids := linkIDs()
	readStore := &readerStore{row: linkedReaderRow(t, "ada", ids), links: projectColumn(t,
		ids["aside"], "side-tool", ids["second"], "second-tool", ids["hidden"], "hidden-tool", ids["first"], "first-tool")}
	snapshot, lease, err := newTestReader(t, readStore).ReadResume(context.Background(), "ada", publicstate.RepresentationJSON)
	if err != nil {
		t.Fatalf("ReadResume() error = %v", err)
	}
	lease.Release()
	want := []ProjectLink{
		{EntryID: ids["first"], ProjectSlug: "first-tool", URL: "https://resume.example/ada/projects/first-tool"},
		{EntryID: ids["second"], ProjectSlug: "second-tool", URL: "https://resume.example/ada/projects/second-tool"},
		{EntryID: ids["aside"], ProjectSlug: "side-tool", URL: "https://resume.example/ada/projects/side-tool"},
	}
	if !reflect.DeepEqual(snapshot.ProjectLinks, want) {
		t.Fatalf("ProjectLinks = %+v, want %+v", snapshot.ProjectLinks, want)
	}
}

func TestReaderReadResume_NoLinkedEntryIsAnEmptyLinkContext(t *testing.T) {
	snapshot, lease, err := newTestReader(t, &readerStore{row: validReaderRow(t, "ada")}).ReadResume(context.Background(), "ada", publicstate.RepresentationJSON)
	if err != nil {
		t.Fatalf("ReadResume() error = %v", err)
	}
	lease.Release()
	if snapshot.ProjectLinks == nil || len(snapshot.ProjectLinks) != 0 {
		t.Fatalf("ProjectLinks = %#v, want an empty, non-nil list", snapshot.ProjectLinks)
	}
}

func TestReaderReadResume_MalformedLinkContextIsUnavailable(t *testing.T) {
	ids := linkIDs()
	for name, column := range map[string]json.RawMessage{
		"not an array":   json.RawMessage(`{}`),
		"null":           json.RawMessage(`null`),
		"invalid slug":   projectColumn(t, ids["first"], "Not A Slug"),
		"path in slug":   projectColumn(t, ids["first"], "../x"),
		"empty entry id": projectColumn(t, "", "first-tool"),
		"no document":    linkColumn(t, ids["first"], "first-tool"),
		"null document":  json.RawMessage(`[{"entryId":"` + ids["first"] + `","projectSlug":"first-tool","document":null}]`),
		"document type":  json.RawMessage(`[{"entryId":"` + ids["first"] + `","projectSlug":"first-tool","document":"text"}]`),
	} {
		t.Run(name, func(t *testing.T) {
			readStore := &readerStore{row: linkedReaderRow(t, "ada", ids), links: column}
			_, lease, err := newTestReader(t, readStore).ReadResume(context.Background(), "ada", publicstate.RepresentationJSON)
			if lease != nil {
				lease.Release()
				t.Fatal("ReadResume() returned a lease for a malformed link context")
			}
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("ReadResume() error = %v, want ErrUnavailable", err)
			}
		})
	}
}

func TestReaderReadResume_LinksOnlyProjectsWithANameThePageCanShow(t *testing.T) {
	ids := linkIDs()
	row := linkedReaderRow(t, "ada", ids)
	blank := [3]string{ids["first"], "first-tool", string(rune(0xfeff))}
	named := [3]string{ids["second"], "second-tool", "Second"}
	snapshot, lease, err := newTestReader(t, &readerStore{row: row, links: namedColumn(t, blank, named)}).ReadResume(context.Background(), "ada", publicstate.RepresentationJSON)
	if err != nil {
		t.Fatalf("ReadResume() error = %v", err)
	}
	lease.Release()
	want := []ProjectLink{{EntryID: ids["second"], ProjectSlug: "second-tool", URL: "https://resume.example/ada/projects/second-tool"}}
	if !reflect.DeepEqual(snapshot.ProjectLinks, want) {
		t.Fatalf("ProjectLinks = %+v, want only the named project %+v", snapshot.ProjectLinks, want)
	}

	snapshot, lease, err = newTestReader(t, &readerStore{row: row, links: namedColumn(t, blank)}).ReadResume(context.Background(), "ada", publicstate.RepresentationJSON)
	if err != nil {
		t.Fatalf("ReadResume() error = %v; a blank name is unavailable, not a failure", err)
	}
	lease.Release()
	if snapshot.ProjectLinks == nil || len(snapshot.ProjectLinks) != 0 {
		t.Fatalf("ProjectLinks = %#v, want an empty, non-nil list", snapshot.ProjectLinks)
	}
}

func TestReaderReadResume_LinkNameFollowsTheProjectPageLanguage(t *testing.T) {
	ids := linkIDs()
	row := linkedReaderRow(t, "ada", ids)
	lng := "vi-VN"
	row.Lng = &lng
	column, err := json.Marshal([]map[string]any{
		{"entryId": ids["first"], "projectSlug": "first-tool", "document": map[string]any{"defaultLanguage": "en", "text": map[string]any{
			"en": map[string]string{"name": "First"}, "vi": map[string]string{"name": " "}}}},
		{"entryId": ids["second"], "projectSlug": "second-tool", "document": map[string]any{"defaultLanguage": "vi", "text": map[string]any{
			"en": map[string]string{"name": "Second"}, "vi": map[string]string{"name": "\u3000"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, lease, err := newTestReader(t, &readerStore{row: row, links: column}).ReadResume(context.Background(), "ada", publicstate.RepresentationJSON)
	if err != nil {
		t.Fatalf("ReadResume() error = %v", err)
	}
	lease.Release()
	want := []ProjectLink{{EntryID: ids["first"], ProjectSlug: "first-tool", URL: "https://resume.example/ada/projects/first-tool"}}
	if !reflect.DeepEqual(snapshot.ProjectLinks, want) {
		t.Fatalf("ProjectLinks = %+v, want the project whose page falls back to a named default %+v", snapshot.ProjectLinks, want)
	}
}
