package resumeapi

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestProjectLinks_SessionWriteAddsAndChangesLinksWhateverTheAgentGrant(t *testing.T) {
	h := newResumeAPITestHarness(t)
	newAgentPrincipalForTest(t, h)
	atlas := createLinkProject(t, h, h.userID, "atlas", linkProjectDocument("Atlas"))
	beacon := createLinkProject(t, h, h.userID, "beacon", linkProjectDocument("Beacon"))
	created := h.createResume(t)
	revision, err := h.resumes.SaveDocument(h.ctx, h.userID, created.ID, withProjectSection(created.Doc, "projects"), created.Revision)
	if err != nil {
		t.Fatalf("seed project section: %v", err)
	}
	entry := linkedEntry(atlas.ID, "Atlas")
	path := apiResumePath + "/" + created.ID.String() + "/entries/projects"
	added := resumeRequest(t, h, http.MethodPatch, path, entryJSON(t, entry), revision, uuid.New(), "")
	if added.status != http.StatusOK {
		t.Fatalf("session add = %d %s, want 200", added.status, added.body)
	}
	revision++
	if links := storedLinks(t, h, created.ID); len(links) != 1 || links[entry.ID] != atlas.ID.String() {
		t.Fatalf("links after session add = %v", links)
	}
	entry.ProjectID = stringPointer(beacon.ID.String())
	changed := resumeRequest(t, h, http.MethodPatch, path, entryJSON(t, entry), revision, uuid.New(), "")
	if changed.status != http.StatusOK {
		t.Fatalf("session change = %d %s, want 200", changed.status, changed.body)
	}
	revision++
	if links := storedLinks(t, h, created.ID); len(links) != 1 || links[entry.ID] != beacon.ID.String() {
		t.Fatalf("links after session change = %v", links)
	}
	linked := linkProject(t, h, created.ID, atlas.ID, revision)
	if linked.status != http.StatusOK {
		t.Fatalf("session project link = %d %s, want 200", linked.status, linked.body)
	}
	if links := storedLinks(t, h, created.ID); len(links) != 2 {
		t.Fatalf("links after session project link = %v, want two", links)
	}
}
