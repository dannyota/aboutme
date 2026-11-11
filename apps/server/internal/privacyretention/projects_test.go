package privacyretention

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"danny.vn/aboutme/apps/server/internal/store"
)

func TestRetain_ExpiredProjectSlugHolds(t *testing.T) {
	ctx, pool := newRetentionPool(t)
	q := store.New(pool)
	owner := createRetentionUser(ctx, t, q)
	t.Cleanup(func() { cleanupUsers(ctx, t, pool, owner) })
	cutoff := retentionNow.Add(-180 * 24 * time.Hour)
	for _, item := range []struct {
		slug     string
		released time.Time
	}{{"before", cutoff.Add(-time.Microsecond)}, {"at", cutoff}, {"after", cutoff.Add(time.Microsecond)}} {
		if _, err := pool.Exec(ctx, "INSERT INTO project_slug_holds(user_id,slug,released_at) VALUES($1,$2,$3)", owner, item.slug, item.released); err != nil {
			t.Fatal(err)
		}
	}
	w, err := New(Config{Pool: pool, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return retentionNow }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.Retain(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, "SELECT slug FROM project_slug_holds WHERE user_id=$1 ORDER BY slug", owner)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var slugs []string
	for rows.Next() {
		var slug string
		if err = rows.Scan(&slug); err != nil {
			t.Fatal(err)
		}
		slugs = append(slugs, slug)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(slugs) != 1 || slugs[0] != "after" {
		t.Fatalf("retained=%v", slugs)
	}
}
