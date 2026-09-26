package migrations_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

// uniqueCardSlug returns a valid slug that no parallel test shares.
func uniqueCardSlug() string {
	return "card-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
}

func insertLiveCardResume(ctx context.Context, t *testing.T, tx pgx.Tx, userID uuid.UUID, slug string) uuid.UUID {
	t.Helper()
	row := defaultResumeRow(userID, "unused")
	row.slug, row.live = &slug, true
	id, err := insertResumeReturningID(ctx, tx, row)
	if err != nil {
		t.Fatalf("insert live resume: %v", err)
	}
	return id
}

func insertCard(ctx context.Context, db sqlExecer, resumeID uuid.UUID, version string, png []byte) error {
	_, err := db.Exec(ctx, `
		INSERT INTO resume_preview_cards (resume_id, version, png, rendered_at)
		VALUES ($1, $2, $3, now())`, resumeID, version, png)
	return err
}

func cardCount(ctx context.Context, t *testing.T, tx pgx.Tx, resumeID uuid.UUID) int {
	t.Helper()
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM resume_preview_cards WHERE resume_id = $1`, resumeID).Scan(&n); err != nil {
		t.Fatalf("count cards: %v", err)
	}
	return n
}

func TestResumePreviewCardConstraints(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	resumeID := insertLiveCardResume(ctx, t, tx, createTestUser(ctx, t, tx), uniqueCardSlug())

	tests := []struct {
		name           string
		version        string
		png            []byte
		wantConstraint string
	}{
		{"uppercase version", "0123456789ABCDEF", []byte{1}, "resume_preview_cards_version_check"},
		{"short version", "0123456789abcde", []byte{1}, "resume_preview_cards_version_check"},
		{"long version", "0123456789abcdef0", []byte{1}, "resume_preview_cards_version_check"},
		{"empty png", "0123456789abcdef", []byte{}, "resume_preview_cards_png_check"},
		{"png over 512 KiB", "0123456789abcdef", bytes.Repeat([]byte{1}, 524_289), "resume_preview_cards_png_check"},
		{"missing resume", "0123456789abcdef", []byte{1}, "resume_preview_cards_resume_id_fkey"},
	}
	for _, test := range tests {
		target := resumeID
		if test.wantConstraint == "resume_preview_cards_resume_id_fkey" {
			target = uuid.New()
		}
		err := withSavepoint(ctx, t, tx, func(sp pgx.Tx) error { return insertCard(ctx, sp, target, test.version, test.png) })
		requireConstraintViolation(t, err, test.wantConstraint)
	}
	if err := insertCard(ctx, tx, resumeID, "0123456789abcdef", bytes.Repeat([]byte{1}, 524_288)); err != nil {
		t.Fatalf("insert card at the byte limit: %v", err)
	}
}

// Unpublish and rename delete the stored card in the same transaction that
// changes the public state; resume delete removes it by cascade. Updates
// that keep the resume live under the same slug keep the card (ADR 0055).
func TestResumePreviewCardRevocation(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	userID := createTestUser(ctx, t, tx)
	png := []byte{0x89, 'P', 'N', 'G'}

	tests := []struct {
		name     string
		mutation string
		wantKept bool
	}{
		{"content save keeps the card", `UPDATE resumes SET revision = revision + 1, title = 'Edited' WHERE id = $1`, true},
		{"discovery change keeps the card", `UPDATE resumes SET seo_geo_enabled = true WHERE id = $1`, true},
		{"unpublish removes the card", `UPDATE resumes SET live = false, seo_geo_enabled = false WHERE id = $1`, false},
		{"rename removes the card", `UPDATE resumes SET slug = $2 WHERE id = $1`, false},
		{"delete removes the card", `DELETE FROM resumes WHERE id = $1`, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resumeID := insertLiveCardResume(ctx, t, tx, userID, uniqueCardSlug())
			if err := insertCard(ctx, tx, resumeID, "0123456789abcdef", png); err != nil {
				t.Fatalf("insert card: %v", err)
			}
			args := []any{resumeID}
			if strings.Contains(test.mutation, "$2") {
				args = append(args, uniqueCardSlug())
			}
			if _, err := tx.Exec(ctx, test.mutation, args...); err != nil {
				t.Fatalf("mutation: %v", err)
			}
			want := 0
			if test.wantKept {
				want = 1
			}
			if got := cardCount(ctx, t, tx, resumeID); got != want {
				t.Fatalf("cards after mutation = %d, want %d", got, want)
			}
			if _, err := tx.Exec(ctx, `DELETE FROM resumes WHERE id = $1`, resumeID); err != nil {
				t.Fatalf("clean up resume: %v", err)
			}
		})
	}
}

func TestResumePreviewCardsMigrationUpDownUp(t *testing.T) {
	t.Parallel()

	dsn := newTestDatabase(t)
	db, err := migrations.Open(dsn)
	if err != nil {
		t.Fatalf("Open() error: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Errorf("close database: %v", closeErr)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), harnessTimeout)
	defer cancel()

	provider, err := migrations.NewProvider(db, migrations.FS)
	if err != nil {
		t.Fatalf("NewProvider() error: %v", err)
	}
	if _, err := provider.UpTo(ctx, 7); err != nil {
		t.Fatalf("UpTo(7) error: %v", err)
	}
	if n := mustQueryInt(ctx, t, db, `SELECT count(*) FROM pg_trigger WHERE tgname = 'resumes_revoke_preview_card'`); n != 1 {
		t.Fatalf("revocation triggers after up = %d, want 1", n)
	}
	if _, err := provider.DownTo(ctx, 6); err != nil {
		t.Fatalf("DownTo(6) error: %v", err)
	}
	if n := mustQueryInt(ctx, t, db, `SELECT count(*) FROM pg_class WHERE relname = 'resume_preview_cards'`); n != 0 {
		t.Fatalf("card tables after down = %d, want 0", n)
	}
	if n := mustQueryInt(ctx, t, db, `SELECT count(*) FROM pg_proc WHERE proname = 'revoke_resume_preview_card'`); n != 0 {
		t.Fatalf("revocation functions after down = %d, want 0", n)
	}
	if _, err := provider.UpTo(ctx, 7); err != nil {
		t.Fatalf("UpTo(7) after down error: %v", err)
	}
}
