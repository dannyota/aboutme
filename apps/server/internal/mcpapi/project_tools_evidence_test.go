package mcpapi

import (
	"context"
	"testing"

	"github.com/google/uuid"
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
