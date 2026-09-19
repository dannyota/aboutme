package resumeapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

// Document v4 adds customization.header.photoPosition and a project entry
// subtitle (docs/adr/0044-header-photo-position-and-project-subtitle.md). A v1
// to v3 client cannot express either, so its writes must keep the stored
// values.

const (
	v4ProjectKept    = "018f0000-0000-7000-8000-0000000000d1"
	v4ProjectDeleted = "018f0000-0000-7000-8000-0000000000d2"
	v4ProjectAdded   = "018f0000-0000-7000-8000-0000000000d3"
)

func seedPhotoPosition(t *testing.T, h *resumeAPITestHarness, position schema.PhotoPosition) (uuid.UUID, int64) {
	t.Helper()
	created := h.createResume(t)
	doc := created.Doc
	doc.Customization.Header = &schema.HeaderClass{
		Align: schema.Center, DetailsLayout: schema.Inline, IconStyle: schema.Outline, PhotoPosition: &position,
	}
	revision, err := h.resumes.SaveDocument(h.ctx, h.userID, created.ID, doc, created.Revision)
	if err != nil {
		t.Fatalf("seed photo position: %v", err)
	}
	return created.ID, revision
}

func storedPhotoPosition(t *testing.T, h *resumeAPITestHarness, id uuid.UUID) (resumeHeader *schema.HeaderClass, revision int64) {
	t.Helper()
	stored, err := h.resumes.Get(h.ctx, h.userID, id)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	return stored.Doc.Customization.Header, stored.Revision
}

func TestWireVersion_OldClientCustomizationKeepsPhotoPosition(t *testing.T) {
	for _, version := range []string{"1", "2", "3"} {
		t.Run("v"+version, func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			id, revision := seedPhotoPosition(t, h, schema.Right)
			response := resumeRequest(t, h, http.MethodPatch,
				apiResumePath+"/"+id.String()+"/customization",
				`{"deltas":[{"op":"set","path":"header.align","value":"left"}]}`,
				revision, uuid.New(), version)
			if response.status != http.StatusOK || response.header.Get(wireVersionHeader) != version {
				t.Fatalf("v%s write = %d schema=%q body=%s",
					version, response.status, response.header.Get(wireVersionHeader), response.body)
			}
			var emitted map[string]any
			if err := json.Unmarshal(decodeResumeResource(t, response).Document, &emitted); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if containsKey(emitted, "photoPosition") {
				t.Fatalf("v%s response carries photoPosition: %v", version, emitted)
			}
			header, _ := storedPhotoPosition(t, h, id)
			if header == nil || header.Align != schema.AlignLeft {
				t.Fatalf("stored header = %#v, want the client's align change", header)
			}
			if header.PhotoPosition == nil || *header.PhotoPosition != schema.Right {
				t.Fatalf("stored photoPosition = %v, want right", header.PhotoPosition)
			}
		})
	}
}

func TestWireVersion_OldClientPersonalDetailsKeepPhotoPosition(t *testing.T) {
	h := newResumeAPITestHarness(t)
	id, revision := seedPhotoPosition(t, h, schema.PhotoPositionLeft)
	response := resumeRequest(t, h, http.MethodPatch,
		apiResumePath+"/"+id.String()+"/personal-details", `{"fullName":"Ada L"}`,
		revision, uuid.New(), "3")
	if response.status != http.StatusOK {
		t.Fatalf("v3 personal details = %d %s", response.status, response.body)
	}
	header, _ := storedPhotoPosition(t, h, id)
	if header == nil || header.PhotoPosition == nil || *header.PhotoPosition != schema.PhotoPositionLeft {
		t.Fatalf("stored header = %#v, want photoPosition left", header)
	}
}

func TestWireVersion_OldClientUnsetHeaderDropsPhotoPosition(t *testing.T) {
	h := newResumeAPITestHarness(t)
	id, revision := seedPhotoPosition(t, h, schema.Right)
	response := resumeRequest(t, h, http.MethodPatch,
		apiResumePath+"/"+id.String()+"/customization",
		`{"deltas":[{"op":"unset","path":"header"}]}`, revision, uuid.New(), "3")
	if response.status != http.StatusOK {
		t.Fatalf("v3 unset header = %d %s", response.status, response.body)
	}
	if header, _ := storedPhotoPosition(t, h, id); header != nil {
		t.Fatalf("stored header = %#v, want absent", header)
	}
}

func TestWireVersion_OldClientCannotSetPhotoPosition(t *testing.T) {
	h := newResumeAPITestHarness(t)
	id, revision := seedPhotoPosition(t, h, schema.Top)
	response := resumeRequest(t, h, http.MethodPatch,
		apiResumePath+"/"+id.String()+"/customization",
		`{"deltas":[{"op":"set","path":"header.photoPosition","value":"left"}]}`,
		revision, uuid.New(), "3")
	if response.status == http.StatusOK || response.status >= http.StatusInternalServerError {
		t.Fatalf("v3 photoPosition set = %d %s, want a client error", response.status, response.body)
	}
}

func TestWireVersion_CurrentClientSetsAndUnsetsPhotoPosition(t *testing.T) {
	h := newResumeAPITestHarness(t)
	id, revision := seedPhotoPosition(t, h, schema.Top)
	path := apiResumePath + "/" + id.String() + "/customization"
	for _, position := range []schema.PhotoPosition{schema.PhotoPositionLeft, schema.Right, schema.Top} {
		set := resumeRequest(t, h, http.MethodPatch, path,
			`{"deltas":[{"op":"set","path":"header.photoPosition","value":"`+string(position)+`"}]}`,
			revision, uuid.New(), "4")
		if set.status != http.StatusOK {
			t.Fatalf("set photoPosition %s = %d %s", position, set.status, set.body)
		}
		var header *schema.HeaderClass
		header, revision = storedPhotoPosition(t, h, id)
		if header == nil || header.PhotoPosition == nil || *header.PhotoPosition != position {
			t.Fatalf("stored header = %#v, want photoPosition %s", header, position)
		}
	}

	for _, bad := range []string{`"bottom"`, `"Left"`, `""`, `1`, `null`} {
		rejected := resumeRequest(t, h, http.MethodPatch, path,
			`{"deltas":[{"op":"set","path":"header.photoPosition","value":`+bad+`}]}`,
			revision, uuid.New(), "4")
		if rejected.status == http.StatusOK || rejected.status >= http.StatusInternalServerError {
			t.Fatalf("photoPosition %s = %d %s, want a client error", bad, rejected.status, rejected.body)
		}
	}

	unset := resumeRequest(t, h, http.MethodPatch, path,
		`{"deltas":[{"op":"unset","path":"header.photoPosition"}]}`, revision, uuid.New(), "4")
	if unset.status != http.StatusOK {
		t.Fatalf("unset photoPosition = %d %s", unset.status, unset.body)
	}
	header, _ := storedPhotoPosition(t, h, id)
	if header == nil || header.PhotoPosition != nil {
		t.Fatalf("stored header = %#v, want a header without photoPosition", header)
	}
}

func seedProjectSubtitles(t *testing.T, h *resumeAPITestHarness) (uuid.UUID, int64) {
	t.Helper()
	created := h.createResume(t)
	doc := created.Doc
	title, kept, deleted := "Engine", "Go, PostgreSQL", "Rust"
	doc.Content["projects"] = schema.NewProjectSection(nil, nil, []schema.ProjectEntry{
		{ID: v4ProjectKept, Title: &title, Subtitle: &kept},
		{ID: v4ProjectDeleted, Subtitle: &deleted},
	})
	doc.Customization.Layout.Sections.Main = append(doc.Customization.Layout.Sections.Main, "projects")
	revision, err := h.resumes.SaveDocument(h.ctx, h.userID, created.ID, doc, created.Revision)
	if err != nil {
		t.Fatalf("seed project subtitles: %v", err)
	}
	return created.ID, revision
}

func storedProjectEntries(t *testing.T, h *resumeAPITestHarness, id uuid.UUID) ([]schema.ProjectEntry, int64) {
	t.Helper()
	stored, err := h.resumes.Get(h.ctx, h.userID, id)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	return stored.Doc.Content["projects"].ProjectEntries, stored.Revision
}

func subtitleOf(entry schema.ProjectEntry) string {
	if entry.Subtitle == nil {
		return "<absent>"
	}
	return *entry.Subtitle
}

func TestWireVersion_OldClientEntryWritesKeepProjectSubtitles(t *testing.T) {
	for _, version := range []string{"1", "2", "3"} {
		t.Run("v"+version, func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			id, revision := seedProjectSubtitles(t, h)
			base := apiResumePath + "/" + id.String() + "/entries/projects"

			updated := resumeRequest(t, h, http.MethodPatch, base,
				`{"entry":{"id":"`+v4ProjectKept+`","title":"Engine v2"}}`, revision, uuid.New(), version)
			if updated.status != http.StatusOK || updated.header.Get(wireVersionHeader) != version {
				t.Fatalf("v%s upsert = %d schema=%q body=%s",
					version, updated.status, updated.header.Get(wireVersionHeader), updated.body)
			}
			var emitted map[string]any
			if err := json.Unmarshal(decodeResumeResource(t, updated).Document, &emitted); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if content, ok := emitted["content"].(map[string]any); !ok || containsKey(content["projects"], "subtitle") {
				t.Fatalf("v%s response carries a project subtitle: %v", version, emitted["content"])
			}
			_, revision = storedProjectEntries(t, h, id)

			added := resumeRequest(t, h, http.MethodPatch, base,
				`{"entry":{"id":"`+v4ProjectAdded+`","title":"New"}}`, revision, uuid.New(), version)
			if added.status != http.StatusOK {
				t.Fatalf("v%s add = %d %s", version, added.status, added.body)
			}
			_, revision = storedProjectEntries(t, h, id)

			deleted := resumeRequest(t, h, http.MethodDelete, base+"/"+v4ProjectDeleted, "", revision, uuid.New(), version)
			if deleted.status != http.StatusOK && deleted.status != http.StatusNoContent {
				t.Fatalf("v%s delete = %d %s", version, deleted.status, deleted.body)
			}

			entries, _ := storedProjectEntries(t, h, id)
			if len(entries) != 2 || entries[0].ID != v4ProjectKept || entries[1].ID != v4ProjectAdded {
				t.Fatalf("stored project entries = %#v", entries)
			}
			if entries[0].Title == nil || *entries[0].Title != "Engine v2" || subtitleOf(entries[0]) != "Go, PostgreSQL" {
				t.Fatalf("updated entry title=%v subtitle=%s, want the new title and the stored subtitle",
					entries[0].Title, subtitleOf(entries[0]))
			}
			if entries[1].Subtitle != nil {
				t.Fatalf("added entry subtitle = %s, want absent", subtitleOf(entries[1]))
			}
		})
	}
}

func TestWireVersion_OldClientOtherWritesKeepProjectSubtitles(t *testing.T) {
	h := newResumeAPITestHarness(t)
	id, revision := seedProjectSubtitles(t, h)
	for _, write := range []struct{ path, body string }{
		{"/sections/projects", `{"displayName":"Side projects"}`},
		{"/personal-details", `{"fullName":"Ada L"}`},
		{"/customization", `{"deltas":[{"op":"set","path":"colors.primary","value":"#112233"}]}`},
	} {
		response := resumeRequest(t, h, http.MethodPatch, apiResumePath+"/"+id.String()+write.path,
			write.body, revision, uuid.New(), "3")
		if response.status != http.StatusOK {
			t.Fatalf("v3 %s = %d %s", write.path, response.status, response.body)
		}
		var entries []schema.ProjectEntry
		entries, revision = storedProjectEntries(t, h, id)
		if len(entries) != 2 || subtitleOf(entries[0]) != "Go, PostgreSQL" || subtitleOf(entries[1]) != "Rust" {
			t.Fatalf("after v3 %s, stored project entries = %#v", write.path, entries)
		}
	}
}

func TestWireVersion_OldClientCannotSetProjectSubtitle(t *testing.T) {
	h := newResumeAPITestHarness(t)
	id, revision := seedProjectSubtitles(t, h)
	response := resumeRequest(t, h, http.MethodPatch, apiResumePath+"/"+id.String()+"/entries/projects",
		`{"entry":{"id":"`+v4ProjectKept+`","subtitle":"Changed"}}`, revision, uuid.New(), "3")
	if response.status == http.StatusOK || response.status >= http.StatusInternalServerError {
		t.Fatalf("v3 subtitle set = %d %s, want a client error", response.status, response.body)
	}
	if entries, _ := storedProjectEntries(t, h, id); subtitleOf(entries[0]) != "Go, PostgreSQL" {
		t.Fatalf("stored subtitle = %s after a rejected write", subtitleOf(entries[0]))
	}
}

func TestWireVersion_CurrentClientSetsAndClearsProjectSubtitle(t *testing.T) {
	h := newResumeAPITestHarness(t)
	id, revision := seedProjectSubtitles(t, h)
	path := apiResumePath + "/" + id.String() + "/entries/projects"
	set := resumeRequest(t, h, http.MethodPatch, path,
		`{"entry":{"id":"`+v4ProjectKept+`","subtitle":"TypeScript"}}`, revision, uuid.New(), "4")
	if set.status != http.StatusOK {
		t.Fatalf("set subtitle = %d %s", set.status, set.body)
	}
	entries, revision := storedProjectEntries(t, h, id)
	if subtitleOf(entries[0]) != "TypeScript" {
		t.Fatalf("stored subtitle = %s, want TypeScript", subtitleOf(entries[0]))
	}
	cleared := resumeRequest(t, h, http.MethodPatch, path,
		`{"entry":{"id":"`+v4ProjectKept+`"}}`, revision, uuid.New(), "4")
	if cleared.status != http.StatusOK {
		t.Fatalf("clear subtitle = %d %s", cleared.status, cleared.body)
	}
	if entries, _ = storedProjectEntries(t, h, id); entries[0].Subtitle != nil {
		t.Fatalf("stored subtitle = %s after a v4 write without it, want absent", subtitleOf(entries[0]))
	}
}
