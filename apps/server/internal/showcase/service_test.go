package showcase

import (
	"testing"
	"time"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/previewcard"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// cardBuilderValues reads the resume the way the preview card builder does and
// returns its card version.
func cardBuilderValues(t *testing.T, e *env, id uuid.UUID) string {
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
	version, err := card.Version()
	if err != nil {
		t.Fatalf("Version() error: %v", err)
	}
	return version
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
	if version := cardBuilderValues(t, e, f.id); row.CardVersion != version {
		t.Fatalf("row version %s, card builder %s", row.CardVersion, version)
	}

	spec.name = "Grace Hopper"
	personal, _, _ := spec.parts(t)
	if _, err := e.pool.Exec(e.ctx, `UPDATE resumes SET personal_details = $2 WHERE id = $1`, f.id, personal); err != nil {
		t.Fatalf("update name: %v", err)
	}
	e.sync(t, f.id)
	changed, _ := e.showcaseRow(t, f.id)
	if version := cardBuilderValues(t, e, f.id); changed.CardVersion == row.CardVersion || changed.CardVersion != version {
		t.Fatalf("after a name change version %s, want the card builder's %s (old %s)", changed.CardVersion, version, row.CardVersion)
	}
	if !changed.RequestedAt.Equal(row.RequestedAt) {
		t.Fatalf("a name change moved requested_at from %v to %v", row.RequestedAt, changed.RequestedAt)
	}

	// A color change moves the card version too.
	spec.customization.Colors.Primary = "#be123c"
	_, _, customization := spec.parts(t)
	if _, err := e.pool.Exec(e.ctx, `UPDATE resumes SET customization = $2 WHERE id = $1`, f.id, customization); err != nil {
		t.Fatalf("update color: %v", err)
	}
	e.sync(t, f.id)
	recolored, _ := e.showcaseRow(t, f.id)
	if recolored.CardVersion == changed.CardVersion {
		t.Fatalf("a color change left the version at %s", changed.CardVersion)
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

// AC-SHOW-001, AC-SHOW-005: turning the switch on inserts a row, a row that
// stays on keeps its opt-in time and takes the new role, and turning it off and
// on again starts a new opt-in. The review columns stay unused.
func TestPublishTxLifecycle(t *testing.T) {
	e := newEnv(t)
	f := e.addResume(t, defaultSpec())

	e.optIn(t, f.id, strp("backend"))
	row, found := e.showcaseRow(t, f.id)
	if !found || row.Role == nil || *row.Role != "backend" {
		t.Fatalf("new opt-in = %+v found %t, want a backend row", row, found)
	}
	requireReviewColumnsUnused(t, row)
	if !row.RequestedAt.Equal(e.now) {
		t.Fatalf("requested_at = %v, want %v", row.RequestedAt, e.now)
	}
	later := e.now.Add(time.Hour)
	e.service.now = func() time.Time { return later }

	e.optIn(t, f.id, strp("qa"))
	row, _ = e.showcaseRow(t, f.id)
	if row.Role == nil || *row.Role != "qa" || !row.RequestedAt.Equal(e.now) {
		t.Fatalf("a row that stays on = %+v, want the new role and the first opt-in time", row)
	}

	e.optIn(t, f.id, nil)
	row, _ = e.showcaseRow(t, f.id)
	if row.Role != nil {
		t.Fatalf("clearing the role = %+v, want no role", row)
	}

	e.publish(t, PublishChange{ResumeID: f.id, Enabled: false})
	if _, found = e.showcaseRow(t, f.id); found {
		t.Fatal("turning the switch off left the row")
	}
	e.publish(t, PublishChange{ResumeID: f.id, Enabled: false})

	e.optIn(t, f.id, nil)
	row, _ = e.showcaseRow(t, f.id)
	requireReviewColumnsUnused(t, row)
	if !row.RequestedAt.Equal(later) {
		t.Fatalf("requested_at after a new opt-in = %v, want %v", row.RequestedAt, later)
	}
}

// requireReviewColumnsUnused checks the columns kept only for the previous
// release (docs/design/showcase.md "Data and contract").
func requireReviewColumnsUnused(t *testing.T, row store.ResumeShowcase) {
	t.Helper()
	if row.ReviewKey != "0000000000000000" || row.ReviewedKey != nil || row.ReviewOutcome != nil || row.ReviewedAt != nil || row.FirstListedAt != nil {
		t.Fatalf("review columns = %+v, want the default key and nothing else", row)
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

// AC-SHOW-006: a startup recompute applies the current derivation to every row
// and skips a resume that cannot be projected.
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

	stale := store.UpdateResumeShowcaseDerivedParams{CardVersion: "bbbbbbbbbbbbbbbb"}
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
		if version := cardBuilderValues(t, e, f.id); row.CardVersion != version {
			t.Errorf("%s: row version %s, want %s", f.slug, row.CardVersion, version)
		}
	}
	if row, _ := e.showcaseRow(t, styledFixture.id); row.TemplateID == nil || *row.TemplateID != files[2].ID {
		t.Errorf("styled template = %v, want %s", row.TemplateID, files[2].ID)
	}
	brokenAfter, _ := e.showcaseRow(t, brokenFixture.id)
	if brokenAfter.CardVersion != brokenBefore.CardVersion {
		t.Errorf("the skipped row changed: %+v then %+v", brokenBefore, brokenAfter)
	}
}
