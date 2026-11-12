package resumeapi

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/resume"
)

func bilingualProject() project.Document {
	month := int64(3)
	return project.Document{
		Dates:           &schema.DateRange{Start: schema.YearMonth{Y: 2024, M: &month}, Present: true},
		RepositoryURL:   "https://github.com/example/atlas",
		DemoURL:         "https://atlas.example",
		DefaultLanguage: "en",
		Text: map[string]project.Text{
			"en": {Name: "Atlas", Role: "Lead", Summary: "Built fast & safe", Outcome: "Never shown"},
			"vi": {Name: "Atlas VI", Role: "Trưởng nhóm", Summary: "Xây dựng"},
		},
	}
}

func createResumeWithLanguage(t *testing.T, h *resumeAPITestHarness, lng *string, doc schema.Resume) resume.Resume {
	t.Helper()
	created, err := h.resumes.CreateTx(h.ctx, h.queries, h.userID, "language", lng, doc)
	if err != nil {
		t.Fatalf("create resume: %v", err)
	}
	return created
}

func linkProject(t *testing.T, h *resumeAPITestHarness, resumeID, projectID uuid.UUID, revision int64) testHTTPResponse {
	t.Helper()
	return linkRouteRequest(t, h, http.MethodPost, apiResumePath+"/"+resumeID.String()+"/project-links",
		`{"projectId":"`+projectID.String()+`"}`, revision, "")
}

func onlyProjectEntry(t *testing.T, h *resumeAPITestHarness, resumeID uuid.UUID, key string) schema.ProjectEntry {
	t.Helper()
	stored, err := h.resumes.Get(h.ctx, h.userID, resumeID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	entries := stored.Doc.Content[key].ProjectEntries
	if len(entries) == 0 {
		t.Fatalf("section %q has no project entries: %+v", key, stored.Doc.Content)
	}
	return entries[len(entries)-1]
}

func requirePrefill(t *testing.T, entry schema.ProjectEntry, projectID uuid.UUID, title, subtitle, description string) {
	t.Helper()
	if entry.ProjectID == nil || *entry.ProjectID != projectID.String() {
		t.Fatalf("projectId = %v, want %s", entry.ProjectID, projectID)
	}
	if entry.Title == nil || *entry.Title != title || entry.Subtitle == nil || *entry.Subtitle != subtitle {
		t.Fatalf("title/subtitle = %v/%v, want %q/%q", entry.Title, entry.Subtitle, title, subtitle)
	}
	if entry.Link == nil || *entry.Link != "https://github.com/example/atlas" {
		t.Fatalf("link = %v, want the repository", entry.Link)
	}
	if entry.Dates == nil || entry.Dates.Start.Y != 2024 || !entry.Dates.Present {
		t.Fatalf("dates = %+v, want the library dates", entry.Dates)
	}
	if entry.Description == nil || *entry.Description != description {
		t.Fatalf("description = %v, want %q", entry.Description, description)
	}
}

func TestProjectLinks_LinkCreatesSectionAndPrefillsByLanguage(t *testing.T) {
	for _, tc := range []struct {
		name, title, subtitle, description string
		lng                                *string
	}{
		{"exact subtag", "Atlas VI", "Trưởng nhóm", "<p>Xây dựng</p>", stringPointer("vi-VN")},
		{"no match falls back", "Atlas", "Lead", "<p>Built fast &amp; safe</p>", stringPointer("fr")},
		{"und uses default", "Atlas", "Lead", "<p>Built fast &amp; safe</p>", stringPointer("und")},
		{"unset uses default", "Atlas", "Lead", "<p>Built fast &amp; safe</p>", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			owned := createLinkProject(t, h, h.userID, "atlas", bilingualProject())
			created := createResumeWithLanguage(t, h, tc.lng, loadMinimalDocument(t))
			response := linkProject(t, h, created.ID, owned.ID, created.Revision)
			if response.status != http.StatusOK || response.header.Get("ETag") != `"r2"` {
				t.Fatalf("link = %d %s", response.status, response.body)
			}
			stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
			if err != nil {
				t.Fatalf("reload: %v", err)
			}
			main := stored.Doc.Customization.Layout.Sections.Main
			if len(main) == 0 || main[len(main)-1] != "projects" || stored.Doc.Content["projects"].SectionType != schema.Project {
				t.Fatalf("main column = %v, want a project section keyed projects at its end", main)
			}
			requirePrefill(t, onlyProjectEntry(t, h, created.ID, "projects"), owned.ID, tc.title, tc.subtitle, tc.description)
		})
	}
}

func TestProjectLinks_LinkUsesTheExistingProjectSection(t *testing.T) {
	h := newResumeAPITestHarness(t)
	owned := createLinkProject(t, h, h.userID, "atlas", bilingualProject())
	created := createResumeWithLanguage(t, h, nil, loadFullDocument(t))
	response := linkProject(t, h, created.ID, owned.ID, created.Revision)
	if response.status != http.StatusOK {
		t.Fatalf("link = %d %s", response.status, response.body)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, added := stored.Doc.Content["projects"]; added || len(stored.Doc.Content["project"].ProjectEntries) != 2 {
		t.Fatalf("link did not append to the existing project section: %+v", stored.Doc.Content["project"])
	}
	again := linkProject(t, h, created.ID, owned.ID, stored.Revision)
	if again.status != http.StatusConflict || errorCodeOf(t, again.body) != "project_already_linked" {
		t.Fatalf("second link = %d %s, want 409 project_already_linked", again.status, again.body)
	}
	foreign := createLinkProject(t, h, createOtherAccount(t, h), "foreign", linkProjectDocument("Foreign"))
	foreignResponse := linkProject(t, h, created.ID, foreign.ID, stored.Revision)
	missingResponse := linkProject(t, h, created.ID, uuid.New(), stored.Revision)
	if foreignResponse.status != http.StatusNotFound || errorCodeOf(t, foreignResponse.body) != "project_not_found" ||
		foreignResponse.status != missingResponse.status || string(foreignResponse.body) != string(missingResponse.body) {
		t.Fatalf("foreign = %d %s, missing = %d %s; want identical 404 project_not_found",
			foreignResponse.status, foreignResponse.body, missingResponse.status, missingResponse.body)
	}
}

func TestProjectLinks_RefreshRepeatsOnlyThePrefill(t *testing.T) {
	h := newResumeAPITestHarness(t)
	owned := createLinkProject(t, h, h.userID, "atlas", bilingualProject())
	created := createResumeWithLanguage(t, h, nil, loadMinimalDocument(t))
	if response := linkProject(t, h, created.ID, owned.ID, created.Revision); response.status != http.StatusOK {
		t.Fatalf("link = %d %s", response.status, response.body)
	}
	entry := onlyProjectEntry(t, h, created.ID, "projects")
	hidden := true
	entry.IsHidden = &hidden
	entry.Title = stringPointer("Atlas for banks")
	entry.Description = stringPointer("<p>Shortened</p>")
	edit := resumeRequest(t, h, http.MethodPatch, apiResumePath+"/"+created.ID.String()+"/entries/projects",
		entryJSON(t, entry), 2, uuid.New(), "")
	if edit.status != http.StatusOK {
		t.Fatalf("owner edit = %d %s", edit.status, edit.body)
	}

	edited := bilingualProject()
	edited.RepositoryURL = ""
	edited.Text["en"] = project.Text{Name: "Atlas 2", Role: "Architect"}
	current, err := project.NewStore().GetTx(h.ctx, h.queries, h.userID, owned.ID)
	if err != nil {
		t.Fatalf("read project: %v", err)
	}
	if _, err = project.NewStore().ReplaceTx(h.ctx, h.queries, h.userID, owned.ID, current.Revision, current.Slug, edited, time.Now(), nil); err != nil {
		t.Fatalf("edit project: %v", err)
	}
	if unchanged := onlyProjectEntry(t, h, created.ID, "projects"); *unchanged.Title != "Atlas for banks" || *unchanged.Description != "<p>Shortened</p>" {
		t.Fatalf("a library edit rewrote the entry: %+v", unchanged)
	}

	refresh := linkRouteRequest(t, h, http.MethodPost,
		apiResumePath+"/"+created.ID.String()+"/entries/"+entry.ID+"/project-refresh", "", 3, "")
	if refresh.status != http.StatusOK || refresh.header.Get("ETag") != `"r4"` {
		t.Fatalf("refresh = %d %s", refresh.status, refresh.body)
	}
	refreshed := onlyProjectEntry(t, h, created.ID, "projects")
	if refreshed.ID != entry.ID || refreshed.IsHidden == nil || !*refreshed.IsHidden || *refreshed.ProjectID != owned.ID.String() {
		t.Fatalf("refresh changed id, hidden or link: %+v", refreshed)
	}
	if *refreshed.Title != "Atlas 2" || *refreshed.Subtitle != "Architect" || refreshed.Link == nil || *refreshed.Link != "https://atlas.example" {
		t.Fatalf("refresh = %+v, want the library name, role and demo link", refreshed)
	}
	if refreshed.Description != nil {
		t.Fatalf("refresh kept a description the library no longer has: %q", *refreshed.Description)
	}

	unlinked := linkedEntry(uuid.Nil, "Plain")
	add := resumeRequest(t, h, http.MethodPatch, apiResumePath+"/"+created.ID.String()+"/entries/projects",
		entryJSON(t, unlinked), 4, uuid.New(), "")
	if add.status != http.StatusOK {
		t.Fatalf("add unlinked = %d %s", add.status, add.body)
	}
	for name, entryID := range map[string]string{"unlinked": unlinked.ID, "missing": uuid.NewString()} {
		response := linkRouteRequest(t, h, http.MethodPost,
			apiResumePath+"/"+created.ID.String()+"/entries/"+entryID+"/project-refresh", "", 5, "")
		if !slices.Contains([]int{http.StatusNotFound}, response.status) {
			t.Fatalf("%s refresh = %d %s, want 404", name, response.status, response.body)
		}
	}
}
