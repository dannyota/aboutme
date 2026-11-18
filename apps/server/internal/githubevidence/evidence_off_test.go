package githubevidence

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/project"
)

func TestEvidenceOff_EveryNestedRouteIsUnavailableAndReadsOmitEvidence(t *testing.T) {
	h := newHarness(t)
	off, err := New(Options{Config: config.GitHubEvidenceConfig{}})
	if err != nil {
		t.Fatal(err)
	}
	eh := wireEvidence(t, h, off)
	eh.project = eh.createProject("atlas")
	eh.seedEvidence(1)
	item := eh.storedIDs()[0]
	routes := []struct{ method, suffix string }{
		{http.MethodGet, "/candidates?kind=repository&page=1"},
		{http.MethodPost, ""},
		{http.MethodPost, "/" + item + "/recheck"},
		{http.MethodPut, "/order"},
		{http.MethodDelete, "/" + item},
		{http.MethodGet, ""},
		{http.MethodPatch, "/order"},
	}
	for _, route := range routes {
		signedIn := eh.write(route.method, route.suffix, "", uuid.NewString(), eh.revision())
		requireError(t, signedIn, http.StatusNotFound, "evidence_unavailable")
		anonymous := eh.do(call{method: route.method, path: eh.path(route.suffix)})
		requireError(t, anonymous, http.StatusNotFound, "evidence_unavailable")
	}
	if _, present := eh.readProject()["evidence"]; present {
		t.Fatal("GET project carries evidence with the capability off")
	}
	if len(eh.stored()) != 1 {
		t.Fatal("off mode changed stored evidence")
	}
	full := eh.sizedDocument(project.MaxStoredBytes - project.EvidenceItemBytes)
	if rec := eh.putDocument(full); rec.Code != http.StatusOK {
		t.Fatalf("PUT with room for the stored item = %d %s", rec.Code, rec.Body.String())
	}
	eh.seedEvidence(1)
	requireError(t, eh.putDocument(full), http.StatusUnprocessableEntity, "document_invalid")
}

func TestEvidenceOn_ProjectReadsCarryTheArrayAndTheListOmitsIt(t *testing.T) {
	eh := newEvidenceHarness(t)
	if items := eh.projectEvidence(); items == nil || len(items) != 0 {
		t.Fatalf("evidence = %v, want an empty array", items)
	}
	eh.seedEvidence(1)
	if items := eh.projectEvidence(); len(items) != 1 || items[0].Confirmed {
		t.Fatalf("evidence = %+v, want one unconfirmed item without a connection", items)
	}
	rec := eh.do(eh.as(http.MethodGet, "/api/v1/projects"))
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"evidence"`) {
		t.Fatalf("project list = %d %s, want no evidence", rec.Code, rec.Body.String())
	}
}
