package main

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/migrations"
)

func TestWithSessionDefaults(t *testing.T) {
	t.Parallel()

	// want maps each setting to the value the rewritten URL must carry;
	// options is the options parameter it must keep unchanged.
	tests := []struct {
		name    string
		dsn     string
		want    map[string]string
		options string
	}{
		{
			name: "url without query",
			dsn:  "postgres://migrator:secret@db.example:5433/aboutme",
			want: map[string]string{"statement_timeout": "0", "lock_timeout": "5s"},
		},
		{
			name: "url with query",
			dsn:  "postgres://migrator:secret@db.example:5433/aboutme?sslmode=disable",
			want: map[string]string{"statement_timeout": "0", "lock_timeout": "5s"},
		},
		{
			name: "url with a question mark in the password",
			dsn:  "postgresql://migrator:sec?ret@db.example:5433/aboutme",
			want: map[string]string{"statement_timeout": "0", "lock_timeout": "5s"},
		},
		{
			name: "explicit url parameter wins",
			dsn:  "postgres://migrator:secret@db.example:5433/aboutme?lock_timeout=30s",
			want: map[string]string{"statement_timeout": "0", "lock_timeout": "30s"},
		},
		{
			name:    "explicit options switch wins",
			dsn:     "postgres://migrator:secret@db.example:5433/aboutme?options=-c%20statement_timeout%3D10min",
			want:    map[string]string{"lock_timeout": "5s"},
			options: "-c statement_timeout=10min",
		},
		{
			name: "keyword/value",
			dsn:  "host=db.example port=5433 dbname=aboutme user=migrator password='sec ret'",
			want: map[string]string{"statement_timeout": "0", "lock_timeout": "5s"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			original, err := pgx.ParseConfig(tt.dsn)
			if err != nil {
				t.Fatalf("parse original: %v", err)
			}
			got, err := withSessionDefaults(tt.dsn)
			if err != nil {
				t.Fatalf("withSessionDefaults() error: %v", err)
			}
			config, err := pgx.ParseConfig(got)
			if err != nil {
				t.Fatalf("parse rewritten: %v", err)
			}
			if config.Host != original.Host || config.Port != original.Port || config.Database != original.Database ||
				config.User != original.User || config.Password != original.Password {
				t.Errorf("rewritten URL changed the connection target or login")
			}
			for name, want := range tt.want {
				if value := config.RuntimeParams[name]; value != want {
					t.Errorf("RuntimeParams[%q] = %q, want %q", name, value, want)
				}
			}
			if _, set := tt.want["statement_timeout"]; !set {
				if value, ok := config.RuntimeParams["statement_timeout"]; ok {
					t.Errorf("RuntimeParams[statement_timeout] = %q, want unset so options applies", value)
				}
			}
			if value := config.RuntimeParams["options"]; value != tt.options {
				t.Errorf("RuntimeParams[options] = %q, want %q", value, tt.options)
			}
		})
	}
}

func TestWithSessionDefaults_RejectsInvalidURL(t *testing.T) {
	t.Parallel()

	if _, err := withSessionDefaults("not a valid dsn ::: with spaces"); err == nil {
		t.Fatal("withSessionDefaults() error = nil, want error for an invalid URL")
	}
}

// TestMigrateConnection_SessionDefaults opens the database the way run does
// and checks the two session settings migrations run under.
func TestMigrateConnection_SessionDefaults(t *testing.T) {
	base := requireMigrateTestDatabaseURL(t)
	dsn, err := withSessionDefaults(newMigrateTestDatabase(t, base))
	if err != nil {
		t.Fatalf("withSessionDefaults() error: %v", err)
	}
	db, err := migrations.Open(dsn)
	if err != nil {
		t.Fatalf("migrations.Open() error: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := db.Close(); closeErr != nil {
			t.Logf("close database: %v", closeErr)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for name, want := range map[string]string{"statement_timeout": "0", "lock_timeout": "5s"} {
		var got string
		if err := db.QueryRowContext(ctx, `SHOW `+name).Scan(&got); err != nil {
			t.Fatalf("SHOW %s error: %v", name, err)
		}
		if got != want {
			t.Errorf("SHOW %s = %q, want %q", name, got, want)
		}
	}
}
