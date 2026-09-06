//nolint:govet // Sequential migration cleanup keeps each error next to its operation.
package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

func applyFS(ctx context.Context, db *sql.DB, fsys fs.FS, identity MigrationIdentity, lockOpts ...lock.SessionLockerOption) ([]*goose.MigrationResult, error) {
	if db == nil || !identity.valid() {
		return nil, errors.New("migrations: invalid Apply arguments")
	}
	if err := validateProtectedMigrationSources(fsys); err != nil {
		return nil, err
	}
	state, err := readMigrationCatalogHint(ctx, db)
	if err != nil {
		return nil, err
	}
	if state.runtimeExists {
		if !state.historyExists {
			return nil, ErrMigrationHistoryCorrupt
		}
		if state.historyOwner == "aboutme" {
			return nil, ErrHistoryAdoptionRequired
		}
		return applyProtected(ctx, db, fsys, identity, lockOpts...)
	}
	if state.historyExists && state.historyOwner != "aboutme" && state.historyOwner != "aboutme_migrator" {
		return nil, ErrMigrationHistoryCorrupt
	}
	stayAdmin := state.historyExists && state.historyOwner == "aboutme"
	if stayAdmin && identity.kind != migrationIdentityLocalAdmin {
		return nil, ErrHistoryAdoptionRequired
	}
	wrapped, err := lock.NewPostgresSessionLocker(append([]lock.SessionLockerOption{lock.WithLockID(LockID)}, lockOpts...)...)
	if err != nil {
		return nil, fmt.Errorf("migrations: create bootstrap locker: %w", err)
	}
	wantOwner := state.historyOwner
	if wantOwner == "" {
		wantOwner = "aboutme_migrator"
	}
	locker := &bootstrapSessionLocker{identity: identity, wrapped: wrapped, stayAdmin: stayAdmin, allowAbsent: !state.historyExists, wantOwner: wantOwner}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, fsys, goose.WithDisableGlobalRegistry(true), goose.WithSessionLocker(locker))
	if err != nil {
		return nil, fmt.Errorf("migrations: create bootstrap provider: %w", err)
	}
	var results []*goose.MigrationResult
	for _, source := range provider.ListSources() {
		if source.Version > 13 {
			break
		}
		result, applyErr := provider.ApplyVersion(ctx, source.Version, true)
		if cleanBootstrapAlreadyApplied(applyErr, locker) {
			continue
		}
		if cleanBootstrapHandoff(applyErr, locker) {
			tracedState, stateErr := readMigrationCatalogHint(ctx, db)
			if stateErr == nil && tracedState.runtimeExists && tracedState.historyExists && tracedState.historyOwner != "aboutme" {
				protectedResults, protectedErr := applyProtected(ctx, db, fsys, identity, lockOpts...)
				return append(results, protectedResults...), protectedErr
			}
		}
		if applyErr != nil {
			return results, fmt.Errorf("migrations: bootstrap version %d: %w", source.Version, applyErr)
		}
		results = append(results, result)
	}
	if stayAdmin {
		return results, ErrHistoryAdoptionRequired
	}
	protectedResults, err := applyProtected(ctx, db, fsys, identity, lockOpts...)
	return append(results, protectedResults...), err
}

func applyProtected(ctx context.Context, db *sql.DB, fsys fs.FS, identity MigrationIdentity, lockOpts ...lock.SessionLockerOption) ([]*goose.MigrationResult, error) {
	wrapped, err := lock.NewPostgresSessionLocker(append([]lock.SessionLockerOption{lock.WithLockID(LockID)}, lockOpts...)...)
	if err != nil {
		return nil, fmt.Errorf("migrations: create protected Goose locker: %w", err)
	}
	locker, err := newRuntimeSessionLocker(identity, wrapped, func(ctx context.Context, conn *sql.Conn, _ int32) error {
		if err := validateProvisioning(ctx, conn); err != nil {
			return err
		}
		if err := validateProtectedHistory(ctx, conn, true, true); err != nil {
			return err
		}
		metadata, err := readLockedRuntimeMetadata(ctx, conn)
		if err != nil {
			return err
		}
		if metadata.enforcement == 0 {
			state, err := readMigrationCatalogState(ctx, conn)
			if err != nil {
				return err
			}
			_, err = validateManifestForProtectedApply(ctx, conn, state.historyOwner)
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, fsys, goose.WithDisableGlobalRegistry(true), goose.WithSessionLocker(locker))
	if err != nil {
		return nil, fmt.Errorf("migrations: create protected provider: %w", err)
	}
	var results []*goose.MigrationResult
	for _, source := range provider.ListSources() {
		if source.Version < 14 {
			continue
		}
		result, applyErr := provider.ApplyVersion(ctx, source.Version, true)
		if cleanProtectedAlreadyApplied(applyErr, locker) {
			continue
		}
		if applyErr != nil {
			return results, fmt.Errorf("migrations: apply protected version %d: %w", source.Version, applyErr)
		}
		results = append(results, result)
	}
	return results, nil
}

func cleanBootstrapAlreadyApplied(err error, locker *bootstrapSessionLocker) bool {
	return err != nil && locker != nil && locker.completedClean && errors.Unwrap(err) == goose.ErrAlreadyApplied //nolint:errorlint // The accepted Goose contract requires this exact one-wrapper shape.
}

func cleanBootstrapHandoff(err error, locker *bootstrapSessionLocker) bool {
	return err != nil && locker != nil && locker.handoffToken != nil && locker.completedClean && errors.Unwrap(err) == locker.handoffToken //nolint:errorlint // Cleanup siblings and nested wrappers must stop the handoff.
}

func cleanProtectedAlreadyApplied(err error, locker *runtimeSessionLocker) bool {
	return err != nil && locker != nil && locker.completedClean && errors.Unwrap(err) == goose.ErrAlreadyApplied //nolint:errorlint // The accepted Goose contract requires this exact one-wrapper shape.
}

func validateManifestForProtectedApply(ctx context.Context, conn *sql.Conn, owner string) (*version13ManifestSnapshot, error) {
	if owner != "aboutme_runtime_owner" {
		return validateVersion13Manifest(ctx, conn, owner)
	}
	if _, err := conn.ExecContext(ctx, `SET ROLE aboutme_runtime_owner`); err != nil {
		return nil, err
	}
	snapshot, validateErr := validateVersion13Manifest(ctx, conn, owner)
	if _, resetErr := conn.ExecContext(ctx, `RESET ROLE`); resetErr != nil {
		return nil, errors.Join(validateErr, resetErr)
	}
	return snapshot, validateErr
}
