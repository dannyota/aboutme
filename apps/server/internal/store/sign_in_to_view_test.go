package store_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/store"
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

	row := publish(1, true, true, time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC))
	if row.ViewPassEpoch != 1 || !row.SignInToView {
		t.Fatalf("after first turn-on: SignInToView=%t ViewPassEpoch=%d, want true 1", row.SignInToView, row.ViewPassEpoch)
	}

	row = publish(2, true, true, time.Date(2026, time.July, 2, 0, 0, 0, 0, time.UTC))
	if row.ViewPassEpoch != 1 {
		t.Fatalf("after unchanged republish: ViewPassEpoch = %d, want 1", row.ViewPassEpoch)
	}

	row = publish(3, false, true, time.Date(2026, time.July, 3, 0, 0, 0, 0, time.UTC))
	if row.ViewPassEpoch != 1 || row.Live {
		t.Fatalf("after unpublish: Live=%t ViewPassEpoch=%d, want false 1", row.Live, row.ViewPassEpoch)
	}

	row = publish(4, true, true, time.Date(2026, time.July, 4, 0, 0, 0, 0, time.UTC))
	if row.ViewPassEpoch != 2 {
		t.Fatalf("after re-live: ViewPassEpoch = %d, want 2", row.ViewPassEpoch)
	}

	row = publish(5, true, false, time.Date(2026, time.July, 5, 0, 0, 0, 0, time.UTC))
	if row.ViewPassEpoch != 2 || row.SignInToView {
		t.Fatalf("after turn-off: SignInToView=%t ViewPassEpoch=%d, want false 2", row.SignInToView, row.ViewPassEpoch)
	}
}

func TestDiscoveryQueriesExcludeSignInToView(t *testing.T) {
	ctx, _, tx, queries := newPublicStoreTx(t)
	userID := createPublicStoreUser(ctx, t, tx)
	slug := "sitv-excl-" + uuid.NewString()[:8]
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
