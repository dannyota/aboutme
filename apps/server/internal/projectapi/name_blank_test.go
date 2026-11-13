package projectapi

import (
	"testing"

	"github.com/google/uuid"
)

// TestProjects_NameIsBlankAsTheRendererTrims proves the write path refuses a
// default-language name of only U+FEFF, which the renderer reads as blank,
// and stores one of only U+0085, which it does not.
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
