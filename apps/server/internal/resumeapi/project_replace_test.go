package resumeapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/previewcard"
	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/showcase"
	"danny.vn/aboutme/apps/server/internal/store"
)

func newTestReplacement(h *resumeAPITestHarness, projectID uuid.UUID, slug string, d project.Document) ProjectReplacement {
	hash := sha256.Sum256([]byte("replacement " + projectID.String() + " " + slug))
	operation := sha256.Sum256([]byte("replace operation " + projectID.String()))
	projects := project.NewStore()
	return ProjectReplacement{
		UserID: h.userID, ProjectID: projectID, Operation: hex.EncodeToString(operation[:]), Key: uuid.New(), RequestHash: hash,
		Change: func(ctx context.Context, q *store.Queries) (project.Change, error) {
			current, err := projects.GetTx(ctx, q, h.userID, projectID)
			if err != nil {
				return project.Unchanged, err
			}
			return projects.ChangeTx(ctx, q, h.userID, projectID, current.Revision, slug, d)
		},
		Authorize: func(context.Context, *store.Queries) error { return nil },
		Replace: func(ctx context.Context, q *store.Queries, changed []ChangedResume) (resume.StoredResponse, error) {
			current, err := projects.GetTx(ctx, q, h.userID, projectID)
			if err != nil {
				return resume.StoredResponse{}, err
			}
			if _, err = projects.ReplaceTx(ctx, q, h.userID, projectID, current.Revision, slug, d, time.Now(), nil); err != nil {
				return resume.StoredResponse{}, err
			}
			body, err := json.Marshal(changed)
			return resume.StoredResponse{Status: http.StatusOK, Body: body, Headers: map[string]string{}}, err
		},
	}
}

func changedRevisions(t *testing.T, result resume.ExecuteResult) map[uuid.UUID]int64 {
	t.Helper()
	var changed []ChangedResume
	if err := json.Unmarshal(result.Response.Body, &changed); err != nil {
		t.Fatalf("changed = %s: %v", result.Response.Body, err)
	}
	out := map[uuid.UUID]int64{}
	for _, item := range changed {
		out[item.ID] = item.Revision
	}
	return out
}

func requireStoredRevision(t *testing.T, h *resumeAPITestHarness, id uuid.UUID, want int64) {
	t.Helper()
	stored, err := h.resumes.Get(h.ctx, h.userID, id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Revision != want {
		t.Fatalf("resume %s revision = %d, want %d", id, stored.Revision, want)
	}
}

func TestReplaceProject_ReplansAfterALinkCommittedBetweenPlanAndLock(t *testing.T) {
	h := newResumeAPITestHarness(t)
	owned := createLinkProject(t, h, h.userID, "atlas", linkProjectDocument("Atlas"))
	first := seedLinkingResume(t, h, owned.ID, true)
	late := seedLinkingResume(t, h, uuid.Nil, true)
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
	result, err := h.service.ReplaceProject(h.ctx, newTestReplacement(h, owned.ID, "atlas", linkProjectDocument("Atlas, renamed")))
	if err != nil || counter.executes != 2 {
		t.Fatalf("replace = %+v, %v after %d executes, want success on the second plan", result, err, counter.executes)
	}
	want := map[uuid.UUID]int64{first.ID: first.Revision + 1, late.ID: late.Revision + 2}
	if got := changedRevisions(t, result); !reflect.DeepEqual(got, want) {
		t.Fatalf("changed = %v, want %v", got, want)
	}
	for id, revision := range want {
		requireStoredRevision(t, h, id, revision)
		requireFenceOpen(t, h, id, revision)
	}
}

func TestReplaceProject_ThreeChangedPlansAreBusyAndChangeNothing(t *testing.T) {
	h := newResumeAPITestHarness(t)
	owned := createLinkProject(t, h, h.userID, "atlas", linkProjectDocument("Atlas"))
	first := seedLinkingResume(t, h, owned.ID, true)
	second := seedLinkingResume(t, h, uuid.Nil, true)
	third := seedLinkingResume(t, h, uuid.Nil, true)
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
	_, err := h.service.ReplaceProject(h.ctx, newTestReplacement(h, owned.ID, "atlas", linkProjectDocument("Atlas, renamed")))
	if !errors.Is(err, ErrPublicStateBusy) || counter.executes != 3 {
		t.Fatalf("replace error = %v after %d executes, want ErrPublicStateBusy after three", err, counter.executes)
	}
	for id, revision := range map[uuid.UUID]int64{first.ID: first.Revision + 1, second.ID: second.Revision + 1, third.ID: third.Revision + 1} {
		requireStoredRevision(t, h, id, revision)
	}
	stored, err := project.NewStore().GetTx(h.ctx, h.queries, h.userID, owned.ID)
	if err != nil || stored.Revision != owned.Revision || stored.Text["en"].Name != "Atlas" {
		t.Fatalf("busy replacement wrote the project: %+v, %v", stored, err)
	}
}

func findListing(t *testing.T, h *resumeAPITestHarness, slug string) showcase.Item {
	t.Helper()
	for page := 1; page <= showcase.MaxPage; page++ {
		listed, err := h.showcase.List(h.ctx, showcase.Filter{Page: page})
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range listed.Items {
			if item.Slug == slug {
				return item
			}
		}
		if page >= listed.PageCount {
			break
		}
	}
	t.Fatalf("showcase does not list %s", slug)
	return showcase.Item{}
}

func readLive(t *testing.T, h *resumeAPITestHarness, slug string) publicresume.Snapshot {
	t.Helper()
	origin, err := publicresume.ParsePublicOrigin(resumeAPITestOrigin, "production")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{
		Store: h.queries, Projector: docmigrate.NewIdentityProjector(), Coordinator: h.service.coordinator, Origin: origin,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, lease, err := reader.ReadResume(h.ctx, slug, publicstate.RepresentationPNG)
	if err != nil {
		t.Fatalf("read %s: %v", slug, err)
	}
	lease.Release()
	return snapshot
}

func TestReplaceProject_AdvanceKeepsTheShowcaseRowListingAndStoredCard(t *testing.T) {
	env := newShowcasePublishEnv(t)
	h := env.h
	owned := createLinkProject(t, h, h.userID, "atlas", linkProjectDocument("Atlas"))
	current, err := h.resumes.Get(h.ctx, h.userID, env.id)
	if err != nil {
		t.Fatal(err)
	}
	if env.revision, err = h.resumes.SaveDocument(h.ctx, h.userID, env.id,
		withProjectSection(current.Doc, "projects", linkedEntry(owned.ID, "Atlas")), current.Revision); err != nil {
		t.Fatalf("link the resume: %v", err)
	}
	env.publishOK(t, publishBody(env.slug, `"showcaseEnabled":true,"showcaseRole":"backend"`))
	linkedRevision := env.revision
	version, err := previewcard.VersionOf(readLive(t, h, env.slug))
	if err != nil {
		t.Fatal(err)
	}
	png := []byte("\x89PNG stored card")
	if err = h.queries.UpsertResumePreviewCard(h.ctx, store.UpsertResumePreviewCardParams{ResumeID: env.id, Version: version, PNG: png, RenderedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	rowBefore, err := h.queries.GetResumeShowcase(h.ctx, env.id)
	if err != nil {
		t.Fatal(err)
	}
	listedBefore := findListing(t, h, env.slug)

	result, err := h.service.ReplaceProject(h.ctx, newTestReplacement(h, owned.ID, "atlas", linkProjectDocument("Atlas, renamed")))
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if got, want := changedRevisions(t, result), map[uuid.UUID]int64{env.id: linkedRevision + 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("changed = %v, want %v", got, want)
	}
	rowAfter, err := h.queries.GetResumeShowcase(h.ctx, env.id)
	if err != nil || !reflect.DeepEqual(rowAfter, rowBefore) {
		t.Fatalf("showcase row = %+v (%v), want %+v unchanged", rowAfter, err, rowBefore)
	}
	listedAfter := findListing(t, h, env.slug)
	if !reflect.DeepEqual(listedAfter, listedBefore) {
		t.Fatalf("listing = %+v, want %+v unchanged", listedAfter, listedBefore)
	}
	listing, err := json.Marshal(listedAfter)
	if err != nil || bytes.Contains(listing, []byte("atlas")) || bytes.Contains(listing, []byte("/projects/")) {
		t.Fatalf("listing %s (%v) carries a project slug or page address", listing, err)
	}
	advanced := readLive(t, h, env.slug)
	advancedVersion, err := previewcard.VersionOf(advanced)
	if err != nil || advanced.Revision != linkedRevision+1 || advancedVersion != version {
		t.Fatalf("advanced card version = %s at revision %d (%v), want %s at %d", advancedVersion, advanced.Revision, err, version, linkedRevision+1)
	}
	card, err := h.queries.GetResumePreviewCard(h.ctx, env.id)
	if err != nil || card.Version != version || !bytes.Equal(card.PNG, png) {
		t.Fatalf("stored card = %s (%v), want the stored version %s kept", card.Version, err, version)
	}
}
