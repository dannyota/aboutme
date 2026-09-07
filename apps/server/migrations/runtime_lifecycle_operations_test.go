package migrations

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestRuntimeLifecycleOperationsFunctionsExist proves migration 00022 installs
// the six fixed controller actions with their exact signatures and result
// composites, plus the owner-only digest and time helpers.
func TestRuntimeLifecycleOperationsFunctionsExist(t *testing.T) {
	db, ctx := runtimeLifecycleDB(t)
	for _, composite := range lifecycleResultComposites {
		var attributes string
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(string_agg(a.attname||':'||format_type(a.atttypid,a.atttypmod),',' ORDER BY a.attnum),'') FROM pg_type t JOIN pg_class c ON c.oid=t.typrelid JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped WHERE t.typnamespace='public'::regnamespace AND t.typname=$1`, composite.name).Scan(&attributes); err != nil {
			t.Fatal(err)
		}
		if attributes != composite.attributes {
			t.Errorf("type %s attributes=%q want=%q", composite.name, attributes, composite.attributes)
		}
	}
	for _, operation := range lifecycleOperationSignatures {
		var returns string
		if err := db.QueryRowContext(ctx, `SELECT COALESCE((SELECT n.nspname||'.'||t.typname FROM pg_proc p JOIN pg_type t ON t.oid=p.prorettype JOIN pg_namespace n ON n.oid=t.typnamespace WHERE p.oid=to_regprocedure('public.'||$1)),'')`, operation.name+operation.signature).Scan(&returns); err != nil {
			t.Fatal(err)
		}
		if returns != "public."+operation.result {
			t.Errorf("function %s%s returns=%q want=%q", operation.name, operation.signature, returns, "public."+operation.result)
		}
	}
	for _, helper := range lifecycleHelperSignatures {
		var installed bool
		if err := db.QueryRowContext(ctx, `SELECT to_regprocedure('public.'||$1) IS NOT NULL`, helper.name+helper.signature).Scan(&installed); err != nil {
			t.Fatal(err)
		}
		if !installed {
			t.Errorf("helper %s%s is missing", helper.name, helper.signature)
		}
	}
	var clock string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE((SELECT format_type(prorettype,NULL)||' argc='||pronargs||' volatile='||provolatile::text FROM pg_proc WHERE oid=to_regprocedure('public.runtime_sample_lifecycle_time()')),'')`).Scan(&clock); err != nil {
		t.Fatal(err)
	}
	if clock != "timestamp with time zone argc=0 volatile=v" {
		t.Errorf("runtime_sample_lifecycle_time=%q want=%q", clock, "timestamp with time zone argc=0 volatile=v")
	}
}

// TestRuntimeLifecycleOperationsDigestVectors reproduces all ten pinned
// vectors with an independent Go encoder, then proves the installed SQL
// helpers hash the identical bytes and that the migration's own literal
// assertion still holds.
func TestRuntimeLifecycleOperationsDigestVectors(t *testing.T) {
	db, _ := runtimeLifecycleDB(t)
	for _, vector := range lifecycleVectors() {
		t.Run(vector.name, func(t *testing.T) {
			arguments := goLifecycleStream(lifecycleKindArguments, vector.argumentFields...)
			results := goLifecycleStream(lifecycleKindResult, vector.resultFields...)
			if len(arguments) != vector.argumentBytes || goLifecycleDigest(arguments) != vector.argumentDigest {
				t.Fatalf("argument bytes=%d/%d digest=%s want=%s", len(arguments), vector.argumentBytes,
					goLifecycleDigest(arguments), vector.argumentDigest)
			}
			if len(results) != vector.resultBytes || goLifecycleDigest(results) != vector.resultDigest {
				t.Fatalf("result bytes=%d/%d digest=%s want=%s", len(results), vector.resultBytes,
					goLifecycleDigest(results), vector.resultDigest)
			}
			var argumentSQL, resultSQL string
			if err := lifecycleOwnerQuery(t, db,
				`SELECT encode(public.runtime_lifecycle_digest(1::smallint,`+lifecycleHexBytes(arguments[35:])+`),'hex')`,
				&argumentSQL); err != nil {
				t.Fatal(err)
			}
			if err := lifecycleOwnerQuery(t, db,
				`SELECT encode(public.runtime_lifecycle_digest(2::smallint,`+lifecycleHexBytes(results[35:])+`),'hex')`,
				&resultSQL); err != nil {
				t.Fatal(err)
			}
			if argumentSQL != vector.argumentDigest || resultSQL != vector.resultDigest {
				t.Fatalf("sql argument=%s result=%s", argumentSQL, resultSQL)
			}
		})
	}
	t.Run("sql_fields_match_go_bytes", func(t *testing.T) {
		for _, field := range []struct {
			name       string
			expression string
			want       []byte
		}{
			{"uuid_present", `public.runtime_lifecycle_field_uuid('` + vectorReplica + `')`, goLifecycleUUID(vectorReplica)},
			{"uuid_null", `public.runtime_lifecycle_field_uuid(NULL::uuid)`, goLifecycleUUID("")},
			{"int8_present", `public.runtime_lifecycle_field_int8(41)`, goLifecycleInt8(41, true)},
			{"int8_negative", `public.runtime_lifecycle_field_int8(-1)`, goLifecycleInt8(-1, true)},
			{"int8_null", `public.runtime_lifecycle_field_int8(NULL::bigint)`, goLifecycleInt8(0, false)},
			{"text_present", `public.runtime_lifecycle_field_text('online')`, goLifecycleText("online", true)},
			{"text_empty_present", `public.runtime_lifecycle_field_text('')`, goLifecycleText("", true)},
			{"text_null", `public.runtime_lifecycle_field_text(NULL::text)`, goLifecycleText("", false)},
			{"bool_true", `public.runtime_lifecycle_field_bool(true)`, goLifecycleBool(true, true)},
			{"bool_false", `public.runtime_lifecycle_field_bool(false)`, goLifecycleBool(false, true)},
			{"bool_null", `public.runtime_lifecycle_field_bool(NULL::boolean)`, goLifecycleBool(false, false)},
		} {
			var encoded string
			if err := lifecycleOwnerQuery(t, db, `SELECT encode(`+field.expression+`,'hex')`, &encoded); err != nil {
				t.Fatal(err)
			}
			if encoded != hex.EncodeToString(field.want) {
				t.Errorf("field %s sql=%s go=%s", field.name, encoded, hex.EncodeToString(field.want))
			}
		}
	})
	t.Run("installed_vectors_still_hold", func(t *testing.T) {
		var asserted string
		if err := lifecycleOwnerQuery(t, db, `SELECT public.runtime_lifecycle_assert_digest_vectors()::text`, &asserted); err != nil {
			t.Fatalf("installed vector assertion failed: %v", err)
		}
	})
	t.Run("field_shapes_are_enforced", func(t *testing.T) {
		for _, invalid := range []struct{ name, expression string }{
			{"unknown_type", `public.runtime_lifecycle_digest_field(5::smallint,decode('00','hex'))`},
			{"null_type", `public.runtime_lifecycle_digest_field(NULL::smallint,decode('00','hex'))`},
			{"short_uuid", `public.runtime_lifecycle_digest_field(1::smallint,decode('0011','hex'))`},
			{"short_int8", `public.runtime_lifecycle_digest_field(2::smallint,decode('0011','hex'))`},
			{"wide_bool", `public.runtime_lifecycle_digest_field(4::smallint,decode('0000','hex'))`},
			{"invalid_bool", `public.runtime_lifecycle_digest_field(4::smallint,decode('02','hex'))`},
			{"unknown_kind", `public.runtime_lifecycle_digest(3::smallint,decode('00','hex'))`},
			{"null_framed", `public.runtime_lifecycle_digest(1::smallint,NULL::bytea)`},
		} {
			var sink string
			err := lifecycleOwnerQuery(t, db, `SELECT encode(`+invalid.expression+`,'hex')`, &sink)
			requireLifecycleError(t, err, "AM001")
		}
	})
}

func requireNilPointers(t *testing.T, result registrationResult, label string) {
	t.Helper()
	if result.desired != nil || result.serving != nil || result.maintenance != nil ||
		result.partition1 != nil || result.partition2 != nil {
		t.Fatalf("%s must carry null count and partition pointers: %+v", label, result)
	}
}

// TestRuntimeLifecycleOperationsServingScaleOut proves initial serving
// activation, prepare-scale-out and the second serving activation, with the
// exact partition, generation and evidence changes of each step.
func TestRuntimeLifecycleOperationsServingScaleOut(t *testing.T) {
	db, _ := runtimeLifecycleDB(t)
	first := validRegistrationIdentity("1")
	second := validRegistrationIdentity("2")

	initial := lifecycleActivateServing(t, db, 1, "op-initial", first, "")
	if initial.state != "active" || initial.kind != "serving" || initial.replayed ||
		initial.controllerGeneration != 2 || initial.controllerOperation != "op-initial" {
		t.Fatalf("initial activation=%+v", initial)
	}
	requireNilPointers(t, initial, "activation")
	capacity := lifecycleCapacity(t, db)
	if capacity.desired != 1 || capacity.controllerGeneration != 2 ||
		capacity.controllerOperation != "op-initial" || capacity.updatedBy != "op-initial" {
		t.Fatalf("capacity after initial activation=%+v", capacity)
	}
	enabled := lifecyclePartition(t, db, 1)
	disabled := lifecyclePartition(t, db, 2)
	if enabled.rows != 24 || enabled.enabled != 24 || enabled.distinctEvidence != 1 ||
		enabled.generation != capacity.generation || enabled.operation != "op-initial" {
		t.Fatalf("partition 1 after initial activation=%+v", enabled)
	}
	if disabled.rows != 24 || disabled.enabled != 0 || disabled.distinctEvidence != 1 {
		t.Fatalf("partition 2 after initial activation=%+v", disabled)
	}

	prepared, err := callLifecycleCapacity(t, db, lifecycleScaleOutExpr(2, "op-scale-out"))
	if err != nil {
		t.Fatal(err)
	}
	if prepared.desired != 2 || prepared.serving != 1 || prepared.maintenance != 0 ||
		!prepared.partition1 || prepared.partition2 || prepared.controllerGen != 3 ||
		prepared.controllerOperation != "op-scale-out" || prepared.replayed ||
		prepared.writeGate.Valid || prepared.writeGeneration.Valid {
		t.Fatalf("prepare scale out=%+v", prepared)
	}
	if summary := lifecyclePartition(t, db, 2); summary.enabled != 0 {
		t.Fatalf("prepare scale out enabled partition 2=%+v", summary)
	}

	activated := lifecycleActivateServing(t, db, 3, "op-scale-out", second, "")
	if activated.state != "active" || activated.controllerGeneration != 4 || activated.replayed {
		t.Fatalf("second activation=%+v", activated)
	}
	requireNilPointers(t, activated, "second activation")
	capacity = lifecycleCapacity(t, db)
	second2 := lifecyclePartition(t, db, 2)
	if second2.enabled != 24 || second2.distinctEvidence != 1 || second2.generation != capacity.generation ||
		second2.operation != "op-scale-out" {
		t.Fatalf("partition 2 after second activation=%+v", second2)
	}
	if first1 := lifecyclePartition(t, db, 1); first1.enabled != 24 || first1.operation != "op-initial" {
		t.Fatalf("second activation disturbed partition 1=%+v", first1)
	}
	if capacity.desired != 2 || capacity.controllerGeneration != 4 {
		t.Fatalf("capacity after second activation=%+v", capacity)
	}
	operations, steps, intents := lifecycleStepCount(t, db)
	if operations != 2 || steps != 3 || intents != 0 {
		t.Fatalf("ledger operations=%d steps=%d intents=%d", operations, steps, intents)
	}
}

// TestRuntimeLifecycleOperationsGracefulScaleIn proves the prepared target
// drains without changing capacity, then that a left target with its exact
// leave receipt and EC2 proof sets desired one and disables partition 2 while
// preserving partition 1.
func TestRuntimeLifecycleOperationsGracefulScaleIn(t *testing.T) {
	db, _ := runtimeLifecycleDB(t)
	first, second := lifecycleTwoNodeFleet(t, db)

	drained, err := callLifecycleReplica(t, db, lifecycleScaleInExpr(4, "op-scale-in", first))
	if err != nil {
		t.Fatal(err)
	}
	if drained.state != "draining" || drained.kind != "serving" || drained.controllerGeneration != 5 || drained.replayed {
		t.Fatalf("prepare scale in=%+v", drained)
	}
	requireNilPointers(t, drained, "prepare scale in")
	capacity := lifecycleCapacity(t, db)
	if capacity.desired != 2 {
		t.Fatalf("prepare scale in changed desired=%+v", capacity)
	}
	if one, two := lifecyclePartition(t, db, 1), lifecyclePartition(t, db, 2); one.enabled != 24 || two.enabled != 24 {
		t.Fatalf("prepare scale in changed flags one=%+v two=%+v", one, two)
	}

	lifecycleWrite(t, db, lifecycleMarkLeftSQL(first), lifecycleLeaveReceiptSQL(first, "leave-1"),
		lifecycleProofSQL(first, "proof-1"))
	finished, err := callLifecycleReplica(t, db, lifecycleFinishExpr(5, "op-scale-in", first, "leave-1", "proof-1"))
	if err != nil {
		t.Fatal(err)
	}
	if finished.state != "left" || finished.controllerGeneration != 6 || finished.replayed {
		t.Fatalf("finish scale in=%+v", finished)
	}
	if finished.desired == nil || *finished.desired != 1 || finished.serving == nil || *finished.serving != 1 ||
		finished.maintenance == nil || *finished.maintenance != 0 ||
		finished.partition1 == nil || !*finished.partition1 || finished.partition2 == nil || *finished.partition2 {
		t.Fatalf("finish scale in pointers=%+v", finished)
	}
	capacity = lifecycleCapacity(t, db)
	one, two := lifecyclePartition(t, db, 1), lifecyclePartition(t, db, 2)
	if capacity.desired != 1 || one.enabled != 24 || one.operation != "op-initial" ||
		two.enabled != 0 || two.distinctEvidence != 1 || two.operation != "op-scale-in" ||
		two.generation != capacity.generation {
		t.Fatalf("finish scale in capacity=%+v one=%+v two=%+v", capacity, one, two)
	}
	if lifecycleReplicaState(t, db, second.replicaID) != "active" {
		t.Fatal("finish scale in disturbed the survivor")
	}
}

// TestRuntimeLifecycleOperationsAbruptScaleIn proves the abrupt branch: a
// fenced target with its exact termination intent and EC2 proof, a null leave
// operation and zero surviving active serving replicas.
func TestRuntimeLifecycleOperationsAbruptScaleIn(t *testing.T) {
	db, _ := runtimeLifecycleDB(t)
	first, second := lifecycleTwoNodeFleet(t, db)

	if _, err := callLifecycleReplica(t, db, lifecycleScaleInExpr(4, "op-scale-in", first)); err != nil {
		t.Fatal(err)
	}
	terminated, err := callLifecycleReplica(t, db,
		lifecycleTerminateExpr(5, "op-terminate", "request-1", first, "drain_failed"))
	if err != nil {
		t.Fatal(err)
	}
	if terminated.state != "terminating" || terminated.controllerGeneration != 6 {
		t.Fatalf("termination=%+v", terminated)
	}
	lifecycleWrite(t, db, lifecycleMarkFencedSQL(first), lifecycleProofSQL(first, "proof-1"),
		lifecycleMarkFencedSQL(second), lifecycleProofSQL(second, "proof-2"))

	finished, err := callLifecycleReplica(t, db, lifecycleFinishExpr(6, "op-scale-in", first, "", "proof-1"))
	if err != nil {
		t.Fatal(err)
	}
	if finished.state != "fenced" || finished.controllerGeneration != 7 || finished.replayed {
		t.Fatalf("abrupt finish=%+v", finished)
	}
	if finished.desired == nil || *finished.desired != 1 || finished.serving == nil || *finished.serving != 0 ||
		finished.partition1 == nil || !*finished.partition1 || finished.partition2 == nil || *finished.partition2 {
		t.Fatalf("abrupt finish pointers=%+v", finished)
	}
	if two := lifecyclePartition(t, db, 2); two.enabled != 0 {
		t.Fatalf("abrupt finish partition 2=%+v", two)
	}
	// A later replacement recovers the fleet, and the zero-survivor result
	// still replays exactly as it was recorded.
	recovery := validRegistrationIdentity("3")
	lifecycleActivateServing(t, db, 7, "op-replace", recovery, second.replicaID)
	replayed, err := callLifecycleReplica(t, db, lifecycleFinishExpr(6, "op-scale-in", first, "", "proof-1"))
	if err != nil {
		t.Fatal(err)
	}
	if !replayed.replayed || replayed.serving == nil || *replayed.serving != 0 ||
		replayed.controllerGeneration != 7 || replayed.partition2 == nil || *replayed.partition2 {
		t.Fatalf("zero-survivor replay after recovery=%+v", replayed)
	}
}

// TestRuntimeLifecycleOperationsGracefulScaleInWithFencedSurvivor proves the
// survivor-dead race: the prepared target leaves gracefully while the other
// incarnation is already fenced, so the finish records zero active survivors.
func TestRuntimeLifecycleOperationsGracefulScaleInWithFencedSurvivor(t *testing.T) {
	db, _ := runtimeLifecycleDB(t)
	first, second := lifecycleTwoNodeFleet(t, db)
	if _, err := callLifecycleReplica(t, db, lifecycleScaleInExpr(4, "op-scale-in", first)); err != nil {
		t.Fatal(err)
	}
	lifecycleWrite(t, db, lifecycleMarkLeftSQL(first), lifecycleLeaveReceiptSQL(first, "leave-1"),
		lifecycleProofSQL(first, "proof-1"), lifecycleMarkFencedSQL(second), lifecycleProofSQL(second, "proof-2"))
	finished, err := callLifecycleReplica(t, db, lifecycleFinishExpr(5, "op-scale-in", first, "leave-1", "proof-1"))
	if err != nil {
		t.Fatal(err)
	}
	if finished.state != "left" || finished.serving == nil || *finished.serving != 0 ||
		finished.desired == nil || *finished.desired != 1 {
		t.Fatalf("survivor-dead finish=%+v", finished)
	}
	if two := lifecyclePartition(t, db, 2); two.enabled != 0 {
		t.Fatalf("survivor-dead finish partition 2=%+v", two)
	}
}

// TestRuntimeLifecycleOperationsMaintenanceCampaign proves maintenance
// activation under a completed maintenance wake and the maintenance drain,
// neither of which touches desired serving capacity or the rate partitions.
func TestRuntimeLifecycleOperationsMaintenanceCampaign(t *testing.T) {
	db, _ := runtimeLifecycleDB(t)
	node := validRegistrationIdentity("3")
	fixture := append(lifecycleWakeFixture("op-wake", "maintenance_wake"), lifecycleSetControllerGenerationSQL(3))
	lifecycleWrite(t, db, fixture...)
	lifecycleRegisterReady(t, db, "maintenance", node)

	activated, err := callLifecycleReplica(t, db, lifecycleActivateExpr(3, "op-wake", node, ""))
	if err != nil {
		t.Fatal(err)
	}
	if activated.kind != "maintenance" || activated.state != "active" || activated.controllerGeneration != 4 {
		t.Fatalf("maintenance activation=%+v", activated)
	}
	requireNilPointers(t, activated, "maintenance activation")
	capacity := lifecycleCapacity(t, db)
	one, two := lifecyclePartition(t, db, 1), lifecyclePartition(t, db, 2)
	if capacity.desired != 1 || one.enabled != 0 || two.enabled != 0 {
		t.Fatalf("maintenance activation changed capacity=%+v one=%+v two=%+v", capacity, one, two)
	}

	drained, err := callLifecycleReplica(t, db, lifecycleDrainExpr(4, "op-wake", node))
	if err != nil {
		t.Fatal(err)
	}
	if drained.kind != "maintenance" || drained.state != "draining" || drained.controllerGeneration != 5 || drained.replayed {
		t.Fatalf("maintenance drain=%+v", drained)
	}
	requireNilPointers(t, drained, "maintenance drain")
	capacity = lifecycleCapacity(t, db)
	one, two = lifecyclePartition(t, db, 1), lifecyclePartition(t, db, 2)
	if capacity.desired != 1 || one.enabled != 0 || two.enabled != 0 ||
		one.operation != "bootstrap-uncomposed-v1" || two.operation != "bootstrap-uncomposed-v1" {
		t.Fatalf("maintenance drain changed capacity=%+v one=%+v two=%+v", capacity, one, two)
	}
}

// TestRuntimeLifecycleOperationsTerminationReasons proves each reason accepts
// only its source state, that drain_failed additionally requires the exact
// prepare step, and that one immutable request-bound intent is recorded.
func TestRuntimeLifecycleOperationsTerminationReasons(t *testing.T) {
	t.Run("startup_failed_from_joining", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		node := validRegistrationIdentity("4")
		lifecycleRegisterReady(t, db, "serving", node)
		result, err := callLifecycleReplica(t, db, lifecycleTerminateExpr(1, "op-startup", "request-a", node, "startup_failed"))
		if err != nil {
			t.Fatal(err)
		}
		if result.state != "terminating" || result.controllerGeneration != 2 || result.replayed {
			t.Fatalf("startup termination=%+v", result)
		}
		requireNilPointers(t, result, "termination")
		var reason, request string
		if err := db.QueryRowContext(context.Background(), `SELECT reason,request_id FROM public.runtime_termination_intents WHERE replica_id=$1`, node.replicaID).Scan(&reason, &request); err != nil {
			t.Fatal(err)
		}
		if reason != "startup_failed" || request != "request-a" {
			t.Fatalf("intent reason=%s request=%s", reason, request)
		}
		if capacity := lifecycleCapacity(t, db); capacity.desired != 1 {
			t.Fatalf("termination changed desired=%+v", capacity)
		}
	})
	t.Run("readiness_failed_from_active", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		node := validRegistrationIdentity("5")
		lifecycleActivateServing(t, db, 1, "op-initial", node, "")
		result, err := callLifecycleReplica(t, db, lifecycleTerminateExpr(2, "op-ready", "request-b", node, "readiness_failed"))
		if err != nil {
			t.Fatal(err)
		}
		if result.state != "terminating" || result.controllerGeneration != 3 {
			t.Fatalf("readiness termination=%+v", result)
		}
		if one := lifecyclePartition(t, db, 1); one.enabled != 24 {
			t.Fatalf("termination disabled partition 1=%+v", one)
		}
	})
	t.Run("wrong_source_state_is_rejected", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		node := validRegistrationIdentity("6")
		lifecycleRegisterReady(t, db, "serving", node)
		for _, reason := range []string{"readiness_failed", "drain_failed"} {
			_, err := callLifecycleReplica(t, db, lifecycleTerminateExpr(1, "op-"+reason, "request-"+reason, node, reason))
			requireLifecycleError(t, err, "55000")
		}
		if _, _, intents := lifecycleStepCount(t, db); intents != 0 {
			t.Fatalf("rejected termination wrote intents=%d", intents)
		}
		if lifecycleReplicaState(t, db, node.replicaID) != "joining" {
			t.Fatal("rejected termination changed the replica")
		}
	})
	t.Run("drain_failed_requires_the_prepare_step", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		first, _ := lifecycleTwoNodeFleet(t, db)
		lifecycleWrite(t, db, fmt.Sprintf(`UPDATE public.runtime_replicas SET state='draining',draining_at=clock_timestamp() WHERE replica_id='%s'`, first.replicaID))
		_, err := callLifecycleReplica(t, db, lifecycleTerminateExpr(4, "op-drain-fail", "request-c", first, "drain_failed"))
		requireLifecycleError(t, err, "55000")
		if _, _, intents := lifecycleStepCount(t, db); intents != 0 {
			t.Fatalf("rejected drain_failed wrote intents=%d", intents)
		}
	})
}

// TestRuntimeLifecycleOperationsReplayIsImmutable proves an exact retry
// returns the stored historical projection with replayed true, mutates no row
// and advances no generation, including after later successful actions.
func TestRuntimeLifecycleOperationsReplayIsImmutable(t *testing.T) {
	db, _ := runtimeLifecycleDB(t)
	first, second := lifecycleTwoNodeFleet(t, db)
	before := lifecycleCapacity(t, db)

	replayedScaleOut, err := callLifecycleCapacity(t, db, lifecycleScaleOutExpr(2, "op-scale-out"))
	if err != nil {
		t.Fatal(err)
	}
	if !replayedScaleOut.replayed || replayedScaleOut.desired != 2 || replayedScaleOut.serving != 1 ||
		replayedScaleOut.partition2 || replayedScaleOut.controllerGen != 3 ||
		replayedScaleOut.controllerOperation != "op-scale-out" {
		t.Fatalf("scale out replay must return the historical row: %+v", replayedScaleOut)
	}
	replayedActivation, err := callLifecycleReplica(t, db, lifecycleActivateExpr(3, "op-scale-out", second, ""))
	if err != nil {
		t.Fatal(err)
	}
	if !replayedActivation.replayed || replayedActivation.controllerGeneration != 4 ||
		replayedActivation.state != "active" {
		t.Fatalf("activation replay=%+v", replayedActivation)
	}
	requireNilPointers(t, replayedActivation, "activation replay")
	replayedInitial, err := callLifecycleReplica(t, db, lifecycleActivateExpr(1, "op-initial", first, ""))
	if err != nil {
		t.Fatal(err)
	}
	if !replayedInitial.replayed || replayedInitial.controllerGeneration != 2 ||
		replayedInitial.capacityGeneration != 4 {
		t.Fatalf("initial activation replay=%+v", replayedInitial)
	}
	after := lifecycleCapacity(t, db)
	if after != before {
		t.Fatalf("replay changed capacity before=%+v after=%+v", before, after)
	}
	operations, steps, intents := lifecycleStepCount(t, db)
	if operations != 2 || steps != 3 || intents != 0 {
		t.Fatalf("replay changed the ledger operations=%d steps=%d intents=%d", operations, steps, intents)
	}

	// A prepared scale-in and a later drain must not change what the older
	// prepare-scale-out replay reports.
	if _, err = callLifecycleReplica(t, db, lifecycleScaleInExpr(4, "op-scale-in", first)); err != nil {
		t.Fatal(err)
	}
	again, err := callLifecycleCapacity(t, db, lifecycleScaleOutExpr(2, "op-scale-out"))
	if err != nil {
		t.Fatal(err)
	}
	if again != replayedScaleOut {
		t.Fatalf("replay after later actions changed=%+v want=%+v", again, replayedScaleOut)
	}
}

// TestRuntimeLifecycleOperationsReplayConflicts fixes the code for every
// supplied conflict, missing predecessor, stale generation and provable
// stored-row corruption.
func TestRuntimeLifecycleOperationsReplayConflicts(t *testing.T) {
	t.Run("changed_arguments_conflict", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		first, _ := lifecycleTwoNodeFleet(t, db)
		other := validRegistrationIdentity("2")
		if _, err := callLifecycleReplica(t, db, lifecycleScaleInExpr(4, "op-scale-in", first)); err != nil {
			t.Fatal(err)
		}
		_, err := callLifecycleReplica(t, db, lifecycleScaleInExpr(4, "op-scale-in", other))
		pgErr := lifecycleErrorOf(t, err, "AM002")
		requireNoLifecycleIdentityLeak(t, pgErr, other.replicaID, other.instanceID, other.release)
		_, err = callLifecycleReplica(t, db, lifecycleScaleInExpr(9, "op-scale-in", first))
		requireLifecycleError(t, err, "AM002")
	})
	t.Run("conflicting_workflow_for_one_operation_id", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		first, _ := lifecycleTwoNodeFleet(t, db)
		_, err := callLifecycleReplica(t, db, lifecycleScaleInExpr(4, "op-scale-out", first))
		requireLifecycleError(t, err, "AM002")
		_, err = callLifecycleCapacity(t, db, lifecycleScaleOutExpr(4, "op-initial"))
		requireLifecycleError(t, err, "AM002")
		_, err = callLifecycleReplica(t, db,
			lifecycleTerminateExpr(4, "op-scale-out", "request-x", first, "readiness_failed"))
		requireLifecycleError(t, err, "AM002")
	})
	t.Run("missing_predecessor_and_stale_generation", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		first, _ := lifecycleTwoNodeFleet(t, db)
		_, err := callLifecycleReplica(t, db, lifecycleFinishExpr(4, "op-orphan", first, "leave-1", "proof-1"))
		requireLifecycleError(t, err, "55000")
		_, err = callLifecycleReplica(t, db, lifecycleDrainExpr(4, "op-orphan", first))
		requireLifecycleError(t, err, "55000")
		lifecycleWrite(t, db, lifecycleParentSQL("op-bare-scale-in", "scale_in"))
		_, err = callLifecycleReplica(t, db, lifecycleFinishExpr(4, "op-bare-scale-in", first, "leave-1", "proof-1"))
		requireLifecycleError(t, err, "55000")
		_, err = callLifecycleReplica(t, db, lifecycleScaleInExpr(3, "op-stale", first))
		requireLifecycleError(t, err, "55000")
		_, err = callLifecycleReplica(t, db, lifecycleScaleInExpr(99, "op-ahead", first))
		requireLifecycleError(t, err, "55000")
	})
	t.Run("corrupt_result_digest_is_am001", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		lifecycleWrite(t, db, lifecycleParentSQL("op-corrupt", "scale_out"),
			lifecycleStepSQLWith("op-corrupt", "scale_out", "prepare_scale_out", 1,
				map[string]string{"argument_digest": goLifecycleArgumentHex(goScaleOutArguments("op-corrupt", 1)...)}))
		_, err := callLifecycleCapacity(t, db, lifecycleScaleOutExpr(1, "op-corrupt"))
		pgErr := lifecycleErrorOf(t, err, "AM001")
		requireNoLifecycleIdentityLeak(t, pgErr, "op-corrupt")
	})
	t.Run("impossible_ledger_row_is_am001", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		ghost := validRegistrationIdentity("7")
		stored := goLifecycleResultFields("op-ghost", "prepare_scale_in", 2, "replica", ghost.replicaID,
			"serving", "draining", 0, 0, 0, false, false, false, false, 2, "op-ghost", true, "online", "", 0, false)
		lifecycleWrite(t, db, lifecycleParentSQL("op-ghost", "scale_in"),
			lifecycleStepSQLWith("op-ghost", "scale_in", "prepare_scale_in", 1, map[string]string{
				"replica_id":      "'" + ghost.replicaID + "'",
				"argument_digest": goLifecycleArgumentHex(goScaleInArguments("op-ghost", 1, ghost)...),
				"result_digest":   goLifecycleResultHex(stored),
			}))
		_, err := callLifecycleReplica(t, db, lifecycleScaleInExpr(1, "op-ghost", ghost))
		requireLifecycleError(t, err, "AM001")

		// The same fixture with the incarnation registered replays cleanly, so
		// the AM001 above came from the impossible replica reference and not
		// from a mistyped result digest. The replayed state is the recorded
		// draining, never the current joining.
		lifecycleRegisterReady(t, db, "serving", ghost)
		replayed, replayErr := callLifecycleReplica(t, db, lifecycleScaleInExpr(1, "op-ghost", ghost))
		if replayErr != nil {
			t.Fatal(replayErr)
		}
		if !replayed.replayed || replayed.state != "draining" || replayed.controllerGeneration != 2 {
			t.Fatalf("historical replay=%+v", replayed)
		}
		if lifecycleReplicaState(t, db, ghost.replicaID) != "joining" {
			t.Fatal("replay mutated the replica")
		}
	})
	t.Run("well_shaped_stored_argument_digest_mismatch_is_am002", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		lifecycleWrite(t, db, lifecycleParentSQL("op-tamper", "scale_out"),
			lifecycleStepSQLWith("op-tamper", "scale_out", "prepare_scale_out", 1,
				map[string]string{"argument_digest": "decode(repeat('ab',32),'hex')"}))
		_, err := callLifecycleCapacity(t, db, lifecycleScaleOutExpr(1, "op-tamper"))
		requireLifecycleError(t, err, "AM002")
	})
}

// TestRuntimeLifecycleOperationsCatalogOwnersAndGrants proves every installed
// object is owner-owned, SECURITY DEFINER, search_path pinned and non-strict,
// that only lifecycle-command executes the six wrappers, and that no login
// reaches any helper.
func TestRuntimeLifecycleOperationsCatalogOwnersAndGrants(t *testing.T) {
	db, ctx := runtimeLifecycleDB(t)
	names := make([]string, 0, len(lifecycleHelperSignatures)+len(lifecycleOperationSignatures))
	for _, helper := range lifecycleHelperSignatures {
		names = append(names, helper.name)
	}
	for _, operation := range lifecycleOperationSignatures {
		names = append(names, operation.name)
	}
	var valid bool
	if err := db.QueryRowContext(ctx, `SELECT count(*)=$2 AND bool_and(pg_get_userbyid(proowner)='aboutme_runtime_owner' AND prosecdef AND proconfig=ARRAY['search_path=pg_catalog']::text[] AND NOT has_function_privilege('public',oid,'EXECUTE') AND NOT proisstrict) FROM pg_proc WHERE pronamespace='public'::regnamespace AND proname=ANY($1)`, names, len(names)).Scan(&valid); err != nil || !valid {
		t.Fatalf("catalog valid=%t error=%v", valid, err)
	}
	for name, want := range lifecycleFunctionVolatility {
		var volatility string
		if err := db.QueryRowContext(ctx, `SELECT COALESCE((SELECT provolatile::text FROM pg_proc WHERE pronamespace='public'::regnamespace AND proname=$1),'')`, name).Scan(&volatility); err != nil {
			t.Fatal(err)
		}
		if volatility != want {
			t.Errorf("function %s volatility=%q want=%q", name, volatility, want)
		}
	}
	for _, role := range []string{"aboutme_app", "aboutme_maintenance", "aboutme_lifecycle_command",
		"aboutme_fencing_proof", "aboutme_restore_verify", "aboutme_migrator"} {
		for _, operation := range lifecycleOperationSignatures {
			var execute bool
			if err := db.QueryRowContext(ctx, `SELECT has_function_privilege($1,'public.'||$2,'EXECUTE')`, role, operation.name+operation.signature).Scan(&execute); err != nil {
				t.Fatal(err)
			}
			if want := role == "aboutme_lifecycle_command"; execute != want {
				t.Errorf("%s %s execute=%t want=%t", role, operation.name, execute, want)
			}
		}
		for _, helper := range lifecycleHelperSignatures {
			var execute bool
			if err := db.QueryRowContext(ctx, `SELECT COALESCE(bool_or(has_function_privilege($1,oid,'EXECUTE')),false) FROM pg_proc WHERE pronamespace='public'::regnamespace AND proname=$2`, role, helper.name).Scan(&execute); err != nil {
				t.Fatal(err)
			}
			if execute {
				t.Errorf("%s reaches helper %s", role, helper.name)
			}
		}
	}
	var typesValid bool
	if err := db.QueryRowContext(ctx, `SELECT NOT has_type_privilege('public','public.runtime_replica_result','USAGE') AND NOT has_type_privilege('public','public.runtime_capacity_result','USAGE') AND has_type_privilege('aboutme_lifecycle_command','public.runtime_replica_result','USAGE') AND has_type_privilege('aboutme_lifecycle_command','public.runtime_capacity_result','USAGE')`).Scan(&typesValid); err != nil || !typesValid {
		t.Fatalf("composite privileges valid=%t error=%v", typesValid, err)
	}
	var body string
	if err := db.QueryRowContext(ctx, `SELECT prosrc FROM pg_proc WHERE oid=to_regprocedure('public.runtime_sample_lifecycle_time()')`).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(body) != "SELECT clock_timestamp()" {
		t.Fatalf("production lifecycle clock body=%q", body)
	}
}

// TestRuntimeLifecycleOperationsRoleMatrix proves the direct session_user check
// runs before any mutable read, so every other login, SET ROLE and the owner
// itself see only 42501.
func TestRuntimeLifecycleOperationsRoleMatrix(t *testing.T) {
	db, _ := runtimeLifecycleDB(t)
	node := validRegistrationIdentity("1")
	statements := map[string]string{
		"scale_out": `SELECT ` + lifecycleScaleOutExpr(1, "op-role"),
		"activate":  `SELECT ` + lifecycleActivateExpr(1, "op-role", node, ""),
		"scale_in":  `SELECT ` + lifecycleScaleInExpr(1, "op-role", node),
		"finish":    `SELECT ` + lifecycleFinishExpr(1, "op-role", node, "leave-1", "proof-1"),
		"drain":     `SELECT ` + lifecycleDrainExpr(1, "op-role", node),
		"terminate": `SELECT ` + lifecycleTerminateExpr(1, "op-role", "request-1", node, "startup_failed"),
	}
	for _, role := range []string{"aboutme_app", "aboutme_maintenance", "aboutme_fencing_proof",
		"aboutme_restore_verify", "aboutme_migrator", "aboutme_runtime_owner"} {
		for name, statement := range statements {
			t.Run(role+"_"+name, func(t *testing.T) {
				pgErr := lifecycleErrorOf(t, registrationRoleExec(t, db, role, statement), "42501")
				requireNoLifecycleIdentityLeak(t, pgErr, node.replicaID, node.instanceID, node.release, "proof-1", "leave-1")
			})
		}
	}
	for _, role := range []string{"aboutme_app", "aboutme_maintenance", "aboutme_lifecycle_command",
		"aboutme_fencing_proof", "aboutme_restore_verify", "aboutme_migrator"} {
		for name, statement := range map[string]string{
			"clock_helper":      `SELECT public.runtime_sample_lifecycle_time()`,
			"digest_helper":     `SELECT public.runtime_lifecycle_digest_field(3::smallint,decode('00','hex'))`,
			"vector_helper":     `SELECT public.runtime_lifecycle_assert_digest_vectors()`,
			"partition_helper":  `SELECT public.runtime_lifecycle_lock_partitions()`,
			"capacity_helper":   `SELECT public.runtime_lifecycle_advance_capacity('x',clock_timestamp(),NULL::smallint)`,
			"clock_replacement": `CREATE OR REPLACE FUNCTION public.runtime_sample_lifecycle_time() RETURNS timestamptz LANGUAGE sql AS $x$ SELECT '2000-01-01'::timestamptz $x$`,
			"capacity_dml":      `UPDATE public.runtime_capacity SET desired_replicas=2`,
			"replica_dml":       `UPDATE public.runtime_replicas SET state='active'`,
			"step_dml":          `DELETE FROM public.runtime_lifecycle_operation_steps`,
			"partition_dml":     `UPDATE public.shared_rate_partitions SET enabled=true`,
		} {
			t.Run(role+"_"+name, func(t *testing.T) {
				requireLifecycleError(t, registrationRoleExec(t, db, role, statement), "42501")
			})
		}
	}
	t.Run("set_role_is_not_a_substitute", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			if _, resetErr := conn.ExecContext(cleanup, `RESET ROLE`); resetErr != nil {
				t.Error(resetErr)
			}
			if closeErr := conn.Close(); closeErr != nil {
				t.Error(closeErr)
			}
		}()
		if _, err = conn.ExecContext(ctx, `SET ROLE aboutme_lifecycle_command`); err != nil {
			t.Fatal(err)
		}
		_, err = conn.ExecContext(ctx, statements["scale_out"])
		requireLifecycleError(t, err, "42501")
	})
	t.Run("lifecycle_without_write_entry_is_am001", func(t *testing.T) {
		requireLifecycleError(t, registrationRoleExec(t, db, "aboutme_lifecycle_command", statements["scale_out"]), "AM001")
	})
}

// TestRuntimeLifecycleOperationsInputMatrix proves every malformed scalar,
// nullable branch and source bound is 22023, that no diagnostic discloses a
// supplied value, and that a rejected call writes nothing.
func TestRuntimeLifecycleOperationsInputMatrix(t *testing.T) {
	db, _ := runtimeLifecycleDB(t)
	node := validRegistrationIdentity("1")
	other := validRegistrationIdentity("2")
	long := strings.Repeat("x", 129)
	badInstance := registrationIdentity{replicaID: node.replicaID, instanceID: "i-ZZZZZZZZZZZZZZZZZ", release: node.release}
	badRelease := registrationIdentity{replicaID: node.replicaID, instanceID: node.instanceID, release: "sha256:zz"}
	nilReplica := registrationIdentity{replicaID: lifecycleNilUUID, instanceID: node.instanceID, release: node.release}
	invalid := []struct{ name, expression, projection string }{
		{"null_generation", `public.runtime_prepare_scale_out(NULL,'op-a')`, lifecycleCapacitySQL},
		{"zero_generation", lifecycleScaleOutExpr(0, "op-a"), lifecycleCapacitySQL},
		{"negative_generation", lifecycleScaleOutExpr(-1, "op-a"), lifecycleCapacitySQL},
		{"null_operation", `public.runtime_prepare_scale_out(1,NULL)`, lifecycleCapacitySQL},
		{"empty_operation", lifecycleScaleOutExpr(1, ""), lifecycleCapacitySQL},
		{"long_operation", lifecycleScaleOutExpr(1, long), lifecycleCapacitySQL},
		{"unprintable_operation", `public.runtime_prepare_scale_out(1,'op'||chr(10))`, lifecycleCapacitySQL},
		{"nil_replica_activate", lifecycleActivateExpr(1, "op-a", nilReplica, ""), lifecycleReplicaSQL},
		{"null_replica_activate", `public.runtime_activate_replica_capacity(1,'op-a',NULL,'` + node.instanceID + `','a','b','c','d','` + node.release + `',NULL)`, lifecycleReplicaSQL},
		{"bad_instance_scale_in", lifecycleScaleInExpr(1, "op-a", badInstance), lifecycleReplicaSQL},
		{"bad_release_scale_in", lifecycleScaleInExpr(1, "op-a", badRelease), lifecycleReplicaSQL},
		{"nil_replica_scale_in", lifecycleScaleInExpr(1, "op-a", nilReplica), lifecycleReplicaSQL},
		{"nil_replacement", lifecycleActivateExpr(1, "op-a", node, lifecycleNilUUID), lifecycleReplicaSQL},
		{"self_replacement", lifecycleActivateExpr(1, "op-a", node, node.replicaID), lifecycleReplicaSQL},
		{"null_fencing_evidence", `public.runtime_finish_scale_in(1,'op-a','` + node.replicaID + `','` + node.instanceID + `','` + node.release + `','leave-1',NULL)`, lifecycleReplicaSQL},
		{"empty_fencing_evidence", lifecycleFinishExpr(1, "op-a", node, "leave-1", ""), lifecycleReplicaSQL},
		{"long_fencing_evidence", lifecycleFinishExpr(1, "op-a", node, "leave-1", long), lifecycleReplicaSQL},
		{"long_leave_operation", lifecycleFinishExpr(1, "op-a", node, long, "proof-1"), lifecycleReplicaSQL},
		{"null_request", `public.runtime_begin_replica_termination(1,'op-a',NULL,'` + node.replicaID + `','` + node.instanceID + `','` + node.release + `','startup_failed')`, lifecycleReplicaSQL},
		{"empty_request", lifecycleTerminateExpr(1, "op-a", "", node, "startup_failed"), lifecycleReplicaSQL},
		{"null_reason", `public.runtime_begin_replica_termination(1,'op-a','request-1','` + node.replicaID + `','` + node.instanceID + `','` + node.release + `',NULL)`, lifecycleReplicaSQL},
		{"unknown_reason", lifecycleTerminateExpr(1, "op-a", "request-1", node, "node_died"), lifecycleReplicaSQL},
		{"bad_instance_drain", lifecycleDrainExpr(1, "op-a", badInstance), lifecycleReplicaSQL},
		{"duplicate_task_arn", `public.runtime_activate_replica_capacity(1,'op-a','` + node.replicaID + `','` + node.instanceID + `','arn:c','arn:same','arn:same','arn:n','` + node.release + `',NULL)`, lifecycleReplicaSQL},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			var sink [16]any
			pgErr := lifecycleErrorOf(t, lifecycleCall(t, db, lifecycleRole, test.expression, test.projection,
				lifecycleSinkTargets(&sink, test.projection)...), "22023")
			requireNoLifecycleIdentityLeak(t, pgErr, node.replicaID, node.instanceID, node.release,
				other.replicaID, long, "proof-1", "leave-1", "request-1")
		})
	}
	operations, steps, intents := lifecycleStepCount(t, db)
	if operations != 0 || steps != 0 || intents != 0 {
		t.Fatalf("invalid input wrote operations=%d steps=%d intents=%d", operations, steps, intents)
	}
	capacity := lifecycleCapacity(t, db)
	if capacity.generation != 1 || capacity.controllerGeneration != 1 || capacity.desired != 1 {
		t.Fatalf("invalid input changed capacity=%+v", capacity)
	}
	if one, two := lifecyclePartition(t, db, 1), lifecyclePartition(t, db, 2); one.enabled != 0 || two.enabled != 0 {
		t.Fatalf("invalid input changed partitions one=%+v two=%+v", one, two)
	}
}

// TestRuntimeLifecycleOperationsReplacementBounds proves one and two bound
// replacements, the consume-once predecessor, the final slot bound and the
// rejection of a null-replacement first activation while partition 1 stays
// enabled after node loss.
func TestRuntimeLifecycleOperationsReplacementBounds(t *testing.T) {
	t.Run("one_replacement_changes_no_flag", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		first, _ := lifecycleTwoNodeFleet(t, db)
		lifecycleWrite(t, db, lifecycleMarkFencedSQL(first))
		replacement := validRegistrationIdentity("3")
		result := lifecycleActivateServing(t, db, 4, "op-replace-1", replacement, first.replicaID)
		if result.state != "active" || result.controllerGeneration != 5 {
			t.Fatalf("replacement=%+v", result)
		}
		capacity := lifecycleCapacity(t, db)
		one, two := lifecyclePartition(t, db, 1), lifecyclePartition(t, db, 2)
		if capacity.desired != 2 || one.enabled != 24 || two.enabled != 24 ||
			one.operation != "op-initial" || two.operation != "op-scale-out" {
			t.Fatalf("replacement changed capacity=%+v one=%+v two=%+v", capacity, one, two)
		}
		fourth := validRegistrationIdentity("4")
		lifecycleRegisterReady(t, db, "serving", fourth)
		_, err := callLifecycleReplica(t, db, lifecycleActivateExpr(5, "op-replace-again", fourth, first.replicaID))
		requireLifecycleError(t, err, "55000")
	})
	t.Run("two_bound_replacements_then_the_slot_bound", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		first, second := lifecycleTwoNodeFleet(t, db)
		lifecycleWrite(t, db, lifecycleMarkFencedSQL(first), lifecycleMarkFencedSQL(second))
		third := validRegistrationIdentity("3")
		fourth := validRegistrationIdentity("4")
		lifecycleActivateServing(t, db, 4, "op-replace-1", third, first.replicaID)
		lifecycleActivateServing(t, db, 5, "op-replace-2", fourth, second.replicaID)
		if serving := lifecycleCapacity(t, db); serving.desired != 2 {
			t.Fatalf("replacements changed desired=%+v", serving)
		}
		fifth := validRegistrationIdentity("5")
		lifecycleRegisterReady(t, db, "serving", fifth)
		_, err := callLifecycleReplica(t, db, lifecycleActivateExpr(6, "op-replace-3", fifth, first.replicaID))
		requireLifecycleError(t, err, "55000")
	})
	t.Run("replacement_needs_a_free_slot_and_a_fenced_predecessor", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		first := validRegistrationIdentity("1")
		lifecycleActivateServing(t, db, 1, "op-initial", first, "")
		lost := validRegistrationIdentity("2")
		lifecycleRegisterReady(t, db, "serving", lost)
		lifecycleWrite(t, db, lifecycleMarkFencedSQL(lost))
		candidate := validRegistrationIdentity("3")
		lifecycleRegisterReady(t, db, "serving", candidate)
		_, err := callLifecycleReplica(t, db, lifecycleActivateExpr(2, "op-replace-full", candidate, lost.replicaID))
		requireLifecycleError(t, err, "55000")
		_, err = callLifecycleReplica(t, db, lifecycleActivateExpr(2, "op-replace-live", candidate, first.replicaID))
		requireLifecycleError(t, err, "55000")
	})
	t.Run("retained_partition_one_forbids_a_null_replacement_activation", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		first := validRegistrationIdentity("1")
		lifecycleActivateServing(t, db, 1, "op-initial", first, "")
		lifecycleWrite(t, db, lifecycleMarkFencedSQL(first))
		successor := validRegistrationIdentity("2")
		lifecycleRegisterReady(t, db, "serving", successor)
		_, err := callLifecycleReplica(t, db, lifecycleActivateExpr(2, "op-fresh-initial", successor, ""))
		requireLifecycleError(t, err, "55000")
		result, err := callLifecycleReplica(t, db, lifecycleActivateExpr(2, "op-replacement", successor, first.replicaID))
		if err != nil {
			t.Fatal(err)
		}
		if result.state != "active" || result.controllerGeneration != 3 {
			t.Fatalf("replacement after node loss=%+v", result)
		}
		if one, two := lifecyclePartition(t, db, 1), lifecyclePartition(t, db, 2); one.enabled != 24 ||
			one.operation != "op-initial" || two.enabled != 0 {
			t.Fatalf("replacement changed flags one=%+v two=%+v", one, two)
		}
	})
}

// TestRuntimeLifecycleOperationsPartitionConsistency proves split or drifted
// partition evidence fails closed and that a flag change moves all 24 rows of
// its ordinal together.
func TestRuntimeLifecycleOperationsPartitionConsistency(t *testing.T) {
	t.Run("split_enabled_state_is_am001", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		first := validRegistrationIdentity("1")
		lifecycleRegisterReady(t, db, "serving", first)
		lifecycleWrite(t, db, `UPDATE public.shared_rate_partitions SET enabled=true WHERE partition=1 AND policy_id='resume.read'`)
		_, err := callLifecycleReplica(t, db, lifecycleActivateExpr(1, "op-initial", first, ""))
		pgErr := lifecycleErrorOf(t, err, "AM001")
		requireNoLifecycleIdentityLeak(t, pgErr, first.replicaID, "resume.read")
	})
	t.Run("drifted_operation_evidence_is_am001", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		first := validRegistrationIdentity("1")
		lifecycleRegisterReady(t, db, "serving", first)
		lifecycleWrite(t, db, `UPDATE public.shared_rate_partitions SET operation_id='drifted' WHERE partition=2 AND policy_id='resume.read'`)
		_, err := callLifecycleReplica(t, db, lifecycleActivateExpr(1, "op-initial", first, ""))
		requireLifecycleError(t, err, "AM001")
	})
	t.Run("drifted_capacity_generation_is_am001", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		first, _ := lifecycleTwoNodeFleet(t, db)
		lifecycleWrite(t, db, `UPDATE public.shared_rate_partitions SET capacity_generation=capacity_generation+1 WHERE partition=1 AND policy_id='mcp.user'`)
		_, err := callLifecycleReplica(t, db, lifecycleScaleInExpr(4, "op-scale-in", first))
		requireLifecycleError(t, err, "AM001")
	})
}

// TestRuntimeLifecycleOperationsBlockersAndSourceStates proves the unfenced
// terminating incarnation, the visible closing transition and every rejected
// source state, and that maintenance drain deliberately ignores the serving
// blockers its own contract omits.
func TestRuntimeLifecycleOperationsBlockersAndSourceStates(t *testing.T) {
	t.Run("unfenced_terminating_blocks_serving_actions", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		first, second := lifecycleTwoNodeFleet(t, db)
		if _, err := callLifecycleReplica(t, db,
			lifecycleTerminateExpr(4, "op-terminate", "request-1", second, "readiness_failed")); err != nil {
			t.Fatal(err)
		}
		_, err := callLifecycleReplica(t, db, lifecycleScaleInExpr(5, "op-scale-in", first))
		requireLifecycleError(t, err, "55000")
		third := validRegistrationIdentity("3")
		lifecycleRegisterReady(t, db, "serving", third)
		_, err = callLifecycleReplica(t, db, lifecycleActivateExpr(5, "op-activate-blocked", third, ""))
		requireLifecycleError(t, err, "55000")
	})
	t.Run("visible_closing_transition_blocks_serving_actions", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		first := validRegistrationIdentity("1")
		lifecycleActivateServing(t, db, 1, "op-initial", first, "")
		second := validRegistrationIdentity("2")
		lifecycleRegisterReady(t, db, "serving", second)
		lifecycleWrite(t, db, transitionFixture()...)
		_, err := callLifecycleCapacity(t, db, lifecycleScaleOutExpr(2, "op-scale-out"))
		requireLifecycleError(t, err, "55000")
		_, err = callLifecycleReplica(t, db, lifecycleActivateExpr(2, "op-activate-blocked", second, ""))
		requireLifecycleError(t, err, "55000")
	})
	t.Run("scale_out_requires_exactly_one_active_serving_at_desired_one", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		_, err := callLifecycleCapacity(t, db, lifecycleScaleOutExpr(1, "op-scale-out-empty"))
		requireLifecycleError(t, err, "55000")
		first, _ := lifecycleTwoNodeFleet(t, db)
		_, err = callLifecycleCapacity(t, db, lifecycleScaleOutExpr(4, "op-scale-out-again"))
		requireLifecycleError(t, err, "55000")
		if state := lifecycleReplicaState(t, db, first.replicaID); state != "active" {
			t.Fatalf("rejected scale out changed the fleet state=%s", state)
		}
	})
	t.Run("activation_requires_a_joining_and_ready_incarnation", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		unready := validRegistrationIdentity("1")
		if _, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", unready); err != nil {
			t.Fatal(err)
		}
		_, err := callLifecycleReplica(t, db, lifecycleActivateExpr(1, "op-initial", unready, ""))
		requireLifecycleError(t, err, "55000")
		absent := validRegistrationIdentity("9")
		_, err = callLifecycleReplica(t, db, lifecycleActivateExpr(1, "op-absent", absent, ""))
		requireLifecycleError(t, err, "55000")
		mismatch := registrationIdentity{
			replicaID: unready.replicaID, instanceID: unready.instanceID, containerARN: "arn:other",
			caddyARN: unready.caddyARN, goARN: unready.goARN, nuxtARN: unready.nuxtARN, release: unready.release,
		}
		_, err = callLifecycleReplica(t, db, lifecycleActivateExpr(1, "op-mismatch", mismatch, ""))
		requireLifecycleError(t, err, "AM002")
	})
	t.Run("scale_in_accepts_either_target_and_rejects_a_second_drain", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		_, second := lifecycleTwoNodeFleet(t, db)
		drained, err := callLifecycleReplica(t, db, lifecycleScaleInExpr(4, "op-scale-in-second", second))
		if err != nil {
			t.Fatal(err)
		}
		if drained.replicaID != second.replicaID || drained.state != "draining" {
			t.Fatalf("either target must be selectable: %+v", drained)
		}
		first := validRegistrationIdentity("1")
		_, err = callLifecycleReplica(t, db, lifecycleScaleInExpr(5, "op-scale-in-first", first))
		requireLifecycleError(t, err, "55000")
	})
	t.Run("maintenance_activation_rejects_another_nonterminal_incarnation", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		blocker := validRegistrationIdentity("8")
		node := validRegistrationIdentity("3")
		lifecycleWrite(t, db, append(lifecycleWakeFixture("op-wake", "maintenance_wake"),
			lifecycleSetControllerGenerationSQL(3))...)
		lifecycleRegisterReady(t, db, "maintenance", blocker)
		lifecycleRegisterReady(t, db, "maintenance", node)
		_, err := callLifecycleReplica(t, db, lifecycleActivateExpr(3, "op-wake", node, ""))
		requireLifecycleError(t, err, "55000")
		lifecycleWrite(t, db, lifecycleMarkFencedSQL(blocker))
		result, err := callLifecycleReplica(t, db, lifecycleActivateExpr(3, "op-wake", node, ""))
		if err != nil {
			t.Fatal(err)
		}
		if result.kind != "maintenance" || result.state != "active" {
			t.Fatalf("maintenance activation after retained fenced history=%+v", result)
		}
	})
	t.Run("maintenance_wake_cannot_activate_serving", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		serving := validRegistrationIdentity("3")
		lifecycleWrite(t, db, append(lifecycleWakeFixture("op-wake", "maintenance_wake"),
			lifecycleSetControllerGenerationSQL(3))...)
		lifecycleRegisterReady(t, db, "serving", serving)
		_, err := callLifecycleReplica(t, db, lifecycleActivateExpr(3, "op-wake", serving, ""))
		requireLifecycleError(t, err, "55000")
	})
	t.Run("serving_wake_cannot_activate_maintenance", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		node := validRegistrationIdentity("3")
		lifecycleWrite(t, db, append(lifecycleWakeFixture("op-wake", "uat_serving_wake"),
			lifecycleSetControllerGenerationSQL(3))...)
		lifecycleRegisterReady(t, db, "maintenance", node)
		_, err := callLifecycleReplica(t, db, lifecycleActivateExpr(3, "op-wake", node, ""))
		requireLifecycleError(t, err, "55000")
	})
}

// TestRuntimeLifecycleOperationsFinishEvidenceMatrix proves both finish
// branches require their exact evidence, and that a missing reference is
// unavailable state while a mismatched one conflicts with immutable identity.
func TestRuntimeLifecycleOperationsFinishEvidenceMatrix(t *testing.T) {
	prepare := func(t *testing.T) (*sql.DB, registrationIdentity, registrationIdentity) {
		t.Helper()
		db, _ := runtimeLifecycleDB(t)
		first, second := lifecycleTwoNodeFleet(t, db)
		if _, err := callLifecycleReplica(t, db, lifecycleScaleInExpr(4, "op-scale-in", first)); err != nil {
			t.Fatal(err)
		}
		return db, first, second
	}
	t.Run("graceful_requires_a_left_target", func(t *testing.T) {
		db, first, _ := prepare(t)
		lifecycleWrite(t, db, lifecycleLeaveReceiptSQL(first, "leave-1"), lifecycleProofSQL(first, "proof-1"))
		_, err := callLifecycleReplica(t, db, lifecycleFinishExpr(5, "op-scale-in", first, "leave-1", "proof-1"))
		requireLifecycleError(t, err, "55000")
	})
	t.Run("graceful_requires_the_exact_leave_operation", func(t *testing.T) {
		db, first, _ := prepare(t)
		lifecycleWrite(t, db, lifecycleMarkLeftSQL(first), lifecycleLeaveReceiptSQL(first, "leave-1"),
			lifecycleProofSQL(first, "proof-1"))
		_, err := callLifecycleReplica(t, db, lifecycleFinishExpr(5, "op-scale-in", first, "leave-other", "proof-1"))
		pgErr := lifecycleErrorOf(t, err, "AM002")
		requireNoLifecycleIdentityLeak(t, pgErr, "leave-other", "leave-1", "proof-1")
		_, err = callLifecycleReplica(t, db, lifecycleFinishExpr(5, "op-scale-in", first, "leave-1", "proof-other"))
		requireLifecycleError(t, err, "AM002")
	})
	t.Run("graceful_requires_a_present_receipt_and_proof", func(t *testing.T) {
		db, first, _ := prepare(t)
		lifecycleWrite(t, db, lifecycleMarkLeftSQL(first))
		_, err := callLifecycleReplica(t, db, lifecycleFinishExpr(5, "op-scale-in", first, "leave-1", "proof-1"))
		requireLifecycleError(t, err, "55000")
		lifecycleWrite(t, db, lifecycleLeaveReceiptSQL(first, "leave-1"))
		_, err = callLifecycleReplica(t, db, lifecycleFinishExpr(5, "op-scale-in", first, "leave-1", "proof-1"))
		requireLifecycleError(t, err, "55000")
	})
	t.Run("abrupt_requires_a_fenced_target_and_an_intent", func(t *testing.T) {
		db, first, _ := prepare(t)
		lifecycleWrite(t, db, lifecycleMarkLeftSQL(first), lifecycleProofSQL(first, "proof-1"))
		_, err := callLifecycleReplica(t, db, lifecycleFinishExpr(5, "op-scale-in", first, "", "proof-1"))
		requireLifecycleError(t, err, "55000")

		db, first, _ = prepare(t)
		lifecycleWrite(t, db, lifecycleMarkFencedSQL(first), lifecycleProofSQL(first, "proof-1"))
		_, err = callLifecycleReplica(t, db, lifecycleFinishExpr(5, "op-scale-in", first, "", "proof-1"))
		requireLifecycleError(t, err, "55000")
	})
	t.Run("finish_requires_the_prepared_target_and_terminal_others", func(t *testing.T) {
		db, first, second := prepare(t)
		lifecycleWrite(t, db, lifecycleMarkLeftSQL(first), lifecycleLeaveReceiptSQL(first, "leave-1"),
			lifecycleProofSQL(first, "proof-1"))
		_, err := callLifecycleReplica(t, db, lifecycleFinishExpr(5, "op-scale-in", second, "leave-1", "proof-1"))
		requireLifecycleError(t, err, "AM002")
		joining := validRegistrationIdentity("3")
		lifecycleRegisterReady(t, db, "serving", joining)
		_, err = callLifecycleReplica(t, db, lifecycleFinishExpr(5, "op-scale-in", first, "leave-1", "proof-1"))
		requireLifecycleError(t, err, "55000")
	})
	t.Run("no_partial_change_on_a_rejected_finish", func(t *testing.T) {
		db, first, _ := prepare(t)
		before := lifecycleCapacity(t, db)
		lifecycleWrite(t, db, lifecycleMarkLeftSQL(first))
		_, err := callLifecycleReplica(t, db, lifecycleFinishExpr(5, "op-scale-in", first, "leave-1", "proof-1"))
		requireLifecycleError(t, err, "55000")
		after := lifecycleCapacity(t, db)
		if after.desired != before.desired || after.controllerGeneration != before.controllerGeneration ||
			after.generation != before.generation {
			t.Fatalf("rejected finish changed capacity before=%+v after=%+v", before, after)
		}
		if two := lifecyclePartition(t, db, 2); two.enabled != 24 {
			t.Fatalf("rejected finish changed partition 2=%+v", two)
		}
	})
}

// TestRuntimeLifecycleOperationsPreserveRateDebt proves every lifecycle flag
// change preserves ordinary buckets, overflow rows, pending P22 attempts and
// the active-key counts of both ordinals.
func TestRuntimeLifecycleOperationsPreserveRateDebt(t *testing.T) {
	db, ctx := runtimeLifecycleDB(t)
	lifecycleWrite(t, db,
		rateBucketSQL("api.outer_request", "31", 1, nil),
		rateBucketSQL("api.outer_request", "32", 2, nil),
		rateBucketSQL("resume.slug_change", "33", 1, nil),
		rateBucketSQL("oauth.failed_grant", "34", 1, map[string]string{"window_started_at": "transaction_timestamp()"}),
		pendingAttemptSQL("21212121-2121-4121-8121-212121212122", "private", "34", 1,
			storedPendingWindowOverrides("private", "34")),
		`UPDATE public.shared_rate_overflow SET token_numerator=17 WHERE policy_id='api.outer_request'`)
	const debtStatement = `SELECT (SELECT count(*) FROM public.shared_rate_buckets),(SELECT sum(active_keys) FROM public.shared_rate_partitions),(SELECT count(*) FROM public.shared_admission_attempts WHERE state='pending'),(SELECT token_numerator FROM public.shared_rate_overflow WHERE policy_id='api.outer_request'),(SELECT count(*) FROM public.shared_rate_overflow)`
	var before, after [5]int64
	scan := func(target *[5]int64) {
		t.Helper()
		if err := db.QueryRowContext(ctx, debtStatement).Scan(&target[0], &target[1], &target[2], &target[3], &target[4]); err != nil {
			t.Fatal(err)
		}
	}
	scan(&before)
	if before[0] != 4 || before[1] != 4 || before[2] != 1 || before[3] != 17 || before[4] != 24 {
		t.Fatalf("debt fixture=%v", before)
	}
	first, _ := lifecycleTwoNodeFleet(t, db)
	if _, err := callLifecycleReplica(t, db, lifecycleScaleInExpr(4, "op-scale-in", first)); err != nil {
		t.Fatal(err)
	}
	lifecycleWrite(t, db, lifecycleMarkLeftSQL(first), lifecycleLeaveReceiptSQL(first, "leave-1"),
		lifecycleProofSQL(first, "proof-1"))
	if _, err := callLifecycleReplica(t, db, lifecycleFinishExpr(5, "op-scale-in", first, "leave-1", "proof-1")); err != nil {
		t.Fatal(err)
	}
	scan(&after)
	if after != before {
		t.Fatalf("lifecycle changed rate debt before=%v after=%v", before, after)
	}
	if two := lifecyclePartition(t, db, 2); two.enabled != 0 {
		t.Fatalf("finish left partition 2 enabled=%+v", two)
	}
}

// TestRuntimeLifecycleOperationsRacesSerialize proves competing controller
// work serializes on the capacity singleton and the partition rows, that the
// exact loser is rejected, and that a held transition parent is never awaited.
func TestRuntimeLifecycleOperationsRacesSerialize(t *testing.T) {
	t.Run("two_fresh_operation_ids_serialize_on_capacity", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		lifecycleActivateServing(t, db, 1, "op-initial", validRegistrationIdentity("1"), "")
		holder := openLifecycleHolder(t, db, lifecycleScaleOutExpr(2, "op-winner"), lifecycleCapacitySQL)
		err := raceLifecycleOperation(t, db, holder.commit, lifecycleScaleOutExpr(2, "op-loser"), lifecycleCapacitySQL)
		requireLifecycleError(t, err, "55000")
		capacity := lifecycleCapacity(t, db)
		if capacity.controllerOperation != "op-winner" || capacity.desired != 2 || capacity.controllerGeneration != 3 {
			t.Fatalf("race winner=%+v", capacity)
		}
		operations, steps, _ := lifecycleStepCount(t, db)
		if operations != 2 || steps != 2 {
			t.Fatalf("loser left ledger rows operations=%d steps=%d", operations, steps)
		}
	})
	t.Run("a_rolled_back_holder_frees_the_waiter", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		lifecycleActivateServing(t, db, 1, "op-initial", validRegistrationIdentity("1"), "")
		holder := openLifecycleHolder(t, db, lifecycleScaleOutExpr(2, "op-abandoned"), lifecycleCapacitySQL)
		if err := raceLifecycleOperation(t, db, holder.rollback, lifecycleScaleOutExpr(2, "op-survivor"), lifecycleCapacitySQL); err != nil {
			t.Fatal(err)
		}
		capacity := lifecycleCapacity(t, db)
		if capacity.controllerOperation != "op-survivor" || capacity.desired != 2 || capacity.controllerGeneration != 3 {
			t.Fatalf("rolled back holder=%+v", capacity)
		}
		operations, steps, _ := lifecycleStepCount(t, db)
		if operations != 2 || steps != 2 {
			t.Fatalf("rolled back holder left ledger rows operations=%d steps=%d", operations, steps)
		}
	})
	t.Run("final_serving_activation_slot_has_one_winner", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		first := validRegistrationIdentity("1")
		second := validRegistrationIdentity("2")
		lifecycleRegisterReady(t, db, "serving", first)
		lifecycleRegisterReady(t, db, "serving", second)
		holder := openLifecycleHolder(t, db, lifecycleActivateExpr(1, "op-first", first, ""), lifecycleReplicaSQL)
		err := raceLifecycleOperation(t, db, holder.commit, lifecycleActivateExpr(1, "op-second", second, ""), lifecycleReplicaSQL)
		requireLifecycleError(t, err, "55000")
		if lifecycleReplicaState(t, db, first.replicaID) != "active" ||
			lifecycleReplicaState(t, db, second.replicaID) != "joining" {
			t.Fatal("activation race produced two active serving replicas")
		}
		if one := lifecyclePartition(t, db, 1); one.enabled != 24 || one.operation != "op-first" {
			t.Fatalf("activation race partition 1=%+v", one)
		}
	})
	t.Run("replacement_predecessor_is_consumed_once", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		first, _ := lifecycleTwoNodeFleet(t, db)
		lifecycleWrite(t, db, lifecycleMarkFencedSQL(first))
		third := validRegistrationIdentity("3")
		fourth := validRegistrationIdentity("4")
		lifecycleRegisterReady(t, db, "serving", third)
		lifecycleRegisterReady(t, db, "serving", fourth)
		holder := openLifecycleHolder(t, db, lifecycleActivateExpr(4, "op-replace-a", third, first.replicaID), lifecycleReplicaSQL)
		err := raceLifecycleOperation(t, db, holder.commit,
			lifecycleActivateExpr(4, "op-replace-b", fourth, first.replicaID), lifecycleReplicaSQL)
		requireLifecycleError(t, err, "55000")
		if lifecycleReplicaState(t, db, third.replicaID) != "active" ||
			lifecycleReplicaState(t, db, fourth.replicaID) != "joining" {
			t.Fatal("both replacements consumed one predecessor")
		}
	})
	t.Run("rate_allocation_and_a_flag_change_serialize_on_partitions", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		first, _ := lifecycleTwoNodeFleet(t, db)
		if _, err := callLifecycleReplica(t, db, lifecycleScaleInExpr(4, "op-scale-in", first)); err != nil {
			t.Fatal(err)
		}
		lifecycleWrite(t, db, lifecycleMarkLeftSQL(first), lifecycleLeaveReceiptSQL(first, "leave-1"),
			lifecycleProofSQL(first, "proof-1"))
		holder := openRateHolder(t, db, "aboutme_app", rateTokenExpression(ratePolicyToken, rateDigest(71)), rateDecisionProjection)
		if err := raceLifecycleOperation(t, db, holder.commit,
			lifecycleFinishExpr(5, "op-scale-in", first, "leave-1", "proof-1"), lifecycleReplicaSQL); err != nil {
			t.Fatal(err)
		}
		if two := lifecyclePartition(t, db, 2); two.enabled != 0 || two.distinctEvidence != 1 {
			t.Fatalf("finish after the rate holder=%+v", two)
		}
		var buckets int
		if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM public.shared_rate_buckets`).Scan(&buckets); err != nil {
			t.Fatal(err)
		}
		if buckets != 1 {
			t.Fatalf("flag change disturbed the allocated bucket count=%d", buckets)
		}
	})
	t.Run("a_held_transition_parent_is_never_awaited", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		first := validRegistrationIdentity("1")
		lifecycleActivateServing(t, db, 1, "op-initial", first, "")
		lifecycleWrite(t, db, transitionFixture()...)
		release := holdTransitionParent(t, db)
		defer release()
		settled := make(chan error, 1)
		go func() {
			_, err := callLifecycleCapacity(t, db, lifecycleScaleOutExpr(2, "op-scale-out"))
			settled <- err
		}()
		select {
		case err := <-settled:
			requireLifecycleError(t, err, "55000")
		case <-time.After(5 * time.Second):
			t.Fatal("lifecycle waited on a locked transition parent")
		}
	})
}

// TestRuntimeLifecycleOperationsSampledTimeIsClamped proves the owner-only
// lifecycle clock is sampled once after the locks, that its value reaches
// capacity and the ledger, and that a backward sample is clamped to the prior
// accepted evidence. Every probe rolls back.
func TestRuntimeLifecycleOperationsSampledTimeIsClamped(t *testing.T) {
	db, ctx := runtimeLifecycleDB(t)
	lifecycleActivateServing(t, db, 1, "op-initial", validRegistrationIdentity("1"), "")
	before := lifecycleCapacity(t, db)

	forward, recorded, err := lifecycleClockProbe(t, db, "2030-01-01 00:00:00+00",
		lifecycleScaleOutExpr(2, "op-forward"), lifecycleCapacitySQL)
	if err != nil {
		t.Fatal(err)
	}
	if forward.UTC().Year() != 2030 || !recorded.Equal(forward) {
		t.Fatalf("forward sample capacity=%s recorded=%s", forward, recorded)
	}

	backward, backwardRecorded, err := lifecycleClockProbe(t, db, "2000-01-01 00:00:00+00",
		lifecycleScaleOutExpr(2, "op-backward"), lifecycleCapacitySQL)
	if err != nil {
		t.Fatal(err)
	}
	if backward.Before(before.updatedAt) || backward.UTC().Year() == 2000 || !backwardRecorded.Equal(backward) {
		t.Fatalf("backward sample must clamp: capacity=%s recorded=%s prior=%s", backward, backwardRecorded, before.updatedAt)
	}

	after := lifecycleCapacity(t, db)
	if after != before {
		t.Fatalf("clock probe leaked before=%+v after=%+v", before, after)
	}
	var body string
	if err := db.QueryRowContext(ctx, `SELECT prosrc FROM pg_proc WHERE oid=to_regprocedure('public.runtime_sample_lifecycle_time()')`).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(body) != "SELECT clock_timestamp()" {
		t.Fatalf("clock probe leaked the replacement body=%q", body)
	}
}

// TestRuntimeLifecycleOperationsPreservesPopulatedVersion21 upgrades a
// populated version 21 database explicitly to 22 and proves every business,
// membership, transition, claim, rate and receipt row survives, that only the
// write generation advances, and that the installed surface then works.
func TestRuntimeLifecycleOperationsPreservesPopulatedVersion21(t *testing.T) {
	db := newMigratedTestDatabase(t, 21)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `INSERT INTO public.users(id,email,name) VALUES('22222222-2222-4222-8222-222222222222','lifecycle-operations-preserve@example.test','lifecycle operations preserved')`); err != nil {
		t.Fatal(err)
	}
	preserved := append(sharedClaimVectorFixture(), transitionFixture()...)
	preserved = append(preserved, transitionAckSQL(initiatorID),
		`INSERT INTO public.runtime_lifecycle_operations(operation_id,workflow_kind) VALUES('lifecycle-operations-preserve','initial_serving')`,
		rateBucketSQL(ratePolicyToken, "31", 1, nil),
		rateBucketSQL(ratePolicyAttempt, "33", 1, map[string]string{"window_started_at": "transaction_timestamp()"}),
		pendingAttemptSQL("22222222-2222-4222-8222-222222222223", "private", "33", 1,
			storedPendingWindowOverrides("private", "33")))
	lifecycleWrite(t, db, preserved...)
	const countStatement = `SELECT s.generation,c.generation,c.controller_generation,(SELECT count(*) FROM public.runtime_replicas),(SELECT count(*) FROM public.runtime_replica_tasks),(SELECT count(*) FROM public.public_transitions),(SELECT count(*) FROM public.public_transition_acks),(SELECT count(*) FROM public.shared_claim_requests),(SELECT count(*) FROM public.shared_claim_scopes),(SELECT count(*) FROM public.shared_rate_policies),(SELECT count(*) FROM public.shared_rate_partitions),(SELECT count(*) FROM public.shared_rate_buckets),(SELECT count(*) FROM public.shared_admission_attempts),(SELECT count(*) FROM public.runtime_lifecycle_operations),(SELECT count(*) FROM public.users),(SELECT sum(active_keys) FROM public.shared_rate_partitions),(SELECT count(*) FROM public.shared_rate_partitions WHERE enabled) FROM public.runtime_write_state s CROSS JOIN public.runtime_capacity c WHERE s.singleton AND c.singleton`
	var before, after [17]int64
	scan := func(target *[17]int64) {
		t.Helper()
		if err := db.QueryRowContext(ctx, countStatement).Scan(&target[0], &target[1], &target[2], &target[3], &target[4],
			&target[5], &target[6], &target[7], &target[8], &target[9], &target[10], &target[11], &target[12],
			&target[13], &target[14], &target[15], &target[16]); err != nil {
			t.Fatal(err)
		}
	}
	scan(&before)
	if _, err := applyFS(ctx, db, runtimeTransitionFixtureFS(t, 22), LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	scan(&after)
	tail := func(values [17]int64) [16]int64 { var rest [16]int64; copy(rest[:], values[1:]); return rest }
	if after[0] != before[0]+1 || tail(after) != tail(before) || before[9] != 24 || before[10] != 48 || before[16] != 0 {
		t.Fatalf("preservation before=%v after=%v", before, after)
	}

	terminated, err := callLifecycleReplica(t, db, lifecycleTerminateExpr(1, "op-upgrade-terminate", "request-upgrade",
		registrationIdentity{replicaID: initiatorID, instanceID: initiatorInstance, release: "sha256:" + strings.Repeat("a", 64)},
		"startup_failed"))
	if err != nil {
		t.Fatal(err)
	}
	if terminated.state != "terminating" || terminated.controllerGeneration != 2 || terminated.replayed {
		t.Fatalf("upgraded surface=%+v", terminated)
	}
}

// TestRuntimeLifecycleOperationsFailedMigrationRollsBackFunctionsAndGeneration
// proves an injected failure leaves no function, grant or write-generation
// advance behind.
func TestRuntimeLifecycleOperationsFailedMigrationRollsBackFunctionsAndGeneration(t *testing.T) {
	db := newMigratedTestDatabase(t, 21)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	var before int64
	if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	fixture := runtimeTransitionFixtureFS(t, 22)
	file := fixture["00022_runtime_lifecycle_operations.sql"]
	anchor := "RESET ROLE;\nREVOKE CREATE"
	if file == nil || !strings.Contains(string(file.Data), anchor) {
		t.Fatal("migration 22 failure anchor is missing")
	}
	file.Data = []byte(strings.Replace(string(file.Data), anchor, "SELECT 1/0;\n"+anchor, 1))
	_, applyErr := applyFS(ctx, db, fixture, LocalAdminMigratorIdentity())
	requireLifecycleError(t, applyErr, "22012")
	names := make([]string, 0, len(lifecycleHelperSignatures)+len(lifecycleOperationSignatures))
	for _, helper := range lifecycleHelperSignatures {
		names = append(names, helper.name)
	}
	for _, operation := range lifecycleOperationSignatures {
		names = append(names, operation.name)
	}
	var after, functions int64
	var schemaCreate, lifecycleGrant bool
	if err := db.QueryRowContext(ctx, `SELECT generation,(SELECT count(*) FROM pg_proc WHERE pronamespace='public'::regnamespace AND proname=ANY($1)),has_schema_privilege('aboutme_runtime_owner','public','CREATE'),has_type_privilege('aboutme_lifecycle_command','public.runtime_replica_result','USAGE') FROM public.runtime_write_state WHERE singleton`, names).
		Scan(&after, &functions, &schemaCreate, &lifecycleGrant); err != nil {
		t.Fatal(err)
	}
	if after != before || functions != 0 || schemaCreate || lifecycleGrant {
		t.Fatalf("rollback generation=%d/%d functions=%d schemaCreate=%t typeGrant=%t", before, after, functions, schemaCreate, lifecycleGrant)
	}
}

// TestRuntimeLifecycleOperationsVectorAssertionIsLoadBearing proves the
// migration refuses to install when a pinned digest literal no longer matches
// what the installed helpers compute.
func TestRuntimeLifecycleOperationsVectorAssertionIsLoadBearing(t *testing.T) {
	db := newMigratedTestDatabase(t, 21)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	fixture := runtimeTransitionFixtureFS(t, 22)
	file := fixture["00022_runtime_lifecycle_operations.sql"]
	pinned := "97583a4d21b893730e66b7862246cc28922a0227c3cded0674bf10bd83d5414c"
	if file == nil || !strings.Contains(string(file.Data), pinned) {
		t.Fatal("migration 22 no longer pins the prepare_scale_out result vector")
	}
	file.Data = []byte(strings.Replace(string(file.Data), pinned, strings.Repeat("0", 64), 1))
	_, applyErr := applyFS(ctx, db, fixture, LocalAdminMigratorIdentity())
	requireLifecycleError(t, applyErr, "AM001")
	var functions int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_proc WHERE pronamespace='public'::regnamespace AND proname='runtime_prepare_scale_out'`).Scan(&functions); err != nil {
		t.Fatal(err)
	}
	if functions != 0 {
		t.Fatalf("a mismatched vector still installed the surface functions=%d", functions)
	}
}

// TestRuntimeLifecycleOperationsAdversarialBoundaries proves a hostile caller
// search path is inert, that bigint exhaustion is detected before addition,
// that an out-of-order predecessor is rejected, and that a mid-flight
// rejection leaves no parent, step, membership, generation or partition
// change behind.
func TestRuntimeLifecycleOperationsAdversarialBoundaries(t *testing.T) {
	t.Run("hostile_caller_search_path_is_inert", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		for _, statement := range []string{
			`CREATE SCHEMA hostile`,
			`GRANT USAGE ON SCHEMA hostile TO aboutme_lifecycle_command`,
			`CREATE TABLE hostile.runtime_capacity(singleton boolean)`,
			`CREATE TABLE hostile.shared_rate_partitions(policy_id text)`,
			`CREATE TABLE hostile.runtime_lifecycle_operation_steps(operation_id text)`,
			`GRANT SELECT,INSERT ON hostile.runtime_capacity,hostile.shared_rate_partitions,hostile.runtime_lifecycle_operation_steps TO aboutme_lifecycle_command`,
			`CREATE FUNCTION hostile.runtime_sample_lifecycle_time() RETURNS timestamptz LANGUAGE sql AS $x$ SELECT '1999-01-01 00:00:00+00'::timestamptz $x$`,
			`CREATE FUNCTION hostile.runtime_lifecycle_serving_count() RETURNS smallint LANGUAGE sql AS $x$ SELECT 9::smallint $x$`,
		} {
			if _, err := db.ExecContext(context.Background(), statement); err != nil {
				t.Fatal(err)
			}
		}
		node := validRegistrationIdentity("1")
		lifecycleRegisterReady(t, db, "serving", node)
		var result registrationResult
		if err := lifecycleCallWithSearchPath(t, db, lifecycleRole, "hostile,pg_temp,public",
			lifecycleActivateExpr(1, "op-initial", node, ""), lifecycleReplicaSQL,
			&result.replicaID, &result.kind, &result.state, &result.desired, &result.serving, &result.maintenance,
			&result.partition1, &result.partition2, &result.capacityGeneration, &result.controllerGeneration,
			&result.controllerOperation, &result.admission, &result.lifecycle, &result.replayed); err != nil {
			t.Fatal(err)
		}
		if result.state != "active" || result.controllerGeneration != 2 {
			t.Fatalf("hostile path result=%+v", result)
		}
		var decoys int
		if err := db.QueryRowContext(context.Background(), `SELECT (SELECT count(*) FROM hostile.runtime_capacity)+(SELECT count(*) FROM hostile.shared_rate_partitions)+(SELECT count(*) FROM hostile.runtime_lifecycle_operation_steps)`).Scan(&decoys); err != nil || decoys != 0 {
			t.Fatalf("hostile decoys=%d error=%v", decoys, err)
		}
		capacity := lifecycleCapacity(t, db)
		if capacity.updatedAt.UTC().Year() == 1999 {
			t.Fatalf("hostile clock reached capacity=%+v", capacity)
		}
		if one := lifecyclePartition(t, db, 1); one.enabled != 24 {
			t.Fatalf("hostile path prevented the real flag change=%+v", one)
		}
	})
	t.Run("generation_exhaustion_is_detected_before_addition", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		node := validRegistrationIdentity("1")
		lifecycleRegisterReady(t, db, "serving", node)
		lifecycleWrite(t, db, `UPDATE public.runtime_capacity SET controller_generation=9223372036854775807 WHERE singleton`)
		_, err := callLifecycleReplica(t, db, lifecycleActivateExpr(9223372036854775807, "op-exhausted", node, ""))
		requireLifecycleError(t, err, "55000")
		lifecycleWrite(t, db, `UPDATE public.runtime_capacity SET controller_generation=1,generation=9223372036854775807 WHERE singleton`)
		_, err = callLifecycleReplica(t, db, lifecycleActivateExpr(1, "op-exhausted-capacity", node, ""))
		requireLifecycleError(t, err, "55000")
		if state := lifecycleReplicaState(t, db, node.replicaID); state != "joining" {
			t.Fatalf("exhaustion changed the replica state=%s", state)
		}
		if one := lifecyclePartition(t, db, 1); one.enabled != 0 {
			t.Fatalf("exhaustion changed partition 1=%+v", one)
		}
	})
	t.Run("an_out_of_order_predecessor_is_rejected", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		first, _ := lifecycleTwoNodeFleet(t, db)
		lifecycleWrite(t, db, lifecycleParentSQL("op-ahead", "scale_in"),
			lifecycleStepSQLWith("op-ahead", "scale_in", "prepare_scale_in", 90,
				map[string]string{"replica_id": "'" + first.replicaID + "'"}))
		_, err := callLifecycleReplica(t, db, lifecycleFinishExpr(4, "op-ahead", first, "leave-1", "proof-1"))
		requireLifecycleError(t, err, "55000")
	})
	t.Run("a_rejected_action_leaves_no_parent_or_step", func(t *testing.T) {
		db, _ := runtimeLifecycleDB(t)
		node := validRegistrationIdentity("1")
		lifecycleRegisterReady(t, db, "serving", node)
		lifecycleWrite(t, db, lifecycleMarkFencedSQL(node))
		before := lifecycleCapacity(t, db)
		_, err := callLifecycleReplica(t, db, lifecycleActivateExpr(1, "op-doomed", node, ""))
		requireLifecycleError(t, err, "55000")
		operations, steps, intents := lifecycleStepCount(t, db)
		if operations != 0 || steps != 0 || intents != 0 {
			t.Fatalf("rejected action left operations=%d steps=%d intents=%d", operations, steps, intents)
		}
		after := lifecycleCapacity(t, db)
		if after != before {
			t.Fatalf("rejected action changed capacity before=%+v after=%+v", before, after)
		}
		if one, two := lifecyclePartition(t, db, 1), lifecyclePartition(t, db, 2); one.enabled != 0 || two.enabled != 0 {
			t.Fatalf("rejected action changed partitions one=%+v two=%+v", one, two)
		}
	})
}

// TestRuntimeLifecycleOperationsQuerySourceParses proves every query in the
// owned sqlc source names an installed function with the exact argument types
// and projects only real composite attributes, so generation cannot fail on a
// stale signature.
func TestRuntimeLifecycleOperationsQuerySourceParses(t *testing.T) {
	db, _ := runtimeLifecycleDB(t)
	source, err := os.ReadFile(filepath.Join("..", "sql", "runtime_lifecycle.sql"))
	if err != nil {
		t.Fatal(err)
	}
	placeholder := regexp.MustCompile(`sqlc\.n?arg\([a-z_0-9]+\)`)
	blocks := strings.Split(string(source), "-- name: ")
	wanted := map[string]bool{
		"RuntimePrepareScaleOut": true, "RuntimeActivateReplicaCapacity": true, "RuntimePrepareScaleIn": true,
		"RuntimeFinishScaleIn": true, "RuntimePrepareMaintenanceDrain": true, "RuntimeBeginReplicaTermination": true,
	}
	found := map[string]bool{}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	if _, err = conn.ExecContext(ctx, `SET SESSION AUTHORIZATION `+lifecycleRole); err != nil {
		t.Fatal(err)
	}
	index := 0
	for _, block := range blocks {
		trimmed := strings.TrimSpace(block)
		if trimmed == "" {
			continue
		}
		name := strings.Fields(trimmed)[0]
		found[name] = true
		body := strings.TrimSpace(trimmed[strings.Index(trimmed, "\n")+1:])
		if !strings.Contains(body, "AS MATERIALIZED") {
			t.Errorf("query %s does not use one MATERIALIZED function result", name)
		}
		index++
		statement := fmt.Sprintf("PREPARE lifecycle_parse_%d AS %s", index, placeholder.ReplaceAllString(body, "NULL"))
		if _, err = conn.ExecContext(ctx, statement); err != nil {
			t.Fatalf("query %s does not parse: %v", name, err)
		}
	}
	for name := range wanted {
		if !found[name] {
			t.Errorf("query %s is missing from the source", name)
		}
	}
	for name := range found {
		if !wanted[name] {
			t.Errorf("query %s is not part of this slice", name)
		}
	}
}
