package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type registrationIdentity struct {
	replicaID, instanceID, containerARN, caddyARN, goARN, nuxtARN, release string
}

type registrationResult struct {
	replicaID, kind, state, controllerOperation, lifecycle string
	capacityGeneration, controllerGeneration               int64
	admission, replayed                                    bool
	desired, serving, maintenance                          *int16
	partition1, partition2                                 *bool
}

func validRegistrationIdentity(suffix string) registrationIdentity {
	return registrationIdentity{
		replicaID:    "11111111-1111-4111-8111-" + strings.Repeat(suffix, 12),
		instanceID:   "i-" + strings.Repeat(suffix, 17),
		containerARN: "arn:container:" + suffix,
		caddyARN:     "arn:task:caddy:" + suffix,
		goARN:        "arn:task:go:" + suffix,
		nuxtARN:      "arn:task:nuxt:" + suffix,
		release:      "sha256:" + strings.Repeat(suffix, 64),
	}
}

func registrationArgs(id registrationIdentity) string {
	return fmt.Sprintf("'%s','%s','%s','%s','%s','%s','%s'", id.replicaID, id.instanceID, id.containerARN, id.caddyARN, id.goARN, id.nuxtARN, id.release)
}

func callRegistration(t *testing.T, db *sql.DB, role, function string, id registrationIdentity) (result registrationResult, resultErr error) {
	return callRegistrationArgs(t, db, role, function, registrationArgs(id))
}

func callRegistrationArgs(t *testing.T, db *sql.DB, role, function, args string) (result registrationResult, resultErr error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return result, err
	}
	authenticated, transactionActive := false, false
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if transactionActive {
			_, rollbackErr := conn.ExecContext(cleanup, `ROLLBACK`)
			resultErr = errors.Join(resultErr, rollbackErr)
		}
		if authenticated {
			_, resetErr := conn.ExecContext(cleanup, `RESET ALL; RESET SESSION AUTHORIZATION`)
			resultErr = errors.Join(resultErr, resetErr)
		}
		resultErr = errors.Join(resultErr, conn.Close())
	}()
	if _, err = conn.ExecContext(ctx, `SET SESSION AUTHORIZATION `+role); err != nil {
		return result, err
	}
	authenticated = true
	if _, err = conn.ExecContext(ctx, `SET search_path=pg_temp,public`); err != nil {
		return result, err
	}
	if _, err = conn.ExecContext(ctx, `BEGIN`); err != nil {
		return result, err
	}
	transactionActive = true
	if _, err = conn.ExecContext(ctx, `SELECT public.runtime_enter_write()`); err != nil {
		return result, err
	}
	statement := fmt.Sprintf(`WITH result AS MATERIALIZED (SELECT public.%s(%s) AS r) SELECT (r).replica_id::text,(r).replica_kind,(r).state,(r).desired_replicas,(r).active_serving_replicas,(r).active_maintenance_replicas,(r).partition_1_enabled,(r).partition_2_enabled,(r).capacity_generation,(r).controller_generation,(r).controller_operation_id,(r).admission_enabled,(r).lifecycle_phase,(r).replayed FROM result`, function, args)
	if err = conn.QueryRowContext(ctx, statement).Scan(&result.replicaID, &result.kind, &result.state, &result.desired, &result.serving, &result.maintenance, &result.partition1, &result.partition2, &result.capacityGeneration, &result.controllerGeneration, &result.controllerOperation, &result.admission, &result.lifecycle, &result.replayed); err != nil {
		return result, err
	}
	if _, err = conn.ExecContext(ctx, `SELECT public.runtime_finish_write(); COMMIT`); err != nil {
		return result, err
	}
	transactionActive = false
	return result, nil
}

func requireRegistrationError(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatal("registration call succeeded")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code {
		t.Fatalf("registration SQLSTATE=%q want=%q: %v", pgErrCode(pgErr), code, err)
	}
}

func registrationRoleExec(t *testing.T, db *sql.DB, role, statement string) (resultErr error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	authenticated := false
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if authenticated {
			_, resetErr := conn.ExecContext(cleanup, `RESET SESSION AUTHORIZATION`)
			resultErr = errors.Join(resultErr, resetErr)
		}
		resultErr = errors.Join(resultErr, conn.Close())
	}()
	if _, err := conn.ExecContext(ctx, `SET SESSION AUTHORIZATION `+role); err != nil {
		return err
	}
	authenticated = true
	_, resultErr = conn.ExecContext(ctx, statement)
	return resultErr
}

func openRegistrationTx(t *testing.T, db *sql.DB, role string) (*sql.Conn, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `SET SESSION AUTHORIZATION `+role); err != nil {
		if closeErr := conn.Close(); closeErr != nil {
			t.Errorf("close failed registration setup: %v", closeErr)
		}
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `BEGIN`); err != nil {
		if _, resetErr := conn.ExecContext(ctx, `RESET SESSION AUTHORIZATION`); resetErr != nil {
			t.Errorf("reset failed registration setup: %v", resetErr)
		}
		if closeErr := conn.Close(); closeErr != nil {
			t.Errorf("close failed registration setup: %v", closeErr)
		}
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `SELECT public.runtime_enter_write()`); err != nil {
		if _, cleanupErr := conn.ExecContext(ctx, `ROLLBACK; RESET SESSION AUTHORIZATION`); cleanupErr != nil {
			t.Errorf("cleanup failed registration entry: %v", cleanupErr)
		}
		if closeErr := conn.Close(); closeErr != nil {
			t.Errorf("close failed registration entry: %v", closeErr)
		}
		t.Fatal(err)
	}
	var pid int
	if err := conn.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		if _, cleanupErr := conn.ExecContext(ctx, `ROLLBACK; RESET SESSION AUTHORIZATION`); cleanupErr != nil {
			t.Errorf("cleanup failed registration pid: %v", cleanupErr)
		}
		if closeErr := conn.Close(); closeErr != nil {
			t.Errorf("close failed registration pid: %v", closeErr)
		}
		t.Fatal(err)
	}
	return conn, pid
}

func finishRegistrationTx(conn *sql.Conn) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := conn.ExecContext(ctx, `SELECT public.runtime_finish_write(); COMMIT; RESET SESSION AUTHORIZATION`)
	return errors.Join(err, conn.Close())
}

func rollbackRegistrationTx(conn *sql.Conn) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := conn.ExecContext(ctx, `ROLLBACK; RESET SESSION AUTHORIZATION`)
	return errors.Join(err, conn.Close())
}
