package publicapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/previewcard"
	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/publiccache"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/renderjob"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

var projectTestOwner = uuid.MustParse("00000000-0000-0000-0000-0000000000d1")

// projectStoreRow is one project an entry links, as the project statements
// join it: entry id, the project's owner, slug and document.
type projectStoreRow struct {
	entryID string
	owner   uuid.UUID
	slug    string
	doc     project.Document
}

// projectPagesStore answers the project statements like PostgreSQL for the
// live gate and the owner join. It does not drop hidden entries, so the
// reader's own availability rule is what the handler tests prove.
type projectPagesStore struct {
	mu    sync.Mutex
	row   store.Resume
	rows  []projectStoreRow
	err   error
	calls int
}

func (s *projectPagesStore) GetPublicState(context.Context) (store.PublicState, error) {
	return store.PublicState{Singleton: true, DiscoveryGeneration: 1}, nil
}

// GetPublicResumeBySlug finds no resume, so the resume page handler over
// this store answers every slug as an unknown resume.
func (s *projectPagesStore) GetPublicResumeBySlug(context.Context, string) (store.GetPublicResumeBySlugRow, error) {
	return store.GetPublicResumeBySlugRow{}, pgx.ErrNoRows
}
func (s *projectPagesStore) GetPublicResumeByOwner(context.Context, store.GetPublicResumeByOwnerParams) (store.Resume, error) {
	return store.Resume{}, errors.New("unused")
}
func (s *projectPagesStore) ListEligiblePublicSlugs(context.Context) ([]string, error) {
	return nil, errors.New("unused")
}

func (s *projectPagesStore) live(slug string) (store.Resume, []projectStoreRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.err != nil {
		return store.Resume{}, nil, s.err
	}
	if s.row.Slug == nil || *s.row.Slug != slug || !s.row.Live {
		return store.Resume{}, nil, pgx.ErrNoRows
	}
	var owned []projectStoreRow
	for _, row := range s.rows {
		if row.owner == s.row.UserID {
			owned = append(owned, row)
		}
	}
	return s.row, owned, nil
}

func (s *projectPagesStore) GetPublicResumeProjectList(_ context.Context, slug string) (store.GetPublicResumeProjectListRow, error) {
	resume, rows, err := s.live(slug)
	if err != nil {
		return store.GetPublicResumeProjectListRow{}, err
	}
	column := []map[string]any{}
	for _, row := range rows {
		document := row.doc
		document.Text = map[string]project.Text{}
		for key, text := range row.doc.Text {
			text.Description = ""
			document.Text[key] = text
		}
		column = append(column, map[string]any{"entryId": row.entryID, "projectSlug": row.slug, "document": document})
	}
	raw, err := json.Marshal(column)
	return store.GetPublicResumeProjectListRow{Resume: resume, Projects: raw}, err
}

func (s *projectPagesStore) GetPublicResumeProject(_ context.Context, arg store.GetPublicResumeProjectParams) (store.GetPublicResumeProjectRow, error) {
	resume, rows, err := s.live(arg.Slug)
	if err != nil {
		return store.GetPublicResumeProjectRow{}, err
	}
	links, projects := []map[string]any{}, []map[string]any{}
	for _, row := range rows {
		links = append(links, map[string]any{"entryId": row.entryID, "projectSlug": row.slug})
		if row.slug == arg.ProjectSlug {
			projects = append(projects, map[string]any{"entryId": row.entryID, "projectSlug": row.slug, "document": row.doc})
		}
	}
	rawLinks, err := json.Marshal(links)
	if err != nil {
		return store.GetPublicResumeProjectRow{}, err
	}
	rawProjects, err := json.Marshal(projects)
	return store.GetPublicResumeProjectRow{Resume: resume, ProjectLinks: rawLinks, Projects: rawProjects}, err
}

func (s *projectPagesStore) update(change func(*projectPagesStore)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	change(s)
}

// projectEntry is one project entry of a test resume; projectID "" leaves
// it unlinked.
type projectEntry struct {
	id, projectID string
	hidden        bool
}

// projectResumeRow is a live resume whose sections hold project entries;
// layout names the sections in the main column.
func projectResumeRow(t *testing.T, lng string, scheme *schema.ColorScheme, dateFormat schema.DateFormat, sections map[string][]projectEntry, layout ...string) store.Resume {
	t.Helper()
	slug, name := "ada", "Ada Lovelace"
	content := map[string]schema.Section{}
	for key, entries := range sections {
		var projectEntries []schema.ProjectEntry
		for _, entry := range entries {
			title := entry.id
			value := schema.ProjectEntry{ID: entry.id, Title: &title}
			if entry.projectID != "" {
				projectID := entry.projectID
				value.ProjectID = &projectID
			}
			if entry.hidden {
				hidden := true
				value.IsHidden = &hidden
			}
			projectEntries = append(projectEntries, value)
		}
		content[key] = schema.Section{SectionType: schema.Project, ProjectEntries: projectEntries}
	}
	var customization schema.Customization
	customization.ColorScheme, customization.DateFormat, customization.Layout.Sections.Main = scheme, dateFormat, layout
	marshal := func(value any) []byte {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	return store.Resume{
		ID: uuid.MustParse("00000000-0000-0000-0000-0000000000d2"), UserID: projectTestOwner, Slug: &slug, Live: true, Revision: 1, Lng: &lng,
		SchemaVersion: int32(schema.CurrentVersion), PersonalDetails: marshal(schema.PersonalDetails{FullName: &name}),
		Content: marshal(content), Customization: marshal(customization),
	}
}

func projectTestReader(t *testing.T, readStore *projectPagesStore) *publicresume.Reader {
	t.Helper()
	reader, _ := projectTestReaderAndCoordinator(t, readStore)
	return reader
}

func projectTestReaderAndCoordinator(t *testing.T, readStore *projectPagesStore) (*publicresume.Reader, *publicstate.Coordinator) {
	t.Helper()
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{Store: readStore, Projector: docmigrate.NewIdentityProjector(), Coordinator: coordinator, Origin: mustPublicOrigin(t)})
	if err != nil {
		t.Fatal(err)
	}
	return reader, coordinator
}

// advanceProjectRevision commits a non-draining transition of the store's
// resume to the next revision, as a library or resume write does.
func advanceProjectRevision(t *testing.T, coordinator *publicstate.Coordinator, readStore *projectPagesStore) {
	t.Helper()
	readStore.mu.Lock()
	id, revision := readStore.row.ID, readStore.row.Revision
	readStore.mu.Unlock()
	transition, err := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: id, ExpectedRevision: revision, Class: publicstate.NonDraining}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := transition.Close(context.Background(), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	readStore.update(func(s *projectPagesStore) { s.row.Revision = revision + 1 })
	if err := transition.Commit(publicstate.CommittedState{ResumeRevisions: map[uuid.UUID]int64{id: revision + 1}}); err != nil {
		t.Fatal(err)
	}
}

// projectTestCards names the corpus card version for every snapshot.
type projectTestCards struct{}

func (projectTestCards) Version(publicresume.Snapshot) (string, error) {
	return "0123456789abcdef", nil
}
func (projectTestCards) Stored(context.Context, uuid.UUID) (previewcard.StoredCard, bool, error) {
	return previewcard.StoredCard{}, false, errors.New("unused")
}
func (projectTestCards) Build(context.Context, publicresume.Snapshot, renderjob.Priority) ([]byte, error) {
	return nil, errors.New("unused")
}

// countingViews counts every observed view.
type countingViews struct{ count int }

func (v *countingViews) ObserveHTML(context.Context, uuid.UUID, string) { v.count++ }

// fakeProjectRenderer is an httptest server standing in for the renderer.
// It answers a project envelope of the corpus with that case's HTML, any
// other project page with the name-only case's HTML renamed to it, and a
// gate envelope with gateValidHTML.
type fakeProjectRenderer struct {
	t         *testing.T
	mu        sync.Mutex
	calls     map[string]int
	envelopes map[string][]byte
	// mutate, when set, changes the HTML of every project page answer.
	mutate func(string) string
}

func newFakeProjectRenderer(t *testing.T) (*fakeProjectRenderer, *directrender.Client) {
	t.Helper()
	fake := &fakeProjectRenderer{t: t, calls: map[string]int{}, envelopes: map[string][]byte{}}
	server := httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(server.Close)
	origin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	transport := htmlRoundTrip(func(request *http.Request) (*http.Response, error) {
		forwarded := request.Clone(request.Context())
		forwarded.URL.Host, forwarded.Host = server.Listener.Addr().String(), server.Listener.Addr().String()
		return http.DefaultTransport.RoundTrip(forwarded)
	})
	return fake, directrender.New(origin, &http.Client{Transport: transport})
}

func (f *fakeProjectRenderer) serve(w http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(w, "read", http.StatusInternalServerError)
		return
	}
	var head struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(body, &head); err != nil {
		http.Error(w, "mode", http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.calls[head.Mode]++
	f.envelopes[head.Mode] = body
	mutate := f.mutate
	f.mu.Unlock()
	page := f.page(head.Mode, body)
	if mutate != nil && head.Mode != directrender.GateRenderMode {
		page = mutate(page)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write([]byte(page)); err != nil {
		f.t.Errorf("fake renderer write: %v", err)
	}
}

func (f *fakeProjectRenderer) page(mode string, body []byte) string {
	if mode == directrender.GateRenderMode {
		var envelope directrender.GateRenderRequest
		if err := json.Unmarshal(body, &envelope); err != nil {
			return ""
		}
		return gateValidHTML(envelope, mustPublicOrigin(f.t))
	}
	var got any
	if err := json.Unmarshal(body, &got); err != nil {
		return ""
	}
	corpus := readProjectCorpus(f.t)
	for _, tc := range corpus {
		var want any
		if err := json.Unmarshal(tc.Envelope, &want); err == nil && reflect.DeepEqual(got, want) {
			return tc.HTML
		}
	}
	var request directrender.ProjectRenderRequest
	if err := json.Unmarshal(body, &request); err != nil {
		return ""
	}
	return strings.ReplaceAll(strings.ReplaceAll(corpus["project-en-name-only"].HTML, "note-g", request.Project.Slug), "Note G", request.Project.Name)
}

func (f *fakeProjectRenderer) count(mode string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[mode]
}

func (f *fakeProjectRenderer) envelope(mode string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return bytes.Clone(f.envelopes[mode])
}

// projectHandlers builds both project handlers and the resume page handler
// over one reader, cache and renderer.
type projectHandlers struct {
	list, page, resume http.Handler
	views              *countingViews
	logs               *bytes.Buffer
}

func newProjectHandlers(t *testing.T, reader *publicresume.Reader, renderer *directrender.Client) projectHandlers {
	t.Helper()
	cache, err := publiccache.New(16, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	logger, logs := newGateCapturingLogger()
	views := &countingViews{}
	dependencies := HTMLDependencies{
		Reader: reader, Cache: cache, Renderer: renderer, PublicOrigin: mustPublicOrigin(t), AppDigest: "sha256:app", RendererDigest: "sha256:renderer",
		Logger: logger, Cards: projectTestCards{}, Views: views, ViewPassKey: gateTestKey, GateProviders: []string{"google", "linkedin"}, JoinInviteTarget: "/register",
	}
	list, err := NewProjectListHandler(dependencies)
	if err != nil {
		t.Fatal(err)
	}
	page, err := NewProjectHandler(dependencies)
	if err != nil {
		t.Fatal(err)
	}
	resume, err := NewHTMLHandler(dependencies)
	if err != nil {
		t.Fatal(err)
	}
	return projectHandlers{list: list, page: page, resume: resume, views: views, logs: logs}
}

func (h projectHandlers) serve(method, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequestWithContext(context.Background(), method, path, nil)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	handler := h.page
	if strings.HasSuffix(strings.SplitN(path, "?", 2)[0], "/projects") {
		handler = h.list
	}
	handler.ServeHTTP(recorder, request)
	return recorder
}
