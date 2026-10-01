package showcase

import (
	"testing"
	"time"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/previewcard"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// cardBuilderValues reads the resume the way the preview card builder does and
// returns its card version and the review key of that card.
func cardBuilderValues(t *testing.T, e *env, id uuid.UUID) (version, key string) {
	t.Helper()
	cards, err := previewcard.NewStore(e.pool, e.reader, time.Now)
	if err != nil {
		t.Fatalf("NewStore() error: %v", err)
	}
	snapshot, live, err := cards.Live(e.ctx, id)
	if err != nil || !live {
		t.Fatalf("Live() = %t, %v", live, err)
	}
	card, err := previewcard.FromSnapshot(snapshot)
	if err != nil {
		t.Fatalf("FromSnapshot() error: %v", err)
	}
	version, err = card.Version()
	if err != nil {
		t.Fatalf("Version() error: %v", err)
	}
	return version, mustKey(t, card)
}

// AC-SHOW-006: the in-transaction derivation equals the card builder's output
// for the same resume, and a write that changes what the listing shows
// recomputes it.
func TestSyncTxEqualsTheCardBuilder(t *testing.T) {
	e := newEnv(t)
	spec := defaultSpec()
	spec.photoKey = "resumes/photo-key.png"
	spec.crop = &schema.PhotoCrop{X: 0.1, Y: 0.1, Width: 0.5, Height: 0.5}
	f := e.addResume(t, spec)
	e.optIn(t, f.id, strp("backend"))

	row, found := e.showcaseRow(t, f.id)
	if !found {
		t.Fatal("no showcase row after the opt-in")
	}
	version, key := cardBuilderValues(t, e, f.id)
	if row.CardVersion != version || row.ReviewKey != key {
		t.Fatalf("row = key %s version %s, card builder = key %s version %s", row.ReviewKey, row.CardVersion, key, version)
	}

	e.approve(t, f)
	spec.name = "Grace Hopper"
	personal, _, _ := spec.parts(t)
	if _, err := e.pool.Exec(e.ctx, `UPDATE resumes SET personal_details = $2 WHERE id = $1`, f.id, personal); err != nil {
		t.Fatalf("update name: %v", err)
	}
	e.sync(t, f.id)
	changed, _ := e.showcaseRow(t, f.id)
	version, key = cardBuilderValues(t, e, f.id)
	if changed.ReviewKey == row.ReviewKey || changed.ReviewKey != key || changed.CardVersion != version {
		t.Fatalf("after a name change row = key %s version %s, want the card builder's key %s version %s (old key %s)",
			changed.ReviewKey, changed.CardVersion, key, version, row.ReviewKey)
	}
	if state := resume.ShowcaseStateOf(changed.ReviewKey, changed.ReviewedKey, changed.ReviewOutcome); state != resume.ShowcasePending {
		t.Fatalf("state after a name change = %s, want pending", state)
	}

	// A color change moves the card version and keeps the key and the review.
	e.approve(t, f)
	approved, _ := e.showcaseRow(t, f.id)
	spec.customization.Colors.Primary = "#be123c"
	_, _, customization := spec.parts(t)
	if _, err := e.pool.Exec(e.ctx, `UPDATE resumes SET customization = $2 WHERE id = $1`, f.id, customization); err != nil {
		t.Fatalf("update color: %v", err)
	}
	e.sync(t, f.id)
	recolored, _ := e.showcaseRow(t, f.id)
	if recolored.ReviewKey != approved.ReviewKey || recolored.CardVersion == approved.CardVersion {
		t.Fatalf("after a color change key %s->%s version %s->%s, want the same key and a new version",
			approved.ReviewKey, recolored.ReviewKey, approved.CardVersion, recolored.CardVersion)
	}
	if state := resume.ShowcaseStateOf(recolored.ReviewKey, recolored.ReviewedKey, recolored.ReviewOutcome); state != resume.ShowcaseListed {
		t.Fatalf("state after a color change = %s, want listed", state)
	}
}

// AC-SHOW-004: the sync derives the template of a preset customization.
func TestSyncTxDerivesTheTemplate(t *testing.T) {
	e := newEnv(t)
	files := readTemplateFiles(t)
	spec := defaultSpec()
	f := e.addResume(t, spec)
	e.optIn(t, f.id, nil)
	if row, _ := e.showcaseRow(t, f.id); row.TemplateID != nil {
		t.Fatalf("template = %q, want nil before a template is applied", *row.TemplateID)
	}
	spec.customization = appliedCustomization(t, files[5], []string{}, []string{})
	_, _, customization := spec.parts(t)
	if _, err := e.pool.Exec(e.ctx, `UPDATE resumes SET customization = $2 WHERE id = $1`, f.id, customization); err != nil {
		t.Fatalf("apply template: %v", err)
	}
	e.sync(t, f.id)
	row, _ := e.showcaseRow(t, f.id)
	if row.TemplateID == nil || *row.TemplateID != files[5].ID {
		t.Fatalf("template = %v, want %s", row.TemplateID, files[5].ID)
	}
}

// AC-SHOW-006: a resume without a row gets none and nothing is written.
func TestSyncTxLeavesAResumeWithoutARowAlone(t *testing.T) {
	e := newEnv(t)
	f := e.addResume(t, defaultSpec())
	e.sync(t, f.id)
	if _, found := e.showcaseRow(t, f.id); found {
		t.Fatal("SyncTx created a showcase row for a resume that did not opt in")
	}
}

// AC-SHOW-001: a row whose resume is no longer live, or requires sign in to
// view, never survives a sync.
func TestSyncTxDeletesARowThatCannotBeListed(t *testing.T) {
	e := newEnv(t)
	for name, statement := range map[string]string{
		"unpublished":  `UPDATE resumes SET live = false WHERE id = $1`,
		"sign in view": `UPDATE resumes SET sign_in_to_view = true WHERE id = $1`,
	} {
		f := e.listed(t, defaultSpec(), nil)
		if _, err := e.pool.Exec(e.ctx, statement, f.id); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		e.sync(t, f.id)
		if _, found := e.showcaseRow(t, f.id); found {
			t.Errorf("%s: the showcase row survived the sync", name)
		}
	}
}

// AC-SHOW-001, AC-SHOW-006: turning the switch on starts a review, a row that
// stays on keeps its review result, and turning it off and on again starts a
// new review.
func TestPublishTxLifecycle(t *testing.T) {
	e := newEnv(t)
	f := e.addResume(t, defaultSpec())

	e.optIn(t, f.id, strp("backend"))
	row, found := e.showcaseRow(t, f.id)
	if !found || row.Role == nil || *row.Role != "backend" || row.ReviewOutcome != nil || row.ReviewedKey != nil || row.FirstListedAt != nil {
		t.Fatalf("new opt-in = %+v found %t, want a pending backend row", row, found)
	}
	if !row.RequestedAt.Equal(e.now) {
		t.Fatalf("requested_at = %v, want %v", row.RequestedAt, e.now)
	}

	e.approve(t, f)
	e.optIn(t, f.id, strp("qa"))
	row, _ = e.showcaseRow(t, f.id)
	if row.Role == nil || *row.Role != "qa" || row.ReviewOutcome == nil || *row.ReviewOutcome != "approved" || row.FirstListedAt == nil {
		t.Fatalf("a row that stays on = %+v, want the new role and the review kept", row)
	}
	firstRequested := row.RequestedAt

	e.optIn(t, f.id, nil)
	row, _ = e.showcaseRow(t, f.id)
	if row.Role != nil || row.ReviewOutcome == nil {
		t.Fatalf("clearing the role = %+v, want no role and the review kept", row)
	}

	e.publish(t, PublishChange{ResumeID: f.id, Enabled: false})
	if _, found = e.showcaseRow(t, f.id); found {
		t.Fatal("turning the switch off left the row")
	}
	e.publish(t, PublishChange{ResumeID: f.id, Enabled: false})

	e.optIn(t, f.id, nil)
	row, _ = e.showcaseRow(t, f.id)
	if row.ReviewOutcome != nil || row.ReviewedKey != nil || row.ReviewedAt != nil || row.FirstListedAt != nil {
		t.Fatalf("opt-in after an opt-out = %+v, want a new review with nothing carried over", row)
	}
	if row.RequestedAt.Before(firstRequested) {
		t.Fatalf("requested_at went back: %v then %v", firstRequested, row.RequestedAt)
	}
}

// AC-SHOW-001: a resume that is not listable gets no row from an opt-in.
func TestPublishTxIgnoresAnOptInForAResumeThatCannotBeListed(t *testing.T) {
	e := newEnv(t)
	notLive := defaultSpec()
	notLive.notLive = true
	gated := defaultSpec()
	gated.signInToView = true
	for name, spec := range map[string]rowSpec{"not live": notLive, "sign in to view": gated} {
		f := e.addResume(t, spec)
		e.optIn(t, f.id, nil)
		if _, found := e.showcaseRow(t, f.id); found {
			t.Errorf("%s: an opt-in created a showcase row", name)
		}
	}
}

// AC-SHOW-006: a startup recompute applies the current derivation to every row,
// keeps the review of a resume whose key did not change, and skips a resume
// that cannot be projected.
func TestRecomputeAllAppliesTheCurrentDerivation(t *testing.T) {
	e := newFreshEnv(t)
	files := readTemplateFiles(t)
	plain := e.listed(t, defaultSpec(), nil)
	styled := defaultSpec()
	styled.customization = appliedCustomization(t, files[2], []string{}, []string{})
	styledFixture := e.listed(t, styled, nil)

	broken := defaultSpec()
	brokenFixture := e.addResume(t, broken)
	e.optIn(t, brokenFixture.id, nil)
	brokenBefore, _ := e.showcaseRow(t, brokenFixture.id)
	// A photo with no key cannot yield a card; the row keeps its values.
	if _, err := e.pool.Exec(e.ctx, `UPDATE resumes SET personal_details = '{"photo":{"key":""}}' WHERE id = $1`, brokenFixture.id); err != nil {
		t.Fatalf("break resume: %v", err)
	}

	stale := store.UpdateResumeShowcaseDerivedParams{ReviewKey: "aaaaaaaaaaaaaaaa", CardVersion: "bbbbbbbbbbbbbbbb"}
	for _, f := range []fixture{plain, styledFixture} {
		stale.ResumeID = f.id
		if err := e.queries.UpdateResumeShowcaseDerived(e.ctx, stale); err != nil {
			t.Fatalf("stale values: %v", err)
		}
	}

	recomputed, skipped, err := e.service.RecomputeAll(e.ctx)
	if err != nil {
		t.Fatalf("RecomputeAll() error: %v", err)
	}
	if recomputed != 2 || skipped != 1 {
		t.Fatalf("RecomputeAll() = %d recomputed, %d skipped, want 2 and 1", recomputed, skipped)
	}
	for _, f := range []fixture{plain, styledFixture} {
		row, _ := e.showcaseRow(t, f.id)
		version, key := cardBuilderValues(t, e, f.id)
		if row.ReviewKey != key || row.CardVersion != version {
			t.Errorf("%s: row key %s version %s, want %s and %s", f.slug, row.ReviewKey, row.CardVersion, key, version)
		}
		// Nothing the key covers changed, so the review result still holds.
		if state := resume.ShowcaseStateOf(row.ReviewKey, row.ReviewedKey, row.ReviewOutcome); state != resume.ShowcaseListed {
			t.Errorf("%s: state = %s, want listed after an unchanged recompute", f.slug, state)
		}
	}
	if row, _ := e.showcaseRow(t, styledFixture.id); row.TemplateID == nil || *row.TemplateID != files[2].ID {
		t.Errorf("styled template = %v, want %s", row.TemplateID, files[2].ID)
	}
	brokenAfter, _ := e.showcaseRow(t, brokenFixture.id)
	if brokenAfter.ReviewKey != brokenBefore.ReviewKey || brokenAfter.CardVersion != brokenBefore.CardVersion {
		t.Errorf("the skipped row changed: %+v then %+v", brokenBefore, brokenAfter)
	}
}
