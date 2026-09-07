package migrations

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

func TestRuntimeReplicaRegistrationFunctionsExist(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	for _, signature := range []string{
		"runtime_register_serving_replica(uuid,text,text,text,text,text,text)",
		"runtime_register_maintenance_replica(uuid,text,text,text,text,text,text)",
		"runtime_mark_serving_replica_join_ready(uuid,text,text,text,text,text,text)",
		"runtime_mark_maintenance_replica_join_ready(uuid,text,text,text,text,text,text)",
	} {
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT to_regprocedure('public.'||$1) IS NOT NULL`, signature).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("function %s is missing", signature)
		}
	}
}

func TestRuntimeReplicaRegistrationFreshReplayAndReady(t *testing.T) {
	for _, test := range []struct{ kind, role, register, ready string }{
		{"serving", "aboutme_app", "runtime_register_serving_replica", "runtime_mark_serving_replica_join_ready"},
		{"maintenance", "aboutme_maintenance", "runtime_register_maintenance_replica", "runtime_mark_maintenance_replica_join_ready"},
	} {
		t.Run(test.kind, func(t *testing.T) {
			db, ctx := runtimeMembershipDB(t)
			id := validRegistrationIdentity(map[string]string{"serving": "1", "maintenance": "2"}[test.kind])
			fresh, err := callRegistration(t, db, test.role, test.register, id)
			if err != nil {
				t.Fatal(err)
			}
			if fresh.kind != test.kind || fresh.state != "joining" || fresh.replayed || fresh.capacityGeneration != 2 || fresh.controllerGeneration != 1 {
				t.Fatalf("fresh result=%+v", fresh)
			}
			replay, err := callRegistration(t, db, test.role, test.register, id)
			if err != nil || !replay.replayed || replay.capacityGeneration != fresh.capacityGeneration {
				t.Fatalf("replay=%+v error=%v", replay, err)
			}
			ready, err := callRegistration(t, db, test.role, test.ready, id)
			if err != nil || ready.state != "joining" || ready.replayed || ready.capacityGeneration != 3 {
				t.Fatalf("ready=%+v error=%v", ready, err)
			}
			readyReplay, err := callRegistration(t, db, test.role, test.ready, id)
			if err != nil || !readyReplay.replayed || readyReplay.capacityGeneration != 3 {
				t.Fatalf("ready replay=%+v error=%v", readyReplay, err)
			}
			var taskCount int
			var joinReady bool
			if err := db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM public.runtime_replica_tasks WHERE replica_id=$1),(SELECT join_ready_at IS NOT NULL FROM public.runtime_replicas WHERE replica_id=$1)`, id.replicaID).Scan(&taskCount, &joinReady); err != nil || taskCount != 3 || !joinReady {
				t.Fatalf("tasks=%d ready=%t error=%v", taskCount, joinReady, err)
			}
		})
	}
}

func TestRuntimeReplicaRegistrationIdentityValidationAndReplayConflict(t *testing.T) {
	base := validRegistrationIdentity("3")
	mutations := []struct {
		name, code string
		mutate     func(*registrationIdentity)
	}{
		{"nil_uuid", "22023", func(i *registrationIdentity) { i.replicaID = "00000000-0000-0000-0000-000000000000" }},
		{"instance", "22023", func(i *registrationIdentity) { i.instanceID = "I-invalid" }},
		{"container_empty", "22023", func(i *registrationIdentity) { i.containerARN = "" }},
		{"container_long", "22023", func(i *registrationIdentity) { i.containerARN = strings.Repeat("a", 513) }},
		{"container_non_ascii", "22023", func(i *registrationIdentity) { i.containerARN = "arn:container:é" }},
		{"caddy_empty", "22023", func(i *registrationIdentity) { i.caddyARN = "" }},
		{"caddy_long", "22023", func(i *registrationIdentity) { i.caddyARN = strings.Repeat("c", 513) }},
		{"caddy_non_ascii", "22023", func(i *registrationIdentity) { i.caddyARN = "arn:task:é" }},
		{"go_empty", "22023", func(i *registrationIdentity) { i.goARN = "" }},
		{"go_long", "22023", func(i *registrationIdentity) { i.goARN = strings.Repeat("g", 513) }},
		{"go_non_ascii", "22023", func(i *registrationIdentity) { i.goARN = "arn:go:é" }},
		{"nuxt_empty", "22023", func(i *registrationIdentity) { i.nuxtARN = "" }},
		{"nuxt_long", "22023", func(i *registrationIdentity) { i.nuxtARN = strings.Repeat("n", 513) }},
		{"nuxt_non_ascii", "22023", func(i *registrationIdentity) { i.nuxtARN = "arn:nuxt:é" }},
		{"duplicate_tasks", "22023", func(i *registrationIdentity) { i.goARN = i.caddyARN }},
		{"release", "22023", func(i *registrationIdentity) { i.release = "bad" }},
	}
	for _, test := range mutations {
		t.Run(test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			id := base
			test.mutate(&id)
			_, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", id)
			requireRegistrationError(t, err, test.code)
		})
	}
	db, _ := runtimeMembershipDB(t)
	if _, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", base); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*registrationIdentity)
	}{
		{"instance", func(i *registrationIdentity) { i.instanceID = "i-33333333333333334" }},
		{"container", func(i *registrationIdentity) { i.containerARN += "x" }},
		{"caddy", func(i *registrationIdentity) { i.caddyARN += "x" }},
		{"go", func(i *registrationIdentity) { i.goARN += "x" }},
		{"nuxt", func(i *registrationIdentity) { i.nuxtARN += "x" }},
		{"release", func(i *registrationIdentity) { i.release = "sha256:" + strings.Repeat("4", 64) }},
	} {
		t.Run("replay_"+test.name, func(t *testing.T) {
			id := base
			test.mutate(&id)
			_, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", id)
			requireRegistrationError(t, err, "AM002")
		})
	}
	private := base
	private.containerARN = "arn:private:registration-secret"
	_, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", private)
	requireRegistrationError(t, err, "AM002")
	if strings.Contains(err.Error(), private.containerARN) || strings.Contains(err.Error(), base.containerARN) {
		t.Fatalf("registration error exposed identity: %v", err)
	}
}

func TestRuntimeReplicaRegistrationEveryInputNullAndARNMaximum(t *testing.T) {
	id := validRegistrationIdentity("3")
	base := []string{"'" + id.replicaID + "'", "'" + id.instanceID + "'", "'" + id.containerARN + "'", "'" + id.caddyARN + "'", "'" + id.goARN + "'", "'" + id.nuxtARN + "'", "'" + id.release + "'"}
	for index, name := range []string{"replica_id", "instance_id", "container_arn", "caddy_arn", "go_arn", "nuxt_arn", "release_digest"} {
		t.Run(name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			args := append([]string(nil), base...)
			args[index] = "NULL"
			_, err := callRegistrationArgs(t, db, "aboutme_app", "runtime_register_serving_replica", strings.Join(args, ","))
			requireRegistrationError(t, err, "22023")
		})
	}
	t.Run("arn_maximum", func(t *testing.T) {
		db, _ := runtimeMembershipDB(t)
		max := validRegistrationIdentity("4")
		max.containerARN = "a" + strings.Repeat("1", 511)
		max.caddyARN = "b" + strings.Repeat("2", 511)
		max.goARN = "c" + strings.Repeat("3", 511)
		max.nuxtARN = "d" + strings.Repeat("4", 511)
		if _, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", max); err != nil {
			t.Fatal(err)
		}
	})
}

func TestRuntimeReplicaRegistrationAllowsSameReleaseForDistinctReplicas(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	first := validRegistrationIdentity("3")
	second := validRegistrationIdentity("4")
	second.release = first.release
	if _, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", first); err != nil {
		t.Fatal(err)
	}
	if _, err := callRegistration(t, db, "aboutme_maintenance", "runtime_register_maintenance_replica", second); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeReplicaRegistrationRoleBoundary(t *testing.T) {
	for _, test := range []struct{ role, function string }{
		{"aboutme_app", "runtime_register_maintenance_replica"},
		{"aboutme_app", "runtime_mark_maintenance_replica_join_ready"},
		{"aboutme_maintenance", "runtime_register_serving_replica"},
		{"aboutme_maintenance", "runtime_mark_serving_replica_join_ready"},
		{"aboutme_lifecycle_command", "runtime_register_serving_replica"},
		{"aboutme_fencing_proof", "runtime_register_serving_replica"},
		{"aboutme_restore_verify", "runtime_register_serving_replica"},
		{"aboutme_migrator", "runtime_register_serving_replica"},
	} {
		t.Run(test.role+"_"+test.function, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			_, err := callRegistration(t, db, test.role, test.function, validRegistrationIdentity("4"))
			requireRegistrationError(t, err, "42501")
		})
	}
	db, _ := runtimeMembershipDB(t)
	id := validRegistrationIdentity("4")
	for _, test := range []struct{ name, role, sql string }{
		{"app_helper", "aboutme_app", `SELECT public.runtime_register_replica('serving','x',` + registrationArgs(id) + `)`},
		{"maintenance_helper", "aboutme_maintenance", `SELECT public.runtime_mark_replica_join_ready('maintenance','x',` + registrationArgs(id) + `)`},
		{"lifecycle_table", "aboutme_lifecycle_command", `SELECT * FROM public.runtime_replicas`},
		{"public_wrapper", "aboutme_restore_verify", `SELECT public.runtime_register_serving_replica(` + registrationArgs(id) + `)`},
	} {
		t.Run(test.name, func(t *testing.T) {
			requireRegistrationError(t, registrationRoleExec(t, db, test.role, test.sql), "42501")
		})
	}
}

func TestRuntimeReplicaRegistrationRequiresEntryBeforeParentLocks(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	id := validRegistrationIdentity("5")
	direct := `SELECT public.runtime_register_serving_replica(` + registrationArgs(id) + `)`
	requireRegistrationError(t, registrationRoleExec(t, db, "aboutme_app", direct), "AM001")
	if _, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", id); err != nil {
		t.Fatal(err)
	}
	requireRegistrationError(t, registrationRoleExec(t, db, "aboutme_app", direct), "AM001")
	_, holder := beginTransitionWrite(t, db)
	if _, err := holder.ExecContext(ctx, `UPDATE public.public_state SET discovery_generation=discovery_generation WHERE singleton`); err != nil {
		t.Fatal(err)
	}
	requireRegistrationError(t, registrationRoleExec(t, db, "aboutme_app", direct), "AM001")
	if err := finishTransitionWrite(holder); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeReplicaRegistrationGenerationExhaustionRollsBack(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	if err := membershipWrite(t, db, `UPDATE public.runtime_capacity SET generation=9223372036854775807`); err != nil {
		t.Fatal(err)
	}
	_, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", validRegistrationIdentity("5"))
	requireRegistrationError(t, err, "55000")
	var replicas int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM public.runtime_replicas`).Scan(&replicas); err != nil || replicas != 0 {
		t.Fatalf("replicas=%d error=%v", replicas, err)
	}
}

func TestRuntimeReplicaRegistrationCatalog(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	var valid bool
	if err := db.QueryRowContext(ctx, `SELECT count(*)=9 AND bool_and(pg_get_userbyid(proowner)='aboutme_runtime_owner' AND prosecdef AND proconfig=ARRAY['search_path=pg_catalog']::text[] AND NOT has_function_privilege('public',oid,'EXECUTE')) FROM pg_proc WHERE pronamespace='public'::regnamespace AND proname LIKE 'runtime_%replica%' AND proname=ANY($1)`, []string{"runtime_validate_replica_identity_input", "runtime_replica_result_value", "runtime_assert_replica_registration_children", "runtime_register_replica", "runtime_mark_replica_join_ready", "runtime_register_serving_replica", "runtime_register_maintenance_replica", "runtime_mark_serving_replica_join_ready", "runtime_mark_maintenance_replica_join_ready"}).Scan(&valid); err != nil || !valid {
		t.Fatalf("catalog valid=%t error=%v", valid, err)
	}
	for role, functions := range map[string][]string{
		"aboutme_app":         {"runtime_register_serving_replica", "runtime_mark_serving_replica_join_ready"},
		"aboutme_maintenance": {"runtime_register_maintenance_replica", "runtime_mark_maintenance_replica_join_ready"},
	} {
		for _, function := range functions {
			var execute bool
			if err := db.QueryRowContext(ctx, `SELECT has_function_privilege($1,'public.'||$2||'(uuid,text,text,text,text,text,text)','EXECUTE')`, role, function).Scan(&execute); err != nil || !execute {
				t.Fatalf("%s %s execute=%t error=%v", role, function, execute, err)
			}
		}
	}
}

func TestRuntimeReplicaRegistrationResultShapeAndCapacityEvidence(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	id := validRegistrationIdentity("6")
	result, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", id)
	if err != nil {
		t.Fatal(err)
	}
	if result.desired != nil || result.serving != nil || result.maintenance != nil || result.partition1 != nil || result.partition2 != nil {
		t.Fatalf("registration returned capacity-only fields: %+v", result)
	}
	var updatedBy string
	var joinedAt, capacityAt string
	if err := db.QueryRowContext(ctx, `SELECT c.updated_by,r.joined_at::text,c.updated_at::text FROM public.runtime_capacity c JOIN public.runtime_replicas r ON r.replica_id=$1`, id.replicaID).Scan(&updatedBy, &joinedAt, &capacityAt); err != nil {
		t.Fatal(err)
	}
	if updatedBy != "runtime_register_serving_replica" || joinedAt != capacityAt {
		t.Fatalf("updated_by=%q joined_at=%q capacity_at=%q", updatedBy, joinedAt, capacityAt)
	}
}

func TestRuntimeReplicaRegistrationAcrossLifecycleAndCurrentReplay(t *testing.T) {
	for index, phase := range []string{"offline", "starting", "online", "stopping"} {
		t.Run(phase, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			admission := phase == "online"
			if err := membershipWrite(t, db, fmt.Sprintf(`UPDATE public.runtime_capacity SET lifecycle_phase='%s',admission_enabled=%t`, phase, admission)); err != nil {
				t.Fatal(err)
			}
			id := validRegistrationIdentity(fmt.Sprint(index + 1))
			fresh, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", id)
			if err != nil || fresh.state != "joining" || fresh.lifecycle != phase || fresh.admission != admission {
				t.Fatalf("fresh=%+v error=%v", fresh, err)
			}
			if mutateErr := membershipWrite(t, db, fmt.Sprintf(`UPDATE public.runtime_replicas SET state='active',join_ready_at=joined_at,activated_at=joined_at WHERE replica_id='%s'`, id.replicaID)); mutateErr != nil {
				t.Fatal(mutateErr)
			}
			replay, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", id)
			if err != nil || !replay.replayed || replay.state != "active" || replay.capacityGeneration != fresh.capacityGeneration {
				t.Fatalf("current replay=%+v error=%v", replay, err)
			}
		})
	}
}

func TestRuntimeReplicaRegistrationJoinReadyAdmissionMatrixAndNoPartitionMutation(t *testing.T) {
	for index, test := range []struct {
		phase     string
		admission bool
		code      string
	}{
		{"online", true, ""}, {"starting", false, ""}, {"offline", false, "55000"}, {"stopping", false, "55000"},
	} {
		t.Run(test.phase, func(t *testing.T) {
			db, ctx := runtimeMembershipDB(t)
			id := validRegistrationIdentity(fmt.Sprint(index + 5))
			if _, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", id); err != nil {
				t.Fatal(err)
			}
			if err := membershipWrite(t, db,
				fmt.Sprintf(`UPDATE public.runtime_capacity SET lifecycle_phase='%s',admission_enabled=%t`, test.phase, test.admission),
				`UPDATE public.shared_rate_partitions SET enabled=false`); err != nil {
				t.Fatal(err)
			}
			result, err := callRegistration(t, db, "aboutme_app", "runtime_mark_serving_replica_join_ready", id)
			if test.code != "" {
				requireRegistrationError(t, err, test.code)
				return
			}
			if err != nil || result.replayed || result.state != "joining" {
				t.Fatalf("ready=%+v error=%v", result, err)
			}
			var enabled int
			if queryErr := db.QueryRowContext(ctx, `SELECT count(*) FROM public.shared_rate_partitions WHERE enabled`).Scan(&enabled); queryErr != nil || enabled != 0 {
				t.Fatalf("enabled partitions=%d error=%v", enabled, queryErr)
			}
			replay, err := callRegistration(t, db, "aboutme_app", "runtime_mark_serving_replica_join_ready", id)
			if err != nil || !replay.replayed || replay.capacityGeneration != result.capacityGeneration {
				t.Fatalf("ready replay=%+v error=%v", replay, err)
			}
		})
	}
}

func TestRuntimeReplicaRegistrationTaskAndInstanceConflictsRollback(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*registrationIdentity, registrationIdentity)
	}{
		{"live_instance", func(i *registrationIdentity, first registrationIdentity) { i.instanceID = first.instanceID }},
		{"cross_role_task", func(i *registrationIdentity, first registrationIdentity) { i.nuxtARN = first.caddyARN }},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, ctx := runtimeMembershipDB(t)
			first := validRegistrationIdentity("7")
			if _, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", first); err != nil {
				t.Fatal(err)
			}
			second := validRegistrationIdentity("8")
			test.mutate(&second, first)
			_, err := callRegistration(t, db, "aboutme_maintenance", "runtime_register_maintenance_replica", second)
			requireRegistrationError(t, err, "AM002")
			var parents, tasks int
			if err := db.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE replica_id=$1),count(*) FILTER (WHERE replica_id=$1) FROM public.runtime_replicas r FULL JOIN public.runtime_replica_tasks t USING(replica_id)`, second.replicaID).Scan(&parents, &tasks); err != nil || parents != 0 || tasks != 0 {
				t.Fatalf("partial parent=%d tasks=%d error=%v", parents, tasks, err)
			}
		})
	}
}

func TestRuntimeReplicaRegistrationClockClampAndReadyOutsideJoining(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	id := validRegistrationIdentity("9")
	if err := membershipWrite(t, db, `UPDATE public.runtime_capacity SET updated_at=clock_timestamp()+interval '1 day'`); err != nil {
		t.Fatal(err)
	}
	if _, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", id); err != nil {
		t.Fatal(err)
	}
	var clamped bool
	if err := db.QueryRowContext(ctx, `SELECT r.joined_at=c.updated_at AND c.updated_at>clock_timestamp() FROM public.runtime_replicas r CROSS JOIN public.runtime_capacity c WHERE r.replica_id=$1`, id.replicaID).Scan(&clamped); err != nil || !clamped {
		t.Fatalf("registration clock clamp=%t error=%v", clamped, err)
	}
	if err := membershipWrite(t, db, fmt.Sprintf(`UPDATE public.runtime_replicas SET state='active',join_ready_at=joined_at,activated_at=joined_at WHERE replica_id='%s'`, id.replicaID)); err != nil {
		t.Fatal(err)
	}
	_, err := callRegistration(t, db, "aboutme_app", "runtime_mark_serving_replica_join_ready", id)
	requireRegistrationError(t, err, "55000")
}

func TestRuntimeReplicaRegistrationReadyClockClampAndExhaustionRollback(t *testing.T) {
	t.Run("clock_clamp", func(t *testing.T) {
		db, ctx := runtimeMembershipDB(t)
		id := validRegistrationIdentity("9")
		if _, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", id); err != nil {
			t.Fatal(err)
		}
		if err := membershipWrite(t, db, `UPDATE public.runtime_capacity SET updated_at=clock_timestamp()+interval '2 days'`); err != nil {
			t.Fatal(err)
		}
		if _, err := callRegistration(t, db, "aboutme_app", "runtime_mark_serving_replica_join_ready", id); err != nil {
			t.Fatal(err)
		}
		var clamped bool
		if err := db.QueryRowContext(ctx, `SELECT r.join_ready_at=c.updated_at AND r.join_ready_at>=r.joined_at FROM public.runtime_replicas r CROSS JOIN public.runtime_capacity c WHERE r.replica_id=$1`, id.replicaID).Scan(&clamped); err != nil || !clamped {
			t.Fatalf("ready clock clamp=%t error=%v", clamped, err)
		}
	})
	t.Run("generation_exhaustion", func(t *testing.T) {
		db, ctx := runtimeMembershipDB(t)
		id := validRegistrationIdentity("9")
		if _, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", id); err != nil {
			t.Fatal(err)
		}
		if err := membershipWrite(t, db, `UPDATE public.runtime_capacity SET generation=9223372036854775807`); err != nil {
			t.Fatal(err)
		}
		_, err := callRegistration(t, db, "aboutme_app", "runtime_mark_serving_replica_join_ready", id)
		requireRegistrationError(t, err, "55000")
		var generation int64
		var ready bool
		if err := db.QueryRowContext(ctx, `SELECT c.generation,r.join_ready_at IS NOT NULL FROM public.runtime_capacity c CROSS JOIN public.runtime_replicas r WHERE r.replica_id=$1`, id.replicaID).Scan(&generation, &ready); err != nil || generation != math.MaxInt64 || ready {
			t.Fatalf("generation=%d ready=%t error=%v", generation, ready, err)
		}
	})
}

func TestRuntimeReplicaRegistrationVisibleTransitionAndReadyRejection(t *testing.T) {
	for _, state := range []string{"closing", "unresolved"} {
		t.Run(state, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			fixture := transitionFixture()
			if state == "unresolved" {
				fixture = append(fixture, fmt.Sprintf(`UPDATE public.public_transitions SET state='unresolved',terminal_at=clock_timestamp(),terminal_error_code='recovery_evidence_conflict' WHERE transition_id='%s'`, transitionID))
			}
			if err := membershipWrite(t, db, fixture...); err != nil {
				t.Fatal(err)
			}
			id := validRegistrationIdentity("5")
			if _, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", id); err != nil {
				t.Fatalf("registration under %s transition: %v", state, err)
			}
			_, err := callRegistration(t, db, "aboutme_app", "runtime_mark_serving_replica_join_ready", id)
			requireRegistrationError(t, err, "55000")
		})
	}
}

func TestRuntimeReplicaRegistrationDoesNotWaitOnTransitionParent(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	if err := membershipWrite(t, db, transitionFixture()...); err != nil {
		t.Fatal(err)
	}
	holder, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	holderOpen := true
	defer func() {
		if holderOpen {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, cleanupErr := holder.ExecContext(cleanup, `ROLLBACK; RESET ROLE; RESET SESSION AUTHORIZATION`); cleanupErr != nil {
				t.Errorf("cleanup transition lock holder: %v", cleanupErr)
			}
			if closeErr := holder.Close(); closeErr != nil {
				t.Errorf("close transition lock holder: %v", closeErr)
			}
		}
	}()
	if _, err := holder.ExecContext(ctx, `SET SESSION AUTHORIZATION aboutme_migrator; SET ROLE aboutme_runtime_owner; BEGIN`); err != nil {
		t.Fatal(err)
	}
	if err := holder.QueryRowContext(ctx, `SELECT transition_id FROM public.public_transitions WHERE transition_id=$1 FOR UPDATE`, transitionID).Scan(new(string)); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	id := validRegistrationIdentity("7")
	go func() {
		_, callErr := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", id)
		result <- callErr
	}()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("registration waited on transition parent")
	}
	if _, err := holder.ExecContext(ctx, `ROLLBACK; RESET ROLE; RESET SESSION AUTHORIZATION`); err != nil {
		t.Fatal(err)
	}
	holderOpen = false
	if err := holder.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeReplicaRegistrationReplaysEveryLaterState(t *testing.T) {
	for _, state := range []struct {
		name string
		set  []string
	}{
		{"joining", nil},
		{"active", []string{`state='active',join_ready_at=joined_at,activated_at=joined_at`}},
		{"draining", []string{`state='active',join_ready_at=joined_at,activated_at=joined_at`, `state='draining',draining_at=joined_at`}},
		{"left", []string{`state='active',join_ready_at=joined_at,activated_at=joined_at`, `state='draining',draining_at=joined_at`, `state='left',left_at=joined_at`}},
		{"terminating", []string{`state='terminating',termination_requested_at=joined_at`}},
		{"fenced", []string{`state='fenced',fenced_at=joined_at`}},
	} {
		t.Run(state.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			id := validRegistrationIdentity("6")
			fresh, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", id)
			if err != nil {
				t.Fatal(err)
			}
			for _, mutation := range state.set {
				if mutationErr := membershipWrite(t, db, fmt.Sprintf(`UPDATE public.runtime_replicas SET %s WHERE replica_id='%s'`, mutation, id.replicaID)); mutationErr != nil {
					t.Fatal(mutationErr)
				}
			}
			replay, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", id)
			if err != nil || !replay.replayed || replay.state != state.name || replay.capacityGeneration != fresh.capacityGeneration {
				t.Fatalf("replay=%+v error=%v", replay, err)
			}
		})
	}
}

func TestRuntimeReplicaRegistrationClosedGateAndTerminationIntent(t *testing.T) {
	for _, gate := range []string{"closing", "closed"} {
		t.Run(gate+"_gate", func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			if _, err := db.ExecContext(context.Background(), `UPDATE public.runtime_write_state SET write_gate=$1`, gate); err != nil {
				t.Fatal(err)
			}
			_, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", validRegistrationIdentity("7"))
			requireRegistrationError(t, err, "55000")
		})
	}
	t.Run("termination_intent", func(t *testing.T) {
		db, _ := runtimeMembershipDB(t)
		id := validRegistrationIdentity("7")
		if _, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", id); err != nil {
			t.Fatal(err)
		}
		if err := membershipWrite(t, db, fmt.Sprintf(`INSERT INTO public.runtime_termination_intents(replica_id,instance_id,release_digest,request_id,reason,requested_at) VALUES('%s','%s','%s','request-1','startup_failed',clock_timestamp())`, id.replicaID, id.instanceID, id.release), fmt.Sprintf(`UPDATE public.runtime_replicas SET state='terminating',termination_requested_at=joined_at WHERE replica_id='%s'`, id.replicaID)); err != nil {
			t.Fatal(err)
		}
		_, err := callRegistration(t, db, "aboutme_app", "runtime_mark_serving_replica_join_ready", id)
		requireRegistrationError(t, err, "55000")
	})
}

func TestRuntimeReplicaRegistrationDetectsStoredChildCorruption(t *testing.T) {
	for _, test := range []struct{ name, mutation string }{
		{"missing", `DELETE FROM public.runtime_replica_tasks WHERE task_role='nuxt'`},
		{"mismatched", `UPDATE public.runtime_replica_tasks SET task_arn=task_arn||'-corrupt' WHERE task_role='go'`},
		{"extra", `ALTER TABLE public.runtime_replica_tasks DROP CONSTRAINT runtime_replica_tasks_task_role_check; ALTER TABLE public.runtime_replica_tasks DROP CONSTRAINT runtime_replica_tasks_replica_id_task_role_key; INSERT INTO public.runtime_replica_tasks(task_arn,replica_id,task_role) SELECT 'arn:task:extra',replica_id,'extra' FROM public.runtime_replicas`},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			id := validRegistrationIdentity("8")
			if _, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", id); err != nil {
				t.Fatal(err)
			}
			if err := membershipWrite(t, db, `ALTER TABLE public.runtime_replica_tasks DISABLE TRIGGER runtime_replica_tasks_immutable`, `ALTER TABLE public.runtime_replica_tasks DISABLE TRIGGER runtime_replica_tasks_trio`); err != nil {
				t.Fatal(err)
			}
			if err := membershipWrite(t, db, test.mutation); err != nil {
				t.Fatal(err)
			}
			if err := membershipWrite(t, db, `ALTER TABLE public.runtime_replica_tasks ENABLE TRIGGER runtime_replica_tasks_immutable`, `ALTER TABLE public.runtime_replica_tasks ENABLE TRIGGER runtime_replica_tasks_trio`); err != nil {
				t.Fatal(err)
			}
			_, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", id)
			requireRegistrationError(t, err, "AM001")
		})
	}
}

func TestRuntimeReplicaRegistrationPreservesPopulatedVersion18(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ProvisionDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := applyFS(ctx, db, runtimeTransitionFixtureFS(t, 18), LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO public.users(id,email,name) VALUES('19191919-1919-4919-8919-191919191919','registration-preserve@example.test','registration preserved')`); err != nil {
		t.Fatal(err)
	}
	preserved := append(sharedClaimVectorFixture(), transitionFixture()...)
	preserved = append(preserved, transitionAckSQL(initiatorID), `INSERT INTO public.runtime_lifecycle_operations(operation_id,workflow_kind) VALUES('registration-preserve-lifecycle','initial_serving')`,
		rateBucketSQL("oauth.failed_grant", "19", 1, map[string]string{"window_started_at": "transaction_timestamp()"}),
		pendingAttemptSQL("19191919-1919-4919-8919-191919191918", "private", "19", 1, storedPendingWindowOverrides("private", "19")))
	if err := membershipWrite(t, db, preserved...); err != nil {
		t.Fatal(err)
	}
	var before, capacityBefore int64
	if err := db.QueryRowContext(ctx, `SELECT s.generation,c.generation FROM public.runtime_write_state s CROSS JOIN public.runtime_capacity c WHERE s.singleton AND c.singleton`).Scan(&before, &capacityBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := applyFS(ctx, db, runtimeTransitionFixtureFS(t, 19), LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	var after, capacityAfter int64
	var name string
	var replicas, lifecycle, transitions, claims, rates, buckets, attempts int
	if err := db.QueryRowContext(ctx, `SELECT s.generation,c.generation,u.name,(SELECT count(*) FROM public.runtime_replicas),(SELECT count(*) FROM public.runtime_lifecycle_operations WHERE operation_id='registration-preserve-lifecycle'),(SELECT count(*) FROM public.public_transitions),(SELECT count(*) FROM public.shared_claim_requests),(SELECT count(*) FROM public.shared_rate_policies),(SELECT count(*) FROM public.shared_rate_buckets),(SELECT count(*) FROM public.shared_admission_attempts) FROM public.runtime_write_state s CROSS JOIN public.runtime_capacity c CROSS JOIN public.users u WHERE s.singleton AND c.singleton AND u.id='19191919-1919-4919-8919-191919191919'`).Scan(&after, &capacityAfter, &name, &replicas, &lifecycle, &transitions, &claims, &rates, &buckets, &attempts); err != nil || after != before+1 || capacityAfter != capacityBefore || name != "registration preserved" || replicas != 2 || lifecycle != 1 || transitions != 1 || claims != 1 || rates != 24 || buckets != 1 || attempts != 1 {
		t.Fatalf("generation=%d/%d capacity=%d/%d name=%q replicas=%d lifecycle=%d transitions=%d claims=%d rates=%d debt=%d/%d error=%v", before, after, capacityBefore, capacityAfter, name, replicas, lifecycle, transitions, claims, rates, buckets, attempts, err)
	}
}

func TestRuntimeReplicaRegistrationFailedMigrationRollsBackFunctionsAndGeneration(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ProvisionDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := applyFS(ctx, db, runtimeTransitionFixtureFS(t, 18), LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	var before int64
	if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	fixture := runtimeTransitionFixtureFS(t, 19)
	file := fixture["00019_runtime_replica_registration.sql"]
	anchor := "RESET ROLE;\nREVOKE CREATE"
	if file == nil || !strings.Contains(string(file.Data), anchor) {
		t.Fatal("migration 19 failure anchor is missing")
	}
	file.Data = []byte(strings.Replace(string(file.Data), anchor, "SELECT 1/0;\n"+anchor, 1))
	_, applyErr := applyFS(ctx, db, fixture, LocalAdminMigratorIdentity())
	requireRegistrationError(t, applyErr, "22012")
	var after int64
	var functions int
	if err := db.QueryRowContext(ctx, `SELECT generation,(SELECT count(*) FROM pg_proc WHERE pronamespace='public'::regnamespace AND proname=ANY($1)) FROM public.runtime_write_state WHERE singleton`, []string{"runtime_register_serving_replica", "runtime_register_maintenance_replica", "runtime_mark_serving_replica_join_ready", "runtime_mark_maintenance_replica_join_ready"}).Scan(&after, &functions); err != nil || after != before || functions != 0 {
		t.Fatalf("generation=%d/%d functions=%d error=%v", before, after, functions, err)
	}
}

func TestRuntimeReplicaRegistrationIdentityAndReadyRacesSerialize(t *testing.T) {
	for _, test := range []struct {
		name, secondFunction, expectedCode string
		mutateSecond                       func(*registrationIdentity, registrationIdentity)
		expectedReplay                     bool
	}{
		{"same_uuid", "runtime_register_serving_replica", "", func(second *registrationIdentity, first registrationIdentity) { *second = first }, true},
		{"live_instance", "runtime_register_serving_replica", "AM002", func(second *registrationIdentity, first registrationIdentity) { second.instanceID = first.instanceID }, false},
		{"cross_role_task", "runtime_register_maintenance_replica", "AM002", func(second *registrationIdentity, first registrationIdentity) { second.goARN = first.caddyARN }, false},
		{"register_ready", "runtime_mark_serving_replica_join_ready", "", func(second *registrationIdentity, first registrationIdentity) { *second = first }, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, ctx := runtimeMembershipDB(t)
			firstID := validRegistrationIdentity("9")
			secondID := validRegistrationIdentity("8")
			test.mutateSecond(&secondID, firstID)
			first, _ := openRegistrationTx(t, db, "aboutme_app")
			firstOpen := true
			defer func() {
				if firstOpen {
					if err := rollbackRegistrationTx(first); err != nil {
						t.Error(err)
					}
				}
			}()
			firstStatement := fmt.Sprintf(`SELECT (public.runtime_register_serving_replica(%s)).replayed`, registrationArgs(firstID))
			var firstReplay bool
			if err := first.QueryRowContext(ctx, firstStatement).Scan(&firstReplay); err != nil || firstReplay {
				t.Fatalf("first replay=%t error=%v", firstReplay, err)
			}
			secondRole := "aboutme_app"
			if test.secondFunction == "runtime_register_maintenance_replica" {
				secondRole = "aboutme_maintenance"
			}
			second, secondPID := openRegistrationTx(t, db, secondRole)
			secondOpen := true
			defer func() {
				if secondOpen {
					if err := rollbackRegistrationTx(second); err != nil {
						t.Error(err)
					}
				}
			}()
			secondStatement := fmt.Sprintf(`SELECT (public.%s(%s)).replayed`, test.secondFunction, registrationArgs(secondID))
			raceCtx, cancelRace := context.WithTimeout(context.Background(), 10*time.Second)
			result := make(chan error, 1)
			var secondReplay bool
			go func() { result <- second.QueryRowContext(raceCtx, secondStatement).Scan(&secondReplay) }()
			if err := waitForTransitionCondition(db, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND NOT granted)`, secondPID); err != nil {
				cancelRace()
				if finishErr := finishRegistrationTx(first); finishErr != nil {
					t.Errorf("finish first registration after observation failure: %v", finishErr)
				}
				firstOpen = false
				<-result
				t.Fatal(err)
			}
			if err := finishRegistrationTx(first); err != nil {
				cancelRace()
				<-result
				firstOpen = false
				t.Fatal(err)
			}
			firstOpen = false
			var secondErr error
			select {
			case secondErr = <-result:
			case <-time.After(5 * time.Second):
				cancelRace()
				<-result
				t.Fatal("timed out joining registration race")
			}
			cancelRace()
			if test.expectedCode != "" {
				requireRegistrationError(t, secondErr, test.expectedCode)
				if err := rollbackRegistrationTx(second); err != nil {
					secondOpen = false
					t.Fatal(err)
				}
				secondOpen = false
			} else {
				if secondErr != nil || secondReplay != test.expectedReplay {
					t.Fatalf("second replay=%t error=%v", secondReplay, secondErr)
				}
				if err := finishRegistrationTx(second); err != nil {
					secondOpen = false
					t.Fatal(err)
				}
				secondOpen = false
			}
			var parents, tasks int
			if err := db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM public.runtime_replicas),(SELECT count(*) FROM public.runtime_replica_tasks)`).Scan(&parents, &tasks); err != nil || parents != 1 || tasks != 3 {
				t.Fatalf("parents=%d tasks=%d error=%v", parents, tasks, err)
			}
		})
	}
}
