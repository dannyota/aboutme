package migrations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
	"time"
)

var claimPublicSignatures = map[string]string{
	"runtime_acquire_single_claim":       "(uuid,text,uuid,uuid,text,bytea)",
	"runtime_acquire_sse_claim":          "(uuid,uuid,bytea,bytea)",
	"runtime_promote_claim":              "(uuid,uuid,bytea)",
	"runtime_release_claim":              "(uuid,uuid,bytea,text)",
	"runtime_resolve_claim":              "(uuid,uuid,bytea)",
	"runtime_gc_released_claim_receipts": "()",
}

var claimHelperNames = []string{"runtime_claim_input_digest", "runtime_claim_validate_role", "runtime_claim_validate_input", "runtime_claim_load", "runtime_claim_result_for", "runtime_claim_absent", "runtime_claim_assert_request", "runtime_claim_acquire"}

func TestRuntimeSharedClaimOperationsFunctionsExist(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	var resultType bool
	if err := db.QueryRowContext(ctx, `SELECT to_regtype('public.runtime_claim_result') IS NOT NULL`).Scan(&resultType); err != nil {
		t.Fatal(err)
	}
	if !resultType {
		t.Error("type runtime_claim_result is missing")
	}
	for name, signature := range claimPublicSignatures {
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT to_regprocedure('public.' || $1) IS NOT NULL`, name+signature).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("function %s%s is missing", name, signature)
		}
	}
}

func TestRuntimeSharedClaimOperationsCatalogGrantsAndResultType(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	var attrs string
	if err := db.QueryRowContext(ctx, `SELECT string_agg(a.attname||':'||format_type(a.atttypid,a.atttypmod),',' ORDER BY a.attnum) FROM pg_type t JOIN pg_class c ON c.oid=t.typrelid JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum>0 WHERE t.oid='public.runtime_claim_result'::regtype`).Scan(&attrs); err != nil {
		t.Fatal(err)
	}
	want := "outcome:text,claim_id:uuid,policy_id:text,replica_id:uuid,work_id:uuid,state:text,admitted_at:timestamp with time zone,deadline_at:timestamp with time zone,released_at:timestamp with time zone,release_reason:text,request_digest:bytea,scope_count:smallint,scope_1_kind:text,scope_1_digest:bytea,scope_1_allocation_ordinal:bigint,scope_2_kind:text,scope_2_digest:bytea,scope_2_allocation_ordinal:bigint"
	if attrs != want {
		t.Fatalf("attributes=%s", attrs)
	}
	names := append([]string(nil), claimHelperNames...)
	for name := range claimPublicSignatures {
		names = append(names, name)
	}
	var valid bool
	if err := db.QueryRowContext(ctx, `SELECT count(*)=$2 AND bool_and(pg_get_userbyid(proowner)='aboutme_runtime_owner' AND prosecdef AND proconfig=ARRAY['search_path=pg_catalog']::text[] AND NOT has_function_privilege('public',oid,'EXECUTE')) FROM pg_proc WHERE pronamespace='public'::regnamespace AND proname=ANY($1)`, names, len(names)).Scan(&valid); err != nil || !valid {
		t.Fatalf("catalog valid=%t error=%v", valid, err)
	}
	var typeUsage bool
	if err := db.QueryRowContext(ctx, `SELECT NOT has_type_privilege('public','public.runtime_claim_result','USAGE') AND has_type_privilege('aboutme_app','public.runtime_claim_result','USAGE') AND has_type_privilege('aboutme_maintenance','public.runtime_claim_result','USAGE') AND pg_get_userbyid((SELECT typowner FROM pg_type WHERE oid='public.runtime_claim_result'::regtype))='aboutme_runtime_owner'`).Scan(&typeUsage); err != nil || !typeUsage {
		t.Fatalf("type privileges valid=%t error=%v", typeUsage, err)
	}
	grants := map[string]map[string]bool{
		"aboutme_app":               {"runtime_acquire_single_claim": true, "runtime_acquire_sse_claim": true, "runtime_promote_claim": true, "runtime_release_claim": true, "runtime_resolve_claim": true},
		"aboutme_maintenance":       {"runtime_acquire_single_claim": true, "runtime_release_claim": true, "runtime_resolve_claim": true, "runtime_gc_released_claim_receipts": true},
		"aboutme_lifecycle_command": {}, "aboutme_fencing_proof": {}, "aboutme_restore_verify": {}, "aboutme_migrator": {},
	}
	for role, allowed := range grants {
		for name, signature := range claimPublicSignatures {
			var execute bool
			if err := db.QueryRowContext(ctx, `SELECT has_function_privilege($1,'public.'||$2,'EXECUTE')`, role, name+signature).Scan(&execute); err != nil || execute != allowed[name] {
				t.Fatalf("%s %s execute=%t want=%t error=%v", role, name, execute, allowed[name], err)
			}
		}
		for _, name := range claimHelperNames {
			var execute bool
			if err := db.QueryRowContext(ctx, `SELECT bool_or(has_function_privilege($1,oid,'EXECUTE')) FROM pg_proc WHERE pronamespace='public'::regnamespace AND proname=$2`, role, name).Scan(&execute); err != nil || execute {
				t.Fatalf("%s helper %s execute=%t error=%v", role, name, execute, err)
			}
		}
	}
	claims, summaries, err := callClaimGC(t, db)
	if err != nil || claims != 0 || summaries != 0 {
		t.Fatalf("gc=%d/%d error=%v", claims, summaries, err)
	}
}

func goClaimRequestDigest(t *testing.T, claimID, policy, replicaID string, workID *string, scopes [][2]string) string {
	t.Helper()
	uuidBytes := func(value string) []byte {
		decoded, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
		if err != nil || len(decoded) != 16 {
			t.Fatalf("uuid %q: %v", value, err)
		}
		return decoded
	}
	frame := append([]byte("aboutme.shared-claim.request.v1"), 0, 1)
	frame = append(frame, uuidBytes(claimID)...)
	frame = append(frame, byte(len(policy)))
	frame = append(frame, policy...)
	frame = append(frame, uuidBytes(replicaID)...)
	if workID == nil {
		frame = append(frame, 0)
	} else {
		frame = append(frame, 1)
		frame = append(frame, uuidBytes(*workID)...)
	}
	frame = append(frame, byte(len(scopes)))
	for index, scope := range scopes {
		digest, err := hex.DecodeString(scope[1])
		if err != nil || len(digest) != 32 {
			t.Fatalf("scope digest %q: %v", scope[1], err)
		}
		frame = append(frame, byte(index+1), byte(len(scope[0])))
		frame = append(frame, scope[0]...)
		frame = append(frame, digest...)
	}
	sum := sha256.Sum256(frame)
	return hex.EncodeToString(sum[:])
}

func requireStoredClaimShape(t *testing.T, r claimOperationResult, policy, replica, state string, scopeCount int) {
	t.Helper()
	if r.outcome != state || !r.policy.Valid || r.policy.String != policy || !r.replica.Valid || r.replica.String != replica || !r.state.Valid || r.state.String != state || !r.admitted.Valid || len(r.digest) != 32 || !r.scopeCount.Valid || int(r.scopeCount.Int16) != scopeCount || !r.scope1Kind.Valid || len(r.scope1Digest) != 32 || !r.allocation1.Valid || r.allocation1.Int64 <= 0 {
		t.Fatalf("stored shape %+v", r)
	}
	if scopeCount == 2 && (!r.scope2Kind.Valid || len(r.scope2Digest) != 32 || !r.allocation2.Valid || r.allocation2.Int64 <= 0) {
		t.Fatalf("two-scope shape %+v", r)
	}
	if scopeCount == 1 && (r.scope2Kind.Valid || r.scope2Digest != nil || r.allocation2.Valid) {
		t.Fatalf("single-scope shape %+v", r)
	}
	if policy == "render.global_claim" {
		if !r.work.Valid || !r.deadline.Valid || r.deadline.Time.Sub(r.admitted.Time) != 20*time.Second {
			t.Fatalf("render shape %+v", r)
		}
	} else if r.work.Valid || r.deadline.Valid {
		t.Fatalf("non-render shape %+v", r)
	}
	if state == "released" {
		if !r.released.Valid || !r.releaseReason.Valid {
			t.Fatalf("released shape %+v", r)
		}
	} else if r.released.Valid || r.releaseReason.Valid {
		t.Fatalf("live shape %+v", r)
	}
}

func requireDeniedClaimShape(t *testing.T, r claimOperationResult, policy, replica string, scopeCount int, work bool) {
	t.Helper()
	if r.outcome != "denied" || !r.policy.Valid || r.policy.String != policy || !r.replica.Valid || r.replica.String != replica || r.work.Valid != work || r.state.Valid || r.admitted.Valid || r.deadline.Valid || r.released.Valid || r.releaseReason.Valid || len(r.digest) != 32 || !r.scopeCount.Valid || int(r.scopeCount.Int16) != scopeCount || !r.scope1Kind.Valid || len(r.scope1Digest) != 32 || r.allocation1.Valid || r.allocation2.Valid {
		t.Fatalf("denied shape %+v", r)
	}
	if (scopeCount == 2) != (r.scope2Kind.Valid && len(r.scope2Digest) == 32) {
		t.Fatalf("denied second scope %+v", r)
	}
}

func requireAbsentClaimShape(t *testing.T, r claimOperationResult, claimID string) {
	t.Helper()
	if r.outcome != "absent" || r.claimID != claimID || r.policy.Valid || r.replica.Valid || r.work.Valid || r.state.Valid || r.admitted.Valid || r.deadline.Valid || r.released.Valid || r.releaseReason.Valid || r.digest != nil || r.scopeCount.Valid || r.scope1Kind.Valid || r.scope1Digest != nil || r.allocation1.Valid || r.scope2Kind.Valid || r.scope2Digest != nil || r.allocation2.Valid {
		t.Fatalf("absent shape %+v", r)
	}
}

func TestRuntimeSharedClaimOperationsLifecycleAndResultMatrix(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	id := validRegistrationIdentity("a")
	activeClaimReplica(t, db, id, "serving")
	replica := id.replicaID
	claim := claimUUID("aaaaaaaa", 1)
	scope := claimHex(1)
	acquired := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(claim, "mail.send", replica, "NULL", "global", scope), "running")
	requireStoredClaimShape(t, acquired, "mail.send", replica, "running", 1)
	if acquired.allocation1.Int64 != 1 || acquired.scope1Kind.String != "global" || hex.EncodeToString(acquired.scope1Digest) != scope {
		t.Fatalf("acquire=%+v", acquired)
	}
	digest := hex.EncodeToString(acquired.digest)
	if want := goClaimRequestDigest(t, claim, "mail.send", replica, nil, [][2]string{{"global", scope}}); digest != want {
		t.Fatalf("sql digest=%s go digest=%s", digest, want)
	}
	requireClaimSummary(t, db, "mail.send", "global", scope, 1, 0)
	resolved := requireClaimOutcome(t, db, "aboutme_app", claimResolveExpression(claim, replica, digest), "running")
	if !claimResultsEqual(resolved, acquired) {
		t.Fatalf("resolve=%+v acquire=%+v", resolved, acquired)
	}
	replay := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(claim, "mail.send", replica, "NULL", "global", scope), "running")
	if !claimResultsEqual(replay, acquired) {
		t.Fatalf("replay=%+v acquire=%+v", replay, acquired)
	}
	requireClaimSummary(t, db, "mail.send", "global", scope, 1, 0)
	requireClaimError(t, db, "aboutme_app", claimResolveExpression(claim, replica, strings.Repeat("ff", 32)), "AM002")
	requireClaimError(t, db, "aboutme_app", claimResolveExpression(claim, claimUUID("aaaaaaaa", 99), digest), "AM002")
	requireClaimError(t, db, "aboutme_app", claimSingleExpression(claim, "password.hash", replica, "NULL", "global", scope), "AM002")
	requireClaimError(t, db, "aboutme_app", claimSingleExpression(claim, "mail.send", replica, "NULL", "global", claimHex(2)), "AM002")
	requireClaimError(t, db, "aboutme_app", claimReleaseExpression(claim, replica, digest, "fenced"), "42501")
	released := requireClaimOutcome(t, db, "aboutme_app", claimReleaseExpression(claim, replica, digest, "joined"), "released")
	requireStoredClaimShape(t, released, "mail.send", replica, "released", 1)
	if released.releaseReason.String != "joined" || released.allocation1.Int64 != 1 || !released.admitted.Time.Equal(acquired.admitted.Time) || released.released.Time.Before(acquired.admitted.Time) {
		t.Fatalf("released=%+v", released)
	}
	requireClaimSummary(t, db, "mail.send", "global", scope, 0, 0)
	for _, expression := range []string{
		claimReleaseExpression(claim, replica, digest, "joined"),
		claimSingleExpression(claim, "mail.send", replica, "NULL", "global", scope),
		claimResolveExpression(claim, replica, digest),
	} {
		again := requireClaimOutcome(t, db, "aboutme_app", expression, "released")
		if !claimResultsEqual(again, released) {
			t.Fatalf("terminal replay %s=%+v want %+v", expression, again, released)
		}
	}
	requireClaimSummary(t, db, "mail.send", "global", scope, 0, 0)
	requireClaimError(t, db, "aboutme_app", claimReleaseExpression(claim, replica, digest, "canceled"), "AM002")
	requireClaimError(t, db, "aboutme_app", claimPromoteExpression(claim, replica, digest), "55000")

	render := claimUUID("aaaaaaaa", 2)
	work := claimUUID("aaaaaaaa", 3)
	renderResult := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(render, "render.global_claim", replica, "'"+work+"'", "global", scope), "running")
	requireStoredClaimShape(t, renderResult, "render.global_claim", replica, "running", 1)
	if renderResult.work.String != work {
		t.Fatalf("render work=%+v", renderResult)
	}
	if want := goClaimRequestDigest(t, render, "render.global_claim", replica, &work, [][2]string{{"global", scope}}); hex.EncodeToString(renderResult.digest) != want {
		t.Fatalf("render digest mismatch %s", want)
	}
	requireClaimError(t, db, "aboutme_app", claimSingleExpression(render, "render.global_claim", replica, "'"+claimUUID("aaaaaaaa", 4)+"'", "global", scope), "AM002")

	vectorReplica := claimReplicaIdentity(claimVectorReplica, "2")
	activeClaimReplica(t, db, vectorReplica, "serving")
	account := strings.Repeat("ff", 32)
	vector := requireClaimOutcome(t, db, "aboutme_app", claimSSEExpression(claimVectorID, claimVectorReplica, strings.Repeat("00", 32), &account), "running")
	requireStoredClaimShape(t, vector, "sse.fleet_account_ip", claimVectorReplica, "running", 2)
	if hex.EncodeToString(vector.digest) != claimVectorDigest || vector.scope1Kind.String != "ip" || vector.scope2Kind.String != "account" || hex.EncodeToString(vector.scope2Digest) != account || vector.allocation1.Int64 != 1 || vector.allocation2.Int64 != 1 {
		t.Fatalf("vector=%+v", vector)
	}
	requireClaimSummary(t, db, "sse.fleet_account_ip", "ip", strings.Repeat("00", 32), 1, 0)
	requireClaimSummary(t, db, "sse.fleet_account_ip", "account", account, 1, 0)
	requireClaimOutcome(t, db, "aboutme_app", claimReleaseExpression(claimVectorID, claimVectorReplica, claimVectorDigest, "canceled"), "released")
	requireClaimSummary(t, db, "sse.fleet_account_ip", "ip", strings.Repeat("00", 32), 0, 0)
	requireClaimSummary(t, db, "sse.fleet_account_ip", "account", account, 0, 0)
	public := requireClaimOutcome(t, db, "aboutme_app", claimSSEExpression(claimUUID("aaaaaaaa", 5), replica, claimHex(7), nil), "running")
	requireStoredClaimShape(t, public, "sse.fleet_account_ip", replica, "running", 1)

	unknown := claimUUID("aaaaaaaa", 6)
	requireAbsentClaimShape(t, requireClaimOutcome(t, db, "aboutme_app", claimResolveExpression(unknown, replica, digest), "absent"), unknown)
	requireAbsentClaimShape(t, requireClaimOutcome(t, db, "aboutme_app", claimPromoteExpression(unknown, replica, digest), "absent"), unknown)
	requireAbsentClaimShape(t, requireClaimOutcome(t, db, "aboutme_app", claimReleaseExpression(unknown, replica, digest, "joined"), "absent"), unknown)
	var storedOutcomeEqualsState bool
	if err := db.QueryRowContext(ctx, `SELECT bool_and(r.state IN ('running','waiting','released')) FROM public.shared_claim_requests r`).Scan(&storedOutcomeEqualsState); err != nil || !storedOutcomeEqualsState {
		t.Fatalf("stored states valid=%t error=%v", storedOutcomeEqualsState, err)
	}
}

func TestRuntimeSharedClaimOperationsCapacityMatrix(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	id := validRegistrationIdentity("b")
	activeClaimReplica(t, db, id, "serving")
	replica := id.replicaID
	for index, boundary := range claimPolicyBoundaries {
		t.Run(boundary.name, func(t *testing.T) {
			prefix := fmt.Sprintf("b%07d", index)
			digest := strings.Repeat(boundary.digestByte, 32)
			expression := func(i int) string {
				claim := claimUUID(prefix, i)
				switch boundary.kind {
				case "account":
					return claimSSEExpression(claim, replica, claimHex(100000+index*1000+i), &digest)
				case "ip":
					return claimSSEExpression(claim, replica, digest, nil)
				}
				work := "NULL"
				if boundary.policy == "render.global_claim" {
					work = "'" + claimUUID("bbbbbbbb", i) + "'"
				}
				return claimSingleExpression(claim, boundary.policy, replica, work, boundary.kind, digest)
			}
			before := countClaimTables(t, db)
			for i := 0; i < boundary.running; i++ {
				r := requireClaimOutcome(t, db, "aboutme_app", expression(i), "running")
				ordinal := r.allocation1.Int64
				if boundary.kind == "account" {
					ordinal = r.allocation2.Int64
				}
				if ordinal != int64(i+1) {
					t.Fatalf("running %d ordinal=%d result=%+v", i, ordinal, r)
				}
			}
			for i := 0; i < boundary.waiting; i++ {
				r := requireClaimOutcome(t, db, "aboutme_app", expression(boundary.running+i), "waiting")
				requireStoredClaimShape(t, r, boundary.policy, replica, "waiting", 1)
			}
			denied := requireClaimOutcome(t, db, "aboutme_app", expression(boundary.running+boundary.waiting), "denied")
			scopeCount := 1
			if boundary.kind == "account" {
				scopeCount = 2
			}
			requireDeniedClaimShape(t, denied, boundary.policy, replica, scopeCount, boundary.policy == "render.global_claim")
			requireClaimSummary(t, db, boundary.policy, boundary.kind, digest, boundary.running, boundary.waiting)
			after := countClaimTables(t, db)
			if after.parents != before.parents+boundary.running+boundary.waiting || after.scopes != before.scopes+(boundary.running+boundary.waiting)*scopeCount {
				t.Fatalf("rows before=%+v after=%+v", before, after)
			}
			if boundary.kind == "account" {
				if deniedIP := claimSummary(t, db, "sse.fleet_account_ip", "ip", claimHex(100000+index*1000+boundary.running)); deniedIP.exists {
					t.Fatalf("denied C05 request created a partial IP summary %+v", deniedIP)
				}
			}
			deniedReplay := requireClaimOutcome(t, db, "aboutme_app", expression(boundary.running+boundary.waiting), "denied")
			if !claimResultsEqual(deniedReplay, denied) {
				t.Fatalf("denied replay=%+v first=%+v", deniedReplay, denied)
			}
		})
	}
	t.Run("denied_leaves_no_summary", func(t *testing.T) {
		before := countClaimTables(t, db)
		denied := requireClaimOutcome(t, db, "aboutme_app", claimSSEExpression(claimUUID("bbbbbbb1", 1), replica, claimHex(200001), func() *string { s := strings.Repeat("60", 32); return &s }()), "denied")
		requireDeniedClaimShape(t, denied, "sse.fleet_account_ip", replica, 2, false)
		if after := countClaimTables(t, db); after != before {
			t.Fatalf("denied acquire changed rows before=%+v after=%+v", before, after)
		}
	})
	t.Run("ordinal_exhaustion", func(t *testing.T) {
		scope := claimHex(300000)
		first := claimUUID("bbbbbbb2", 1)
		acquired := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(first, "mail.send", replica, "NULL", "global", scope), "running")
		requireClaimOutcome(t, db, "aboutme_app", claimReleaseExpression(first, replica, hex.EncodeToString(acquired.digest), "joined"), "released")
		if err := membershipWrite(t, db, fmt.Sprintf(`UPDATE public.shared_claim_scope_summaries SET next_ordinal=9223372036854775807 WHERE policy_id='mail.send' AND scope_digest=decode('%s','hex')`, scope)); err != nil {
			t.Fatal(err)
		}
		before := countClaimTables(t, db)
		requireClaimError(t, db, "aboutme_app", claimSingleExpression(claimUUID("bbbbbbb2", 2), "mail.send", replica, "NULL", "global", scope), "55000")
		if after := countClaimTables(t, db); after != before {
			t.Fatalf("exhaustion changed rows before=%+v after=%+v", before, after)
		}
		if summary := claimSummary(t, db, "mail.send", "global", scope); summary.nextOrdinal != 9223372036854775807 || summary.running != 0 {
			t.Fatalf("summary=%+v", summary)
		}
	})
}

func TestRuntimeSharedClaimOperationsQueueOrderPromotionAndExpiry(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	id := validRegistrationIdentity("c")
	activeClaimReplica(t, db, id, "serving")
	replica := id.replicaID
	scope := claimHex(2)
	render := func(i int) string {
		return claimSingleExpression(claimUUID("cccccccc", i), "render.global_claim", replica, "'"+claimUUID("cccccccd", i)+"'", "global", scope)
	}
	a := requireClaimOutcome(t, db, "aboutme_app", render(1), "running")
	b := requireClaimOutcome(t, db, "aboutme_app", render(2), "waiting")
	c := requireClaimOutcome(t, db, "aboutme_app", render(3), "waiting")
	if a.allocation1.Int64 != 1 || b.allocation1.Int64 != 2 || c.allocation1.Int64 != 3 {
		t.Fatalf("ordinals a=%d b=%d c=%d", a.allocation1.Int64, b.allocation1.Int64, c.allocation1.Int64)
	}
	da, dbb, dc := hex.EncodeToString(a.digest), hex.EncodeToString(b.digest), hex.EncodeToString(c.digest)
	promote := func(claim claimOperationResult, digest string) claimOperationResult {
		return mustClaim(t, db, "aboutme_app", claimPromoteExpression(claim.claimID, replica, digest))
	}
	if blocked := promote(c, dc); !claimResultsEqual(blocked, c) {
		t.Fatalf("blocked promotion changed row %+v want %+v", blocked, c)
	}
	if blocked := promote(b, dbb); !claimResultsEqual(blocked, b) {
		t.Fatalf("blocked promotion changed row %+v want %+v", blocked, b)
	}
	requireClaimError(t, db, "aboutme_app", claimPromoteExpression(a.claimID, replica, da), "55000")
	requireClaimSummary(t, db, "render.global_claim", "global", scope, 1, 2)
	requireClaimOutcome(t, db, "aboutme_app", claimReleaseExpression(a.claimID, replica, da, "joined"), "released")
	requireClaimSummary(t, db, "render.global_claim", "global", scope, 0, 2)
	if outOfOrder := promote(c, dc); !claimResultsEqual(outOfOrder, c) {
		t.Fatalf("queue order violated %+v", outOfOrder)
	}
	promoted := promote(b, dbb)
	requireStoredClaimShape(t, promoted, "render.global_claim", replica, "running", 1)
	if !promoted.admitted.Time.Equal(b.admitted.Time) || !promoted.deadline.Time.Equal(b.deadline.Time) || promoted.allocation1.Int64 != 2 {
		t.Fatalf("promotion changed original deadline %+v want %+v", promoted, b)
	}
	requireClaimSummary(t, db, "render.global_claim", "global", scope, 1, 1)
	if still := promote(c, dc); !claimResultsEqual(still, c) {
		t.Fatalf("promotion without capacity changed row %+v", still)
	}
	requireClaimOutcome(t, db, "aboutme_app", claimReleaseExpression(b.claimID, replica, dbb, "joined"), "released")
	if last := promote(c, dc); last.outcome != "running" || last.allocation1.Int64 != 3 {
		t.Fatalf("last promotion=%+v", last)
	}
	requireClaimSummary(t, db, "render.global_claim", "global", scope, 1, 0)
	requireClaimError(t, db, "aboutme_app", claimPromoteExpression(b.claimID, replica, dbb), "55000")

	mail := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(claimUUID("cccccccc", 10), "mail.send", replica, "NULL", "global", scope), "running")
	requireClaimError(t, db, "aboutme_app", claimPromoteExpression(mail.claimID, replica, hex.EncodeToString(mail.digest)), "55000")

	expiredScope := claimHex(3)
	d := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(claimUUID("cccccccc", 20), "render.global_claim", replica, "'"+claimUUID("cccccccd", 20)+"'", "global", expiredScope), "running")
	e := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(claimUUID("cccccccc", 21), "render.global_claim", replica, "'"+claimUUID("cccccccd", 21)+"'", "global", expiredScope), "waiting")
	backdateClaim(t, db, e.claimID, 30)
	expired := promote(e, hex.EncodeToString(e.digest))
	requireStoredClaimShape(t, expired, "render.global_claim", replica, "released", 1)
	if expired.releaseReason.String != "expired" || !expired.deadline.Time.Equal(e.deadline.Time.Add(-30*time.Second)) {
		t.Fatalf("expired=%+v", expired)
	}
	requireClaimSummary(t, db, "render.global_claim", "global", expiredScope, 1, 0)
	requireClaimOutcome(t, db, "aboutme_app", claimResolveExpression(e.claimID, replica, hex.EncodeToString(e.digest)), "released")
	backdateClaim(t, db, d.claimID, 30)
	requireClaimError(t, db, "aboutme_app", claimPromoteExpression(d.claimID, replica, hex.EncodeToString(d.digest)), "55000")
	stillRunning := requireClaimOutcome(t, db, "aboutme_app", claimResolveExpression(d.claimID, replica, hex.EncodeToString(d.digest)), "running")
	replayed := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(d.claimID, "render.global_claim", replica, "'"+claimUUID("cccccccd", 20)+"'", "global", expiredScope), "running")
	if !claimResultsEqual(replayed, stillRunning) || !replayed.deadline.Time.Equal(d.deadline.Time.Add(-30*time.Second)) {
		t.Fatalf("replay=%+v resolve=%+v", replayed, stillRunning)
	}
	requireClaimSummary(t, db, "render.global_claim", "global", expiredScope, 1, 0)
	callerExpired := requireClaimOutcome(t, db, "aboutme_app", claimReleaseExpression(d.claimID, replica, hex.EncodeToString(d.digest), "expired"), "released")
	if callerExpired.releaseReason.String != "expired" {
		t.Fatalf("caller expiry=%+v", callerExpired)
	}
	requireClaimSummary(t, db, "render.global_claim", "global", expiredScope, 0, 0)

	passwordScope := claimHex(4)
	p1 := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(claimUUID("cccccccc", 30), "password.hash", replica, "NULL", "global", passwordScope), "running")
	requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(claimUUID("cccccccc", 31), "password.hash", replica, "NULL", "global", passwordScope), "running")
	p3 := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(claimUUID("cccccccc", 32), "password.hash", replica, "NULL", "global", passwordScope), "waiting")
	if waiting := promote(p3, hex.EncodeToString(p3.digest)); !claimResultsEqual(waiting, p3) {
		t.Fatalf("password promotion without capacity %+v", waiting)
	}
	requireClaimOutcome(t, db, "aboutme_app", claimReleaseExpression(p1.claimID, replica, hex.EncodeToString(p1.digest), "joined"), "released")
	if running := promote(p3, hex.EncodeToString(p3.digest)); running.outcome != "running" || running.deadline.Valid {
		t.Fatalf("password promotion=%+v", running)
	}
	requireClaimSummary(t, db, "password.hash", "global", passwordScope, 2, 0)
}

func TestRuntimeSharedClaimOperationsRoleMatrix(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	serving := validRegistrationIdentity("d")
	maintenance := validRegistrationIdentity("e")
	activeClaimReplica(t, db, serving, "serving")
	activeClaimReplica(t, db, maintenance, "maintenance")
	scope := claimHex(5)
	app := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(claimUUID("dddddddd", 1), "mail.send", serving.replicaID, "NULL", "global", scope), "running")
	appDigest := hex.EncodeToString(app.digest)
	maint := requireClaimOutcome(t, db, "aboutme_maintenance", claimSingleExpression(claimUUID("eeeeeeee", 1), "mail.send", maintenance.replicaID, "NULL", "global", scope), "running")
	maintDigest := hex.EncodeToString(maint.digest)
	requireClaimSummary(t, db, "mail.send", "global", scope, 2, 0)
	for _, test := range []struct{ name, role, expression string }{
		{"maintenance_sse_acl", "aboutme_maintenance", claimSSEExpression(claimUUID("eeeeeeee", 2), maintenance.replicaID, scope, nil)},
		{"maintenance_promote_acl", "aboutme_maintenance", claimPromoteExpression(maint.claimID, maintenance.replicaID, maintDigest)},
		{"maintenance_password", "aboutme_maintenance", claimSingleExpression(claimUUID("eeeeeeee", 3), "password.hash", maintenance.replicaID, "NULL", "global", scope)},
		{"maintenance_render", "aboutme_maintenance", claimSingleExpression(claimUUID("eeeeeeee", 4), "render.global_claim", maintenance.replicaID, "'"+claimUUID("eeeeeeee", 5)+"'", "global", scope)},
		{"maintenance_serving_replica", "aboutme_maintenance", claimSingleExpression(claimUUID("eeeeeeee", 6), "mail.send", serving.replicaID, "NULL", "global", scope)},
		{"app_maintenance_replica", "aboutme_app", claimSingleExpression(claimUUID("dddddddd", 2), "mail.send", maintenance.replicaID, "NULL", "global", scope)},
		{"app_resolves_maintenance_claim", "aboutme_app", claimResolveExpression(maint.claimID, maintenance.replicaID, maintDigest)},
		{"app_releases_maintenance_claim", "aboutme_app", claimReleaseExpression(maint.claimID, maintenance.replicaID, maintDigest, "joined")},
		{"app_promotes_maintenance_claim", "aboutme_app", claimPromoteExpression(maint.claimID, maintenance.replicaID, maintDigest)},
		{"app_replays_maintenance_claim", "aboutme_app", claimSingleExpression(maint.claimID, "mail.send", maintenance.replicaID, "NULL", "global", scope)},
		{"maintenance_resolves_app_claim", "aboutme_maintenance", claimResolveExpression(app.claimID, serving.replicaID, appDigest)},
		{"maintenance_releases_app_claim", "aboutme_maintenance", claimReleaseExpression(app.claimID, serving.replicaID, appDigest, "joined")},
		{"maintenance_replays_app_claim", "aboutme_maintenance", claimSingleExpression(app.claimID, "mail.send", serving.replicaID, "NULL", "global", scope)},
		{"app_fenced", "aboutme_app", claimReleaseExpression(app.claimID, serving.replicaID, appDigest, "fenced")},
		{"maintenance_fenced", "aboutme_maintenance", claimReleaseExpression(maint.claimID, maintenance.replicaID, maintDigest, "fenced")},
	} {
		t.Run(test.name, func(t *testing.T) {
			requireClaimError(t, db, test.role, test.expression, "42501")
		})
	}
	direct := claimSingleExpression(claimUUID("dddddddd", 3), "mail.send", serving.replicaID, "NULL", "global", scope)
	for _, test := range []struct{ name, role, statement string }{
		{"lifecycle_command", "aboutme_lifecycle_command", `SELECT ` + direct},
		{"fencing_proof", "aboutme_fencing_proof", `SELECT ` + direct},
		{"restore_verify", "aboutme_restore_verify", `SELECT ` + direct},
		{"migrator", "aboutme_migrator", `SELECT ` + direct},
		{"app_gc", "aboutme_app", `SELECT * FROM public.runtime_gc_released_claim_receipts()`},
		{"lifecycle_gc", "aboutme_lifecycle_command", `SELECT * FROM public.runtime_gc_released_claim_receipts()`},
		{"app_helper_acquire", "aboutme_app", fmt.Sprintf(`SELECT public.runtime_claim_acquire('%s','mail.send','%s',NULL,1::smallint,'global',decode('%s','hex'),NULL,NULL)`, claimUUID("dddddddd", 4), serving.replicaID, scope)},
		{"app_helper_load", "aboutme_app", fmt.Sprintf(`SELECT public.runtime_claim_load('%s')`, app.claimID)},
		{"app_helper_digest", "aboutme_app", fmt.Sprintf(`SELECT public.runtime_claim_input_digest('%s','mail.send','%s',NULL,1::smallint,'global',decode('%s','hex'),NULL,NULL)`, app.claimID, serving.replicaID, scope)},
		{"maintenance_helper_assert", "aboutme_maintenance", fmt.Sprintf(`SELECT public.runtime_claim_assert_request('%s','%s',decode('%s','hex'))`, maint.claimID, maintenance.replicaID, maintDigest)},
		{"app_select_requests", "aboutme_app", `SELECT count(*) FROM public.shared_claim_requests`},
		{"app_select_scopes", "aboutme_app", `SELECT count(*) FROM public.shared_claim_scopes`},
		{"app_update_summaries", "aboutme_app", `UPDATE public.shared_claim_scope_summaries SET running_count=0`},
		{"maintenance_delete_requests", "aboutme_maintenance", `DELETE FROM public.shared_claim_requests`},
		{"maintenance_insert_policies", "aboutme_maintenance", `INSERT INTO public.shared_claim_policies VALUES('mail.send','user',1,0,false,'none')`},
	} {
		t.Run(test.name, func(t *testing.T) {
			requireRegistrationError(t, registrationRoleExec(t, db, test.role, test.statement), "42501")
		})
	}
	requireClaimOutcome(t, db, "aboutme_maintenance", claimResolveExpression(maint.claimID, maintenance.replicaID, maintDigest), "running")
	requireClaimOutcome(t, db, "aboutme_maintenance", claimSingleExpression(maint.claimID, "mail.send", maintenance.replicaID, "NULL", "global", scope), "running")
	requireClaimOutcome(t, db, "aboutme_maintenance", claimReleaseExpression(maint.claimID, maintenance.replicaID, maintDigest, "joined"), "released")
	requireClaimOutcome(t, db, "aboutme_app", claimReleaseExpression(app.claimID, serving.replicaID, appDigest, "canceled"), "released")
	requireClaimSummary(t, db, "mail.send", "global", scope, 0, 0)
}

func TestRuntimeSharedClaimOperationsInputCorruptionAndFixedErrors(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	id := validRegistrationIdentity("f")
	activeClaimReplica(t, db, id, "serving")
	replica := id.replicaID
	scope := claimHex(6)
	nilUUID := "00000000-0000-0000-0000-000000000000"
	valid := claimUUID("ffffffff", 1)
	work := claimUUID("ffffffff", 2)
	before := countClaimTables(t, db)
	for _, test := range []struct{ name, expression string }{
		{"nil_claim", claimSingleExpression(nilUUID, "mail.send", replica, "NULL", "global", scope)},
		{"nil_replica", claimSingleExpression(valid, "mail.send", nilUUID, "NULL", "global", scope)},
		{"null_claim", fmt.Sprintf(`public.runtime_acquire_single_claim(NULL,'mail.send','%s',NULL,'global',decode('%s','hex'))`, replica, scope)},
		{"null_replica", fmt.Sprintf(`public.runtime_acquire_single_claim('%s','mail.send',NULL,NULL,'global',decode('%s','hex'))`, valid, scope)},
		{"null_policy", fmt.Sprintf(`public.runtime_acquire_single_claim('%s',NULL,'%s',NULL,'global',decode('%s','hex'))`, valid, replica, scope)},
		{"unknown_policy", claimSingleExpression(valid, "bogus", replica, "NULL", "global", scope)},
		{"sse_policy_via_single", claimSingleExpression(valid, "sse.fleet_account_ip", replica, "NULL", "ip", scope)},
		{"wrong_kind", claimSingleExpression(valid, "mail.send", replica, "NULL", "user", scope)},
		{"null_kind", fmt.Sprintf(`public.runtime_acquire_single_claim('%s','mail.send','%s',NULL,NULL,decode('%s','hex'))`, valid, replica, scope)},
		{"unknown_kind", claimSingleExpression(valid, "mcp.user_concurrent", replica, "NULL", "tenant", scope)},
		{"work_without_render", claimSingleExpression(valid, "mail.send", replica, "'"+work+"'", "global", scope)},
		{"render_without_work", claimSingleExpression(valid, "render.global_claim", replica, "NULL", "global", scope)},
		{"render_nil_work", claimSingleExpression(valid, "render.global_claim", replica, "'"+nilUUID+"'", "global", scope)},
		{"short_digest", claimSingleExpression(valid, "mail.send", replica, "NULL", "global", scope[:62])},
		{"long_digest", claimSingleExpression(valid, "mail.send", replica, "NULL", "global", scope+"ff")},
		{"null_digest", fmt.Sprintf(`public.runtime_acquire_single_claim('%s','mail.send','%s',NULL,'global',NULL)`, valid, replica)},
		{"sse_nil_claim", claimSSEExpression(nilUUID, replica, scope, nil)},
		{"sse_null_ip", fmt.Sprintf(`public.runtime_acquire_sse_claim('%s','%s',NULL,NULL)`, valid, replica)},
		{"sse_short_ip", claimSSEExpression(valid, replica, scope[:62], nil)},
		{"sse_short_account", claimSSEExpression(valid, replica, scope, func() *string { s := scope[:62]; return &s }())},
		{"sse_empty_account", claimSSEExpression(valid, replica, scope, func() *string { s := ""; return &s }())},
		{"resolve_nil_claim", claimResolveExpression(nilUUID, replica, scope)},
		{"resolve_nil_replica", claimResolveExpression(valid, nilUUID, scope)},
		{"resolve_short_digest", claimResolveExpression(valid, replica, scope[:62])},
		{"resolve_null_digest", fmt.Sprintf(`public.runtime_resolve_claim('%s','%s',NULL)`, valid, replica)},
		{"promote_nil_claim", claimPromoteExpression(nilUUID, replica, scope)},
		{"promote_short_digest", claimPromoteExpression(valid, replica, scope[:62])},
		{"release_nil_replica", claimReleaseExpression(valid, nilUUID, scope, "joined")},
		{"release_short_digest", claimReleaseExpression(valid, replica, scope[:62], "joined")},
		{"release_bogus_reason", claimReleaseExpression(valid, replica, scope, "bogus")},
		{"release_empty_reason", claimReleaseExpression(valid, replica, scope, "")},
		{"release_null_reason", fmt.Sprintf(`public.runtime_release_claim('%s','%s',decode('%s','hex'),NULL)`, valid, replica, scope)},
	} {
		t.Run(test.name, func(t *testing.T) {
			pgErr := claimErrorOf(t, db, "aboutme_app", test.expression, "22023")
			requireNoClaimIdentityLeak(t, pgErr, valid, replica, scope[:40])
		})
	}
	if after := countClaimTables(t, db); after != before {
		t.Fatalf("invalid input changed rows before=%+v after=%+v", before, after)
	}
	unknownReplica := claimUUID("ffffffff", 77)
	requireClaimError(t, db, "aboutme_app", claimSingleExpression(valid, "mail.send", unknownReplica, "NULL", "global", scope), "55000")

	x := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(claimUUID("ffffffff", 10), "mail.send", replica, "NULL", "global", claimHex(10)), "running")
	xDigest := hex.EncodeToString(x.digest)
	conflict := claimErrorOf(t, db, "aboutme_app", claimResolveExpression(x.claimID, replica, strings.Repeat("ab", 32)), "AM002")
	requireNoClaimIdentityLeak(t, conflict, x.claimID, replica, xDigest[:40], strings.Repeat("ab", 20))
	forbidden := claimErrorOf(t, db, "aboutme_app", claimReleaseExpression(x.claimID, replica, xDigest, "fenced"), "42501")
	requireNoClaimIdentityLeak(t, forbidden, x.claimID, replica, xDigest[:40])
	corruptClaimRows(t, db, fmt.Sprintf(`UPDATE public.shared_claim_requests SET request_digest=decode('%s','hex') WHERE claim_id='%s'`, strings.Repeat("ab", 32), x.claimID))
	for _, test := range []struct{ name, expression string }{
		{"resolve_original", claimResolveExpression(x.claimID, replica, xDigest)},
		{"resolve_stored", claimResolveExpression(x.claimID, replica, strings.Repeat("ab", 32))},
		{"acquire_replay", claimSingleExpression(x.claimID, "mail.send", replica, "NULL", "global", claimHex(10))},
		{"release", claimReleaseExpression(x.claimID, replica, xDigest, "joined")},
		{"promote", claimPromoteExpression(x.claimID, replica, xDigest)},
	} {
		t.Run("corrupt_digest_"+test.name, func(t *testing.T) {
			corrupt := claimErrorOf(t, db, "aboutme_app", test.expression, "AM001")
			requireNoClaimIdentityLeak(t, corrupt, x.claimID, replica, xDigest[:40])
		})
	}
	y := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(claimUUID("ffffffff", 11), "mail.send", replica, "NULL", "global", claimHex(11)), "running")
	corruptClaimRows(t, db, fmt.Sprintf(`DELETE FROM public.shared_claim_scopes WHERE claim_id='%s'`, y.claimID))
	requireClaimError(t, db, "aboutme_app", claimResolveExpression(y.claimID, replica, hex.EncodeToString(y.digest)), "AM001")
	requireClaimError(t, db, "aboutme_app", claimSingleExpression(y.claimID, "mail.send", replica, "NULL", "global", claimHex(11)), "AM001")
	z := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(claimUUID("ffffffff", 12), "mail.send", replica, "NULL", "global", claimHex(12)), "running")
	corruptClaimRows(t, db, fmt.Sprintf(`UPDATE public.shared_claim_scopes SET state='released' WHERE claim_id='%s'`, z.claimID))
	requireClaimError(t, db, "aboutme_app", claimResolveExpression(z.claimID, replica, hex.EncodeToString(z.digest)), "AM001")
	requireClaimError(t, db, "aboutme_app", claimReleaseExpression(z.claimID, replica, hex.EncodeToString(z.digest), "joined"), "AM001")

	fresh := claimSingleExpression(claimUUID("ffffffff", 20), "mail.send", replica, "NULL", "global", claimHex(20))
	for _, test := range []struct{ name, role, statement string }{
		{"acquire", "aboutme_app", `SELECT ` + fresh},
		{"resolve", "aboutme_app", `SELECT ` + claimResolveExpression(x.claimID, replica, xDigest)},
		{"promote", "aboutme_app", `SELECT ` + claimPromoteExpression(x.claimID, replica, xDigest)},
		{"release", "aboutme_app", `SELECT ` + claimReleaseExpression(x.claimID, replica, xDigest, "joined")},
		{"gc", "aboutme_maintenance", `SELECT * FROM public.runtime_gc_released_claim_receipts()`},
	} {
		t.Run("entry_"+test.name, func(t *testing.T) {
			requireRegistrationError(t, registrationRoleExec(t, db, test.role, test.statement), "AM001")
		})
	}
}

func TestRuntimeSharedClaimOperationsDrainAndAdmission(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	draining := validRegistrationIdentity("a")
	joining := validRegistrationIdentity("b")
	activeClaimReplica(t, db, draining, "serving")
	if err := membershipWrite(t, db, replicaInsert(joining.replicaID, joining.instanceID, "b")...); err != nil {
		t.Fatal(err)
	}
	scope := claimHex(30)
	requireClaimError(t, db, "aboutme_app", claimSingleExpression(claimUUID("abababab", 1), "mail.send", joining.replicaID, "NULL", "global", scope), "55000")
	mail := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(claimUUID("abababab", 2), "mail.send", draining.replicaID, "NULL", "global", scope), "running")
	first := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(claimUUID("abababab", 3), "render.global_claim", draining.replicaID, "'"+claimUUID("abababab", 4)+"'", "global", scope), "running")
	waiting := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(claimUUID("abababab", 5), "render.global_claim", draining.replicaID, "'"+claimUUID("abababab", 6)+"'", "global", scope), "waiting")
	if err := membershipWrite(t, db, fmt.Sprintf(`UPDATE public.runtime_replicas SET state='draining',draining_at=clock_timestamp() WHERE replica_id='%s'`, draining.replicaID)); err != nil {
		t.Fatal(err)
	}
	requireClaimError(t, db, "aboutme_app", claimSingleExpression(claimUUID("abababab", 7), "mail.send", draining.replicaID, "NULL", "global", scope), "55000")
	requireClaimError(t, db, "aboutme_app", claimSSEExpression(claimUUID("abababab", 8), draining.replicaID, scope, nil), "55000")
	replay := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(mail.claimID, "mail.send", draining.replicaID, "NULL", "global", scope), "running")
	if !claimResultsEqual(replay, mail) {
		t.Fatalf("drain replay=%+v want %+v", replay, mail)
	}
	requireClaimOutcome(t, db, "aboutme_app", claimResolveExpression(waiting.claimID, draining.replicaID, hex.EncodeToString(waiting.digest)), "waiting")
	requireClaimOutcome(t, db, "aboutme_app", claimReleaseExpression(first.claimID, draining.replicaID, hex.EncodeToString(first.digest), "joined"), "released")
	requireClaimError(t, db, "aboutme_app", claimPromoteExpression(waiting.claimID, draining.replicaID, hex.EncodeToString(waiting.digest)), "55000")
	requireClaimOutcome(t, db, "aboutme_app", claimResolveExpression(waiting.claimID, draining.replicaID, hex.EncodeToString(waiting.digest)), "waiting")
	requireClaimOutcome(t, db, "aboutme_app", claimReleaseExpression(waiting.claimID, draining.replicaID, hex.EncodeToString(waiting.digest), "canceled"), "released")
	requireClaimOutcome(t, db, "aboutme_app", claimReleaseExpression(mail.claimID, draining.replicaID, hex.EncodeToString(mail.digest), "joined"), "released")
	requireClaimSummary(t, db, "mail.send", "global", scope, 0, 0)
	requireClaimSummary(t, db, "render.global_claim", "global", scope, 0, 0)

	online := validRegistrationIdentity("c")
	activeClaimReplica(t, db, online, "serving")
	live := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(claimUUID("abababab", 9), "password.hash", online.replicaID, "NULL", "global", scope), "running")
	if err := membershipWrite(t, db, `UPDATE public.runtime_capacity SET admission_enabled=false,lifecycle_phase='stopping',updated_at=clock_timestamp() WHERE singleton`); err != nil {
		t.Fatal(err)
	}
	requireClaimError(t, db, "aboutme_app", claimSingleExpression(claimUUID("abababab", 10), "password.hash", online.replicaID, "NULL", "global", scope), "55000")
	requireClaimError(t, db, "aboutme_app", claimPromoteExpression(live.claimID, online.replicaID, hex.EncodeToString(live.digest)), "55000")
	requireClaimOutcome(t, db, "aboutme_app", claimResolveExpression(live.claimID, online.replicaID, hex.EncodeToString(live.digest)), "running")
	requireClaimOutcome(t, db, "aboutme_app", claimReleaseExpression(live.claimID, online.replicaID, hex.EncodeToString(live.digest), "joined"), "released")
	requireClaimSummary(t, db, "password.hash", "global", scope, 0, 0)
}

func TestRuntimeSharedClaimOperationsRacesSerialize(t *testing.T) {
	db, _ := runtimeMembershipDB(t)
	id := validRegistrationIdentity("a")
	activeClaimReplica(t, db, id, "serving")
	replica := id.replicaID
	t.Run("duplicate_acquire_replays", func(t *testing.T) {
		claim := claimUUID("acacacac", 1)
		expression := claimSingleExpression(claim, "mail.send", replica, "NULL", "global", claimHex(40))
		holder := openClaimHolder(t, db, "aboutme_app", expression)
		result, err := raceClaimOperation(t, db, holder.commit, "aboutme_app", expression)
		if err != nil || result.outcome != "running" || result.allocation1.Int64 != 1 {
			t.Fatalf("result=%+v error=%v", result, err)
		}
		requireClaimSummary(t, db, "mail.send", "global", claimHex(40), 1, 0)
		var parents int
		if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM public.shared_claim_requests WHERE claim_id=$1`, claim).Scan(&parents); err != nil || parents != 1 {
			t.Fatalf("parents=%d error=%v", parents, err)
		}
	})
	t.Run("rolled_back_holder_admits_fresh", func(t *testing.T) {
		claim := claimUUID("acacacac", 2)
		expression := claimSingleExpression(claim, "mail.send", replica, "NULL", "global", claimHex(41))
		holder := openClaimHolder(t, db, "aboutme_app", expression)
		result, err := raceClaimOperation(t, db, holder.rollback, "aboutme_app", expression)
		if err != nil || result.outcome != "running" || result.allocation1.Int64 != 1 {
			t.Fatalf("result=%+v error=%v", result, err)
		}
		requireClaimSummary(t, db, "mail.send", "global", claimHex(41), 1, 0)
	})
	t.Run("concurrent_capacity_denies_third", func(t *testing.T) {
		scope := claimHex(42)
		requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(claimUUID("acacacac", 3), "mail.send", replica, "NULL", "global", scope), "running")
		holder := openClaimHolder(t, db, "aboutme_app", claimSingleExpression(claimUUID("acacacac", 4), "mail.send", replica, "NULL", "global", scope))
		result, err := raceClaimOperation(t, db, holder.commit, "aboutme_app", claimSingleExpression(claimUUID("acacacac", 5), "mail.send", replica, "NULL", "global", scope))
		if err != nil || result.outcome != "denied" {
			t.Fatalf("result=%+v error=%v", result, err)
		}
		requireClaimSummary(t, db, "mail.send", "global", scope, 2, 0)
	})
	t.Run("overlapping_c05_scopes_serialize", func(t *testing.T) {
		account := claimHex(43)
		holder := openClaimHolder(t, db, "aboutme_app", claimSSEExpression(claimUUID("acacacac", 6), replica, claimHex(44), &account))
		result, err := raceClaimOperation(t, db, holder.commit, "aboutme_app", claimSSEExpression(claimUUID("acacacac", 7), replica, claimHex(45), &account))
		if err != nil || result.outcome != "running" || result.allocation1.Int64 != 1 || result.allocation2.Int64 != 2 {
			t.Fatalf("result=%+v error=%v", result, err)
		}
		requireClaimSummary(t, db, "sse.fleet_account_ip", "account", account, 2, 0)
		requireClaimSummary(t, db, "sse.fleet_account_ip", "ip", claimHex(44), 1, 0)
		requireClaimSummary(t, db, "sse.fleet_account_ip", "ip", claimHex(45), 1, 0)
	})
	t.Run("release_then_promotion", func(t *testing.T) {
		scope := claimHex(46)
		a := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(claimUUID("acacacac", 8), "render.global_claim", replica, "'"+claimUUID("acacacad", 8)+"'", "global", scope), "running")
		b := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(claimUUID("acacacac", 9), "render.global_claim", replica, "'"+claimUUID("acacacad", 9)+"'", "global", scope), "waiting")
		holder := openClaimHolder(t, db, "aboutme_app", claimReleaseExpression(a.claimID, replica, hex.EncodeToString(a.digest), "joined"))
		result, err := raceClaimOperation(t, db, holder.commit, "aboutme_app", claimPromoteExpression(b.claimID, replica, hex.EncodeToString(b.digest)))
		if err != nil || result.outcome != "running" || result.allocation1.Int64 != 2 {
			t.Fatalf("result=%+v error=%v", result, err)
		}
		requireClaimSummary(t, db, "render.global_claim", "global", scope, 1, 0)
	})
	t.Run("drain_commits_before_acquire", func(t *testing.T) {
		drainer := validRegistrationIdentity("b")
		activeClaimReplica(t, db, drainer, "serving")
		_, tx := beginTransitionWrite(t, db)
		if _, err := tx.ExecContext(context.Background(), fmt.Sprintf(`UPDATE public.runtime_replicas SET state='draining',draining_at=clock_timestamp() WHERE replica_id='%s'`, drainer.replicaID)); err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				t.Error(rollbackErr)
			}
			t.Fatal(err)
		}
		_, err := raceClaimOperation(t, db, func() error { return finishTransitionWrite(tx) }, "aboutme_app", claimSingleExpression(claimUUID("acacacac", 10), "mail.send", drainer.replicaID, "NULL", "global", claimHex(47)))
		requireRegistrationError(t, err, "55000")
	})
	t.Run("receipt_cleanup_before_resolve", func(t *testing.T) {
		receipt := claimUUID("acacacac", 11)
		if err := membershipWrite(t, db, releasedReceiptFixture(receipt, replica, "mail.send", "global", claimHex(48), "25 hours", 1)...); err != nil {
			t.Fatal(err)
		}
		digest := goClaimRequestDigest(t, receipt, "mail.send", replica, nil, [][2]string{{"global", claimHex(48)}})
		requireClaimOutcome(t, db, "aboutme_app", claimResolveExpression(receipt, replica, digest), "released")
		conn, _ := openRegistrationTx(t, db, "aboutme_maintenance")
		var claims, summaries int
		if err := conn.QueryRowContext(context.Background(), `SELECT * FROM public.runtime_gc_released_claim_receipts()`).Scan(&claims, &summaries); err != nil || claims != 1 || summaries != 1 {
			if rollbackErr := rollbackRegistrationTx(conn); rollbackErr != nil {
				t.Error(rollbackErr)
			}
			t.Fatalf("gc=%d/%d error=%v", claims, summaries, err)
		}
		result, err := raceClaimOperation(t, db, func() error { return finishRegistrationTx(conn) }, "aboutme_app", claimResolveExpression(receipt, replica, digest))
		if err != nil {
			t.Fatal(err)
		}
		requireAbsentClaimShape(t, result, receipt)
	})
}

func TestRuntimeSharedClaimOperationsReceiptCleanupBounds(t *testing.T) {
	db, ctx := runtimeMembershipDB(t)
	id := validRegistrationIdentity("a")
	activeClaimReplica(t, db, id, "serving")
	replica := id.replicaID
	live := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(claimUUID("dcdcdcdc", 1), "mail.send", replica, "NULL", "global", claimHex(51)), "running")
	fixture := releasedReceiptFixture(claimUUID("dcdcdcdc", 2), replica, "mail.send", "global", claimHex(50), "25 hours", 1)
	fixture = append(fixture, releasedReceiptFixture(claimUUID("dcdcdcdc", 3), replica, "mail.send", "global", claimHex(53), "23 hours", 1)...)
	fixture = append(fixture, releasedReceiptFixture(claimUUID("dcdcdcdc", 4), replica, "mail.send", "global", claimHex(51), "25 hours", 2)...)
	fixture = append(fixture, releasedReceiptFixture(claimUUID("dcdcdcdc", 5), replica, "render.global_claim", "global", claimHex(54), "24 hours 10 seconds", 1)...)
	fixture = append(fixture, releasedReceiptFixture(claimUUID("dcdcdcdc", 6), replica, "render.global_claim", "global", claimHex(55), "23 hours 59 minutes 50 seconds", 1)...)
	if err := membershipWrite(t, db, fixture...); err != nil {
		t.Fatal(err)
	}
	oldRunning := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(claimUUID("dcdcdcdc", 7), "password.hash", replica, "NULL", "global", claimHex(56)), "running")
	corruptClaimRows(t, db, fmt.Sprintf(`UPDATE public.shared_claim_requests SET admitted_at=admitted_at-interval '30 hours' WHERE claim_id='%s'`, oldRunning.claimID))
	before := countClaimTables(t, db)
	claims, summaries, err := callClaimGCWithCommit(t, db, false)
	if err != nil || claims != 3 || summaries != 2 {
		t.Fatalf("uncommitted gc=%d/%d error=%v", claims, summaries, err)
	}
	if after := countClaimTables(t, db); after != before {
		t.Fatalf("rolled-back gc changed rows before=%+v after=%+v", before, after)
	}
	claims, summaries, err = callClaimGC(t, db)
	if err != nil || claims != 3 || summaries != 2 {
		t.Fatalf("gc=%d/%d error=%v", claims, summaries, err)
	}
	after := countClaimTables(t, db)
	if after.parents != before.parents-3 || after.scopes != before.scopes-3 || after.summaries != before.summaries-2 {
		t.Fatalf("gc rows before=%+v after=%+v", before, after)
	}
	for _, test := range []struct {
		claim  string
		exists bool
	}{
		{claimUUID("dcdcdcdc", 1), true}, {claimUUID("dcdcdcdc", 2), false}, {claimUUID("dcdcdcdc", 3), true}, {claimUUID("dcdcdcdc", 4), false}, {claimUUID("dcdcdcdc", 5), false}, {claimUUID("dcdcdcdc", 6), true}, {oldRunning.claimID, true},
	} {
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM public.shared_claim_requests WHERE claim_id=$1)`, test.claim).Scan(&exists); err != nil || exists != test.exists {
			t.Fatalf("claim %s exists=%t want=%t error=%v", test.claim, exists, test.exists, err)
		}
	}
	if summary := claimSummary(t, db, "mail.send", "global", claimHex(50)); summary.exists {
		t.Fatalf("unreferenced summary retained %+v", summary)
	}
	requireClaimSummary(t, db, "mail.send", "global", claimHex(51), 1, 0)
	requireClaimSummary(t, db, "mail.send", "global", claimHex(53), 0, 0)
	if summary := claimSummary(t, db, "render.global_claim", "global", claimHex(54)); summary.exists {
		t.Fatalf("boundary summary retained %+v", summary)
	}
	requireClaimSummary(t, db, "render.global_claim", "global", claimHex(55), 0, 0)
	requireClaimOutcome(t, db, "aboutme_app", claimResolveExpression(live.claimID, replica, hex.EncodeToString(live.digest)), "running")
	fresh := requireClaimOutcome(t, db, "aboutme_app", claimSingleExpression(claimUUID("dcdcdcdc", 8), "mail.send", replica, "NULL", "global", claimHex(50)), "running")
	if fresh.allocation1.Int64 != 1 {
		t.Fatalf("new summary lifetime ordinal=%d", fresh.allocation1.Int64)
	}
	batch := make([]string, 0, 3*257)
	for i := 0; i < 257; i++ {
		batch = append(batch, releasedReceiptFixture(claimUUID("dcdcdcdd", i+1), replica, "mcp.user_concurrent", "user", claimHex(57), "25 hours", i+1)...)
	}
	if err := membershipWrite(t, db, batch...); err != nil {
		t.Fatal(err)
	}
	for _, want := range [][2]int{{256, 0}, {1, 1}, {0, 0}} {
		claims, summaries, err := callClaimGC(t, db)
		if err != nil || claims != want[0] || summaries != want[1] {
			t.Fatalf("gc=%d/%d want=%v error=%v", claims, summaries, want, err)
		}
	}
	if summary := claimSummary(t, db, "mcp.user_concurrent", "user", claimHex(57)); summary.exists {
		t.Fatalf("batch summary retained %+v", summary)
	}
}

func TestRuntimeSharedClaimOperationsPreservesPopulatedVersion19(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := ProvisionDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := applyFS(ctx, db, runtimeTransitionFixtureFS(t, 19), LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO public.users(id,email,name) VALUES('20202020-2020-4020-8020-202020202020','claim-operations-preserve@example.test','claim operations preserved')`); err != nil {
		t.Fatal(err)
	}
	preserved := append(sharedClaimVectorFixture(), transitionFixture()...)
	preserved = append(preserved, transitionAckSQL(initiatorID), `INSERT INTO public.runtime_lifecycle_operations(operation_id,workflow_kind) VALUES('claim-operations-preserve-lifecycle','initial_serving')`,
		rateBucketSQL("oauth.failed_grant", "20", 1, map[string]string{"window_started_at": "transaction_timestamp()"}),
		pendingAttemptSQL("20202020-2020-4020-8020-202020202021", "private", "20", 1, storedPendingWindowOverrides("private", "20")))
	if err := membershipWrite(t, db, preserved...); err != nil {
		t.Fatal(err)
	}
	if _, err := callRegistration(t, db, "aboutme_app", "runtime_register_serving_replica", validRegistrationIdentity("3")); err != nil {
		t.Fatal(err)
	}
	countStatement := `SELECT s.generation,c.generation,(SELECT count(*) FROM public.runtime_replicas),(SELECT count(*) FROM public.runtime_replica_tasks),(SELECT count(*) FROM public.runtime_lifecycle_operations),(SELECT count(*) FROM public.public_transitions),(SELECT count(*) FROM public.public_transition_acks),(SELECT count(*) FROM public.shared_claim_requests),(SELECT count(*) FROM public.shared_claim_scopes),(SELECT count(*) FROM public.shared_claim_scope_summaries),(SELECT count(*) FROM public.shared_claim_policies),(SELECT count(*) FROM public.shared_rate_buckets),(SELECT count(*) FROM public.shared_admission_attempts),(SELECT count(*) FROM public.users) FROM public.runtime_write_state s CROSS JOIN public.runtime_capacity c WHERE s.singleton AND c.singleton`
	var before, after [14]int64
	scan := func(target *[14]int64) error {
		return db.QueryRowContext(ctx, countStatement).Scan(&target[0], &target[1], &target[2], &target[3], &target[4], &target[5], &target[6], &target[7], &target[8], &target[9], &target[10], &target[11], &target[12], &target[13])
	}
	if err := scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := applyFS(ctx, db, runtimeTransitionFixtureFS(t, 20), LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	if err := scan(&after); err != nil {
		t.Fatal(err)
	}
	tail := func(values [14]int64) [13]int64 { var rest [13]int64; copy(rest[:], values[1:]); return rest }
	if after[0] != before[0]+1 || tail(after) != tail(before) || before[7] != 1 || before[8] != 2 || before[9] != 2 || before[10] != 6 || before[2] != 3 {
		t.Fatalf("preservation before=%v after=%v", before, after)
	}
	var digest string
	if err := db.QueryRowContext(ctx, `SELECT encode(public.runtime_shared_claim_request_digest($1),'hex') FROM public.shared_claim_requests WHERE claim_id=$1`, claimVectorID).Scan(&digest); err != nil || digest != claimVectorDigest {
		t.Fatalf("digest=%s error=%v", digest, err)
	}
	resolved := requireClaimOutcome(t, db, "aboutme_app", claimResolveExpression(claimVectorID, claimVectorReplica, claimVectorDigest), "running")
	requireStoredClaimShape(t, resolved, "sse.fleet_account_ip", claimVectorReplica, "running", 2)
}

func TestRuntimeSharedClaimOperationsFailedMigrationRollsBackTypeFunctionsAndGeneration(t *testing.T) {
	db := newCompositionTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := ProvisionDatabase(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := applyFS(ctx, db, runtimeTransitionFixtureFS(t, 19), LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	var before int64
	if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	fixture := runtimeTransitionFixtureFS(t, 20)
	file := fixture["00020_runtime_shared_claim_operations.sql"]
	anchor := "RESET ROLE;\nREVOKE CREATE"
	if file == nil || !strings.Contains(string(file.Data), anchor) {
		t.Fatal("migration 20 failure anchor is missing")
	}
	file.Data = []byte(strings.Replace(string(file.Data), anchor, "SELECT 1/0;\n"+anchor, 1))
	_, applyErr := applyFS(ctx, db, fixture, LocalAdminMigratorIdentity())
	requireRegistrationError(t, applyErr, "22012")
	names := append([]string(nil), claimHelperNames...)
	for name := range claimPublicSignatures {
		names = append(names, name)
	}
	var after int64
	var functions, types int
	var appGrant bool
	if err := db.QueryRowContext(ctx, `SELECT generation,(SELECT count(*) FROM pg_proc WHERE pronamespace='public'::regnamespace AND proname=ANY($1)),(SELECT count(*) FROM pg_type WHERE typnamespace='public'::regnamespace AND typname='runtime_claim_result'),EXISTS(SELECT 1 FROM pg_default_acl WHERE defaclrole='aboutme_runtime_owner'::regrole) FROM public.runtime_write_state WHERE singleton`, names).Scan(&after, &functions, &types, &appGrant); err != nil || after != before || functions != 0 || types != 0 {
		t.Fatalf("generation=%d/%d functions=%d types=%d error=%v", before, after, functions, types, err)
	}
	_ = appGrant
	var schemaCreate bool
	if err := db.QueryRowContext(ctx, `SELECT has_schema_privilege('aboutme_runtime_owner','public','CREATE')`).Scan(&schemaCreate); err != nil || schemaCreate {
		t.Fatalf("schema create retained=%t error=%v", schemaCreate, err)
	}
}

var _ = sql.ErrNoRows
