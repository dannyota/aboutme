package projectapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
)

func minimalResumeDocument(t *testing.T) schema.Resume {
	t.Helper()
	var doc schema.Resume
	if err := json.Unmarshal([]byte(`{"schemaVersion":6,"personalDetails":{"fullName":"Ada","details":[]},"content":{},`+
		`"customization":{"font":{"family":"inter","baseSizePx":14},"colors":{"primary":"#1a1a1a","text":"#1a1a1a","background":"#ffffff"},`+
		`"spacing":{"sectionGap":16,"entryGap":8,"lineHeight":1.4},"heading":{"style":"normal","showRule":false},`+
		`"layout":{"columns":1,"sections":{"main":[],"sidebar":[]}},"sectionDisplay":{"skill":{"style":"text"},"language":{"style":"text"}},`+
		`"pageFormat":"a4","dateFormat":"MM/YYYY"}}`), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func seedLinkedResume(t *testing.T, h *harness, projectID uuid.UUID, slug string) resume.Resume {
	t.Helper()
	title := "Atlas, my words"
	other := "Other"
	doc := minimalResumeDocument(t)
	link := projectID.String()
	doc.Content["projects"] = schema.Section{SectionType: schema.Project, ProjectEntries: []schema.ProjectEntry{
		{ID: uuid.NewString(), Title: &title, ProjectID: &link},
		{ID: uuid.NewString(), Title: &other},
	}}
	doc.Customization.Layout.Sections.Main = []string{"projects"}
	resumes := resume.NewStore(h.pool, docmigrate.NewIdentityProjector())
	created, err := resumes.Create(t.Context(), h.user.ID, "linked", doc)
	if err != nil {
		t.Fatalf("seed linked resume: %v", err)
	}
	if slug != "" {
		if _, err = h.pool.Exec(t.Context(), `UPDATE resumes SET slug = $2, live = true, revision = revision + 1 WHERE id = $1`, created.ID, slug); err != nil {
			t.Fatal(err)
		}
	}
	created, err = resumes.Get(t.Context(), h.user.ID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	return created
}

type changedResumesBody struct {
	Data struct {
		ChangedResumes []struct {
			ID       uuid.UUID `json:"id"`
			Revision string    `json:"revision"`
		} `json:"changedResumes"`
	} `json:"data"`
}

func reauthenticate(t *testing.T, h *harness) {
	t.Helper()
	if _, err := h.pool.Exec(t.Context(), `UPDATE sessions SET reauthenticated_at = now() WHERE id = $1`, h.session.ID); err != nil {
		t.Fatal(err)
	}
}

func createOther(t *testing.T, h *harness) string {
	t.Helper()
	r := h.request(t, http.MethodPost, "/api/v1/projects", `{"slug":"other","defaultLanguage":"en","text":{"en":{"name":"Other"}}}`, uuid.NewString(), "")
	if r.Code != http.StatusCreated {
		t.Fatalf("create other = %d %s", r.Code, r.Body.String())
	}
	return idOf(t, r)
}

func deleteProject(t *testing.T, h *harness, id, key, match string) *httptest.ResponseRecorder {
	t.Helper()
	return h.request(t, http.MethodDelete, "/api/v1/projects/"+id, "", key, match)
}

func TestProjectDelete_UnlinksEveryLinkingResume(t *testing.T) {
	h := newHarness(t)
	id := idOf(t, h.create(t))
	projectID := uuid.MustParse(id)
	live := seedLinkedResume(t, h, projectID, "link-live-"+uuid.NewString()[:8])
	draft := seedLinkedResume(t, h, projectID, "")
	unrelated := seedLinkedResume(t, h, uuid.MustParse(createOther(t, h)), "")
	reauthenticate(t, h)

	listed := h.request(t, http.MethodGet, "/api/v1/projects/"+id, "", "", "")
	var resource struct {
		Data struct {
			LinkedResumeIDs []uuid.UUID `json:"linkedResumeIds"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &resource); err != nil || len(resource.Data.LinkedResumeIDs) != 2 {
		t.Fatalf("project read = %s, want two linked resume ids", listed.Body.String())
	}

	key := uuid.NewString()
	first := deleteProject(t, h, id, key, `"r1"`)
	if first.Code != http.StatusOK {
		t.Fatalf("delete = %d %s", first.Code, first.Body.String())
	}
	var body changedResumesBody
	if err := json.Unmarshal(first.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := map[uuid.UUID]string{live.ID: "3", draft.ID: "2"}
	if len(body.Data.ChangedResumes) != len(want) {
		t.Fatalf("changed resumes = %+v, want %v", body.Data.ChangedResumes, want)
	}
	for _, changed := range body.Data.ChangedResumes {
		if want[changed.ID] != changed.Revision {
			t.Fatalf("changed resumes = %+v, want %v", body.Data.ChangedResumes, want)
		}
	}
	resumes := resume.NewStore(h.pool, docmigrate.NewIdentityProjector())
	for _, seeded := range []resume.Resume{live, draft} {
		stored, err := resumes.Get(t.Context(), h.user.ID, seeded.ID)
		if err != nil {
			t.Fatal(err)
		}
		entries := stored.Doc.Content["projects"].ProjectEntries
		if len(entries) != 2 || entries[0].ProjectID != nil || entries[0].Title == nil || *entries[0].Title != "Atlas, my words" {
			t.Fatalf("stored entries after delete = %+v, want the entry and its text without the link", entries)
		}
	}
	if stored, err := resumes.Get(t.Context(), h.user.ID, unrelated.ID); err != nil || stored.Revision != unrelated.Revision {
		t.Fatalf("unrelated resume revision = %d, want %d unchanged (err %v)", stored.Revision, unrelated.Revision, err)
	}

	replay := deleteProject(t, h, id, key, `"r1"`)
	if replay.Code != first.Code || replay.Body.String() != first.Body.String() {
		t.Fatalf("replay = %d %s, want %d %s", replay.Code, replay.Body.String(), first.Code, first.Body.String())
	}
	gone := h.request(t, http.MethodGet, "/api/v1/projects/"+id, "", "", "")
	assertCode(t, gone, http.StatusNotFound, "project_not_found")
}

func TestProjectDelete_WithoutLinksChangesNoResume(t *testing.T) {
	h := newHarness(t)
	id := idOf(t, h.create(t))
	untouched := seedLinkedResume(t, h, uuid.MustParse(createOther(t, h)), "")
	reauthenticate(t, h)
	response := deleteProject(t, h, id, uuid.NewString(), `"r1"`)
	if response.Code != http.StatusOK {
		t.Fatalf("delete = %d %s", response.Code, response.Body.String())
	}
	var body changedResumesBody
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Data.ChangedResumes == nil || len(body.Data.ChangedResumes) != 0 {
		t.Fatalf("delete body = %s, want an empty changed list", response.Body.String())
	}
	stored, err := resume.NewStore(h.pool, docmigrate.NewIdentityProjector()).Get(t.Context(), h.user.ID, untouched.ID)
	if err != nil || stored.Revision != untouched.Revision {
		t.Fatalf("untouched revision = %d, want %d (err %v)", stored.Revision, untouched.Revision, err)
	}
}
