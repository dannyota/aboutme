package mcpapi

import (
	"bytes"
	"context"
	"maps"
	"strconv"
	"testing"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/project"
)

const projectToolKeyReuseCode = "validation_failed"

func TestProjectTools_ResumeToolKeyReuseIsValidationFailed(t *testing.T) {
	w := newProjectToolWorld(t, projectToolOldGrant)
	key := uuid.NewString()
	first := map[string]any{
		"idempotency_key": key, "resume_id": w.resume.ID.String(),
		"revision": strconv.FormatInt(w.resume.Revision, 10), "title": "First title",
	}
	firstResult := requireProjectToolSuccess(t, w.session, "update_resume_metadata", first)
	changed := maps.Clone(first)
	changed["title"] = "Second title"
	requireProjectToolError(t, w.session, "update_resume_metadata", changed, projectToolKeyReuseCode)
	if replay := requireProjectToolSuccess(t, w.session, "update_resume_metadata", first); !bytes.Equal(replay, firstResult) {
		t.Fatalf("exact retry = %s, want the first result %s", replay, firstResult)
	}
}

func TestProjectTools_KeyReuseWithADifferentRequestIsRefusedAndKeepsTheFirstResult(t *testing.T) {
	for _, tc := range []struct {
		tool   string
		first  func(w *projectToolWorld, key string) map[string]any
		change func(w *projectToolWorld, args map[string]any)
		check  func(t *testing.T, w *projectToolWorld)
	}{
		{
			tool: "create_project",
			first: func(_ *projectToolWorld, key string) map[string]any {
				return map[string]any{"idempotency_key": key, "slug": "echo", "document": projectToolDocumentArgument("Echo")}
			},
			change: func(_ *projectToolWorld, args map[string]any) {
				args["slug"] = "foxtrot"
				args["document"] = projectToolDocumentArgument("Foxtrot")
			},
			check: func(t *testing.T, w *projectToolWorld) {
				t.Helper()
				slugs := w.projectSlugs(t)
				if !slugs["echo"] || slugs["foxtrot"] || len(slugs) != 4 {
					t.Fatalf("project slugs = %v, want the world's three and echo only", slugs)
				}
			},
		},
		{
			tool: "update_project",
			first: func(w *projectToolWorld, key string) map[string]any {
				return map[string]any{
					"idempotency_key": key, "project_id": w.beacon.ID.String(),
					"revision": strconv.FormatInt(w.beacon.Revision, 10), "slug": w.beacon.Slug,
					"document": projectToolDocumentArgument("Beacon two"),
				}
			},
			change: func(_ *projectToolWorld, args map[string]any) {
				args["document"] = projectToolDocumentArgument("Beacon three")
			},
			check: func(t *testing.T, w *projectToolWorld) {
				t.Helper()
				stored := w.project(t, w.beacon.ID)
				if stored.Revision != w.beacon.Revision+1 || stored.Text["en"].Name != "Beacon two" {
					t.Fatalf("beacon = revision %d name %q, want revision %d name Beacon two",
						stored.Revision, stored.Text["en"].Name, w.beacon.Revision+1)
				}
			},
		},
		{
			tool: "delete_project",
			first: func(w *projectToolWorld, key string) map[string]any {
				return map[string]any{
					"idempotency_key": key, "project_id": w.carto.ID.String(),
					"revision": strconv.FormatInt(w.carto.Revision, 10),
				}
			},
			change: func(w *projectToolWorld, args map[string]any) {
				args["revision"] = strconv.FormatInt(w.carto.Revision+1, 10)
			},
			check: func(t *testing.T, w *projectToolWorld) {
				t.Helper()
				if slugs := w.projectSlugs(t); slugs["carto"] || !slugs["atlas"] || !slugs["beacon"] {
					t.Fatalf("project slugs = %v, want carto deleted and the others kept", slugs)
				}
			},
		},
		{
			tool: "link_project",
			first: func(w *projectToolWorld, key string) map[string]any {
				return map[string]any{
					"idempotency_key": key, "resume_id": w.resume.ID.String(),
					"revision": strconv.FormatInt(w.resume.Revision, 10), "project_id": w.beacon.ID.String(),
				}
			},
			change: func(w *projectToolWorld, args map[string]any) {
				args["entry_id"] = w.plainEntry
			},
			check: func(t *testing.T, w *projectToolWorld) {
				t.Helper()
				stored := w.storedResume(t)
				links := projectToolLinksOf(stored)
				beaconLinks := 0
				for _, linked := range links {
					if linked == w.beacon.ID.String() {
						beaconLinks++
					}
				}
				if stored.Revision != w.resume.Revision+1 || len(links) != 2 || links[w.plainEntry] != "" ||
					links[w.linkedEntry] != w.atlas.ID.String() || beaconLinks != 1 {
					t.Fatalf("resume = revision %d links %v, want revision %d with atlas and one new beacon entry",
						stored.Revision, links, w.resume.Revision+1)
				}
			},
		},
		{
			tool: "refresh_project_entry",
			first: func(w *projectToolWorld, key string) map[string]any {
				return map[string]any{
					"idempotency_key": key, "resume_id": w.resume.ID.String(),
					"revision": strconv.FormatInt(w.resume.Revision, 10), "entry_id": w.linkedEntry,
				}
			},
			change: func(w *projectToolWorld, args map[string]any) {
				args["revision"] = strconv.FormatInt(w.resume.Revision+1, 10)
			},
			check: func(t *testing.T, w *projectToolWorld) {
				t.Helper()
				stored := w.storedResume(t)
				if stored.Revision != w.resume.Revision+1 {
					t.Fatalf("resume revision = %d, want %d after one refresh", stored.Revision, w.resume.Revision+1)
				}
			},
		},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			w := newProjectToolWorld(t, projectToolAllScopes)
			first := tc.first(w, uuid.NewString())
			firstResult := requireProjectToolSuccess(t, w.session, tc.tool, first)
			changed := maps.Clone(first)
			tc.change(w, changed)
			requireProjectToolError(t, w.session, tc.tool, changed, projectToolKeyReuseCode)
			tc.check(t, w)
			if replay := requireProjectToolSuccess(t, w.session, tc.tool, first); !bytes.Equal(replay, firstResult) {
				t.Fatalf("exact retry = %s, want the first result %s", replay, firstResult)
			}
			tc.check(t, w)
		})
	}
}

func (w *projectToolWorld) projectSlugs(t *testing.T) map[string]bool {
	t.Helper()
	projects, err := project.NewStore().ListTx(context.Background(), w.h.q, w.h.user.ID)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	slugs := make(map[string]bool, len(projects))
	for _, p := range projects {
		slugs[p.Slug] = true
	}
	return slugs
}

func (w *projectToolWorld) project(t *testing.T, id uuid.UUID) project.Project {
	t.Helper()
	stored, err := project.NewStore().GetTx(context.Background(), w.h.q, w.h.user.ID, id)
	if err != nil {
		t.Fatalf("read project %s: %v", id, err)
	}
	return stored
}
