package accountapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestExport_ProjectRecordsCarryTheirEvidence(t *testing.T) {
	h := newExportTestHarness(t)
	if _, err := h.pool.Exec(h.ctx, `INSERT INTO github_connections (user_id, github_user_id, login, connected_at, generation, binding) VALUES ($1, 583231, 'octocat', now(), 4, 2)`, h.user.ID); err != nil {
		t.Fatal(err)
	}
	var withEvidence, without string
	for slug, target := range map[string]*string{"with": &withEvidence, "without": &without} {
		if err := h.pool.QueryRow(h.ctx, `INSERT INTO projects(user_id,slug,document) VALUES($1,$2,'{"defaultLanguage":"en","text":{"en":{"name":"Example"}}}') RETURNING id`, h.user.ID, slug).Scan(target); err != nil {
			t.Fatal(err)
		}
	}
	checked := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	for i, binding := range []int{2, 1} {
		if _, err := h.pool.Exec(h.ctx, `INSERT INTO project_evidence (project_id, position, kind, repository_id, item_id, pull_number, url,
			repository_name, title, item_date, checked_at, checked_binding, rule_met)
			VALUES ($1, $2, 'pullRequest', 101, $3, 42, 'https://github.com/octocat/hello/pull/42', 'octocat/hello', 'Fix the thing',
			'2026-01-02T03:04:05Z', $4, $5, true)`, withEvidence, i, 9001+i, checked, binding); err != nil {
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
				ID       string                       `json:"id"`
				Evidence []map[string]json.RawMessage `json:"evidence"`
			} `json:"projects"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Data.ExportVersion != 2 || len(bundle.Data.Projects) != 2 {
		t.Fatalf("export = %s", response.Body.String())
	}
	for _, p := range bundle.Data.Projects {
		switch p.ID {
		case without:
			if p.Evidence == nil || len(p.Evidence) != 0 {
				t.Fatalf("project without evidence = %v, want an empty array", p.Evidence)
			}
		case withEvidence:
			if len(p.Evidence) != 2 {
				t.Fatalf("evidence = %v", p.Evidence)
			}
			first, second := p.Evidence[0], p.Evidence[1]
			if string(first["confirmed"]) != "true" || string(second["confirmed"]) != "false" || string(first["flagReason"]) != "null" ||
				string(first["kind"]) != `"pullRequest"` || string(first["url"]) != `"https://github.com/octocat/hello/pull/42"` ||
				string(first["repositoryName"]) != `"octocat/hello"` || string(first["title"]) != `"Fix the thing"` ||
				string(first["itemDate"]) != `"2026-01-02T03:04:05Z"` || string(first["checkedAt"]) != `"2026-11-16T09:00:00Z"` {
				t.Fatalf("evidence = %v", p.Evidence)
			}
			if len(first) != 9 {
				t.Fatalf("evidence keys = %v, want exactly the nine read-model fields", first)
			}
		default:
			t.Fatalf("unexpected project %s", p.ID)
		}
	}
	body := strings.ToLower(response.Body.String())
	for _, forbidden := range []string{"generation", "binding", "itemid", "repositoryid", "rulemet"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("export mentions %q", forbidden)
		}
	}
}
