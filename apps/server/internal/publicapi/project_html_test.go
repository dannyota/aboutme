package publicapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/publicformat"
	"danny.vn/aboutme/apps/server/internal/viewpass"
)

// projectPageSections is the name-only corpus page's resume: a project
// section with the linked note-g, a hidden linked entry, an unlinked entry
// and an entry linking another account's project, and a linked entry in a
// section outside the layout.
func projectPageSections() map[string][]projectEntry {
	return map[string][]projectEntry{
		"projects": {
			{id: "e-note-g", projectID: "00000000-0000-0000-0000-00000000e001"},
			{id: "e-hidden", projectID: "00000000-0000-0000-0000-00000000e002", hidden: true},
			{id: "e-unlinked"},
			{id: "e-foreign", projectID: "00000000-0000-0000-0000-00000000e004"},
		},
		"outside": {{id: "e-outside", projectID: "00000000-0000-0000-0000-00000000e003"}},
	}
}

func namedProject(name string) project.Document {
	return project.Document{DefaultLanguage: "en", Text: map[string]project.Text{"en": {Name: name}}}
}

func projectPageRows() []projectStoreRow {
	return []projectStoreRow{
		{entryID: "e-note-g", owner: projectTestOwner, slug: "note-g", doc: namedProject("Note G")},
		{entryID: "e-hidden", owner: projectTestOwner, slug: "hidden-tool", doc: namedProject("Hidden")},
		{entryID: "e-outside", owner: projectTestOwner, slug: "outside-tool", doc: namedProject("Outside")},
		{entryID: "e-foreign", owner: uuid.MustParse("00000000-0000-0000-0000-0000000000d9"), slug: "foreign-tool", doc: namedProject("Foreign")},
	}
}

// projectPageFixture is the resume behind the name-only corpus page.
func projectPageFixture(t *testing.T) *projectPagesStore {
	t.Helper()
	return &projectPagesStore{row: projectResumeRow(t, "en", nil, schema.MmYyyy, projectPageSections(), "projects"), rows: projectPageRows()}
}

// projectListFixture is the resume behind the Vietnamese corpus list.
func projectListFixture(t *testing.T) *projectPagesStore {
	t.Helper()
	system := schema.System
	sections := map[string][]projectEntry{"projects": {
		{id: "e-may", projectID: "00000000-0000-0000-0000-00000000f001"},
		{id: "e-difference", projectID: "00000000-0000-0000-0000-00000000f002"},
	}}
	may := project.Document{
		Dates:        &schema.DateRange{Start: schema.YearMonth{Y: 2019}, End: &schema.YearMonth{Y: 2021}},
		Technologies: []string{"Toán học", "Bìa đục lỗ", "Thuật toán", "Số Bernoulli", "Dịch thuật", "Ghi chú", "Lịch sử"}, DefaultLanguage: "vi",
		Text: map[string]project.Text{"vi": {Name: "Máy phân tích", Role: "Tác giả", Summary: "Ghi chú về máy tính cơ học đầu tiên.", Description: "<p>Mô tả</p>"}},
	}
	return &projectPagesStore{row: projectResumeRow(t, "vi-VN", &system, schema.Yyyy, sections, "projects"), rows: []projectStoreRow{
		{entryID: "e-may", owner: projectTestOwner, slug: "may-phan-tich", doc: may},
		{entryID: "e-difference", owner: projectTestOwner, slug: "difference-engine", doc: namedProject("Difference Engine study")},
	}}
}

func requireCorpusEnvelope(t *testing.T, got []byte, name string) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(readProjectCorpus(t)[name].Envelope, &wantValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("render request differs from corpus %s:\n got %s", name, got)
	}
}

func TestProjectPages_ServeTheCorpusPages(t *testing.T) {
	corpus := readProjectCorpus(t)
	for _, test := range []struct {
		name, path, corpus, mode string
		store                    func(*testing.T) *projectPagesStore
	}{
		{"list", "/ada/projects", "list-vi-two-projects", directrender.ProjectListRenderMode, projectListFixture},
		{"page", "/ada/projects/note-g", "project-en-name-only", directrender.ProjectRenderMode, projectPageFixture},
	} {
		t.Run(test.name, func(t *testing.T) {
			renderer, client := newFakeProjectRenderer(t)
			handlers := newProjectHandlers(t, projectTestReader(t, test.store(t)), client)
			response := handlers.serve(http.MethodGet, test.path)
			if response.Code != http.StatusOK || response.Body.String() != corpus[test.corpus].HTML {
				t.Fatalf("response = %d %q; log %s", response.Code, response.Body.String(), handlers.logs.String())
			}
			requireCorpusEnvelope(t, renderer.envelope(test.mode), test.corpus)
			header := response.Header()
			if header.Get("Cache-Control") != "no-cache, must-revalidate" || header.Get("Content-Security-Policy") != publicformat.BaseCSP ||
				header.Get("X-Robots-Tag") != "noindex, noarchive" || header.Get("Content-Type") != "text/html; charset=utf-8" || header.Get("ETag") == "" {
				t.Fatalf("headers = %#v", header)
			}
			if handlers.views.count != 0 {
				t.Fatalf("views observed = %d, want none on a project page", handlers.views.count)
			}
		})
	}
}

func TestProjectPages_DiscoverableHasNoRobotsHeader(t *testing.T) {
	readStore := projectPageFixture(t)
	readStore.row.SEOGeoEnabled = true
	renderer, client := newFakeProjectRenderer(t)
	renderer.mutate = func(page string) string { return page }
	handlers := newProjectHandlers(t, projectTestReader(t, readStore), client)
	response := handlers.serve(http.MethodGet, "/ada/projects/note-g")
	if response.Code != http.StatusOK || response.Header().Get("X-Robots-Tag") != "" {
		t.Fatalf("response = %d %#v; log %s", response.Code, response.Header(), handlers.logs.String())
	}
}

// TestProjectPages_UnavailableAnswersLikeAnUnknownResume proves every state
// without an available page answers with the exact status, headers and body
// of an unknown resume.
func TestProjectPages_UnavailableAnswersLikeAnUnknownResume(t *testing.T) {
	without := func(slug string) []projectStoreRow {
		var rows []projectStoreRow
		for _, row := range projectPageRows() {
			if row.slug != slug {
				rows = append(rows, row)
			}
		}
		return rows
	}
	withSections := func(change func(map[string][]projectEntry)) func(*testing.T, *projectPagesStore) {
		return func(t *testing.T, s *projectPagesStore) {
			sections := projectPageSections()
			change(sections)
			s.row = projectResumeRow(t, "en", nil, schema.MmYyyy, sections, "projects")
		}
	}
	gated := func(change func(*testing.T, *projectPagesStore)) func(*testing.T, *projectPagesStore) {
		return func(t *testing.T, s *projectPagesStore) {
			if change != nil {
				change(t, s)
			}
			s.row.SignInToView, s.row.ViewPassEpoch = true, 1
		}
	}
	const unknownWithPass, emptyWithPass = "unknown project slug behind sign in to view, with a pass", "no available project behind sign in to view, with a pass"
	withPass := map[string]bool{unknownWithPass: true, emptyWithPass: true}
	for _, test := range []struct {
		name, method, path string
		change             func(*testing.T, *projectPagesStore)
	}{
		{"unpublished", http.MethodGet, "/ada/projects/note-g", func(_ *testing.T, s *projectPagesStore) { s.row.Live = false }},
		{"unpublished list", http.MethodGet, "/ada/projects", func(_ *testing.T, s *projectPagesStore) { s.row.Live = false }},
		{"deleted", http.MethodGet, "/ada/projects/note-g", func(_ *testing.T, s *projectPagesStore) { s.err = pgx.ErrNoRows }},
		{"hidden entry", http.MethodGet, "/ada/projects/note-g", withSections(func(sections map[string][]projectEntry) { sections["projects"][0].hidden = true })},
		{"entry in an omitted section", http.MethodGet, "/ada/projects/note-g", func(t *testing.T, s *projectPagesStore) {
			s.row = projectResumeRow(t, "en", nil, schema.MmYyyy, projectPageSections(), "outside-of-layout")
		}},
		{"unlinked", http.MethodGet, "/ada/projects/note-g", func(t *testing.T, s *projectPagesStore) {
			withSections(func(sections map[string][]projectEntry) { sections["projects"][0].projectID = "" })(t, s)
			s.rows = without("note-g")
		}},
		{"project deleted", http.MethodGet, "/ada/projects/note-g", func(_ *testing.T, s *projectPagesStore) { s.rows = without("note-g") }},
		{"old resume slug after a rename", http.MethodGet, "/old-ada/projects/note-g", nil},
		{"old project slug after a rename", http.MethodGet, "/ada/projects/note-g", func(_ *testing.T, s *projectPagesStore) {
			s.rows = append(without("note-g"), projectStoreRow{entryID: "e-note-g", owner: projectTestOwner, slug: "note-g-renamed", doc: namedProject("Note G")})
		}},
		{"no available project", http.MethodGet, "/ada/projects", withSections(func(sections map[string][]projectEntry) { sections["projects"][0].hidden = true })},
		{"unknown project slug", http.MethodGet, "/ada/projects/unknown-tool", nil},
		{"hidden project by slug", http.MethodGet, "/ada/projects/hidden-tool", nil},
		{"omitted project by slug", http.MethodGet, "/ada/projects/outside-tool", nil},
		{"another account's project by slug", http.MethodGet, "/ada/projects/foreign-tool", nil},
		{"unknown project slug, HEAD", http.MethodHead, "/ada/projects/unknown-tool", nil},
		// With a valid pass, availability answers exactly as without sign in
		// to view; without one, only an unknown or non-live resume does.
		{unknownWithPass, http.MethodGet, "/ada/projects/unknown-tool", gated(nil)},
		{emptyWithPass, http.MethodGet, "/ada/projects",
			gated(withSections(func(sections map[string][]projectEntry) { sections["projects"][0].hidden = true }))},
		{"unpublished behind sign in to view, without a pass", http.MethodGet, "/ada/projects/note-g", gated(func(_ *testing.T, s *projectPagesStore) { s.row.Live = false })},
	} {
		t.Run(test.name, func(t *testing.T) {
			readStore := projectPageFixture(t)
			if test.change != nil {
				test.change(t, readStore)
			}
			renderer, client := newFakeProjectRenderer(t)
			handlers := newProjectHandlers(t, projectTestReader(t, readStore), client)
			reference := httptest.NewRecorder()
			handlers.resume.ServeHTTP(reference, httptest.NewRequestWithContext(context.Background(), test.method, "/nobody", nil))
			var cookies []*http.Cookie
			if withPass[test.name] {
				cookies = append(cookies, projectPass(t, 1))
			}
			got := handlers.serve(test.method, test.path, cookies...)
			if reference.Code != http.StatusNotFound || got.Code != reference.Code || !reflect.DeepEqual(got.Header(), reference.Header()) || got.Body.String() != reference.Body.String() {
				t.Fatalf("response = %d %#v %q, want the unknown resume's %d %#v %q", got.Code, got.Header(), got.Body.String(), reference.Code, reference.Header(), reference.Body.String())
			}
			if renderer.count(directrender.ProjectRenderMode)+renderer.count(directrender.ProjectListRenderMode)+renderer.count(directrender.GateRenderMode) != 0 {
				t.Fatal("an unavailable page reached the renderer")
			}
		})
	}
}

func projectPass(t *testing.T, epoch int32) *http.Cookie {
	t.Helper()
	id := uuid.MustParse("00000000-0000-0000-0000-0000000000d2")
	return &http.Cookie{Name: viewpass.CookieName, Value: viewpass.Seal(gateTestKey, id, epoch, time.Now().Add(time.Hour))}
}

// TestProjectPages_GatedResumeServesOneGateWithoutAPass proves that,
// without a pass, every project address of a gated resume serves the same
// gate, available or not, so no one can probe which project slugs exist.
// Only page in the provider links differs, and Content-Length with it.
func TestProjectPages_GatedResumeServesOneGateWithoutAPass(t *testing.T) {
	gatedHandlers := func(t *testing.T, hideNoteG bool) projectHandlers {
		t.Helper()
		sections := projectPageSections()
		sections["projects"][0].hidden = hideNoteG
		readStore := projectPageFixture(t)
		readStore.row = projectResumeRow(t, "en", nil, schema.MmYyyy, sections, "projects")
		readStore.row.SignInToView, readStore.row.ViewPassEpoch = true, 1
		_, client := newFakeProjectRenderer(t)
		return newProjectHandlers(t, projectTestReader(t, readStore), client)
	}
	available, empty := gatedHandlers(t, false), gatedHandlers(t, true)
	pageGate := available.serve(http.MethodGet, "/ada/projects/note-g")
	listGate := available.serve(http.MethodGet, "/ada/projects")
	for _, reference := range []*httptest.ResponseRecorder{pageGate, listGate} {
		if reference.Code != http.StatusOK || !strings.Contains(reference.Body.String(), `id="public-gate"`) {
			t.Fatalf("available page without a pass = %d %q, want the gate", reference.Code, reference.Body.String())
		}
	}
	for _, test := range []struct {
		name      string
		handlers  projectHandlers
		path      string
		page      string
		reference *httptest.ResponseRecorder
	}{
		{"unknown project slug", available, "/ada/projects/unknown-tool", "projects/unknown-tool", pageGate},
		{"hidden project slug", available, "/ada/projects/hidden-tool", "projects/hidden-tool", pageGate},
		{"another account's project slug", available, "/ada/projects/foreign-tool", "projects/foreign-tool", pageGate},
		{"unknown project slug with no available project", empty, "/ada/projects/unknown-tool", "projects/unknown-tool", pageGate},
		{"list with no available project", empty, "/ada/projects", "projects", listGate},
	} {
		t.Run(test.name, func(t *testing.T) {
			wantBody := test.reference.Body.String()
			if test.reference == pageGate {
				wantBody = strings.ReplaceAll(wantBody, "&amp;page=projects/note-g\"", "&amp;page="+test.page+"\"")
			}
			wantHeader := test.reference.Header().Clone()
			wantHeader.Set("Content-Length", strconv.Itoa(len(wantBody)))
			got := test.handlers.serve(http.MethodGet, test.path)
			if got.Code != test.reference.Code || !reflect.DeepEqual(got.Header(), wantHeader) || got.Body.String() != wantBody {
				t.Fatalf("response = %d %#v %q, want the gate %d %#v %q", got.Code, got.Header(), got.Body.String(), test.reference.Code, wantHeader, wantBody)
			}
			if !strings.Contains(got.Body.String(), "&amp;page="+test.page+"\"") {
				t.Fatalf("gate links do not carry page=%s", test.page)
			}
		})
	}
}

func TestProjectPages_SignInToViewServesTheGateWithThePage(t *testing.T) {
	readStore := projectPageFixture(t)
	readStore.row.SignInToView, readStore.row.ViewPassEpoch = true, 1
	renderer, client := newFakeProjectRenderer(t)
	handlers := newProjectHandlers(t, projectTestReader(t, readStore), client)

	for _, test := range []struct{ path, page, message string }{
		{"/ada/projects/note-g", "projects/note-g", "none"},
		{"/ada/projects?signin=failed&page=evil", "projects", "failed"},
	} {
		gate := handlers.serve(http.MethodGet, test.path)
		if gate.Code != http.StatusOK || gate.Header().Get("Cache-Control") != "no-store" || !strings.Contains(gate.Body.String(), `id="public-gate"`) {
			t.Fatalf("%s: gate = %d %#v; log %s", test.path, gate.Code, gate.Header(), handlers.logs.String())
		}
		var envelope directrender.GateRenderRequest
		if err := json.Unmarshal(renderer.envelope(directrender.GateRenderMode), &envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Page != test.page || envelope.Message != test.message || envelope.Slug != "ada" {
			t.Fatalf("%s: gate envelope = %+v", test.path, envelope)
		}
		for _, provider := range []string{"google", "linkedin"} {
			link := `/api/v1/auth/` + provider + `/start?purpose=view&amp;slug=ada&amp;page=` + test.page + `"`
			if !strings.Contains(gate.Body.String(), link) {
				t.Fatalf("%s: gate body lacks %s", test.path, link)
			}
		}
	}
	if renderer.count(directrender.ProjectRenderMode) != 0 || renderer.count(directrender.ProjectListRenderMode) != 0 {
		t.Fatal("a project page was rendered without a pass")
	}

	page := handlers.serve(http.MethodGet, "/ada/projects/note-g", projectPass(t, 1))
	if page.Code != http.StatusOK || page.Body.String() != readProjectCorpus(t)["project-en-name-only"].HTML ||
		page.Header().Get("Cache-Control") != "private, no-cache, must-revalidate" || page.Header().Get("X-Robots-Tag") != "noindex, noarchive" {
		t.Fatalf("valid pass = %d %#v; log %s", page.Code, page.Header(), handlers.logs.String())
	}
	// A cached page is not reached before the pass check.
	if gate := handlers.serve(http.MethodGet, "/ada/projects/note-g", projectPass(t, 0)); !strings.Contains(gate.Body.String(), `id="public-gate"`) {
		t.Fatalf("old pass after a cached page = %d %q, want the gate", gate.Code, gate.Body.String())
	}
}

func TestProjectPages_CacheKeepsRevisionVariantAndSlugApart(t *testing.T) {
	readStore := projectPageFixture(t)
	readStore.row.ViewPassEpoch = 1
	sections := projectPageSections()
	sections["projects"] = append(sections["projects"], projectEntry{id: "e-note-h", projectID: "00000000-0000-0000-0000-00000000e005"})
	readStore.row = projectResumeRow(t, "en", nil, schema.MmYyyy, sections, "projects")
	readStore.row.ViewPassEpoch = 1
	readStore.rows = append(readStore.rows, projectStoreRow{entryID: "e-note-h", owner: projectTestOwner, slug: "note-h", doc: namedProject("Note H")})
	renderer, client := newFakeProjectRenderer(t)
	reader, coordinator := projectTestReaderAndCoordinator(t, readStore)
	handlers := newProjectHandlers(t, reader, client)

	step := func(name, path string, wantRenders int, cookies ...*http.Cookie) {
		t.Helper()
		response := handlers.serve(http.MethodGet, path, cookies...)
		if response.Code != http.StatusOK || renderer.count(directrender.ProjectRenderMode) != wantRenders {
			t.Fatalf("%s: status %d, renders %d, want 200 and %d; log %s", name, response.Code, renderer.count(directrender.ProjectRenderMode), wantRenders, handlers.logs.String())
		}
	}
	step("miss", "/ada/projects/note-g", 1)
	step("hit", "/ada/projects/note-g", 1)
	step("another project slug", "/ada/projects/note-h", 2)
	step("each slug keeps its entry", "/ada/projects/note-g", 2)
	advanceProjectRevision(t, coordinator, readStore)
	step("revision advance", "/ada/projects/note-g", 3)
	readStore.update(func(s *projectPagesStore) { s.row.SignInToView = true })
	step("sign-in variant", "/ada/projects/note-g", 4, projectPass(t, 1))
	readStore.update(func(s *projectPagesStore) { s.row.SignInToView = false })
	step("ordinary variant still cached", "/ada/projects/note-g", 4)
}

func TestProjectPages_HeadAndMethods(t *testing.T) {
	_, client := newFakeProjectRenderer(t)
	handlers := newProjectHandlers(t, projectTestReader(t, projectPageFixture(t)), client)
	get := handlers.serve(http.MethodGet, "/ada/projects/note-g")
	head := handlers.serve(http.MethodHead, "/ada/projects/note-g")
	if head.Code != http.StatusOK || head.Body.Len() != 0 || !reflect.DeepEqual(head.Header(), get.Header()) ||
		head.Header().Get("Content-Length") != strconv.Itoa(get.Body.Len()) {
		t.Fatalf("HEAD = %d %#v %q, want GET's headers and no body", head.Code, head.Header(), head.Body.String())
	}
	reference := httptest.NewRecorder()
	handlers.resume.ServeHTTP(reference, httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/ada", nil))
	for _, path := range []string{"/ada/projects", "/ada/projects/note-g"} {
		post := handlers.serve(http.MethodPost, path)
		if post.Code != http.StatusMethodNotAllowed || post.Header().Get("Allow") != "GET, HEAD" || !reflect.DeepEqual(post.Header(), reference.Header()) || post.Body.String() != reference.Body.String() {
			t.Fatalf("POST %s = %d %#v %q", path, post.Code, post.Header(), post.Body.String())
		}
	}
}

func TestProjectPages_RejectedHTMLIs503WithAClosedLogLine(t *testing.T) {
	for path, fixture := range map[string]func(*testing.T) *projectPagesStore{"/ada/projects": projectListFixture, "/ada/projects/note-g": projectPageFixture} {
		t.Run(path, func(t *testing.T) {
			readStore := fixture(t)
			renderer, client := newFakeProjectRenderer(t)
			renderer.mutate = func(page string) string {
				return strings.Replace(page, "</head>", "<script>alert(1)</script></head>", 1)
			}
			handlers := newProjectHandlers(t, projectTestReader(t, readStore), client)
			response := handlers.serve(http.MethodGet, path)
			if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "1" {
				t.Fatalf("response = %d %#v", response.Code, response.Header())
			}
			logs := handlers.logs.String()
			if strings.Count(logs, "\n") != 1 || !strings.Contains(logs, "reason=html_rejected rule=script_element") {
				t.Fatalf("log = %q, want one closed html_rejected line", logs)
			}
			for _, private := range []string{"ada", "note-g", "Note G", "Lovelace", "projects/"} {
				if strings.Contains(strings.ToLower(logs), strings.ToLower(private)) {
					t.Fatalf("log %q carries %q", logs, private)
				}
			}
			if !bytes.Contains(response.Body.Bytes(), []byte("Temporarily unavailable")) {
				t.Fatalf("body = %q", response.Body.String())
			}
		})
	}
}
