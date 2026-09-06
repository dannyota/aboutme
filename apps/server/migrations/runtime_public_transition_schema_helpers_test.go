package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

const (
	transitionID      = "12345678-1234-4234-8234-123456789abc"
	initiatorID       = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	initiatorInstance = "i-aaaaaaaaaaaaaaaaa"
	publishedDigest   = "34cd4cb37c1cf4467283a97904ce4170484ad2c335c277cb465403b9752ac68c"
)

func transitionFixture() []string {
	replica := replicaInsert(initiatorID, initiatorInstance, "a")
	return append(replica,
		fmt.Sprintf(`INSERT INTO public.public_transitions(transition_id,initiator_replica_id,initiator_instance_id,initiator_release_digest,operation,state,created_at,deadline_at,target_digest) VALUES('%s','%s','%s','sha256:%s','resume_publication','closing',transaction_timestamp(),transaction_timestamp()+interval '5 seconds',decode('%s','hex'))`, transitionID, initiatorID, initiatorInstance, strings.Repeat("a", 64), publishedDigest),
		fmt.Sprintf(`INSERT INTO public.public_transition_targets(transition_id,ordinal,kind,resume_id,expected_generation,class) VALUES('%s',0,'discovery',NULL,7,'revoking'),('%s',1,'resume','00112233-4455-6677-8899-aabbccddeeff',42,'non_draining')`, transitionID, transitionID),
		fmt.Sprintf(`INSERT INTO public.public_transition_replicas(transition_id,replica_id,snapshot_state,instance_id,release_digest,target_digest) VALUES('%s','%s','required','%s','sha256:%s',decode('%s','hex'))`, transitionID, initiatorID, initiatorInstance, strings.Repeat("a", 64), publishedDigest),
	)
}

func transitionWrite(t *testing.T, db *sql.DB, statements ...string) error {
	t.Helper()
	return membershipWrite(t, db, statements...)
}

func replaceTransitionFixture(t *testing.T, old, replacement string) []string {
	t.Helper()
	fixture := transitionFixture()
	joined := strings.Join(fixture, ";")
	if !strings.Contains(joined, old) {
		t.Fatalf("fixture does not contain %q", old)
	}
	return []string{strings.Replace(joined, old, replacement, 1)}
}

func secondReplicaFixture() []string {
	return replicaInsert("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", "i-bbbbbbbbbbbbbbbbb", "b")
}

func secondRequiredReplicaSQL() string {
	return fmt.Sprintf(`INSERT INTO public.public_transition_replicas(transition_id,replica_id,snapshot_state,instance_id,release_digest,target_digest) VALUES('%s','bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb','required','i-bbbbbbbbbbbbbbbbb','sha256:%s',decode('%s','hex'))`, transitionID, strings.Repeat("b", 64), publishedDigest)
}

func transitionAckSQL(replicaID string) string {
	return fmt.Sprintf(`INSERT INTO public.public_transition_acks(transition_id,replica_id,target_digest,acked_at,local_result,revoking_count,non_draining_count) VALUES('%s','%s',decode('%s','hex'),clock_timestamp(),'closed',1,1)`, transitionID, replicaID, publishedDigest)
}

func beginTransitionWrite(t *testing.T, db *sql.DB) (*sql.Conn, *sql.Tx) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	conn, err := db.Conn(ctx)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, `SET SESSION AUTHORIZATION aboutme_migrator`); err != nil {
		cancel()
		if closeErr := conn.Close(); closeErr != nil {
			t.Errorf("close failed transition connection: %v", closeErr)
		}
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(ctx, `SELECT public.runtime_enter_migrator()`); err != nil {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, resetErr := conn.ExecContext(cleanup, `RESET SESSION AUTHORIZATION`); resetErr != nil {
			t.Errorf("reset failed transition authorization: %v", resetErr)
		}
		cancel()
		if closeErr := conn.Close(); closeErr != nil {
			t.Errorf("close failed transition connection: %v", closeErr)
		}
		t.Fatal(err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if _, exitErr := conn.ExecContext(cleanup, `SELECT public.runtime_exit_migrator()`); exitErr != nil {
			t.Errorf("exit failed transition migrator: %v", exitErr)
		}
		if _, resetErr := conn.ExecContext(cleanup, `RESET SESSION AUTHORIZATION`); resetErr != nil {
			t.Errorf("reset failed transition authorization: %v", resetErr)
		}
		cancel()
		if closeErr := conn.Close(); closeErr != nil {
			t.Errorf("close failed transition connection: %v", closeErr)
		}
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `SELECT public.runtime_begin_migration_write('migration-100016'); SET LOCAL ROLE aboutme_runtime_owner`); err != nil {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			t.Errorf("rollback failed transition setup: %v", rollbackErr)
		}
		if _, exitErr := conn.ExecContext(cleanup, `SELECT public.runtime_exit_migrator()`); exitErr != nil {
			t.Errorf("exit failed transition migrator: %v", exitErr)
		}
		if _, resetErr := conn.ExecContext(cleanup, `RESET SESSION AUTHORIZATION`); resetErr != nil {
			t.Errorf("reset failed transition authorization: %v", resetErr)
		}
		cancel()
		if closeErr := conn.Close(); closeErr != nil {
			t.Errorf("close failed transition connection: %v", closeErr)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			t.Errorf("rollback transition transaction: %v", rollbackErr)
		}
		if _, exitErr := conn.ExecContext(cleanup, `SELECT public.runtime_exit_migrator()`); exitErr != nil {
			t.Errorf("exit transition migrator: %v", exitErr)
		}
		if _, resetErr := conn.ExecContext(cleanup, `RESET SESSION AUTHORIZATION`); resetErr != nil {
			t.Errorf("reset transition authorization: %v", resetErr)
		}
		if closeErr := conn.Close(); closeErr != nil {
			t.Errorf("close transition connection: %v", closeErr)
		}
	})
	return conn, tx
}

func rollbackTransitionWrite(t *testing.T, tx *sql.Tx) {
	t.Helper()
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		t.Fatal(err)
	}
}

func receiveTransitionRace(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("timed out joining transition race query")
		return nil
	}
}

func finishTransitionWrite(tx *sql.Tx) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := tx.ExecContext(ctx, `RESET ROLE; SELECT public.runtime_finish_write()`); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

func waitForTransitionCondition(db *sql.DB, statement string, args ...any) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		var ready bool
		if err := db.QueryRowContext(ctx, statement, args...).Scan(&ready); err != nil {
			return err
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for database lock: %w", ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func runtimeTransitionFixtureFS(t *testing.T, through int64) fstest.MapFS {
	t.Helper()
	result := fstest.MapFS{}
	for _, source := range migrationSourcesFromFSForTest(t, through) {
		result[source.name] = &fstest.MapFile{Data: source.data}
	}
	return result
}
