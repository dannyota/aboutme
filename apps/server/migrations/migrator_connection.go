package migrations

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
)

const migrationBackendRetirementTimeout = 5 * time.Second

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

func retireMigrationBackend(ctx context.Context, conn *sql.Conn) error {
	if conn == nil {
		return errors.New("migrations: nil connection for backend retirement")
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), migrationBackendRetirementTimeout)
	defer cancel()

	var callbackRan bool
	var closeErr error
	var invariantErr error
	rawErr := conn.Raw(func(driverConn any) error {
		callbackRan = true
		stdlibConn, ok := driverConn.(*stdlib.Conn)
		if !ok || stdlibConn == nil {
			invariantErr = fmt.Errorf("migrations: unsupported SQL driver connection %T during retirement", driverConn)
			return driver.ErrBadConn
		}
		pgxConn := stdlibConn.Conn()
		if pgxConn == nil {
			invariantErr = errors.New("migrations: pgx driver connection is nil during retirement")
			return driver.ErrBadConn
		}
		closeErr = pgxConn.Close(cleanupCtx)
		if !pgxConn.IsClosed() {
			invariantErr = errors.New("migrations: physical backend closure was not confirmed")
			return driver.ErrBadConn
		}
		return nil
	})
	if !callbackRan {
		return fmt.Errorf("migrations: retirement Raw callback unavailable: %w", rawErr)
	}
	if invariantErr != nil {
		return errors.Join(invariantErr, closeErr, rawErr)
	}
	if closeErr != nil {
		return fmt.Errorf("migrations: close physical backend: %w", closeErr)
	}
	if rawErr != nil {
		return fmt.Errorf("migrations: retire physical backend: %w", rawErr)
	}
	return nil
}
