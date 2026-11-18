package projectapi

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestEvidence_WithoutASourceEveryNestedRouteIsUnavailable(t *testing.T) {
	h := newHarness(t)
	id := idOf(t, h.create(t))
	for _, route := range []struct{ method, path string }{
		{"GET", "/evidence/candidates?kind=repository&page=1"},
		{"POST", "/evidence"},
		{"POST", "/evidence/" + uuid.NewString() + "/recheck"},
		{"PUT", "/evidence/order"},
		{"DELETE", "/evidence/" + uuid.NewString()},
	} {
		r := h.request(t, route.method, "/api/v1/projects/"+id+route.path, "", uuid.NewString(), `"r1"`)
		assertCode(t, r, 404, "evidence_unavailable")
	}
	r := h.request(t, "GET", "/api/v1/projects/"+id, "", "", "")
	if r.Code != 200 || strings.Contains(r.Body.String(), `"evidence"`) {
		t.Fatalf("GET project = %d %s, want no evidence field", r.Code, r.Body.String())
	}
}
