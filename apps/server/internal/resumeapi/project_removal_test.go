package resumeapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/store"
)

// removalIdempotency runs before before every Execute, and with lostAck
// reports a committed Execute as an unknown commit.
type removalIdempotency struct {
	idempotencyBoundary
	before   func(attempt int)
	lostAck  bool
	executes int
}

func (r *removalIdempotency) Execute(ctx context.Context, userID uuid.UUID, operation string, key uuid.UUID, hash [32]byte,
	callback func(*store.Queries) (resume.StoredResponse, error),
) (resume.ExecuteResult, error) {
	r.executes++
	if r.before != nil {
		r.before(r.executes)
	}
	result, err := r.idempotencyBoundary.Execute(ctx, userID, operation, key, hash, callback)
	if r.lostAck && err == nil {
		return resume.ExecuteResult{Outcome: resume.CommitUnknown}, errors.New("commit acknowledgement lost")
	}
	return result, err
}

func seedLinkingResume(t *testing.T, h *resumeAPITestHarness, projectID uuid.UUID, live bool) resume.Resume {
	t.Helper()
	created := h.createResume(t)
	if projectID != uuid.Nil {
		if _, err := h.resumes.SaveDocument(h.ctx, h.userID, created.ID,
			withProjectSection(created.Doc, "projects", linkedEntry(projectID, "Atlas")), created.Revision); err != nil {
			t.Fatalf("seed link: %v", err)
		}
	}
	if live {
		if _, err := h.pool.Exec(h.ctx, `UPDATE resumes SET slug = $2, live = true, revision = revision + 1 WHERE id = $1`,
			created.ID, "removal-"+uuid.NewString()[:8]); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := h.resumes.Get(h.ctx, h.userID, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

func newTestRemoval(h *resumeAPITestHarness, projectID uuid.UUID, authorize func(context.Context, *store.Queries) error) ProjectRemoval {
	hash := sha256.Sum256([]byte("removal " + projectID.String()))
	operation := sha256.Sum256([]byte("operation " + projectID.String()))
	if authorize == nil {
		authorize = func(ctx context.Context, q *store.Queries) error {
			_, err := project.NewStore().GetTx(ctx, q, h.userID, projectID)
			return err
		}
	}
	return ProjectRemoval{
		UserID: h.userID, ProjectID: projectID, Operation: hex.EncodeToString(operation[:]), Key: uuid.New(), RequestHash: hash,
		Authorize: authorize,
		Remove: func(ctx context.Context, q *store.Queries, changed []ChangedResume) (resume.StoredResponse, error) {
			current, err := project.NewStore().GetTx(ctx, q, h.userID, projectID)
			if err != nil {
				return resume.StoredResponse{}, err
			}
			if err = project.NewStore().DeleteTx(ctx, q, h.userID, projectID, current.Revision, time.Now()); err != nil {
				return resume.StoredResponse{}, err
			}
			body, err := json.Marshal(changed)
			return resume.StoredResponse{Status: http.StatusOK, Body: body, Headers: map[string]string{}}, err
		},
	}
}

func requireFenceOpen(t *testing.T, h *resumeAPITestHarness, id uuid.UUID, revision int64) {
	t.Helper()
	lease, err := h.service.coordinator.AcquireResume(h.ctx, id, revision, publicstate.RepresentationJSON)
	if err != nil {
		t.Fatalf("fence for %s at revision %d is not open: %v", id, revision, err)
	}
	lease.Release()
}

func requireUnlinkedAt(t *testing.T, h *resumeAPITestHarness, id uuid.UUID, revision int64) {
	t.Helper()
	stored, err := h.resumes.Get(h.ctx, h.userID, id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Revision != revision || len(storedLinks(t, h, id)) != 0 || len(stored.Doc.Content["projects"].ProjectEntries) != 1 {
		t.Fatalf("resume %s = revision %d links %v, want revision %d with the entry kept and unlinked",
			id, stored.Revision, storedLinks(t, h, id), revision)
	}
}

func TestRemoveProject_ReplansAfterALinkCommittedBetweenPlanAndLock(t *testing.T) {
	h := newResumeAPITestHarness(t)
	owned := createLinkProject(t, h, h.userID, "atlas", linkProjectDocument("Atlas"))
	first := seedLinkingResume(t, h, owned.ID, true)
	late := seedLinkingResume(t, h, uuid.Nil, false)
	counter := &removalIdempotency{idempotencyBoundary: h.service.idempotency}
	counter.before = func(attempt int) {
		if attempt != 1 {
			return
		}
		if _, err := h.resumes.SaveDocument(h.ctx, h.userID, late.ID,
			withProjectSection(late.Doc, "projects", linkedEntry(owned.ID, "Atlas")), late.Revision); err != nil {
			t.Errorf("concurrent link: %v", err)
		}
	}
	h.service.idempotency = counter
	result, err := h.service.RemoveProject(h.ctx, newTestRemoval(h, owned.ID, nil))
	if err != nil || counter.executes != 2 {
		t.Fatalf("remove = %+v, %v after %d executes, want success on the second plan", result, err, counter.executes)
	}
	requireUnlinkedAt(t, h, first.ID, first.Revision+1)
	requireUnlinkedAt(t, h, late.ID, late.Revision+2)
	requireFenceOpen(t, h, late.ID, late.Revision+2)
}

func TestRemoveProject_ThreeChangedPlansAreBusyAndChangeNothing(t *testing.T) {
	h := newResumeAPITestHarness(t)
	owned := createLinkProject(t, h, h.userID, "atlas", linkProjectDocument("Atlas"))
	first := seedLinkingResume(t, h, owned.ID, false)
	second := seedLinkingResume(t, h, uuid.Nil, false)
	third := seedLinkingResume(t, h, uuid.Nil, false)
	link := func(target resume.Resume) {
		if _, err := h.resumes.SaveDocument(h.ctx, h.userID, target.ID,
			withProjectSection(target.Doc, "projects", linkedEntry(owned.ID, "Atlas")), target.Revision); err != nil {
			t.Errorf("concurrent link: %v", err)
		}
	}
	counter := &removalIdempotency{idempotencyBoundary: h.service.idempotency}
	counter.before = func(attempt int) {
		switch attempt {
		case 1:
			link(second)
		case 2:
			link(third)
		default:
			if _, err := h.pool.Exec(h.ctx, `UPDATE resumes SET revision = revision + 1 WHERE id = $1`, first.ID); err != nil {
				t.Errorf("concurrent revision: %v", err)
			}
		}
	}
	h.service.idempotency = counter
	if _, err := h.service.RemoveProject(h.ctx, newTestRemoval(h, owned.ID, nil)); !errors.Is(err, ErrPublicStateBusy) || counter.executes != 3 {
		t.Fatalf("remove error = %v after %d executes, want ErrPublicStateBusy after three", err, counter.executes)
	}
	for _, item := range []resume.Resume{first, second, third} {
		if links := storedLinks(t, h, item.ID); len(links) != 1 {
			t.Fatalf("busy removal changed links of %s to %v", item.ID, links)
		}
	}
	if _, err := project.NewStore().GetTx(h.ctx, h.queries, h.userID, owned.ID); err != nil {
		t.Fatalf("busy removal deleted the project: %v", err)
	}
}
