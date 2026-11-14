package mcpapi

import (
	"strconv"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func requireNarrowedCallRefused(t *testing.T, name string, outcome projectToolCallOutcome, codes ...string) {
	t.Helper()
	if !outcome.waited {
		t.Errorf("%s finished without waiting on the owner lock", name)
	}
	if outcome.err != nil {
		t.Fatalf("%s: protocol error %v, want a refusal", name, outcome.err)
	}
	got := toolErrorText(outcome.result)
	if !outcome.result.IsError {
		t.Fatalf("%s succeeded under a narrowed grant, want one of %v", name, codes)
	}
	for _, code := range codes {
		if got == code {
			return
		}
	}
	t.Fatalf("%s = %q, want one of %v", name, got, codes)
}

func TestProjectTools_RefreshRereadsTheGrantUnderTheOwnerLock(t *testing.T) {
	w := newProjectToolWorld(t, projectToolAllScopes)
	before := w.snapshot(t, w.h.user.ID)
	args := w.arguments("refresh_project_entry")
	outcome := narrowGrantWhileCallWaits(t, w.h, projectToolScopesWithout("projects:read"), func() (*mcp.CallToolResult, error) {
		return callProjectTool(w.session, "refresh_project_entry", args)
	})
	requireNarrowedCallRefused(t, "refresh_project_entry", outcome, "scope_denied")
	if after := w.snapshot(t, w.h.user.ID); after != before {
		t.Fatalf("refused refresh wrote:\nbefore %s\nafter  %s", before, after)
	}
}

func TestProjectTools_LibraryWritesRereadTheGrantUnderTheOwnerLock(t *testing.T) {
	for _, tool := range []string{"update_project", "delete_project"} {
		t.Run(tool, func(t *testing.T) {
			w := newProjectToolWorld(t, projectToolAllScopes)
			before := w.snapshot(t, w.h.user.ID)
			args := map[string]any{
				"idempotency_key": uuid.NewString(), "project_id": w.atlas.ID.String(),
				"revision": strconv.FormatInt(w.atlas.Revision, 10),
			}
			if tool == "update_project" {
				args["slug"] = "atlas-renamed"
				args["document"] = projectToolDocumentArgument("Atlas renamed")
			}
			outcome := narrowGrantWhileCallWaits(t, w.h, projectToolScopesWithout("projects:write"), func() (*mcp.CallToolResult, error) {
				return callProjectTool(w.session, tool, args)
			})
			requireNarrowedCallRefused(t, tool, outcome, "scope_denied", "agent_access_unavailable")
			if after := w.snapshot(t, w.h.user.ID); after != before {
				t.Fatalf("refused %s changed the project or its linking resume:\nbefore %s\nafter  %s", tool, before, after)
			}
		})
	}
}

func TestProjectTools_ResumeWriteHonoursAGrantNarrowedBetweenRequests(t *testing.T) {
	w := newProjectToolWorld(t, projectToolOldGrant)
	before := w.snapshot(t, w.h.user.ID)
	storeProjectToolGrantScopes(t, w.h, "resumes:read")
	requireProjectToolError(t, w.session, "update_section", w.sectionRenameArguments(), "scope_denied")
	if after := w.snapshot(t, w.h.user.ID); after != before {
		t.Fatalf("refused update_section wrote:\nbefore %s\nafter  %s", before, after)
	}
}

func TestProjectTools_ResumeWriteRereadsTheGrantUnderTheOwnerLock(t *testing.T) {
	w := newProjectToolWorld(t, projectToolOldGrant)
	before := w.snapshot(t, w.h.user.ID)
	args := w.sectionRenameArguments()
	outcome := narrowGrantWhileCallWaits(t, w.h, "resumes:read", func() (*mcp.CallToolResult, error) {
		return callProjectTool(w.session, "update_section", args)
	})
	requireNarrowedCallRefused(t, "update_section", outcome, "scope_denied", "agent_access_unavailable")
	if after := w.snapshot(t, w.h.user.ID); after != before {
		t.Fatalf("refused update_section wrote:\nbefore %s\nafter  %s", before, after)
	}
}

func (w *projectToolWorld) sectionRenameArguments() map[string]any {
	return map[string]any{
		"idempotency_key": uuid.NewString(), "resume_id": w.resume.ID.String(),
		"revision": strconv.FormatInt(w.resume.Revision, 10), "section_key": projectToolSectionKey,
		"display_name": "Renamed while narrowed",
	}
}
