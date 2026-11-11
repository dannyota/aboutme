package migrations_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"danny.vn/aboutme/apps/server/migrations"
)

// TestOpen_SessionDefaults checks the session settings migrations run
// under, and that a value the URL sets is kept.
func TestOpen_SessionDefaults(t *testing.T) {
	t.Parallel()

	dsn := newTestDatabase(t)
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	for _, tc := range []struct {
		name string
		dsn  string
		want map[string]string
	}{
		{
			name: "defaults",
			dsn:  dsn,
			want: map[string]string{"statement_timeout": "0", "lock_timeout": "5s"},
		},
		{
			name: "url value wins",
			dsn:  dsn + separator + "lock_timeout=30s",
			want: map[string]string{"statement_timeout": "0", "lock_timeout": "30s"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := migrations.Open(tc.dsn)
			if err != nil {
				t.Fatalf("Open() error: %v", err)
			}
			t.Cleanup(func() {
				if closeErr := db.Close(); closeErr != nil {
					t.Errorf("close database: %v", closeErr)
				}
			})

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			for name, want := range tc.want {
				var got string
				if scanErr := db.QueryRowContext(ctx, `SHOW `+name).Scan(&got); scanErr != nil {
					t.Fatalf("SHOW %s error: %v", name, scanErr)
				}
				if got != want {
					t.Errorf("SHOW %s = %q, want %q", name, got, want)
				}
			}
		})
	}
}
