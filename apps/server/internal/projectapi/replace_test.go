package projectapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/publicapi"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

func putProject(t *testing.T, h *harness, id, body string, revision int64) *httptest.ResponseRecorder {
	t.Helper()
	return h.request(t, http.MethodPut, "/api/v1/projects/"+id, body, uuid.NewString(), revisionTag(revision))
}

func requireChanged(t *testing.T, r *httptest.ResponseRecorder, want map[uuid.UUID]int64) {
	t.Helper()
	if r.Code != http.StatusOK {
		t.Fatalf("replace = %d %s", r.Code, r.Body.String())
	}
	var body changedResumesBody
	if err := json.Unmarshal(r.Body.Bytes(), &body); err != nil || body.Data.ChangedResumes == nil {
		t.Fatalf("replace body = %s (%v), want a changedResumes list", r.Body.String(), err)
	}
	got := map[uuid.UUID]int64{}
	for _, item := range body.Data.ChangedResumes {
		revision, err := strconv.ParseInt(item.Revision, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		got[item.ID] = revision
	}
	if len(body.Data.ChangedResumes) != len(want) || !reflect.DeepEqual(got, want) {
		t.Fatalf("changed resumes = %v, want %v", got, want)
	}
}

func storedRow(t *testing.T, h *harness, id uuid.UUID) store.Resume {
	t.Helper()
	row, err := h.q.GetResumeForUser(t.Context(), store.GetResumeForUserParams{ID: id, UserID: h.user.ID})
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func requireRevision(t *testing.T, h *harness, item resume.Resume, want int64) {
	t.Helper()
	if got := storedRow(t, h, item.ID).Revision; got != want {
		t.Fatalf("resume %s revision = %d, want %d", item.ID, got, want)
	}
}

func holdPublicLease(t *testing.T, h *harness, item resume.Resume) (*publicstate.Lease, *atomic.Bool) {
	t.Helper()
	lease, err := h.coordinator.AcquireResume(t.Context(), item.ID, item.Revision, publicstate.RepresentationHTML)
	if err != nil {
		t.Fatal(err)
	}
	revoked := &atomic.Bool{}
	if err = lease.OnCancel(func() { revoked.Store(true); lease.Release() }); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lease.Release)
	return lease, revoked
}

func requireFenceAt(t *testing.T, h *harness, id uuid.UUID, revision int64) {
	t.Helper()
	lease, err := h.coordinator.AcquireResume(t.Context(), id, revision, publicstate.RepresentationJSON)
	if err != nil {
		t.Fatalf("fence for %s does not admit revision %d: %v", id, revision, err)
	}
	lease.Release()
	if stale, staleErr := h.coordinator.AcquireResume(t.Context(), id, revision-1, publicstate.RepresentationJSON); staleErr == nil {
		stale.Release()
		t.Fatalf("fence for %s still admits revision %d", id, revision-1)
	}
}

func publicJSON(t *testing.T, h *harness, slug string) (publicresume.Snapshot, []byte) {
	t.Helper()
	publicOrigin, err := publicresume.ParsePublicOrigin(origin, "production")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{
		Store: h.q, Projector: docmigrate.NewIdentityProjector(), Coordinator: h.coordinator, Origin: publicOrigin,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, lease, err := reader.ReadResume(t.Context(), slug, publicstate.RepresentationJSON)
	if err != nil {
		t.Fatalf("read public resume %s: %v", slug, err)
	}
	defer lease.Release()
	response, err := publicapi.NewPublicJSON(snapshot.Public, snapshot.ProjectLinks)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, response.Body
}

func linkedEntryID(t *testing.T, item resume.Resume) string {
	t.Helper()
	for _, entry := range item.Doc.Content["projects"].ProjectEntries {
		if entry.ProjectID != nil {
			return entry.ID
		}
	}
	t.Fatal("resume has no linked entry")
	return ""
}

func TestProjectReplace_UnchangedStoredFormsAdvanceNoResume(t *testing.T) {
	h := newHarness(t)
	id := idOf(t, h.create(t))
	live := seedLinkedResume(t, h, uuid.MustParse(id), "same-"+uuid.NewString()[:8])
	draft := seedLinkedResume(t, h, uuid.MustParse(id), "")
	for i, body := range []string{
		projectBody,
		`{ "text": { "en": { "description": "<p onmouseover=\"other()\">Work<script>other()</script></p>", "name": "Example" } },
		   "defaultLanguage": "en", "slug": "example" }`,
	} {
		r := putProject(t, h, id, body, int64(i+1))
		requireChanged(t, r, map[uuid.UUID]int64{})
		if got, want := r.Header().Get("ETag"), revisionTag(int64(i+2)); got != want {
			t.Fatalf("replace %d ETag = %s, want %s: the project revision always advances", i, got, want)
		}
	}
	requireRevision(t, h, live, live.Revision)
	requireRevision(t, h, draft, draft.Revision)
}

func TestProjectReplace_DocumentChangeAdvancesLinkingLiveResumesWithoutDraining(t *testing.T) {
	h := newHarness(t)
	id := idOf(t, h.create(t))
	live := seedLinkedResume(t, h, uuid.MustParse(id), "edit-"+uuid.NewString()[:8])
	draft := seedLinkedResume(t, h, uuid.MustParse(id), "")
	unrelated := seedLinkedResume(t, h, uuid.MustParse(createOther(t, h)), "other-"+uuid.NewString()[:8])
	before := storedRow(t, h, live.ID)
	lease, revoked := holdPublicLease(t, h, live)

	r := putProject(t, h, id, strings.Replace(projectBody, `"name":"Example"`, `"name":"Example, renamed"`, 1), 1)
	requireChanged(t, r, map[uuid.UUID]int64{live.ID: live.Revision + 1})
	if revoked.Load() || lease.Context().Err() != nil {
		t.Fatal("a document change drained an admitted public request")
	}
	after := storedRow(t, h, live.ID)
	if after.Revision != before.Revision+1 || !after.UpdatedAt.Equal(before.UpdatedAt) || after.SchemaVersion != before.SchemaVersion ||
		!bytes.Equal(after.PersonalDetails, before.PersonalDetails) || !bytes.Equal(after.Content, before.Content) || !bytes.Equal(after.Customization, before.Customization) {
		t.Fatalf("advanced row = revision %d updated %v, want revision %d with updated_at %v and the document unchanged",
			after.Revision, after.UpdatedAt, before.Revision+1, before.UpdatedAt)
	}
	requireRevision(t, h, draft, draft.Revision)
	requireRevision(t, h, unrelated, unrelated.Revision)
	lease.Release()
	requireFenceAt(t, h, live.ID, live.Revision+1)
}

func TestProjectReplace_SlugChangeRevokesLinkingLiveResumesAndMovesTheLink(t *testing.T) {
	h := newHarness(t)
	id := idOf(t, h.create(t))
	liveSlug := "move-" + uuid.NewString()[:8]
	live := seedLinkedResume(t, h, uuid.MustParse(id), liveSlug)
	draft := seedLinkedResume(t, h, uuid.MustParse(id), "")
	reauthenticate(t, h)
	_, revoked := holdPublicLease(t, h, live)

	r := putProject(t, h, id, strings.Replace(projectBody, `"slug":"example"`, `"slug":"renamed"`, 1), 1)
	requireChanged(t, r, map[uuid.UUID]int64{live.ID: live.Revision + 1})
	if !revoked.Load() {
		t.Fatal("a slug change kept an admitted public request of the old page address")
	}
	requireRevision(t, h, draft, draft.Revision)
	requireFenceAt(t, h, live.ID, live.Revision+1)
	if _, err := h.q.GetProjectSlugHold(t.Context(), store.GetProjectSlugHoldParams{UserID: h.user.ID, Slug: "example"}); err != nil {
		t.Fatalf("old slug hold: %v", err)
	}

	snapshot, body := publicJSON(t, h, liveSlug)
	want := []publicresume.ProjectLink{{EntryID: linkedEntryID(t, live), ProjectSlug: "renamed", URL: origin + "/" + liveSlug + "/projects/renamed"}}
	if snapshot.Revision != live.Revision+1 || !reflect.DeepEqual(snapshot.ProjectLinks, want) {
		t.Fatalf("public read = revision %d links %+v, want revision %d links %+v", snapshot.Revision, snapshot.ProjectLinks, live.Revision+1, want)
	}
	if bytes.Contains(body, []byte(id)) {
		t.Fatalf("public JSON carries the project id: %s", body)
	}
}

func TestPublicJSON_FromTheDatabaseLinksOnlyVisibleOwnedProjects(t *testing.T) {
	h := newHarness(t)
	shown := idOf(t, h.create(t))
	hiddenProject := createOther(t, h)
	foreign := idOf(t, newHarness(t).create(t))
	title, hidden := "Entry", true
	entry := func(project string) schema.ProjectEntry {
		e := schema.ProjectEntry{ID: uuid.NewString(), Title: &title}
		if project != "" {
			e.ProjectID = &project
		}
		return e
	}
	linked, hiddenEntry, unlinked := entry(shown), entry(hiddenProject), entry("")
	hiddenEntry.IsHidden = &hidden
	doc := minimalResumeDocument(t)
	doc.Content["projects"] = schema.Section{SectionType: schema.Project, ProjectEntries: []schema.ProjectEntry{linked, hiddenEntry, unlinked}}
	doc.Customization.Layout.Sections.Main = []string{"projects"}
	created, err := resume.NewStore(h.pool, docmigrate.NewIdentityProjector()).Create(t.Context(), h.user.ID, "links", doc)
	if err != nil {
		t.Fatal(err)
	}
	slug := "links-" + uuid.NewString()[:8]
	foreignEntry := `[{"id":"` + uuid.NewString() + `","title":"Foreign","projectId":"` + foreign + `"}]`
	if _, err = h.pool.Exec(t.Context(), `UPDATE resumes SET slug = $2, live = true, revision = revision + 1,
		content = jsonb_set(content, '{projects,entries}', (content->'projects'->'entries') || $3::jsonb) WHERE id = $1`,
		created.ID, slug, foreignEntry); err != nil {
		t.Fatal(err)
	}

	snapshot, body := publicJSON(t, h, slug)
	want := []publicresume.ProjectLink{{EntryID: linked.ID, ProjectSlug: "example", URL: origin + "/" + slug + "/projects/example"}}
	if !reflect.DeepEqual(snapshot.ProjectLinks, want) {
		t.Fatalf("project links = %+v, want %+v", snapshot.ProjectLinks, want)
	}
	for _, leak := range []string{shown, hiddenProject, foreign, `"projectId"`} {
		if bytes.Contains(body, []byte(leak)) {
			t.Fatalf("public JSON carries %s: %s", leak, body)
		}
	}
}
