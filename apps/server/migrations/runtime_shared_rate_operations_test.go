package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRuntimeSharedRateOperationsFunctionsExist(t *testing.T) {
	db, ctx := runtimeSharedRateDB(t)
	for _, composite := range rateResultComposites {
		var attributes string
		if err := db.QueryRowContext(ctx, `SELECT COALESCE(string_agg(a.attname||':'||format_type(a.atttypid,a.atttypmod),',' ORDER BY a.attnum),'') FROM pg_type t JOIN pg_class c ON c.oid=t.typrelid JOIN pg_attribute a ON a.attrelid=c.oid AND a.attnum>0 AND NOT a.attisdropped WHERE t.typnamespace='public'::regnamespace AND t.typname=$1`, composite.name).Scan(&attributes); err != nil {
			t.Fatal(err)
		}
		if attributes != composite.attributes {
			t.Errorf("type %s attributes=%q want=%q", composite.name, attributes, composite.attributes)
		}
	}
	for _, operation := range rateOperationSignatures {
		var returns string
		if err := db.QueryRowContext(ctx, `SELECT COALESCE((SELECT n.nspname||'.'||t.typname FROM pg_proc p JOIN pg_type t ON t.oid=p.prorettype JOIN pg_namespace n ON n.oid=t.typnamespace WHERE p.oid=to_regprocedure('public.'||$1)),'')`, operation.name+operation.signature).Scan(&returns); err != nil {
			t.Fatal(err)
		}
		if returns != "public."+operation.result {
			t.Errorf("function %s%s returns=%q want=%q", operation.name, operation.signature, returns, "public."+operation.result)
		}
	}
	var clock string
	if err := db.QueryRowContext(ctx, `SELECT COALESCE((SELECT format_type(prorettype,NULL)||' argc='||pronargs||' volatile='||provolatile::text FROM pg_proc WHERE oid=to_regprocedure('public.runtime_sample_rate_time()')),'')`).Scan(&clock); err != nil {
		t.Fatal(err)
	}
	if clock != "timestamp with time zone argc=0 volatile=v" {
		t.Errorf("runtime_sample_rate_time=%q want=%q", clock, "timestamp with time zone argc=0 volatile=v")
	}
}

// TestRuntimeSharedRateOperationsTokenPolicyBoundaries proves the exact
// consume, deny and ceiling-retry boundary of every catalog token rate.
func TestRuntimeSharedRateOperationsTokenPolicyBoundaries(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	setRateClock(t, db, rateFutureClock)
	at := "'" + rateFutureClock + "'::timestamptz"
	expected := rateTime(t, rateFutureClock)
	for index, policy := range rateTokenPolicies(t, db) {
		t.Run(policy.policy, func(t *testing.T) {
			allowDigest := rateByteDigest(fmt.Sprintf("%02x", index+1))
			edgeDigest := rateByteDigest(fmt.Sprintf("%02x", index+40))
			emptyDigest := rateByteDigest(fmt.Sprintf("%02x", index+80))
			seed := func(digest string, numerator int64) {
				rateSeedBucket(t, db, policy.policy, digest[:2], 1, map[string]string{
					"key_digest": fmt.Sprintf("decode('%s','hex')", digest), "last_seen": at,
					"token_numerator": fmt.Sprint(numerator), "refill_at": at,
				})
			}
			seed(allowDigest, policy.window)
			seed(edgeDigest, policy.window-1)
			seed(emptyDigest, 0)

			allowed, err := callRateDecision(t, db, "aboutme_app", rateTokenExpression(policy.policy, allowDigest))
			if err != nil {
				t.Fatal(err)
			}
			if !allowed.allowed || allowed.retry != 0 || allowed.kind != "private" || !allowed.partition.Valid || allowed.partition.Int16 != 1 || !allowed.effectiveAt.Equal(expected) {
				t.Fatalf("exact allow=%+v", allowed)
			}
			if state := rateBucket(t, db, policy.policy, allowDigest); state.numerator.Int64 != 0 {
				t.Fatalf("exact allow numerator=%d", state.numerator.Int64)
			}

			edge, err := callRateDecision(t, db, "aboutme_app", rateTokenExpression(policy.policy, edgeDigest))
			if err != nil {
				t.Fatal(err)
			}
			if edge.allowed || edge.retry != 1 || edge.kind != "private" {
				t.Fatalf("one-below deny=%+v", edge)
			}
			if state := rateBucket(t, db, policy.policy, edgeDigest); state.numerator.Int64 != policy.window-1 {
				t.Fatalf("denial consumed numerator=%d want=%d", state.numerator.Int64, policy.window-1)
			}

			empty, err := callRateDecision(t, db, "aboutme_app", rateTokenExpression(policy.policy, emptyDigest))
			if err != nil {
				t.Fatal(err)
			}
			if empty.allowed || empty.retry != rateTokenRetry(policy.capacity, policy.window) {
				t.Fatalf("empty deny=%+v want retry=%d", empty, rateTokenRetry(policy.capacity, policy.window))
			}
			if state := rateBucket(t, db, policy.policy, emptyDigest); state.numerator.Int64 != 0 {
				t.Fatalf("empty denial changed numerator=%d", state.numerator.Int64)
			}
		})
	}
}

// TestRuntimeSharedRateOperationsTokenRefillAndClockBounds proves the exact
// refill threshold, the long forward-jump cap and the backward clamp.
func TestRuntimeSharedRateOperationsTokenRefillAndClockBounds(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	const capacity, window = 300, 60000000
	threshold := rateCeilDiv(window, capacity)
	base := rateFutureClock
	seed := func(digest string) {
		rateSeedBucket(t, db, ratePolicyToken, digest[:2], 1, map[string]string{
			"key_digest": fmt.Sprintf("decode('%s','hex')", digest),
			"last_seen":  "'" + base + "'::timestamptz", "token_numerator": "0", "refill_at": "'" + base + "'::timestamptz",
		})
	}
	below, exact, jump, backward := rateByteDigest("a1"), rateByteDigest("a2"), rateByteDigest("a3"), rateByteDigest("a4")
	for _, digest := range []string{below, exact, jump, backward} {
		seed(digest)
	}

	setRateClock(t, db, fmt.Sprintf("2030-01-01 00:00:00.%06d+00", threshold-1))
	result, err := callRateDecision(t, db, "aboutme_app", rateTokenExpression(ratePolicyToken, below))
	if err != nil {
		t.Fatal(err)
	}
	if result.allowed || result.retry != 1 {
		t.Fatalf("one microsecond below threshold=%+v", result)
	}
	if state := rateBucket(t, db, ratePolicyToken, below); state.numerator.Int64 != (threshold-1)*capacity {
		t.Fatalf("partial refill numerator=%d want=%d", state.numerator.Int64, (threshold-1)*capacity)
	}

	setRateClock(t, db, fmt.Sprintf("2030-01-01 00:00:00.%06d+00", threshold))
	if result, err = callRateDecision(t, db, "aboutme_app", rateTokenExpression(ratePolicyToken, exact)); err != nil {
		t.Fatal(err)
	}
	if !result.allowed || result.retry != 0 {
		t.Fatalf("exact threshold=%+v", result)
	}
	if state := rateBucket(t, db, ratePolicyToken, exact); state.numerator.Int64 != 0 {
		t.Fatalf("exact threshold numerator=%d", state.numerator.Int64)
	}

	setRateClock(t, db, "2130-01-01 00:00:00+00")
	if result, err = callRateDecision(t, db, "aboutme_app", rateTokenExpression(ratePolicyToken, jump)); err != nil {
		t.Fatal(err)
	}
	if !result.allowed || result.retry != 0 {
		t.Fatalf("long forward jump=%+v", result)
	}
	if state := rateBucket(t, db, ratePolicyToken, jump); state.numerator.Int64 != int64(capacity)*window-window {
		t.Fatalf("saturated numerator=%d want=%d", state.numerator.Int64, int64(capacity)*window-window)
	}

	before := rateClock(t, db, ratePolicyToken)
	if err = membershipWrite(t, db, fmt.Sprintf(`UPDATE public.shared_rate_buckets SET token_numerator=0,refill_at='%s'::timestamptz,last_seen='%s'::timestamptz WHERE policy_id='%s' AND key_digest=decode('%s','hex')`, before.highWater.Format(time.RFC3339Nano), before.highWater.Format(time.RFC3339Nano), ratePolicyToken, backward)); err != nil {
		t.Fatal(err)
	}
	setRateClock(t, db, "2029-01-01 00:00:00+00")
	if result, err = callRateDecision(t, db, "aboutme_app", rateTokenExpression(ratePolicyToken, backward)); err != nil {
		t.Fatal(err)
	}
	if !result.effectiveAt.Equal(before.highWater) {
		t.Fatalf("backward sample effective=%v want clamped=%v", result.effectiveAt, before.highWater)
	}
	if result.allowed || result.retry != rateTokenRetry(capacity, window) {
		t.Fatalf("backward sample changed the decision=%+v", result)
	}
	if state := rateBucket(t, db, ratePolicyToken, backward); state.numerator.Int64 != 0 || !state.lastSeen.Equal(before.highWater) {
		t.Fatalf("backward sample reset debt or idle=%+v", state)
	}
	after := rateClock(t, db, ratePolicyToken)
	if !after.highWater.Equal(before.highWater) || after.anomalies != before.anomalies+1 || !after.lastRaw.Equal(rateTime(t, "2029-01-01 00:00:00+00")) {
		t.Fatalf("backward clock before=%+v after=%+v", before, after)
	}
}

// TestRuntimeSharedRateOperationsSamplesClockOncePerCall proves each operation
// invokes the owner-only helper exactly once.
func TestRuntimeSharedRateOperationsSamplesClockOncePerCall(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	setRateClock(t, db, rateFutureClock)
	enableRatePartitions(t, db, ratePolicyToken, 1)
	if calls := rateClockCalls(t, db); calls != 0 {
		t.Fatalf("clock calls before work=%d", calls)
	}
	if _, err := callRateDecision(t, db, "aboutme_app", rateTokenExpression(ratePolicyToken, rateDigest(1))); err != nil {
		t.Fatal(err)
	}
	if calls := rateClockCalls(t, db); calls != 1 {
		t.Fatalf("clock calls after one admission=%d want=1", calls)
	}
	if _, err := callRateDecision(t, db, "aboutme_app", rateTokenExpression(ratePolicyToken, rateDigest(1))); err != nil {
		t.Fatal(err)
	}
	if calls := rateClockCalls(t, db); calls != 2 {
		t.Fatalf("clock calls after two admissions=%d want=2", calls)
	}
	restoreRateClock(t, db)
	var body string
	if err := db.QueryRowContext(context.Background(), `SELECT prosrc FROM pg_proc WHERE oid=to_regprocedure('public.runtime_sample_rate_time()')`).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(body) != "SELECT clock_timestamp()" {
		t.Fatalf("restored clock body=%q", body)
	}
}

// TestRuntimeSharedRateOperationsSlugRollingWindow proves the P12 rolling
// cutoff, the 29/30/31 boundary and the fixed one-second denial retry.
func TestRuntimeSharedRateOperationsSlugRollingWindow(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	setRateClock(t, db, rateFutureClock)
	now := rateTime(t, rateFutureClock)
	events := func(count int, offset string) map[string]string {
		return map[string]string{
			"last_seen":      "'" + rateFutureClock + "'::timestamptz",
			"count":          fmt.Sprint(count),
			"rolling_events": fmt.Sprintf(`ARRAY(SELECT '%s'::timestamptz-%s FROM generate_series(1,%d))`, rateFutureClock, offset, count),
		}
	}
	below, atLimit, cutoff, inside := rateByteDigest("b1"), rateByteDigest("b2"), rateByteDigest("b3"), rateByteDigest("b4")
	for digest, override := range map[string]map[string]string{
		below:   events(29, "interval '30 minutes'"),
		atLimit: events(30, "interval '30 minutes'"),
		cutoff:  events(30, "interval '1 hour'"),
		inside:  events(30, "(interval '1 hour'-interval '1 microsecond')"),
	} {
		override["key_digest"] = fmt.Sprintf("decode('%s','hex')", digest)
		rateSeedBucket(t, db, ratePolicySlug, digest[:2], 1, override)
	}
	for _, test := range []struct {
		name    string
		digest  string
		allowed bool
		retry   int
		count   int32
	}{
		{"twenty_nine_allows", below, true, 0, 30},
		{"thirty_denies", atLimit, false, 1, 30},
		{"exact_hour_cutoff_prunes_all", cutoff, true, 0, 1},
		{"one_microsecond_inside_denies", inside, false, 1, 30},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := callRateDecision(t, db, "aboutme_app", rateSlugExpression(test.digest))
			if err != nil {
				t.Fatal(err)
			}
			if result.allowed != test.allowed || result.retry != test.retry || result.kind != "private" || !result.effectiveAt.Equal(now) {
				t.Fatalf("result=%+v", result)
			}
			state := rateBucket(t, db, ratePolicySlug, test.digest)
			if state.eventCount.Int32 != test.count || state.count.Int32 != test.count {
				t.Fatalf("events=%d count=%d want=%d", state.eventCount.Int32, state.count.Int32, test.count)
			}
		})
	}
	t.Run("repeated_denial_keeps_thirty", func(t *testing.T) {
		result, err := callRateDecision(t, db, "aboutme_app", rateSlugExpression(atLimit))
		if err != nil {
			t.Fatal(err)
		}
		if result.allowed || result.retry != 1 {
			t.Fatalf("repeated denial=%+v", result)
		}
		var oldest, newest time.Time
		if err := db.QueryRowContext(context.Background(), `SELECT (SELECT min(e) FROM unnest(rolling_events) e),(SELECT max(e) FROM unnest(rolling_events) e) FROM public.shared_rate_buckets WHERE policy_id=$1 AND key_digest=decode($2,'hex')`, ratePolicySlug, atLimit).Scan(&oldest, &newest); err != nil {
			t.Fatal(err)
		}
		if !newest.Equal(now) || !oldest.Equal(now.Add(-30*time.Minute)) {
			t.Fatalf("denied debt oldest=%v newest=%v", oldest, newest)
		}
		if state := rateBucket(t, db, ratePolicySlug, atLimit); state.eventCount.Int32 != 30 {
			t.Fatalf("denied array grew to %d", state.eventCount.Int32)
		}
	})
}

// TestRuntimeSharedRateOperationsPartitionRoutingAndOverflow proves ownership,
// the lowest enabled route, bounded legitimate expiry and the overflow
// fallback that never grants work because of capacity pressure.
func TestRuntimeSharedRateOperationsPartitionRoutingAndOverflow(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	const policy, full = ratePolicyAlloc, 72000000000
	t.Run("both_disabled_uses_overflow", func(t *testing.T) {
		result, err := callRateDecision(t, db, "aboutme_app", rateTokenExpression(policy, rateDigest(1)))
		if err != nil {
			t.Fatal(err)
		}
		if !result.allowed || result.kind != "overflow" || result.partition.Valid {
			t.Fatalf("overflow route=%+v", result)
		}
		if rateBucketCount(t, db, policy) != 0 {
			t.Fatal("disabled partitions owned a new key")
		}
	})
	t.Run("lowest_enabled_partition_owns_new_key", func(t *testing.T) {
		enableRatePartitions(t, db, policy, 2)
		result, err := callRateDecision(t, db, "aboutme_app", rateTokenExpression(policy, rateDigest(2)))
		if err != nil {
			t.Fatal(err)
		}
		if !result.allowed || result.kind != "private" || result.partition.Int16 != 2 {
			t.Fatalf("partition two route=%+v", result)
		}
		enableRatePartitions(t, db, policy, 1)
		if result, err = callRateDecision(t, db, "aboutme_app", rateTokenExpression(policy, rateDigest(3))); err != nil {
			t.Fatal(err)
		}
		if result.partition.Int16 != 1 {
			t.Fatalf("lowest enabled route=%+v", result)
		}
		if one, two := ratePartition(t, db, policy, 1), ratePartition(t, db, policy, 2); one.activeKeys != 1 || two.activeKeys != 1 {
			t.Fatalf("active keys one=%d two=%d", one.activeKeys, two.activeKeys)
		}
	})
	t.Run("existing_key_stays_charged_in_disabled_partition", func(t *testing.T) {
		disableRatePartitions(t, db, policy, 2)
		result, err := callRateDecision(t, db, "aboutme_app", rateTokenExpression(policy, rateDigest(2)))
		if err != nil {
			t.Fatal(err)
		}
		if !result.allowed || result.kind != "private" || result.partition.Int16 != 2 {
			t.Fatalf("disabled existing key=%+v", result)
		}
		if state := rateBucket(t, db, policy, rateDigest(2)); state.numerator.Int64 >= full {
			t.Fatalf("disabled key was not charged: %+v", state)
		}
		enableRatePartitions(t, db, policy, 2)
	})
	t.Run("full_partition_routes_to_the_next_then_overflow", func(t *testing.T) {
		rateFillBuckets(t, db, policy, 1, 9999, false)
		result, err := callRateDecision(t, db, "aboutme_app", rateTokenExpression(policy, rateDigest(4)))
		if err != nil {
			t.Fatal(err)
		}
		if result.partition.Int16 != 2 {
			t.Fatalf("full partition one route=%+v (active=%d)", result, ratePartition(t, db, policy, 1).activeKeys)
		}
		rateFillBuckets(t, db, policy, 2, 9998, false)
		if result, err = callRateDecision(t, db, "aboutme_app", rateTokenExpression(policy, rateDigest(5))); err != nil {
			t.Fatal(err)
		}
		if result.kind != "overflow" || result.partition.Valid {
			t.Fatalf("both full route=%+v one=%d two=%d", result, ratePartition(t, db, policy, 1).activeKeys, ratePartition(t, db, policy, 2).activeKeys)
		}
		if one, two := ratePartition(t, db, policy, 1), ratePartition(t, db, policy, 2); one.activeKeys != 10000 || two.activeKeys != 10000 {
			t.Fatalf("saturated active keys one=%d two=%d", one.activeKeys, two.activeKeys)
		}
	})
	t.Run("bounded_expiry_frees_one_oldest_eligible_row", func(t *testing.T) {
		if err := membershipWrite(t, db, fmt.Sprintf(`UPDATE public.shared_rate_buckets SET last_seen=transaction_timestamp()-interval '48 hours' WHERE policy_id='%s' AND key_digest=sha256(convert_to('fill-1-1','UTF8'))`, policy)); err != nil {
			t.Fatal(err)
		}
		result, err := callRateDecision(t, db, "aboutme_app", rateTokenExpression(policy, rateDigest(6)))
		if err != nil {
			t.Fatal(err)
		}
		if result.partition.Int16 != 1 || result.kind != "private" {
			t.Fatalf("expiry route=%+v", result)
		}
		if state := rateBucket(t, db, policy, rateFillDigest(t, db, 1, 1)); state.exists {
			t.Fatal("expired oldest row survived")
		}
		if one := ratePartition(t, db, policy, 1); one.activeKeys != 10000 {
			t.Fatalf("active keys after expiry=%d", one.activeKeys)
		}
		if err := membershipWrite(t, db, fmt.Sprintf(`UPDATE public.shared_rate_buckets SET last_seen=transaction_timestamp()-interval '48 hours' WHERE policy_id='%s' AND key_digest IN (sha256(convert_to('fill-1-2','UTF8')),sha256(convert_to('fill-1-3','UTF8')))`, policy)); err != nil {
			t.Fatal(err)
		}
		if _, err := callRateDecision(t, db, "aboutme_app", rateTokenExpression(policy, rateDigest(7))); err != nil {
			t.Fatal(err)
		}
		survivors := 0
		for _, index := range []int{2, 3} {
			if rateBucket(t, db, policy, rateFillDigest(t, db, 1, index)).exists {
				survivors++
			}
		}
		if survivors != 1 {
			t.Fatalf("bounded expiry deleted %d of two eligible rows", 2-survivors)
		}
		var rows, active int
		if err := db.QueryRowContext(context.Background(), `SELECT (SELECT count(*) FROM public.shared_rate_buckets WHERE policy_id=$1 AND partition=1),(SELECT active_keys FROM public.shared_rate_partitions WHERE policy_id=$1 AND partition=1)`, policy).Scan(&rows, &active); err != nil || rows != active {
			t.Fatalf("count equality rows=%d active=%d error=%v", rows, active, err)
		}
	})
}

func requirePasswordShape(t *testing.T, r ratePasswordResult, kind string, partition int, exhausted bool, retry int, allocated, refreshed bool) {
	t.Helper()
	if r.kind != kind || r.exhausted != exhausted || r.retry != retry || r.allocated != allocated || r.refreshed != refreshed {
		t.Fatalf("password result=%+v want kind=%s exhausted=%t retry=%d allocated=%t refreshed=%t", r, kind, exhausted, retry, allocated, refreshed)
	}
	if partition == 0 {
		if r.partition.Valid {
			t.Fatalf("password result partition=%+v want null", r.partition)
		}
		return
	}
	if !r.partition.Valid || int(r.partition.Int16) != partition {
		t.Fatalf("password result partition=%+v want %d", r.partition, partition)
	}
}

// TestRuntimeSharedRateOperationsPasswordFailureLifecycle proves the P07
// state, record and clear matrices, first-window creation, nonextension and
// exact expiry.
func TestRuntimeSharedRateOperationsPasswordFailureLifecycle(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	setRateClock(t, db, rateFutureClock)
	enableRatePartitions(t, db, ratePolicyFailure, 1)
	digest := rateDigest(11)
	start := rateTime(t, rateFutureClock)

	overflowBefore := rateOverflow(t, db, ratePolicyFailure)
	absent, err := callRatePassword(t, db, "aboutme_app", rateStateExpression(digest))
	if err != nil {
		t.Fatal(err)
	}
	requirePasswordShape(t, absent, "private", 0, false, 0, false, false)
	if rateBucketCount(t, db, ratePolicyFailure) != 0 {
		t.Fatal("absent state allocated a bucket")
	}
	if after := rateOverflow(t, db, ratePolicyFailure); !after.lastSeen.Equal(overflowBefore.lastSeen) {
		t.Fatal("absent state touched overflow")
	}

	first, err := callRatePassword(t, db, "aboutme_app", rateRecordExpression(digest))
	if err != nil {
		t.Fatal(err)
	}
	requirePasswordShape(t, first, "private", 1, false, 0, true, true)
	state := rateBucket(t, db, ratePolicyFailure, digest)
	if state.count.Int32 != 1 || !state.startedAt.Time.Equal(start) {
		t.Fatalf("first window=%+v", state)
	}

	existing, err := callRatePassword(t, db, "aboutme_app", rateStateExpression(digest))
	if err != nil {
		t.Fatal(err)
	}
	requirePasswordShape(t, existing, "private", 1, false, 0, false, true)

	for i := 2; i <= 10; i++ {
		result, recordErr := callRatePassword(t, db, "aboutme_app", rateRecordExpression(digest))
		if recordErr != nil {
			t.Fatal(recordErr)
		}
		requirePasswordShape(t, result, "private", 1, i == 10, map[bool]int{true: 900, false: 0}[i == 10], false, true)
	}
	eleventh, err := callRatePassword(t, db, "aboutme_app", rateRecordExpression(digest))
	if err != nil {
		t.Fatal(err)
	}
	requirePasswordShape(t, eleventh, "private", 1, true, 900, false, true)
	if state = rateBucket(t, db, ratePolicyFailure, digest); state.count.Int32 != 10 || !state.startedAt.Time.Equal(start) {
		t.Fatalf("nonextension state=%+v", state)
	}

	setRateClock(t, db, "2030-01-01 00:14:59.999999+00")
	near, err := callRatePassword(t, db, "aboutme_app", rateStateExpression(digest))
	if err != nil {
		t.Fatal(err)
	}
	requirePasswordShape(t, near, "private", 1, true, 1, false, true)

	setRateClock(t, db, "2030-01-01 00:15:00+00")
	expired, err := callRatePassword(t, db, "aboutme_app", rateStateExpression(digest))
	if err != nil {
		t.Fatal(err)
	}
	requirePasswordShape(t, expired, "private", 1, false, 0, false, true)
	if state = rateBucket(t, db, ratePolicyFailure, digest); state.count.Int32 != 10 || !state.startedAt.Time.Equal(start) {
		t.Fatalf("state swept stored debt=%+v", state)
	}
	if !state.lastSeen.Equal(rateTime(t, "2030-01-01 00:15:00+00")) {
		t.Fatalf("state did not refresh activity=%v", state.lastSeen)
	}

	renewed, err := callRatePassword(t, db, "aboutme_app", rateRecordExpression(digest))
	if err != nil {
		t.Fatal(err)
	}
	requirePasswordShape(t, renewed, "private", 1, false, 0, false, true)
	if state = rateBucket(t, db, ratePolicyFailure, digest); state.count.Int32 != 1 || !state.startedAt.Time.Equal(rateTime(t, "2030-01-01 00:15:00+00")) {
		t.Fatalf("renewed window=%+v", state)
	}

	cleared, err := callRateClear(t, db, "aboutme_app", rateClearExpression(digest))
	if err != nil {
		t.Fatal(err)
	}
	if !cleared.cleared || cleared.kind != "private" || !cleared.partition.Valid || cleared.partition.Int16 != 1 {
		t.Fatalf("clear=%+v", cleared)
	}
	if rateBucket(t, db, ratePolicyFailure, digest).exists || ratePartition(t, db, ratePolicyFailure, 1).activeKeys != 0 {
		t.Fatal("clear did not delete the bucket and decrement active keys")
	}

	overflowBefore = rateOverflow(t, db, ratePolicyFailure)
	absentClear, err := callRateClear(t, db, "aboutme_app", rateClearExpression(digest))
	if err != nil {
		t.Fatal(err)
	}
	if absentClear.cleared || absentClear.kind != "private" || absentClear.partition.Valid {
		t.Fatalf("absent clear=%+v", absentClear)
	}
	if after := rateOverflow(t, db, ratePolicyFailure); !after.lastSeen.Equal(overflowBefore.lastSeen) || after.count.Int32 != overflowBefore.count.Int32 {
		t.Fatal("absent clear read or changed overflow")
	}
	if ratePartition(t, db, ratePolicyFailure, 1).activeKeys != 0 {
		t.Fatal("absent clear changed active keys")
	}
}

// TestRuntimeSharedRateOperationsPasswordOverflowRouting proves the
// no-enabled-partition and saturated routes and that overflow is never
// cleared.
func TestRuntimeSharedRateOperationsPasswordOverflowRouting(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	setRateClock(t, db, rateFutureClock)
	digest := rateDigest(12)
	state, err := callRatePassword(t, db, "aboutme_app", rateStateExpression(digest))
	if err != nil {
		t.Fatal(err)
	}
	requirePasswordShape(t, state, "overflow", 0, false, 0, false, true)
	if overflow := rateOverflow(t, db, ratePolicyFailure); !overflow.lastSeen.Equal(rateTime(t, rateFutureClock)) {
		t.Fatalf("overflow activity=%v", overflow.lastSeen)
	}
	for i := 1; i <= 10; i++ {
		result, recordErr := callRatePassword(t, db, "aboutme_app", rateRecordExpression(digest))
		if recordErr != nil {
			t.Fatal(recordErr)
		}
		requirePasswordShape(t, result, "overflow", 0, i == 10, map[bool]int{true: 900, false: 0}[i == 10], false, true)
	}
	if overflow := rateOverflow(t, db, ratePolicyFailure); overflow.count.Int32 != 10 {
		t.Fatalf("overflow count=%d", overflow.count.Int32)
	}
	if rateBucketCount(t, db, ratePolicyFailure) != 0 {
		t.Fatal("overflow route allocated a private bucket")
	}
	cleared, err := callRateClear(t, db, "aboutme_app", rateClearExpression(digest))
	if err != nil {
		t.Fatal(err)
	}
	if cleared.cleared || cleared.kind != "private" || cleared.partition.Valid {
		t.Fatalf("overflow clear=%+v", cleared)
	}
	if overflow := rateOverflow(t, db, ratePolicyFailure); overflow.count.Int32 != 10 {
		t.Fatalf("clear changed overflow count=%d", overflow.count.Int32)
	}
	restoreRateClock(t, db)
	rateFillBuckets(t, db, ratePolicyFailure, 1, 10000, false)
	enableRatePartitions(t, db, ratePolicyFailure, 1)
	saturated, err := callRatePassword(t, db, "aboutme_app", rateStateExpression(rateDigest(13)))
	if err != nil {
		t.Fatal(err)
	}
	requirePasswordShape(t, saturated, "overflow", 0, true, saturated.retry, false, true)
	if saturated.retry < 1 {
		t.Fatalf("saturated overflow retry=%d", saturated.retry)
	}
	if rows := rateBucketCount(t, db, ratePolicyFailure); rows != 10000 {
		t.Fatalf("saturated state changed rows=%d", rows)
	}
	if ratePartition(t, db, ratePolicyFailure, 1).activeKeys != 10000 {
		t.Fatal("saturated state changed active keys")
	}
}

func requireReserveShape(t *testing.T, r rateReserveResult, allowed bool, retry int, kind string, partition int, replayed bool) {
	t.Helper()
	if r.allowed != allowed || r.retry != retry || r.replayed != replayed {
		t.Fatalf("reserve=%+v want allowed=%t retry=%d replayed=%t", r, allowed, retry, replayed)
	}
	if kind == "" {
		if r.kind.Valid || r.partition.Valid {
			t.Fatalf("denied reserve disclosed a bucket=%+v", r)
		}
		return
	}
	if !r.kind.Valid || r.kind.String != kind {
		t.Fatalf("reserve kind=%+v want %s", r.kind, kind)
	}
	if partition == 0 {
		if r.partition.Valid {
			t.Fatalf("reserve partition=%+v want null", r.partition)
		}
		return
	}
	if !r.partition.Valid || int(r.partition.Int16) != partition {
		t.Fatalf("reserve partition=%+v want %d", r.partition, partition)
	}
}

// TestRuntimeSharedRateOperationsFailedGrantReserveMatrix proves the P22
// unkeyed client digest, first-window creation, the exact ten-slot bound, the
// denial that adds no debt and exact retained replay.
func TestRuntimeSharedRateOperationsFailedGrantReserveMatrix(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	setRateClock(t, db, rateFutureClock)
	enableRatePartitions(t, db, ratePolicyAttempt, 1)
	client := rateUUID("cccccccc", 1)
	digest := goRateClientDigest(t, client)
	start := rateTime(t, rateFutureClock)

	first, err := callRateReserve(t, db, "aboutme_app", rateReserveExpression(rateUUID("aaaaaaaa", 1), client))
	if err != nil {
		t.Fatal(err)
	}
	requireReserveShape(t, first, true, 0, "private", 1, false)
	bucket := rateBucket(t, db, ratePolicyAttempt, digest)
	if !bucket.exists || bucket.count.Int32 != 0 || !bucket.startedAt.Valid || !bucket.startedAt.Time.Equal(start) {
		t.Fatalf("first pending window=%+v (sql digest must match the pinned go vector)", bucket)
	}
	attempt := rateAttempt(t, db, rateUUID("aaaaaaaa", 1))
	if !attempt.exists || attempt.state != "pending" || attempt.kind != "private" || attempt.partition.Int16 != 1 ||
		!attempt.windowStartedAt.Equal(start) || !attempt.reservedAt.Equal(start) || !attempt.effectiveUntil.Equal(start.Add(15*time.Minute)) {
		t.Fatalf("stored attempt=%+v", attempt)
	}
	if got := fmt.Sprintf("%x", attempt.digest); got != digest {
		t.Fatalf("sql client digest=%s go vector=%s", got, digest)
	}

	for i := 2; i <= 10; i++ {
		result, reserveErr := callRateReserve(t, db, "aboutme_app", rateReserveExpression(rateUUID("aaaaaaaa", i), client))
		if reserveErr != nil {
			t.Fatal(reserveErr)
		}
		requireReserveShape(t, result, true, 0, "private", 1, false)
	}
	pending, terminal := rateAttemptCounts(t, db)
	if pending != 10 || terminal != 0 {
		t.Fatalf("pending=%d terminal=%d", pending, terminal)
	}

	denied, err := callRateReserve(t, db, "aboutme_app", rateReserveExpression(rateUUID("aaaaaaaa", 11), client))
	if err != nil {
		t.Fatal(err)
	}
	requireReserveShape(t, denied, false, 900, "", 0, false)
	if pending, terminal = rateAttemptCounts(t, db); pending != 10 || terminal != 0 {
		t.Fatalf("denial added debt pending=%d terminal=%d", pending, terminal)
	}
	if bucket = rateBucket(t, db, ratePolicyAttempt, digest); bucket.count.Int32 != 0 {
		t.Fatalf("denial charged the bucket=%+v", bucket)
	}
	if rateAttempt(t, db, rateUUID("aaaaaaaa", 11)).exists {
		t.Fatal("denied reserve inserted an attempt")
	}

	replay, err := callRateReserve(t, db, "aboutme_app", rateReserveExpression(rateUUID("aaaaaaaa", 1), client))
	if err != nil {
		t.Fatal(err)
	}
	requireReserveShape(t, replay, true, 0, "private", 1, true)
	if pending, terminal = rateAttemptCounts(t, db); pending != 10 || terminal != 0 {
		t.Fatalf("replay consumed a slot pending=%d terminal=%d", pending, terminal)
	}

	_, err = callRateReserve(t, db, "aboutme_app", rateReserveExpression(rateUUID("aaaaaaaa", 1), rateUUID("cccccccc", 2)))
	requireRateOperationError(t, err, "AM002")
}

// TestRuntimeSharedRateOperationsFailedGrantFinishMatrix proves failure
// conversion, neutral release, private success clearing, overflow success,
// caller replay, outcome conflict, system no-op and absent no-op.
func TestRuntimeSharedRateOperationsFailedGrantFinishMatrix(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	setRateClock(t, db, rateFutureClock)
	enableRatePartitions(t, db, ratePolicyAttempt, 1)
	client := rateUUID("cccccccc", 3)
	digest := goRateClientDigest(t, client)
	reserve := func(index int) {
		result, err := callRateReserve(t, db, "aboutme_app", rateReserveExpression(rateUUID("bbbbbbbb", index), client))
		if err != nil {
			t.Fatal(err)
		}
		requireReserveShape(t, result, true, 0, "private", 1, false)
	}
	finish := func(index int, outcome, resolution, stored string) {
		t.Helper()
		result, err := callRateFinish(t, db, "aboutme_app", rateFinishExpression(rateUUID("bbbbbbbb", index), outcome))
		if err != nil {
			t.Fatal(err)
		}
		if result.resolution != resolution || result.storedOutcome.String != stored || result.storedOutcome.Valid != (stored != "") {
			t.Fatalf("finish %d %s=%+v want %s/%s", index, outcome, result, resolution, stored)
		}
	}
	reserve(1)
	reserve(2)
	reserve(3)
	finish(1, "failure", "caller_finished", "failure")
	if bucket := rateBucket(t, db, ratePolicyAttempt, digest); bucket.count.Int32 != 1 {
		t.Fatalf("failure conversion count=%d", bucket.count.Int32)
	}
	finish(1, "failure", "caller_replay", "failure")
	_, err := callRateFinish(t, db, "aboutme_app", rateFinishExpression(rateUUID("bbbbbbbb", 1), "neutral"))
	requireRateOperationError(t, err, "AM002")

	finish(2, "neutral", "caller_finished", "neutral")
	if bucket := rateBucket(t, db, ratePolicyAttempt, digest); bucket.count.Int32 != 1 {
		t.Fatalf("neutral release changed debt=%+v", bucket)
	}

	reserve(4)
	finish(3, "success", "caller_finished", "success")
	if rateBucket(t, db, ratePolicyAttempt, digest).exists {
		t.Fatal("private success left the bucket")
	}
	if ratePartition(t, db, ratePolicyAttempt, 1).activeKeys != 0 {
		t.Fatal("private success did not decrement active keys")
	}
	sibling := rateAttempt(t, db, rateUUID("bbbbbbbb", 4))
	if sibling.state != "terminal" || sibling.outcome.String != "neutral" || sibling.terminalReason.String != "private_success_clear" {
		t.Fatalf("sibling=%+v", sibling)
	}
	finish(4, "failure", "system_noop", "neutral")
	finish(4, "success", "system_noop", "neutral")

	replay, err := callRateReserve(t, db, "aboutme_app", rateReserveExpression(rateUUID("bbbbbbbb", 3), client))
	if err != nil {
		t.Fatal(err)
	}
	requireReserveShape(t, replay, true, 0, "private", 1, true)

	absent, err := callRateFinish(t, db, "aboutme_app", rateFinishExpression(rateUUID("bbbbbbbb", 90), "success"))
	if err != nil {
		t.Fatal(err)
	}
	if absent.resolution != "absent_noop" || absent.storedOutcome.Valid {
		t.Fatalf("absent finish=%+v", absent)
	}

	t.Run("empty_window_restoration", func(t *testing.T) {
		lone := rateUUID("bbbbbbbb", 20)
		if _, reserveErr := callRateReserve(t, db, "aboutme_app", rateReserveExpression(lone, client)); reserveErr != nil {
			t.Fatal(reserveErr)
		}
		if bucket := rateBucket(t, db, ratePolicyAttempt, digest); !bucket.startedAt.Valid || bucket.count.Int32 != 0 {
			t.Fatalf("restored window bucket=%+v", bucket)
		}
		finish(20, "neutral", "caller_finished", "neutral")
		if bucket := rateBucket(t, db, ratePolicyAttempt, digest); bucket.startedAt.Valid || bucket.count.Int32 != 0 {
			t.Fatalf("empty window not restored=%+v", bucket)
		}
	})
}

// TestRuntimeSharedRateOperationsFailedGrantOverflowAndExpiry proves overflow
// success preserves shared debt, expiry resolves only the selected bucket and
// an unrelated expired reservation stays bound at rest.
func TestRuntimeSharedRateOperationsFailedGrantOverflowAndExpiry(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	setRateClock(t, db, rateFutureClock)
	overflowClient := rateUUID("cccccccc", 4)
	for index := 1; index <= 3; index++ {
		result, err := callRateReserve(t, db, "aboutme_app", rateReserveExpression(rateUUID("dddddddd", index), overflowClient))
		if err != nil {
			t.Fatal(err)
		}
		requireReserveShape(t, result, true, 0, "overflow", 0, false)
	}
	if _, err := callRateFinish(t, db, "aboutme_app", rateFinishExpression(rateUUID("dddddddd", 1), "failure")); err != nil {
		t.Fatal(err)
	}
	if overflow := rateOverflow(t, db, ratePolicyAttempt); overflow.count.Int32 != 1 {
		t.Fatalf("overflow failure count=%d", overflow.count.Int32)
	}
	if _, err := callRateFinish(t, db, "aboutme_app", rateFinishExpression(rateUUID("dddddddd", 2), "success")); err != nil {
		t.Fatal(err)
	}
	overflow := rateOverflow(t, db, ratePolicyAttempt)
	if overflow.count.Int32 != 1 || !overflow.startedAt.Valid {
		t.Fatalf("overflow success erased shared debt=%+v", overflow)
	}
	if third := rateAttempt(t, db, rateUUID("dddddddd", 3)); third.state != "pending" {
		t.Fatalf("overflow success resolved a sibling=%+v", third)
	}

	enableRatePartitions(t, db, ratePolicyAttempt, 1)
	clientA, clientB := rateUUID("cccccccc", 5), rateUUID("cccccccc", 6)
	digestA, digestB := goRateClientDigest(t, clientA), goRateClientDigest(t, clientB)
	for _, pair := range [][2]string{{rateUUID("eeeeeeee", 1), clientA}, {rateUUID("eeeeeeee", 2), clientB}} {
		if _, err := callRateReserve(t, db, "aboutme_app", rateReserveExpression(pair[0], pair[1])); err != nil {
			t.Fatal(err)
		}
	}
	setRateClock(t, db, "2030-01-01 00:15:00+00")
	selected, err := callRateReserve(t, db, "aboutme_app", rateReserveExpression(rateUUID("eeeeeeee", 3), clientA))
	if err != nil {
		t.Fatal(err)
	}
	requireReserveShape(t, selected, true, 0, "private", 1, false)
	resolvedA := rateAttempt(t, db, rateUUID("eeeeeeee", 1))
	if resolvedA.state != "terminal" || resolvedA.outcome.String != "neutral" || resolvedA.terminalReason.String != "window_expired" {
		t.Fatalf("selected expiry did not resolve A=%+v", resolvedA)
	}
	unresolvedB := rateAttempt(t, db, rateUUID("eeeeeeee", 2))
	if unresolvedB.state != "pending" {
		t.Fatalf("unrelated key B was swept=%+v", unresolvedB)
	}
	if bucketB := rateBucket(t, db, ratePolicyAttempt, digestB); !bucketB.startedAt.Valid {
		t.Fatalf("unrelated key B lost its window identity=%+v", bucketB)
	}
	if bucketA := rateBucket(t, db, ratePolicyAttempt, digestA); !bucketA.startedAt.Time.Equal(rateTime(t, "2030-01-01 00:15:00+00")) {
		t.Fatalf("selected key A did not start a fresh window=%+v", bucketA)
	}
	expiredFinish, err := callRateFinish(t, db, "aboutme_app", rateFinishExpression(rateUUID("eeeeeeee", 2), "failure"))
	if err != nil {
		t.Fatal(err)
	}
	if expiredFinish.resolution != "system_noop" || expiredFinish.storedOutcome.String != "neutral" {
		t.Fatalf("expired finish=%+v", expiredFinish)
	}
	if bucketB := rateBucket(t, db, ratePolicyAttempt, digestB); bucketB.count.Int32 != 0 || bucketB.startedAt.Valid {
		t.Fatalf("expired finish charged a later window=%+v", bucketB)
	}
}

// rateSeedTerminalReceipts inserts count complete terminal P22 receipts whose
// terminal_at is age old.
func rateSeedTerminalReceipts(t *testing.T, db *sql.DB, prefix string, count int, age string) {
	t.Helper()
	statement := fmt.Sprintf(`INSERT INTO public.shared_admission_attempts(attempt_id,policy_id,client_id,bucket_kind,partition,key_digest,window_started_at,reserved_at,effective_until,state,outcome,terminal_reason,terminal_at)
 SELECT ('%s-0000-4000-8000-'||lpad(g::text,12,'0'))::uuid,'oauth.failed_grant','aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa','overflow',NULL,NULL,
  transaction_timestamp()-interval '40 hours',transaction_timestamp()-interval '40 hours',transaction_timestamp()-interval '40 hours'+interval '15 minutes',
  'terminal','failure','caller_finish',transaction_timestamp()-interval '%s' FROM generate_series(1,%d) g`, prefix, age, count)
	if err := membershipWrite(t, db, statement); err != nil {
		t.Fatal(err)
	}
}

// TestRuntimeSharedRateOperationsReceiptCleanupBounds proves the page bounds,
// the 24-hour terminal cutoff, pending exclusion and the zero no-op row.
func TestRuntimeSharedRateOperationsReceiptCleanupBounds(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	for _, page := range []string{"0", "257", "-1", "NULL"} {
		_, err := callRateReceiptCleanup(t, db, "aboutme_maintenance", fmt.Sprintf(`public.runtime_cleanup_admission_attempt_receipts(%s)`, page))
		requireRateOperationError(t, err, "22023")
	}
	empty, err := callRateReceiptCleanup(t, db, "aboutme_maintenance", rateReceiptCleanupExpression(256))
	if err != nil || empty != 0 {
		t.Fatalf("empty cleanup=%d error=%v", empty, err)
	}
	enableRatePartitions(t, db, ratePolicyAttempt, 1)
	live := rateUUID("99999999", 1)
	if _, reserveErr := callRateReserve(t, db, "aboutme_app", rateReserveExpression(live, rateUUID("cccccccc", 9))); reserveErr != nil {
		t.Fatal(reserveErr)
	}
	rateSeedTerminalReceipts(t, db, "aaaa1111", 3, "25 hours")
	rateSeedTerminalReceipts(t, db, "bbbb2222", 2, "23 hours")
	rateSeedTerminalReceipts(t, db, "cccc3333", 1, "24 hours 10 seconds")
	partitionBefore := ratePartition(t, db, ratePolicyAttempt, 1)
	deleted, err := callRateReceiptCleanup(t, db, "aboutme_maintenance", rateReceiptCleanupExpression(1))
	if err != nil || deleted != 1 {
		t.Fatalf("page one=%d error=%v", deleted, err)
	}
	if deleted, err = callRateReceiptCleanup(t, db, "aboutme_maintenance", rateReceiptCleanupExpression(256)); err != nil || deleted != 3 {
		t.Fatalf("page 256=%d error=%v", deleted, err)
	}
	if deleted, err = callRateReceiptCleanup(t, db, "aboutme_maintenance", rateReceiptCleanupExpression(256)); err != nil || deleted != 0 {
		t.Fatalf("exhausted cleanup=%d error=%v", deleted, err)
	}
	pending, terminal := rateAttemptCounts(t, db)
	if pending != 1 || terminal != 2 {
		t.Fatalf("retained pending=%d terminal=%d want 1/2", pending, terminal)
	}
	if !rateAttempt(t, db, live).exists {
		t.Fatal("receipt cleanup deleted a pending reservation")
	}
	if after := ratePartition(t, db, ratePolicyAttempt, 1); after != partitionBefore {
		t.Fatalf("receipt cleanup touched a partition before=%+v after=%+v", partitionBefore, after)
	}
	if rateBucketCount(t, db, ratePolicyAttempt) != 1 {
		t.Fatal("receipt cleanup touched a bucket")
	}
	rateSeedTerminalReceipts(t, db, "dddd4444", 257, "25 hours")
	for _, want := range []int{256, 1, 0} {
		if deleted, err = callRateReceiptCleanup(t, db, "aboutme_maintenance", rateReceiptCleanupExpression(256)); err != nil || deleted != want {
			t.Fatalf("batch cleanup=%d want=%d error=%v", deleted, want, err)
		}
	}
}

// TestRuntimeSharedRateOperationsRateCleanupBounds proves the page bounds,
// exact eligibility, the count decrement and the locked policy_idle predicate.
func TestRuntimeSharedRateOperationsRateCleanupBounds(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	const policy, full = ratePolicyAlloc, 72000000000
	for _, page := range []string{"0", "257", "NULL"} {
		_, err := callRateCleanup(t, db, "aboutme_maintenance", fmt.Sprintf(`public.runtime_cleanup_rate_buckets('%s',%s)`, policy, page))
		requireRateOperationError(t, err, "22023")
	}
	_, err := callRateCleanup(t, db, "aboutme_maintenance", `public.runtime_cleanup_rate_buckets(NULL,1)`)
	requireRateOperationError(t, err, "22023")
	_, err = callRateCleanup(t, db, "aboutme_maintenance", rateCleanupExpression("no.such.policy", 1))
	requireRateOperationError(t, err, "55000")

	idle, err := callRateCleanup(t, db, "aboutme_maintenance", rateCleanupExpression(policy, 1))
	if err != nil || idle.deleted != 0 || !idle.policyIdle {
		t.Fatalf("seeded policy cleanup=%+v error=%v", idle, err)
	}
	for index, numerator := range []string{fmt.Sprint(full), fmt.Sprint(full), fmt.Sprint(full), "0"} {
		digest := rateByteDigest(fmt.Sprintf("%02x", 0xc0+index))
		rateSeedBucket(t, db, policy, digest[:2], 1, map[string]string{
			"key_digest": fmt.Sprintf("decode('%s','hex')", digest), "token_numerator": numerator,
		})
	}
	if ratePartition(t, db, policy, 1).activeKeys != 4 {
		t.Fatal("fixture active keys")
	}
	page, err := callRateCleanup(t, db, "aboutme_maintenance", rateCleanupExpression(policy, 1))
	if err != nil || page.deleted != 1 || page.policyIdle {
		t.Fatalf("page one=%+v error=%v", page, err)
	}
	if ratePartition(t, db, policy, 1).activeKeys != 3 {
		t.Fatalf("page one active keys=%d", ratePartition(t, db, policy, 1).activeKeys)
	}
	if page, err = callRateCleanup(t, db, "aboutme_maintenance", rateCleanupExpression(policy, 256)); err != nil || page.deleted != 2 || page.policyIdle {
		t.Fatalf("full page=%+v error=%v", page, err)
	}
	if page, err = callRateCleanup(t, db, "aboutme_maintenance", rateCleanupExpression(policy, 256)); err != nil || page.deleted != 0 || page.policyIdle {
		t.Fatalf("debt-carrying row=%+v error=%v", page, err)
	}
	if rateBucketCount(t, db, policy) != 1 || ratePartition(t, db, policy, 1).activeKeys != 1 {
		t.Fatal("cleanup deleted a debt-carrying row")
	}
	lastDigest := rateByteDigest(fmt.Sprintf("%02x", 0xc3))
	if err = membershipWrite(t, db, fmt.Sprintf(`UPDATE public.shared_rate_buckets SET last_seen=transaction_timestamp()-interval '48 hours' WHERE policy_id='%s' AND key_digest=decode('%s','hex')`, policy, lastDigest)); err != nil {
		t.Fatal(err)
	}
	if page, err = callRateCleanup(t, db, "aboutme_maintenance", rateCleanupExpression(policy, 256)); err != nil || page.deleted != 1 || !page.policyIdle {
		t.Fatalf("idle backstop=%+v error=%v", page, err)
	}
	if rateBucketCount(t, db, policy) != 0 || ratePartition(t, db, policy, 1).activeKeys != 0 {
		t.Fatal("idle backstop left rows")
	}

	t.Run("overflow_debt_blocks_idle_until_it_matures", func(t *testing.T) {
		decision, admitErr := callRateDecision(t, db, "aboutme_app", rateTokenExpression(policy, rateDigest(31)))
		if admitErr != nil {
			t.Fatal(admitErr)
		}
		if decision.kind != "overflow" {
			t.Fatalf("overflow admission=%+v", decision)
		}
		result, cleanupErr := callRateCleanup(t, db, "aboutme_maintenance", rateCleanupExpression(policy, 1))
		if cleanupErr != nil || result.policyIdle {
			t.Fatalf("overflow debt idle=%+v error=%v", result, cleanupErr)
		}
		if writeErr := membershipWrite(t, db, fmt.Sprintf(`UPDATE public.shared_rate_overflow SET refill_at=refill_at-interval '2 hours' WHERE policy_id='%s'`, policy)); writeErr != nil {
			t.Fatal(writeErr)
		}
		if result, cleanupErr = callRateCleanup(t, db, "aboutme_maintenance", rateCleanupExpression(policy, 1)); cleanupErr != nil || !result.policyIdle {
			t.Fatalf("matured overflow idle=%+v error=%v", result, cleanupErr)
		}
		if overflow := rateOverflow(t, db, policy); overflow.numerator.Int64 != full {
			t.Fatalf("overflow refill=%d want=%d", overflow.numerator.Int64, full)
		}
	})

	t.Run("attempt_pending_blocks_idle_and_terminal_does_not", func(t *testing.T) {
		enableRatePartitions(t, db, ratePolicyAttempt, 1)
		attempt := rateUUID("77777777", 1)
		if _, reserveErr := callRateReserve(t, db, "aboutme_app", rateReserveExpression(attempt, rateUUID("cccccccc", 21))); reserveErr != nil {
			t.Fatal(reserveErr)
		}
		result, cleanupErr := callRateCleanup(t, db, "aboutme_maintenance", rateCleanupExpression(ratePolicyAttempt, 256))
		if cleanupErr != nil || result.policyIdle || result.deleted != 0 {
			t.Fatalf("pending debt=%+v error=%v", result, cleanupErr)
		}
		if _, finishErr := callRateFinish(t, db, "aboutme_app", rateFinishExpression(attempt, "neutral")); finishErr != nil {
			t.Fatal(finishErr)
		}
		if result, cleanupErr = callRateCleanup(t, db, "aboutme_maintenance", rateCleanupExpression(ratePolicyAttempt, 256)); cleanupErr != nil || result.deleted != 1 || !result.policyIdle {
			t.Fatalf("terminal receipt blocked idle=%+v error=%v", result, cleanupErr)
		}
		if _, terminal := rateAttemptCounts(t, db); terminal == 0 {
			t.Fatal("rate cleanup deleted a terminal receipt")
		}
	})

	t.Run("rolling_overflow_prunes_only_the_accepted_cutoff", func(t *testing.T) {
		if writeErr := membershipWrite(t, db, fmt.Sprintf(`UPDATE public.shared_rate_overflow SET rolling_events=ARRAY[transaction_timestamp()-interval '2 hours',transaction_timestamp()-interval '30 minutes'],count=2 WHERE policy_id='%s'`, ratePolicySlug)); writeErr != nil {
			t.Fatal(writeErr)
		}
		result, cleanupErr := callRateCleanup(t, db, "aboutme_maintenance", rateCleanupExpression(ratePolicySlug, 256))
		if cleanupErr != nil || result.policyIdle {
			t.Fatalf("rolling overflow debt=%+v error=%v", result, cleanupErr)
		}
		overflow := rateOverflow(t, db, ratePolicySlug)
		if overflow.eventCount.Int32 != 1 || overflow.count.Int32 != 1 {
			t.Fatalf("rolling overflow pruning=%+v", overflow)
		}
	})

	t.Run("fixed_window_overflow_clears_only_after_expiry", func(t *testing.T) {
		if writeErr := membershipWrite(t, db, fmt.Sprintf(`UPDATE public.shared_rate_overflow SET count=4,window_started_at=transaction_timestamp()-interval '10 minutes' WHERE policy_id='%s'`, ratePolicyFailure)); writeErr != nil {
			t.Fatal(writeErr)
		}
		result, cleanupErr := callRateCleanup(t, db, "aboutme_maintenance", rateCleanupExpression(ratePolicyFailure, 256))
		if cleanupErr != nil || result.policyIdle {
			t.Fatalf("live fixed window overflow=%+v error=%v", result, cleanupErr)
		}
		if overflow := rateOverflow(t, db, ratePolicyFailure); overflow.count.Int32 != 4 {
			t.Fatalf("cleanup erased a live fixed window=%+v", overflow)
		}
		if writeErr := membershipWrite(t, db, fmt.Sprintf(`UPDATE public.shared_rate_overflow SET window_started_at=transaction_timestamp()-interval '16 minutes' WHERE policy_id='%s'`, ratePolicyFailure)); writeErr != nil {
			t.Fatal(writeErr)
		}
		if result, cleanupErr = callRateCleanup(t, db, "aboutme_maintenance", rateCleanupExpression(ratePolicyFailure, 256)); cleanupErr != nil || !result.policyIdle {
			t.Fatalf("expired fixed window overflow=%+v error=%v", result, cleanupErr)
		}
		if overflow := rateOverflow(t, db, ratePolicyFailure); overflow.count.Int32 != 0 || overflow.startedAt.Valid {
			t.Fatalf("expired overflow normalization=%+v", overflow)
		}
	})
}

// TestRuntimeSharedRateOperationsCatalogOwnersAndGrants proves every object is
// owner-owned, security definer, pg_catalog-scoped and carries exactly the
// accepted grant matrix.
func TestRuntimeSharedRateOperationsCatalogOwnersAndGrants(t *testing.T) {
	db, ctx := runtimeSharedRateDB(t)
	names := append([]string(nil), rateHelperNames...)
	for _, operation := range rateOperationSignatures {
		names = append(names, operation.name)
	}
	var valid bool
	if err := db.QueryRowContext(ctx, `SELECT count(*)=$2 AND bool_and(pg_get_userbyid(proowner)='aboutme_runtime_owner' AND prosecdef AND proconfig=ARRAY['search_path=pg_catalog']::text[] AND NOT has_function_privilege('public',oid,'EXECUTE') AND NOT proisstrict) FROM pg_proc WHERE pronamespace='public'::regnamespace AND proname=ANY($1)`, names, len(names)).Scan(&valid); err != nil || !valid {
		t.Fatalf("catalog valid=%t error=%v", valid, err)
	}
	appTypes := map[string]bool{
		"runtime_rate_decision_result": true, "runtime_password_failure_result": true, "runtime_password_clear_result": true,
		"runtime_failed_grant_reserve_result": true, "runtime_admission_attempt_finish_result": true,
	}
	maintenanceTypes := map[string]bool{"runtime_admission_receipt_cleanup_result": true, "runtime_rate_cleanup_result": true}
	for _, composite := range rateResultComposites {
		var public, app, maintenance bool
		var owner string
		if err := db.QueryRowContext(ctx, `SELECT has_type_privilege('public','public.'||$1,'USAGE'),has_type_privilege('aboutme_app','public.'||$1,'USAGE'),has_type_privilege('aboutme_maintenance','public.'||$1,'USAGE'),pg_get_userbyid((SELECT typowner FROM pg_type WHERE typnamespace='public'::regnamespace AND typname=$1))`, composite.name).Scan(&public, &app, &maintenance, &owner); err != nil {
			t.Fatal(err)
		}
		if public || owner != "aboutme_runtime_owner" || app != appTypes[composite.name] || maintenance != maintenanceTypes[composite.name] {
			t.Fatalf("type %s public=%t owner=%s app=%t maintenance=%t", composite.name, public, owner, app, maintenance)
		}
	}
	grants := map[string]map[string]bool{
		"aboutme_app": {
			"runtime_admit_token_rate": true, "runtime_password_failure_state": true, "runtime_record_password_failure": true,
			"runtime_clear_password_failure": true, "runtime_admit_slug_change": true,
			"runtime_reserve_oauth_failed_grant": true, "runtime_finish_admission_attempt": true,
		},
		"aboutme_maintenance": {
			"runtime_cleanup_admission_attempt_receipts": true, "runtime_cleanup_rate_buckets": true,
		},
		"aboutme_lifecycle_command": {}, "aboutme_fencing_proof": {}, "aboutme_restore_verify": {}, "aboutme_migrator": {},
	}
	for role, allowed := range grants {
		for _, operation := range rateOperationSignatures {
			var execute bool
			if err := db.QueryRowContext(ctx, `SELECT has_function_privilege($1,'public.'||$2,'EXECUTE')`, role, operation.name+operation.signature).Scan(&execute); err != nil || execute != allowed[operation.name] {
				t.Fatalf("%s %s execute=%t want=%t error=%v", role, operation.name, execute, allowed[operation.name], err)
			}
		}
		for _, helper := range rateHelperNames {
			var execute bool
			if err := db.QueryRowContext(ctx, `SELECT COALESCE(bool_or(has_function_privilege($1,oid,'EXECUTE')),false) FROM pg_proc WHERE pronamespace='public'::regnamespace AND proname=$2`, role, helper).Scan(&execute); err != nil || execute {
				t.Fatalf("%s helper %s execute=%t error=%v", role, helper, execute, err)
			}
		}
	}
}

// TestRuntimeSharedRateOperationsRoleMatrix proves the direct session_user
// check rejects every other login, SET ROLE, the owner and PUBLIC, and that no
// runtime login can execute or replace the sampling helper.
func TestRuntimeSharedRateOperationsRoleMatrix(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	admit := `SELECT public.runtime_admit_token_rate('` + ratePolicyToken + `',decode('` + rateDigest(41) + `','hex'))`
	cleanup := `SELECT public.runtime_cleanup_rate_buckets('` + ratePolicyToken + `',1)`
	receipts := `SELECT public.runtime_cleanup_admission_attempt_receipts(1)`
	for _, test := range []struct{ name, role, statement string }{
		{"app_rate_cleanup", "aboutme_app", cleanup},
		{"app_receipt_cleanup", "aboutme_app", receipts},
		{"maintenance_admit", "aboutme_maintenance", admit},
		{"maintenance_state", "aboutme_maintenance", `SELECT public.runtime_password_failure_state(decode('` + rateDigest(41) + `','hex'))`},
		{"maintenance_reserve", "aboutme_maintenance", `SELECT public.runtime_reserve_oauth_failed_grant('` + rateUUID("aaaaaaaa", 60) + `','` + rateUUID("cccccccc", 60) + `')`},
		{"lifecycle_admit", "aboutme_lifecycle_command", admit},
		{"lifecycle_cleanup", "aboutme_lifecycle_command", cleanup},
		{"proof_admit", "aboutme_fencing_proof", admit},
		{"restore_admit", "aboutme_restore_verify", admit},
		{"migrator_admit", "aboutme_migrator", admit},
		{"migrator_cleanup", "aboutme_migrator", cleanup},
		{"owner_admit", "aboutme_runtime_owner", admit},
		{"owner_cleanup", "aboutme_runtime_owner", cleanup},
		{"app_clock_helper", "aboutme_app", `SELECT public.runtime_sample_rate_time()`},
		{"maintenance_clock_helper", "aboutme_maintenance", `SELECT public.runtime_sample_rate_time()`},
		{"app_route_helper", "aboutme_app", `SELECT public.runtime_rate_sample('` + ratePolicyToken + `')`},
		{"app_client_digest_helper", "aboutme_app", `SELECT public.runtime_rate_client_digest('` + rateUUID("cccccccc", 60) + `')`},
		{"app_clock_replacement", "aboutme_app", `CREATE OR REPLACE FUNCTION public.runtime_sample_rate_time() RETURNS timestamptz LANGUAGE sql AS $x$ SELECT '2000-01-01'::timestamptz $x$`},
		{"maintenance_clock_replacement", "aboutme_maintenance", `CREATE OR REPLACE FUNCTION public.runtime_sample_rate_time() RETURNS timestamptz LANGUAGE sql AS $x$ SELECT '2000-01-01'::timestamptz $x$`},
		{"app_select_buckets", "aboutme_app", `SELECT count(*) FROM public.shared_rate_buckets`},
		{"app_update_partitions", "aboutme_app", `UPDATE public.shared_rate_partitions SET active_keys=0`},
		{"maintenance_delete_attempts", "aboutme_maintenance", `DELETE FROM public.shared_admission_attempts`},
		{"maintenance_update_clocks", "aboutme_maintenance", `UPDATE public.shared_policy_clocks SET anomaly_count=0`},
	} {
		t.Run(test.name, func(t *testing.T) {
			requireRateOperationError(t, registrationRoleExec(t, db, test.role, test.statement), "42501")
		})
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
		if _, err = conn.ExecContext(ctx, `SET ROLE aboutme_app`); err != nil {
			t.Fatal(err)
		}
		_, err = conn.ExecContext(ctx, admit)
		requireRateOperationError(t, err, "42501")
	})
	t.Run("clock_helper_body_is_pinned", func(t *testing.T) {
		var body string
		if err := db.QueryRowContext(context.Background(), `SELECT prosrc FROM pg_proc WHERE oid=to_regprocedure('public.runtime_sample_rate_time()')`).Scan(&body); err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(body) != "SELECT clock_timestamp()" {
			t.Fatalf("production clock body=%q", body)
		}
	})
}

// TestRuntimeSharedRateOperationsInputMatrixAndFixedErrors proves every fixed
// shape rejection, the caller policy error, the missing write entry and that
// no diagnostic discloses a private value.
func TestRuntimeSharedRateOperationsInputMatrixAndFixedErrors(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	digest := rateDigest(51)
	short := digest[:62]
	long := digest + "ff"
	attempt := rateUUID("aaaaaaaa", 70)
	client := rateUUID("cccccccc", 70)
	invalid := []struct{ name, role, expression, projection string }{
		{"token_null_policy", "aboutme_app", `public.runtime_admit_token_rate(NULL,decode('` + digest + `','hex'))`, rateDecisionProjection},
		{"token_null_digest", "aboutme_app", `public.runtime_admit_token_rate('` + ratePolicyToken + `',NULL)`, rateDecisionProjection},
		{"token_short_digest", "aboutme_app", rateTokenExpression(ratePolicyToken, short), rateDecisionProjection},
		{"token_long_digest", "aboutme_app", rateTokenExpression(ratePolicyToken, long), rateDecisionProjection},
		{"slug_null_digest", "aboutme_app", `public.runtime_admit_slug_change(NULL)`, rateDecisionProjection},
		{"slug_short_digest", "aboutme_app", rateSlugExpression(short), rateDecisionProjection},
		{"state_null_digest", "aboutme_app", `public.runtime_password_failure_state(NULL)`, rateFailureProjection},
		{"state_long_digest", "aboutme_app", rateStateExpression(long), rateFailureProjection},
		{"record_null_digest", "aboutme_app", `public.runtime_record_password_failure(NULL)`, rateFailureProjection},
		{"record_short_digest", "aboutme_app", rateRecordExpression(short), rateFailureProjection},
		{"clear_null_digest", "aboutme_app", `public.runtime_clear_password_failure(NULL)`, rateClearProjection},
		{"clear_short_digest", "aboutme_app", rateClearExpression(short), rateClearProjection},
		{"reserve_null_attempt", "aboutme_app", `public.runtime_reserve_oauth_failed_grant(NULL,'` + client + `')`, rateReserveProjection},
		{"reserve_null_client", "aboutme_app", `public.runtime_reserve_oauth_failed_grant('` + attempt + `',NULL)`, rateReserveProjection},
		{"reserve_nil_attempt", "aboutme_app", rateReserveExpression(rateNilUUID, client), rateReserveProjection},
		{"reserve_nil_client", "aboutme_app", rateReserveExpression(attempt, rateNilUUID), rateReserveProjection},
		{"finish_null_attempt", "aboutme_app", `public.runtime_finish_admission_attempt(NULL,'failure')`, rateFinishProjection},
		{"finish_nil_attempt", "aboutme_app", rateFinishExpression(rateNilUUID, "failure"), rateFinishProjection},
		{"finish_null_outcome", "aboutme_app", `public.runtime_finish_admission_attempt('` + attempt + `',NULL)`, rateFinishProjection},
		{"finish_unknown_outcome", "aboutme_app", rateFinishExpression(attempt, "abandoned"), rateFinishProjection},
		{"finish_empty_outcome", "aboutme_app", rateFinishExpression(attempt, ""), rateFinishProjection},
		{"receipts_zero_page", "aboutme_maintenance", rateReceiptCleanupExpression(0), rateReceiptProjection},
		{"receipts_over_page", "aboutme_maintenance", rateReceiptCleanupExpression(257), rateReceiptProjection},
		{"cleanup_zero_page", "aboutme_maintenance", rateCleanupExpression(ratePolicyToken, 0), rateCleanupProjection},
		{"cleanup_over_page", "aboutme_maintenance", rateCleanupExpression(ratePolicyToken, 257), rateCleanupProjection},
		{"cleanup_null_policy", "aboutme_maintenance", `public.runtime_cleanup_rate_buckets(NULL,1)`, rateCleanupProjection},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			var sink [8]any
			pgErr := rateOperationErrorOf(t, rateCall(t, db, test.role, test.expression, test.projection, sinkTargets(&sink, test.projection)...), "22023")
			requireNoRateIdentityLeak(t, pgErr, digest[:40], attempt, client)
		})
	}
	for _, test := range []struct{ name, expression string }{
		{"unknown_policy", rateTokenExpression("no.such.policy", digest)},
		{"fixed_window_policy", rateTokenExpression(ratePolicyFailure, digest)},
		{"rolling_policy", rateTokenExpression(ratePolicySlug, digest)},
		{"attempt_policy", rateTokenExpression(ratePolicyAttempt, digest)},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := callRateDecision(t, db, "aboutme_app", test.expression)
			pgErr := rateOperationErrorOf(t, err, "55000")
			requireNoRateIdentityLeak(t, pgErr, digest[:40])
		})
	}
	if rateBucketCount(t, db, ratePolicyToken) != 0 {
		t.Fatal("invalid input changed rows")
	}
	for _, test := range []struct{ name, role, statement string }{
		{"admit", "aboutme_app", `SELECT public.runtime_admit_token_rate('` + ratePolicyToken + `',decode('` + digest + `','hex'))`},
		{"record", "aboutme_app", `SELECT public.runtime_record_password_failure(decode('` + digest + `','hex'))`},
		{"clear", "aboutme_app", `SELECT public.runtime_clear_password_failure(decode('` + digest + `','hex'))`},
		{"reserve", "aboutme_app", `SELECT public.runtime_reserve_oauth_failed_grant('` + attempt + `','` + client + `')`},
		{"finish", "aboutme_app", `SELECT public.runtime_finish_admission_attempt('` + attempt + `','neutral')`},
		{"receipts", "aboutme_maintenance", `SELECT public.runtime_cleanup_admission_attempt_receipts(1)`},
		{"buckets", "aboutme_maintenance", `SELECT public.runtime_cleanup_rate_buckets('` + ratePolicyToken + `',1)`},
	} {
		t.Run("missing_write_entry_"+test.name, func(t *testing.T) {
			requireRateOperationError(t, registrationRoleExec(t, db, test.role, test.statement), "AM001")
		})
	}
}

// sinkTargets returns discard destinations matching a projection's arity.
func sinkTargets(sink *[8]any, projection string) []any {
	count := strings.Count(projection, ",") + 1
	targets := make([]any, 0, count)
	for i := 0; i < count; i++ {
		sink[i] = new(any)
		targets = append(targets, sink[i])
	}
	return targets
}

type rateHold struct {
	conn *sql.Conn
	pid  int
}

func (h rateHold) commit() error   { return finishRegistrationTx(h.conn) }
func (h rateHold) rollback() error { return rollbackRegistrationTx(h.conn) }

// openRateHolder runs one operation inside an open write transaction and keeps
// its locks until the caller settles it.
func openRateHolder(t *testing.T, db *sql.DB, role, expression, projection string) rateHold {
	t.Helper()
	conn, pid := openRegistrationTx(t, db, role)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var sink [8]any
	if err := conn.QueryRowContext(ctx, rateStatement(expression, projection)).Scan(sinkTargets(&sink, projection)...); err != nil {
		if rollbackErr := rollbackRegistrationTx(conn); rollbackErr != nil {
			t.Error(rollbackErr)
		}
		t.Fatal(err)
	}
	return rateHold{conn: conn, pid: pid}
}

// raceRateOperation proves the waiter blocks on a database lock, settles the
// holder, then joins and commits the waiter.
func raceRateOperation(t *testing.T, db *sql.DB, settle func() error, waiterRole, expression, projection string, dest ...any) error {
	t.Helper()
	waiter, waiterPID := openRegistrationTx(t, db, waiterRole)
	raceCtx, cancelRace := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancelRace()
	done := make(chan error, 1)
	go func() {
		done <- waiter.QueryRowContext(raceCtx, rateStatement(expression, projection)).Scan(dest...)
	}()
	waitErr := waitForTransitionCondition(db, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND NOT granted)`, waiterPID)
	settleErr := settle()
	var queryErr error
	select {
	case queryErr = <-done:
	case <-time.After(15 * time.Second):
		cancelRace()
		queryErr = <-done
		if rollbackErr := rollbackRegistrationTx(waiter); rollbackErr != nil {
			t.Error(rollbackErr)
		}
		t.Fatalf("timed out joining rate race: %v", queryErr)
	}
	if waitErr != nil || settleErr != nil {
		if rollbackErr := rollbackRegistrationTx(waiter); rollbackErr != nil {
			t.Error(rollbackErr)
		}
		t.Fatalf("race setup wait=%v settle=%v query=%v", waitErr, settleErr, queryErr)
	}
	if queryErr != nil {
		if rollbackErr := rollbackRegistrationTx(waiter); rollbackErr != nil {
			t.Error(rollbackErr)
		}
		return queryErr
	}
	if err := finishRegistrationTx(waiter); err != nil {
		t.Fatal(err)
	}
	return nil
}

// TestRuntimeSharedRateOperationsRacesSerialize proves same-policy work
// serializes on the policy clock and bucket rows, that allocation, clearing,
// cleanup, P22 outcomes and partition toggles settle deterministically, and
// that independent policies never serialize on a rate lock.
func TestRuntimeSharedRateOperationsRacesSerialize(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	const policy, full, window = ratePolicyAlloc, 72000000000, 3600000000
	setRateClock(t, db, rateFutureClock)
	enableRatePartitions(t, db, policy, 1)
	enableRatePartitions(t, db, ratePolicyFailure, 1)
	enableRatePartitions(t, db, ratePolicyAttempt, 1)

	t.Run("same_key_admissions_serialize", func(t *testing.T) {
		digest := rateDigest(61)
		if _, err := callRateDecision(t, db, "aboutme_app", rateTokenExpression(policy, digest)); err != nil {
			t.Fatal(err)
		}
		holder := openRateHolder(t, db, "aboutme_app", rateTokenExpression(policy, digest), rateDecisionProjection)
		var result rateDecisionResult
		if err := raceRateOperation(t, db, holder.commit, "aboutme_app", rateTokenExpression(policy, digest), rateDecisionProjection,
			&result.allowed, &result.retry, &result.kind, &result.partition, &result.effectiveAt); err != nil {
			t.Fatal(err)
		}
		if !result.allowed || result.kind != "private" {
			t.Fatalf("serialized admission=%+v", result)
		}
		if state := rateBucket(t, db, policy, digest); state.numerator.Int64 != full-3*window {
			t.Fatalf("serialized numerator=%d want=%d", state.numerator.Int64, full-3*window)
		}
	})

	t.Run("independent_policies_do_not_serialize", func(t *testing.T) {
		holder := openRateHolder(t, db, "aboutme_app", rateTokenExpression(policy, rateDigest(62)), rateDecisionProjection)
		other, err := callRateDecision(t, db, "aboutme_app", rateTokenExpression(ratePolicyToken, rateDigest(62)))
		if err != nil {
			if rollbackErr := holder.rollback(); rollbackErr != nil {
				t.Error(rollbackErr)
			}
			t.Fatalf("independent policy blocked: %v", err)
		}
		if !other.allowed {
			t.Fatalf("independent policy result=%+v", other)
		}
		if err = holder.commit(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("allocation_then_clear", func(t *testing.T) {
		digest := rateDigest(63)
		holder := openRateHolder(t, db, "aboutme_app", rateRecordExpression(digest), rateFailureProjection)
		var result rateClearResult
		if err := raceRateOperation(t, db, holder.commit, "aboutme_app", rateClearExpression(digest), rateClearProjection,
			&result.cleared, &result.kind, &result.partition); err != nil {
			t.Fatal(err)
		}
		if !result.cleared || result.kind != "private" || result.partition.Int16 != 1 {
			t.Fatalf("clear after allocation=%+v", result)
		}
		if rateBucket(t, db, ratePolicyFailure, digest).exists || ratePartition(t, db, ratePolicyFailure, 1).activeKeys != 0 {
			t.Fatal("racing clear left rows or counts")
		}
	})

	t.Run("cleanup_then_allocation", func(t *testing.T) {
		stale := rateByteDigest("e1")
		rateSeedBucket(t, db, policy, "e1", 1, map[string]string{"token_numerator": fmt.Sprint(full)})
		holder := openRateHolder(t, db, "aboutme_maintenance", rateCleanupExpression(policy, 1), rateCleanupProjection)
		var result rateDecisionResult
		if err := raceRateOperation(t, db, holder.commit, "aboutme_app", rateTokenExpression(policy, rateDigest(64)), rateDecisionProjection,
			&result.allowed, &result.retry, &result.kind, &result.partition, &result.effectiveAt); err != nil {
			t.Fatal(err)
		}
		if !result.allowed || result.kind != "private" {
			t.Fatalf("allocation after cleanup=%+v", result)
		}
		if rateBucket(t, db, policy, stale).exists {
			t.Fatal("cleanup did not commit")
		}
		var rows, active int
		if err := db.QueryRowContext(context.Background(), `SELECT (SELECT count(*) FROM public.shared_rate_buckets WHERE policy_id=$1 AND partition=1),(SELECT active_keys FROM public.shared_rate_partitions WHERE policy_id=$1 AND partition=1)`, policy).Scan(&rows, &active); err != nil || rows != active {
			t.Fatalf("count drift rows=%d active=%d error=%v", rows, active, err)
		}
	})

	t.Run("attempt_reserve_boundary_serializes", func(t *testing.T) {
		client := rateUUID("cccccccc", 80)
		for index := 1; index <= 9; index++ {
			if _, err := callRateReserve(t, db, "aboutme_app", rateReserveExpression(rateUUID("abababab", index), client)); err != nil {
				t.Fatal(err)
			}
		}
		holder := openRateHolder(t, db, "aboutme_app", rateReserveExpression(rateUUID("abababab", 10), client), rateReserveProjection)
		var result rateReserveResult
		if err := raceRateOperation(t, db, holder.commit, "aboutme_app", rateReserveExpression(rateUUID("abababab", 11), client), rateReserveProjection,
			&result.allowed, &result.retry, &result.kind, &result.partition, &result.replayed); err != nil {
			t.Fatal(err)
		}
		requireReserveShape(t, result, false, result.retry, "", 0, false)
		if result.retry < 1 {
			t.Fatalf("losing reserve retry=%d", result.retry)
		}
		pending, _ := rateAttemptCounts(t, db)
		if pending != 10 {
			t.Fatalf("boundary pending=%d want=10", pending)
		}
	})

	t.Run("attempt_success_then_finish", func(t *testing.T) {
		client := rateUUID("cccccccc", 81)
		first, second := rateUUID("acacacac", 1), rateUUID("acacacac", 2)
		for _, attempt := range []string{first, second} {
			if _, err := callRateReserve(t, db, "aboutme_app", rateReserveExpression(attempt, client)); err != nil {
				t.Fatal(err)
			}
		}
		holder := openRateHolder(t, db, "aboutme_app", rateFinishExpression(first, "success"), rateFinishProjection)
		var result rateFinishResult
		if err := raceRateOperation(t, db, holder.commit, "aboutme_app", rateFinishExpression(second, "failure"), rateFinishProjection,
			&result.resolution, &result.storedOutcome); err != nil {
			t.Fatal(err)
		}
		if result.resolution != "system_noop" || result.storedOutcome.String != "neutral" {
			t.Fatalf("losing finish=%+v", result)
		}
		if rateBucket(t, db, ratePolicyAttempt, goRateClientDigest(t, client)).exists {
			t.Fatal("private success left the bucket after the race")
		}
	})

	t.Run("partition_toggle_then_allocation", func(t *testing.T) {
		disableRatePartitions(t, db, ratePolicyToken, 1, 2)
		_, tx := beginTransitionWrite(t, db)
		toggleCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := tx.ExecContext(toggleCtx, `UPDATE public.shared_rate_partitions SET enabled=true,updated_at=clock_timestamp() WHERE policy_id='`+ratePolicyToken+`' AND partition=2`); err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				t.Error(rollbackErr)
			}
			t.Fatal(err)
		}
		var result rateDecisionResult
		if err := raceRateOperation(t, db, func() error { return finishTransitionWrite(tx) }, "aboutme_app", rateTokenExpression(ratePolicyToken, rateDigest(65)), rateDecisionProjection,
			&result.allowed, &result.retry, &result.kind, &result.partition, &result.effectiveAt); err != nil {
			t.Fatal(err)
		}
		if result.kind != "private" || result.partition.Int16 != 2 {
			t.Fatalf("allocation after enable=%+v", result)
		}
	})
}

// TestRuntimeSharedRateOperationsPreservesPopulatedVersion20 proves the
// upgrade changes no prior business, membership, transition, claim or rate row
// and advances only the write generation.
func TestRuntimeSharedRateOperationsPreservesPopulatedVersion20(t *testing.T) {
	db := newMigratedTestDatabase(t, 20)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `INSERT INTO public.users(id,email,name) VALUES('21212121-2121-4121-8121-212121212121','rate-operations-preserve@example.test','rate operations preserved')`); err != nil {
		t.Fatal(err)
	}
	preserved := append(sharedClaimVectorFixture(), transitionFixture()...)
	preserved = append(preserved, transitionAckSQL(initiatorID),
		`INSERT INTO public.runtime_lifecycle_operations(operation_id,workflow_kind) VALUES('rate-operations-preserve-lifecycle','initial_serving')`,
		rateBucketSQL(ratePolicyToken, "31", 1, nil),
		rateBucketSQL(ratePolicySlug, "32", 1, nil),
		rateBucketSQL(ratePolicyAttempt, "33", 1, map[string]string{"window_started_at": "transaction_timestamp()"}),
		pendingAttemptSQL("21212121-2121-4121-8121-212121212122", "private", "33", 1, storedPendingWindowOverrides("private", "33")))
	if err := membershipWrite(t, db, preserved...); err != nil {
		t.Fatal(err)
	}
	countStatement := `SELECT s.generation,c.generation,(SELECT count(*) FROM public.runtime_replicas),(SELECT count(*) FROM public.public_transitions),(SELECT count(*) FROM public.public_transition_acks),(SELECT count(*) FROM public.shared_claim_requests),(SELECT count(*) FROM public.shared_claim_scopes),(SELECT count(*) FROM public.shared_rate_policies),(SELECT count(*) FROM public.shared_rate_policy_key_shapes),(SELECT count(*) FROM public.shared_policy_clocks),(SELECT count(*) FROM public.shared_rate_partitions),(SELECT count(*) FROM public.shared_rate_buckets),(SELECT count(*) FROM public.shared_rate_overflow),(SELECT count(*) FROM public.shared_admission_attempts),(SELECT count(*) FROM public.users),(SELECT sum(active_keys) FROM public.shared_rate_partitions) FROM public.runtime_write_state s CROSS JOIN public.runtime_capacity c WHERE s.singleton AND c.singleton`
	var before, after [16]int64
	scan := func(target *[16]int64) error {
		return db.QueryRowContext(ctx, countStatement).Scan(&target[0], &target[1], &target[2], &target[3], &target[4], &target[5], &target[6], &target[7], &target[8], &target[9], &target[10], &target[11], &target[12], &target[13], &target[14], &target[15])
	}
	if err := scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := applyFS(ctx, db, runtimeTransitionFixtureFS(t, 21), LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	if err := scan(&after); err != nil {
		t.Fatal(err)
	}
	tail := func(values [16]int64) [15]int64 { var rest [15]int64; copy(rest[:], values[1:]); return rest }
	if after[0] != before[0]+1 || tail(after) != tail(before) || before[7] != 24 || before[11] != 3 || before[13] != 1 {
		t.Fatalf("preservation before=%v after=%v", before, after)
	}
	var clocks int64
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM public.shared_policy_clocks WHERE anomaly_count<>0`).Scan(&clocks); err != nil || clocks != 0 {
		t.Fatalf("upgrade disturbed clocks=%d error=%v", clocks, err)
	}
	enableRatePartitions(t, db, ratePolicyToken, 1)
	result, err := callRateDecision(t, db, "aboutme_app", rateTokenExpression(ratePolicyToken, rateByteDigest("31")))
	if err != nil || !result.allowed || result.kind != "private" || result.partition.Int16 != 1 {
		t.Fatalf("preserved bucket admission=%+v error=%v", result, err)
	}
}

// TestRuntimeSharedRateOperationsFailedMigrationRollsBackTypesFunctionsAndGeneration
// proves an injected failure leaves no type, function, grant or generation
// advance behind.
func TestRuntimeSharedRateOperationsFailedMigrationRollsBackTypesFunctionsAndGeneration(t *testing.T) {
	db := newMigratedTestDatabase(t, 20)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	var before int64
	if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	fixture := runtimeTransitionFixtureFS(t, 21)
	file := fixture["00021_runtime_shared_rate_operations.sql"]
	anchor := "RESET ROLE;\nREVOKE CREATE"
	if file == nil || !strings.Contains(string(file.Data), anchor) {
		t.Fatal("migration 21 failure anchor is missing")
	}
	file.Data = []byte(strings.Replace(string(file.Data), anchor, "SELECT 1/0;\n"+anchor, 1))
	_, applyErr := applyFS(ctx, db, fixture, LocalAdminMigratorIdentity())
	requireRateOperationError(t, applyErr, "22012")
	names := append([]string(nil), rateHelperNames...)
	types := make([]string, 0, len(rateResultComposites))
	for _, operation := range rateOperationSignatures {
		names = append(names, operation.name)
	}
	for _, composite := range rateResultComposites {
		types = append(types, composite.name)
	}
	var after, functions, typeCount int64
	var schemaCreate bool
	if err := db.QueryRowContext(ctx, `SELECT generation,(SELECT count(*) FROM pg_proc WHERE pronamespace='public'::regnamespace AND proname=ANY($1)),(SELECT count(*) FROM pg_type WHERE typnamespace='public'::regnamespace AND typname=ANY($2)),has_schema_privilege('aboutme_runtime_owner','public','CREATE') FROM public.runtime_write_state WHERE singleton`, names, types).Scan(&after, &functions, &typeCount, &schemaCreate); err != nil {
		t.Fatal(err)
	}
	if after != before || functions != 0 || typeCount != 0 || schemaCreate {
		t.Fatalf("rollback generation=%d/%d functions=%d types=%d schemaCreate=%t", before, after, functions, typeCount, schemaCreate)
	}
}

// TestRuntimeSharedRateOperationsAdversarialBoundaries proves capacity
// pressure never grants work, that committed failures and pending
// reservations share the P22 bound, that the idle backstop never detaches
// effective debt, the receipt deletion order, a hostile caller search path and
// that overflow is never deleted.
func TestRuntimeSharedRateOperationsAdversarialBoundaries(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	t.Run("capacity_pressure_never_grants_work", func(t *testing.T) {
		const policy, capacity, window = "account.export", 5, 60000000
		for index := 0; index < capacity; index++ {
			result, err := callRateDecision(t, db, "aboutme_app", rateTokenExpression(policy, rateDigest(100+index)))
			if err != nil {
				t.Fatal(err)
			}
			if !result.allowed || result.kind != "overflow" || result.partition.Valid {
				t.Fatalf("overflow admission %d=%+v", index, result)
			}
		}
		denied, err := callRateDecision(t, db, "aboutme_app", rateTokenExpression(policy, rateDigest(200)))
		if err != nil {
			t.Fatal(err)
		}
		if denied.allowed || denied.kind != "overflow" || denied.partition.Valid || denied.retry != rateTokenRetry(capacity, window) {
			t.Fatalf("capacity pressure granted work=%+v", denied)
		}
		if rateBucketCount(t, db, policy) != 0 {
			t.Fatal("overflow denial allocated a private bucket")
		}
	})

	t.Run("committed_failures_and_pending_share_the_bound", func(t *testing.T) {
		setRateClock(t, db, rateFutureClock)
		enableRatePartitions(t, db, ratePolicyAttempt, 1)
		client := rateUUID("cccccccc", 90)
		for index := 1; index <= 5; index++ {
			if _, err := callRateReserve(t, db, "aboutme_app", rateReserveExpression(rateUUID("adadadad", index), client)); err != nil {
				t.Fatal(err)
			}
			if _, err := callRateFinish(t, db, "aboutme_app", rateFinishExpression(rateUUID("adadadad", index), "failure")); err != nil {
				t.Fatal(err)
			}
		}
		if bucket := rateBucket(t, db, ratePolicyAttempt, goRateClientDigest(t, client)); bucket.count.Int32 != 5 {
			t.Fatalf("committed failures=%d", bucket.count.Int32)
		}
		for index := 6; index <= 10; index++ {
			result, err := callRateReserve(t, db, "aboutme_app", rateReserveExpression(rateUUID("adadadad", index), client))
			if err != nil {
				t.Fatal(err)
			}
			requireReserveShape(t, result, true, 0, "private", 1, false)
		}
		denied, err := callRateReserve(t, db, "aboutme_app", rateReserveExpression(rateUUID("adadadad", 11), client))
		if err != nil {
			t.Fatal(err)
		}
		requireReserveShape(t, denied, false, 900, "", 0, false)
		restoreRateClock(t, db)
	})

	t.Run("idle_backstop_never_detaches_effective_debt", func(t *testing.T) {
		enableRatePartitions(t, db, ratePolicyAttempt, 2)
		fixture := []string{
			rateBucketSQL(ratePolicyAttempt, "44", 2, map[string]string{
				"window_started_at": rateAttemptClockSQL,
				"last_seen":         rateAttemptClockSQL + "-interval '48 hours'",
			}),
			pendingAttemptSQL("44444444-4444-4444-8444-444444444444", "private", "44", 2, storedPendingWindowOverrides("private", "44")),
		}
		if err := membershipWrite(t, db, fixture...); err != nil {
			t.Fatal(err)
		}
		before := ratePartition(t, db, ratePolicyAttempt, 2)
		result, err := callRateCleanup(t, db, "aboutme_maintenance", rateCleanupExpression(ratePolicyAttempt, 256))
		if err != nil {
			t.Fatal(err)
		}
		if result.deleted != 0 || result.policyIdle {
			t.Fatalf("idle backstop detached effective debt=%+v", result)
		}
		if !rateBucket(t, db, ratePolicyAttempt, rateByteDigest("44")).exists {
			t.Fatal("cleanup deleted a bucket carrying effective pending debt")
		}
		if rateAttempt(t, db, "44444444-4444-4444-8444-444444444444").state != "pending" {
			t.Fatal("cleanup resolved an unexpired reservation")
		}
		if after := ratePartition(t, db, ratePolicyAttempt, 2); after != before {
			t.Fatalf("cleanup changed a partition before=%+v after=%+v", before, after)
		}
	})

	t.Run("receipt_cleanup_deletes_oldest_first", func(t *testing.T) {
		rateSeedTerminalReceipts(t, db, "1a1a1a1a", 1, "30 hours")
		rateSeedTerminalReceipts(t, db, "2b2b2b2b", 1, "29 hours")
		rateSeedTerminalReceipts(t, db, "3c3c3c3c", 1, "28 hours")
		for _, want := range []string{"1a1a1a1a", "2b2b2b2b", "3c3c3c3c"} {
			deleted, err := callRateReceiptCleanup(t, db, "aboutme_maintenance", rateReceiptCleanupExpression(1))
			if err != nil || deleted != 1 {
				t.Fatalf("ordered cleanup=%d error=%v", deleted, err)
			}
			if rateAttempt(t, db, want+"-0000-4000-8000-000000000001").exists {
				t.Fatalf("cleanup skipped the oldest receipt %s", want)
			}
		}
	})

	t.Run("hostile_caller_search_path_is_inert", func(t *testing.T) {
		for _, statement := range []string{
			`CREATE SCHEMA hostile`,
			`GRANT USAGE ON SCHEMA hostile TO aboutme_app`,
			`CREATE TABLE hostile.shared_rate_buckets(policy_id text)`,
			`CREATE TABLE hostile.shared_policy_clocks(policy_id text)`,
			`GRANT SELECT,INSERT ON hostile.shared_rate_buckets,hostile.shared_policy_clocks TO aboutme_app`,
			`CREATE FUNCTION hostile.runtime_sample_rate_time() RETURNS timestamptz LANGUAGE sql AS $x$ SELECT '1999-01-01 00:00:00+00'::timestamptz $x$`,
		} {
			if _, err := db.ExecContext(context.Background(), statement); err != nil {
				t.Fatal(err)
			}
		}
		enableRatePartitions(t, db, ratePolicyToken, 1)
		var result rateDecisionResult
		if err := rateCallWithSearchPath(t, db, "aboutme_app", "hostile,pg_temp,public", rateTokenExpression(ratePolicyToken, rateDigest(210)), rateDecisionProjection,
			&result.allowed, &result.retry, &result.kind, &result.partition, &result.effectiveAt); err != nil {
			t.Fatal(err)
		}
		if !result.allowed || result.kind != "private" || result.partition.Int16 != 1 || result.effectiveAt.Year() == 1999 {
			t.Fatalf("hostile path result=%+v", result)
		}
		var decoys int
		if err := db.QueryRowContext(context.Background(), `SELECT (SELECT count(*) FROM hostile.shared_rate_buckets)+(SELECT count(*) FROM hostile.shared_policy_clocks)`).Scan(&decoys); err != nil || decoys != 0 {
			t.Fatalf("hostile decoys=%d error=%v", decoys, err)
		}
		if !rateBucket(t, db, ratePolicyToken, rateDigest(210)).exists {
			t.Fatal("hostile path prevented the real allocation")
		}
	})

	t.Run("overflow_is_never_deleted", func(t *testing.T) {
		for _, policy := range []string{ratePolicyToken, ratePolicyFailure, ratePolicySlug, ratePolicyAttempt} {
			if _, err := callRateCleanup(t, db, "aboutme_maintenance", rateCleanupExpression(policy, 256)); err != nil {
				t.Fatal(err)
			}
		}
		var overflow int
		if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM public.shared_rate_overflow`).Scan(&overflow); err != nil || overflow != 24 {
			t.Fatalf("overflow rows=%d error=%v", overflow, err)
		}
	})
}

// TestRuntimeSharedRateOperationsInstalledCorruptionFailsClosed proves stored
// clock, partition, token and P22 corruption raises AM001 without disclosing
// any private value or committing a decision.
func TestRuntimeSharedRateOperationsInstalledCorruptionFailsClosed(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	digest := rateDigest(220)
	t.Run("missing_policy_clock", func(t *testing.T) {
		corruptRateRows(t, db, `DELETE FROM public.shared_policy_clocks WHERE policy_id='public.render_miss'`)
		_, err := callRateDecision(t, db, "aboutme_app", rateTokenExpression("public.render_miss", digest))
		pgErr := rateOperationErrorOf(t, err, "AM001")
		requireNoRateIdentityLeak(t, pgErr, digest[:40])
	})
	t.Run("missing_partition_row", func(t *testing.T) {
		corruptRateRows(t, db, `DELETE FROM public.shared_rate_partitions WHERE policy_id='public.artifact_request' AND partition=2`)
		_, err := callRateDecision(t, db, "aboutme_app", rateTokenExpression("public.artifact_request", digest))
		pgErr := rateOperationErrorOf(t, err, "AM001")
		requireNoRateIdentityLeak(t, pgErr, digest[:40])
	})
	t.Run("token_numerator_above_capacity", func(t *testing.T) {
		enableRatePartitions(t, db, ratePolicyToken, 1)
		if _, err := callRateDecision(t, db, "aboutme_app", rateTokenExpression(ratePolicyToken, digest)); err != nil {
			t.Fatal(err)
		}
		corruptRateRows(t, db, fmt.Sprintf(`UPDATE public.shared_rate_buckets SET token_numerator=18000000001 WHERE policy_id='%s' AND key_digest=decode('%s','hex')`, ratePolicyToken, digest))
		_, err := callRateDecision(t, db, "aboutme_app", rateTokenExpression(ratePolicyToken, digest))
		pgErr := rateOperationErrorOf(t, err, "AM001")
		requireNoRateIdentityLeak(t, pgErr, digest[:40])
		if state := rateBucket(t, db, ratePolicyToken, digest); state.numerator.Int64 != 18000000001 {
			t.Fatalf("corrupt decision committed=%+v", state)
		}
	})
	t.Run("attempt_debt_above_capacity", func(t *testing.T) {
		enableRatePartitions(t, db, ratePolicyAttempt, 1)
		client := rateUUID("cccccccc", 95)
		clientDigest := goRateClientDigest(t, client)
		if _, err := callRateReserve(t, db, "aboutme_app", rateReserveExpression(rateUUID("afafafaf", 1), client)); err != nil {
			t.Fatal(err)
		}
		corruptRateRows(t, db, fmt.Sprintf(`UPDATE public.shared_rate_buckets SET count=10 WHERE policy_id='%s' AND key_digest=decode('%s','hex')`, ratePolicyAttempt, clientDigest))
		_, err := callRateReserve(t, db, "aboutme_app", rateReserveExpression(rateUUID("afafafaf", 2), client))
		pgErr := rateOperationErrorOf(t, err, "AM001")
		requireNoRateIdentityLeak(t, pgErr, clientDigest[:40], client)
		if rateAttempt(t, db, rateUUID("afafafaf", 2)).exists {
			t.Fatal("corrupt reserve inserted an attempt")
		}
	})
}
