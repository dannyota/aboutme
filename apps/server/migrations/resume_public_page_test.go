package migrations_test

import (
	"context"
	"strings"
	"testing"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

// 00003 adds the public page title and emoji favicon. The checks bound
// storage and reject the empty string; the down path drops both columns and
// the up path can apply again.
func TestResumePublicPageMigrationUpDownUp(t *testing.T) {
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
	if _, err := provider.UpTo(ctx, 3); err != nil {
		t.Fatalf("UpTo(3) error: %v", err)
	}
	var userID string
	if err := db.QueryRowContext(ctx, `INSERT INTO users (email, name) VALUES ('page@example.test', 'Page') RETURNING id::text`).Scan(&userID); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	insert := func(title, emoji any) error {
		_, execErr := db.ExecContext(ctx, `
			INSERT INTO resumes (user_id, title, schema_version, personal_details, content, customization, public_title, favicon_emoji)
			VALUES ($1::uuid, 'Resume', 3, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb, $2, $3)`, userID, title, emoji)
		return execErr
	}
	if err := insert(nil, nil); err != nil {
		t.Fatalf("NULL settings rejected: %v", err)
	}
	if err := insert("Danny from aboutme.vn", "\U0001F680"); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}
	for name, values := range map[string][2]any{
		"empty title":    {"", nil},
		"overlong title": {strings.Repeat("x", 561), nil},
		"empty emoji":    {nil, ""},
		"overlong emoji": {nil, strings.Repeat("\U0001F680", 17)},
	} {
		if err := insert(values[0], values[1]); err == nil {
			t.Fatalf("%s accepted, want the check to reject it", name)
		}
	}

	if _, err := provider.DownTo(ctx, 2); err != nil {
		t.Fatalf("DownTo(2) error: %v", err)
	}
	if n := mustQueryInt(ctx, t, db, `SELECT count(*) FROM information_schema.columns WHERE table_name = 'resumes' AND column_name IN ('public_title', 'favicon_emoji')`); n != 0 {
		t.Fatalf("columns after down = %d, want 0", n)
	}
	if _, err := provider.UpTo(ctx, 3); err != nil {
		t.Fatalf("UpTo(3) after down error: %v", err)
	}
	if err := insert("Again", "\U0001F680"); err != nil {
		t.Fatalf("settings rejected after re-up: %v", err)
	}
}
