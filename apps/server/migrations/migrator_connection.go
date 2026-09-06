package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/stdlib"

	"github.com/dannyota/aboutme/apps/server/internal/pgtransport"
)

type migrationBackend struct {
	capability *pgtransport.Capability
}

func captureMigrationBackend(conn *sql.Conn) (*migrationBackend, error) {
	capability, err := pgtransport.Capture(conn)
	if err != nil {
		return nil, fmt.Errorf("migrations: capture physical backend: %w", err)
	}
	return &migrationBackend{capability: capability}, nil
}

func (b *migrationBackend) retire(ctx context.Context, conn *sql.Conn) error {
	if b == nil || b.capability == nil {
		return errors.New("migrations: missing physical backend capability")
	}
	result := b.capability.Retire(ctx, conn)
	return result.Err
}

func (b *migrationBackend) releaseClean(conn *sql.Conn) error {
	if b == nil || b.capability == nil {
		return errors.New("migrations: missing physical backend capability")
	}
	return b.capability.ReleaseClean(conn)
}

func finalizeMigrationBackend(ctx context.Context, backend *migrationBackend, conn *sql.Conn, releaseClean bool) error {
	if releaseClean {
		releaseErr := backend.releaseClean(conn)
		if releaseErr == nil {
			return nil
		}
		retireErr := backend.retire(ctx, conn)
		logicalErr := conn.Close()
		if retireErr == nil && errors.Is(logicalErr, sql.ErrConnDone) {
			logicalErr = nil
		}
		return errors.Join(releaseErr, retireErr, logicalErr)
	}
	retireErr := backend.retire(ctx, conn)
	logicalErr := conn.Close()
	if retireErr == nil && errors.Is(logicalErr, sql.ErrConnDone) {
		logicalErr = nil
	}
	return errors.Join(retireErr, logicalErr)
}

func verifyMigrationDriver(conn *sql.Conn) error {
	if conn == nil {
		return errors.New("migrations: nil connection for driver admission")
	}
	return conn.Raw(func(driverConn any) error {
		stdlibConn, ok := driverConn.(*stdlib.Conn)
		if !ok || stdlibConn == nil {
			return fmt.Errorf("migrations: unsupported SQL driver connection %T", driverConn)
		}
		if stdlibConn.Conn() == nil {
			return errors.New("migrations: pgx driver connection is nil")
		}
		return nil
	})
}
