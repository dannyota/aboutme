package migrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRuntimeSharedClaimSchemaObjectsExist(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	for _, name := range []string{
		"shared_claim_policies",
		"shared_claim_scope_summaries",
		"shared_claim_requests",
		"shared_claim_scopes",
	} {
		var exists bool
		if err := db.QueryRowContext(ctx,
			`SELECT to_regclass('public.' || $1) IS NOT NULL`,
			name,
		).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("table %s is missing", name)
		}
	}
}

func TestRuntimeSharedClaimSchemaCatalogAndPublishedDigest(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	if err := membershipWrite(t, db, sharedClaimVectorFixture()...); err != nil {
		t.Fatal(err)
	}
	var rows int
	var digest string
	if err := db.QueryRowContext(ctx, `SELECT count(*),(SELECT encode(public.runtime_shared_claim_request_digest($1),'hex') FROM public.shared_claim_requests WHERE claim_id=$1) FROM public.shared_claim_policies`, claimVectorID).Scan(&rows, &digest); err != nil {
		t.Fatal(err)
	}
	if rows != 6 || digest != claimVectorDigest {
		t.Fatalf("catalog rows=%d digest=%s", rows, digest)
	}
	var exact bool
	if err := db.QueryRowContext(ctx, `SELECT array_agg(ROW(policy_id,scope_kind,running_limit,waiting_limit,queue_enabled,deadline_mode)::text ORDER BY policy_id,scope_kind)=ARRAY['(mail.send,global,2,0,f,none)','(mcp.user_concurrent,user,4,0,f,none)','(password.hash,global,2,16,t,none)','(render.global_claim,global,1,8,t,render_20s)','(sse.fleet_account_ip,account,20,0,f,none)','(sse.fleet_account_ip,ip,100,0,f,none)'] FROM public.shared_claim_policies`).Scan(&exact); err != nil || !exact {
		t.Fatalf("exact catalog=%t error=%v", exact, err)
	}
	requireSharedClaimPGError(t, membershipWrite(t, db, `UPDATE public.shared_claim_policies SET running_limit=running_limit+1 WHERE policy_id='mail.send'`), "55000", "", "")
	requireSharedClaimPGError(t, membershipWrite(t, db, `DELETE FROM public.shared_claim_policies WHERE policy_id='mail.send'`), "55000", "", "")
}

func TestRuntimeSharedClaimSchemaPreservesPopulatedVersion16(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ProvisionDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := applyFS(ctx, db, runtimeTransitionFixtureFS(t, 16), LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO public.users(id,email,name) VALUES('cccccccc-cccc-4ccc-8ccc-cccccccccccc','claim-preserve@example.test','claim preserved')`); err != nil {
		t.Fatal(err)
	}
	preserved := append(transitionFixture(), transitionAckSQL(initiatorID), `INSERT INTO public.runtime_lifecycle_operations(operation_id,workflow_kind) VALUES('claim-preserve-lifecycle','initial_serving')`)
	if err := membershipWrite(t, db, preserved...); err != nil {
		t.Fatal(err)
	}
	var before int64
	if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := applyFS(ctx, db, runtimeTransitionFixtureFS(t, 17), LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	var after int64
	var name, instance, workflow string
	var targets, required, acks int
	if err := db.QueryRowContext(ctx, `SELECT s.generation,u.name,r.instance_id,o.workflow_kind,(SELECT count(*) FROM public.public_transition_targets WHERE transition_id=$2),(SELECT count(*) FROM public.public_transition_replicas WHERE transition_id=$2),(SELECT count(*) FROM public.public_transition_acks WHERE transition_id=$2) FROM public.runtime_write_state s CROSS JOIN public.users u CROSS JOIN public.runtime_replicas r CROSS JOIN public.runtime_lifecycle_operations o WHERE s.singleton AND u.id='cccccccc-cccc-4ccc-8ccc-cccccccccccc' AND r.replica_id=$1 AND o.operation_id='claim-preserve-lifecycle'`, initiatorID, transitionID).Scan(&after, &name, &instance, &workflow, &targets, &required, &acks); err != nil {
		t.Fatal(err)
	}
	if after != before+1 || name != "claim preserved" || instance != initiatorInstance || workflow != "initial_serving" || targets != 2 || required != 1 || acks != 1 {
		t.Fatalf("preservation generation=%d/%d name=%q instance=%q workflow=%q transition=%d/%d/%d", before, after, name, instance, workflow, targets, required, acks)
	}
}

func TestRuntimeSharedClaimSchemaPublishedVectorIndependentGoEncoding(t *testing.T) {
	encoded := append([]byte("aboutme.shared-claim.request.v1"), 0, 1)
	claim, err := hex.DecodeString("00000000000000000000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	replica, err := hex.DecodeString("00000000000000000000000000000002")
	if err != nil {
		t.Fatal(err)
	}
	encoded = append(encoded, claim...)
	encoded = append(encoded, byte(len("sse.fleet_account_ip")))
	encoded = append(encoded, []byte("sse.fleet_account_ip")...)
	encoded = append(encoded, replica...)
	encoded = append(encoded, 0, 2, 1, byte(len("ip")))
	encoded = append(encoded, 'i', 'p')
	encoded = append(encoded, make([]byte, 32)...)
	encoded = append(encoded, 2, byte(len("account")))
	encoded = append(encoded, []byte("account")...)
	for i := 0; i < 32; i++ {
		encoded = append(encoded, 0xff)
	}
	if len(encoded) != 165 {
		t.Fatalf("frame length=%d", len(encoded))
	}
	digest := sha256.Sum256(encoded)
	if got := hex.EncodeToString(digest[:]); got != claimVectorDigest {
		t.Fatalf("digest=%s", got)
	}
}

func TestRuntimeSharedClaimSchemaParentAndScopeConstraints(t *testing.T) {
	tests := []struct{ name, old, replacement, code, constraint, column string }{
		{"nil_claim", claimVectorID, "00000000-0000-0000-0000-000000000000", "23514", "shared_claim_requests_id_non_nil", ""},
		{"nil_replica", "sse.fleet_account_ip','" + claimVectorReplica + "',NULL", "sse.fleet_account_ip','00000000-0000-0000-0000-000000000000',NULL", "23514", "shared_claim_requests_replica_non_nil", ""},
		{"short_request_digest", "decode('" + claimVectorDigest + "','hex')", "decode('00','hex')", "23514", "shared_claim_requests_digest_check", ""},
		{"short_scope_digest", "decode(repeat('ff',32),'hex')", "decode('ff','hex')", "23514", "shared_claim_scope_summaries_digest_check", ""},
		{"zero_allocation", ",1,'running')", ",0,'running')", "23514", "shared_claim_scopes_allocation_ordinal_check", ""},
		{"mismatched_child_state", ",1,'running')", ",1,'waiting')", "23514", "shared_claim_summary_exact_counts", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			requireSharedClaimPGError(t, membershipWrite(t, db, replaceSharedClaimFixture(t, test.old, test.replacement)...), test.code, test.constraint, test.column)
		})
	}
}

func TestRuntimeSharedClaimSchemaEveryRequiredColumnRejectsNull(t *testing.T) {
	tests := []struct{ name, old, replacement, column string }{
		{"request_claim", "VALUES('" + claimVectorID + "','sse", "VALUES(NULL,'sse", "claim_id"},
		{"request_policy", "'sse.fleet_account_ip','" + claimVectorReplica, "NULL,'" + claimVectorReplica, "policy_id"},
		{"request_replica", "'sse.fleet_account_ip','" + claimVectorReplica + "',NULL", "'sse.fleet_account_ip',NULL,NULL", "replica_id"},
		{"request_state", "NULL,'running',transaction_timestamp()", "NULL,NULL,transaction_timestamp()", "state"},
		{"request_admitted", "'running',transaction_timestamp(),NULL", "'running',NULL,NULL", "admitted_at"},
		{"request_scope_count", ",2,decode('" + claimVectorDigest, ",NULL,decode('" + claimVectorDigest, "scope_count"},
		{"request_digest", "decode('" + claimVectorDigest + "','hex'))", "NULL)", "request_digest"},
		{"summary_policy", "VALUES('sse.fleet_account_ip','ip',decode", "VALUES(NULL,'ip',decode", "policy_id"},
		{"summary_kind", "VALUES('sse.fleet_account_ip','ip',decode", "VALUES('sse.fleet_account_ip',NULL,decode", "scope_kind"},
		{"summary_digest", "VALUES('sse.fleet_account_ip','ip',decode(repeat('00',32),'hex')", "VALUES('sse.fleet_account_ip','ip',NULL", "scope_digest"},
		{"scope_claim", "VALUES('" + claimVectorID + "',1,'sse", "VALUES(NULL,1,'sse", "claim_id"},
		{"scope_request_ordinal", "VALUES('" + claimVectorID + "',1,'sse", "VALUES('" + claimVectorID + "',NULL,'sse", "request_ordinal"},
		{"scope_policy", ",1,'sse.fleet_account_ip','ip'", ",1,NULL,'ip'", "policy_id"},
		{"scope_kind", "'sse.fleet_account_ip','ip',decode", "'sse.fleet_account_ip',NULL,decode", "scope_kind"},
		{"scope_digest", "'ip',decode(repeat('00',32),'hex'),1", "'ip',NULL,1", "scope_digest"},
		{"scope_allocation", "'hex'),1,'running'),", "'hex'),NULL,'running'),", "allocation_ordinal"},
		{"scope_state", ",1,'running'),('" + claimVectorID, ",1,NULL),('" + claimVectorID, "state"},
		{"summary_running", ",1,0,2,transaction_timestamp())", ",NULL,0,2,transaction_timestamp())", "running_count"},
		{"summary_waiting", ",1,0,2,transaction_timestamp())", ",1,NULL,2,transaction_timestamp())", "waiting_count"},
		{"summary_next", ",1,0,2,transaction_timestamp())", ",1,0,NULL,transaction_timestamp())", "next_ordinal"},
		{"summary_updated", ",1,0,2,transaction_timestamp())", ",1,0,2,NULL)", "updated_at"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			requireSharedClaimPGError(t, membershipWrite(t, db, replaceSharedClaimFixture(t, test.old, test.replacement)...), "23502", "", test.column)
		})
	}
}

func TestRuntimeSharedClaimSchemaRenderWorkAndDeadlineMatrix(t *testing.T) {
	boundary := claimPolicyBoundaries[0]
	tests := []struct{ name, old, replacement, constraint string }{
		{"missing_work", "'" + capacityWorkID(0) + "','running'", "NULL,'running'", "shared_claim_requests_work_deadline_check"},
		{"nil_work", "'" + capacityWorkID(0) + "','running'", "'00000000-0000-0000-0000-000000000000','running'", "shared_claim_requests_work_non_nil"},
		{"null_deadline", "transaction_timestamp()+interval '20 seconds'", "NULL", "shared_claim_requests_work_deadline_check"},
		{"incorrect_deadline", "interval '20 seconds'", "interval '19 seconds'", "shared_claim_requests_work_deadline_check"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			fixture := sharedClaimCapacityFixture(boundary, "running", 1)
			joined := strings.Join(fixture, ";")
			if !strings.Contains(joined, test.old) {
				t.Fatalf("fixture missing %q", test.old)
			}
			requireSharedClaimPGError(t, membershipWrite(t, db, strings.Replace(joined, test.old, test.replacement, 1)), "23514", test.constraint, "")
		})
	}
}

func TestRuntimeSharedClaimSchemaParentFieldShapeMatrix(t *testing.T) {
	tests := []struct{ name, old, replacement, constraint string }{
		{"non_render_work", "NULL,'running'", "'00000000-0000-0000-0000-000000000009','running'", "shared_claim_requests_work_deadline_check"},
		{"non_render_deadline", "transaction_timestamp(),NULL,NULL,NULL,2", "transaction_timestamp(),transaction_timestamp()+interval '20 seconds',NULL,NULL,2", "shared_claim_requests_work_deadline_check"},
		{"live_released_at", "NULL,NULL,2,decode", "transaction_timestamp(),NULL,2,decode", "shared_claim_requests_release_shape_check"},
		{"live_release_reason", "NULL,NULL,2,decode", "NULL,'joined',2,decode", "shared_claim_requests_release_shape_check"},
		{"released_missing_time", "'running',transaction_timestamp(),NULL,NULL,NULL,2", "'released',transaction_timestamp(),NULL,NULL,'joined',2", "shared_claim_requests_release_shape_check"},
		{"released_missing_reason", "'running',transaction_timestamp(),NULL,NULL,NULL,2", "'released',transaction_timestamp(),NULL,transaction_timestamp(),NULL,2", "shared_claim_requests_release_shape_check"},
		{"unknown_reason", "'running',transaction_timestamp(),NULL,NULL,NULL,2", "'released',transaction_timestamp(),NULL,transaction_timestamp(),'unknown',2", "shared_claim_requests_release_shape_check"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			requireSharedClaimPGError(t, membershipWrite(t, db, replaceSharedClaimFixture(t, test.old, test.replacement)...), "23514", test.constraint, "")
		})
	}
}

func TestRuntimeSharedClaimSchemaLegalAndForbiddenStateEdges(t *testing.T) {
	for _, edge := range []struct {
		name, from, to string
		allowed        bool
	}{
		{"waiting_running", "waiting", "running", true}, {"waiting_released", "waiting", "released", true}, {"running_released", "running", "released", true},
		{"running_waiting", "running", "waiting", false}, {"released_running", "released", "running", false}, {"released_waiting", "released", "waiting", false},
	} {
		t.Run(edge.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			initialState := edge.from
			if initialState == "released" {
				initialState = "running"
			}
			parent, child := singleClaimSQL(claimVectorID, claimVectorReplica, "password.hash", "global", `decode(repeat('33',32),'hex')`, initialState, 1, nil)
			running, waiting := 1, 0
			if edge.from == "waiting" {
				running, waiting = 0, 1
			}
			fixture := append(replicaInsert(claimVectorReplica, "i-00000000000000002", "2"), fmt.Sprintf(`INSERT INTO public.shared_claim_scope_summaries(policy_id,scope_kind,scope_digest,running_count,waiting_count,next_ordinal,updated_at) VALUES('password.hash','global',decode(repeat('33',32),'hex'),%d,%d,2,transaction_timestamp())`, running, waiting), parent, child)
			if err := membershipWrite(t, db, fixture...); err != nil {
				t.Fatal(err)
			}
			if edge.from == "released" {
				if err := membershipWrite(t, db, `UPDATE public.shared_claim_scopes SET state='released'`, `UPDATE public.shared_claim_requests SET state='released',released_at=transaction_timestamp(),release_reason='joined'`, `UPDATE public.shared_claim_scope_summaries SET running_count=0,waiting_count=0,updated_at=transaction_timestamp()`); err != nil {
					t.Fatal(err)
				}
			}
			parentFields := ""
			if edge.to == "released" {
				parentFields = ",released_at=transaction_timestamp(),release_reason='joined'"
			}
			statements := []string{`UPDATE public.shared_claim_scopes SET state='` + edge.to + `'`, `UPDATE public.shared_claim_requests SET state='` + edge.to + `'` + parentFields}
			switch edge.to {
			case "running":
				statements = append(statements, `UPDATE public.shared_claim_scope_summaries SET running_count=1,waiting_count=0,updated_at=transaction_timestamp()`)
			case "released":
				statements = append(statements, `UPDATE public.shared_claim_scope_summaries SET running_count=0,waiting_count=0,updated_at=transaction_timestamp()`)
			}
			err := membershipWrite(t, db, statements...)
			if edge.allowed {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			requireSharedClaimPGError(t, err, "55000", "", "")
		})
	}
}

func TestRuntimeSharedClaimSchemaDigestBindsIdentityAndShape(t *testing.T) {
	firstReplica, secondReplica := claimVectorReplica, "00000000-0000-0000-0000-000000000003"
	work1, work2 := "00000000-0000-0000-0000-000000000010", "00000000-0000-0000-0000-000000000011"
	tests := []struct {
		name, actualClaim, actualReplica, policy, kind, digest string
		actualWork                                             *string
		oldClaim, oldReplica, oldPolicy, oldKind, oldDigest    string
		oldWork                                                *string
		oldOrdinal                                             int
	}{
		{"claim", "00000000-0000-0000-0000-000000000005", firstReplica, "mail.send", "global", "88", nil, claimVectorID, firstReplica, "mail.send", "global", "88", nil, 1},
		{"policy", claimVectorID, firstReplica, "mail.send", "global", "88", nil, claimVectorID, firstReplica, "password.hash", "global", "88", nil, 1},
		{"replica", claimVectorID, secondReplica, "mail.send", "global", "88", nil, claimVectorID, firstReplica, "mail.send", "global", "88", nil, 1},
		{"work_presence", claimVectorID, firstReplica, "render.global_claim", "global", "88", &work1, claimVectorID, firstReplica, "render.global_claim", "global", "88", nil, 1},
		{"work_uuid", claimVectorID, firstReplica, "render.global_claim", "global", "88", &work2, claimVectorID, firstReplica, "render.global_claim", "global", "88", &work1, 1},
		{"request_ordinal", claimVectorID, firstReplica, "mail.send", "global", "88", nil, claimVectorID, firstReplica, "mail.send", "global", "88", nil, 2},
		{"kind", claimVectorID, firstReplica, "mail.send", "global", "88", nil, claimVectorID, firstReplica, "mail.send", "user", "88", nil, 1},
		{"scope_digest", claimVectorID, firstReplica, "mail.send", "global", "88", nil, claimVectorID, firstReplica, "mail.send", "global", "99", nil, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			scope := fmt.Sprintf(`decode(repeat('%s',32),'hex')`, test.digest)
			parent, child := singleClaimSQL(test.actualClaim, test.actualReplica, test.policy, test.kind, scope, "running", 1, test.actualWork)
			actualDigest := singleClaimDigestSQL(test.actualClaim, test.actualReplica, test.policy, test.kind, scope, test.actualWork)
			oldDigest := singleClaimDigestCustomSQL(test.oldClaim, test.oldReplica, test.oldPolicy, test.oldOrdinal, test.oldKind, fmt.Sprintf(`decode(repeat('%s',32),'hex')`, test.oldDigest), test.oldWork)
			parent = strings.Replace(parent, actualDigest, oldDigest, 1)
			fixture := append(replicaInsert(firstReplica, "i-00000000000000002", "2"), replicaInsert(secondReplica, "i-00000000000000003", "3")...)
			fixture = append(fixture, fmt.Sprintf(`INSERT INTO public.shared_claim_scope_summaries(policy_id,scope_kind,scope_digest,running_count,waiting_count,next_ordinal,updated_at) VALUES('%s','%s',%s,1,0,2,transaction_timestamp())`, test.policy, test.kind, scope), parent, child)
			requireSharedClaimPGError(t, membershipWrite(t, db, fixture...), "23514", "shared_claim_request_complete", "")
		})
	}
	t.Run("scope_count", func(t *testing.T) {
		db, _ := runtimeMembershipDB(t)
		ip := `decode(repeat('00',32),'hex')`
		parent, child := singleClaimSQL(claimVectorID, firstReplica, "sse.fleet_account_ip", "ip", ip, "running", 1, nil)
		parent = strings.Replace(parent, singleClaimDigestSQL(claimVectorID, firstReplica, "sse.fleet_account_ip", "ip", ip, nil), dualClaimDigestSQL(claimVectorID, firstReplica, ip, `decode(repeat('ff',32),'hex')`), 1)
		fixture := append(replicaInsert(firstReplica, "i-00000000000000002", "2"), `INSERT INTO public.shared_claim_scope_summaries(policy_id,scope_kind,scope_digest,running_count,waiting_count,next_ordinal,updated_at) VALUES('sse.fleet_account_ip','ip',decode(repeat('00',32),'hex'),1,0,2,transaction_timestamp())`, parent, child)
		requireSharedClaimPGError(t, membershipWrite(t, db, fixture...), "23514", "shared_claim_request_complete", "")
	})
}

func TestRuntimeSharedClaimSchemaC05RequiresIPFirst(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	requireSharedClaimPGError(t, membershipWrite(t, db, sharedClaimAccountOnlyFixture()...), "23514", "shared_claim_request_complete", "")
}

func TestRuntimeSharedClaimSchemaC05ScopeShapeMatrix(t *testing.T) {
	t.Run("missing_account", func(t *testing.T) {
		db, _ := runtimeMembershipDB(t)
		fixture := sharedClaimVectorFixture()
		fixture[2] = strings.Replace(fixture[2], "'account',decode(repeat('ff',32),'hex'),1,0,2", "'account',decode(repeat('ff',32),'hex'),0,0,1", 1)
		fixture[len(fixture)-1] = fmt.Sprintf(`INSERT INTO public.shared_claim_scopes(claim_id,request_ordinal,policy_id,scope_kind,scope_digest,allocation_ordinal,state) VALUES('%s',1,'sse.fleet_account_ip','ip',decode(repeat('00',32),'hex'),1,'running')`, claimVectorID)
		requireSharedClaimPGError(t, membershipWrite(t, db, fixture...), "23514", "shared_claim_request_complete", "")
	})
	t.Run("extra_account", func(t *testing.T) {
		db, _ := runtimeMembershipDB(t)
		ip := `decode(repeat('00',32),'hex')`
		account := `decode(repeat('ff',32),'hex')`
		parent, child := singleClaimSQL(claimVectorID, claimVectorReplica, "sse.fleet_account_ip", "ip", ip, "running", 1, nil)
		fixture := append(replicaInsert(claimVectorReplica, "i-00000000000000002", "2"),
			`INSERT INTO public.shared_claim_scope_summaries(policy_id,scope_kind,scope_digest,running_count,waiting_count,next_ordinal,updated_at) VALUES('sse.fleet_account_ip','ip',decode(repeat('00',32),'hex'),1,0,2,transaction_timestamp()),('sse.fleet_account_ip','account',decode(repeat('ff',32),'hex'),1,0,2,transaction_timestamp())`, parent, child,
			fmt.Sprintf(`INSERT INTO public.shared_claim_scopes(claim_id,request_ordinal,policy_id,scope_kind,scope_digest,allocation_ordinal,state) VALUES('%s',2,'sse.fleet_account_ip','account',%s,1,'running')`, claimVectorID, account))
		requireSharedClaimPGError(t, membershipWrite(t, db, fixture...), "23514", "shared_claim_request_complete", "")
	})
	t.Run("swapped", func(t *testing.T) {
		db, _ := runtimeMembershipDB(t)
		fixture := sharedClaimVectorFixture()
		digest := twoScopeClaimDigestSQL(claimVectorID, claimVectorReplica, "account", `decode(repeat('ff',32),'hex')`, "ip", `decode(repeat('00',32),'hex')`)
		fixture[3] = fmt.Sprintf(`INSERT INTO public.shared_claim_requests(claim_id,policy_id,replica_id,state,admitted_at,scope_count,request_digest) VALUES('%s','sse.fleet_account_ip','%s','running',transaction_timestamp(),2,%s)`, claimVectorID, claimVectorReplica, digest)
		fixture[4] = fmt.Sprintf(`INSERT INTO public.shared_claim_scopes(claim_id,request_ordinal,policy_id,scope_kind,scope_digest,allocation_ordinal,state) VALUES('%s',1,'sse.fleet_account_ip','account',decode(repeat('ff',32),'hex'),1,'running'),('%s',2,'sse.fleet_account_ip','ip',decode(repeat('00',32),'hex'),1,'running')`, claimVectorID, claimVectorID)
		requireSharedClaimPGError(t, membershipWrite(t, db, fixture...), "23514", "shared_claim_request_complete", "")
	})
	t.Run("duplicate_kind", func(t *testing.T) {
		db, _ := runtimeMembershipDB(t)
		fixture := sharedClaimVectorFixture()
		fixture[4] = fmt.Sprintf(`INSERT INTO public.shared_claim_scopes(claim_id,request_ordinal,policy_id,scope_kind,scope_digest,allocation_ordinal,state) VALUES('%s',1,'sse.fleet_account_ip','ip',decode(repeat('00',32),'hex'),1,'running'),('%s',2,'sse.fleet_account_ip','ip',decode(repeat('00',32),'hex'),2,'running')`, claimVectorID, claimVectorID)
		requireSharedClaimPGError(t, membershipWrite(t, db, fixture...), "23505", "shared_claim_scopes_claim_kind_key", "")
	})
	t.Run("cross_parent_policy", func(t *testing.T) {
		db, _ := runtimeMembershipDB(t)
		fixture := sharedClaimVectorFixture()
		other := "00000000-0000-0000-0000-000000000009"
		parent, _ := singleClaimSQL(other, claimVectorReplica, "password.hash", "global", `decode(repeat('77',32),'hex')`, "running", 1, nil)
		fixture = append(fixture[:4], append([]string{parent}, fixture[4:]...)...)
		fixture[5] = strings.Replace(fixture[5], "('"+claimVectorID+"',2", "('"+other+"',2", 1)
		requireSharedClaimPGError(t, membershipWrite(t, db, fixture...), "23503", "shared_claim_scopes_parent_fkey", "")
	})
}

func TestRuntimeSharedClaimSchemaC05AtomicInsertReleaseAndRollback(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	invalid := sharedClaimVectorFixture()
	invalid[2] = strings.Replace(invalid[2], "'account',decode(repeat('ff',32),'hex'),1,0,2", "'account',decode(repeat('ff',32),'hex'),0,0,1", 1)
	invalid[len(invalid)-1] = fmt.Sprintf(`INSERT INTO public.shared_claim_scopes(claim_id,request_ordinal,policy_id,scope_kind,scope_digest,allocation_ordinal,state) VALUES('%s',1,'sse.fleet_account_ip','ip',decode(repeat('00',32),'hex'),1,'running')`, claimVectorID)
	requireSharedClaimPGError(t, membershipWrite(t, db, invalid...), "23514", "shared_claim_request_complete", "")
	var parents, scopes, summaries int
	if err := db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM public.shared_claim_requests),(SELECT count(*) FROM public.shared_claim_scopes),(SELECT count(*) FROM public.shared_claim_scope_summaries)`).Scan(&parents, &scopes, &summaries); err != nil {
		t.Fatal(err)
	}
	if parents != 0 || scopes != 0 || summaries != 0 {
		t.Fatalf("partial insert parents=%d scopes=%d summaries=%d", parents, scopes, summaries)
	}
	if err := membershipWrite(t, db, sharedClaimVectorFixture()...); err != nil {
		t.Fatal(err)
	}
	requireSharedClaimPGError(t, membershipWrite(t, db, `UPDATE public.shared_claim_scopes SET state='released' WHERE claim_id='`+claimVectorID+`' AND request_ordinal=1`, `UPDATE public.shared_claim_requests SET state='released',released_at=transaction_timestamp(),release_reason='joined' WHERE claim_id='`+claimVectorID+`'`, `UPDATE public.shared_claim_scope_summaries SET running_count=0,updated_at=transaction_timestamp() WHERE scope_kind='ip'`), "23514", "shared_claim_request_complete", "")
	var runningParents, runningScopes, totalRunning int
	if err := db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM public.shared_claim_requests WHERE state='running'),(SELECT count(*) FROM public.shared_claim_scopes WHERE state='running'),(SELECT sum(running_count) FROM public.shared_claim_scope_summaries)`).Scan(&runningParents, &runningScopes, &totalRunning); err != nil {
		t.Fatal(err)
	}
	if runningParents != 1 || runningScopes != 2 || totalRunning != 2 {
		t.Fatalf("partial release parents=%d scopes=%d counts=%d", runningParents, runningScopes, totalRunning)
	}
	if err := membershipWrite(t, db, `UPDATE public.shared_claim_scopes SET state='released' WHERE claim_id='`+claimVectorID+`'`, `UPDATE public.shared_claim_requests SET state='released',released_at=transaction_timestamp(),release_reason='joined' WHERE claim_id='`+claimVectorID+`'`, `UPDATE public.shared_claim_scope_summaries SET running_count=0,updated_at=transaction_timestamp()`); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeSharedClaimSchemaExactCountsAndReceiptPreservation(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	requireSharedClaimPGError(t, membershipWrite(t, db, replaceSharedClaimFixture(t, ",1,0,2,transaction_timestamp())", ",0,0,2,transaction_timestamp())")...), "23514", "shared_claim_summary_exact_counts", "")
	if err := membershipWrite(t, db, sharedClaimVectorFixture()...); err != nil {
		t.Fatal(err)
	}
	requireSharedClaimPGError(t, membershipWrite(t, db, `DELETE FROM public.shared_claim_requests WHERE claim_id='`+claimVectorID+`'`), "55000", "", "")
	if err := membershipWrite(t, db,
		`UPDATE public.shared_claim_scopes SET state='released' WHERE claim_id='`+claimVectorID+`'`,
		`UPDATE public.shared_claim_requests SET state='released',released_at=transaction_timestamp(),release_reason='joined' WHERE claim_id='`+claimVectorID+`'`,
		`UPDATE public.shared_claim_scope_summaries SET running_count=0,updated_at=transaction_timestamp()`,
	); err != nil {
		t.Fatal(err)
	}
	if err := membershipWrite(t, db,
		`DELETE FROM public.shared_claim_scopes WHERE claim_id='`+claimVectorID+`'`,
		`DELETE FROM public.shared_claim_requests WHERE claim_id='`+claimVectorID+`'`,
		`DELETE FROM public.shared_claim_scope_summaries`,
	); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeSharedClaimSchemaIdentityAndTerminalFieldsAreImmutable(t *testing.T) {
	for _, mutation := range []struct{ name, statement string }{
		{"parent_claim", `UPDATE public.shared_claim_requests SET claim_id='00000000-0000-0000-0000-000000000009'`},
		{"parent_policy", `UPDATE public.shared_claim_requests SET policy_id='password.hash'`},
		{"parent_replica", `UPDATE public.shared_claim_requests SET replica_id='00000000-0000-0000-0000-000000000009'`},
		{"parent_work", `UPDATE public.shared_claim_requests SET work_id='00000000-0000-0000-0000-000000000009'`},
		{"parent_admitted", `UPDATE public.shared_claim_requests SET admitted_at=admitted_at+interval '1 second'`},
		{"parent_scope_count", `UPDATE public.shared_claim_requests SET scope_count=1`},
		{"parent_digest", `UPDATE public.shared_claim_requests SET request_digest=decode(repeat('aa',32),'hex')`},
		{"scope_claim", `UPDATE public.shared_claim_scopes SET claim_id='00000000-0000-0000-0000-000000000009' WHERE request_ordinal=1`},
		{"scope_ordinal", `UPDATE public.shared_claim_scopes SET request_ordinal=2 WHERE request_ordinal=1`},
		{"scope_policy", `UPDATE public.shared_claim_scopes SET policy_id='password.hash' WHERE request_ordinal=1`},
		{"scope_kind", `UPDATE public.shared_claim_scopes SET scope_kind='account' WHERE request_ordinal=1`},
		{"scope_digest", `UPDATE public.shared_claim_scopes SET scope_digest=decode(repeat('aa',32),'hex') WHERE request_ordinal=1`},
		{"scope_allocation", `UPDATE public.shared_claim_scopes SET allocation_ordinal=2 WHERE request_ordinal=1`},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			if err := membershipWrite(t, db, sharedClaimVectorFixture()...); err != nil {
				t.Fatal(err)
			}
			requireSharedClaimPGError(t, membershipWrite(t, db, mutation.statement), "55000", "", "")
		})
	}
	db, _ := runtimeMembershipDB(t)
	if err := membershipWrite(t, db, sharedClaimVectorFixture()...); err != nil {
		t.Fatal(err)
	}
	if err := membershipWrite(t, db, `UPDATE public.shared_claim_scopes SET state='released'`, `UPDATE public.shared_claim_requests SET state='released',released_at=transaction_timestamp(),release_reason='joined'`, `UPDATE public.shared_claim_scope_summaries SET running_count=0,updated_at=transaction_timestamp()`); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{`UPDATE public.shared_claim_requests SET released_at=released_at+interval '1 second'`, `UPDATE public.shared_claim_requests SET release_reason='canceled'`, `UPDATE public.shared_claim_requests SET state='released'`, `UPDATE public.shared_claim_scopes SET state='released'`} {
		requireSharedClaimPGError(t, membershipWrite(t, db, statement), "55000", "", "")
	}
}

func TestRuntimeSharedClaimSchemaReferencedZeroSummaryAndNewLifetime(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	if err := membershipWrite(t, db, sharedClaimVectorFixture()...); err != nil {
		t.Fatal(err)
	}
	if err := membershipWrite(t, db, `UPDATE public.shared_claim_scopes SET state='released'`, `UPDATE public.shared_claim_requests SET state='released',released_at=transaction_timestamp(),release_reason='joined'`, `UPDATE public.shared_claim_scope_summaries SET running_count=0,updated_at=transaction_timestamp()`); err != nil {
		t.Fatal(err)
	}
	requireSharedClaimPGError(t, membershipWrite(t, db, `DELETE FROM public.shared_claim_scope_summaries WHERE scope_kind='ip'`), "55000", "", "")
	if err := membershipWrite(t, db, `DELETE FROM public.shared_claim_scopes`, `DELETE FROM public.shared_claim_requests`, `DELETE FROM public.shared_claim_scope_summaries`); err != nil {
		t.Fatal(err)
	}
	parent, child := singleClaimSQL("00000000-0000-0000-0000-000000000009", claimVectorReplica, "sse.fleet_account_ip", "ip", `decode(repeat('00',32),'hex')`, "running", 1, nil)
	if err := membershipWrite(t, db, `INSERT INTO public.shared_claim_scope_summaries(policy_id,scope_kind,scope_digest,running_count,waiting_count,next_ordinal,updated_at) VALUES('sse.fleet_account_ip','ip',decode(repeat('00',32),'hex'),1,0,2,transaction_timestamp())`, parent, child); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeSharedClaimSchemaFailedWriteRollsBackGenerationAndRows(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	var before int64
	if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	requireSharedClaimPGError(t, membershipWrite(t, db, replaceSharedClaimFixture(t, ",1,0,2,transaction_timestamp())", ",0,0,2,transaction_timestamp())")...), "23514", "shared_claim_summary_exact_counts", "")
	var after int64
	var parents, scopes int
	if err := db.QueryRowContext(ctx, `SELECT generation,(SELECT count(*) FROM public.shared_claim_requests),(SELECT count(*) FROM public.shared_claim_scopes) FROM public.runtime_write_state WHERE singleton`).Scan(&after, &parents, &scopes); err != nil {
		t.Fatal(err)
	}
	if after != before || parents != 0 || scopes != 0 {
		t.Fatalf("failed write generation=%d/%d parents=%d scopes=%d", before, after, parents, scopes)
	}
}

func TestRuntimeSharedClaimSchemaFailedMigrationRollsBackSchemaSeedAndGeneration(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := ProvisionDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := applyFS(ctx, db, runtimeTransitionFixtureFS(t, 16), LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	var before int64
	if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	fixture := runtimeTransitionFixtureFS(t, 17)
	file := fixture["00017_runtime_shared_claim_schema.sql"]
	if file == nil {
		t.Fatal("migration 17 fixture is missing")
	}
	source := string(file.Data)
	needle := "SELECT public.runtime_finish_write();"
	if !strings.Contains(source, needle) {
		t.Fatal("migration 17 finish statement is missing")
	}
	file.Data = []byte(strings.Replace(source, needle, "SELECT 1/0;\n"+needle, 1))
	_, applyErr := applyFS(ctx, db, fixture, LocalAdminMigratorIdentity())
	requireSharedClaimPGError(t, applyErr, "22012", "", "")
	var after int64
	var tableExists bool
	if err := db.QueryRowContext(ctx, `SELECT generation,to_regclass('public.shared_claim_policies') IS NOT NULL FROM public.runtime_write_state WHERE singleton`).Scan(&after, &tableExists); err != nil {
		t.Fatal(err)
	}
	if after != before || tableExists {
		t.Fatalf("failed migration generation=%d/%d table_exists=%t", before, after, tableExists)
	}
}

func TestRuntimeSharedClaimSchemaForcedCheckDoesNotLoseLaterAssertion(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	fixture := append(sharedClaimVectorFixture(),
		`SET CONSTRAINTS shared_claim_requests_complete,shared_claim_scopes_request_complete,shared_claim_scope_summaries_exact,shared_claim_scopes_summary_exact IMMEDIATE`,
		`SET CONSTRAINTS shared_claim_requests_complete,shared_claim_scopes_request_complete,shared_claim_scope_summaries_exact,shared_claim_scopes_summary_exact DEFERRED`,
		`UPDATE public.shared_claim_scope_summaries SET running_count=0,updated_at=transaction_timestamp() WHERE scope_kind='ip'`,
	)
	requireSharedClaimPGError(t, membershipWrite(t, db, fixture...), "23514", "shared_claim_summary_exact_counts", "")
}

func TestRuntimeSharedClaimSchemaAllocationOrdinalAndCapacityBounds(t *testing.T) {
	tests := []struct {
		name        string
		old         string
		replacement string
		constraint  string
	}{
		{"allocation_not_below_next", ",1,0,2,transaction_timestamp())", ",1,0,1,transaction_timestamp())", "shared_claim_summary_exact_counts"},
		{"running_over_ip_limit", ",1,0,2,transaction_timestamp())", ",101,0,102,transaction_timestamp())", "shared_claim_summary_exact_counts"},
		{"negative_count", ",1,0,2,transaction_timestamp())", ",-1,0,2,transaction_timestamp())", "shared_claim_scope_summaries_counts_check"},
		{"zero_next", ",1,0,2,transaction_timestamp())", ",1,0,0,transaction_timestamp())", "shared_claim_scope_summaries_next_ordinal_check"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, _ := runtimeMembershipDB(t)
			requireSharedClaimPGError(t, membershipWrite(t, db, replaceSharedClaimFixture(t, test.old, test.replacement)...), "23514", test.constraint, "")
		})
	}
}

func TestRuntimeSharedClaimSchemaEveryPolicyCapacityBoundary(t *testing.T) {
	for _, boundary := range claimPolicyBoundaries {
		for _, stateLimit := range []struct {
			state string
			limit int
		}{{"running", boundary.running}, {"waiting", boundary.waiting}} {
			t.Run(boundary.name+"_"+stateLimit.state, func(t *testing.T) {
				db, _ := runtimeMembershipDB(t)
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := membershipWrite(t, db, sharedClaimCapacityFixture(boundary, stateLimit.state, stateLimit.limit)...); err != nil {
					t.Fatalf("exact boundary: %v", err)
				}
				overflow := sharedClaimCapacityOverflow(boundary, stateLimit.state, stateLimit.limit+1)
				requireSharedClaimPGError(t, membershipWrite(t, db, overflow...), "23514", "shared_claim_summary_exact_counts", "")
				var actual int
				if err := db.QueryRowContext(ctx, fmt.Sprintf(`SELECT %s_count FROM public.shared_claim_scope_summaries WHERE policy_id=$1 AND scope_kind=$2 AND scope_digest=decode(repeat($3,32),'hex')`, stateLimit.state), boundary.policy, boundary.kind, boundary.digestByte).Scan(&actual); err != nil {
					t.Fatal(err)
				}
				if actual != stateLimit.limit {
					t.Fatalf("count=%d want=%d", actual, stateLimit.limit)
				}
			})
		}
	}
}

func TestRuntimeSharedClaimSchemaPrivilegesAndWriteEntry(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	for _, role := range []string{"aboutme_app", "aboutme_maintenance", "aboutme_lifecycle_command", "aboutme_fencing_proof", "aboutme_restore_verify", "aboutme_migrator"} {
		t.Run(role, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close() //nolint:errcheck
			if _, err = conn.ExecContext(ctx, `SET SESSION AUTHORIZATION `+role); err != nil {
				t.Fatal(err)
			}
			for _, statement := range []string{`INSERT INTO public.shared_claim_requests(claim_id) VALUES('10000000-0000-4000-8000-000000000000')`, `UPDATE public.shared_claim_scope_summaries SET running_count=0`, `DELETE FROM public.shared_claim_policies`, `TRUNCATE public.shared_claim_scopes`, `SELECT public.runtime_shared_claim_request_digest('10000000-0000-4000-8000-000000000000')`, `SELECT public.runtime_assert_shared_claim_request()`} {
				_, callErr := conn.ExecContext(ctx, statement)
				requireSharedClaimPGError(t, callErr, "42501", "", "")
			}
			if _, err = conn.ExecContext(ctx, `RESET SESSION AUTHORIZATION`); err != nil {
				t.Fatal(err)
			}
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := db.ExecContext(ctx, `SET ROLE aboutme_runtime_owner; INSERT INTO public.shared_claim_scope_summaries(policy_id,scope_kind,scope_digest,updated_at) VALUES('mail.send','global',decode(repeat('00',32),'hex'),transaction_timestamp())`)
	requireSharedClaimPGError(t, err, "AM001", "", "")
	for _, table := range []string{"shared_claim_policies", "shared_claim_scope_summaries", "shared_claim_requests", "shared_claim_scopes"} {
		requireSharedClaimPGError(t, membershipWrite(t, db, `TRUNCATE public.`+table+` CASCADE`), "AM001", "", "")
	}
	requireSharedClaimPGError(t, membershipWrite(t, db, `TRUNCATE public.shared_claim_scopes,public.shared_claim_requests,public.shared_claim_scope_summaries,public.shared_claim_policies CASCADE`), "AM001", "", "")
}

func TestRuntimeSharedClaimSchemaOwnerHelperAndTriggerCatalog(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	tables := []string{"shared_claim_policies", "shared_claim_scope_summaries", "shared_claim_requests", "shared_claim_scopes"}
	var badOwners, columnACLs int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_class WHERE relnamespace='public'::regnamespace AND relname=ANY($1) AND pg_get_userbyid(relowner)<>'aboutme_runtime_owner'`, tables).Scan(&badOwners); err != nil || badOwners != 0 {
		t.Fatalf("owners=%d error=%v", badOwners, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.column_privileges WHERE table_schema='public' AND table_name=ANY($1) AND grantee<>'aboutme_runtime_owner'`, tables).Scan(&columnACLs); err != nil || columnACLs != 0 {
		t.Fatalf("column ACLs=%d error=%v", columnACLs, err)
	}
	var helpersOK bool
	if err := db.QueryRowContext(ctx, `SELECT count(*)=8 AND bool_and(pg_get_userbyid(proowner)='aboutme_runtime_owner' AND prosecdef AND proconfig=ARRAY['search_path=pg_catalog']::text[] AND NOT has_function_privilege('public',oid,'EXECUTE')) FROM pg_proc WHERE pronamespace='public'::regnamespace AND proname=ANY(ARRAY['runtime_shared_claim_request_digest','runtime_assert_shared_claim_request','runtime_assert_shared_claim_summary','runtime_validate_shared_claim_policy','runtime_validate_shared_claim_request','runtime_validate_shared_claim_scope','runtime_validate_shared_claim_summary','runtime_reject_shared_claim_truncate'])`).Scan(&helpersOK); err != nil || !helpersOK {
		t.Fatalf("helpers=%t error=%v", helpersOK, err)
	}
	var triggerCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid WHERE c.relnamespace='public'::regnamespace AND c.relname=ANY($1) AND NOT t.tgisinternal`, tables).Scan(&triggerCount); err != nil || triggerCount != 16 {
		t.Fatalf("trigger count=%d error=%v", triggerCount, err)
	}
	var deferred, writeEntries, truncateGuards bool
	if err := db.QueryRowContext(ctx, `SELECT (SELECT count(*)=4 FROM pg_trigger WHERE tgname IN ('shared_claim_requests_complete','shared_claim_scopes_request_complete','shared_claim_scope_summaries_exact','shared_claim_scopes_summary_exact') AND tgdeferrable AND tginitdeferred AND (tgtype&1)=1),(SELECT count(*)=4 FROM pg_trigger WHERE tgname LIKE 'shared_claim%_write_entry' AND (tgtype&2)=2 AND (tgtype&1)=0),(SELECT count(*)=4 FROM pg_trigger WHERE tgname LIKE 'shared_claim%_no_truncate' AND (tgtype&2)=2 AND (tgtype&1)=0)`).Scan(&deferred, &writeEntries, &truncateGuards); err != nil || !deferred || !writeEntries || !truncateGuards {
		t.Fatalf("deferred=%t write=%t truncate=%t error=%v", deferred, writeEntries, truncateGuards, err)
	}
	if err := membershipWrite(t, db, `CREATE FUNCTION pg_temp.sha256(bytea) RETURNS bytea LANGUAGE sql AS 'SELECT decode(repeat(''00'',32),''hex'')'`, `SET LOCAL search_path=pg_temp,public,pg_catalog`, sharedClaimVectorFixture()[0], sharedClaimVectorFixture()[1], sharedClaimVectorFixture()[2], sharedClaimVectorFixture()[3], sharedClaimVectorFixture()[4]); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeSharedClaimSchemaDuplicateClaimSerializes(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	setup := append(replicaInsert(claimVectorReplica, "i-00000000000000002", "2"), `INSERT INTO public.shared_claim_scope_summaries(policy_id,scope_kind,scope_digest,running_count,waiting_count,next_ordinal,updated_at) VALUES('mail.send','global',decode(repeat('11',32),'hex'),0,0,1,transaction_timestamp())`)
	if err := membershipWrite(t, db, setup...); err != nil {
		t.Fatal(err)
	}
	parent, child := singleClaimSQL(claimVectorID, claimVectorReplica, "mail.send", "global", `decode(repeat('11',32),'hex')`, "running", 1, nil)
	_, first := beginSharedClaimWrite(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := first.ExecContext(ctx, parent); err != nil {
		t.Fatal(err)
	}
	if _, err := first.ExecContext(ctx, child); err != nil {
		t.Fatal(err)
	}
	if _, err := first.ExecContext(ctx, `UPDATE public.shared_claim_scope_summaries SET running_count=1,next_ordinal=2,updated_at=transaction_timestamp()`); err != nil {
		t.Fatal(err)
	}
	_, second := beginSharedClaimWrite(t, db)
	var secondPID int
	if err := second.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&secondPID); err != nil {
		t.Fatal(err)
	}
	raceCtx, cancelRace := context.WithTimeout(context.Background(), 5*time.Second)
	result := make(chan error, 1)
	go func() { _, err := second.ExecContext(raceCtx, parent); result <- err }()
	if err := waitForTransitionCondition(db, `SELECT cardinality(pg_blocking_pids($1))>0 OR EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')`, secondPID); err != nil {
		rollbackTransitionWrite(t, first)
		cancelRace()
		if raceErr := receiveSharedClaimRace(t, func() {}, result); raceErr != nil {
			t.Errorf("duplicate claim race cleanup: %v", raceErr)
		}
		rollbackTransitionWrite(t, second)
		t.Fatal(err)
	}
	if err := finishTransitionWrite(first); err != nil {
		cancelRace()
		if raceErr := receiveSharedClaimRace(t, func() {}, result); raceErr != nil {
			t.Errorf("duplicate claim race cleanup: %v", raceErr)
		}
		rollbackTransitionWrite(t, second)
		t.Fatal(err)
	}
	requireSharedClaimPGError(t, receiveSharedClaimRace(t, cancelRace, result), "23505", "shared_claim_requests_pkey", "")
	rollbackTransitionWrite(t, second)
}

func TestRuntimeSharedClaimSchemaDuplicateAllocationSerializes(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	secondReplica := "00000000-0000-0000-0000-000000000003"
	setup := append(replicaInsert(claimVectorReplica, "i-00000000000000002", "2"), replicaInsert(secondReplica, "i-00000000000000003", "3")...)
	setup = append(setup, `INSERT INTO public.shared_claim_scope_summaries(policy_id,scope_kind,scope_digest,running_count,waiting_count,next_ordinal,updated_at) VALUES('mail.send','global',decode(repeat('22',32),'hex'),0,0,1,transaction_timestamp())`)
	if err := membershipWrite(t, db, setup...); err != nil {
		t.Fatal(err)
	}
	parent1, child1 := singleClaimSQL(claimVectorID, claimVectorReplica, "mail.send", "global", `decode(repeat('22',32),'hex')`, "running", 1, nil)
	parent2, child2 := singleClaimSQL("00000000-0000-0000-0000-000000000004", secondReplica, "mail.send", "global", `decode(repeat('22',32),'hex')`, "running", 1, nil)
	_, first := beginSharedClaimWrite(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := first.ExecContext(ctx, parent1); err != nil {
		t.Fatal(err)
	}
	if _, err := first.ExecContext(ctx, child1); err != nil {
		t.Fatal(err)
	}
	if _, err := first.ExecContext(ctx, `UPDATE public.shared_claim_scope_summaries SET running_count=1,next_ordinal=2,updated_at=transaction_timestamp()`); err != nil {
		t.Fatal(err)
	}
	_, second := beginSharedClaimWrite(t, db)
	if _, err := second.ExecContext(ctx, parent2); err != nil {
		t.Fatal(err)
	}
	var secondPID int
	if err := second.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&secondPID); err != nil {
		t.Fatal(err)
	}
	raceCtx, cancelRace := context.WithTimeout(context.Background(), 5*time.Second)
	result := make(chan error, 1)
	go func() { _, err := second.ExecContext(raceCtx, child2); result <- err }()
	if err := waitForTransitionCondition(db, `SELECT cardinality(pg_blocking_pids($1))>0 OR EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')`, secondPID); err != nil {
		rollbackTransitionWrite(t, first)
		cancelRace()
		if raceErr := receiveSharedClaimRace(t, func() {}, result); raceErr != nil {
			t.Errorf("duplicate allocation race cleanup: %v", raceErr)
		}
		rollbackTransitionWrite(t, second)
		t.Fatal(err)
	}
	if err := finishTransitionWrite(first); err != nil {
		cancelRace()
		if raceErr := receiveSharedClaimRace(t, func() {}, result); raceErr != nil {
			t.Errorf("duplicate allocation race cleanup: %v", raceErr)
		}
		rollbackTransitionWrite(t, second)
		t.Fatal(err)
	}
	requireSharedClaimPGError(t, receiveSharedClaimRace(t, cancelRace, result), "23505", "shared_claim_scopes_allocation_key", "")
	rollbackTransitionWrite(t, second)
}

func TestRuntimeSharedClaimSchemaSameScopeCapacityUpdateSerializes(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	replica2 := "00000000-0000-0000-0000-000000000003"
	replica3 := "00000000-0000-0000-0000-000000000004"
	parent1, child1 := singleClaimSQL(claimVectorID, claimVectorReplica, "mail.send", "global", `decode(repeat('44',32),'hex')`, "running", 1, nil)
	setup := append(replicaInsert(claimVectorReplica, "i-00000000000000002", "2"), replicaInsert(replica2, "i-00000000000000003", "3")...)
	setup = append(setup, replicaInsert(replica3, "i-00000000000000004", "4")...)
	setup = append(setup, `INSERT INTO public.shared_claim_scope_summaries(policy_id,scope_kind,scope_digest,running_count,waiting_count,next_ordinal,updated_at) VALUES('mail.send','global',decode(repeat('44',32),'hex'),1,0,2,transaction_timestamp())`, parent1, child1)
	if err := membershipWrite(t, db, setup...); err != nil {
		t.Fatal(err)
	}
	parent2, child2 := singleClaimSQL("00000000-0000-0000-0000-000000000005", replica2, "mail.send", "global", `decode(repeat('44',32),'hex')`, "running", 2, nil)
	parent3, child3 := singleClaimSQL("00000000-0000-0000-0000-000000000006", replica3, "mail.send", "global", `decode(repeat('44',32),'hex')`, "running", 3, nil)
	_, first := beginSharedClaimWrite(t, db)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, statement := range []string{parent2, child2, `UPDATE public.shared_claim_scope_summaries SET running_count=running_count+1,next_ordinal=next_ordinal+1,updated_at=transaction_timestamp() WHERE scope_digest=decode(repeat('44',32),'hex')`} {
		if _, err := first.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	_, second := beginSharedClaimWrite(t, db)
	for _, statement := range []string{parent3, child3} {
		if _, err := second.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	var secondPID int
	if err := second.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&secondPID); err != nil {
		t.Fatal(err)
	}
	raceCtx, cancelRace := context.WithTimeout(context.Background(), 5*time.Second)
	result := make(chan error, 1)
	go func() {
		if _, err := second.ExecContext(raceCtx, `UPDATE public.shared_claim_scope_summaries SET running_count=running_count+1,next_ordinal=next_ordinal+1,updated_at=transaction_timestamp() WHERE scope_digest=decode(repeat('44',32),'hex')`); err != nil {
			result <- err
			return
		}
		result <- finishSharedClaimWrite(raceCtx, second)
	}()
	if err := waitForTransitionCondition(db, `SELECT cardinality(pg_blocking_pids($1))>0 OR EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')`, secondPID); err != nil {
		rollbackTransitionWrite(t, first)
		cancelRace()
		if raceErr := receiveSharedClaimRace(t, func() {}, result); raceErr != nil {
			t.Errorf("capacity race cleanup: %v", raceErr)
		}
		rollbackTransitionWrite(t, second)
		t.Fatal(err)
	}
	if err := finishTransitionWrite(first); err != nil {
		cancelRace()
		if raceErr := receiveSharedClaimRace(t, func() {}, result); raceErr != nil {
			t.Errorf("capacity race cleanup: %v", raceErr)
		}
		rollbackTransitionWrite(t, second)
		t.Fatal(err)
	}
	requireSharedClaimPGError(t, receiveSharedClaimRace(t, cancelRace, result), "23514", "shared_claim_summary_exact_counts", "")
	var running int
	finalCtx, cancelFinal := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFinal()
	if err := db.QueryRowContext(finalCtx, `SELECT running_count FROM public.shared_claim_scope_summaries WHERE scope_digest=decode(repeat('44',32),'hex')`).Scan(&running); err != nil || running != 2 {
		t.Fatalf("final running=%d error=%v", running, err)
	}
}
