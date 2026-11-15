package mcpapi

import (
	"bytes"
	"strconv"
	"testing"

	"github.com/google/uuid"
)

func TestProjectTools_AnotherGrantCannotReplayAResumeToolAnswer(t *testing.T) {
	w := newProjectToolWorld(t, "resumes:read resumes:write projects:read")
	other := w.grantSession(t, "resumes:read resumes:write")
	args := map[string]any{
		"idempotency_key": uuid.NewString(), "resume_id": w.resume.ID.String(),
		"revision": strconv.FormatInt(w.resume.Revision, 10), "section_key": projectToolSectionKey,
		"entry": map[string]any{"id": w.plainEntry, "title": "Unlinked work", "projectId": w.beacon.ID.String()},
	}
	first := requireProjectToolSuccess(t, w.session, "upsert_entry", args)
	before := w.snapshot(t, w.h.user.ID)
	requireProjectToolError(t, other, "upsert_entry", args, "validation_failed")
	if after := w.snapshot(t, w.h.user.ID); after != before {
		t.Fatalf("another grant's reuse wrote:\nbefore %s\nafter  %s", before, after)
	}
	if replay := requireProjectToolSuccess(t, w.session, "upsert_entry", args); !bytes.Equal(replay, first) {
		t.Fatalf("own retry = %s, want the first answer %s", replay, first)
	}
}

func TestProjectTools_AnotherGrantCannotReplayAProjectToolAnswer(t *testing.T) {
	w := newProjectToolWorld(t, projectToolAllScopes)
	other := w.grantSession(t, projectToolAllScopes)
	args := map[string]any{
		"idempotency_key": uuid.NewString(), "slug": "echo", "document": projectToolDocumentArgument("Echo"),
	}
	first := requireProjectToolSuccess(t, w.session, "create_project", args)
	before := w.snapshot(t, w.h.user.ID)
	requireProjectToolError(t, other, "create_project", args, "validation_failed")
	if after := w.snapshot(t, w.h.user.ID); after != before {
		t.Fatalf("another grant's reuse wrote:\nbefore %s\nafter  %s", before, after)
	}
	if replay := requireProjectToolSuccess(t, w.session, "create_project", args); !bytes.Equal(replay, first) {
		t.Fatalf("own retry = %s, want the first answer %s", replay, first)
	}
}

func TestProjectTools_NarrowedGrantStillReplaysItsOwnAnswer(t *testing.T) {
	w := newProjectToolWorld(t, "resumes:read resumes:write projects:read")
	args := map[string]any{
		"idempotency_key": uuid.NewString(), "resume_id": w.resume.ID.String(),
		"revision": strconv.FormatInt(w.resume.Revision, 10), "section_key": projectToolSectionKey,
		"entry": map[string]any{"id": w.plainEntry, "title": "Unlinked work", "projectId": w.beacon.ID.String()},
	}
	first := requireProjectToolSuccess(t, w.session, "upsert_entry", args)
	storeProjectToolGrantScopes(t, w.h, "resumes:read resumes:write")
	before := w.snapshot(t, w.h.user.ID)
	if replay := requireProjectToolSuccess(t, w.session, "upsert_entry", args); !bytes.Equal(replay, first) {
		t.Fatalf("retry after narrowing = %s, want the first answer %s", replay, first)
	}
	if after := w.snapshot(t, w.h.user.ID); after != before {
		t.Fatalf("replay after narrowing wrote:\nbefore %s\nafter  %s", before, after)
	}
}
