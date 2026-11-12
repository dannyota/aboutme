package resumeapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"
)

// A bearer grant cannot hold the project read scope yet, so an agent may keep
// or remove a stored link but never add or change one
// (docs/design/projects-contract.md "Agents").
func TestProjectLinks_BearerMayKeepOrRemoveButNotAddOrChange(t *testing.T) {
	h := newResumeAPITestHarness(t)
	first := createLinkProject(t, h, h.userID, "atlas", linkProjectDocument("Atlas"))
	second := createLinkProject(t, h, h.userID, "beacon", linkProjectDocument("Beacon"))
	created := h.createResume(t)
	linked := linkedEntry(first.ID, "Atlas")
	revision, err := h.resumes.SaveDocument(h.ctx, h.userID, created.ID,
		withProjectSection(created.Doc, "projects", linked), created.Revision)
	if err != nil {
		t.Fatalf("seed link: %v", err)
	}
	principal := newAgentPrincipalForTest(t, h)
	upsert := func(entry schema.ProjectEntry) AgentResponse {
		return h.service.ExecuteAgent(h.ctx, principal, AgentCall{
			Operation: AgentUpsertEntry, IdempotencyKey: uuid.NewString(), ResumeID: created.ID.String(),
			Revision: formatRevision(revision), SectionKey: "projects",
			Payload: json.RawMessage(entryJSON(t, entry)),
		})
	}
	requireDenied := func(name string, response AgentResponse) {
		t.Helper()
		if response.Status != http.StatusForbidden || errorCodeOf(t, response.Body) != "scope_denied" {
			t.Fatalf("%s = %d %s, want 403 scope_denied", name, response.Status, response.Body)
		}
		links := storedLinks(t, h, created.ID)
		if len(links) != 1 || links[linked.ID] != first.ID.String() {
			t.Fatalf("%s changed the stored links to %v", name, links)
		}
	}

	changed := linked
	changed.ProjectID = stringPointer(second.ID.String())
	requireDenied("change", upsert(changed))
	requireDenied("add", upsert(linkedEntry(second.ID, "Beacon")))

	kept := linked
	kept.Title = stringPointer("Atlas, rewritten")
	keptResponse := upsert(kept)
	if keptResponse.Status != http.StatusOK {
		t.Fatalf("keep = %d %s, want 200", keptResponse.Status, keptResponse.Body)
	}
	revision++
	if links := storedLinks(t, h, created.ID); links[linked.ID] != first.ID.String() {
		t.Fatalf("keep lost the link: %v", links)
	}

	unrelated := h.service.ExecuteAgent(h.ctx, principal, AgentCall{
		Operation: AgentUpdateCustomization, IdempotencyKey: uuid.NewString(), ResumeID: created.ID.String(),
		Revision: formatRevision(revision),
		Payload:  json.RawMessage(`{"deltas":[{"op":"set","path":"pageFormat","value":"letter"}]}`),
	})
	if unrelated.Status != http.StatusOK {
		t.Fatalf("unrelated write on a linked resume = %d %s", unrelated.Status, unrelated.Body)
	}
	revision++

	removed := kept
	removed.ProjectID = nil
	removedResponse := upsert(removed)
	if removedResponse.Status != http.StatusOK {
		t.Fatalf("remove = %d %s, want 200", removedResponse.Status, removedResponse.Body)
	}
	if links := storedLinks(t, h, created.ID); len(links) != 0 {
		t.Fatalf("remove kept links %v", links)
	}
}
