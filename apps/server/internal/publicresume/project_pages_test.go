package publicresume

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/store"
)

// projectPagesRow stores a main project section with two linked entries
// around an unlinked one and a hidden linked entry, a sidebar project
// section with one linked entry, and a linked entry in a section outside the
// layout. The first entry carries its own link.
func projectPagesRow(t *testing.T, slug string, ids map[string]string) store.Resume {
	t.Helper()
	row := validReaderRow(t, slug)
	hidden, link := true, "https://ada.example/notes"
	entry := func(name string) schema.ProjectEntry {
		title, project := name, uuid.NewString()
		return schema.ProjectEntry{ID: ids[name], Title: &title, ProjectID: &project}
	}
	first, unlinked, hiddenEntry := entry("first"), entry("unlinked"), entry("hidden")
	first.Link, unlinked.ProjectID, hiddenEntry.IsHidden = &link, nil, &hidden
	content := map[string]schema.Section{
		"work":    {SectionType: schema.Project, ProjectEntries: []schema.ProjectEntry{first, unlinked, hiddenEntry, entry("second")}},
		"side":    {SectionType: schema.Project, ProjectEntries: []schema.ProjectEntry{entry("aside")}},
		"outside": {SectionType: schema.Project, ProjectEntries: []schema.ProjectEntry{entry("outside")}},
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

func projectPagesIDs() map[string]string {
	ids := map[string]string{}
	for _, name := range []string{"first", "unlinked", "hidden", "second", "aside", "outside"} {
		ids[name] = uuid.NewString()
	}
	return ids
}

// projectColumn is a projects column: entry id, project slug and a document
// named after the slug, for each pair.
func projectColumn(t *testing.T, pairs ...string) json.RawMessage {
	t.Helper()
	rows := []map[string]any{}
	for i := 0; i < len(pairs); i += 2 {
		document := map[string]any{"defaultLanguage": "en", "text": map[string]any{"en": map[string]string{"name": pairs[i+1]}}}
		rows = append(rows, map[string]any{"entryId": pairs[i], "projectSlug": pairs[i+1], "document": document})
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func projectNames(pages ProjectPages) []string {
	names := []string{}
	for _, project := range pages.Projects {
		names = append(names, project.Slug+"="+project.Document.Text["en"].Name)
	}
	return names
}

func TestReaderReadProjects_ListFollowsLayoutAndOmitsWhatTheProjectionOmits(t *testing.T) {
	ids := projectPagesIDs()
	// The statement returns rows in no particular order and, as a guard,
	// rows for a hidden entry and an entry outside the layout.
	readStore := &readerStore{row: projectPagesRow(t, "ada", ids), projects: projectColumn(t,
		ids["aside"], "side-tool", ids["outside"], "outside-tool", ids["second"], "second-tool", ids["hidden"], "hidden-tool", ids["first"], "first-tool")}
	snapshot, pages, lease, err := newTestReader(t, readStore).ReadProjects(context.Background(), "ada", "")
	if err != nil {
		t.Fatalf("ReadProjects() error = %v", err)
	}
	lease.Release()
	if got, want := projectNames(pages), []string{"first-tool=first-tool", "second-tool=second-tool", "side-tool=side-tool"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("projects = %v, want %v", got, want)
	}
	if pages.Available != 3 || readStore.calls != 1 {
		t.Fatalf("available = %d, reads = %d, want 3 and 1", pages.Available, readStore.calls)
	}
	first := pages.Projects[0]
	if first.URL != "https://resume.example/ada/projects/first-tool" || first.EntryLink != "https://ada.example/notes" || pages.Projects[1].EntryLink != "" {
		t.Fatalf("first project = %+v, second entry link %q", first, pages.Projects[1].EntryLink)
	}
	if len(snapshot.ProjectLinks) != 3 || snapshot.Revision != 1 || snapshot.Public.Slug != "ada" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestReaderReadProjects_PageReadsOnlyTheRequestedProject(t *testing.T) {
	ids := projectPagesIDs()
	readStore := &readerStore{
		row:      projectPagesRow(t, "ada", ids),
		links:    linkColumn(t, ids["first"], "first-tool", ids["second"], "second-tool", ids["aside"], "side-tool"),
		projects: projectColumn(t, ids["second"], "second-tool"),
	}
	_, pages, lease, err := newTestReader(t, readStore).ReadProjects(context.Background(), "ada", "second-tool")
	if err != nil {
		t.Fatalf("ReadProjects() error = %v", err)
	}
	lease.Release()
	if got, want := projectNames(pages), []string{"second-tool=second-tool"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("projects = %v, want %v", got, want)
	}
	if pages.Available != 3 || readStore.projectSlug != "second-tool" {
		t.Fatalf("available = %d, statement slug = %q", pages.Available, readStore.projectSlug)
	}
}

// TestReaderReadProjects_UnknownResumeIsNotFound proves an unknown or
// non-live resume, and a project slug outside the grammar, answer exactly as
// an unknown resume: ErrNotFound and no lease.
func TestReaderReadProjects_UnknownResumeIsNotFound(t *testing.T) {
	ids := projectPagesIDs()
	row := projectPagesRow(t, "ada", ids)
	notLive := row
	notLive.Live = false
	for _, test := range []struct {
		name        string
		store       *readerStore
		projectSlug string
	}{
		{"unknown resume", &readerStore{err: pgx.ErrNoRows}, ""},
		{"unknown resume page", &readerStore{err: pgx.ErrNoRows}, "first-tool"},
		{"not live", &readerStore{row: notLive, projects: projectColumn(t, ids["first"], "first-tool")}, ""},
		{"invalid page slug", &readerStore{row: row, projects: projectColumn(t, ids["first"], "first-tool")}, "First-Tool"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, lease, err := newTestReader(t, test.store).ReadProjects(context.Background(), "ada", test.projectSlug)
			if lease != nil {
				lease.Release()
				t.Fatal("ReadProjects() returned a lease for an unknown resume")
			}
			if !errors.Is(err, ErrNotFound) || errors.Is(err, ErrUnavailable) {
				t.Fatalf("ReadProjects() error = %v, want only ErrNotFound", err)
			}
		})
	}
}

// TestReaderReadProjects_UnavailablePageKeepsTheAdmission proves a live
// resume with nothing to show on the requested page is still admitted: the
// snapshot and lease come back with pages that are not Shown, so the caller
// applies sign in to view before it answers as an unknown resume.
func TestReaderReadProjects_UnavailablePageKeepsTheAdmission(t *testing.T) {
	ids := projectPagesIDs()
	row := projectPagesRow(t, "ada", ids)
	allLinks := linkColumn(t, ids["first"], "first-tool", ids["hidden"], "hidden-tool", ids["outside"], "outside-tool")
	for _, test := range []struct {
		name          string
		store         *readerStore
		projectSlug   string
		wantAvailable int
	}{
		{"no linked project", &readerStore{row: row}, "", 0},
		{"only hidden and omitted entries", &readerStore{row: row, projects: projectColumn(t, ids["hidden"], "hidden-tool", ids["outside"], "outside-tool")}, "", 0},
		{"page of a hidden entry", &readerStore{row: row, links: allLinks, projects: projectColumn(t, ids["hidden"], "hidden-tool")}, "hidden-tool", 1},
		{"page in an omitted section", &readerStore{row: row, links: allLinks, projects: projectColumn(t, ids["outside"], "outside-tool")}, "outside-tool", 1},
		{"page not linked", &readerStore{row: row, links: allLinks}, "other-tool", 1},
		{"page slug of another row", &readerStore{row: row, links: allLinks, projects: projectColumn(t, ids["first"], "first-tool")}, "second-tool", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot, pages, lease, err := newTestReader(t, test.store).ReadProjects(context.Background(), "ada", test.projectSlug)
			if err != nil || lease == nil {
				t.Fatalf("ReadProjects() error = %v, lease %v; want an admitted read", err, lease)
			}
			lease.Release()
			if pages.Shown() || len(pages.Projects) != 0 || pages.Available != test.wantAvailable {
				t.Fatalf("pages = %+v, want nothing shown and %d available", pages, test.wantAvailable)
			}
			if snapshot.ResumeID != row.ID || snapshot.Revision != row.Revision || snapshot.Public.Slug != "ada" {
				t.Fatalf("snapshot = %+v, want the admitted resume", snapshot)
			}
		})
	}
}

func TestReaderReadProjects_MalformedColumnsAreUnavailable(t *testing.T) {
	ids := projectPagesIDs()
	row := projectPagesRow(t, "ada", ids)
	for name, column := range map[string]json.RawMessage{
		"not an array":   json.RawMessage(`{}`),
		"null":           json.RawMessage(`null`),
		"invalid slug":   projectColumn(t, ids["first"], "Not A Slug"),
		"empty entry id": projectColumn(t, "", "first-tool"),
		"document type":  json.RawMessage(`[{"entryId":"` + ids["first"] + `","projectSlug":"first-tool","document":"text"}]`),
	} {
		t.Run(name, func(t *testing.T) {
			_, _, lease, err := newTestReader(t, &readerStore{row: row, projects: column}).ReadProjects(context.Background(), "ada", "")
			if lease != nil {
				lease.Release()
				t.Fatal("ReadProjects() returned a lease for a malformed column")
			}
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("ReadProjects() error = %v, want ErrUnavailable", err)
			}
		})
	}
}

func TestReaderReadProjects_MismatchRetriesOnceThenUnavailable(t *testing.T) {
	ids := projectPagesIDs()
	row := projectPagesRow(t, "ada", ids)
	row.Revision = 3
	readStore := &readerStore{row: row, projects: projectColumn(t, ids["first"], "first-tool")}
	reader := newTestReader(t, readStore)
	lease, err := reader.coordinator.AcquireResume(context.Background(), row.ID, 4, publicstate.RepresentationHTML)
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	if _, _, lease, err := reader.ReadProjects(context.Background(), "ada", ""); !errors.Is(err, ErrUnavailable) || lease != nil {
		t.Fatalf("ReadProjects() error = %v, want unavailable after the second mismatch", err)
	}
	if readStore.calls != 2 {
		t.Fatalf("database reads = %d, want 2", readStore.calls)
	}
}
