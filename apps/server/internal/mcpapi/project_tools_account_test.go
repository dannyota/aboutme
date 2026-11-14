package mcpapi

import (
	"strconv"
	"testing"

	"github.com/google/uuid"
)

func TestProjectTools_AnotherAccountsProjectIsNotFound(t *testing.T) {
	w := newProjectToolWorld(t, projectToolAllScopes)
	foreignResume, foreignEntry := w.foreignLinkedResume(t)
	before := w.snapshot(t, w.h.user.ID)
	otherBefore := w.snapshot(t, w.other)
	foreignID := w.foreign.ID.String()
	foreignRevision := strconv.FormatInt(w.foreign.Revision, 10)
	revision := strconv.FormatInt(w.resume.Revision, 10)
	for _, tc := range []struct {
		name string
		tool string
		args map[string]any
	}{
		{"get", "get_project", map[string]any{"project_id": foreignID}},
		{"update", "update_project", map[string]any{
			"idempotency_key": uuid.NewString(), "project_id": foreignID, "revision": foreignRevision,
			"slug": "taken-over", "document": projectToolDocumentArgument("Taken over"),
		}},
		{"delete", "delete_project", map[string]any{
			"idempotency_key": uuid.NewString(), "project_id": foreignID, "revision": foreignRevision,
		}},
		{"link as a new entry", "link_project", map[string]any{
			"idempotency_key": uuid.NewString(), "resume_id": w.resume.ID.String(), "revision": revision,
			"project_id": foreignID,
		}},
		{"link an existing entry", "link_project", map[string]any{
			"idempotency_key": uuid.NewString(), "resume_id": w.resume.ID.String(), "revision": revision,
			"project_id": foreignID, "entry_id": w.plainEntry,
		}},
		{"refresh an own entry linked to it", "refresh_project_entry", map[string]any{
			"idempotency_key": uuid.NewString(), "resume_id": foreignResume.ID.String(),
			"revision": strconv.FormatInt(foreignResume.Revision, 10), "entry_id": foreignEntry,
		}},
		{"refresh the other account's entry", "refresh_project_entry", map[string]any{
			"idempotency_key": uuid.NewString(), "resume_id": w.otherResume.ID.String(),
			"revision": strconv.FormatInt(w.otherResume.Revision, 10), "entry_id": w.otherEntry,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireProjectToolError(t, w.session, tc.tool, tc.args, "not_found")
		})
	}
	if after := w.snapshot(t, w.other); after != otherBefore {
		t.Fatalf("project tools changed the other account:\nbefore %s\nafter  %s", otherBefore, after)
	}
	if after := w.snapshot(t, w.h.user.ID); after != before {
		t.Fatalf("refused project tools changed the caller's account:\nbefore %s\nafter  %s", before, after)
	}
}

func TestProjectTools_ResumeWriteNamingAnotherAccountsProjectIsNotFound(t *testing.T) {
	w := newProjectToolWorld(t, projectToolAllScopes)
	before := w.snapshot(t, w.h.user.ID)
	otherBefore := w.snapshot(t, w.other)
	requireProjectToolError(t, w.session, "upsert_entry", map[string]any{
		"idempotency_key": uuid.NewString(), "resume_id": w.resume.ID.String(),
		"revision": strconv.FormatInt(w.resume.Revision, 10), "section_key": projectToolSectionKey,
		"entry": map[string]any{"id": w.plainEntry, "title": "Unlinked work", "projectId": w.foreign.ID.String()},
	}, "not_found")
	doc := projectToolResumeDocument(t, projectToolEntry(w.foreign.ID, "Foreign"))
	requireProjectToolError(t, w.session, "create_resume", map[string]any{
		"idempotency_key": uuid.NewString(), "title": "Foreign link", "document": projectToolDocumentMap(t, doc),
	}, "not_found")
	if after := w.snapshot(t, w.h.user.ID); after != before {
		t.Fatalf("refused resume writes changed the caller's account:\nbefore %s\nafter  %s", before, after)
	}
	if after := w.snapshot(t, w.other); after != otherBefore {
		t.Fatalf("refused resume writes changed the other account:\nbefore %s\nafter  %s", otherBefore, after)
	}
}
