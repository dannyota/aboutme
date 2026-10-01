package resumeapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

// Document v5 adds customization.colorScheme
// (docs/design/public-page-theme.md). A v1 to v4 client cannot express it, so
// its writes must keep the stored value.

func seedColorScheme(t *testing.T, h *resumeAPITestHarness, scheme schema.ColorScheme) (uuid.UUID, int64) {
	t.Helper()
	created := h.createResume(t)
	doc := created.Doc
	doc.Customization.ColorScheme = &scheme
	revision, err := h.resumes.SaveDocument(h.ctx, h.userID, created.ID, doc, created.Revision)
	if err != nil {
		t.Fatalf("seed color scheme: %v", err)
	}
	return created.ID, revision
}

func storedColorScheme(t *testing.T, h *resumeAPITestHarness, id uuid.UUID) (scheme *schema.ColorScheme, revision int64) {
	t.Helper()
	stored, err := h.resumes.Get(h.ctx, h.userID, id)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	return stored.Doc.Customization.ColorScheme, stored.Revision
}

func requireStoredScheme(t *testing.T, h *resumeAPITestHarness, id uuid.UUID, want schema.ColorScheme) {
	t.Helper()
	got, _ := storedColorScheme(t, h, id)
	if got == nil || *got != want {
		t.Fatalf("stored colorScheme = %v, want %s", got, want)
	}
}

func TestWireVersion_OldClientCustomizationKeepsColorScheme(t *testing.T) {
	for _, version := range []string{"1", "2", "3", "4"} {
		t.Run("v"+version, func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			id, revision := seedColorScheme(t, h, schema.Dark)
			response := resumeRequest(t, h, http.MethodPatch,
				apiResumePath+"/"+id.String()+"/customization",
				`{"deltas":[{"op":"set","path":"pageFormat","value":"letter"}]}`,
				revision, uuid.New(), version)
			if response.status != http.StatusOK || response.header.Get(wireVersionHeader) != version {
				t.Fatalf("v%s write = %d schema=%q body=%s",
					version, response.status, response.header.Get(wireVersionHeader), response.body)
			}
			var emitted map[string]any
			if err := json.Unmarshal(decodeResumeResource(t, response).Document, &emitted); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if containsKey(emitted, "colorScheme") {
				t.Fatalf("v%s response carries colorScheme: %v", version, emitted)
			}
			stored, err := h.resumes.Get(h.ctx, h.userID, id)
			if err != nil {
				t.Fatalf("reload: %v", err)
			}
			if stored.Doc.Customization.PageFormat != schema.Letter {
				t.Fatalf("stored pageFormat = %s, want the client's change", stored.Doc.Customization.PageFormat)
			}
			requireStoredScheme(t, h, id, schema.Dark)
		})
	}
}

func TestWireVersion_OldClientPersonalDetailsKeepColorScheme(t *testing.T) {
	for _, version := range []string{"1", "4"} {
		t.Run("v"+version, func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			id, revision := seedColorScheme(t, h, schema.System)
			response := resumeRequest(t, h, http.MethodPatch,
				apiResumePath+"/"+id.String()+"/personal-details", `{"fullName":"Ada L"}`,
				revision, uuid.New(), version)
			if response.status != http.StatusOK {
				t.Fatalf("v%s personal details = %d %s", version, response.status, response.body)
			}
			requireStoredScheme(t, h, id, schema.System)
		})
	}
}

func TestWireVersion_OldClientUnsetHeaderKeepsColorScheme(t *testing.T) {
	h := newResumeAPITestHarness(t)
	id, revision := seedColorScheme(t, h, schema.Dark)
	stored, err := h.resumes.Get(h.ctx, h.userID, id)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	doc := stored.Doc
	doc.Customization.Header = &schema.HeaderClass{Align: schema.Center, DetailsLayout: schema.Inline, IconStyle: schema.Outline}
	revision, err = h.resumes.SaveDocument(h.ctx, h.userID, id, doc, revision)
	if err != nil {
		t.Fatalf("seed header: %v", err)
	}
	response := resumeRequest(t, h, http.MethodPatch,
		apiResumePath+"/"+id.String()+"/customization",
		`{"deltas":[{"op":"unset","path":"header"}]}`, revision, uuid.New(), "4")
	if response.status != http.StatusOK {
		t.Fatalf("v4 unset header = %d %s", response.status, response.body)
	}
	requireStoredScheme(t, h, id, schema.Dark)
}

func TestWireVersion_OldClientCannotSetColorScheme(t *testing.T) {
	h := newResumeAPITestHarness(t)
	id, revision := seedColorScheme(t, h, schema.Light)
	response := resumeRequest(t, h, http.MethodPatch,
		apiResumePath+"/"+id.String()+"/customization",
		`{"deltas":[{"op":"set","path":"colorScheme","value":"dark"}]}`,
		revision, uuid.New(), "4")
	if response.status == http.StatusOK || response.status >= http.StatusInternalServerError {
		t.Fatalf("v4 colorScheme set = %d %s, want a client error", response.status, response.body)
	}
	requireStoredScheme(t, h, id, schema.Light)
}

func TestWireVersion_CurrentClientSetsAndUnsetsColorScheme(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	id, revision := created.ID, created.Revision
	if scheme, _ := storedColorScheme(t, h, id); scheme != nil {
		t.Fatalf("new resume colorScheme = %v, want absent", *scheme)
	}
	path := apiResumePath + "/" + id.String() + "/customization"
	for _, scheme := range []schema.ColorScheme{schema.Dark, schema.System, schema.Light} {
		set := resumeRequest(t, h, http.MethodPatch, path,
			`{"deltas":[{"op":"set","path":"colorScheme","value":"`+string(scheme)+`"}]}`,
			revision, uuid.New(), "5")
		if set.status != http.StatusOK || set.header.Get(wireVersionHeader) != "5" {
			t.Fatalf("set colorScheme %s = %d %s", scheme, set.status, set.body)
		}
		var emitted map[string]any
		if err := json.Unmarshal(decodeResumeResource(t, set).Document, &emitted); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if !containsKey(emitted, "colorScheme") {
			t.Fatalf("v5 response lacks colorScheme %s: %v", scheme, emitted)
		}
		requireStoredScheme(t, h, id, scheme)
		_, revision = storedColorScheme(t, h, id)
	}

	for _, bad := range []string{`"auto"`, `"Dark"`, `""`, `1`, `null`, `true`} {
		rejected := resumeRequest(t, h, http.MethodPatch, path,
			`{"deltas":[{"op":"set","path":"colorScheme","value":`+bad+`}]}`,
			revision, uuid.New(), "5")
		if rejected.status == http.StatusOK || rejected.status >= http.StatusInternalServerError {
			t.Fatalf("colorScheme %s = %d %s, want a client error", bad, rejected.status, rejected.body)
		}
	}
	requireStoredScheme(t, h, id, schema.Light)

	unset := resumeRequest(t, h, http.MethodPatch, path,
		`{"deltas":[{"op":"unset","path":"colorScheme"}]}`, revision, uuid.New(), "5")
	if unset.status != http.StatusOK {
		t.Fatalf("unset colorScheme = %d %s", unset.status, unset.body)
	}
	if scheme, _ := storedColorScheme(t, h, id); scheme != nil {
		t.Fatalf("stored colorScheme = %v after unset, want absent", *scheme)
	}
}
