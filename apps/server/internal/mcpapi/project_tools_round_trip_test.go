package mcpapi

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/google/uuid"
)

func decodeProjectToolResult(t *testing.T, raw []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode tool result %s: %v", raw, err)
	}
}

func TestProjectTools_LibraryRoundTripThroughTheTools(t *testing.T) {
	w := newProjectToolWorld(t, projectToolAllScopes)
	var listed struct {
		Projects []map[string]any `json:"projects"`
	}
	decodeProjectToolResult(t, requireProjectToolSuccess(t, w.session, "list_projects", map[string]any{}), &listed)
	if len(listed.Projects) != 3 {
		t.Fatalf("list_projects = %v, want the world's three projects", listed.Projects)
	}
	var got struct {
		Project map[string]any `json:"project"`
	}
	decodeProjectToolResult(t, requireProjectToolSuccess(t, w.session, "get_project", map[string]any{"project_id": w.atlas.ID.String()}), &got)
	linked, ok := got.Project["linkedResumeIds"].([]any)
	if got.Project["id"] != w.atlas.ID.String() || got.Project["slug"] != "atlas" || !ok || len(linked) != 1 || linked[0] != w.resume.ID.String() {
		t.Fatalf("get_project = %v, want atlas linked from the world's resume", got.Project)
	}
	if _, present := got.Project["evidence"]; present {
		t.Fatalf("get_project = %v, must carry no evidence field", got.Project)
	}

	var created struct {
		Revision string         `json:"revision"`
		Project  map[string]any `json:"project"`
	}
	decodeProjectToolResult(t, requireProjectToolSuccess(t, w.session, "create_project", map[string]any{
		"idempotency_key": uuid.NewString(), "document": projectToolDocumentArgument("Delta"),
	}), &created)
	if created.Revision != "1" || created.Project["slug"] != "delta" {
		t.Fatalf("create_project = %+v, want revision 1 with the proposed slug delta", created)
	}

	var renamed struct {
		Revision       string           `json:"revision"`
		Project        map[string]any   `json:"project"`
		ChangedResumes []map[string]any `json:"changed_resumes"`
	}
	decodeProjectToolResult(t, requireProjectToolSuccess(t, w.session, "update_project", map[string]any{
		"idempotency_key": uuid.NewString(), "project_id": w.atlas.ID.String(), "revision": "1",
		"slug": "atlas-two", "document": projectToolDocumentArgument("Atlas two"),
	}), &renamed)
	if renamed.Revision != "2" || renamed.Project["slug"] != "atlas-two" || renamed.ChangedResumes == nil || len(renamed.ChangedResumes) != 0 {
		t.Fatalf("update_project rename = %+v, want revision 2, slug atlas-two and no rewritten draft", renamed)
	}

	revision := w.resume.Revision
	linkedOutput := requireProjectToolSuccess(t, w.session, "link_project", map[string]any{
		"idempotency_key": uuid.NewString(), "resume_id": w.resume.ID.String(),
		"revision": strconv.FormatInt(revision, 10), "project_id": w.beacon.ID.String(),
	})
	revision++
	if output := decodeMutationBytes(t, linkedOutput); output.Revision != strconv.FormatInt(revision, 10) {
		t.Fatalf("link_project revision = %q, want %d", output.Revision, revision)
	}
	requireProjectToolSuccess(t, w.session, "refresh_project_entry", map[string]any{
		"idempotency_key": uuid.NewString(), "resume_id": w.resume.ID.String(),
		"revision": strconv.FormatInt(revision, 10), "entry_id": w.linkedEntry,
	})
	revision++
	if refreshed := projectToolEntryTitle(t, w, w.linkedEntry); refreshed != "Atlas two" {
		t.Fatalf("refreshed entry title = %q, want the library name Atlas two", refreshed)
	}

	var deleted struct {
		ID             string           `json:"id"`
		Deleted        bool             `json:"deleted"`
		ChangedResumes []map[string]any `json:"changed_resumes"`
	}
	decodeProjectToolResult(t, requireProjectToolSuccess(t, w.session, "delete_project", map[string]any{
		"idempotency_key": uuid.NewString(), "project_id": w.atlas.ID.String(), "revision": "2",
	}), &deleted)
	if deleted.ID != w.atlas.ID.String() || !deleted.Deleted || len(deleted.ChangedResumes) != 1 ||
		deleted.ChangedResumes[0]["id"] != w.resume.ID.String() || deleted.ChangedResumes[0]["revision"] != strconv.FormatInt(revision+1, 10) {
		t.Fatalf("delete_project = %+v, want the linking resume at revision %d", deleted, revision+1)
	}
	if links := w.links(t, w.resume.ID); links[w.linkedEntry] != "" || len(links) != 1 {
		t.Fatalf("links after delete = %v, want only the beacon link", links)
	}

	for _, slug := range []string{"beacon", "atlas"} {
		requireProjectToolError(t, w.session, "create_project", map[string]any{
			"idempotency_key": uuid.NewString(), "slug": slug, "document": projectToolDocumentArgument("Taken"),
		}, "slug_unavailable")
	}
	requireProjectToolError(t, w.session, "create_project", map[string]any{
		"idempotency_key": uuid.NewString(), "document": map[string]any{
			"slug": "inside", "defaultLanguage": "en", "text": map[string]any{"en": map[string]any{"name": "Inside"}},
		},
	}, "validation_failed")
}

func TestProjectTools_ResumeCapIsLimitReached(t *testing.T) {
	w := newProjectToolWorld(t, projectToolOldGrant)
	for w.resumeCount(t, w.h.user.ID) < 3 {
		createProjectToolResume(t, w, w.h.user.ID)
	}
	before := w.snapshot(t, w.h.user.ID)
	requireProjectToolError(t, w.session, "create_resume", map[string]any{
		"idempotency_key": uuid.NewString(), "title": "Fourth",
	}, "limit_reached")
	if after := w.snapshot(t, w.h.user.ID); after != before {
		t.Fatalf("refused create changed the account:\nbefore %s\nafter  %s", before, after)
	}
}

func decodeMutationBytes(t *testing.T, raw []byte) mutationOutput {
	t.Helper()
	var output mutationOutput
	decodeProjectToolResult(t, raw, &output)
	return output
}

func projectToolEntryTitle(t *testing.T, w *projectToolWorld, entryID string) string {
	t.Helper()
	stored, err := w.resumes.Get(context.Background(), w.h.user.ID, w.resume.ID)
	if err != nil {
		t.Fatalf("reload resume: %v", err)
	}
	for _, section := range stored.Doc.Content {
		for _, entry := range section.ProjectEntries {
			if entry.ID == entryID && entry.Title != nil {
				return *entry.Title
			}
		}
	}
	return ""
}
