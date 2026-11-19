package githubevidence

import (
	"errors"
	"testing"

	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

func (eh *evidenceHarness) publicRead(slug string) ([]publicresume.PublicEvidence, int64) {
	eh.t.Helper()
	origin, err := publicresume.ParsePublicOrigin(testOrigin, "production")
	if err != nil {
		eh.t.Fatal(err)
	}
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{
		Store: store.New(eh.pool), Projector: docmigrate.NewIdentityProjector(), Coordinator: eh.coordinator, Origin: origin, Evidence: true,
	})
	if err != nil {
		eh.t.Fatal(err)
	}
	snapshot, read, lease, err := reader.ReadProjects(eh.t.Context(), slug, eh.project.Slug)
	if err != nil {
		eh.t.Fatalf("public read of %s/projects/%s: %v", slug, eh.project.Slug, err)
	}
	defer lease.Release()
	pages, err := read.Pages()
	if err != nil || len(pages.Projects) != 1 {
		eh.t.Fatalf("public project page = %+v, %v; want the linked project", pages, err)
	}
	return pages.Projects[0].Evidence, snapshot.Revision
}

func TestEvidencePublicRead_AnAttachIsConfirmedAtTheAdvancedRevision(t *testing.T) {
	eh := newEvidenceHarness(t)
	live := eh.linkingResume(true)
	if items, revision := eh.publicRead(*live.Slug); len(items) != 0 || revision != live.Revision {
		t.Fatalf("before the attach = %+v at %d, want no evidence at %d", items, revision, live.Revision)
	}
	eh.attachedPullRequest()
	items, revision := eh.publicRead(*live.Slug)
	if revision != live.Revision+1 || len(items) != 1 || !items[0].Confirmed ||
		items[0].Kind != "pullRequest" || items[0].URL != "https://github.com/octocat/hello/pull/42" {
		t.Fatalf("after the attach = %+v at %d, want the confirmed pull request at %d", items, revision, live.Revision+1)
	}
	_, err := eh.coordinator.AcquireResume(t.Context(), live.ID, live.Revision, publicstate.RepresentationHTML)
	var mismatch *publicstate.GenerationMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("a lease at the old revision = %v, want a generation mismatch", err)
	}
}

func TestEvidencePublicRead_DisconnectKeepsAPlainLinkOrRemovesIt(t *testing.T) {
	for _, keepLinks := range []bool{true, false} {
		eh := newEvidenceHarness(t)
		live := eh.linkingResume(true)
		eh.attachedPullRequest()
		_, attached := eh.publicRead(*live.Slug)
		requireDisconnected(t, eh.disconnect(keepLinks))
		items, revision := eh.publicRead(*live.Slug)
		if revision <= attached {
			t.Fatalf("keepLinks=%t: revision after the disconnect = %d, want past %d", keepLinks, revision, attached)
		}
		if keepLinks && (len(items) != 1 || items[0].Confirmed) {
			t.Fatalf("keepLinks=true: evidence = %+v, want one plain link", items)
		}
		if !keepLinks && len(items) != 0 {
			t.Fatalf("keepLinks=false: evidence = %+v, want none", items)
		}
	}
}
