package store_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

func TestNewPool_TimeoutDefaults(t *testing.T) {
	t.Parallel()

	// want maps each timeout setting to the value NewPool must send, or ""
	// when NewPool must leave the key out.
	tests := []struct {
		name    string
		query   string
		want    map[string]string
		options string
	}{
		{
			name:  "defaults",
			query: "",
			want: map[string]string{
				"statement_timeout":                   "15s",
				"idle_in_transaction_session_timeout": "30s",
				"lock_timeout":                        "",
			},
		},
		{
			name:  "url parameter wins",
			query: "&statement_timeout=1min",
			want: map[string]string{
				"statement_timeout":                   "1min",
				"idle_in_transaction_session_timeout": "30s",
				"lock_timeout":                        "",
			},
		},
		{
			name:  "explicit lock_timeout is kept",
			query: "&lock_timeout=2s",
			want: map[string]string{
				"statement_timeout":                   "15s",
				"idle_in_transaction_session_timeout": "30s",
				"lock_timeout":                        "2s",
			},
		},
		{
			name:  "url parameter matches without case",
			query: "&Statement_Timeout=1min",
			want: map[string]string{
				"statement_timeout":                   "",
				"idle_in_transaction_session_timeout": "30s",
				"lock_timeout":                        "",
			},
		},
		{
			name:    "options plain switch wins",
			query:   "&options=-c%20idle_in_transaction_session_timeout%3D1min",
			options: "-c idle_in_transaction_session_timeout=1min",
			want: map[string]string{
				"statement_timeout":                   "15s",
				"idle_in_transaction_session_timeout": "",
				"lock_timeout":                        "",
			},
		},
		{
			name:    "options grouped switch wins",
			query:   "&options=-ecstatement_timeout%3D60000",
			options: "-ecstatement_timeout=60000",
			want: map[string]string{
				"statement_timeout":                   "",
				"idle_in_transaction_session_timeout": "30s",
				"lock_timeout":                        "",
			},
		},
		{
			name:    "options long form with dashes and case wins",
			query:   "&options=--Idle-In-Transaction-Session-Timeout%3D1min",
			options: "--Idle-In-Transaction-Session-Timeout=1min",
			want: map[string]string{
				"statement_timeout":                   "15s",
				"idle_in_transaction_session_timeout": "",
				"lock_timeout":                        "",
			},
		},
		{
			name:    "unrelated options value keeps both defaults",
			query:   "&options=-c%20search_path%3Dpublic",
			options: "-c search_path=public",
			want: map[string]string{
				"statement_timeout":                   "15s",
				"idle_in_transaction_session_timeout": "30s",
				"lock_timeout":                        "",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			pool, err := store.NewPool(ctx, unreachableDSN+tt.query)
			if err != nil {
				t.Fatalf("NewPool() unexpected error: %v", err)
			}
			defer pool.Close(ctx)

			params := pool.Config().ConnConfig.RuntimeParams
			for name, want := range tt.want {
				got, ok := params[name]
				if want == "" {
					if ok {
						t.Errorf("RuntimeParams[%q] = %q, want unset", name, got)
					}
					continue
				}
				if got != want {
					t.Errorf("RuntimeParams[%q] = %q, want %q", name, got, want)
				}
			}
			if got := params["options"]; got != tt.options {
				t.Errorf("RuntimeParams[options] = %q, want %q", got, tt.options)
			}
		})
	}
}

// TestPool_Integration_TimeoutDefaults checks the settings a pooled
// connection reports.
func TestPool_Integration_TimeoutDefaults(t *testing.T) {
	dsn := testutil.RequireTestDatabaseURL(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() error: %v", err)
	}
	defer pool.Close(ctx)

	for name, want := range map[string]string{
		"statement_timeout":                   "15s",
		"idle_in_transaction_session_timeout": "30s",
		"lock_timeout":                        "0",
	} {
		if got := showSetting(ctx, t, pool, name); got != want {
			t.Errorf("SHOW %s = %q, want %q", name, got, want)
		}
	}
}

// TestPool_Integration_SetLocalResetsAfterCommit checks that a transaction's
// SET LOCAL wins inside it and that the same connection is back at the
// default after commit.
func TestPool_Integration_SetLocalResetsAfterCommit(t *testing.T) {
	dsn := testutil.RequireTestDatabaseURL(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool() error: %v", err)
	}
	defer pool.Close(ctx)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire() error: %v", err)
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error: %v", err)
	}
	if _, execErr := tx.Exec(ctx, `SET LOCAL statement_timeout = '30s'`); execErr != nil {
		t.Fatalf("SET LOCAL error: %v", execErr)
	}
	var got string
	if scanErr := tx.QueryRow(ctx, `SHOW statement_timeout`).Scan(&got); scanErr != nil {
		t.Fatalf("SHOW in transaction error: %v", scanErr)
	}
	if got != "30s" {
		t.Errorf("SHOW statement_timeout after SET LOCAL = %q, want 30s", got)
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		t.Fatalf("Commit() error: %v", commitErr)
	}
	if scanErr := conn.QueryRow(ctx, `SHOW statement_timeout`).Scan(&got); scanErr != nil {
		t.Fatalf("SHOW after commit error: %v", scanErr)
	}
	if got != "15s" {
		t.Errorf("SHOW statement_timeout after commit = %q, want the 15s default", got)
	}
}

// TestPool_Integration_IdleTransactionEndsAndPoolRecovers leaves a
// transaction idle past a short idle timeout from the URL and checks that
// PostgreSQL ends the session and the one-connection pool serves the next
// query on a new connection.
func TestPool_Integration_IdleTransactionEndsAndPoolRecovers(t *testing.T) {
	dsn := testutil.RequireTestDatabaseURL(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := store.NewPool(ctx, dsn+urlParamSeparator(dsn)+"pool_max_conns=1&idle_in_transaction_session_timeout=200ms")
	if err != nil {
		t.Fatalf("NewPool() error: %v", err)
	}
	defer pool.Close(ctx)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error: %v", err)
	}
	var first int32
	if scanErr := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&first); scanErr != nil {
		t.Fatalf("first query error: %v", scanErr)
	}
	time.Sleep(time.Second)
	_, err = tx.Exec(ctx, `SELECT 1`)
	if err == nil || !tx.Conn().IsClosed() {
		t.Fatalf("query after idle timeout: err=%v closed=%v, want an error on a closed connection", err, tx.Conn().IsClosed())
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code != "25P03" {
		t.Errorf("idle timeout SQLSTATE = %s, want 25P03", pgErr.Code)
	}
	if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
		t.Logf("rollback on the ended session: %v", rollbackErr)
	}

	var next int32
	if scanErr := pool.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&next); scanErr != nil {
		t.Fatalf("query after recovery error: %v", scanErr)
	}
	if next == first {
		t.Errorf("next query ran on backend %d again, want a new connection", next)
	}
}

// TestPool_Integration_ExplicitLockTimeoutFailsLockWait sets a short
// lock_timeout through options, so the lock wait fails fast, and checks the
// URL's value reached the session.
func TestPool_Integration_ExplicitLockTimeoutFailsLockWait(t *testing.T) {
	dsn := testutil.RequireTestDatabaseURL(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	holder, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool(holder) error: %v", err)
	}
	defer holder.Close(ctx)
	waiter, err := store.NewPool(ctx, dsn+urlParamSeparator(dsn)+"options=-c%20lock_timeout%3D250ms")
	if err != nil {
		t.Fatalf("NewPool(waiter) error: %v", err)
	}
	defer waiter.Close(ctx)

	if got := showSetting(ctx, t, waiter, "lock_timeout"); got != "250ms" {
		t.Fatalf("SHOW lock_timeout = %q, want the explicit 250ms", got)
	}
	if got := showSetting(ctx, t, waiter, "statement_timeout"); got != "15s" {
		t.Errorf("SHOW statement_timeout = %q, want the 15s default", got)
	}

	release := holdAdvisoryLock(ctx, t, holder)
	defer release()

	_, err = waiter.Exec(ctx, `SELECT pg_advisory_xact_lock(`+testLockKey+`)`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "55P03" {
		t.Fatalf("lock wait error = %v, want SQLSTATE 55P03 (lock_not_available)", err)
	}
}

// TestPool_Integration_StatementTimeoutCutsLockWait checks that, with no
// lock_timeout, the statement timeout ends a lock wait. A short explicit
// statement_timeout keeps the test fast.
func TestPool_Integration_StatementTimeoutCutsLockWait(t *testing.T) {
	dsn := testutil.RequireTestDatabaseURL(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	holder, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPool(holder) error: %v", err)
	}
	defer holder.Close(ctx)
	waiter, err := store.NewPool(ctx, dsn+urlParamSeparator(dsn)+"statement_timeout=250ms")
	if err != nil {
		t.Fatalf("NewPool(waiter) error: %v", err)
	}
	defer waiter.Close(ctx)

	if got := showSetting(ctx, t, waiter, "lock_timeout"); got != "0" {
		t.Fatalf("SHOW lock_timeout = %q, want 0", got)
	}
	if got := showSetting(ctx, t, waiter, "statement_timeout"); got != "250ms" {
		t.Fatalf("SHOW statement_timeout = %q, want the explicit 250ms", got)
	}

	release := holdAdvisoryLock(ctx, t, holder)
	defer release()

	_, err = waiter.Exec(ctx, `SELECT pg_advisory_xact_lock(`+testLockKey+`)`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "57014" {
		t.Fatalf("lock wait error = %v, want SQLSTATE 57014 (query_canceled)", err)
	}
	if ctx.Err() != nil {
		t.Fatalf("test context ended first (%v); the cancel did not come from statement_timeout", ctx.Err())
	}
}

// testLockKey is the advisory lock the lock-wait tests contend for.
const testLockKey = `hashtextextended('aboutme.store-test.lock-timeout', 0)`

// holdAdvisoryLock takes testLockKey in a transaction on pool and returns
// the function that releases it. Callers defer it after the pools' Close, so
// it runs first and Close does not wait on the held connection.
func holdAdvisoryLock(ctx context.Context, t *testing.T, pool *store.Pool) func() {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error: %v", err)
	}
	release := func() {
		if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil {
			t.Logf("rollback: %v", rollbackErr)
		}
	}
	var held bool
	if holdErr := tx.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock(`+testLockKey+`)`).Scan(&held); holdErr != nil || !held {
		release()
		t.Fatalf("hold advisory lock: held=%v err=%v", held, holdErr)
	}
	return release
}

// showSetting runs SHOW for name, one of the fixed setting names above.
func showSetting(ctx context.Context, t *testing.T, pool *store.Pool, name string) string {
	t.Helper()
	var value string
	if err := pool.QueryRow(ctx, `SHOW `+name).Scan(&value); err != nil {
		t.Fatalf("SHOW %s error: %v", name, err)
	}
	return value
}

func urlParamSeparator(dsn string) string {
	if strings.Contains(dsn, "?") {
		return "&"
	}
	return "?"
}
