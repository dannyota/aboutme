package showcase

import "testing"

// dropShowcaseReviewColumns removes the unused review columns, their checks,
// and the approval index, as the drop migration will.
const dropShowcaseReviewColumns = `
DROP INDEX resume_showcase_listing_idx;
ALTER TABLE resume_showcase
    DROP CONSTRAINT resume_showcase_review_key_check,
    DROP CONSTRAINT resume_showcase_reviewed_key_check,
    DROP CONSTRAINT resume_showcase_review_outcome_check,
    DROP CONSTRAINT resume_showcase_review_columns_check,
    DROP CONSTRAINT resume_showcase_first_listed_check,
    DROP COLUMN review_key,
    DROP COLUMN reviewed_key,
    DROP COLUMN review_outcome,
    DROP COLUMN reviewed_at,
    DROP COLUMN first_listed_at;
`

// After the review columns are dropped, every showcase query this release
// runs still works: the opt-in insert, the locked role and derived-value
// updates, the owner's read and list, the public listing, the recompute, the
// account export, and the opt-out delete (docs/design/release-floors.md
// "Schema floor").
func TestServiceRunsWithoutShowcaseReviewColumns(t *testing.T) {
	e := newFreshEnv(t)
	if _, err := e.pool.Exec(e.ctx, dropShowcaseReviewColumns); err != nil {
		t.Fatalf("drop review columns: %v", err)
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
