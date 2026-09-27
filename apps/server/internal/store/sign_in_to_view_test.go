// Sign in to view (docs/design/viewer-analytics/sign-in-to-view.md "Gated
// routes"; AC-VIEW-007): PublishResumeCAS raises the pass epoch exactly
// when the switch turns on, or a sign_in resume becomes live again after
// being unpublished.
package store_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func TestPublishResumeCASRaisesViewPassEpochOnTurnOn(t *testing.T) {
	ctx, _, tx, queries := newPublicStoreTx(t)
	userID := createPublicStoreUser(ctx, t, tx)
	resumeID := createPublicStoreResume(ctx, t, tx, userID, nil, false, false)
	slug := "sign-in-to-view-a"

	publish := func(revision int64, live, signInToView bool, when time.Time) store.Resume {
		t.Helper()
		row, err := queries.PublishResumeCAS(ctx, store.PublishResumeCASParams{
			ID: resumeID, UserID: userID, ExpectedRevision: revision,
			Slug: &slug, Live: live, DownloadEnabled: true, SEOGeoEnabled: false,
			SignInToView: signInToView, UpdatedAt: when,
		})
		if err != nil {
			t.Fatalf("PublishResumeCAS() error: %v", err)
		}
		return row
	}

	// First publish, live, switch on: a fresh grant, epoch rises to 1.
	row := publish(1, true, true, time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC))
	if row.ViewPassEpoch != 1 || !row.SignInToView {
		t.Fatalf("after first turn-on: SignInToView=%t ViewPassEpoch=%d, want true 1", row.SignInToView, row.ViewPassEpoch)
	}

	// Re-publishing with the switch already on and staying live does not
	// raise the epoch again.
	row = publish(2, true, true, time.Date(2026, time.July, 2, 0, 0, 0, 0, time.UTC))
	if row.ViewPassEpoch != 1 {
		t.Fatalf("after unchanged republish: ViewPassEpoch = %d, want 1", row.ViewPassEpoch)
	}

	// Unpublish: no epoch change (turning off needs no fence and never
	// raises the epoch).
	row = publish(3, false, true, time.Date(2026, time.July, 3, 0, 0, 0, 0, time.UTC))
	if row.ViewPassEpoch != 1 || row.Live {
		t.Fatalf("after unpublish: Live=%t ViewPassEpoch=%d, want false 1", row.Live, row.ViewPassEpoch)
	}

	// Becoming live again while sign_in stays on raises the epoch, ending
	// any pass issued during the earlier live period.
	row = publish(4, true, true, time.Date(2026, time.July, 4, 0, 0, 0, 0, time.UTC))
	if row.ViewPassEpoch != 2 {
		t.Fatalf("after re-live: ViewPassEpoch = %d, want 2", row.ViewPassEpoch)
	}

	// Turning the switch off never raises the epoch, even though it is a
	// state change.
	row = publish(5, true, false, time.Date(2026, time.July, 5, 0, 0, 0, 0, time.UTC))
	if row.ViewPassEpoch != 2 || row.SignInToView {
		t.Fatalf("after turn-off: SignInToView=%t ViewPassEpoch=%d, want false 2", row.SignInToView, row.ViewPassEpoch)
	}
}

// AC-VIEW-010: a live, discovery-eligible resume with sign_in_to_view on is
// excluded from both discovery queries the sitemap and llms.txt read from
// (docs/design/viewer-analytics/delivery.md "Schema").
func TestDiscoveryQueriesExcludeSignInToView(t *testing.T) {
	ctx, _, tx, queries := newPublicStoreTx(t)
	userID := createPublicStoreUser(ctx, t, tx)
	slug := "sign-in-to-view-excluded-" + uuid.NewString()[:8]
	resumeID := createPublicStoreResume(ctx, t, tx, userID, &slug, true, true)
	if _, err := tx.Exec(ctx, `UPDATE resumes SET sign_in_to_view = true WHERE id = $1`, resumeID); err != nil {
		t.Fatalf("set sign_in_to_view: %v", err)
	}

	slugs, err := queries.ListEligiblePublicSlugs(ctx)
	if err != nil {
		t.Fatalf("ListEligiblePublicSlugs() error: %v", err)
	}
	for _, got := range slugs {
		if got == slug {
			t.Fatalf("ListEligiblePublicSlugs() = %v, must exclude a sign_in_to_view resume's slug %q", slugs, slug)
		}
	}

	snapshot, err := queries.GetPublicDiscoverySnapshot(ctx)
	if err != nil {
		t.Fatalf("GetPublicDiscoverySnapshot() error: %v", err)
	}
	for _, got := range snapshot.Slugs {
		if got == slug {
			t.Fatalf("GetPublicDiscoverySnapshot().Slugs = %v, must exclude a sign_in_to_view resume's slug %q", snapshot.Slugs, slug)
		}
	}
}
