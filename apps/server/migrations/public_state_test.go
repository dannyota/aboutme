package migrations_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

func TestPublicStateStartsAtOne(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	var singleton bool
	var generation int64
	if err := tx.QueryRow(ctx, `
		SELECT singleton, discovery_generation
		FROM public_state
	`).Scan(&singleton, &generation); err != nil {
		t.Fatalf("read public_state: %v", err)
	}
	if !singleton {
		t.Fatal("public_state.singleton = false, want true")
	}
	if generation != 1 {
		t.Fatalf("public_state.discovery_generation = %d, want 1", generation)
	}
}

func TestPublicStateEnforcesSingletonAndPositiveGeneration(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	tests := []struct {
		name           string
		write          func(pgx.Tx) error
		wantConstraint string
	}{
		{
			name: "second singleton row",
			write: func(sp pgx.Tx) error {
				_, err := sp.Exec(ctx, `
					INSERT INTO public_state (singleton, discovery_generation)
					VALUES (true, 2)
				`)
				return err
			},
			wantConstraint: "public_state_pkey",
		},
		{
			name: "false singleton",
			write: func(sp pgx.Tx) error {
				_, err := sp.Exec(ctx, `
					UPDATE public_state SET singleton = false
					WHERE singleton = true
				`)
				return err
			},
			wantConstraint: "public_state_singleton_check",
		},
		{
			name: "zero generation",
			write: func(sp pgx.Tx) error {
				_, err := sp.Exec(ctx, `
					UPDATE public_state SET discovery_generation = 0
					WHERE singleton = true
				`)
				return err
			},
			wantConstraint: "public_state_discovery_generation_positive_check",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := withSavepoint(ctx, t, tx, tt.write)
			requireConstraintViolation(t, err, tt.wantConstraint)
		})
	}
}

func TestPublicStateGenerationAdvancesMonotonically(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)

	for want := int64(2); want <= 3; want++ {
		var got int64
		if err := tx.QueryRow(ctx, `
			UPDATE public_state
			SET discovery_generation = discovery_generation + 1
			WHERE singleton = true
			RETURNING discovery_generation
		`).Scan(&got); err != nil {
			t.Fatalf("advance public generation to %d: %v", want, err)
		}
		if got != want {
			t.Fatalf("advanced generation = %d, want %d", got, want)
		}
	}
}

func TestPublicStateMigrationDownUp(t *testing.T) {
	t.Parallel()
	dsn := newTestDatabase(t)
	db := openTestDB(t, dsn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	provider, err := migrations.NewProvider(db, migrations.FS)
	if err != nil {
		t.Fatalf("NewProvider() error: %v", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatalf("Up() error: %v", err)
	}
	if _, err := provider.DownTo(ctx, 6); err != nil {
		t.Fatalf("DownTo(6) error: %v", err)
	}

	var relation *string
	if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.public_state')::text`).Scan(&relation); err != nil {
		t.Fatalf("probe public_state after down: %v", err)
	}
	if relation != nil {
		t.Fatalf("public_state relation after down = %q, want absent", *relation)
	}

	if _, err := provider.UpTo(ctx, 7); err != nil {
		t.Fatalf("UpTo(7) error: %v", err)
	}
	var generation int64
	if err := db.QueryRowContext(ctx, `
		SELECT discovery_generation FROM public_state WHERE singleton = true
	`).Scan(&generation); err != nil {
		t.Fatalf("read public_state after up: %v", err)
	}
	if generation != 1 {
		t.Fatalf("generation after down/up = %d, want 1", generation)
	}
}
