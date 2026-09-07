package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

func runtimeSharedRateDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	db := newMigratedTestDatabase(t, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return db, ctx
}

func beginRateWrite(t *testing.T, db *sql.DB, timeout time.Duration) (*sql.Conn, *sql.Tx) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	conn, err := db.Conn(ctx)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, `SET SESSION AUTHORIZATION aboutme_migrator`); err != nil {
		cancel()
		if closeErr := conn.Close(); closeErr != nil {
			t.Error(closeErr)
		}
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, `SELECT public.runtime_enter_migrator()`); err != nil {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, resetErr := conn.ExecContext(cleanup, `RESET SESSION AUTHORIZATION`); resetErr != nil {
			t.Error(resetErr)
		}
		cancel()
		if closeErr := conn.Close(); closeErr != nil {
			t.Error(closeErr)
		}
		t.Fatal(err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, exitErr := conn.ExecContext(cleanup, `SELECT public.runtime_exit_migrator()`); exitErr != nil {
			t.Error(exitErr)
		}
		if _, resetErr := conn.ExecContext(cleanup, `RESET SESSION AUTHORIZATION`); resetErr != nil {
			t.Error(resetErr)
		}
		cancel()
		if closeErr := conn.Close(); closeErr != nil {
			t.Error(closeErr)
		}
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `SELECT public.runtime_begin_migration_write('migration-100018'); SET LOCAL ROLE aboutme_runtime_owner`); err != nil {
		cancel()
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			t.Error(rollbackErr)
		}
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, exitErr := conn.ExecContext(cleanup, `SELECT public.runtime_exit_migrator()`); exitErr != nil {
			t.Error(exitErr)
		}
		if _, resetErr := conn.ExecContext(cleanup, `RESET SESSION AUTHORIZATION`); resetErr != nil {
			t.Error(resetErr)
		}
		if closeErr := conn.Close(); closeErr != nil {
			t.Error(closeErr)
		}
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			t.Error(rollbackErr)
		}
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, exitErr := conn.ExecContext(cleanup, `SELECT public.runtime_exit_migrator()`); exitErr != nil {
			t.Error(exitErr)
		}
		if _, resetErr := conn.ExecContext(cleanup, `RESET SESSION AUTHORIZATION`); resetErr != nil {
			t.Error(resetErr)
		}
		if closeErr := conn.Close(); closeErr != nil && !errors.Is(closeErr, sql.ErrConnDone) {
			t.Error(closeErr)
		}
		cancel()
	})
	return conn, tx
}

func rateBucketSQL(policy, digestByte string, partition int, overrides map[string]string) string {
	algorithm, token, refill, started, count, events := "token_bucket", "60000000", "transaction_timestamp()", "NULL", "NULL", "NULL"
	switch policy {
	case "password.login_failure_email", "oauth.failed_grant":
		algorithm, token, refill, count = "fixed_window", "NULL", "NULL", "0"
	case "resume.slug_change":
		algorithm, token, refill, count, events = "rolling_slug", "NULL", "NULL", "0", "ARRAY[]::timestamptz[]"
	}
	values := map[string]string{
		"policy_id": "'" + policy + "'", "key_digest": fmt.Sprintf("decode(repeat('%s',32),'hex')", digestByte),
		"partition": fmt.Sprint(partition), "algorithm": "'" + algorithm + "'", "last_seen": "transaction_timestamp()",
		"token_numerator": token, "refill_at": refill, "window_started_at": started, "count": count, "rolling_events": events,
	}
	for column, value := range overrides {
		values[column] = value
	}
	columns := []string{"policy_id", "key_digest", "partition", "algorithm", "last_seen", "token_numerator", "refill_at", "window_started_at", "count", "rolling_events"}
	ordered := make([]string, 0, len(columns))
	for _, column := range columns {
		ordered = append(ordered, values[column])
	}
	return `INSERT INTO public.shared_rate_buckets(` + joinRate(columns) + `) VALUES(` + joinRate(ordered) + `); UPDATE public.shared_rate_partitions SET active_keys=active_keys+1 WHERE policy_id='` + policy + `' AND partition=` + fmt.Sprint(partition)
}

func joinRate(values []string) string {
	result := ""
	for index, value := range values {
		if index > 0 {
			result += ","
		}
		result += value
	}
	return result
}

func pendingAttemptSQL(id, kind, digestByte string, partition int, overrides map[string]string) string {
	partitionSQL, digestSQL := fmt.Sprint(partition), fmt.Sprintf("decode(repeat('%s',32),'hex')", digestByte)
	if kind == "overflow" {
		partitionSQL, digestSQL = "NULL", "NULL"
	}
	values := map[string]string{
		"attempt_id": "'" + id + "'", "policy_id": "'oauth.failed_grant'", "client_id": "'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'",
		"bucket_kind": "'" + kind + "'", "partition": partitionSQL, "key_digest": digestSQL,
		"window_started_at": "transaction_timestamp()", "reserved_at": "transaction_timestamp()", "effective_until": "transaction_timestamp()+interval '15 minutes'",
		"state": "'pending'", "outcome": "NULL", "terminal_reason": "NULL", "terminal_at": "NULL",
	}
	for column, value := range overrides {
		values[column] = value
	}
	columns := []string{"attempt_id", "policy_id", "client_id", "bucket_kind", "partition", "key_digest", "window_started_at", "reserved_at", "effective_until", "state", "outcome", "terminal_reason", "terminal_at"}
	ordered := make([]string, 0, len(columns))
	for _, column := range columns {
		ordered = append(ordered, values[column])
	}
	return `INSERT INTO public.shared_admission_attempts(` + joinRate(columns) + `) VALUES(` + joinRate(ordered) + `)`
}

func storedPendingWindowOverrides(kind, digestByte string) map[string]string {
	window := `(SELECT window_started_at FROM public.shared_rate_overflow WHERE policy_id='oauth.failed_grant')`
	if kind == "private" {
		window = fmt.Sprintf(`(SELECT window_started_at FROM public.shared_rate_buckets WHERE policy_id='oauth.failed_grant' AND key_digest=decode(repeat('%s',32),'hex'))`, digestByte)
	}
	return map[string]string{
		"window_started_at": window,
		"reserved_at":       window,
		"effective_until":   window + `+interval '15 minutes'`,
	}
}

func requireRateError(t *testing.T, err error, code, constraint, column string) {
	t.Helper()
	requireMembershipPGError(t, err, code, constraint, column)
}
