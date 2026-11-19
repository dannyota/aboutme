package publicapi

import (
	"bytes"
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/publiccache"
	"danny.vn/aboutme/apps/server/internal/publicformat"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

func storedEvidence(items []publicresume.PublicEvidence) []map[string]any {
	out := []map[string]any{}
	for _, item := range items {
		out = append(out, map[string]any{
			"kind": item.Kind, "title": item.Title, "url": item.URL, "repositoryName": item.RepositoryName,
			"itemDate": item.ItemDate.Format(time.RFC3339Nano), "checkedAt": item.CheckedAt.Format(time.RFC3339Nano), "confirmed": item.Confirmed,
		})
	}
	return out
}

func projectEvidenceFixture(t *testing.T) *projectPagesStore {
	t.Helper()
	light := schema.Light
	sections := map[string][]projectEntry{"projects": {
		{id: "e-may", projectID: "00000000-0000-0000-0000-00000000f101"},
		{id: "e-difference", projectID: "00000000-0000-0000-0000-00000000f102"},
	}}
	may := corpusEvidenceProject()
	return &projectPagesStore{row: projectResumeRow(t, "vi-VN", &light, schema.MmYyyy, sections, "projects"), rows: []projectStoreRow{
		{entryID: "e-may", owner: projectTestOwner, slug: may.Slug, doc: may.Document, evidence: storedEvidence(may.Evidence)},
		{entryID: "e-difference", owner: projectTestOwner, slug: "difference-engine", doc: namedProject("Difference Engine study")},
	}}
}

func evidenceEnvelope(t *testing.T, renderer *fakeProjectRenderer) directrender.ProjectRenderRequest {
	t.Helper()
	var request directrender.ProjectRenderRequest
	if err := json.Unmarshal(renderer.envelope(directrender.ProjectRenderMode), &request); err != nil {
		t.Fatal(err)
	}
	return request
}

func TestProjectEvidencePages_ServeTheCorpusPage(t *testing.T) {
	readStore := projectEvidenceFixture(t)
	reader, _ := projectReaderWithEvidence(t, readStore, true)
	renderer, client := newFakeProjectRenderer(t)
	handlers := newProjectHandlers(t, reader, client)
	response := handlers.serve(http.MethodGet, "/ada/projects/may-phan-tich")
	if response.Code != http.StatusOK || response.Body.String() != readProjectCorpus(t)["project-vi-evidence"].HTML {
		t.Fatalf("response = %d %q; log %s", response.Code, response.Body.String(), handlers.logs.String())
	}
	requireCorpusEnvelope(t, renderer.envelope(directrender.ProjectRenderMode), "project-vi-evidence")
	if len(readStore.withEvidence) != 1 || !readStore.withEvidence[0] {
		t.Fatalf("statement evidence argument = %v, want [true]", readStore.withEvidence)
	}
}

func TestProjectEvidencePages_FormatVersionThreeIgnoresVersionTwoEntries(t *testing.T) {
	if projectFormatVersion != 3 {
		t.Fatalf("projectFormatVersion = %d, want 3", projectFormatVersion)
	}
	readStore := projectEvidenceFixture(t)
	reader, _ := projectReaderWithEvidence(t, readStore, true)
	renderer, client := newFakeProjectRenderer(t)
	cache, err := publiccache.New(16, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	key := publiccache.Key{
		RouteClass: "project", Representation: publicstate.RepresentationHTML, Variant: "nondiscoverable", ResumeID: readStore.row.ID,
		Generation: readStore.row.Revision, FormatVersion: 2, AppDigest: "sha256:app", RendererDigest: "sha256:renderer", Subject: "may-phan-tich",
	}
	stale := []byte("<!doctype html><title>version 2</title>")
	cache.Put(key, publiccache.Value{Status: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: stale})
	handlers := newProjectHandlersWithCache(t, reader, client, cache)
	response := handlers.serve(http.MethodGet, "/ada/projects/may-phan-tich")
	if response.Code != http.StatusOK || bytes.Equal(response.Body.Bytes(), stale) || renderer.count(directrender.ProjectRenderMode) != 1 {
		t.Fatalf("response = %d %q after %d renders; want a fresh version 3 render", response.Code, response.Body.String(), renderer.count(directrender.ProjectRenderMode))
	}
	key.FormatVersion = projectFormatVersion
	if cached, ok := cache.Get(key); !ok || !bytes.Equal(cached.Body, response.Body.Bytes()) {
		t.Fatal("the page was not cached under format version 3")
	}
}

func TestProjectEvidencePages_ARevisionAdvanceDropsTheMark(t *testing.T) {
	readStore := projectEvidenceFixture(t)
	readStore.row.Revision = 7
	reader, coordinator := projectReaderWithEvidence(t, readStore, true)
	renderer, client := newFakeProjectRenderer(t)
	corpus := readProjectCorpus(t)["project-vi-evidence"].HTML
	renderer.respond = func(string) string { return corpus }
	handlers := newProjectHandlers(t, reader, client)
	if response := handlers.serve(http.MethodGet, "/ada/projects/may-phan-tich"); response.Code != http.StatusOK || !evidenceEnvelope(t, renderer).Evidence[0].Confirmed {
		t.Fatalf("revision 7 = %d, envelope %+v; want the first item confirmed", response.Code, evidenceEnvelope(t, renderer).Evidence)
	}
	readStore.update(func(s *projectPagesStore) {
		for i := range s.rows[0].evidence {
			s.rows[0].evidence[i]["confirmed"] = false
		}
	})
	advanceProjectRevision(t, coordinator, readStore)
	response := handlers.serve(http.MethodGet, "/ada/projects/may-phan-tich")
	if response.Code != http.StatusOK || renderer.count(directrender.ProjectRenderMode) != 2 {
		t.Fatalf("revision 8 = %d after %d renders; want a second render", response.Code, renderer.count(directrender.ProjectRenderMode))
	}
	for _, item := range evidenceEnvelope(t, renderer).Evidence {
		if item.Confirmed {
			t.Fatalf("revision 8 envelope = %+v, want every item unconfirmed", evidenceEnvelope(t, renderer).Evidence)
		}
	}
}

func TestProjectEvidencePages_CapabilityOffSendsAnEmptyArray(t *testing.T) {
	readStore := projectPageFixture(t)
	readStore.rows[0].evidence = storedEvidence(corpusEvidenceProject().Evidence)
	renderer, client := newFakeProjectRenderer(t)
	handlers := newProjectHandlers(t, projectTestReader(t, readStore), client)
	response := handlers.serve(http.MethodGet, "/ada/projects/note-g")
	if response.Code != http.StatusOK || response.Body.String() != readProjectCorpus(t)["project-en-name-only"].HTML {
		t.Fatalf("response = %d; log %s", response.Code, handlers.logs.String())
	}
	if !bytes.Contains(renderer.envelope(directrender.ProjectRenderMode), []byte(`"evidence":[]`)) || len(readStore.withEvidence) != 1 || readStore.withEvidence[0] {
		t.Fatalf("envelope %s, statement argument %v; want [] and false", renderer.envelope(directrender.ProjectRenderMode), readStore.withEvidence)
	}
}

func TestProjectEvidencePages_TheGateCarriesNoEvidence(t *testing.T) {
	readStore := projectEvidenceFixture(t)
	readStore.row.SignInToView, readStore.row.ViewPassEpoch = true, 1
	readStore.rows[0].evidence[0]["kind"] = "gist"
	reader, _ := projectReaderWithEvidence(t, readStore, true)
	renderer, client := newFakeProjectRenderer(t)
	handlers := newProjectHandlers(t, reader, client)
	response := handlers.serve(http.MethodGet, "/ada/projects/may-phan-tich")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `id="public-gate"`) {
		t.Fatalf("response = %d %q, want the gate before any evidence is decoded", response.Code, response.Body.String())
	}
	gate := renderer.envelope(directrender.GateRenderMode)
	for _, leak := range []string{"Thêm ghi chú G", corpusEvidencePull, "evidence", "github.com"} {
		if bytes.Contains(gate, []byte(leak)) || strings.Contains(response.Body.String(), leak) {
			t.Fatalf("the gate carries %q: %s", leak, gate)
		}
	}
}

func TestProjectEvidencePages_UnavailableStatesStill404(t *testing.T) {
	for name, change := range map[string]func(*testing.T, *projectPagesStore){
		"unpublished": func(_ *testing.T, s *projectPagesStore) { s.row.Live = false },
		"deleted":     func(_ *testing.T, s *projectPagesStore) { s.err = pgx.ErrNoRows },
		"hidden entry": func(t *testing.T, s *projectPagesStore) {
			light := schema.Light
			s.row = projectResumeRow(t, "vi-VN", &light, schema.MmYyyy, map[string][]projectEntry{"projects": {
				{id: "e-may", projectID: "00000000-0000-0000-0000-00000000f101", hidden: true},
			}}, "projects")
		},
		"project deleted": func(_ *testing.T, s *projectPagesStore) { s.rows = s.rows[1:] },
	} {
		t.Run(name, func(t *testing.T) {
			readStore := projectEvidenceFixture(t)
			change(t, readStore)
			reader, _ := projectReaderWithEvidence(t, readStore, true)
			renderer, client := newFakeProjectRenderer(t)
			handlers := newProjectHandlers(t, reader, client)
			if response := handlers.serve(http.MethodGet, "/ada/projects/may-phan-tich"); response.Code != http.StatusNotFound {
				t.Fatalf("response = %d, want 404", response.Code)
			}
			if renderer.count(directrender.ProjectRenderMode) != 0 {
				t.Fatal("an unavailable page reached the renderer")
			}
		})
	}
}

type evidenceServiceStore struct {
	*publicServiceStore
	projects json.RawMessage
}

func (s *evidenceServiceStore) GetPublicResumeProject(context.Context, store.GetPublicResumeProjectParams) (store.GetPublicResumeProjectRow, error) {
	return store.GetPublicResumeProjectRow{Resume: s.row, ProjectLinks: linkContext(s.links), Projects: s.projects}, nil
}

func TestPublicRepresentations_CarryNoEvidence(t *testing.T) {
	plain, slug, entryID, _ := linkedPublicStore(t)
	document := map[string]any{"defaultLanguage": "en", "text": map[string]any{"en": map[string]any{"name": "Atlas"}}}
	projects, err := json.Marshal([]map[string]any{{"entryId": entryID, "projectSlug": "atlas-tool", "document": document, "evidence": storedEvidence(corpusEvidenceProject().Evidence)}})
	if err != nil {
		t.Fatal(err)
	}
	plain.links = projects
	leaks := []string{"Thêm ghi chú G", "ada/may-phan-tich", "github.com", "evidence"}
	for _, leak := range leaks {
		if !bytes.Contains(plain.links, []byte(leak)) {
			t.Fatalf("the resume read's link context lacks %q: %s", leak, plain.links)
		}
	}
	backing := &evidenceServiceStore{publicServiceStore: plain, projects: projects}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{
		Store: backing, Projector: docmigrate.NewIdentityProjector(), Coordinator: coordinator, Origin: mustPublicOrigin(t), Evidence: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, read, pageLease, err := reader.ReadProjects(context.Background(), slug, "atlas-tool")
	if err != nil {
		t.Fatal(err)
	}
	pageLease.Release()
	pages, err := read.Pages()
	if err != nil || len(pages.Projects) != 1 || len(pages.Projects[0].Evidence) != 4 {
		t.Fatalf("project page read = %+v %v, want the four stored items", pages, err)
	}
	snapshot, lease, err := reader.ReadResume(context.Background(), slug, publicstate.RepresentationHTML)
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	jsonLD, err := publicformat.JSONLD(snapshot.Public, mustPublicOrigin(t), true)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := publiccache.New(4, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.ProjectLinks) != 1 || snapshot.ProjectLinks[0].ProjectSlug != "atlas-tool" {
		t.Fatalf("resume read links = %+v, want the evidence-bearing link decoded", snapshot.ProjectLinks)
	}
	svc := newPublicServiceForReader(t, reader, coordinator, cache)
	calls := plain.calls
	publicJSON := servePublic(t, svc, "/api/v1/public/resumes/"+slug)
	if !bytes.Contains(publicJSON, []byte(`"projectSlug":"atlas-tool"`)) {
		t.Fatalf("public json = %s, want the project link from the evidence-bearing read", publicJSON)
	}
	for name, body := range map[string][]byte{
		"public json": publicJSON,
		"markdown":    servePublic(t, svc, "/"+slug+".md"),
		"json-ld":     jsonLD.JSON,
	} {
		for _, leak := range leaks {
			if bytes.Contains(body, []byte(leak)) {
				t.Fatalf("%s carries %q: %s", name, leak, body)
			}
		}
	}
	if plain.calls-calls != 2 {
		t.Fatalf("resume reads = %d, want public json and markdown each to read the evidence-bearing row", plain.calls-calls)
	}
}

func TestPublicPackages_DoNotImportGitHubEvidence(t *testing.T) {
	const module = "danny.vn/aboutme/apps/server/"
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	pending := []string{module + "internal/publicapi", module + "internal/publicresume", module + "internal/directrender"}
	for len(pending) > 0 {
		pkg := pending[0]
		pending = pending[1:]
		if seen[pkg] {
			continue
		}
		seen[pkg] = true
		if strings.HasSuffix(pkg, "/internal/githubevidence") {
			t.Fatalf("a public package reaches %s", pkg)
		}
		dir := filepath.Join(root, strings.TrimPrefix(pkg, module))
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, entry.Name()), nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, spec := range file.Imports {
				path, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				if strings.HasPrefix(path, module) {
					pending = append(pending, path)
				}
			}
		}
	}
	if len(seen) < 10 {
		t.Fatalf("walked %d packages, want the public packages' whole module closure", len(seen))
	}
}
