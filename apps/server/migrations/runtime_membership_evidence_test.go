package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestRuntimeMembershipEvidenceSurfaceExists proves migration 00023 adds the
// two retained proof fields with their constraints, installs the three fixed
// evidence operations with their exact signatures, and leaves both result
// composites in their accepted field order and type.
func TestRuntimeMembershipEvidenceSurfaceExists(t *testing.T) {
	db, ctx := membershipEvidenceDB(t)

	var columns string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(string_agg(a.attname||':'||format_type(a.atttypid,a.atttypmod)||':'||a.attnotnull,',' ORDER BY a.attnum),'') FROM pg_attribute a WHERE a.attrelid='public.runtime_fencing_proofs'::regclass AND a.attnum>0 AND NOT a.attisdropped AND a.attname IN ('request_id','reclaimed_claim_count')`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if columns != membershipProofColumns {
		t.Errorf("runtime_fencing_proofs new columns=%q want=%q", columns, membershipProofColumns)
	}
	for _, constraint := range membershipProofConstraints {
		var kind string
		if err := db.QueryRowContext(ctx, `SELECT COALESCE((SELECT contype::text FROM pg_constraint WHERE conrelid='public.runtime_fencing_proofs'::regclass AND conname=$1),'')`, constraint.name).Scan(&kind); err != nil {
			t.Fatal(err)
		}
		if kind != constraint.kind {
			t.Errorf("constraint %s contype=%q want=%q", constraint.name, kind, constraint.kind)
		}
	}

	for _, composite := range membershipEvidenceComposites {
		var attributes string
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(string_agg(a.attname||':'||format_type(a.atttypid,a.atttypmod),',' ORDER BY a.attnum),'') FROM pg_type t JOIN pg_class c ON c.oid=t.typrelid JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped WHERE t.typnamespace='public'::regnamespace AND t.typname=$1`, composite.name).Scan(&attributes); err != nil {
			t.Fatal(err)
		}
		if attributes != composite.attributes {
			t.Errorf("type %s attributes=%q want=%q", composite.name, attributes, composite.attributes)
		}
	}

	for _, operation := range membershipEvidenceSignatures {
		var returns string
		if err := db.QueryRowContext(ctx, `SELECT COALESCE((SELECT n.nspname||'.'||t.typname FROM pg_proc p JOIN pg_type t ON t.oid=p.prorettype JOIN pg_namespace n ON n.oid=t.typnamespace WHERE p.oid=to_regprocedure('public.'||$1)),'')`, operation.name+operation.signature).Scan(&returns); err != nil {
			t.Fatal(err)
		}
		if returns != "public."+operation.result {
			t.Errorf("function %s%s returns=%q want=%q", operation.name, operation.signature, returns, "public."+operation.result)
		}
	}

	for _, helper := range membershipEvidenceHelperSignatures {
		var installed bool
		if err := db.QueryRowContext(ctx, `SELECT to_regprocedure('public.'||$1) IS NOT NULL`, helper.name+helper.signature).Scan(&installed); err != nil {
			t.Fatal(err)
		}
		if !installed {
			t.Errorf("helper %s%s is missing", helper.name, helper.signature)
		}
	}

	var clock string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE((SELECT format_type(prorettype,NULL)||' argc='||pronargs||' volatile='||provolatile::text FROM pg_proc WHERE oid=to_regprocedure('public.runtime_sample_membership_time()')),'')`).Scan(&clock); err != nil {
		t.Fatal(err)
	}
	if clock != "timestamp with time zone argc=0 volatile=v" {
		t.Errorf("runtime_sample_membership_time=%q want=%q", clock, "timestamp with time zone argc=0 volatile=v")
	}
	var body string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE((SELECT prosrc FROM pg_proc WHERE oid=to_regprocedure('public.runtime_sample_membership_time()')),'')`).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(body) != "SELECT clock_timestamp()" {
		t.Errorf("production membership clock body=%q", body)
	}
}

// TestRuntimeMembershipEvidenceCatalogOwnersAndGrants proves every installed
// object is owner-owned, SECURITY DEFINER, search_path-pinned and non-STRICT,
// that only each operation's own login role may execute it, and that no login
// reaches any owner helper.
func TestRuntimeMembershipEvidenceCatalogOwnersAndGrants(t *testing.T) {
	db, ctx := membershipEvidenceDB(t)
	names := membershipEvidenceNames()
	var valid bool
	if err := db.QueryRowContext(ctx, `SELECT count(*)=$2 AND bool_and(pg_get_userbyid(proowner)='aboutme_runtime_owner' AND prosecdef AND proconfig=ARRAY['search_path=pg_catalog']::text[] AND NOT has_function_privilege('public',oid,'EXECUTE') AND NOT proisstrict) FROM pg_proc WHERE pronamespace='public'::regnamespace AND proname=ANY($1)`, names, len(names)).Scan(&valid); err != nil || !valid {
		t.Fatalf("catalog valid=%t error=%v", valid, err)
	}
	for name, want := range membershipEvidenceVolatility {
		var volatility string
		if err := db.QueryRowContext(ctx, `SELECT COALESCE((SELECT provolatile::text FROM pg_proc WHERE pronamespace='public'::regnamespace AND proname=$1),'')`, name).Scan(&volatility); err != nil {
			t.Fatal(err)
		}
		if volatility != want {
			t.Errorf("function %s volatility=%q want=%q", name, volatility, want)
		}
	}
	for _, role := range membershipRoles {
		for _, operation := range membershipEvidenceSignatures {
			var execute bool
			if err := db.QueryRowContext(ctx, `SELECT has_function_privilege($1,'public.'||$2,'EXECUTE')`, role, operation.name+operation.signature).Scan(&execute); err != nil {
				t.Fatal(err)
			}
			if want := role == operation.role; execute != want {
				t.Errorf("%s %s execute=%t want=%t", role, operation.name, execute, want)
			}
		}
		for _, helper := range membershipEvidenceHelperSignatures {
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
	if err := db.QueryRowContext(ctx, `SELECT NOT has_type_privilege('public','public.runtime_leave_result','USAGE') AND NOT has_type_privilege('public','public.runtime_fence_result','USAGE') AND has_type_privilege('aboutme_app','public.runtime_leave_result','USAGE') AND has_type_privilege('aboutme_maintenance','public.runtime_leave_result','USAGE') AND has_type_privilege('aboutme_fencing_proof','public.runtime_fence_result','USAGE') AND NOT has_type_privilege('aboutme_fencing_proof','public.runtime_leave_result','USAGE') AND NOT has_type_privilege('aboutme_app','public.runtime_fence_result','USAGE')`).Scan(&typesValid); err != nil || !typesValid {
		t.Fatalf("composite privileges valid=%t error=%v", typesValid, err)
	}
}

// TestRuntimeMembershipEvidenceFencedReleaseStaysOwnerOnly proves the fenced
// claim release helper in both directions: no login can execute it, and no
// caller entry point can request fencing. The exact proof definer is its only
// installed caller.
func TestRuntimeMembershipEvidenceFencedReleaseStaysOwnerOnly(t *testing.T) {
	db, ctx := membershipEvidenceDB(t)

	// Direction one: the helper is unreachable by name from every real login.
	for _, role := range membershipRoles {
		var execute bool
		if err := db.QueryRowContext(ctx, `SELECT has_function_privilege($1,'public.runtime_release_fenced_replica_claims(uuid,timestamptz)','EXECUTE')`, role).Scan(&execute); err != nil {
			t.Fatal(err)
		}
		if execute {
			t.Errorf("%s holds EXECUTE on runtime_release_fenced_replica_claims", role)
		}
		requireRegistrationError(t, registrationRoleExec(t, db, role,
			`SELECT public.runtime_release_fenced_replica_claims('11111111-1111-4111-8111-111111111111',clock_timestamp())`), "42501")
		requireRegistrationError(t, registrationRoleExec(t, db, role,
			`SELECT public.runtime_sample_membership_time()`), "42501")
	}

	// Direction two: exactly one installed function calls the helper, it is
	// the exact proof definer, and no caller may ask any claim function for a
	// fenced release.
	var callers string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(string_agg(proname,',' ORDER BY proname),'') FROM pg_proc WHERE pronamespace='public'::regnamespace AND prosrc LIKE '%runtime_release_fenced_replica_claims%' AND proname<>'runtime_release_fenced_replica_claims'`).Scan(&callers); err != nil {
		t.Fatal(err)
	}
	if callers != "runtime_record_ec2_termination" {
		t.Fatalf("fenced release callers=%q want=%q", callers, "runtime_record_ec2_termination")
	}
	var reasonArgument bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_proc WHERE oid=to_regprocedure('public.runtime_release_fenced_replica_claims(uuid,timestamptz)') AND pronargs=2)`).Scan(&reasonArgument); err != nil {
		t.Fatal(err)
	}
	if !reasonArgument {
		t.Fatal("the fenced release helper no longer takes exactly the replica and effective time")
	}
	first, _ := lifecycleTwoNodeFleet(t, db)
	claims := membershipLiveClaims(t, db, first.replicaID, 61)
	digest := membershipClaimDigest(t, db, claims[1])
	requireClaimError(t, db, membershipAppRole,
		claimReleaseExpression(claims[1], first.replicaID, digest, "fenced"), "42501")
	requireClaimError(t, db, membershipMaintenanceRole,
		claimReleaseExpression(claims[1], first.replicaID, digest, "fenced"), "42501")
	if states := membershipClaimStates(t, db, first.replicaID); len(states) != 4 {
		t.Fatalf("forbidden fenced release changed claims=%v", states)
	}
}

// TestRuntimeMembershipEvidenceRoleMatrix proves the direct session_user check
// runs before any mutable read, so every other login, SET ROLE and the owner
// itself see only 42501, and that no login performs direct evidence DML.
func TestRuntimeMembershipEvidenceRoleMatrix(t *testing.T) {
	db, _ := membershipEvidenceDB(t)
	node := validRegistrationIdentity("1")
	statements := map[string]string{
		"serving_leave":     `SELECT ` + membershipServingLeaveExpr(node, 1, "op-role"),
		"maintenance_leave": `SELECT ` + membershipMaintenanceLeaveExpr(node, 1, "op-role"),
		"proof":             `SELECT ` + membershipValidProofExpr(node, "request-role", "evidence-role"),
	}
	allowed := map[string]string{
		"serving_leave":     membershipAppRole,
		"maintenance_leave": membershipMaintenanceRole,
		"proof":             membershipProofRole,
	}
	for _, role := range append(append([]string{}, membershipRoles...), "aboutme_runtime_owner") {
		for name, statement := range statements {
			if role == allowed[name] {
				continue
			}
			t.Run(role+"_"+name, func(t *testing.T) {
				pgErr := membershipErrorOf(t, registrationRoleExec(t, db, role, statement), "42501")
				requireNoClaimIdentityLeak(t, pgErr, node.replicaID, node.instanceID, node.release,
					"request-role", "evidence-role", "op-role")
			})
		}
	}
	for _, role := range membershipRoles {
		for name, statement := range map[string]string{
			"clock_helper":      `SELECT public.runtime_sample_membership_time()`,
			"lock_helper":       `SELECT public.runtime_membership_lock_singletons()`,
			"capacity_helper":   `SELECT public.runtime_membership_advance_capacity('x',clock_timestamp())`,
			"leave_helper":      `SELECT public.runtime_membership_finish_graceful_leave('serving','x','scale_in','prepare_scale_in','` + node.replicaID + `','` + node.instanceID + `','` + node.release + `',1,'op-role')`,
			"release_helper":    `SELECT public.runtime_release_fenced_replica_claims('` + node.replicaID + `',clock_timestamp())`,
			"clock_replacement": `CREATE OR REPLACE FUNCTION public.runtime_sample_membership_time() RETURNS timestamptz LANGUAGE sql AS $x$ SELECT '2000-01-01'::timestamptz $x$`,
			"receipt_dml":       `INSERT INTO public.runtime_leave_receipts(replica_id,instance_id,release_digest,controller_generation,operation_id,joined_transition_count,joined_claim_count) VALUES('` + node.replicaID + `','` + node.instanceID + `','` + node.release + `',1,'forged',0,0)`,
			"proof_dml":         `INSERT INTO public.runtime_fencing_proofs(replica_id,instance_id,release_digest,adapter,request_id,evidence_id,requested_at,observed_terminated_at,observed_state,reclaimed_claim_count) VALUES('` + node.replicaID + `','` + node.instanceID + `','` + node.release + `','ec2_terminated_v1','forged','forged',clock_timestamp(),clock_timestamp(),'terminated',0)`,
			"replica_dml":       `UPDATE public.runtime_replicas SET state='fenced'`,
			"capacity_dml":      `UPDATE public.runtime_capacity SET generation=generation+1`,
			"claim_dml":         `UPDATE public.shared_claim_requests SET state='released'`,
		} {
			t.Run(role+"_"+name, func(t *testing.T) {
				requireRegistrationError(t, registrationRoleExec(t, db, role, statement), "42501")
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
		if _, err = conn.ExecContext(ctx, `SET ROLE aboutme_fencing_proof`); err != nil {
			t.Fatal(err)
		}
		_, err = conn.ExecContext(ctx, statements["proof"])
		requireRegistrationError(t, err, "42501")
	})
	t.Run("evidence_without_write_entry_is_am001", func(t *testing.T) {
		requireRegistrationError(t, registrationRoleExec(t, db, membershipProofRole, statements["proof"]), "AM001")
		requireRegistrationError(t, registrationRoleExec(t, db, membershipAppRole, statements["serving_leave"]), "AM001")
	})
}

// TestRuntimeMembershipEvidenceProofColumnsAreConstrained proves the two
// retained proof fields carry their accepted bounds, request uniqueness and
// the existing row immutability.
func TestRuntimeMembershipEvidenceProofColumnsAreConstrained(t *testing.T) {
	db, _ := membershipEvidenceDB(t)
	first, second := lifecycleTwoNodeFleet(t, db)
	proofSQL := func(id registrationIdentity, request, evidence, count string) string {
		return fmt.Sprintf(`INSERT INTO public.runtime_fencing_proofs(replica_id,instance_id,release_digest,adapter,request_id,evidence_id,requested_at,observed_terminated_at,observed_state,reclaimed_claim_count) VALUES('%s','%s','%s','ec2_terminated_v1',%s,'%s',transaction_timestamp(),transaction_timestamp(),'terminated',%s)`,
			id.replicaID, id.instanceID, id.release, request, evidence, count)
	}
	for _, invalid := range []struct{ name, request, count, code string }{
		{"empty_request", `''`, "0", "23514"},
		{"long_request", `'` + membershipLongText() + `'`, "0", "23514"},
		{"unprintable_request", `'req'||chr(10)`, "0", "23514"},
		{"null_request", `NULL`, "0", "23502"},
		{"negative_count", `'request-ok'`, "-1", "23514"},
		{"null_count", `'request-ok'`, "NULL", "23502"},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			err := membershipWrite(t, db, proofSQL(first, invalid.request, "evidence-"+invalid.name, invalid.count))
			requireRegistrationError(t, err, invalid.code)
		})
	}
	membershipOwnerExec(t, db, proofSQL(first, `'request-unique'`, "evidence-unique", "3"))
	t.Run("request_id_is_unique_across_replicas", func(t *testing.T) {
		requireRegistrationError(t, membershipWrite(t, db, proofSQL(second, `'request-unique'`, "evidence-other", "0")), "23505")
	})
	t.Run("both_new_fields_are_immutable", func(t *testing.T) {
		requireRegistrationError(t, membershipWrite(t, db, `UPDATE public.runtime_fencing_proofs SET request_id='changed'`), "55000")
		requireRegistrationError(t, membershipWrite(t, db, `UPDATE public.runtime_fencing_proofs SET reclaimed_claim_count=9`), "55000")
	})
	stored := membershipProof(t, db, first.replicaID)
	if !stored.exists || stored.requestID != "request-unique" || stored.reclaimedClaimCount != 3 {
		t.Fatalf("stored proof=%+v", stored)
	}
}

// TestRuntimeMembershipEvidencePreservesPopulatedVersion22 proves the ALTER
// preserves every ordinary business, membership, transition, claim, rate and
// evidence row, advances only the write generation, and leaves a working
// evidence surface behind.
func TestRuntimeMembershipEvidencePreservesPopulatedVersion22(t *testing.T) {
	db := newMigratedTestDatabase(t, 22)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `INSERT INTO public.users(id,email,name) VALUES('22222222-2222-4222-8222-222222222222','membership-evidence-preserve@example.test','membership evidence preserved')`); err != nil {
		t.Fatal(err)
	}
	claimRelease := "sha256:" + strings.Repeat("2", 64)
	preserved := append(sharedClaimVectorFixture(), transitionFixture()...)
	preserved = append(preserved, transitionAckSQL(initiatorID),
		`INSERT INTO public.runtime_lifecycle_operations(operation_id,workflow_kind) VALUES('membership-evidence-preserve','initial_serving')`,
		fmt.Sprintf(`INSERT INTO public.runtime_termination_intents(replica_id,instance_id,release_digest,request_id,reason,requested_at) VALUES('%s','%s','sha256:%s','preserved-request','startup_failed',transaction_timestamp())`,
			initiatorID, initiatorInstance, strings.Repeat("a", 64)),
		fmt.Sprintf(`INSERT INTO public.runtime_leave_receipts(replica_id,instance_id,release_digest,controller_generation,operation_id,joined_transition_count,joined_claim_count) VALUES('%s','i-00000000000000002','%s',1,'preserved-leave',0,0)`,
			claimVectorReplica, claimRelease),
		rateBucketSQL(ratePolicyToken, "31", 1, nil),
		rateBucketSQL(ratePolicyAttempt, "33", 1, map[string]string{"window_started_at": "transaction_timestamp()"}),
		pendingAttemptSQL("22222222-2222-4222-8222-222222222223", "private", "33", 1,
			storedPendingWindowOverrides("private", "33")))
	lifecycleWrite(t, db, preserved...)
	before := scanMembershipCounts(ctx, t, db)
	if _, err := applyFS(ctx, db, runtimeTransitionFixtureFS(t, 23), LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	after := scanMembershipCounts(ctx, t, db)
	if after[0] != before[0]+1 {
		t.Fatalf("write generation before=%d after=%d", before[0], after[0])
	}
	for index := 1; index < len(before); index++ {
		if after[index] != before[index] {
			t.Fatalf("upgrade changed column %d before=%v after=%v", index, before, after)
		}
	}
	if before[17] != 0 || before[18] != 1 || before[19] != 1 {
		t.Fatalf("fixture evidence counts proofs=%d receipts=%d intents=%d", before[17], before[18], before[19])
	}
	var preservedRequest, preservedLeave string
	if err := db.QueryRowContext(ctx, `SELECT (SELECT request_id FROM public.runtime_termination_intents),(SELECT operation_id FROM public.runtime_leave_receipts)`).Scan(&preservedRequest, &preservedLeave); err != nil {
		t.Fatal(err)
	}
	if preservedRequest != "preserved-request" || preservedLeave != "preserved-leave" {
		t.Fatalf("preserved evidence request=%q leave=%q", preservedRequest, preservedLeave)
	}

	// The upgraded surface works on the preserved fleet: the claim-vector
	// replica is joining with one live two-scope claim, and exact proof fences
	// it and reclaims that one parent.
	target := registrationIdentity{replicaID: claimVectorReplica, instanceID: "i-00000000000000002", release: claimRelease}
	fenced := mustMembershipProof(t, db, membershipValidProofExpr(target, "upgrade-request", "upgrade-evidence"))
	if fenced.state != "fenced" || fenced.reclaimedClaimCount != 1 || fenced.replayed {
		t.Fatalf("upgraded surface=%+v", fenced)
	}
}

// TestRuntimeMembershipEvidenceRejectsLegacyProof proves a nonempty legacy
// proof table fails the upgrade closed, preserves that proof and every other
// row, and installs no column, function or generation advance.
func TestRuntimeMembershipEvidenceRejectsLegacyProof(t *testing.T) {
	db := newMigratedTestDatabase(t, 22)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	legacy := registrationIdentity{replicaID: initiatorID, instanceID: initiatorInstance, release: "sha256:" + strings.Repeat("a", 64)}
	lifecycleWrite(t, db, append(transitionFixture(), membershipLegacyProofSQL(legacy, "legacy-evidence"))...)
	before := scanMembershipCounts(ctx, t, db)
	var headBefore int64
	if err := db.QueryRowContext(ctx, `SELECT max(version_id) FILTER (WHERE is_applied) FROM public.goose_db_version`).Scan(&headBefore); err != nil {
		t.Fatal(err)
	}
	_, applyErr := applyFS(ctx, db, runtimeTransitionFixtureFS(t, 23), LocalAdminMigratorIdentity())
	pgErr := membershipErrorOf(t, applyErr, "AM001")
	requireNoClaimIdentityLeak(t, pgErr, "legacy-evidence", legacy.replicaID, legacy.instanceID, legacy.release)
	after := scanMembershipCounts(ctx, t, db)
	if after != before {
		t.Fatalf("rejected upgrade changed rows before=%v after=%v", before, after)
	}
	var headAfter int64
	var columns, functions int64
	var evidence string
	if err := db.QueryRowContext(ctx, `SELECT (SELECT max(version_id) FILTER (WHERE is_applied) FROM public.goose_db_version),(SELECT count(*) FROM pg_attribute WHERE attrelid='public.runtime_fencing_proofs'::regclass AND attnum>0 AND NOT attisdropped AND attname IN ('request_id','reclaimed_claim_count')),(SELECT count(*) FROM pg_proc WHERE pronamespace='public'::regnamespace AND proname=ANY($1)),(SELECT evidence_id FROM public.runtime_fencing_proofs)`, membershipEvidenceNames()).
		Scan(&headAfter, &columns, &functions, &evidence); err != nil {
		t.Fatal(err)
	}
	if headAfter != headBefore || columns != 0 || functions != 0 || evidence != "legacy-evidence" {
		t.Fatalf("rejected upgrade head=%d/%d columns=%d functions=%d evidence=%q", headBefore, headAfter, columns, functions, evidence)
	}
}

// TestRuntimeMembershipEvidenceFailedMigrationRollsBack proves an injected
// failure rolls the two columns, every function, the type grants and the write
// generation back together.
func TestRuntimeMembershipEvidenceFailedMigrationRollsBack(t *testing.T) {
	db := newMigratedTestDatabase(t, 22)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	var before int64
	if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	fixture := runtimeTransitionFixtureFS(t, 23)
	file := fixture["00023_runtime_membership_evidence.sql"]
	anchor := "RESET ROLE;\nREVOKE CREATE"
	if file == nil || !strings.Contains(string(file.Data), anchor) {
		t.Fatal("migration 23 failure anchor is missing")
	}
	file.Data = []byte(strings.Replace(string(file.Data), anchor, "SELECT 1/0;\n"+anchor, 1))
	_, applyErr := applyFS(ctx, db, fixture, LocalAdminMigratorIdentity())
	requireRegistrationError(t, applyErr, "22012")
	var after, functions, columns int64
	var schemaCreate, appTypeGrant, proofTypeGrant bool
	if err := db.QueryRowContext(ctx, `SELECT generation,(SELECT count(*) FROM pg_proc WHERE pronamespace='public'::regnamespace AND proname=ANY($1)),(SELECT count(*) FROM pg_attribute WHERE attrelid='public.runtime_fencing_proofs'::regclass AND attnum>0 AND NOT attisdropped AND attname IN ('request_id','reclaimed_claim_count')),has_schema_privilege('aboutme_runtime_owner','public','CREATE'),has_type_privilege('aboutme_app','public.runtime_leave_result','USAGE'),has_type_privilege('aboutme_fencing_proof','public.runtime_fence_result','USAGE') FROM public.runtime_write_state WHERE singleton`, membershipEvidenceNames()).
		Scan(&after, &functions, &columns, &schemaCreate, &appTypeGrant, &proofTypeGrant); err != nil {
		t.Fatal(err)
	}
	if after != before || functions != 0 || columns != 0 || schemaCreate || appTypeGrant || proofTypeGrant {
		t.Fatalf("rollback generation=%d/%d functions=%d columns=%d schemaCreate=%t appType=%t proofType=%t",
			before, after, functions, columns, schemaCreate, appTypeGrant, proofTypeGrant)
	}
}

// TestRuntimeMembershipEvidenceServingGracefulLeave proves the serving wrapper
// records one immutable zero-count receipt, moves draining to left, advances
// only the membership generation, and replays the retained result unchanged
// after later controller work.
func TestRuntimeMembershipEvidenceServingGracefulLeave(t *testing.T) {
	db, _ := membershipEvidenceDB(t)
	first, second := membershipServingDrained(t, db)
	before := membershipCapacity(t, db)
	partitionOne, partitionTwo := lifecyclePartition(t, db, 1), lifecyclePartition(t, db, 2)

	left := mustMembershipLeave(t, db, membershipAppRole, membershipServingLeaveExpr(first, 5, "op-scale-in"))
	if left.replicaID != first.replicaID || left.state != "left" || left.operationID != "op-scale-in" ||
		left.controllerGeneration != 5 || left.transitionCount != 0 || left.claimCount != 0 || left.replayed {
		t.Fatalf("serving leave=%+v", left)
	}
	receipt := membershipReceipt(t, db, first.replicaID)
	if !receipt.exists || receipt.instanceID != first.instanceID || receipt.releaseDigest != first.release ||
		receipt.operationID != "op-scale-in" || receipt.controllerGeneration != 5 ||
		receipt.transitionCount != 0 || receipt.claimCount != 0 || !receipt.recordedAt.Equal(left.recordedAt) {
		t.Fatalf("stored receipt=%+v result=%+v", receipt, left)
	}
	if state := lifecycleReplicaState(t, db, first.replicaID); state != "left" {
		t.Fatalf("target state=%q", state)
	}
	after := membershipCapacity(t, db)
	if after.generation != before.generation+1 || after.controllerGeneration != before.controllerGeneration ||
		after.desired != before.desired || after.phase != before.phase || after.admission != before.admission ||
		after.controllerOperation != before.controllerOperation ||
		after.updatedBy != "runtime_finish_serving_graceful_leave" {
		t.Fatalf("capacity before=%+v after=%+v", before, after)
	}
	if one, two := lifecyclePartition(t, db, 1), lifecyclePartition(t, db, 2); one != partitionOne || two != partitionTwo {
		t.Fatalf("leave changed partitions one=%+v/%+v two=%+v/%+v", partitionOne, one, partitionTwo, two)
	}
	if lifecycleReplicaState(t, db, second.replicaID) != "active" {
		t.Fatal("leave disturbed the survivor")
	}

	// A later unrelated controller action advances the controller generation;
	// exact replay still returns the retained historical result.
	if _, err := callLifecycleReplica(t, db,
		lifecycleTerminateExpr(5, "op-terminate", "request-late", second, "readiness_failed")); err != nil {
		t.Fatal(err)
	}
	replayBefore := membershipCapacity(t, db)
	replayed := mustMembershipLeave(t, db, membershipAppRole, membershipServingLeaveExpr(first, 5, "op-scale-in"))
	if !replayed.replayed || replayed.state != "left" || replayed.controllerGeneration != 5 ||
		replayed.operationID != "op-scale-in" || !replayed.recordedAt.Equal(left.recordedAt) {
		t.Fatalf("leave replay=%+v want the retained result %+v", replayed, left)
	}
	if replayAfter := membershipCapacity(t, db); replayAfter != replayBefore {
		t.Fatalf("replay changed capacity before=%+v after=%+v", replayBefore, replayAfter)
	}
}

// TestRuntimeMembershipEvidenceMaintenanceGracefulLeave proves the maintenance
// wrapper binds its own prepare step and that neither wrapper accepts the
// other's replica kind.
func TestRuntimeMembershipEvidenceMaintenanceGracefulLeave(t *testing.T) {
	db, _ := membershipEvidenceDB(t)
	node := membershipMaintenanceDrained(t, db)
	before := membershipCapacity(t, db)
	left := mustMembershipLeave(t, db, membershipMaintenanceRole, membershipMaintenanceLeaveExpr(node, 5, "op-wake"))
	if left.state != "left" || left.operationID != "op-wake" || left.controllerGeneration != 5 || left.replayed {
		t.Fatalf("maintenance leave=%+v", left)
	}
	after := membershipCapacity(t, db)
	if after.generation != before.generation+1 || after.controllerGeneration != before.controllerGeneration ||
		after.updatedBy != "runtime_finish_maintenance_graceful_leave" {
		t.Fatalf("maintenance capacity before=%+v after=%+v", before, after)
	}

	t.Run("cross_kind_wrappers_are_forbidden", func(t *testing.T) {
		other, _ := membershipEvidenceDB(t)
		serving, _ := membershipServingDrained(t, other)
		maintenance := membershipMaintenanceDrained(t, other)
		pgErr := membershipLeaveErrorOf(t, other, membershipAppRole,
			membershipServingLeaveExpr(maintenance, 6, "op-wake"), "42501")
		requireNoClaimIdentityLeak(t, pgErr, maintenance.replicaID, maintenance.instanceID, maintenance.release, "op-wake")
		pgErr = membershipLeaveErrorOf(t, other, membershipMaintenanceRole,
			membershipMaintenanceLeaveExpr(serving, 5, "op-scale-in"), "42501")
		requireNoClaimIdentityLeak(t, pgErr, serving.replicaID, serving.instanceID, serving.release, "op-scale-in")
		if state := lifecycleReplicaState(t, other, serving.replicaID); state != "draining" {
			t.Fatalf("rejected cross-kind leave changed the serving target state=%q", state)
		}
		if state := lifecycleReplicaState(t, other, maintenance.replicaID); state != "draining" {
			t.Fatalf("rejected cross-kind leave changed the maintenance target state=%q", state)
		}
	})
}

// TestRuntimeMembershipEvidenceLeavePredicates proves every leave precondition,
// the historical prepare binding, and that no rejection changes durable state.
func TestRuntimeMembershipEvidenceLeavePredicates(t *testing.T) {
	t.Run("live_running_and_waiting_claims_block_leave", func(t *testing.T) {
		db, _ := membershipEvidenceDB(t)
		first, _ := lifecycleTwoNodeFleet(t, db)
		claims := membershipLiveClaims(t, db, first.replicaID, 71)
		waiting := membershipWaitingRenderClaim(t, db, first.replicaID, 71)
		if _, err := callLifecycleReplica(t, db, lifecycleScaleInExpr(4, "op-scale-in", first)); err != nil {
			t.Fatal(err)
		}
		requireMembershipLeaveError(t, db, membershipAppRole, membershipServingLeaveExpr(first, 5, "op-scale-in"), "55000")
		for _, claim := range claims {
			requireClaimOutcome(t, db, membershipAppRole,
				claimReleaseExpression(claim, first.replicaID, membershipClaimDigest(t, db, claim), "joined"), "released")
		}
		requireMembershipLeaveError(t, db, membershipAppRole, membershipServingLeaveExpr(first, 5, "op-scale-in"), "55000")
		requireClaimOutcome(t, db, membershipAppRole,
			claimReleaseExpression(waiting, first.replicaID, membershipClaimDigest(t, db, waiting), "canceled"), "released")
		if left := mustMembershipLeave(t, db, membershipAppRole, membershipServingLeaveExpr(first, 5, "op-scale-in")); left.state != "left" {
			t.Fatalf("leave after every claim joined=%+v", left)
		}
	})

	t.Run("owned_closing_and_unresolved_transitions_block_leave", func(t *testing.T) {
		db, _ := membershipEvidenceDB(t)
		first, second := membershipServingDrained(t, db)
		const ownedTransition = "22222222-1234-4234-8234-123456789abc"
		const otherTransition = "33333333-1234-4234-8234-123456789abc"
		lifecycleWrite(t, db, membershipTransitionFixture(second, otherTransition, second.replicaID)...)
		if left := mustMembershipLeave(t, db, membershipAppRole, membershipServingLeaveExpr(first, 5, "op-scale-in")); left.state != "left" {
			t.Fatalf("another replica's closing transition blocked leave=%+v", left)
		}
		if membershipTransitionState(t, db, otherTransition) != "closing:-:-:true" {
			t.Fatal("leave changed an unrelated transition")
		}

		fresh, _ := membershipEvidenceDB(t)
		blocked, _ := membershipServingDrained(t, fresh)
		lifecycleWrite(t, fresh, membershipTransitionFixture(blocked, ownedTransition, blocked.replicaID)...)
		requireMembershipLeaveError(t, fresh, membershipAppRole, membershipServingLeaveExpr(blocked, 5, "op-scale-in"), "55000")
		lifecycleWrite(t, fresh, membershipUnresolveTransitionSQL(ownedTransition))
		requireMembershipLeaveError(t, fresh, membershipAppRole, membershipServingLeaveExpr(blocked, 5, "op-scale-in"), "55000")
		if receipt := membershipReceipt(t, fresh, blocked.replicaID); receipt.exists {
			t.Fatalf("blocked leave stored a receipt=%+v", receipt)
		}
	})

	t.Run("state_intent_evidence_and_generation_bounds", func(t *testing.T) {
		db, _ := membershipEvidenceDB(t)
		first, second := membershipServingDrained(t, db)
		before := membershipCapacity(t, db)
		lifecycleWrite(t, db, lifecycleParentSQL("op-other", "scale_in"),
			lifecycleStepSQL("op-other", "scale_in", "prepare_scale_in", 4))
		for _, invalid := range []struct {
			name, expression, code string
		}{
			{"active_target", membershipServingLeaveExpr(second, 5, "op-scale-in"), "55000"},
			{"unknown_replica", membershipServingLeaveExpr(registrationIdentity{
				replicaID: "99999999-9999-4999-8999-999999999999", instanceID: first.instanceID, release: first.release}, 5, "op-scale-in"), "55000"},
			{"wrong_instance", membershipServingLeaveExpr(registrationIdentity{
				replicaID: first.replicaID, instanceID: second.instanceID, release: first.release}, 5, "op-scale-in"), "AM002"},
			{"wrong_release", membershipServingLeaveExpr(registrationIdentity{
				replicaID: first.replicaID, instanceID: first.instanceID, release: second.release}, 5, "op-scale-in"), "AM002"},
			{"missing_prepare", membershipServingLeaveExpr(first, 5, "op-unknown"), "55000"},
			{"prepare_names_another_replica", membershipServingLeaveExpr(first, 5, "op-other"), "55000"},
			{"generation_below_the_prepare_result", membershipServingLeaveExpr(first, 4, "op-scale-in"), "55000"},
			{"generation_above_the_prepare_result", membershipServingLeaveExpr(first, 6, "op-scale-in"), "55000"},
		} {
			t.Run(invalid.name, func(t *testing.T) {
				pgErr := membershipLeaveErrorOf(t, db, membershipAppRole, invalid.expression, invalid.code)
				requireNoClaimIdentityLeak(t, pgErr, first.replicaID, first.instanceID, first.release,
					second.replicaID, "op-scale-in", "op-other", "op-unknown")
			})
		}
		if after := membershipCapacity(t, db); after != before {
			t.Fatalf("rejected leave changed capacity before=%+v after=%+v", before, after)
		}
		if receipt := membershipReceipt(t, db, first.replicaID); receipt.exists {
			t.Fatal("rejected leave stored a receipt")
		}
		if state := lifecycleReplicaState(t, db, first.replicaID); state != "draining" {
			t.Fatalf("rejected leave changed state=%q", state)
		}
	})

	t.Run("a_termination_intent_blocks_a_draining_target", func(t *testing.T) {
		db, _ := membershipEvidenceDB(t)
		first, _ := membershipServingDrained(t, db)
		lifecycleWrite(t, db, membershipIntentSQL(first, "request-blocking", "drain_failed"))
		requireMembershipLeaveError(t, db, membershipAppRole, membershipServingLeaveExpr(first, 5, "op-scale-in"), "55000")
		if receipt := membershipReceipt(t, db, first.replicaID); receipt.exists {
			t.Fatal("an intent-blocked leave stored a receipt")
		}
	})

	t.Run("left_and_fenced_targets_without_a_receipt_are_unavailable", func(t *testing.T) {
		db, _ := membershipEvidenceDB(t)
		first, _ := membershipServingDrained(t, db)
		lifecycleWrite(t, db, lifecycleMarkLeftSQL(first))
		requireMembershipLeaveError(t, db, membershipAppRole, membershipServingLeaveExpr(first, 5, "op-scale-in"), "55000")

		other, _ := membershipEvidenceDB(t)
		fenced, _ := membershipServingDrained(t, other)
		lifecycleWrite(t, other, lifecycleMarkFencedSQL(fenced))
		requireMembershipLeaveError(t, other, membershipAppRole, membershipServingLeaveExpr(fenced, 5, "op-scale-in"), "55000")
	})

	t.Run("a_later_controller_generation_still_permits_the_prepared_leave", func(t *testing.T) {
		db, _ := membershipEvidenceDB(t)
		first, second := membershipServingDrained(t, db)
		if _, err := callLifecycleReplica(t, db,
			lifecycleTerminateExpr(5, "op-terminate", "request-unrelated", second, "readiness_failed")); err != nil {
			t.Fatal(err)
		}
		left := mustMembershipLeave(t, db, membershipAppRole, membershipServingLeaveExpr(first, 5, "op-scale-in"))
		if left.state != "left" || left.controllerGeneration != 5 || left.replayed {
			t.Fatalf("leave after an unrelated controller action=%+v", left)
		}
	})

	t.Run("a_controller_generation_below_the_prepare_result_is_am001", func(t *testing.T) {
		db, _ := membershipEvidenceDB(t)
		first, _ := membershipServingDrained(t, db)
		lifecycleWrite(t, db, lifecycleSetControllerGenerationSQL(4))
		requireMembershipLeaveError(t, db, membershipAppRole, membershipServingLeaveExpr(first, 5, "op-scale-in"), "AM001")
	})
}

// TestRuntimeMembershipEvidenceLeaveInputMatrix proves every malformed scalar
// is 22023, that no diagnostic discloses a supplied value, and that a rejected
// call writes nothing.
func TestRuntimeMembershipEvidenceLeaveInputMatrix(t *testing.T) {
	db, _ := membershipEvidenceDB(t)
	first, _ := membershipServingDrained(t, db)
	before := membershipCapacity(t, db)
	long := membershipLongText()
	for _, invalid := range []struct{ name, expression string }{
		{"nil_replica", membershipServingLeaveExpr(registrationIdentity{
			replicaID: membershipNilUUID, instanceID: first.instanceID, release: first.release}, 5, "op-scale-in")},
		{"null_replica", `public.runtime_finish_serving_graceful_leave(NULL,'` + first.instanceID + `','` + first.release + `',5,'op-scale-in')`},
		{"bad_instance", membershipServingLeaveExpr(registrationIdentity{
			replicaID: first.replicaID, instanceID: "i-ZZZZZZZZZZZZZZZZZ", release: first.release}, 5, "op-scale-in")},
		{"bad_release", membershipServingLeaveExpr(registrationIdentity{
			replicaID: first.replicaID, instanceID: first.instanceID, release: "sha256:zz"}, 5, "op-scale-in")},
		{"zero_generation", membershipServingLeaveExpr(first, 0, "op-scale-in")},
		{"negative_generation", membershipServingLeaveExpr(first, -1, "op-scale-in")},
		{"null_generation", `public.runtime_finish_serving_graceful_leave('` + first.replicaID + `','` + first.instanceID + `','` + first.release + `',NULL,'op-scale-in')`},
		{"empty_operation", membershipServingLeaveExpr(first, 5, "")},
		{"long_operation", membershipServingLeaveExpr(first, 5, long)},
		{"null_operation", `public.runtime_finish_serving_graceful_leave('` + first.replicaID + `','` + first.instanceID + `','` + first.release + `',5,NULL)`},
		{"unprintable_operation", `public.runtime_finish_serving_graceful_leave('` + first.replicaID + `','` + first.instanceID + `','` + first.release + `',5,'op'||chr(10))`},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			pgErr := membershipLeaveErrorOf(t, db, membershipAppRole, invalid.expression, "22023")
			requireNoClaimIdentityLeak(t, pgErr, first.replicaID, first.instanceID, first.release, long, "op-scale-in")
		})
	}
	if after := membershipCapacity(t, db); after != before {
		t.Fatalf("rejected input changed capacity before=%+v after=%+v", before, after)
	}
	if receipt := membershipReceipt(t, db, first.replicaID); receipt.exists {
		t.Fatal("rejected input stored a receipt")
	}
}

// TestRuntimeMembershipEvidenceLeaveReceiptConflicts proves receipt-first
// replay authority: a changed operation or generation conflicts, and a stored
// receipt that contradicts its member row is durable corruption.
func TestRuntimeMembershipEvidenceLeaveReceiptConflicts(t *testing.T) {
	db, _ := membershipEvidenceDB(t)
	first, _ := membershipServingDrained(t, db)
	left := mustMembershipLeave(t, db, membershipAppRole, membershipServingLeaveExpr(first, 5, "op-scale-in"))
	before := membershipCapacity(t, db)
	for _, conflict := range []struct{ name, expression string }{
		{"changed_operation", membershipServingLeaveExpr(first, 5, "op-other")},
		{"changed_generation", membershipServingLeaveExpr(first, 6, "op-scale-in")},
	} {
		t.Run(conflict.name, func(t *testing.T) {
			pgErr := membershipLeaveErrorOf(t, db, membershipAppRole, conflict.expression, "AM002")
			requireNoClaimIdentityLeak(t, pgErr, first.replicaID, first.instanceID, first.release, "op-scale-in", "op-other")
		})
	}
	if after := membershipCapacity(t, db); after != before {
		t.Fatalf("a conflicting replay changed capacity before=%+v after=%+v", before, after)
	}
	if receipt := membershipReceipt(t, db, first.replicaID); !receipt.recordedAt.Equal(left.recordedAt) {
		t.Fatalf("a conflicting replay changed the receipt=%+v", receipt)
	}

	t.Run("a_receipt_that_contradicts_its_member_row_is_am001", func(t *testing.T) {
		other, _ := membershipEvidenceDB(t)
		draining, _ := membershipServingDrained(t, other)
		lifecycleWrite(t, other, membershipReceiptSQL(draining, 5, "op-scale-in"))
		requireMembershipLeaveError(t, other, membershipAppRole,
			membershipServingLeaveExpr(draining, 5, "op-scale-in"), "AM001")
	})
}

// membershipFenceableTarget builds one replica in the requested source state
// and returns it with the request ID any existing intent binds.
func membershipFenceableTarget(t *testing.T, db *sql.DB, state string) (registrationIdentity, string) {
	t.Helper()
	switch state {
	case "joining":
		node := validRegistrationIdentity("4")
		if _, err := callRegistration(t, db, membershipAppRole, "runtime_register_serving_replica", node); err != nil {
			t.Fatal(err)
		}
		return node, "request-fresh"
	case "active":
		first, _ := lifecycleTwoNodeFleet(t, db)
		return first, "request-fresh"
	case "draining":
		first, _ := membershipServingDrained(t, db)
		return first, "request-fresh"
	case "terminating":
		first, _ := membershipServingDrained(t, db)
		if _, err := callLifecycleReplica(t, db,
			lifecycleTerminateExpr(5, "op-terminate", "request-intent", first, "drain_failed")); err != nil {
			t.Fatal(err)
		}
		return first, "request-intent"
	case "left":
		first, _ := membershipServingDrained(t, db)
		if left := mustMembershipLeave(t, db, membershipAppRole, membershipServingLeaveExpr(first, 5, "op-scale-in")); left.state != "left" {
			t.Fatalf("leave fixture=%+v", left)
		}
		return first, "request-fresh"
	}
	t.Fatalf("unknown source state %q", state)
	return registrationIdentity{}, ""
}

// TestRuntimeMembershipEvidenceProofFencesEverySourceState proves exact
// terminated evidence fences joining, active, draining and terminating, leaves
// a left target left with an audit proof, and advances only the membership
// generation in every case.
func TestRuntimeMembershipEvidenceProofFencesEverySourceState(t *testing.T) {
	for _, source := range []struct{ state, want string }{
		{"joining", "fenced"}, {"active", "fenced"}, {"draining", "fenced"},
		{"terminating", "fenced"}, {"left", "left"},
	} {
		t.Run(source.state, func(t *testing.T) {
			db, _ := membershipEvidenceDB(t)
			target, request := membershipFenceableTarget(t, db, source.state)
			before := membershipCapacity(t, db)
			result := mustMembershipProof(t, db, membershipValidProofExpr(target, request, "evidence-"+source.state))
			if result.replicaID != target.replicaID || result.state != source.want ||
				result.evidenceID != "evidence-"+source.state || result.reclaimedClaimCount != 0 || result.replayed {
				t.Fatalf("proof=%+v want state=%q", result, source.want)
			}
			if state := lifecycleReplicaState(t, db, target.replicaID); state != source.want {
				t.Fatalf("target state=%q want=%q", state, source.want)
			}
			stored := membershipProof(t, db, target.replicaID)
			if !stored.exists || stored.adapter != "ec2_terminated_v1" || stored.requestID != request ||
				stored.evidenceID != "evidence-"+source.state || stored.observedState != "terminated" ||
				stored.reclaimedClaimCount != 0 || !stored.recordedAt.Equal(result.recordedAt) {
				t.Fatalf("stored proof=%+v", stored)
			}
			if stored.requestedAt.UTC().Year() != 2030 || stored.observedTerminatedAt.UTC().Year() != 2030 {
				t.Fatalf("external times were not stored raw=%+v", stored)
			}
			if !stored.recordedAt.Before(stored.requestedAt) {
				t.Fatalf("a raw external time entered database ordering stored=%+v", stored)
			}
			after := membershipCapacity(t, db)
			if after.generation != before.generation+1 || after.controllerGeneration != before.controllerGeneration ||
				after.desired != before.desired || after.phase != before.phase || after.admission != before.admission ||
				after.controllerOperation != before.controllerOperation ||
				after.updatedBy != "runtime_record_ec2_termination" {
				t.Fatalf("capacity before=%+v after=%+v", before, after)
			}
			replayBefore := membershipCapacity(t, db)
			replayed := mustMembershipProof(t, db, membershipValidProofExpr(target, request, "evidence-"+source.state))
			if !replayed.replayed || replayed.state != source.want || replayed.evidenceID != result.evidenceID ||
				replayed.reclaimedClaimCount != result.reclaimedClaimCount || !replayed.recordedAt.Equal(result.recordedAt) {
				t.Fatalf("proof replay=%+v want the retained result %+v", replayed, result)
			}
			if replayAfter := membershipCapacity(t, db); replayAfter != replayBefore {
				t.Fatalf("proof replay changed capacity before=%+v after=%+v", replayBefore, replayAfter)
			}
		})
	}
}

// TestRuntimeMembershipEvidenceProofReleasesClaims proves one fence releases
// every live claim parent of the exact replica across several policies and
// scopes in one transaction, decrements each affected summary once, retains
// identities and ordinals, leaves another replica's shared-summary claim
// untouched, counts parents rather than scope rows, and replays that stored
// historical count after ordinary claim GC.
func TestRuntimeMembershipEvidenceProofReleasesClaims(t *testing.T) {
	db, _ := membershipEvidenceDB(t)
	first, second := lifecycleTwoNodeFleet(t, db)
	const index = 71
	joined := claimUUID("47777777", index)
	requireClaimOutcome(t, db, membershipAppRole,
		claimSingleExpression(joined, "password.hash", first.replicaID, "NULL", "global", claimHex(index+1)), "running")
	requireClaimOutcome(t, db, membershipAppRole,
		claimReleaseExpression(joined, first.replicaID, membershipClaimDigest(t, db, joined), "joined"), "released")
	joinedBefore := membershipClaimStates(t, db, first.replicaID)

	membershipLiveClaims(t, db, first.replicaID, index)
	membershipWaitingRenderClaim(t, db, first.replicaID, index)
	survivor := claimUUID("46666666", index)
	requireClaimOutcome(t, db, membershipAppRole,
		claimSingleExpression(survivor, "password.hash", second.replicaID, "NULL", "global", claimHex(index)), "running")
	survivorBefore := membershipClaimStates(t, db, second.replicaID)
	requireClaimSummary(t, db, "password.hash", "global", claimHex(index), 2, 0)
	requireClaimSummary(t, db, "render.global_claim", "global", claimHex(index), 1, 1)

	beforeCounts := countClaimTables(t, db)
	result := mustMembershipProof(t, db, membershipValidProofExpr(first, "request-fence", "evidence-fence"))
	if result.state != "fenced" || result.reclaimedClaimCount != 5 || result.replayed {
		t.Fatalf("fence=%+v want five reclaimed parents", result)
	}
	if stored := membershipProof(t, db, first.replicaID); stored.reclaimedClaimCount != 5 {
		t.Fatalf("stored reclaimed count=%+v", stored)
	}
	if afterCounts := countClaimTables(t, db); afterCounts != beforeCounts {
		t.Fatalf("fence deleted or added claim rows before=%+v after=%+v", beforeCounts, afterCounts)
	}
	requireClaimSummary(t, db, "password.hash", "global", claimHex(index), 1, 0)
	requireClaimSummary(t, db, "render.global_claim", "global", claimHex(index), 0, 0)
	requireClaimSummary(t, db, "mcp.user_concurrent", "user", claimHex(index), 0, 0)
	requireClaimSummary(t, db, "sse.fleet_account_ip", "ip", claimHex(index+256), 0, 0)
	requireClaimSummary(t, db, "sse.fleet_account_ip", "account", claimHex(index+512), 0, 0)
	if survivorAfter := membershipClaimStates(t, db, second.replicaID); len(survivorAfter) != 1 ||
		survivorAfter[0] != survivorBefore[0] {
		t.Fatalf("fence changed another replica's claim before=%v after=%v", survivorBefore, survivorAfter)
	}
	states := membershipClaimStates(t, db, first.replicaID)
	fenced, unchanged := 0, ""
	for _, state := range states {
		switch {
		case strings.Contains(state, ":released:fenced:"):
			fenced++
			if !strings.Contains(state, "released/") {
				t.Fatalf("a fenced parent kept a live scope=%q", state)
			}
		case strings.Contains(state, ":released:joined:"):
			unchanged = state
		default:
			t.Fatalf("fence left a live claim=%q", state)
		}
	}
	if fenced != 5 || unchanged != joinedBefore[0] {
		t.Fatalf("fenced=%d existing released receipt before=%q after=%q", fenced, joinedBefore[0], unchanged)
	}
	var scopeRows, releasedAtMatches int
	if err := db.QueryRowContext(context.Background(), `SELECT (SELECT count(*) FROM public.shared_claim_scopes c JOIN public.shared_claim_requests r ON r.claim_id=c.claim_id WHERE r.replica_id=$1 AND r.release_reason='fenced'),(SELECT count(*) FROM public.shared_claim_requests r WHERE r.replica_id=$1 AND r.release_reason='fenced' AND r.released_at=$2)`, first.replicaID, result.recordedAt).
		Scan(&scopeRows, &releasedAtMatches); err != nil {
		t.Fatal(err)
	}
	if scopeRows != 6 || releasedAtMatches != 5 {
		t.Fatalf("fenced scope rows=%d parents released at the proof time=%d", scopeRows, releasedAtMatches)
	}

	// The stored historical count survives ordinary 24-hour claim GC.
	corruptClaimRows(t, db, `UPDATE public.shared_claim_requests SET released_at=released_at-interval '25 hours' WHERE state='released'`)
	claims, _, err := callClaimGC(t, db)
	if err != nil {
		t.Fatal(err)
	}
	if claims == 0 {
		t.Fatal("claim GC deleted nothing")
	}
	replayed := mustMembershipProof(t, db, membershipValidProofExpr(first, "request-fence", "evidence-fence"))
	if !replayed.replayed || replayed.reclaimedClaimCount != 5 || !replayed.recordedAt.Equal(result.recordedAt) {
		t.Fatalf("replay after claim GC=%+v want the retained count five", replayed)
	}
}

// TestRuntimeMembershipEvidenceProofIdentityAndReplay proves request and
// evidence identity in every direction: an existing intent binds its request,
// a no-intent proof stores the supplied request, cross-replica reuse and every
// changed retained field conflict, and impossible stored evidence is
// corruption.
func TestRuntimeMembershipEvidenceProofIdentityAndReplay(t *testing.T) {
	t.Run("an_intent_binds_its_exact_request", func(t *testing.T) {
		db, _ := membershipEvidenceDB(t)
		target, request := membershipFenceableTarget(t, db, "terminating")
		pgErr := membershipProofErrorOf(t, db, membershipValidProofExpr(target, "request-other", "evidence-a"), "AM002")
		requireNoClaimIdentityLeak(t, pgErr, target.replicaID, target.instanceID, target.release, "request-other", request)
		if proof := membershipProof(t, db, target.replicaID); proof.exists {
			t.Fatal("a conflicting request stored a proof")
		}
		if result := mustMembershipProof(t, db, membershipValidProofExpr(target, request, "evidence-a")); result.state != "fenced" {
			t.Fatalf("exact intent request=%+v", result)
		}
	})

	t.Run("cross_replica_request_and_evidence_reuse_conflict", func(t *testing.T) {
		db, _ := membershipEvidenceDB(t)
		first, second := lifecycleTwoNodeFleet(t, db)
		if result := mustMembershipProof(t, db, membershipValidProofExpr(first, "request-shared", "evidence-shared")); result.state != "fenced" {
			t.Fatalf("first fence=%+v", result)
		}
		requireMembershipProofError(t, db, membershipValidProofExpr(second, "request-shared", "evidence-second"), "AM002")
		requireMembershipProofError(t, db, membershipValidProofExpr(second, "request-second", "evidence-shared"), "AM002")
		if proof := membershipProof(t, db, second.replicaID); proof.exists {
			t.Fatal("a conflicting reuse stored a proof")
		}
		if state := lifecycleReplicaState(t, db, second.replicaID); state != "active" {
			t.Fatalf("a conflicting reuse changed the second replica state=%q", state)
		}
	})

	t.Run("every_changed_retained_field_conflicts", func(t *testing.T) {
		db, _ := membershipEvidenceDB(t)
		first, second := lifecycleTwoNodeFleet(t, db)
		result := mustMembershipProof(t, db, membershipValidProofExpr(first, "request-exact", "evidence-exact"))
		before := membershipCapacity(t, db)
		for _, conflict := range []struct{ name, expression, code string }{
			{"changed_request", membershipValidProofExpr(first, "request-changed", "evidence-exact"), "AM002"},
			{"changed_evidence", membershipValidProofExpr(first, "request-exact", "evidence-changed"), "AM002"},
			{"changed_requested_at", membershipProofExpr(first, "request-exact", "evidence-exact",
				`'2029-01-01 00:00:00+00'::timestamptz`, membershipObservedAt, "terminated"), "AM002"},
			{"changed_observed_at", membershipProofExpr(first, "request-exact", "evidence-exact",
				membershipRequestedAt, `'2031-01-01 00:00:00+00'::timestamptz`, "terminated"), "AM002"},
			{"changed_instance", membershipValidProofExpr(registrationIdentity{
				replicaID: first.replicaID, instanceID: second.instanceID, release: first.release}, "request-exact", "evidence-exact"), "AM002"},
			{"changed_release", membershipValidProofExpr(registrationIdentity{
				replicaID: first.replicaID, instanceID: first.instanceID, release: second.release}, "request-exact", "evidence-exact"), "AM002"},
		} {
			t.Run(conflict.name, func(t *testing.T) {
				pgErr := membershipProofErrorOf(t, db, conflict.expression, conflict.code)
				requireNoClaimIdentityLeak(t, pgErr, first.replicaID, first.instanceID, first.release,
					"request-exact", "evidence-exact", "request-changed", "evidence-changed")
			})
		}
		if after := membershipCapacity(t, db); after != before {
			t.Fatalf("a conflicting replay changed capacity before=%+v after=%+v", before, after)
		}
		if stored := membershipProof(t, db, first.replicaID); !stored.recordedAt.Equal(result.recordedAt) ||
			stored.requestID != "request-exact" || stored.evidenceID != "evidence-exact" {
			t.Fatalf("a conflicting replay changed the proof=%+v", stored)
		}
	})

	t.Run("impossible_stored_evidence_is_am001", func(t *testing.T) {
		db, _ := membershipEvidenceDB(t)
		first, _ := lifecycleTwoNodeFleet(t, db)
		if result := mustMembershipProof(t, db, membershipValidProofExpr(first, "request-corrupt", "evidence-corrupt")); result.state != "fenced" {
			t.Fatalf("fence fixture=%+v", result)
		}
		lifecycleWrite(t, db, `ALTER TABLE public.runtime_replicas DISABLE TRIGGER runtime_replicas_identity`)
		lifecycleWrite(t, db, fmt.Sprintf(`UPDATE public.runtime_replicas SET state='active',fenced_at=NULL WHERE replica_id='%s'`, first.replicaID))
		lifecycleWrite(t, db, `ALTER TABLE public.runtime_replicas ENABLE TRIGGER runtime_replicas_identity`)
		requireMembershipProofError(t, db, membershipValidProofExpr(first, "request-corrupt", "evidence-corrupt"), "AM001")
	})

	t.Run("an_intent_on_a_left_target_is_am001", func(t *testing.T) {
		db, _ := membershipEvidenceDB(t)
		first, _ := membershipServingDrained(t, db)
		if left := mustMembershipLeave(t, db, membershipAppRole, membershipServingLeaveExpr(first, 5, "op-scale-in")); left.state != "left" {
			t.Fatalf("leave fixture=%+v", left)
		}
		lifecycleWrite(t, db, `ALTER TABLE public.runtime_termination_intents DISABLE TRIGGER runtime_termination_intents_shape`,
			membershipIntentSQL(first, "request-impossible", "drain_failed"),
			`ALTER TABLE public.runtime_termination_intents ENABLE TRIGGER runtime_termination_intents_shape`)
		requireMembershipProofError(t, db, membershipValidProofExpr(first, "request-impossible", "evidence-impossible"), "AM001")
	})
}

// TestRuntimeMembershipEvidenceProofInputMatrix proves every malformed scalar,
// time order and observed state is 22023, discloses no supplied value and
// writes nothing.
func TestRuntimeMembershipEvidenceProofInputMatrix(t *testing.T) {
	db, _ := membershipEvidenceDB(t)
	first, _ := lifecycleTwoNodeFleet(t, db)
	before := membershipCapacity(t, db)
	long := membershipLongText()
	for _, invalid := range []struct{ name, expression string }{
		{"nil_replica", membershipValidProofExpr(registrationIdentity{
			replicaID: membershipNilUUID, instanceID: first.instanceID, release: first.release}, "request-a", "evidence-a")},
		{"bad_instance", membershipValidProofExpr(registrationIdentity{
			replicaID: first.replicaID, instanceID: "i-ZZZZZZZZZZZZZZZZZ", release: first.release}, "request-a", "evidence-a")},
		{"bad_release", membershipValidProofExpr(registrationIdentity{
			replicaID: first.replicaID, instanceID: first.instanceID, release: "sha256:zz"}, "request-a", "evidence-a")},
		{"empty_request", membershipValidProofExpr(first, "", "evidence-a")},
		{"long_request", membershipValidProofExpr(first, long, "evidence-a")},
		{"empty_evidence", membershipValidProofExpr(first, "request-a", "")},
		{"long_evidence", membershipValidProofExpr(first, "request-a", long)},
		{"unprintable_evidence", `public.runtime_record_ec2_termination('` + first.replicaID + `','` + first.instanceID + `','` + first.release + `','request-a','evidence'||chr(10),` + membershipRequestedAt + `,` + membershipObservedAt + `,'terminated')`},
		{"null_requested_at", membershipProofExpr(first, "request-a", "evidence-a", "NULL::timestamptz", membershipObservedAt, "terminated")},
		{"null_observed_at", membershipProofExpr(first, "request-a", "evidence-a", membershipRequestedAt, "NULL::timestamptz", "terminated")},
		{"inverted_times", membershipProofExpr(first, "request-a", "evidence-a", membershipObservedAt, membershipRequestedAt, "terminated")},
		{"observed_state_stopping", membershipProofExpr(first, "request-a", "evidence-a", membershipRequestedAt, membershipObservedAt, "stopping")},
		{"null_observed_state", `public.runtime_record_ec2_termination('` + first.replicaID + `','` + first.instanceID + `','` + first.release + `','request-a','evidence-a',` + membershipRequestedAt + `,` + membershipObservedAt + `,NULL)`},
	} {
		t.Run(invalid.name, func(t *testing.T) {
			pgErr := membershipProofErrorOf(t, db, invalid.expression, "22023")
			requireNoClaimIdentityLeak(t, pgErr, first.replicaID, first.instanceID, first.release,
				long, "request-a", "evidence-a", "stopping")
		})
	}
	if after := membershipCapacity(t, db); after != before {
		t.Fatalf("rejected proof input changed capacity before=%+v after=%+v", before, after)
	}
	if proof := membershipProof(t, db, first.replicaID); proof.exists {
		t.Fatal("rejected proof input stored a proof")
	}
	if state := lifecycleReplicaState(t, db, first.replicaID); state != "active" {
		t.Fatalf("rejected proof input changed state=%q", state)
	}
}

// TestRuntimeMembershipEvidenceProofAtomicity proves a corrupt claim set fails
// the whole proof closed: no proof row, no fence, no summary change and no
// generation advance survive.
func TestRuntimeMembershipEvidenceProofAtomicity(t *testing.T) {
	db, _ := membershipEvidenceDB(t)
	first, _ := lifecycleTwoNodeFleet(t, db)
	claims := membershipLiveClaims(t, db, first.replicaID, 81)
	before := membershipCapacity(t, db)
	beforeStates := membershipClaimStates(t, db, first.replicaID)
	beforeSummary := claimSummary(t, db, "password.hash", "global", claimHex(81))
	corruptClaimRows(t, db, fmt.Sprintf(`UPDATE public.shared_claim_scopes SET state='waiting' WHERE claim_id='%s'`, claims[1]))

	requireMembershipProofError(t, db, membershipValidProofExpr(first, "request-atomic", "evidence-atomic"), "AM001")
	if proof := membershipProof(t, db, first.replicaID); proof.exists {
		t.Fatal("a failed cleanup stored a proof")
	}
	if state := lifecycleReplicaState(t, db, first.replicaID); state != "active" {
		t.Fatalf("a failed cleanup fenced the replica state=%q", state)
	}
	if after := membershipCapacity(t, db); after != before {
		t.Fatalf("a failed cleanup changed capacity before=%+v after=%+v", before, after)
	}
	if afterSummary := claimSummary(t, db, "password.hash", "global", claimHex(81)); afterSummary != beforeSummary {
		t.Fatalf("a failed cleanup changed a summary before=%+v after=%+v", beforeSummary, afterSummary)
	}
	corruptClaimRows(t, db, fmt.Sprintf(`UPDATE public.shared_claim_scopes SET state='running' WHERE claim_id='%s'`, claims[1]))
	if afterStates := membershipClaimStates(t, db, first.replicaID); len(afterStates) != len(beforeStates) {
		t.Fatalf("a failed cleanup changed claim rows before=%v after=%v", beforeStates, afterStates)
	}
	if result := mustMembershipProof(t, db, membershipValidProofExpr(first, "request-atomic", "evidence-atomic")); result.reclaimedClaimCount != 4 {
		t.Fatalf("the repaired fixture fence=%+v", result)
	}
}

// TestRuntimeMembershipEvidenceLockBoundaries proves claim acquire, ordinary
// release, leave and proof serialize on the accepted membership prefix, that a
// competing proof identity resolves after the winner commits, and that a held
// transition parent neither blocks a proof nor is changed by one.
func TestRuntimeMembershipEvidenceLockBoundaries(t *testing.T) {
	t.Run("claim_acquire_waits_for_a_fence_and_then_finds_no_admission", func(t *testing.T) {
		db, _ := membershipEvidenceDB(t)
		first, _ := lifecycleTwoNodeFleet(t, db)
		hold := openMembershipHolder(t, db, membershipProofRole,
			membershipValidProofExpr(first, "request-race", "evidence-race"), membershipFenceProjection)
		_, err := raceClaimOperation(t, db, hold.commit, membershipAppRole,
			claimSingleExpression(claimUUID("48888888", 91), "password.hash", first.replicaID, "NULL", "global", claimHex(91)))
		requireRegistrationError(t, err, "55000")
		if state := lifecycleReplicaState(t, db, first.replicaID); state != "fenced" {
			t.Fatalf("the fence did not commit state=%q", state)
		}
	})

	t.Run("leave_waits_for_an_ordinary_release_and_then_succeeds", func(t *testing.T) {
		db, _ := membershipEvidenceDB(t)
		first, _ := lifecycleTwoNodeFleet(t, db)
		claims := membershipLiveClaims(t, db, first.replicaID, 92)
		digest := membershipClaimDigest(t, db, claims[1])
		if _, err := callLifecycleReplica(t, db, lifecycleScaleInExpr(4, "op-scale-in", first)); err != nil {
			t.Fatal(err)
		}
		for _, claim := range claims[2:] {
			requireClaimOutcome(t, db, membershipAppRole,
				claimReleaseExpression(claim, first.replicaID, membershipClaimDigest(t, db, claim), "joined"), "released")
		}
		requireClaimOutcome(t, db, membershipAppRole,
			claimReleaseExpression(claims[0], first.replicaID, membershipClaimDigest(t, db, claims[0]), "joined"), "released")
		hold := openClaimHolder(t, db, membershipAppRole,
			claimReleaseExpression(claims[1], first.replicaID, digest, "joined"))
		if err := raceMembershipOperation(t, db, hold.commit, membershipAppRole,
			membershipServingLeaveExpr(first, 5, "op-scale-in"), membershipLeaveProjection); err != nil {
			t.Fatalf("leave after the release committed: %v", err)
		}
		if state := lifecycleReplicaState(t, db, first.replicaID); state != "left" {
			t.Fatalf("leave state=%q", state)
		}
	})

	t.Run("a_competing_proof_identity_resolves_after_the_winner_commits", func(t *testing.T) {
		db, _ := membershipEvidenceDB(t)
		first, _ := lifecycleTwoNodeFleet(t, db)
		hold := openMembershipHolder(t, db, membershipProofRole,
			membershipValidProofExpr(first, "request-winner", "evidence-winner"), membershipFenceProjection)
		err := raceMembershipOperation(t, db, hold.commit, membershipProofRole,
			membershipValidProofExpr(first, "request-loser", "evidence-loser"), membershipFenceProjection)
		requireRegistrationError(t, err, "AM002")
		stored := membershipProof(t, db, first.replicaID)
		if !stored.exists || stored.requestID != "request-winner" || stored.evidenceID != "evidence-winner" {
			t.Fatalf("the winner's proof=%+v", stored)
		}
	})

	t.Run("a_held_transition_parent_neither_blocks_nor_changes_a_proof", func(t *testing.T) {
		db, _ := membershipEvidenceDB(t)
		first, second := membershipServingDrained(t, db)
		const transition = "44444444-1234-4234-8234-123456789abc"
		lifecycleWrite(t, db, membershipTransitionFixture(second, transition, first.replicaID)...)
		before := membershipTransitionState(t, db, transition)
		release := holdTransitionParent(t, db)
		defer release()
		requireMembershipLeaveError(t, db, membershipAppRole, membershipServingLeaveExpr(first, 5, "op-scale-in"), "55000")
		fenced := mustMembershipProof(t, db, membershipValidProofExpr(first, "request-held", "evidence-held"))
		if fenced.state != "fenced" || fenced.replayed {
			t.Fatalf("proof under a held transition parent=%+v", fenced)
		}
		if after := membershipTransitionState(t, db, transition); after != before {
			t.Fatalf("the proof changed a transition before=%q after=%q", before, after)
		}
	})
}

// TestRuntimeMembershipEvidenceSampledTimeIsClamped proves the isolated
// sampler is the only clock, that a past sample clamps to prior durable
// evidence, and that a future sample is used as is.
func TestRuntimeMembershipEvidenceSampledTimeIsClamped(t *testing.T) {
	db, _ := membershipEvidenceDB(t)
	first, _ := membershipServingDrained(t, db)
	prior := lifecycleCapacity(t, db).updatedAt

	capacityAt, recordedAt, err := membershipClockProbe(t, db, "2000-01-01 00:00:00+00", membershipAppRole,
		membershipServingLeaveExpr(first, 5, "op-scale-in"), membershipLeaveProjection)
	if err != nil {
		t.Fatal(err)
	}
	if !capacityAt.Equal(recordedAt) || capacityAt.Before(prior) || capacityAt.UTC().Year() == 2000 {
		t.Fatalf("a past sample was not clamped capacity=%s receipt=%s prior=%s", capacityAt, recordedAt, prior)
	}

	capacityAt, recordedAt, err = membershipClockProbe(t, db, "2999-01-01 00:00:00+00", membershipProofRole,
		membershipValidProofExpr(first, "request-clock", "evidence-clock"), membershipFenceProjection)
	if err != nil {
		t.Fatal(err)
	}
	if !capacityAt.Equal(recordedAt) || capacityAt.UTC().Year() != 2999 {
		t.Fatalf("a future sample was not used capacity=%s proof=%s", capacityAt, recordedAt)
	}
	if state := lifecycleReplicaState(t, db, first.replicaID); state != "draining" {
		t.Fatalf("a rolled-back probe changed durable state=%q", state)
	}
	if after := lifecycleCapacity(t, db).updatedAt; !after.Equal(prior) {
		t.Fatalf("a rolled-back probe changed capacity before=%s after=%s", prior, after)
	}
}

// TestRuntimeMembershipEvidenceAdversarialBoundaries proves a hostile caller
// search path is inert and that membership generation exhaustion is detected
// before any mutation.
func TestRuntimeMembershipEvidenceAdversarialBoundaries(t *testing.T) {
	t.Run("hostile_caller_search_path_is_inert", func(t *testing.T) {
		db, _ := membershipEvidenceDB(t)
		first, _ := membershipServingDrained(t, db)
		for _, statement := range []string{
			`CREATE SCHEMA hostile`,
			`GRANT USAGE ON SCHEMA hostile TO aboutme_app,aboutme_fencing_proof`,
			`CREATE TABLE hostile.runtime_capacity(singleton boolean)`,
			`CREATE TABLE hostile.runtime_leave_receipts(replica_id uuid)`,
			`CREATE TABLE hostile.runtime_fencing_proofs(replica_id uuid)`,
			`CREATE TABLE hostile.shared_claim_requests(claim_id uuid)`,
			`GRANT SELECT,INSERT ON hostile.runtime_capacity,hostile.runtime_leave_receipts,hostile.runtime_fencing_proofs,hostile.shared_claim_requests TO aboutme_app,aboutme_fencing_proof`,
			`CREATE FUNCTION hostile.runtime_sample_membership_time() RETURNS timestamptz LANGUAGE sql AS $x$ SELECT '1999-01-01 00:00:00+00'::timestamptz $x$`,
			`CREATE FUNCTION hostile.runtime_release_fenced_replica_claims(uuid,timestamptz) RETURNS integer LANGUAGE sql AS $x$ SELECT 99 $x$`,
		} {
			if _, err := db.ExecContext(context.Background(), statement); err != nil {
				t.Fatal(err)
			}
		}
		var result membershipLeaveResult
		if err := lifecycleCallWithSearchPath(t, db, membershipAppRole, "hostile,pg_temp,public",
			membershipServingLeaveExpr(first, 5, "op-scale-in"), membershipLeaveProjection,
			&result.replicaID, &result.state, &result.operationID, &result.controllerGeneration,
			&result.transitionCount, &result.claimCount, &result.recordedAt, &result.replayed); err != nil {
			t.Fatal(err)
		}
		if result.state != "left" || result.controllerGeneration != 5 || result.replayed {
			t.Fatalf("hostile path leave=%+v", result)
		}
		var decoys int
		if err := db.QueryRowContext(context.Background(), `SELECT (SELECT count(*) FROM hostile.runtime_capacity)+(SELECT count(*) FROM hostile.runtime_leave_receipts)+(SELECT count(*) FROM hostile.runtime_fencing_proofs)+(SELECT count(*) FROM hostile.shared_claim_requests)`).Scan(&decoys); err != nil || decoys != 0 {
			t.Fatalf("hostile decoys=%d error=%v", decoys, err)
		}
		if capacity := lifecycleCapacity(t, db); capacity.updatedAt.UTC().Year() == 1999 {
			t.Fatalf("a hostile clock reached capacity=%+v", capacity)
		}
		if receipt := membershipReceipt(t, db, first.replicaID); !receipt.exists || receipt.recordedAt.UTC().Year() == 1999 {
			t.Fatalf("a hostile clock reached the receipt=%+v", receipt)
		}
	})

	t.Run("generation_exhaustion_is_detected_before_addition", func(t *testing.T) {
		db, _ := membershipEvidenceDB(t)
		first, _ := membershipServingDrained(t, db)
		lifecycleWrite(t, db, `UPDATE public.runtime_capacity SET generation=9223372036854775807 WHERE singleton`)
		requireMembershipLeaveError(t, db, membershipAppRole, membershipServingLeaveExpr(first, 5, "op-scale-in"), "55000")
		requireMembershipProofError(t, db, membershipValidProofExpr(first, "request-full", "evidence-full"), "55000")
		if state := lifecycleReplicaState(t, db, first.replicaID); state != "draining" {
			t.Fatalf("exhaustion changed the replica state=%q", state)
		}
		if receipt := membershipReceipt(t, db, first.replicaID); receipt.exists {
			t.Fatal("exhaustion stored a receipt")
		}
		if proof := membershipProof(t, db, first.replicaID); proof.exists {
			t.Fatal("exhaustion stored a proof")
		}
	})
}

// TestRuntimeMembershipEvidenceQuerySourceParses proves the owned sqlc source
// names the three installed functions with their exact argument types, uses one
// materialized function result per query, and projects only real composite
// attributes.
func TestRuntimeMembershipEvidenceQuerySourceParses(t *testing.T) {
	db, _ := membershipEvidenceDB(t)
	source, err := os.ReadFile(filepath.Join("..", "sql", "runtime_membership_evidence.sql"))
	if err != nil {
		t.Fatal(err)
	}
	placeholder := regexp.MustCompile(`sqlc\.n?arg\([a-z_0-9]+\)`)
	found := map[string]bool{}
	index := 0
	for _, block := range strings.Split(string(source), "-- name: ") {
		trimmed := strings.TrimSpace(block)
		if trimmed == "" {
			continue
		}
		name := strings.Fields(trimmed)[0]
		role, wanted := membershipQueryNames[name]
		if !wanted {
			t.Errorf("query %s is not part of this slice", name)
			continue
		}
		found[name] = true
		body := strings.TrimSpace(trimmed[strings.Index(trimmed, "\n")+1:])
		if !strings.Contains(body, "AS MATERIALIZED") {
			t.Errorf("query %s does not use one MATERIALIZED function result", name)
		}
		if strings.Contains(body, "COALESCE") {
			t.Errorf("query %s coalesces a required result attribute", name)
		}
		index++
		statement := fmt.Sprintf("PREPARE membership_evidence_parse_%d AS %s", index, placeholder.ReplaceAllString(body, "NULL"))
		if err := registrationRoleExec(t, db, role, statement); err != nil {
			t.Errorf("query %s does not parse as %s: %v", name, role, err)
		}
	}
	for name := range membershipQueryNames {
		if !found[name] {
			t.Errorf("query %s is missing from the source", name)
		}
	}
}
