package store_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/previewcard"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

var previewCardPNG = []byte("\x89PNG\r\n\x1a\nstored-card")

type previewCardEnv struct {
	ctx      context.Context
	pool     *pgxpool.Pool
	cards    *previewcard.Store
	resumeID uuid.UUID
	version  string
}

// newPreviewCardEnv commits one live resume with a real document, so the
// card store's own transactions see it, and removes its owner afterwards.
func newPreviewCardEnv(t *testing.T) previewCardEnv {
	t.Helper()
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New() error: %v", err)
	}
	t.Cleanup(pool.Close)

	name, headline := "Ada Lovelace", "Engineer"
	document := schema.Resume{
		SchemaVersion:   schema.CurrentVersion,
		PersonalDetails: schema.PersonalDetails{FullName: &name, Headline: &headline},
		Content:         map[string]schema.Section{},
	}
	document.Customization.Colors = schema.Colors{Primary: "#1d4ed8", Text: "#111111", Background: "#ffffff"}
	personal, content, customization := mustJSON(t, document.PersonalDetails), mustJSON(t, document.Content), mustJSON(t, document.Customization)
	var userID, resumeID uuid.UUID
	if err = pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, 'Card') RETURNING id`, uuid.NewString()+"@example.com").Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		if _, cleanupErr := pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID); cleanupErr != nil {
			t.Errorf("delete user: %v", cleanupErr)
		}
	})
	slug := "card-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	lng := "en"
	if err = pool.QueryRow(ctx, `
		INSERT INTO resumes (user_id, title, slug, live, schema_version, lng, personal_details, content, customization)
		VALUES ($1, 'Card', $2, true, $3, $4, $5, $6, $7) RETURNING id`,
		userID, slug, int32(schema.CurrentVersion), lng, personal, content, customization).Scan(&resumeID); err != nil {
		t.Fatalf("insert resume: %v", err)
	}

	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	origin, err := publicresume.ParsePublicOrigin("https://resume.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	queries := store.New(pool)
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{
		Store: queries, Projector: docmigrate.NewIdentityProjector(), Coordinator: coordinator, Origin: origin,
	})
	if err != nil {
		t.Fatal(err)
	}
	cards, err := previewcard.NewStore(pool, reader, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	row, err := queries.GetLiveResumeByID(ctx, resumeID)
	if err != nil {
		t.Fatalf("GetLiveResumeByID() error: %v", err)
	}
	snapshot, err := reader.ProjectRow(row)
	if err != nil {
		t.Fatal(err)
	}
	version, err := previewcard.VersionOf(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return previewCardEnv{ctx: ctx, pool: pool, cards: cards, resumeID: resumeID, version: version}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func (env previewCardEnv) storedVersion(t *testing.T) string {
	t.Helper()
	card, found, err := env.cards.Stored(env.ctx, env.resumeID)
	if err != nil {
		t.Fatalf("Stored() error: %v", err)
	}
	if !found {
		return ""
	}
	return card.Version
}

func TestPreviewCardSaveStoresOnlyALiveCurrentCard(t *testing.T) {
	env := newPreviewCardEnv(t)
	if err := env.cards.Save(env.ctx, env.resumeID, "0123456789abcdef", previewCardPNG); !errors.Is(err, previewcard.ErrStale) {
		t.Fatalf("Save(old version) error = %v, want ErrStale", err)
	}
	if got := env.storedVersion(t); got != "" {
		t.Fatalf("stored version after a stale save = %q, want none", got)
	}
	if err := env.cards.Save(env.ctx, env.resumeID, env.version, previewCardPNG); err != nil {
		t.Fatalf("Save(current) error: %v", err)
	}
	if got := env.storedVersion(t); got != env.version {
		t.Fatalf("stored version = %q, want %q", got, env.version)
	}
	live, err := env.cards.LiveCards(env.ctx)
	if err != nil {
		t.Fatal(err)
	}
	listed := false
	for _, card := range live {
		listed = listed || (card.ResumeID == env.resumeID && card.StoredVersion == env.version)
	}
	if !listed {
		t.Fatal("LiveCards() does not list the stored card")
	}

	if _, err := env.pool.Exec(env.ctx, `UPDATE resumes SET live = false WHERE id = $1`, env.resumeID); err != nil {
		t.Fatalf("unpublish: %v", err)
	}
	if got := env.storedVersion(t); got != "" {
		t.Fatalf("stored version after unpublish = %q, want none", got)
	}
	if err := env.cards.Save(env.ctx, env.resumeID, env.version, previewCardPNG); !errors.Is(err, previewcard.ErrNotLive) {
		t.Fatalf("Save() after unpublish error = %v, want ErrNotLive", err)
	}
	if _, live, err := env.cards.Live(env.ctx, env.resumeID); err != nil || live {
		t.Fatalf("Live() after unpublish = %t, %v, want not live", live, err)
	}
	if err := env.cards.Save(env.ctx, uuid.New(), env.version, previewCardPNG); !errors.Is(err, previewcard.ErrNotLive) {
		t.Fatalf("Save(missing resume) error = %v, want ErrNotLive", err)
	}
}

// A store that starts while an unpublish holds the resume row waits for it,
// then sees the committed unpublish and writes nothing.
func TestPreviewCardSaveWaitsForAnUnpublishAndIsRefused(t *testing.T) {
	env := newPreviewCardEnv(t)
	unpublish, err := env.pool.Begin(env.ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rollbackPublicTestTx(t, unpublish) })
	if _, err := unpublish.Exec(env.ctx, `UPDATE resumes SET live = false WHERE id = $1`, env.resumeID); err != nil {
		t.Fatalf("unpublish: %v", err)
	}
	saved := make(chan error, 1)
	go func() { saved <- env.cards.Save(env.ctx, env.resumeID, env.version, previewCardPNG) }()
	waitForBlockedShareLock(env.ctx, t, env.pool)
	if err := unpublish.Commit(env.ctx); err != nil {
		t.Fatalf("commit unpublish: %v", err)
	}
	select {
	case err := <-saved:
		if !errors.Is(err, previewcard.ErrNotLive) {
			t.Fatalf("Save() racing an unpublish error = %v, want ErrNotLive", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Save() did not finish after the unpublish committed")
	}
	if got := env.storedVersion(t); got != "" {
		t.Fatalf("stored version after a racing unpublish = %q, want none", got)
	}
}

// A store that commits while an unpublish waits on the resume row leaves no
// card: the unpublish trigger removes the row the store just committed.
func TestPreviewCardCommittedBeforeAWaitingUnpublishIsRemoved(t *testing.T) {
	env := newPreviewCardEnv(t)
	storeTx, err := env.pool.Begin(env.ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rollbackPublicTestTx(t, storeTx) })
	queries := store.New(storeTx)
	if _, err = queries.LockResumeForPreviewCard(env.ctx, env.resumeID); err != nil {
		t.Fatalf("LockResumeForPreviewCard() error: %v", err)
	}
	if err = queries.UpsertResumePreviewCard(env.ctx, store.UpsertResumePreviewCardParams{
		ResumeID: env.resumeID, Version: env.version, PNG: previewCardPNG, RenderedAt: time.Now(),
	}); err != nil {
		t.Fatalf("UpsertResumePreviewCard() error: %v", err)
	}
	unpublished := make(chan error, 1)
	go func() {
		_, execErr := env.pool.Exec(env.ctx, `UPDATE resumes SET live = false WHERE id = $1`, env.resumeID)
		unpublished <- execErr
	}()
	waitForBlockedUpdate(env.ctx, t, env.pool)
	if err = storeTx.Commit(env.ctx); err != nil {
		t.Fatalf("commit store: %v", err)
	}
	select {
	case err = <-unpublished:
		if err != nil {
			t.Fatalf("unpublish: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the unpublish did not finish after the store committed")
	}
	if got := env.storedVersion(t); got != "" {
		t.Fatalf("stored version after the waiting unpublish = %q, want none", got)
	}
}

func waitForBlockedUpdate(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting int
		if err := pool.QueryRow(ctx, `
			SELECT count(*) FROM pg_stat_activity
			WHERE wait_event_type = 'Lock' AND query LIKE 'UPDATE resumes SET live = false%'`).Scan(&waiting); err != nil {
			t.Fatalf("probe blocked unpublish: %v", err)
		}
		if waiting > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the unpublish did not wait on the card store lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A committed card is removed by a rename and by a delete.
func TestPreviewCardIsRemovedByRenameAndDelete(t *testing.T) {
	for _, mutation := range []string{
		`UPDATE resumes SET slug = slug || '-x' WHERE id = $1`,
		`DELETE FROM resumes WHERE id = $1`,
	} {
		env := newPreviewCardEnv(t)
		if err := env.cards.Save(env.ctx, env.resumeID, env.version, previewCardPNG); err != nil {
			t.Fatalf("Save() error: %v", err)
		}
		if _, err := env.pool.Exec(env.ctx, mutation, env.resumeID); err != nil {
			t.Fatalf("%s: %v", mutation, err)
		}
		if got := env.storedVersion(t); got != "" {
			t.Fatalf("stored version after %q = %q, want none", mutation, got)
		}
	}
}

func waitForBlockedShareLock(ctx context.Context, t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting int
		if err := pool.QueryRow(ctx, `
			SELECT count(*) FROM pg_stat_activity
			WHERE wait_event_type = 'Lock' AND query LIKE '%FOR SHARE%'`).Scan(&waiting); err != nil {
			t.Fatalf("probe blocked store: %v", err)
		}
		if waiting > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the card store did not wait on the unpublish lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
