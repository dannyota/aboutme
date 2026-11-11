package accountapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
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
