package mcpapi

import (
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestProjectTools_ResumeWriteWithProjectsReadMayAddOrChangeProjectID(t *testing.T) {
	w := newProjectToolWorld(t, "resumes:write projects:read")
	revision := w.resume.Revision
	requireProjectToolSuccess(t, w.session, "upsert_entry", map[string]any{
		"idempotency_key": uuid.NewString(), "resume_id": w.resume.ID.String(), "revision": strconv.FormatInt(revision, 10),
		"section_key": projectToolSectionKey,
		"entry":       map[string]any{"id": w.plainEntry, "title": "Unlinked work", "projectId": w.beacon.ID.String()},
	})
	revision++
	requireProjectToolSuccess(t, w.session, "upsert_entry", map[string]any{
		"idempotency_key": uuid.NewString(), "resume_id": w.resume.ID.String(), "revision": strconv.FormatInt(revision, 10),
		"section_key": projectToolSectionKey,
		"entry":       map[string]any{"id": w.linkedEntry, "title": "Atlas, my words", "projectId": w.carto.ID.String()},
	})
	links := w.links(t, w.resume.ID)
	if len(links) != 2 || links[w.plainEntry] != w.beacon.ID.String() || links[w.linkedEntry] != w.carto.ID.String() {
		t.Fatalf("links after add and change = %v", links)
	}
	doc := projectToolResumeDocument(t, projectToolEntry(w.atlas.ID, "Atlas again"))
	requireProjectToolSuccess(t, w.session, "create_resume", map[string]any{
		"idempotency_key": uuid.NewString(), "title": "Linked by an agent", "document": projectToolDocumentMap(t, doc),
	})
	if count := w.resumeCount(t, w.h.user.ID); count != 2 {
		t.Fatalf("resume count after a linking create = %d, want 2", count)
	}
}

func TestProjectTools_ResumeWriteWithoutProjectsReadMayNotAddOrChangeProjectID(t *testing.T) {
	for _, scopes := range []string{"resumes:write", "resumes:write projects:write"} {
		t.Run(scopes, func(t *testing.T) {
			w := newProjectToolWorld(t, scopes)
			before := w.snapshot(t, w.h.user.ID)
			revision := strconv.FormatInt(w.resume.Revision, 10)
			requireProjectToolError(t, w.session, "upsert_entry", map[string]any{
				"idempotency_key": uuid.NewString(), "resume_id": w.resume.ID.String(), "revision": revision,
				"section_key": projectToolSectionKey,
				"entry":       map[string]any{"id": w.plainEntry, "title": "Unlinked work", "projectId": w.beacon.ID.String()},
			}, "scope_denied")
			requireProjectToolError(t, w.session, "upsert_entry", map[string]any{
				"idempotency_key": uuid.NewString(), "resume_id": w.resume.ID.String(), "revision": revision,
				"section_key": projectToolSectionKey,
				"entry":       map[string]any{"id": w.linkedEntry, "title": "Atlas, my words", "projectId": w.carto.ID.String()},
			}, "scope_denied")
			doc := projectToolResumeDocument(t, projectToolEntry(w.atlas.ID, "Atlas again"))
			requireProjectToolError(t, w.session, "create_resume", map[string]any{
				"idempotency_key": uuid.NewString(), "title": "Linked by an agent", "document": projectToolDocumentMap(t, doc),
			}, "scope_denied")
			if after := w.snapshot(t, w.h.user.ID); after != before {
				t.Fatalf("refused link writes changed the account:\nbefore %s\nafter  %s", before, after)
			}
		})
	}
}

func TestProjectTools_ResumeWriteOnlyMayKeepOrRemoveProjectID(t *testing.T) {
	w := newProjectToolWorld(t, "resumes:write")
	revision := w.resume.Revision
	requireProjectToolSuccess(t, w.session, "upsert_entry", map[string]any{
		"idempotency_key": uuid.NewString(), "resume_id": w.resume.ID.String(), "revision": strconv.FormatInt(revision, 10),
		"section_key": projectToolSectionKey,
		"entry":       map[string]any{"id": w.linkedEntry, "title": "Atlas, rewritten", "projectId": w.atlas.ID.String()},
	})
	revision++
	requireProjectToolSuccess(t, w.session, "update_section", map[string]any{
		"idempotency_key": uuid.NewString(), "resume_id": w.resume.ID.String(), "revision": strconv.FormatInt(revision, 10),
		"section_key": projectToolSectionKey, "display_name": "Selected work",
		"entry_order": []any{w.plainEntry, w.linkedEntry},
	})
	revision++
	if links := w.links(t, w.resume.ID); len(links) != 1 || links[w.linkedEntry] != w.atlas.ID.String() {
		t.Fatalf("links after keep writes = %v", links)
	}
	requireProjectToolSuccess(t, w.session, "upsert_entry", map[string]any{
		"idempotency_key": uuid.NewString(), "resume_id": w.resume.ID.String(), "revision": strconv.FormatInt(revision, 10),
		"section_key": projectToolSectionKey,
		"entry":       map[string]any{"id": w.linkedEntry, "title": "Atlas, rewritten"},
	})
	if links := w.links(t, w.resume.ID); len(links) != 0 {
		t.Fatalf("links after remove = %v, want none", links)
	}
}

func TestProjectTools_OldGrantGetsScopeDeniedOnEveryProjectTool(t *testing.T) {
	w := newProjectToolWorld(t, projectToolOldGrant)
	before := w.snapshot(t, w.h.user.ID)
	for _, tool := range projectToolNames {
		t.Run(tool, func(t *testing.T) {
			requireProjectToolError(t, w.session, tool, w.arguments(tool), "scope_denied")
		})
	}
	if after := w.snapshot(t, w.h.user.ID); after != before {
		t.Fatalf("scope-denied project tools changed the account:\nbefore %s\nafter  %s", before, after)
	}
}

func TestProjectTools_GrantMissingOneRequiredScopeGetsScopeDenied(t *testing.T) {
	required := map[string][]string{
		"list_projects":         {"projects:read"},
		"get_project":           {"projects:read"},
		"create_project":        {"projects:write"},
		"update_project":        {"projects:write", "resumes:write"},
		"delete_project":        {"projects:write", "resumes:write"},
		"link_project":          {"resumes:write", "projects:read"},
		"refresh_project_entry": {"resumes:write", "projects:read"},
	}
	for _, tool := range projectToolNames {
		for _, missing := range required[tool] {
			t.Run(tool+" without "+missing, func(t *testing.T) {
				w := newProjectToolWorld(t, projectToolScopesWithout(missing))
				before := w.snapshot(t, w.h.user.ID)
				requireProjectToolError(t, w.session, tool, w.arguments(tool), "scope_denied")
				if after := w.snapshot(t, w.h.user.ID); after != before {
					t.Fatalf("scope-denied %s changed the account:\nbefore %s\nafter  %s", tool, before, after)
				}
			})
		}
	}
}

func TestProjectTools_LinkWithoutProjectsReadCannotTellOwnProjectFromForeign(t *testing.T) {
	for _, scopes := range []string{projectToolOldGrant, projectToolScopesWithout("projects:read")} {
		t.Run(scopes, func(t *testing.T) {
			w := newProjectToolWorld(t, scopes)
			foreignResume, foreignEntry := w.foreignLinkedResume(t)
			before := w.snapshot(t, w.h.user.ID)
			otherBefore := w.snapshot(t, w.other)
			revision := strconv.FormatInt(w.resume.Revision, 10)
			for _, tc := range []struct {
				name string
				tool string
				args map[string]any
			}{
				{"link own project", "link_project", map[string]any{
					"idempotency_key": uuid.NewString(), "resume_id": w.resume.ID.String(), "revision": revision,
					"project_id": w.beacon.ID.String(),
				}},
				{"link another account's project", "link_project", map[string]any{
					"idempotency_key": uuid.NewString(), "resume_id": w.resume.ID.String(), "revision": revision,
					"project_id": w.foreign.ID.String(),
				}},
				{"refresh entry of own project", "refresh_project_entry", map[string]any{
					"idempotency_key": uuid.NewString(), "resume_id": w.resume.ID.String(), "revision": revision,
					"entry_id": w.linkedEntry,
				}},
				{"refresh entry of another account's project", "refresh_project_entry", map[string]any{
					"idempotency_key": uuid.NewString(), "resume_id": foreignResume.ID.String(),
					"revision": strconv.FormatInt(foreignResume.Revision, 10), "entry_id": foreignEntry,
				}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					requireProjectToolError(t, w.session, tc.tool, tc.args, "scope_denied")
				})
			}
			if after := w.snapshot(t, w.h.user.ID); after != before {
				t.Fatalf("scope-denied link calls changed the account:\nbefore %s\nafter  %s", before, after)
			}
			if after := w.snapshot(t, w.other); after != otherBefore {
				t.Fatalf("scope-denied link calls changed the other account:\nbefore %s\nafter  %s", otherBefore, after)
			}
		})
	}
}

func projectToolScopesWithout(missing string) string {
	kept := make([]string, 0, 3)
	for _, scope := range strings.Fields(projectToolAllScopes) {
		if scope != missing {
			kept = append(kept, scope)
		}
	}
	return strings.Join(kept, " ")
}
