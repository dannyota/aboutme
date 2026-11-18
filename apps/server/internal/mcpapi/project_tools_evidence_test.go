package mcpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/project"
)

func TestProjectTools_GetProjectCarriesEvidenceThatNoToolWrites(t *testing.T) {
	w := newProjectToolWorld(t, projectToolAllScopes)
	if _, err := w.h.pool.Exec(context.Background(), `INSERT INTO project_evidence (project_id, position, kind, repository_id, item_id,
		url, repository_name, title, item_date, checked_at, checked_binding, rule_met)
		VALUES ($1, 0, 'repository', 101, 101, 'https://github.com/octocat/hello', 'octocat/hello', 'octocat/hello', now(), now(), 1, true)`, w.atlas.ID); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Project map[string]any `json:"project"`
	}
	decodeProjectToolResult(t, requireProjectToolSuccess(t, w.session, "get_project", map[string]any{"project_id": w.atlas.ID.String()}), &got)
	evidence, isArray := got.Project["evidence"].([]any)
	if !isArray || len(evidence) != 1 {
		t.Fatalf("get_project evidence = %v, want one item", got.Project["evidence"])
	}
	item, isObject := evidence[0].(map[string]any)
	if !isObject || item["kind"] != "repository" || item["url"] != "https://github.com/octocat/hello" || item["confirmed"] != false ||
		item["flagReason"] != nil || len(item) != 9 {
		t.Fatalf("evidence item = %v", evidence[0])
	}

	var listed struct {
		Projects []map[string]any `json:"projects"`
	}
	decodeProjectToolResult(t, requireProjectToolSuccess(t, w.session, "list_projects", map[string]any{}), &listed)
	for _, p := range listed.Projects {
		if _, present := p["evidence"]; present {
			t.Fatalf("list_projects carries evidence: %v", p)
		}
	}

	before := w.snapshot(t, w.h.user.ID)
	document := projectToolDocumentArgument("Atlas")
	document["evidence"] = evidence
	requireProjectToolError(t, w.session, "update_project", map[string]any{
		"idempotency_key": uuid.NewString(), "project_id": w.atlas.ID.String(), "revision": "1", "slug": "atlas", "document": document,
	}, "validation_failed")
	if after := w.snapshot(t, w.h.user.ID); after != before {
		t.Fatalf("a rejected evidence write changed the account:\nbefore %s\nafter  %s", before, after)
	}
	var stored int
	if err := w.h.pool.QueryRow(context.Background(), `SELECT count(*) FROM project_evidence WHERE project_id = $1`, w.atlas.ID).Scan(&stored); err != nil || stored != 1 {
		t.Fatalf("stored evidence = %d (%v), want the one item untouched", stored, err)
	}
}

func projectToolStoredBytes(t *testing.T, w *projectToolWorld, d project.Document) int {
	t.Helper()
	prepared, err := project.Prepare(d)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(prepared)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err = w.h.pool.QueryRow(context.Background(), "SELECT octet_length($1::jsonb::text)", raw).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func projectToolSizedDocument(t *testing.T, w *projectToolWorld, target int) map[string]any {
	t.Helper()
	d := project.Document{DefaultLanguage: "en", Text: map[string]project.Text{"en": {Name: "Atlas"}}}
	padding := []string{"vi", "ja", "fr"}
	for _, key := range padding {
		d.Text[key] = project.Text{Name: key, Description: "a"}
	}
	remaining := target - projectToolStoredBytes(t, w, d)
	for _, key := range padding {
		share := min(remaining, 32766)
		text := d.Text[key]
		text.Description = strings.Repeat(`\`, share/2) + strings.Repeat("a", share%2+1)
		d.Text[key] = text
		remaining -= share
	}
	if got := projectToolStoredBytes(t, w, d); remaining != 0 || got != target {
		t.Fatalf("sized document = %d bytes, want %d", got, target)
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var argument map[string]any
	if err = json.Unmarshal(raw, &argument); err != nil {
		t.Fatal(err)
	}
	return argument
}

func (w *projectToolWorld) seedEvidence(t *testing.T, projectID uuid.UUID, itemID int64) {
	t.Helper()
	if _, err := w.h.pool.Exec(context.Background(), `INSERT INTO project_evidence (project_id, position, kind, repository_id, item_id,
		url, repository_name, title, item_date, checked_at, checked_binding, rule_met)
		SELECT $1, COALESCE(max(position) + 1, 0), 'repository', $2, $2, 'https://github.com/octocat/hello', 'octocat/hello', 'octocat/hello', now(), now(), 1, true
		FROM project_evidence WHERE project_id = $1`, projectID, itemID); err != nil {
		t.Fatal(err)
	}
}

func TestProjectTools_UpdateProjectCountsStoredEvidenceTowardTheSizeBound(t *testing.T) {
	w := newProjectToolWorld(t, projectToolAllScopes)
	w.seedEvidence(t, w.atlas.ID, 101)
	full := projectToolSizedDocument(t, w, project.MaxStoredBytes-project.EvidenceItemBytes)
	requireProjectToolSuccess(t, w.session, "update_project", map[string]any{
		"idempotency_key": uuid.NewString(), "project_id": w.atlas.ID.String(), "revision": "1", "slug": "atlas", "document": full,
	})
	w.seedEvidence(t, w.atlas.ID, 102)
	before := w.snapshot(t, w.h.user.ID)
	requireProjectToolError(t, w.session, "update_project", map[string]any{
		"idempotency_key": uuid.NewString(), "project_id": w.atlas.ID.String(), "revision": "2", "slug": "atlas", "document": full,
	}, "validation_failed")
	if after := w.snapshot(t, w.h.user.ID); after != before {
		t.Fatalf("an update over the size bound changed the account:\nbefore %s\nafter  %s", before, after)
	}
	var stored int
	if err := w.h.pool.QueryRow(context.Background(), `SELECT count(*) FROM project_evidence WHERE project_id = $1`, w.atlas.ID).Scan(&stored); err != nil || stored != 2 {
		t.Fatalf("stored evidence = %d (%v), want both items untouched", stored, err)
	}
}
