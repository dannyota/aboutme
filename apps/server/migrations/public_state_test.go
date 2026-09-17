package migrations_test

import (
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestPublicStateSeedIsSingletonAndPositive(t *testing.T) {
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
	// The exact seed value (1) only holds for a freshly migrated database;
	// against the shared database the generation has legitimately advanced,
	// so assert only positivity here.
	if generation < 1 {
		t.Fatalf("public_state.discovery_generation = %d, want positive", generation)
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

	var start int64
	if err := tx.QueryRow(ctx, `
		SELECT discovery_generation FROM public_state WHERE singleton = true
	`).Scan(&start); err != nil {
		t.Fatalf("read current public generation: %v", err)
	}
	for i := int64(1); i <= 2; i++ {
		want := start + i
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
