package githubevidence

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume"
)

func (eh *evidenceHarness) storedIDs() []string {
	eh.t.Helper()
	var ids []string
	for _, row := range eh.stored() {
		ids = append(ids, row.ID.String())
	}
	return ids
}

func orderBody(ids ...string) string {
	return `{"order":["` + strings.Join(ids, `","`) + `"]}`
}

func (eh *evidenceHarness) reorder(ids ...string) *httptest.ResponseRecorder {
	eh.t.Helper()
	return eh.write(http.MethodPut, "/order", orderBody(ids...), uuid.NewString(), eh.revision())
}

func TestEvidenceOrder_NamesEveryItemOnce(t *testing.T) {
	eh := newEvidenceHarness(t)
	eh.seedEvidence(3)
	ids := eh.storedIDs()
	out := requireWrite(t, eh.reorder(ids[2], ids[0], ids[1]))
	if got := []string{out.Evidence[0].ID, out.Evidence[1].ID, out.Evidence[2].ID}; got[0] != ids[2] || got[1] != ids[0] || got[2] != ids[1] {
		t.Fatalf("order = %v", got)
	}
	if stored := eh.storedIDs(); stored[0] != ids[2] || stored[1] != ids[0] || stored[2] != ids[1] {
		t.Fatalf("stored order = %v", stored)
	}
	revision := eh.revision()
	for name, order := range map[string][]string{
		"missing":  {ids[0], ids[1]},
		"repeated": {ids[0], ids[1], ids[1]},
		"unknown":  {ids[0], ids[1], ids[2], uuid.NewString()},
		"replaced": {ids[0], ids[1], uuid.NewString()},
	} {
		requireError(t, eh.reorder(order...), http.StatusBadRequest, "request_invalid")
		if eh.revision() != revision {
			t.Fatalf("%s order changed the project", name)
		}
	}
}

func TestEvidenceRemove_DeletesOneItemWithoutASession(t *testing.T) {
	eh := newEvidenceHarness(t)
	eh.seedEvidence(2)
	ids := eh.storedIDs()
	out := requireWrite(t, eh.write(http.MethodDelete, "/"+ids[0], "", uuid.NewString(), eh.revision()))
	if len(out.Evidence) != 1 || out.Evidence[0].ID != ids[1] {
		t.Fatalf("after remove = %+v", out.Evidence)
	}
	requireError(t, eh.write(http.MethodDelete, "/"+ids[0], "", uuid.NewString(), eh.revision()), http.StatusNotFound, "evidence_not_found")
	if calls := eh.gh.readCalls(); len(calls) != 0 {
		t.Fatalf("remove read GitHub: %v", calls)
	}
}

type heldLease struct {
	lease   *publicstate.Lease
	revoked atomic.Bool
}

func (eh *evidenceHarness) holdLease(item resume.Resume) *heldLease {
	eh.t.Helper()
	lease, err := eh.coordinator.AcquireResume(eh.t.Context(), item.ID, item.Revision, publicstate.RepresentationHTML)
	if err != nil {
		eh.t.Fatalf("admit a public request at revision %d: %v", item.Revision, err)
	}
	held := &heldLease{lease: lease}
	if err = lease.OnCancel(func() {
		held.revoked.Store(true)
		lease.Release()
	}); err != nil {
		eh.t.Fatal(err)
	}
	eh.t.Cleanup(lease.Release)
	return held
}

func TestEvidenceWrites_AdvanceEachLinkingLiveResumeWithoutDraining(t *testing.T) {
	eh := newEvidenceHarness(t)
	live := eh.linkingResume(true)
	draft := eh.linkingResume(false)
	requireOutcome(t, eh.connect(eh.raw, eh.session), outcomeConnected)
	eh.servePullRequest()
	eh.seedEvidence(1)
	steps := []func(id string) *httptest.ResponseRecorder{
		func(string) *httptest.ResponseRecorder { return eh.attach(pullRequestLink) },
		func(id string) *httptest.ResponseRecorder {
			return eh.write(http.MethodPost, "/"+id+"/recheck", "", uuid.NewString(), eh.revision())
		},
		func(string) *httptest.ResponseRecorder { ids := eh.storedIDs(); return eh.reorder(ids[1], ids[0]) },
		func(id string) *httptest.ResponseRecorder {
			return eh.write(http.MethodDelete, "/"+id, "", uuid.NewString(), eh.revision())
		},
	}
	expected := live.Revision
	for i, step := range steps {
		current, err := eh.resumes.Get(t.Context(), eh.user.ID, live.ID)
		if err != nil {
			t.Fatal(err)
		}
		held := eh.holdLease(current)
		project := eh.revision()
		out := requireWrite(t, step(eh.storedIDs()[0]))
		expected++
		if len(out.ResumeRevisions) != 1 || out.ResumeRevisions[0].ID != live.ID.String() || out.ResumeRevisions[0].Revision != strconv.FormatInt(expected, 10) {
			t.Fatalf("step %d resume revisions = %+v, want %s at %d", i, out.ResumeRevisions, live.ID, expected)
		}
		if out.ProjectRevision != strconv.FormatInt(project+1, 10) || eh.revision() != project+1 {
			t.Fatalf("step %d project revision = %s, want %d", i, out.ProjectRevision, project+1)
		}
		if got := eh.resumeRevision(live.ID); got != expected {
			t.Fatalf("step %d live resume revision = %d, want %d", i, got, expected)
		}
		if held.revoked.Load() {
			t.Fatalf("step %d drained an admitted request", i)
		}
		lease, err := eh.coordinator.AcquireResume(t.Context(), live.ID, expected, publicstate.RepresentationJSON)
		if err != nil {
			t.Fatalf("step %d left the fence closed at %d: %v", i, expected, err)
		}
		lease.Release()
	}
	if got := eh.resumeRevision(draft.ID); got != draft.Revision {
		t.Fatalf("draft resume revision = %d, want %d unchanged", got, draft.Revision)
	}
}

func TestEvidenceWrites_AProjectNoLiveResumeLinksChangesOnlyItsRow(t *testing.T) {
	eh := newEvidenceHarness(t)
	draft := eh.linkingResume(false)
	eh.seedEvidence(1)
	out := requireWrite(t, eh.write(http.MethodDelete, "/"+eh.storedIDs()[0], "", uuid.NewString(), eh.revision()))
	if out.ResumeRevisions == nil || len(out.ResumeRevisions) != 0 || len(out.Evidence) != 0 {
		t.Fatalf("remove = %+v, want no resume revision", out)
	}
	if got := eh.resumeRevision(draft.ID); got != draft.Revision {
		t.Fatalf("draft resume revision = %d, want %d", got, draft.Revision)
	}
}
