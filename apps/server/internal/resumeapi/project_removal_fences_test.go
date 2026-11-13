package resumeapi

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/store"
)

type heldLease struct {
	lease   *publicstate.Lease
	revoked atomic.Bool
}

func holdLease(t *testing.T, h *resumeAPITestHarness, item resume.Resume) *heldLease {
	t.Helper()
	lease, err := h.service.coordinator.AcquireResume(h.ctx, item.ID, item.Revision, publicstate.RepresentationHTML)
	if err != nil {
		t.Fatalf("admit a public request for %s at revision %d: %v", item.ID, item.Revision, err)
	}
	held := &heldLease{lease: lease}
	if err = lease.OnCancel(func() {
		held.revoked.Store(true)
		lease.Release()
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(lease.Release)
	return held
}

func requireFenceRefuses(t *testing.T, h *resumeAPITestHarness, id uuid.UUID, revision int64) {
	t.Helper()
	if lease, err := h.service.coordinator.AcquireResume(h.ctx, id, revision, publicstate.RepresentationJSON); err == nil {
		lease.Release()
		t.Fatalf("fence for %s still admits revision %d", id, revision)
	}
}

func requireStillLinkedAt(t *testing.T, h *resumeAPITestHarness, item resume.Resume, projectID uuid.UUID) {
	t.Helper()
	stored, err := h.resumes.Get(h.ctx, h.userID, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	links := storedLinks(t, h, item.ID)
	if stored.Revision != item.Revision || len(links) != 1 {
		t.Fatalf("resume %s = revision %d links %v, want revision %d still linked", item.ID, stored.Revision, links, item.Revision)
	}
	for _, linked := range links {
		if linked != projectID.String() {
			t.Fatalf("resume %s links %s, want %s", item.ID, linked, projectID)
		}
	}
}

type failingSaveResumes struct {
	*resume.Store
	saves atomic.Int32
}

var errInjectedSave = errors.New("injected save failure")

func (f *failingSaveResumes) SaveDocumentTx(ctx context.Context, qtx *store.Queries, userID, id uuid.UUID, doc schema.Resume, expected int64) (resume.Resume, error) {
	if f.saves.Add(1) == 2 {
		return resume.Resume{}, errInjectedSave
	}
	return f.Store.SaveDocumentTx(ctx, qtx, userID, id, doc, expected)
}

func TestRemoveProject_RevokesLiveLeasesKeepsDraftLeasesAndAdvancesFences(t *testing.T) {
	h := newResumeAPITestHarness(t)
	owned := createLinkProject(t, h, h.userID, "atlas", linkProjectDocument("Atlas"))
	seeded := []resume.Resume{
		seedLinkingResume(t, h, owned.ID, true), seedLinkingResume(t, h, owned.ID, true), seedLinkingResume(t, h, owned.ID, false),
	}
	leases := make([]*heldLease, len(seeded))
	for i, item := range seeded {
		leases[i] = holdLease(t, h, item)
	}
	result, err := h.service.RemoveProject(h.ctx, newTestRemoval(h, owned.ID, nil))
	if err != nil || result.Outcome != resume.CommitCommitted {
		t.Fatalf("remove = %+v, %v", result, err)
	}
	var changed []ChangedResume
	if err = json.Unmarshal(result.Response.Body, &changed); err != nil || len(changed) != 3 {
		t.Fatalf("changed = %s (%v), want three resumes", result.Response.Body, err)
	}
	for i, item := range seeded {
		if item.Live && !leases[i].revoked.Load() {
			t.Fatalf("live resume %s kept its admitted public request", item.ID)
		}
		if !item.Live && (leases[i].revoked.Load() || leases[i].lease.Context().Err() != nil) {
			t.Fatalf("draft resume %s had its admitted request drained", item.ID)
		}
		requireUnlinkedAt(t, h, item.ID, item.Revision+1)
		requireFenceOpen(t, h, item.ID, item.Revision+1)
		requireFenceRefuses(t, h, item.ID, item.Revision)
	}
	if _, err = project.NewStore().GetTx(h.ctx, h.queries, h.userID, owned.ID); !errors.Is(err, project.ErrNotFound) {
		t.Fatalf("project after removal: %v, want not found", err)
	}
}

func TestRemoveProject_FailureAfterAWrittenDocumentRollsEverythingBack(t *testing.T) {
	h := newResumeAPITestHarness(t)
	owned := createLinkProject(t, h, h.userID, "atlas", linkProjectDocument("Atlas"))
	seeded := []resume.Resume{seedLinkingResume(t, h, owned.ID, true), seedLinkingResume(t, h, owned.ID, false)}
	for _, item := range seeded {
		holdLease(t, h, item)
	}
	failing := &failingSaveResumes{Store: h.resumes}
	h.service.resumes = failing
	if _, err := h.service.RemoveProject(h.ctx, newTestRemoval(h, owned.ID, nil)); !errors.Is(err, errInjectedSave) {
		t.Fatalf("remove error = %v, want the injected failure", err)
	}
	if failing.saves.Load() != 2 {
		t.Fatalf("document saves = %d, want the failure after one written document", failing.saves.Load())
	}
	for _, item := range seeded {
		requireStillLinkedAt(t, h, item, owned.ID)
		requireFenceOpen(t, h, item.ID, item.Revision)
		requireFenceRefuses(t, h, item.ID, item.Revision+1)
	}
	if _, err := project.NewStore().GetTx(h.ctx, h.queries, h.userID, owned.ID); err != nil {
		t.Fatalf("rolled back removal deleted the project: %v", err)
	}
}

func TestRemoveProject_LostCommitAcknowledgementRecoversBothResumes(t *testing.T) {
	h := newResumeAPITestHarness(t)
	owned := createLinkProject(t, h, h.userID, "atlas", linkProjectDocument("Atlas"))
	seeded := []resume.Resume{seedLinkingResume(t, h, owned.ID, true), seedLinkingResume(t, h, owned.ID, false)}
	for _, item := range seeded {
		holdLease(t, h, item)
	}
	h.service.idempotency = &removalIdempotency{idempotencyBoundary: h.service.idempotency, lostAck: true}
	result, err := h.service.RemoveProject(h.ctx, newTestRemoval(h, owned.ID, nil))
	if err != nil || !result.Replayed || result.Outcome != resume.CommitCommitted {
		t.Fatalf("recovered remove = %+v, %v; want the recovered stored response", result, err)
	}
	var changed []ChangedResume
	if err = json.Unmarshal(result.Response.Body, &changed); err != nil || len(changed) != 2 {
		t.Fatalf("recovered response = %s (%v), want two resumes", result.Response.Body, err)
	}
	revisions := map[uuid.UUID]int64{}
	for _, item := range changed {
		revisions[item.ID] = item.Revision
	}
	for _, item := range seeded {
		if revisions[item.ID] != item.Revision+1 {
			t.Fatalf("recovered revisions = %v, want %s at %d", revisions, item.ID, item.Revision+1)
		}
		requireUnlinkedAt(t, h, item.ID, item.Revision+1)
		requireFenceOpen(t, h, item.ID, item.Revision+1)
		requireFenceRefuses(t, h, item.ID, item.Revision)
	}
}

func TestRemoveProject_UndrainedLiveLeaseTimesOutAndChangesNothing(t *testing.T) {
	h := newResumeAPITestHarness(t)
	owned := createLinkProject(t, h, h.userID, "atlas", linkProjectDocument("Atlas"))
	live := seedLinkingResume(t, h, owned.ID, true)
	draft := seedLinkingResume(t, h, owned.ID, false)
	stuck, err := h.service.coordinator.AcquireResume(h.ctx, live.ID, live.Revision, publicstate.RepresentationHTML)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stuck.Release)
	draftLease := holdLease(t, h, draft)
	h.service.clock = func() time.Time { return time.Now().Add(-publicStateDrainTimeout) }
	_, err = h.service.RemoveProject(h.ctx, newTestRemoval(h, owned.ID, nil))
	var drain *publicstate.DrainTimeoutError
	if !errors.As(err, &drain) {
		t.Fatalf("remove error = %v, want a drain timeout", err)
	}
	if stuck.Context().Err() == nil {
		t.Fatal("the live resume's admitted request was not revoked")
	}
	if draftLease.revoked.Load() {
		t.Fatal("the draft resume's admitted request was drained")
	}
	for _, item := range []resume.Resume{live, draft} {
		requireStillLinkedAt(t, h, item, owned.ID)
	}
	if _, err = project.NewStore().GetTx(h.ctx, h.queries, h.userID, owned.ID); err != nil {
		t.Fatalf("timed out removal deleted the project: %v", err)
	}
	stuck.Release()
	requireFenceOpen(t, h, live.ID, live.Revision)
	requireFenceOpen(t, h, draft.ID, draft.Revision)
}
