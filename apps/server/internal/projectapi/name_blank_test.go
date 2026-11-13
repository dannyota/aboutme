package projectapi

import (
	"testing"

	"github.com/google/uuid"
)

func TestProjects_NameIsBlankAsTheRendererTrims(t *testing.T) {
	h := newHarness(t)
	body := func(slug, escapedName string) string {
		return `{"slug":"` + slug + `","defaultLanguage":"en","text":{"en":{"name":"` + escapedName + `"}}}`
	}
	assertCode(t, h.request(t, "POST", "/api/v1/projects", body("bom-name", `\ufeff`), uuid.NewString(), ""), 422, "document_invalid")
	if created := h.request(t, "POST", "/api/v1/projects", body("nel-name", `\u0085`), uuid.NewString(), ""); created.Code != 201 {
		t.Fatalf("U+0085 name: status = %d, body = %s; want 201", created.Code, created.Body.String())
	}
}
