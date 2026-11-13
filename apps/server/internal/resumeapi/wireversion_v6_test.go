package resumeapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestWireVersion_V5ClientKeepsStoredProjectLinks(t *testing.T) {
	h := newResumeAPITestHarness(t)
	owned := createLinkProject(t, h, h.userID, "atlas", linkProjectDocument("Atlas"))
	created := h.createResume(t)
	linked := linkedEntry(owned.ID, "Atlas")
	revision, err := h.resumes.SaveDocument(h.ctx, h.userID, created.ID,
		withProjectSection(created.Doc, "projects", linked), created.Revision)
	if err != nil {
		t.Fatalf("seed link: %v", err)
	}
	path := apiResumePath + "/" + created.ID.String()

	read := resumeRequest(t, h, http.MethodGet, path, "", 0, uuid.Nil, "5")
	if read.status != http.StatusOK || read.header.Get(wireVersionHeader) != "5" {
		t.Fatalf("v5 read = %d %s", read.status, read.body)
	}
	var emitted map[string]any
	if err = json.Unmarshal(decodeResumeResource(t, read).Document, &emitted); err != nil {
		t.Fatalf("decode v5 document: %v", err)
	}
	if containsKey(emitted, "projectId") {
		t.Fatalf("v5 document carries projectId: %v", emitted)
	}

	edited := `{"entry":{"id":"` + linked.ID + `","title":"Atlas, edited"}}`
	upsert := resumeRequest(t, h, http.MethodPatch, path+"/entries/projects", edited, revision, uuid.New(), "5")
	if upsert.status != http.StatusOK {
		t.Fatalf("v5 upsert = %d %s", upsert.status, upsert.body)
	}
	revision++
	if links := storedLinks(t, h, created.ID); links[linked.ID] != owned.ID.String() {
		t.Fatalf("v5 upsert dropped the stored link: %v", links)
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if title := stored.Doc.Content["projects"].ProjectEntries[0].Title; title == nil || *title != "Atlas, edited" {
		t.Fatalf("v5 upsert lost the client's title: %v", title)
	}

	added := `{"entry":{"id":"` + uuid.NewString() + `","title":"New"}}`
	addResponse := resumeRequest(t, h, http.MethodPatch, path+"/entries/projects", added, revision, uuid.New(), "4")
	if addResponse.status != http.StatusOK {
		t.Fatalf("v4 add = %d %s", addResponse.status, addResponse.body)
	}
	revision++
	if links := storedLinks(t, h, created.ID); len(links) != 1 || links[linked.ID] != owned.ID.String() {
		t.Fatalf("v4 add changed links to %v", links)
	}

	smuggled := `{"entry":{"id":"` + linked.ID + `","projectId":"` + uuid.NewString() + `"}}`
	refused := resumeRequest(t, h, http.MethodPatch, path+"/entries/projects", smuggled, revision, uuid.New(), "5")
	if refused.status == http.StatusOK || refused.status >= http.StatusInternalServerError {
		t.Fatalf("v5 projectId write = %d %s, want a client error", refused.status, refused.body)
	}
	if links := storedLinks(t, h, created.ID); links[linked.ID] != owned.ID.String() {
		t.Fatalf("refused v5 write changed links to %v", links)
	}

	removed := resumeRequest(t, h, http.MethodDelete, path+"/entries/projects/"+linked.ID, "", revision, uuid.New(), "5")
	if removed.status != http.StatusNoContent {
		t.Fatalf("v5 entry delete = %d %s", removed.status, removed.body)
	}
	if links := storedLinks(t, h, created.ID); len(links) != 0 {
		t.Fatalf("deleting the entry kept links %v", links)
	}
}
