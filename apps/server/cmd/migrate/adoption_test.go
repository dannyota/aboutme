package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

func TestFixedMigrationCommandsRejectInvalidCombinationsBeforeOutput(t *testing.T) {
	for _, command := range []string{"provision-sql", "adopt", "role=aboutme", "drop"} {
		t.Run(command, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://invalid:invalid@127.0.0.1:1/secret_database?sslmode=disable")
			t.Setenv("MIGRATION_IDENTITY", "local-aboutme")
			var output bytes.Buffer
			err := runCommand(command, false, &output)
			if err == nil {
				t.Fatal("invalid command succeeded")
			}
			if strings.Contains(err.Error(), "secret_database") || strings.Contains(err.Error(), "postgres://") {
				t.Fatalf("error exposed connection detail: %v", err)
			}
		})
	}
}

func TestMigrationCommandErrorHidesDatabaseDetail(t *testing.T) {
	tests := []struct {
		err  error
		want string
	}{
		{fmt.Errorf("driver secret-host SQLSTATE XX000: %w", migrations.ErrMigrationHistoryMissing), "migration database is uninitialized; run migrate provision"},
		{fmt.Errorf("catalog secret: %w", migrations.ErrHistoryAdoptionRequired), "migration history adoption is required; run migrate adopt-history-owner"},
		{fmt.Errorf("catalog secret: %w", migrations.ErrMigrationHistoryCorrupt), "migration state is corrupt; inspect the database catalog"},
		{errors.New("postgres://secret@host/database SQLSTATE 08006"), "migration operation failed"},
	}
	for _, test := range tests {
		if got := migrationCommandError(test.err).Error(); got != test.want {
			t.Errorf("migrationCommandError(%v)=%q want %q", test.err, got, test.want)
		}
	}
}
