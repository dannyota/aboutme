package showcase

import "testing"

func TestServiceRunsWithoutShowcaseReviewColumns(t *testing.T) {
	e := newFreshEnv(t)
	reviewColumns := []string{"review_key", "reviewed_key", "review_outcome", "reviewed_at", "first_listed_at"}
	var present int
	if err := e.pool.QueryRow(e.ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'resume_showcase' AND column_name = ANY($1::text[])`,
		reviewColumns).Scan(&present); err != nil {
		t.Fatalf("count review columns: %v", err)
	}
	if present != 0 {
		t.Fatalf("review columns at head = %d, want 0", present)
	}
	f := e.addResume(t, defaultSpec())

	e.optIn(t, f.id, strp("backend"))
	e.optIn(t, f.id, strp("qa"))
	e.sync(t, f.id)

	row, found := e.showcaseRow(t, f.id)
	if !found || row.Role == nil || *row.Role != "qa" || !row.RequestedAt.Equal(e.now) {
		t.Fatalf("showcase row = %+v (found %t), want role qa opted in at %v", row, found, e.now)
	}
	owned, err := e.queries.ListResumeShowcasesForUser(e.ctx, f.user)
	if err != nil || len(owned) != 1 || owned[0].ResumeID != f.id {
		t.Fatalf("owner's showcase rows = %+v (%v), want only %s", owned, err, f.id)
	}
	page := e.list(t, Filter{})
	if got := slugsOf(page); len(got) != 1 || got[0] != f.slug || page.Total != 1 {
		t.Fatalf("listing = %v total %d, want only %s", got, page.Total, f.slug)
	}
	recomputed, skipped, err := e.service.RecomputeAll(e.ctx)
	if err != nil || recomputed != 1 || skipped != 0 {
		t.Fatalf("RecomputeAll() = %d recomputed, %d skipped (%v), want 1 and 0", recomputed, skipped, err)
	}
	exported, err := e.queries.ListAccountExportResumes(e.ctx, f.user)
	if err != nil || len(exported) != 1 || exported[0].ShowcaseRole == nil || *exported[0].ShowcaseRole != "qa" {
		t.Fatalf("account export = %+v (%v), want one resume with role qa", exported, err)
	}

	e.publish(t, PublishChange{ResumeID: f.id, Enabled: false})
	if _, found = e.showcaseRow(t, f.id); found {
		t.Fatal("turning the switch off left the row")
	}
}
