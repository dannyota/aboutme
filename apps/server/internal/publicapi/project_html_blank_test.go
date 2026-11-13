package publicapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/project"
)

// TestProjectPages_BlankNamedProjectIsUnavailable proves a linked, visible
// project whose renderable name is blank by the renderer's rule never fails
// a page: its own page and a list with nothing else answer exactly as an
// unknown resume, and a list with other projects leaves it out.
func TestProjectPages_BlankNamedProjectIsUnavailable(t *testing.T) {
	blankName := project.Document{DefaultLanguage: "en", Text: map[string]project.Text{"en": {Name: string(rune(0xfeff))}}}
	withBlank := func(t *testing.T, base func(*testing.T) *projectPagesStore, section string) *projectPagesStore {
		t.Helper()
		readStore := base(t)
		var content map[string]schema.Section
		if err := json.Unmarshal(readStore.row.Content, &content); err != nil {
			t.Fatal(err)
		}
		projectID, title := "00000000-0000-0000-0000-00000000b001", "blank"
		entries := content[section]
		entries.ProjectEntries = append(entries.ProjectEntries, schema.ProjectEntry{ID: "e-blank", Title: &title, ProjectID: &projectID})
		content[section] = entries
		raw, err := json.Marshal(content)
		if err != nil {
			t.Fatal(err)
		}
		readStore.row.Content = raw
		readStore.rows = append(readStore.rows, projectStoreRow{entryID: "e-blank", owner: projectTestOwner, slug: "blank-tool", doc: blankName})
		return readStore
	}
	onlyBlank := func(t *testing.T) *projectPagesStore {
		t.Helper()
		readStore := withBlank(t, projectPageFixture, "projects")
		var kept []projectStoreRow
		for _, row := range readStore.rows {
			if row.slug != "note-g" {
				kept = append(kept, row)
			}
		}
		readStore.rows = kept
		return readStore
	}
	for _, test := range []struct {
		name, path string
		store      func(*testing.T) *projectPagesStore
	}{
		{"page of the blank-named project", "/ada/projects/blank-tool", func(t *testing.T) *projectPagesStore { return withBlank(t, projectPageFixture, "projects") }},
		{"list of only the blank-named project", "/ada/projects", onlyBlank},
	} {
		t.Run(test.name, func(t *testing.T) {
			renderer, client := newFakeProjectRenderer(t)
			handlers := newProjectHandlers(t, projectTestReader(t, test.store(t)), client)
			reference := httptest.NewRecorder()
			handlers.resume.ServeHTTP(reference, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/nobody", nil))
			got := handlers.serve(http.MethodGet, test.path)
			if got.Code != http.StatusNotFound || !reflect.DeepEqual(got.Header(), reference.Header()) || got.Body.String() != reference.Body.String() {
				t.Fatalf("response = %d %#v; want the unknown resume's 404; log %s", got.Code, got.Header(), handlers.logs.String())
			}
			if renderer.count(directrender.ProjectRenderMode)+renderer.count(directrender.ProjectListRenderMode) != 0 {
				t.Fatal("an unavailable project reached the renderer")
			}
		})
	}

	renderer, client := newFakeProjectRenderer(t)
	handlers := newProjectHandlers(t, projectTestReader(t, withBlank(t, projectListFixture, "projects")), client)
	list := handlers.serve(http.MethodGet, "/ada/projects")
	if list.Code != http.StatusOK || list.Body.String() != readProjectCorpus(t)["list-vi-two-projects"].HTML {
		t.Fatalf("list = %d; want the corpus list without the blank-named project; log %s", list.Code, handlers.logs.String())
	}
	requireCorpusEnvelope(t, renderer.envelope(directrender.ProjectListRenderMode), "list-vi-two-projects")
}
