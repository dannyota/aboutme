package accountapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
)

func TestExport_ProjectsVersionAndCreationOrder(t *testing.T) {
	h := newExportTestHarness(t)
	earlier := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	later := earlier.Add(time.Hour)
	for _, item := range []struct {
		slug    string
		created time.Time
	}{{"later", later}, {"earlier", earlier}} {
		if _, err := h.pool.Exec(h.ctx, `INSERT INTO projects(user_id,slug,document,created_at) VALUES($1,$2,$3,$4)`, h.user.ID, item.slug, `{"defaultLanguage":"en","text":{"en":{"name":"Example","outcome":"Shipped"}},"technologies":["Go"]}`, item.created); err != nil {
			t.Fatal(err)
		}
	}
	response := h.request(t, "GET", ExportPath, nil)
	if response.Code != 200 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var bundle struct {
		Data struct {
			ExportVersion int `json:"exportVersion"`
			Projects      []struct {
				Slug            string `json:"slug"`
				Revision        string `json:"revision"`
				DefaultLanguage string `json:"defaultLanguage"`
				Text            map[string]struct {
					Name    string `json:"name"`
					Outcome string `json:"outcome"`
				} `json:"text"`
				Technologies []string `json:"technologies"`
			} `json:"projects"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Data.ExportVersion != 2 || len(bundle.Data.Projects) != 2 {
		t.Fatalf("bundle=%s", response.Body.String())
	}
	for i, slug := range []string{"earlier", "later"} {
		p := bundle.Data.Projects[i]
		if p.Slug != slug || p.Revision != "1" || p.DefaultLanguage != "en" || p.Text["en"].Name != "Example" || p.Text["en"].Outcome != "Shipped" || len(p.Technologies) != 1 || p.Technologies[0] != "Go" {
			t.Fatalf("project=%+v", p)
		}
	}
	if strings.Contains(response.Body.String(), "linkedResumeIds") {
		t.Fatal("export contains derived reference list")
	}
}

func TestExport_EmptyProjectsArray(t *testing.T) {
	h := newExportTestHarness(t)
	response := h.request(t, "GET", ExportPath, nil)
	var bundle struct {
		Data struct {
			Projects []json.RawMessage `json:"projects"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &bundle); err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || bundle.Data.Projects == nil || len(bundle.Data.Projects) != 0 {
		t.Fatalf("export=%s", response.Body.String())
	}
}

func TestAccountDeletion_RemovesProjectsAndSlugHolds(t *testing.T) {
	h := newExportTestHarness(t)
	if _, err := h.pool.Exec(h.ctx, `INSERT INTO projects(user_id,slug,document) VALUES($1,'owned','{}');`, h.user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.pool.Exec(h.ctx, `INSERT INTO project_slug_holds(user_id,slug,released_at) VALUES($1,'released',now())`, h.user.ID); err != nil {
		t.Fatal(err)
	}
	plan, err := h.service.prepareDeletion(h.ctx, deletionRequest{userID: h.user.ID, session: &h.session})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = h.service.executeDeletion(h.ctx, plan); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"projects", "project_slug_holds"} {
		var n int
		if err = h.pool.QueryRow(h.ctx, "SELECT count(*) FROM "+table+" WHERE user_id=$1", h.user.ID).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s count=%d error=%v", table, n, err)
		}
	}
}

// maximumProjectDocument returns a valid document whose stored JSON text is
// exactly the project bound, filled with characters HTML escaping would grow.
func maximumProjectDocument(t *testing.T, h *exportTestHarness) []byte {
	t.Helper()
	d := project.Document{
		DefaultLanguage: "en",
		DemoURL:         "https://example.test/?a=1&b=2",
		Technologies:    []string{"C&<>"},
		Text: map[string]project.Text{"en": {
			Name: "<&> Tools & <Tests>", Role: "R&D <lead>", Summary: "a < b & c > d",
			Outcome: "&&&", Description: "<p>&lt;&amp;&gt;</p>",
		}},
	}
	measure := func(d project.Document) int {
		prepared, err := project.Prepare(d)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(prepared)
		if err != nil {
			t.Fatal(err)
		}
		var n int
		if err = h.pool.QueryRow(h.ctx, "SELECT octet_length($1::jsonb::text)", raw).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	d.RepositoryURL = "https://example.test/?"
	d.RepositoryURL += strings.Repeat("&", project.MaxStoredBytes-measure(d))
	prepared, err := project.Prepare(d)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(prepared)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestExport_MaximumLibraryKeepsLiteralCharacters(t *testing.T) {
	h := newExportTestHarness(t)
	raw := maximumProjectDocument(t, h)
	for i := 1; i <= project.MaxProjects; i++ {
		if _, err := h.pool.Exec(h.ctx, "INSERT INTO projects(user_id,slug,document) VALUES($1,$2,$3)", h.user.ID, fmt.Sprintf("max-%d", i), raw); err != nil {
			t.Fatal(err)
		}
	}
	var stored, count int
	if err := h.pool.QueryRow(h.ctx, "SELECT min(octet_length(document::text)), count(*) FROM projects WHERE user_id=$1", h.user.ID).Scan(&stored, &count); err != nil || stored != project.MaxStoredBytes || count != project.MaxProjects {
		t.Fatalf("stored=%d count=%d error=%v", stored, count, err)
	}
	response := h.request(t, "GET", ExportPath, nil)
	if response.Code != 200 {
		t.Fatalf("status=%d, want 200 for a maximum library", response.Code)
	}
	body := response.Body.Bytes()
	if !bytes.Contains(body, []byte(`"name":"<&> Tools & <Tests>"`)) || bytes.Contains(body, []byte(`\u0026`)) || bytes.Contains(body, []byte(`\u003c`)) {
		t.Fatal("export escaped HTML characters")
	}
	if !bytes.HasSuffix(body, []byte("}")) || response.Header().Get("Content-Length") != fmt.Sprint(len(body)) {
		t.Fatalf("export tail=%q length header=%s", body[len(body)-1:], response.Header().Get("Content-Length"))
	}
	var bundle struct {
		Data struct {
			Projects []json.RawMessage `json:"projects"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &bundle); err != nil || len(bundle.Data.Projects) != project.MaxProjects {
		t.Fatalf("projects=%d error=%v", len(bundle.Data.Projects), err)
	}
}

func TestExport_ResumeDocumentsKeepProjectLinks(t *testing.T) {
	h := newExportTestHarness(t)
	var projectID string
	if err := h.pool.QueryRow(h.ctx, `INSERT INTO projects(user_id,slug,document) VALUES($1,'linked','{"defaultLanguage":"en","text":{"en":{"name":"Linked"}}}') RETURNING id`, h.user.ID).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	var doc schema.Resume
	if err := json.Unmarshal([]byte(`{"schemaVersion":6,"personalDetails":{"fullName":"Ada","details":[]},`+
		`"content":{"projects":{"sectionType":"project","entries":[{"id":"6f0d8f5e-6d0b-4b8e-9b55-2f7c2a0c9e11","title":"Linked","projectId":"`+projectID+`"}]}},`+
		`"customization":{"font":{"family":"inter","baseSizePx":14},"colors":{"primary":"#1a1a1a","text":"#1a1a1a","background":"#ffffff"},`+
		`"spacing":{"sectionGap":16,"entryGap":8,"lineHeight":1.4},"heading":{"style":"normal","showRule":false},`+
		`"layout":{"columns":1,"sections":{"main":["projects"],"sidebar":[]}},"sectionDisplay":{"skill":{"style":"text"},"language":{"style":"text"}},`+
		`"pageFormat":"a4","dateFormat":"MM/YYYY"}}`), &doc); err != nil {
		t.Fatal(err)
	}
	if _, err := resume.NewStore(h.pool, docmigrate.NewIdentityProjector()).Create(h.ctx, h.user.ID, "linked", doc); err != nil {
		t.Fatalf("seed linked resume: %v", err)
	}
	response := h.request(t, "GET", ExportPath, nil)
	if response.Code != 200 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var bundle struct {
		Data struct {
			Resumes []struct {
				Document schema.Resume `json:"document"`
			} `json:"resumes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &bundle); err != nil {
		t.Fatal(err)
	}
	if len(bundle.Data.Resumes) != 1 {
		t.Fatalf("export=%s", response.Body.String())
	}
	entries := bundle.Data.Resumes[0].Document.Content["projects"].ProjectEntries
	if len(entries) != 1 || entries[0].ProjectID == nil || *entries[0].ProjectID != projectID {
		t.Fatalf("exported entries = %+v, want the project link kept", entries)
	}
}
