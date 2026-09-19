package resumeapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

// Document v3 adds details[].display and customization.font.textAlign
// (docs/adr/0041-contact-link-display-and-body-justify.md). A v1 or v2 client
// cannot express either field, so its writes must keep the stored values.

const (
	v3DetailKept    = "018f0000-0000-7000-8000-0000000000b1"
	v3DetailDropped = "018f0000-0000-7000-8000-0000000000b2"
	v3DetailAdded   = "018f0000-0000-7000-8000-0000000000b3"
)

func seedV3Fields(t *testing.T, h *resumeAPITestHarness) (uuid.UUID, int64) {
	t.Helper()
	created := h.createResume(t)
	doc := created.Doc
	label, full, justify := schema.Label, schema.Full, schema.Justify
	doc.PersonalDetails.Details = []schema.PersonalDetail{
		{ID: v3DetailKept, Type: schema.Github, Value: "https://github.com/ada", Display: &label},
		{ID: v3DetailDropped, Type: schema.Website, Value: "https://ada.example.com", Display: &full},
	}
	doc.Customization.Font.TextAlign = &justify
	revision, err := h.resumes.SaveDocument(h.ctx, h.userID, created.ID, doc, created.Revision)
	if err != nil {
		t.Fatalf("seed v3 fields: %v", err)
	}
	return created.ID, revision
}

func TestWireVersion_OldClientPersonalDetailsKeepV3Fields(t *testing.T) {
	for _, version := range []string{"1", "2"} {
		t.Run("v"+version, func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			id, revision := seedV3Fields(t, h)
			body := `{"fullName":"Ada","details":[` +
				`{"id":"` + v3DetailKept + `","type":"github","value":"https://github.com/ada-l","isHidden":false},` +
				`{"id":"` + v3DetailAdded + `","type":"custom","value":"https://orcid.example/ada","isHidden":false}]}`
			response := resumeRequest(t, h, http.MethodPatch,
				apiResumePath+"/"+id.String()+"/personal-details", body, revision, uuid.New(), version)
			if response.status != http.StatusOK || response.header.Get(wireVersionHeader) != version {
				t.Fatalf("v%s write = %d schema=%q body=%s",
					version, response.status, response.header.Get(wireVersionHeader), response.body)
			}
			var emitted map[string]any
			if err := json.Unmarshal(decodeResumeResource(t, response).Document, &emitted); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if containsKey(emitted, "display") || containsKey(emitted, "textAlign") {
				t.Fatalf("v%s response carries a v3 field: %v", version, emitted)
			}

			stored, err := h.resumes.Get(h.ctx, h.userID, id)
			if err != nil {
				t.Fatalf("reload: %v", err)
			}
			details := stored.Doc.PersonalDetails.Details
			if len(details) != 2 || details[0].ID != v3DetailKept || details[1].ID != v3DetailAdded {
				t.Fatalf("stored details = %#v", details)
			}
			if details[0].Value != "https://github.com/ada-l" {
				t.Fatalf("old client value change lost: %q", details[0].Value)
			}
			if details[0].Display == nil || *details[0].Display != schema.Label {
				t.Fatalf("surviving detail display = %v, want label", details[0].Display)
			}
			if details[1].Display != nil {
				t.Fatalf("added detail display = %v, want absent", *details[1].Display)
			}
			if got := stored.Doc.Customization.Font.TextAlign; got == nil || *got != schema.Justify {
				t.Fatalf("stored textAlign = %v, want justify", got)
			}
		})
	}
}

func TestWireVersion_OldClientCustomizationKeepsTextAlign(t *testing.T) {
	h := newResumeAPITestHarness(t)
	id, revision := seedV3Fields(t, h)
	response := resumeRequest(t, h, http.MethodPatch,
		apiResumePath+"/"+id.String()+"/customization",
		`{"deltas":[{"op":"set","path":"colors.primary","value":"#112233"}]}`,
		revision, uuid.New(), "2")
	if response.status != http.StatusOK {
		t.Fatalf("v2 customization = %d %s", response.status, response.body)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, id)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := stored.Doc.Customization.Font.TextAlign; got == nil || *got != schema.Justify {
		t.Fatalf("stored textAlign = %v, want justify", got)
	}
	if stored.Doc.Customization.Colors.Primary != "#112233" {
		t.Fatalf("stored primary = %q", stored.Doc.Customization.Colors.Primary)
	}
	if details := stored.Doc.PersonalDetails.Details; len(details) != 2 ||
		details[1].Display == nil || *details[1].Display != schema.Full {
		t.Fatalf("stored details = %#v, want displays kept", details)
	}
}

func TestWireVersion_CurrentClientSetsAndUnsetsTextAlign(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	path := apiResumePath + "/" + created.ID.String() + "/customization"
	set := resumeRequest(t, h, http.MethodPatch, path,
		`{"deltas":[{"op":"set","path":"font.textAlign","value":"justify"}]}`,
		created.Revision, uuid.New(), "3")
	if set.status != http.StatusOK {
		t.Fatalf("set textAlign = %d %s", set.status, set.body)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := stored.Doc.Customization.Font.TextAlign; got == nil || *got != schema.Justify {
		t.Fatalf("stored textAlign = %v, want justify", got)
	}

	for _, bad := range []string{`"right"`, `"Justify"`, `1`, `null`} {
		rejected := resumeRequest(t, h, http.MethodPatch, path,
			`{"deltas":[{"op":"set","path":"font.textAlign","value":`+bad+`}]}`,
			stored.Revision, uuid.New(), "3")
		if rejected.status == http.StatusOK {
			t.Fatalf("textAlign %s accepted", bad)
		}
	}

	unset := resumeRequest(t, h, http.MethodPatch, path,
		`{"deltas":[{"op":"unset","path":"font.textAlign"}]}`, stored.Revision, uuid.New(), "3")
	if unset.status != http.StatusOK {
		t.Fatalf("unset textAlign = %d %s", unset.status, unset.body)
	}
	stored, err = h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if stored.Doc.Customization.Font.TextAlign != nil {
		t.Fatalf("textAlign after unset = %v, want absent", *stored.Doc.Customization.Font.TextAlign)
	}
}

func TestWireVersion_OldClientCannotSetTextAlign(t *testing.T) {
	h := newResumeAPITestHarness(t)
	created := h.createResume(t)
	response := resumeRequest(t, h, http.MethodPatch,
		apiResumePath+"/"+created.ID.String()+"/customization",
		`{"deltas":[{"op":"set","path":"font.textAlign","value":"justify"}]}`,
		created.Revision, uuid.New(), "2")
	if response.status == http.StatusOK || response.status >= http.StatusInternalServerError {
		t.Fatalf("v2 textAlign set = %d %s, want a client error", response.status, response.body)
	}
}

func containsKey(value any, key string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for name, child := range typed {
			if name == key || containsKey(child, key) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if containsKey(child, key) {
				return true
			}
		}
	}
	return false
}
