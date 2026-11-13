package publicapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/viewpass"
)

func projectPass(t *testing.T, epoch int32) *http.Cookie {
	t.Helper()
	id := uuid.MustParse("00000000-0000-0000-0000-0000000000d2")
	return &http.Cookie{Name: viewpass.CookieName, Value: viewpass.Seal(gateTestKey, id, epoch, time.Now().Add(time.Hour))}
}

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
	if gate := handlers.serve(http.MethodGet, "/ada/projects/note-g", projectPass(t, 0)); !strings.Contains(gate.Body.String(), `id="public-gate"`) {
		t.Fatalf("old pass after a cached page = %d %q, want the gate", gate.Code, gate.Body.String())
	}
}

func TestProjectPages_GateDoesNotBuildTheProjectPages(t *testing.T) {
	readStore := projectPageFixture(t)
	readStore.row.SignInToView, readStore.row.ViewPassEpoch = true, 1
	readStore.rows[0].raw = json.RawMessage(`"not a project document"`)
	_, client := newFakeProjectRenderer(t)
	handlers := newProjectHandlers(t, projectTestReader(t, readStore), client)

	gate := handlers.serve(http.MethodGet, "/ada/projects/note-g")
	unknown := handlers.serve(http.MethodGet, "/ada/projects/unknown-tool")
	wantBody := strings.ReplaceAll(unknown.Body.String(), "&amp;page=projects/unknown-tool\"", "&amp;page=projects/note-g\"")
	wantHeader := unknown.Header().Clone()
	wantHeader.Set("Content-Length", strconv.Itoa(len(wantBody)))
	if gate.Code != http.StatusOK || !strings.Contains(gate.Body.String(), `id="public-gate"`) ||
		!reflect.DeepEqual(gate.Header(), wantHeader) || gate.Body.String() != wantBody {
		t.Fatalf("gate = %d %#v %q, want the unknown slug's gate; log %s", gate.Code, gate.Header(), gate.Body.String(), handlers.logs.String())
	}
	if list := handlers.serve(http.MethodGet, "/ada/projects"); list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `id="public-gate"`) {
		t.Fatalf("list without a pass = %d, want the gate", list.Code)
	}
	if handlers.logs.Len() != 0 {
		t.Fatalf("log = %q, want nothing logged before a pass", handlers.logs.String())
	}
	withPass := handlers.serve(http.MethodGet, "/ada/projects/note-g", projectPass(t, 1))
	if withPass.Code != http.StatusServiceUnavailable || !strings.Contains(handlers.logs.String(), "reason=pages_failed") {
		t.Fatalf("with a pass = %d, log %q; want 503 and reason=pages_failed", withPass.Code, handlers.logs.String())
	}
}
