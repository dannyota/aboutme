package publicresume

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// namedColumn is a projects column of entry id, project slug and name rows,
// each document with only an English text.
func namedColumn(t *testing.T, rows ...[3]string) json.RawMessage {
	t.Helper()
	column := []map[string]any{}
	for _, row := range rows {
		document := map[string]any{"defaultLanguage": "en", "text": map[string]any{"en": map[string]string{"name": row[2]}}}
		column = append(column, map[string]any{"entryId": row[0], "projectSlug": row[1], "document": document})
	}
	raw, err := json.Marshal(column)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestReaderReadProjects_BlankNamedProjectIsUnavailable proves a linked,
// visible project whose renderable name is blank by the renderer's rule is
// not available: the list skips it and its own page shows nothing, so both
// answer as an unknown resume when nothing else remains.
func TestReaderReadProjects_BlankNamedProjectIsUnavailable(t *testing.T) {
	ids := projectPagesIDs()
	row := projectPagesRow(t, "ada", ids)
	bom := string(rune(0xfeff))
	blank := [3]string{ids["first"], "first-tool", bom}
	named := [3]string{ids["second"], "second-tool", "Second"}

	_, pages := readProjectPages(t, newTestReader(t, &readerStore{row: row, projects: namedColumn(t, blank, named)}), "")
	if len(pages.Projects) != 1 || pages.Projects[0].Slug != "second-tool" || pages.Available != 1 {
		t.Fatalf("list = %+v, available %d; want only second-tool", pages.Projects, pages.Available)
	}
	if _, pages := readProjectPages(t, newTestReader(t, &readerStore{row: row, projects: namedColumn(t, blank)}), ""); pages.Shown() {
		t.Fatalf("list of only a blank-named project = %+v, want nothing shown", pages.Projects)
	}
	links := namedColumn(t, blank, named)
	if _, pages := readProjectPages(t, newTestReader(t, &readerStore{row: row, links: links, projects: namedColumn(t, blank)}), "first-tool"); pages.Shown() {
		t.Fatalf("page of a blank-named project = %+v, want nothing shown", pages.Projects)
	}
	_, read, lease, err := newTestReader(t, &readerStore{row: row, projects: namedColumn(t, blank)}).ReadProjects(context.Background(), "ada", "")
	if err != nil || lease == nil {
		t.Fatalf("ReadProjects() error = %v; want the resume admitted", err)
	}
	lease.Release()
	if _, err := read.Pages(); err != nil {
		t.Fatalf("Pages() error = %v; a blank name is unavailable, not a failure", err)
	}
}

// TestReaderReadProjects_OtherProjectsNeedARenderableName proves a project
// page counts another project as available only when its name, in the
// language the renderer would show, is not blank, as the list does.
func TestReaderReadProjects_OtherProjectsNeedARenderableName(t *testing.T) {
	ids := projectPagesIDs()
	row := projectPagesRow(t, "ada", ids)
	blank := [3]string{ids["first"], "first-tool", string(rune(0xfeff))}
	named := [3]string{ids["second"], "second-tool", "Second"}
	aside := [3]string{ids["aside"], "side-tool", "Aside"}

	only := &readerStore{row: row, links: namedColumn(t, blank, named), projects: namedColumn(t, named)}
	if _, pages := readProjectPages(t, newTestReader(t, only), "second-tool"); pages.Available != 1 {
		t.Errorf("available = %d beside only a blank-named project, want 1", pages.Available)
	}
	another := &readerStore{row: row, links: namedColumn(t, blank, named, aside), projects: namedColumn(t, named)}
	if _, pages := readProjectPages(t, newTestReader(t, another), "second-tool"); pages.Available != 2 {
		t.Errorf("available = %d beside a named project, want 2", pages.Available)
	}
	bare := &readerStore{row: row, links: linkColumn(t, ids["first"], "first-tool", ids["second"], "second-tool"), projects: namedColumn(t, named)}
	_, read, lease, err := newTestReader(t, bare).ReadProjects(context.Background(), "ada", "second-tool")
	if err != nil || lease == nil {
		t.Fatalf("ReadProjects() error = %v; want the resume admitted", err)
	}
	lease.Release()
	if _, err := read.Pages(); !errors.Is(err, ErrUnavailable) {
		t.Errorf("Pages() error = %v beside a link without a document, want ErrUnavailable", err)
	}
}
