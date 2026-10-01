package showcase

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func pendingSlugs(t *testing.T, e *env) map[string]PendingItem {
	t.Helper()
	items, err := e.reviewer.Pending(e.ctx)
	if err != nil {
		t.Fatalf("Pending() error: %v", err)
	}
	bySlug := make(map[string]PendingItem, len(items))
	for _, item := range items {
		bySlug[item.Slug] = item
	}
	return bySlug
}

// AC-SHOW-006: pending lists an opt-in until its current key has a result.
func TestPendingListsOptInsWithoutAResultForTheCurrentKey(t *testing.T) {
	e := newEnv(t)
	waiting := e.addResume(t, defaultSpec())
	e.optIn(t, waiting.id, nil)
	approved := e.listed(t, defaultSpec(), nil)
	declinedFixture := e.addResume(t, defaultSpec())
	e.optIn(t, declinedFixture.id, nil)
	row, _ := e.showcaseRow(t, declinedFixture.id)
	if done, err := e.reviewer.Decline(e.ctx, declinedFixture.slug, row.ReviewKey); err != nil || !done {
		t.Fatalf("Decline() = %t, %v", done, err)
	}
	changed := defaultSpec()
	reviewedThenChanged := e.listed(t, changed, nil)
	changed.name = "Grace Hopper"
	personal, _, _ := changed.parts(t)
	if _, err := e.pool.Exec(e.ctx, `UPDATE resumes SET personal_details = $2 WHERE id = $1`, reviewedThenChanged.id, personal); err != nil {
		t.Fatalf("change name: %v", err)
	}
	e.sync(t, reviewedThenChanged.id)

	got := pendingSlugs(t, e)
	for slug, want := range map[string]bool{
		waiting.slug: true, approved.slug: false, declinedFixture.slug: false, reviewedThenChanged.slug: true,
	} {
		if _, present := got[slug]; present != want {
			t.Errorf("pending lists %s = %t, want %t", slug, present, want)
		}
	}
	item := got[waiting.slug]
	waitingRow, _ := e.showcaseRow(t, waiting.id)
	if item.ReviewKey != waitingRow.ReviewKey || item.CardVersion != waitingRow.CardVersion || !item.RequestedAt.Equal(e.now) {
		t.Fatalf("pending item = %+v, want the row's key %s and version %s", item, waitingRow.ReviewKey, waitingRow.CardVersion)
	}
}

// AC-SHOW-006: approve sets the first-listed time once, and a stale key does
// nothing.
func TestApproveAndDeclineNeedTheCurrentKey(t *testing.T) {
	e := newEnv(t)
	f := e.addResume(t, defaultSpec())
	e.optIn(t, f.id, nil)
	row, _ := e.showcaseRow(t, f.id)

	for _, stale := range []string{"aaaaaaaaaaaaaaaa", row.CardVersion} {
		if applied, err := e.reviewer.Approve(e.ctx, f.slug, stale); err != nil || applied {
			t.Fatalf("Approve(stale %s) = %t, %v, want false", stale, applied, err)
		}
		if applied, err := e.reviewer.Decline(e.ctx, f.slug, stale); err != nil || applied {
			t.Fatalf("Decline(stale %s) = %t, %v, want false", stale, applied, err)
		}
	}
	untouched, _ := e.showcaseRow(t, f.id)
	if untouched.ReviewOutcome != nil || untouched.ReviewedKey != nil || untouched.ReviewedAt != nil || untouched.FirstListedAt != nil {
		t.Fatalf("a stale key changed the row: %+v", untouched)
	}
	if applied, err := e.reviewer.Approve(e.ctx, "no-such-slug-"+uuid.NewString()[:8], row.ReviewKey); err != nil || applied {
		t.Fatalf("Approve(unknown slug) = %t, %v, want false", applied, err)
	}
	other := e.addResume(t, defaultSpec())
	if applied, err := e.reviewer.Approve(e.ctx, other.slug, row.ReviewKey); err != nil || applied {
		t.Fatalf("Approve(resume without an opt-in) = %t, %v, want false", applied, err)
	}

	applied, err := e.reviewer.Approve(e.ctx, f.slug, row.ReviewKey)
	if err != nil || !applied {
		t.Fatalf("Approve() = %t, %v", applied, err)
	}
	approved, _ := e.showcaseRow(t, f.id)
	if approved.ReviewOutcome == nil || *approved.ReviewOutcome != "approved" || approved.ReviewedKey == nil || *approved.ReviewedKey != row.ReviewKey ||
		approved.ReviewedAt == nil || !approved.ReviewedAt.Equal(e.now) || approved.FirstListedAt == nil || !approved.FirstListedAt.Equal(e.now) {
		t.Fatalf("approved row = %+v", approved)
	}

	// A later approval keeps the first-listed time, so editing never moves a
	// resume up.
	earlier := e.now.Add(-48 * time.Hour)
	if _, err = e.pool.Exec(e.ctx, `UPDATE resume_showcase SET first_listed_at = $2 WHERE resume_id = $1`, f.id, earlier); err != nil {
		t.Fatalf("set first listed: %v", err)
	}
	if applied, err = e.reviewer.Approve(e.ctx, f.slug, row.ReviewKey); err != nil || !applied {
		t.Fatalf("second Approve() = %t, %v", applied, err)
	}
	again, _ := e.showcaseRow(t, f.id)
	if again.FirstListedAt == nil || !again.FirstListedAt.Equal(earlier) {
		t.Fatalf("first_listed_at = %v after a re-approval, want %v", again.FirstListedAt, earlier)
	}

	if applied, err = e.reviewer.Decline(e.ctx, f.slug, row.ReviewKey); err != nil || !applied {
		t.Fatalf("Decline() = %t, %v", applied, err)
	}
	declined, _ := e.showcaseRow(t, f.id)
	if declined.ReviewOutcome == nil || *declined.ReviewOutcome != "declined" || declined.FirstListedAt == nil || !declined.FirstListedAt.Equal(earlier) {
		t.Fatalf("declined row = %+v, want declined with the first-listed time kept", declined)
	}
}

// AC-SHOW-006: show prints the state of one resume.
func TestShowReportsTheState(t *testing.T) {
	e := newEnv(t)
	f := e.addResume(t, defaultSpec())
	if _, found, err := e.reviewer.Show(e.ctx, f.slug); err != nil || found {
		t.Fatalf("Show(no opt-in) found = %t, %v", found, err)
	}
	e.optIn(t, f.id, strp("devops"))
	state, found, err := e.reviewer.Show(e.ctx, f.slug)
	if err != nil || !found {
		t.Fatalf("Show() found = %t, %v", found, err)
	}
	row, _ := e.showcaseRow(t, f.id)
	if state.State != "pending" || state.Role == nil || *state.Role != "devops" || state.ReviewKey != row.ReviewKey || state.CardVersion != row.CardVersion {
		t.Fatalf("Show() = %+v, want a pending devops opt-in with the row's key", state)
	}
	e.approve(t, f)
	state, found, err = e.reviewer.Show(e.ctx, f.slug)
	if err != nil || !found {
		t.Fatalf("Show() after approval found = %t, %v", found, err)
	}
	if state.State != "listed" || state.Outcome == nil || state.FirstListedAt == nil {
		t.Fatalf("Show() after approval = %+v, want listed", state)
	}
}
