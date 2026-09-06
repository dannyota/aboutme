package migrations

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestRuntimeMembershipSchemaPreservesVersion14ApplicationData(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ProvisionDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := applyFS(ctx, db, enforcementFixtureFS(t), LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO public.users(id,email,name) VALUES('aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa','membership-preserve@example.test','preserved')`); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(ctx, db, LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := db.QueryRowContext(ctx, `SELECT name FROM public.users WHERE id='aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'`).Scan(&name); err != nil || name != "preserved" {
		t.Fatalf("preserved name=%q error=%v", name, err)
	}
}

func TestRuntimeMembershipSchemaObjectsExist(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	for _, name := range runtimeMembershipTables {
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.'||$1) IS NOT NULL`, name).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("table %s is missing", name)
		}
	}
}

func TestRuntimeMembershipSchemaSeedAndEmptyEvidence(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	var desired int16
	var generation, controller int64
	var operation, phase, updatedBy string
	var admission bool
	if err := db.QueryRowContext(ctx, `SELECT desired_replicas,generation,controller_generation,controller_operation_id,admission_enabled,lifecycle_phase,updated_by FROM public.runtime_capacity WHERE singleton`).Scan(&desired, &generation, &controller, &operation, &admission, &phase, &updatedBy); err != nil {
		t.Fatal(err)
	}
	if desired != 1 || generation != 1 || controller != 1 || operation != "bootstrap-uncomposed-v1" || !admission || phase != "online" || updatedBy != operation {
		t.Fatalf("capacity seed=%d/%d/%d/%q/%t/%q/%q", desired, generation, controller, operation, admission, phase, updatedBy)
	}
	for _, table := range []string{"runtime_replicas", "runtime_lifecycle_operations", "runtime_termination_intents", "runtime_leave_receipts", "runtime_fencing_proofs"} {
		var count int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM public.`+table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count=%d error=%v", table, count, err)
		}
	}
}

func TestRuntimeMembershipSchemaTaskTrioAndIdentity(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	valid := replicaInsert("11111111-1111-4111-8111-111111111111", "i-11111111111111111", "a")
	if err := membershipWrite(t, db, valid...); err != nil {
		t.Fatal(err)
	}
	missing := replicaInsert("22222222-2222-4222-8222-222222222222", "i-22222222222222222", "a")
	var generationBefore, generationAfter int64
	if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&generationBefore); err != nil {
		t.Fatal(err)
	}
	requireMembershipFailure(t, membershipWrite(t, db, missing[0]))
	if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&generationAfter); err != nil {
		t.Fatal(err)
	}
	if generationAfter != generationBefore {
		t.Fatalf("failed deferred trio advanced write generation %d -> %d", generationBefore, generationAfter)
	}
	sameBuild := replicaInsert("33333333-3333-4333-8333-333333333333", "i-33333333333333333", "b")
	sameBuild[0] = strings.Replace(sameBuild[0], "sha256:"+strings.Repeat("b", 64), "sha256:"+strings.Repeat("a", 64), 1)
	if err := membershipWrite(t, db, sameBuild...); err != nil {
		t.Fatalf("same release build rejected: %v", err)
	}
	reusedTask := replicaInsert("44444444-4444-4444-8444-444444444444", "i-44444444444444444", "b")
	reusedTask[1] = `INSERT INTO public.runtime_replica_tasks(task_arn,replica_id,task_role) VALUES('arn:task:caddy:a','44444444-4444-4444-8444-444444444444','caddy'),('arn:task:go:b','44444444-4444-4444-8444-444444444444','go'),('arn:task:nuxt:b','44444444-4444-4444-8444-444444444444','nuxt')`
	requireMembershipFailure(t, membershipWrite(t, db, reusedTask...))
	requireMembershipFailure(t, membershipWrite(t, db, `UPDATE public.runtime_replicas SET instance_id='i-55555555555555555' WHERE replica_id='11111111-1111-4111-8111-111111111111'`))
}

func TestRuntimeMembershipSchemaEvidenceImmutableAndTupleBound(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	valid := replicaInsert("11111111-1111-4111-8111-111111111111", "i-11111111111111111", "a")
	if err := membershipWrite(t, db, valid...); err != nil {
		t.Fatal(err)
	}
	badTuple := `INSERT INTO public.runtime_fencing_proofs(replica_id,instance_id,release_digest,adapter,evidence_id,requested_at,observed_terminated_at,observed_state) VALUES('11111111-1111-4111-8111-111111111111','i-99999999999999999','sha256:` + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + `','ec2_terminated_v1','evidence-1',transaction_timestamp(),transaction_timestamp(),'terminated')`
	requireMembershipFailure(t, membershipWrite(t, db, badTuple))
	proof := `INSERT INTO public.runtime_fencing_proofs(replica_id,instance_id,release_digest,adapter,evidence_id,requested_at,observed_terminated_at,observed_state) VALUES('11111111-1111-4111-8111-111111111111','i-11111111111111111','sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa','ec2_terminated_v1','evidence-1',transaction_timestamp(),transaction_timestamp(),'terminated')`
	if err := membershipWrite(t, db, proof); err != nil {
		t.Fatal(err)
	}
	second := replicaInsert("22222222-2222-4222-8222-222222222222", "i-22222222222222222", "b")
	futureObservation := `INSERT INTO public.runtime_fencing_proofs(replica_id,instance_id,release_digest,adapter,evidence_id,requested_at,observed_terminated_at,observed_state) VALUES('22222222-2222-4222-8222-222222222222','i-22222222222222222','sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','ec2_terminated_v1','evidence-future',transaction_timestamp(),transaction_timestamp()+interval '1 hour','terminated')`
	if err := membershipWrite(t, db, append(second, futureObservation)...); err != nil {
		t.Fatalf("future external observation rejected by database clock: %v", err)
	}
	requireMembershipFailure(t, membershipWrite(t, db, `UPDATE public.runtime_fencing_proofs SET evidence_id='changed' WHERE evidence_id='evidence-1'`))
	requireMembershipFailure(t, membershipWrite(t, db, `DELETE FROM public.runtime_fencing_proofs`))
	requireMembershipPGError(t, membershipWrite(t, db, `TRUNCATE public.runtime_fencing_proofs CASCADE`), "55000", "", "")
}

func TestRuntimeMembershipSchemaLifecycleShapeAndPredecessor(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	parent := `INSERT INTO public.runtime_lifecycle_operations(operation_id,workflow_kind) VALUES('scale-out-1','scale_out')`
	badAction := `INSERT INTO public.runtime_lifecycle_operation_steps(operation_id,action,workflow_kind,expected_generation,result_generation,argument_digest,result_digest,result_kind,capacity_generation,controller_generation,controller_operation_id,admission_enabled,lifecycle_phase) VALUES('scale-out-1','activate_replica_capacity','scale_out',1,2,decode(repeat('00',32),'hex'),decode(repeat('11',32),'hex'),'replica',2,2,'scale-out-1',true,'online')`
	requireMembershipFailure(t, membershipWrite(t, db, parent, badAction))
	missingCapacity := `INSERT INTO public.runtime_lifecycle_operation_steps(operation_id,action,workflow_kind,expected_generation,result_generation,argument_digest,result_digest,result_kind,capacity_generation,controller_generation,controller_operation_id,admission_enabled,lifecycle_phase) VALUES('scale-out-1','prepare_scale_out','scale_out',1,2,decode(repeat('00',32),'hex'),decode(repeat('11',32),'hex'),'capacity',2,2,'scale-out-1',true,'online')`
	requireMembershipFailure(t, membershipWrite(t, db, parent, missingCapacity))
}

func TestRuntimeMembershipSchemaEveryLifecycleActionAndInterleavedPredecessor(t *testing.T) {
	tests := []struct {
		workflow string
		actions  []struct {
			name     string
			expected int64
		}
	}{
		{workflow: "initial_serving", actions: []struct {
			name     string
			expected int64
		}{{"activate_replica_capacity", 1}}},
		{workflow: "scale_out", actions: []struct {
			name     string
			expected int64
		}{{"prepare_scale_out", 1}, {"activate_replica_capacity", 2}}},
		{workflow: "replacement_serving", actions: []struct {
			name     string
			expected int64
		}{{"activate_replica_capacity", 1}}},
		{workflow: "scale_in", actions: []struct {
			name     string
			expected int64
		}{{"prepare_scale_in", 1}, {"finish_scale_in", 3}}},
		{workflow: "uat_serving_wake", actions: []struct {
			name     string
			expected int64
		}{{"begin_wake", 1}, {"complete_wake", 2}, {"activate_replica_capacity", 3}}},
		{workflow: "maintenance_wake", actions: []struct {
			name     string
			expected int64
		}{{"begin_wake", 1}, {"complete_wake", 2}, {"activate_replica_capacity", 3}, {"prepare_maintenance_drain", 4}}},
		{workflow: "replica_termination", actions: []struct {
			name     string
			expected int64
		}{{"begin_replica_termination", 1}}},
	}
	for i, test := range tests {
		t.Run(test.workflow, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			operation := fmt.Sprintf("operation-%d", i)
			statements := []string{fmt.Sprintf(`INSERT INTO public.runtime_lifecycle_operations(operation_id,workflow_kind) VALUES('%s','%s')`, operation, test.workflow)}
			for _, action := range test.actions {
				statements = append(statements, lifecycleStepSQL(operation, test.workflow, action.name, action.expected))
			}
			if err := membershipWrite(t, db, statements...); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRuntimeMembershipSchemaReplacementPredecessorIsOneUse(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	first := []string{
		`INSERT INTO public.runtime_lifecycle_operations(operation_id,workflow_kind) VALUES('replacement-1','replacement_serving')`,
		lifecycleStepSQL("replacement-1", "replacement_serving", "activate_replica_capacity", 1),
	}
	if err := membershipWrite(t, db, first...); err != nil {
		t.Fatal(err)
	}
	second := []string{
		`INSERT INTO public.runtime_lifecycle_operations(operation_id,workflow_kind) VALUES('replacement-2','replacement_serving')`,
		lifecycleStepSQL("replacement-2", "replacement_serving", "activate_replica_capacity", 2),
	}
	requireMembershipFailure(t, membershipWrite(t, db, second...))
}

func TestRuntimeMembershipSchemaLifecycleNullableResultMatrix(t *testing.T) {
	tests := []struct {
		name, workflow, action string
		prefix                 []string
		expected               int64
	}{
		{"initial_activate", "initial_serving", "activate_replica_capacity", nil, 1},
		{"replacement_activate", "replacement_serving", "activate_replica_capacity", nil, 1},
		{"prepare_out", "scale_out", "prepare_scale_out", nil, 1},
		{"prepare_in", "scale_in", "prepare_scale_in", nil, 1},
		{"finish_in", "scale_in", "finish_scale_in", []string{"prepare_scale_in"}, 3},
		{"maintenance_drain", "maintenance_wake", "prepare_maintenance_drain", []string{"begin_wake", "complete_wake", "activate_replica_capacity"}, 4},
		{"terminate", "replica_termination", "begin_replica_termination", nil, 1},
		{"begin_wake", "uat_serving_wake", "begin_wake", nil, 1},
		{"complete_wake", "uat_serving_wake", "complete_wake", []string{"begin_wake"}, 2},
	}
	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := fmt.Sprintf("shape-%d", i)
			base := []string{fmt.Sprintf(`INSERT INTO public.runtime_lifecycle_operations(operation_id,workflow_kind) VALUES('%s','%s')`, operation, test.workflow)}
			for predecessorIndex, predecessor := range test.prefix {
				base = append(base, lifecycleStepSQL(operation, test.workflow, predecessor, int64(predecessorIndex+1)))
			}
			required := []string{"capacity_generation", "controller_generation", "controller_operation_id", "admission_enabled", "lifecycle_phase"}
			forbidden := map[string]string{}
			if test.action == "prepare_scale_out" || test.action == "begin_wake" || test.action == "complete_wake" {
				required = append(required, "desired_replicas", "active_serving_replicas", "active_maintenance_replicas", "partition_1_enabled", "partition_2_enabled")
				forbidden["replica_id"], forbidden["replica_kind"], forbidden["replica_state"] = "'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'", "'serving'", "'active'"
			} else {
				required = append(required, "replica_id", "replica_kind", "replica_state")
				if test.action == "finish_scale_in" {
					required = append(required, "desired_replicas", "active_serving_replicas", "active_maintenance_replicas", "partition_1_enabled", "partition_2_enabled")
				} else {
					forbidden["desired_replicas"], forbidden["active_serving_replicas"], forbidden["active_maintenance_replicas"] = "1", "0", "0"
					forbidden["partition_1_enabled"], forbidden["partition_2_enabled"] = "false", "false"
				}
			}
			if test.action == "begin_wake" || test.action == "complete_wake" {
				required = append(required, "write_gate", "write_generation")
			} else {
				forbidden["write_gate"], forbidden["write_generation"] = "'open'", "2"
			}
			if test.workflow == "replacement_serving" {
				required = append(required, "argument_replaced_replica_id")
			} else {
				forbidden["argument_replaced_replica_id"] = "'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb'"
			}
			db, _ := runtimeMembershipDB(t)
			for _, field := range required {
				err := membershipWrite(t, db, append(base, lifecycleStepSQLWith(operation, test.workflow, test.action, test.expected, map[string]string{field: "NULL"}))...)
				code, constraint, column := "23514", "runtime_lifecycle_steps_result_shape_check", ""
				switch field {
				case "capacity_generation", "controller_generation", "controller_operation_id", "admission_enabled", "lifecycle_phase":
					code, constraint, column = "23502", "", field
				case "write_gate", "write_generation":
					constraint = "runtime_lifecycle_steps_write_shape_check"
				case "argument_replaced_replica_id":
					constraint = "runtime_lifecycle_steps_replaced_shape_check"
				}
				requireMembershipPGError(t, err, code, constraint, column)
			}
			for field, value := range forbidden {
				err := membershipWrite(t, db, append(base, lifecycleStepSQLWith(operation, test.workflow, test.action, test.expected, map[string]string{field: value}))...)
				constraint := "runtime_lifecycle_steps_result_shape_check"
				switch field {
				case "write_gate", "write_generation":
					constraint = "runtime_lifecycle_steps_write_shape_check"
				case "argument_replaced_replica_id":
					constraint = "runtime_lifecycle_steps_replaced_shape_check"
				}
				requireMembershipPGError(t, err, "23514", constraint, "")
			}
		})
	}
}

func TestRuntimeMembershipSchemaNonWakeRejectsWriteGenerationWithoutGate(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	operation := "nonwake-write-generation"
	parent := `INSERT INTO public.runtime_lifecycle_operations(operation_id,workflow_kind) VALUES('nonwake-write-generation','initial_serving')`
	invalid := strings.Replace(lifecycleStepSQL(operation, "initial_serving", "activate_replica_capacity", 1), ",NULL,NULL)", ",NULL,2)", 1)
	requireMembershipPGError(t, membershipWrite(t, db, parent, invalid), "23514", "runtime_lifecycle_steps_write_shape_check", "")
}

func TestRuntimeMembershipSchemaTrioMismatchAndExtra(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	fixture := replicaInsert("55555555-5555-4555-8555-555555555555", "i-55555555555555555", "c")
	mismatch := strings.Replace(fixture[1], "arn:task:nuxt:c", "arn:task:wrong:c", 1)
	requireMembershipFailure(t, membershipWrite(t, db, fixture[0], mismatch))
	extra := fixture[1] + `; INSERT INTO public.runtime_replica_tasks(task_arn,replica_id,task_role) VALUES('arn:task:extra:c','55555555-5555-4555-8555-555555555555','go')`
	requireMembershipFailure(t, membershipWrite(t, db, fixture[0], extra))
}

func TestRuntimeMembershipSchemaReplicaBoundsAndTerminalReopen(t *testing.T) {
	tests := []struct {
		name string
		edit func([]string)
	}{
		{"zero_uuid", func(s []string) {
			s[0] = strings.Replace(s[0], "66666666-6666-4666-8666-666666666666", "00000000-0000-0000-0000-000000000000", 1)
			s[1] = strings.ReplaceAll(s[1], "66666666-6666-4666-8666-666666666666", "00000000-0000-0000-0000-000000000000")
		}},
		{"bad_instance", func(s []string) { s[0] = strings.Replace(s[0], "i-66666666666666666", "I-66666666666666666", 1) }},
		{"bad_arn", func(s []string) { s[0] = strings.Replace(s[0], "arn:container:d", "arn container d", 1) }},
		{"bad_release", func(s []string) {
			s[0] = strings.Replace(s[0], "sha256:"+strings.Repeat("d", 64), "SHA256:"+strings.Repeat("d", 64), 1)
		}},
		{"bad_state", func(s []string) { s[0] = strings.Replace(s[0], "'joining'", "'unknown'", 1) }},
		{"active_missing_timestamps", func(s []string) { s[0] = strings.Replace(s[0], "'joining'", "'active'", 1) }},
		{"timestamp_reverse", func(s []string) {
			s[0] = strings.Replace(s[0], "state,joined_at) VALUES", "state,joined_at,join_ready_at,activated_at) VALUES", 1)
			s[0] = strings.Replace(s[0], "'joining',clock_timestamp())", "'active',clock_timestamp(),clock_timestamp()-interval '1 second',clock_timestamp()-interval '1 second')", 1)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			fixture := replicaInsert("66666666-6666-4666-8666-666666666666", "i-66666666666666666", "d")
			test.edit(fixture)
			requireMembershipFailure(t, membershipWrite(t, db, fixture...))
		})
	}
	db, _ := runtimeMembershipDB(t)
	fixture := replicaInsert("77777777-7777-4777-8777-777777777777", "i-77777777777777777", "e")
	if err := membershipWrite(t, db, append(fixture, `UPDATE public.runtime_replicas SET state='fenced',fenced_at=clock_timestamp() WHERE replica_id='77777777-7777-4777-8777-777777777777'`)...); err != nil {
		t.Fatal(err)
	}
	requireMembershipFailure(t, membershipWrite(t, db, `UPDATE public.runtime_replicas SET state='joining',fenced_at=NULL WHERE replica_id='77777777-7777-4777-8777-777777777777'`))
}

func TestRuntimeMembershipSchemaTerminationReasonMatchesReplicaState(t *testing.T) {
	cases := []struct {
		name, state, reason string
		transitions         []string
	}{
		{"startup", "joining", "startup_failed", nil},
		{"readiness", "active", "readiness_failed", []string{`UPDATE public.runtime_replicas SET state='active',join_ready_at=clock_timestamp(),activated_at=clock_timestamp() WHERE replica_id='aaaaaaaa-1111-4111-8111-111111111111'`}},
		{"drain", "draining", "drain_failed", []string{`UPDATE public.runtime_replicas SET state='active',join_ready_at=clock_timestamp(),activated_at=clock_timestamp() WHERE replica_id='aaaaaaaa-1111-4111-8111-111111111111'`, `UPDATE public.runtime_replicas SET state='draining',draining_at=clock_timestamp() WHERE replica_id='aaaaaaaa-1111-4111-8111-111111111111'`}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			fixture := replicaInsert("aaaaaaaa-1111-4111-8111-111111111111", "i-11111111111111112", "9")
			statements := append(fixture, test.transitions...)
			intent := fmt.Sprintf(`INSERT INTO public.runtime_termination_intents(replica_id,instance_id,release_digest,request_id,reason,requested_at) VALUES('aaaaaaaa-1111-4111-8111-111111111111','i-11111111111111112','sha256:%s','request-%s','%s',clock_timestamp())`, strings.Repeat("9", 64), test.name, test.reason)
			if err := membershipWrite(t, db, append(statements, intent)...); err != nil {
				t.Fatalf("valid %s intent in %s: %v", test.reason, test.state, err)
			}
		})
	}
	db, _ := runtimeMembershipDB(t)
	fixture := replicaInsert("bbbbbbbb-1111-4111-8111-111111111111", "i-11111111111111113", "8")
	invalid := fmt.Sprintf(`INSERT INTO public.runtime_termination_intents(replica_id,instance_id,release_digest,request_id,reason,requested_at) VALUES('bbbbbbbb-1111-4111-8111-111111111111','i-11111111111111113','sha256:%s','request-invalid','readiness_failed',clock_timestamp())`, strings.Repeat("8", 64))
	requireMembershipFailure(t, membershipWrite(t, db, append(fixture, invalid)...))
}

func TestRuntimeMembershipSchemaMissingEntryAndHostileSearchPath(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	if _, err = conn.ExecContext(ctx, `SET SESSION AUTHORIZATION aboutme_runtime_owner`); err != nil {
		t.Fatal(err)
	}
	_, err = conn.ExecContext(ctx, `UPDATE public.runtime_capacity SET updated_at=clock_timestamp()`)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "AM001" {
		t.Fatalf("missing entry error=%v", err)
	}
	if _, err = conn.ExecContext(ctx, `RESET SESSION AUTHORIZATION`); err != nil {
		t.Fatal(err)
	}
	if err = membershipWrite(t, db, `SET LOCAL search_path=pg_temp,public`, `UPDATE public.runtime_capacity SET updated_at=clock_timestamp(),updated_by='hostile-path' WHERE singleton`); err != nil {
		t.Fatalf("qualified triggers failed with hostile search path: %v", err)
	}
}

func TestRuntimeMembershipSchemaConcurrentIdentityReuseHasOneWinner(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, fixture := range [][]string{
		replicaInsert("11111111-1111-4111-8111-111111111111", "i-11111111111111111", "a"),
		replicaInsert("22222222-2222-4222-8222-222222222222", "i-11111111111111111", "b"),
	} {
		fixture := fixture
		go func() {
			<-start
			results <- membershipWrite(t, db, fixture...)
		}()
	}
	close(start)
	winners := 0
	for range 2 {
		if err := <-results; err == nil {
			winners++
		} else {
			requireMembershipFailure(t, err)
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent nonterminal instance winners=%d", winners)
	}
}

func TestRuntimeMembershipSchemaConcurrentCrossRoleTaskReuseHasOneWinner(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	first := replicaInsert("88888888-8888-4888-8888-888888888888", "i-88888888888888888", "f")
	second := replicaInsert("99999999-9999-4999-8999-999999999999", "i-99999999999999999", "0")
	first[0] = strings.Replace(first[0], "arn:task:caddy:f", "arn:task:shared", 1)
	first[1] = strings.Replace(first[1], "arn:task:caddy:f", "arn:task:shared", 1)
	second[0] = strings.Replace(second[0], "arn:task:go:0", "arn:task:shared", 1)
	second[1] = strings.Replace(second[1], "arn:task:go:0", "arn:task:shared", 1)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, fixture := range [][]string{first, second} {
		fixture := fixture
		go func() {
			<-start
			results <- membershipWrite(t, db, fixture...)
		}()
	}
	close(start)
	winners := 0
	for range 2 {
		if err := <-results; err == nil {
			winners++
		} else {
			requireMembershipFailure(t, err)
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent cross-role task winners=%d", winners)
	}
}

func TestRuntimeMembershipSchemaPrivilegesAndCatalog(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, resetErr := conn.ExecContext(cleanupCtx, `RESET SESSION AUTHORIZATION`); resetErr != nil {
			t.Error(resetErr)
		}
		if closeErr := conn.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	if _, err := conn.ExecContext(ctx, `SET SESSION AUTHORIZATION aboutme_app`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM public.runtime_capacity`).Scan(&count); err != nil {
		t.Fatalf("app capacity SELECT: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO public.runtime_capacity(singleton,desired_replicas,generation,controller_generation,controller_operation_id,admission_enabled,lifecycle_phase,updated_at,updated_by) VALUES(false,1,1,1,'x',true,'online',now(),'x')`); err == nil {
		t.Fatal("app direct capacity INSERT succeeded")
	}
	var helperExecute bool
	if err := conn.QueryRowContext(ctx, `SELECT has_function_privilege(current_user,'public.runtime_validate_replica_change()','EXECUTE')`).Scan(&helperExecute); err != nil || helperExecute {
		t.Fatalf("helper execute=%t error=%v", helperExecute, err)
	}
	if _, err := conn.ExecContext(ctx, `RESET SESSION AUTHORIZATION`); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"aboutme_maintenance", "aboutme_lifecycle_command", "aboutme_fencing_proof", "aboutme_restore_verify"} {
		if _, err := conn.ExecContext(ctx, `SET SESSION AUTHORIZATION `+role); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.ExecContext(ctx, `SELECT * FROM public.runtime_capacity`); err == nil {
			t.Fatalf("%s read runtime_capacity", role)
		}
		if _, err := conn.ExecContext(ctx, `DELETE FROM public.runtime_capacity`); err == nil {
			t.Fatalf("%s deleted runtime_capacity", role)
		}
		if _, err := conn.ExecContext(ctx, `RESET SESSION AUTHORIZATION`); err != nil {
			t.Fatal(err)
		}
	}
	appReadable := map[string]bool{
		"runtime_capacity":            true,
		"runtime_termination_intents": true,
		"runtime_leave_receipts":      true,
		"runtime_fencing_proofs":      true,
	}
	for _, role := range []string{"aboutme_app", "aboutme_maintenance", "aboutme_lifecycle_command", "aboutme_fencing_proof", "aboutme_restore_verify"} {
		if _, err := conn.ExecContext(ctx, `SET SESSION AUTHORIZATION `+role); err != nil {
			t.Fatal(err)
		}
		for _, table := range runtimeMembershipTables {
			_, selectErr := conn.ExecContext(ctx, `SELECT * FROM public.`+table)
			wantSelect := role == "aboutme_app" && appReadable[table]
			if (selectErr == nil) != wantSelect {
				t.Errorf("%s %s SELECT error=%v wantSelect=%t", role, table, selectErr, wantSelect)
			}
			if _, dmlErr := conn.ExecContext(ctx, `DELETE FROM public.`+table); dmlErr == nil {
				t.Errorf("%s %s DELETE succeeded", role, table)
			}
		}
		if _, err := conn.ExecContext(ctx, `RESET SESSION AUTHORIZATION`); err != nil {
			t.Fatal(err)
		}
	}
	for _, role := range []string{"aboutme_app", "aboutme_maintenance", "aboutme_lifecycle_command", "aboutme_fencing_proof", "aboutme_restore_verify"} {
		for _, table := range runtimeMembershipTables {
			var canSelect, canDML bool
			if err := db.QueryRowContext(ctx, `SELECT has_table_privilege($1,'public.'||$2,'SELECT'), has_table_privilege($1,'public.'||$2,'INSERT,UPDATE,DELETE,TRUNCATE')`, role, table).Scan(&canSelect, &canDML); err != nil {
				t.Fatal(err)
			}
			wantSelect := role == "aboutme_app" && appReadable[table]
			if canSelect != wantSelect || canDML {
				t.Errorf("%s %s select=%t want=%t dml=%t", role, table, canSelect, wantSelect, canDML)
			}
		}
	}
	for _, typeName := range []string{"runtime_replica_result", "runtime_capacity_result", "runtime_leave_result", "runtime_fence_result"} {
		var owner string
		if err := db.QueryRowContext(ctx, `SELECT pg_get_userbyid(typowner) FROM pg_type WHERE typnamespace='public'::regnamespace AND typname=$1`, typeName).Scan(&owner); err != nil || owner != "aboutme_runtime_owner" {
			t.Fatalf("type %s owner=%q error=%v", typeName, owner, err)
		}
	}
	var badOwners int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_class WHERE relnamespace='public'::regnamespace AND relname=ANY($1) AND pg_get_userbyid(relowner)<>'aboutme_runtime_owner'`, runtimeMembershipTables).Scan(&badOwners); err != nil || badOwners != 0 {
		t.Fatalf("bad table owners=%d error=%v", badOwners, err)
	}
	var validHelpers bool
	if err := db.QueryRowContext(ctx, `SELECT count(*)=5 AND bool_and(prosecdef AND proconfig=ARRAY['search_path=pg_catalog']::text[] AND NOT has_function_privilege('public',oid,'EXECUTE')) FROM pg_proc WHERE pronamespace='public'::regnamespace AND proname=ANY(ARRAY['runtime_assert_immutable_row','runtime_validate_replica_change','runtime_assert_replica_task_trio','runtime_validate_lifecycle_step','runtime_validate_termination_intent'])`).Scan(&validHelpers); err != nil || !validHelpers {
		t.Fatalf("helper catalog valid=%t error=%v", validHelpers, err)
	}
	var writeEntryTriggers int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid WHERE c.relnamespace='public'::regnamespace AND c.relname=ANY($1) AND t.tgname LIKE 'runtime_%_write_entry' AND NOT t.tgisinternal AND (t.tgtype & 2)=2 AND (t.tgtype & 1)=0`, runtimeMembershipTables).Scan(&writeEntryTriggers); err != nil || writeEntryTriggers != len(runtimeMembershipTables) {
		t.Fatalf("BEFORE STATEMENT write-entry triggers=%d error=%v", writeEntryTriggers, err)
	}
	var columnACLs int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid WHERE c.relnamespace='public'::regnamespace AND c.relname=ANY($1) AND a.attnum>0 AND NOT a.attisdropped AND a.attacl IS NOT NULL`, runtimeMembershipTables).Scan(&columnACLs); err != nil || columnACLs != 0 {
		t.Fatalf("column ACL rows=%d error=%v", columnACLs, err)
	}
}
