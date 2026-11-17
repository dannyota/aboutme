package githubevidence

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/projectapi"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
	"danny.vn/aboutme/apps/server/internal/store"
)

const evidenceRoot = "/api/v1/projects/"

func (gh *stubGitHub) serveRead(w http.ResponseWriter, r *http.Request) {
	gh.mu.Lock()
	gh.readLog = append(gh.readLog, r.URL.RequestURI())
	handle := gh.reads[r.URL.EscapedPath()]
	gh.mu.Unlock()
	if handle == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	handle(w, r)
}

func (gh *stubGitHub) read(path string, handle http.HandlerFunc) {
	gh.mu.Lock()
	defer gh.mu.Unlock()
	if gh.reads == nil {
		gh.reads = map[string]http.HandlerFunc{}
	}
	gh.reads[path] = handle
}

func (gh *stubGitHub) readJSON(path string, status int, body string) {
	gh.read(path, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		gh.write(w, body)
	})
}

func (gh *stubGitHub) readCalls() []string {
	gh.mu.Lock()
	defer gh.mu.Unlock()
	return append([]string(nil), gh.readLog...)
}

type evidenceHarness struct {
	*harness
	projects    *projectapi.Service
	resumes     *resume.Store
	coordinator *publicstate.Coordinator
	project     project.Project
}

func newEvidenceHarness(t *testing.T) *evidenceHarness {
	t.Helper()
	h := newHarness(t)
	eh := wireEvidence(t, h, h.svc)
	eh.project = eh.createProject("atlas")
	return eh
}

func newLibrary(t *testing.T, pool *store.Pool, sessions *auth.SessionManager) (*resumeapi.Service, *publicstate.Coordinator) {
	t.Helper()
	public, err := store.New(pool).GetPublicState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: public.DiscoveryGeneration})
	if err != nil {
		t.Fatal(err)
	}
	projector := docmigrate.NewIdentityProjector()
	library := resumeapi.New(resume.NewStore(pool, projector), resume.NewIdempotencyStore(pool), projector, nil, resumeapi.Options{
		SessionManager: sessions, PublicOrigin: testOrigin, Coordinator: coordinator, RecoveryPool: pool,
	})
	return library, coordinator
}

func wireEvidence(t *testing.T, h *harness, source projectapi.EvidenceSource) *evidenceHarness {
	t.Helper()
	projector := docmigrate.NewIdentityProjector()
	pass := func(next http.Handler) http.Handler { return next }
	projects := projectapi.New(h.pool, resume.NewIdempotencyStore(h.pool), projectapi.Options{
		Sessions: h.sessions, PublicOrigin: testOrigin, ReadLimit: pass, WriteLimit: pass,
		AllowSlug: func(uuid.UUID, time.Time) bool { return true }, Links: h.library, Evidence: source,
	})
	register := []func(*http.ServeMux){h.auth.RegisterRoutes, projects.RegisterRoutes}
	if svc, ok := source.(*Service); ok {
		register = append(register, svc.RegisterRoutes)
	}
	h.handler = api.New(slog.New(slog.NewJSONHandler(io.Discard, nil)), noopPinger{}, api.Options{}, nil, register...)
	return &evidenceHarness{harness: h, projects: projects, resumes: resume.NewStore(h.pool, projector), coordinator: h.coordinator}
}

func (eh *evidenceHarness) createProject(slug string) project.Project {
	eh.t.Helper()
	d := project.Document{DefaultLanguage: "en", Text: map[string]project.Text{"en": {Name: "Atlas"}}}
	created, err := project.NewStore().CreateTx(eh.t.Context(), eh.q, eh.user.ID, slug, d, time.Now(), nil)
	if err != nil {
		eh.t.Fatal(err)
	}
	return created
}

func (eh *evidenceHarness) revision() int64 {
	eh.t.Helper()
	current, err := project.NewStore().GetTx(eh.t.Context(), eh.q, eh.user.ID, eh.project.ID)
	if err != nil {
		eh.t.Fatal(err)
	}
	return current.Revision
}

func (eh *evidenceHarness) path(suffix string) string {
	return evidenceRoot + eh.project.ID.String() + "/evidence" + suffix
}

func (eh *evidenceHarness) write(method, suffix, body, key string, revision int64) *httptest.ResponseRecorder {
	eh.t.Helper()
	c := eh.as(method, eh.path(suffix))
	c.body = body
	c.header = http.Header{"Idempotency-Key": {key}, "If-Match": {`"r` + strconv.FormatInt(revision, 10) + `"`}}
	if body != "" {
		c.header.Set("Content-Type", "application/json")
	}
	return eh.do(c)
}

func (eh *evidenceHarness) attach(body string) *httptest.ResponseRecorder {
	eh.t.Helper()
	return eh.write(http.MethodPost, "", body, uuid.NewString(), eh.revision())
}

type evidenceItem struct {
	ID             string    `json:"id"`
	Kind           string    `json:"kind"`
	URL            string    `json:"url"`
	RepositoryName string    `json:"repositoryName"`
	Title          string    `json:"title"`
	ItemDate       time.Time `json:"itemDate"`
	CheckedAt      time.Time `json:"checkedAt"`
	Confirmed      bool      `json:"confirmed"`
	FlagReason     *string   `json:"flagReason"`
}

type revisionOf struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
}

type evidenceWrite struct {
	Evidence        []evidenceItem `json:"evidence"`
	ProjectRevision string         `json:"projectRevision"`
	ResumeRevisions []revisionOf   `json:"resumeRevisions"`
}

func decodeData[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	return envelope.Data
}

func requireWrite(t *testing.T, rec *httptest.ResponseRecorder) evidenceWrite {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("evidence write = %d %s, want 200", rec.Code, rec.Body.String())
	}
	return decodeData[evidenceWrite](t, rec)
}

func requireError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil || rec.Code != status || envelope.Error.Code != code {
		t.Fatalf("response = %d %s, want %d %s", rec.Code, rec.Body.String(), status, code)
	}
}

func (eh *evidenceHarness) stored() []store.ProjectEvidence {
	eh.t.Helper()
	rows, err := eh.pool.Query(eh.t.Context(), `SELECT id, project_id, position, kind, repository_id, item_id, pull_number, release_tag, url,
		repository_name, title, item_date, checked_at, checked_binding, rule_met, flag_reason FROM project_evidence WHERE project_id = $1 ORDER BY position`, eh.project.ID)
	if err != nil {
		eh.t.Fatal(err)
	}
	defer rows.Close()
	var out []store.ProjectEvidence
	for rows.Next() {
		var e store.ProjectEvidence
		if err = rows.Scan(&e.ID, &e.ProjectID, &e.Position, &e.Kind, &e.RepositoryID, &e.ItemID, &e.PullNumber, &e.ReleaseTag, &e.Url,
			&e.RepositoryName, &e.Title, &e.ItemDate, &e.CheckedAt, &e.CheckedBinding, &e.RuleMet, &e.FlagReason); err != nil {
			eh.t.Fatal(err)
		}
		out = append(out, e)
	}
	if err = rows.Err(); err != nil {
		eh.t.Fatal(err)
	}
	return out
}

func (eh *evidenceHarness) seedEvidence(n int) {
	eh.t.Helper()
	offset := len(eh.stored())
	for i := offset; i < offset+n; i++ {
		id := int64(500000 + i)
		if _, err := eh.pool.Exec(eh.t.Context(), `INSERT INTO project_evidence (project_id, position, kind, repository_id, item_id, url,
			repository_name, title, item_date, checked_at, checked_binding, rule_met)
			SELECT $1, COALESCE(max(position) + 1, 0), 'repository', $2, $2, $3, $4, $4, now(), now(), 1, true
			FROM project_evidence WHERE project_id = $1`,
			eh.project.ID, id, fmt.Sprintf("https://github.com/octocat/seed-%d", i), fmt.Sprintf("octocat/seed-%d", i)); err != nil {
			eh.t.Fatal(err)
		}
	}
}

func (eh *evidenceHarness) readProject() map[string]json.RawMessage {
	eh.t.Helper()
	rec := eh.do(eh.as(http.MethodGet, evidenceRoot+eh.project.ID.String()))
	if rec.Code != http.StatusOK {
		eh.t.Fatalf("GET project = %d %s", rec.Code, rec.Body.String())
	}
	return decodeData[map[string]json.RawMessage](eh.t, rec)
}

func (eh *evidenceHarness) projectEvidence() []evidenceItem {
	eh.t.Helper()
	raw, present := eh.readProject()["evidence"]
	if !present {
		eh.t.Fatal("GET project carries no evidence field")
	}
	var items []evidenceItem
	if err := json.Unmarshal(raw, &items); err != nil {
		eh.t.Fatal(err)
	}
	return items
}

func (eh *evidenceHarness) linkingResume(live bool) resume.Resume {
	eh.t.Helper()
	ctx := context.Background()
	created, err := eh.resumes.Create(ctx, eh.user.ID, "Linking", documentForDeletion())
	if err != nil {
		eh.t.Fatal(err)
	}
	doc := created.Doc
	title := "Atlas"
	projectID := eh.project.ID.String()
	doc.Content["projects"] = schema.Section{SectionType: schema.Project, ProjectEntries: []schema.ProjectEntry{
		{ID: uuid.NewString(), Title: &title, ProjectID: &projectID},
	}}
	doc.Customization.Layout.Sections.Main = append(doc.Customization.Layout.Sections.Main, "projects")
	if _, err = eh.resumes.SaveDocument(ctx, eh.user.ID, created.ID, doc, created.Revision); err != nil {
		eh.t.Fatalf("link the resume: %v", err)
	}
	if live {
		if _, err = eh.pool.Exec(ctx, `UPDATE resumes SET slug = $2, live = true, revision = revision + 1 WHERE id = $1`,
			created.ID, "evidence-"+uuid.NewString()[:8]); err != nil {
			eh.t.Fatal(err)
		}
	}
	stored, err := eh.resumes.Get(ctx, eh.user.ID, created.ID)
	if err != nil {
		eh.t.Fatal(err)
	}
	return stored
}

func (eh *evidenceHarness) resumeRevision(id uuid.UUID) int64 {
	eh.t.Helper()
	stored, err := eh.resumes.Get(context.Background(), eh.user.ID, id)
	if err != nil {
		eh.t.Fatal(err)
	}
	return stored.Revision
}

func (eh *evidenceHarness) servePullRequest() {
	eh.gh.readJSON("/repos/octocat/hello", http.StatusOK, repositoryJSON)
	eh.gh.readJSON("/repos/octocat/hello/pulls/42", http.StatusOK, pullRequestJSON)
}

const pullRequestLink = `{"kind":"pullRequest","link":"https://github.com/octocat/hello/pull/42"}`
