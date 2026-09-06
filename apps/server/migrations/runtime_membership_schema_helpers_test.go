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

var runtimeMembershipTables = []string{
	"runtime_replicas",
	"runtime_replica_tasks",
	"runtime_capacity",
	"runtime_lifecycle_operations",
	"runtime_lifecycle_operation_steps",
	"runtime_termination_intents",
	"runtime_leave_receipts",
	"runtime_fencing_proofs",
}

func runtimeMembershipDB(t *testing.T) (*sql.DB, context.Context) {
	t.Helper()
	db := newCompositionTestDatabase(t)
	ctx := context.Background()
	if err := ProvisionDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, db, LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	return db, ctx
}

func membershipWrite(t *testing.T, db *sql.DB, statements ...string) (resultErr error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	authenticated, entered, transactionActive := false, false, false
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if transactionActive {
			_, rollbackErr := conn.ExecContext(cleanupCtx, `ROLLBACK`)
			resultErr = errors.Join(resultErr, rollbackErr)
		}
		if entered {
			_, exitErr := conn.ExecContext(cleanupCtx, `SELECT public.runtime_exit_migrator()`)
			resultErr = errors.Join(resultErr, exitErr)
		}
		if authenticated {
			_, resetErr := conn.ExecContext(cleanupCtx, `RESET SESSION AUTHORIZATION`)
			resultErr = errors.Join(resultErr, resetErr)
		}
		resultErr = errors.Join(resultErr, conn.Close())
	}()
	if _, authErr := conn.ExecContext(ctx, `SET SESSION AUTHORIZATION aboutme_migrator`); authErr != nil {
		return authErr
	}
	authenticated = true
	if _, enterErr := conn.ExecContext(ctx, `SELECT public.runtime_enter_migrator()`); enterErr != nil {
		return enterErr
	}
	entered = true
	if _, beginErr := conn.ExecContext(ctx, `BEGIN`); beginErr != nil {
		return beginErr
	}
	transactionActive = true
	if _, markerErr := conn.ExecContext(ctx, `SELECT public.runtime_begin_migration_write('migration-100015')`); markerErr != nil {
		return markerErr
	}
	if _, roleErr := conn.ExecContext(ctx, `SET LOCAL ROLE aboutme_runtime_owner`); roleErr != nil {
		return roleErr
	}
	for _, statement := range statements {
		if _, statementErr := conn.ExecContext(ctx, statement); statementErr != nil {
			return statementErr
		}
	}
	if _, finishErr := conn.ExecContext(ctx, `RESET ROLE; SELECT public.runtime_finish_write()`); finishErr != nil {
		return finishErr
	}
	_, err = conn.ExecContext(ctx, `COMMIT`)
	transactionActive = false
	return err
}

func replicaInsert(replicaID, instance, suffix string) []string {
	release := "sha256:" + strings.Repeat(suffix, 64)
	parent := fmt.Sprintf(`INSERT INTO public.runtime_replicas(replica_id,replica_kind,instance_id,container_instance_arn,caddy_task_arn,go_task_arn,nuxt_task_arn,release_digest,state,joined_at) VALUES('%s','serving','%s','arn:container:%s','arn:task:caddy:%s','arn:task:go:%s','arn:task:nuxt:%s','%s','joining',clock_timestamp())`, replicaID, instance, suffix, suffix, suffix, suffix, release)
	children := fmt.Sprintf(`INSERT INTO public.runtime_replica_tasks(task_arn,replica_id,task_role) VALUES('arn:task:caddy:%[1]s','%[2]s','caddy'),('arn:task:go:%[1]s','%[2]s','go'),('arn:task:nuxt:%[1]s','%[2]s','nuxt')`, suffix, replicaID)
	return []string{parent, children}
}

func lifecycleStepSQL(operation, workflow, action string, expected int64) string {
	return lifecycleStepSQLWith(operation, workflow, action, expected, nil)
}

func lifecycleStepSQLWith(operation, workflow, action string, expected int64, overrides map[string]string) string {
	capacityAction := action == "prepare_scale_out" || action == "begin_wake" || action == "complete_wake"
	finish := action == "finish_scale_in"
	replicaID, replicaKind, replicaState := "NULL", "NULL", "NULL"
	desired, serving, maintenance, partition1, partition2 := "NULL", "NULL", "NULL", "NULL", "NULL"
	resultKind := "replica"
	if capacityAction || finish {
		desired, serving, maintenance, partition1, partition2 = "1", "0", "0", "false", "false"
	}
	if capacityAction {
		resultKind = "capacity"
	} else {
		replicaID = "'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'"
		replicaKind = "'serving'"
		replicaState = "'active'"
		switch action {
		case "prepare_scale_in", "prepare_maintenance_drain":
			replicaState = "'draining'"
		case "begin_replica_termination":
			replicaState = "'terminating'"
		case "finish_scale_in":
			replicaState = "'left'"
		}
		if action == "prepare_maintenance_drain" {
			replicaKind = "'maintenance'"
		}
	}
	writeGate, writeGeneration := "NULL", "NULL"
	switch action {
	case "begin_wake":
		writeGate, writeGeneration = "'closing'", fmt.Sprint(expected+1)
	case "complete_wake":
		writeGate, writeGeneration = "'open'", fmt.Sprint(expected+1)
	}
	replaced := "NULL"
	if workflow == "replacement_serving" {
		replaced = "'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb'"
	}
	values := map[string]string{
		"operation_id": operation, "action": action, "workflow_kind": workflow,
		"expected_generation": fmt.Sprint(expected), "result_generation": fmt.Sprint(expected + 1),
		"argument_digest": "decode(repeat('00',32),'hex')", "result_digest": "decode(repeat('11',32),'hex')",
		"argument_replaced_replica_id": replaced, "result_kind": "'" + resultKind + "'",
		"replica_id": replicaID, "replica_kind": replicaKind, "replica_state": replicaState,
		"desired_replicas": desired, "active_serving_replicas": serving, "active_maintenance_replicas": maintenance,
		"partition_1_enabled": partition1, "partition_2_enabled": partition2,
		"capacity_generation": fmt.Sprint(expected + 1), "controller_generation": fmt.Sprint(expected + 1),
		"controller_operation_id": "'" + operation + "'", "admission_enabled": "true", "lifecycle_phase": "'online'",
		"write_gate": writeGate, "write_generation": writeGeneration,
	}
	values["operation_id"] = "'" + operation + "'"
	values["action"] = "'" + action + "'"
	values["workflow_kind"] = "'" + workflow + "'"
	for column, value := range overrides {
		values[column] = value
	}
	columns := []string{"operation_id", "action", "workflow_kind", "expected_generation", "result_generation", "argument_digest", "result_digest", "argument_replaced_replica_id", "result_kind", "replica_id", "replica_kind", "replica_state", "desired_replicas", "active_serving_replicas", "active_maintenance_replicas", "partition_1_enabled", "partition_2_enabled", "capacity_generation", "controller_generation", "controller_operation_id", "admission_enabled", "lifecycle_phase", "write_gate", "write_generation"}
	orderedValues := make([]string, 0, len(columns))
	for _, column := range columns {
		orderedValues = append(orderedValues, values[column])
	}
	return `INSERT INTO public.runtime_lifecycle_operation_steps(` + strings.Join(columns, ",") + `) VALUES(` + strings.Join(orderedValues, ",") + `)`
}

func requireMembershipFailure(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("invalid membership write succeeded")
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unexpected cancellation: %v", err)
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || (pgErr.Code != "23514" && pgErr.Code != "23503" && pgErr.Code != "23505" && pgErr.Code != "23502" && pgErr.Code != "55000") {
		t.Fatalf("unexpected membership failure: %v", err)
	}
}

func requireMembershipPGError(t *testing.T, err error, code, constraint, column string) {
	t.Helper()
	if err == nil {
		t.Fatal("invalid membership write succeeded")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code || pgErr.ConstraintName != constraint || pgErr.ColumnName != column {
		t.Fatalf("membership error code=%q constraint=%q column=%q: %v", pgErrCode(pgErr), pgErrConstraint(pgErr), pgErrColumn(pgErr), err)
	}
}

func pgErrCode(err *pgconn.PgError) string {
	if err == nil {
		return ""
	}
	return err.Code
}

func pgErrConstraint(err *pgconn.PgError) string {
	if err == nil {
		return ""
	}
	return err.ConstraintName
}

func pgErrColumn(err *pgconn.PgError) string {
	if err == nil {
		return ""
	}
	return err.ColumnName
}
