package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

var runtimeSharedRateTables = []string{
	"shared_rate_policies",
	"shared_rate_policy_key_shapes",
	"shared_policy_clocks",
	"shared_rate_partitions",
	"shared_rate_buckets",
	"shared_rate_overflow",
	"shared_admission_attempts",
}

func TestRuntimeSharedRateSchemaExactSeeds(t *testing.T) {
	db, ctx := runtimeSharedRateDB(t)
	var policies, shapes, clocks, partitions, overflow, buckets, attempts int
	var oneTimestamp bool
	if err := db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM public.shared_rate_policies),(SELECT count(*) FROM public.shared_rate_policy_key_shapes),(SELECT count(*) FROM public.shared_policy_clocks),(SELECT count(*) FROM public.shared_rate_partitions),(SELECT count(*) FROM public.shared_rate_overflow),(SELECT count(*) FROM public.shared_rate_buckets),(SELECT count(*) FROM public.shared_admission_attempts),(SELECT count(DISTINCT stamp)=1 FROM (SELECT high_water_at stamp FROM public.shared_policy_clocks UNION ALL SELECT last_raw_at FROM public.shared_policy_clocks UNION ALL SELECT updated_at FROM public.shared_rate_partitions UNION ALL SELECT last_seen FROM public.shared_rate_overflow UNION ALL SELECT refill_at FROM public.shared_rate_overflow WHERE refill_at IS NOT NULL) s)`).Scan(&policies, &shapes, &clocks, &partitions, &overflow, &buckets, &attempts, &oneTimestamp); err != nil {
		t.Fatal(err)
	}
	if policies != 24 || shapes != 35 || clocks != 24 || partitions != 48 || overflow != 24 || buckets != 0 || attempts != 0 || !oneTimestamp {
		t.Fatalf("seed counts=%d/%d/%d/%d/%d/%d/%d oneTimestamp=%t", policies, shapes, clocks, partitions, overflow, buckets, attempts, oneTimestamp)
	}
	var flags bool
	if err := db.QueryRowContext(ctx, `SELECT bool_and(allow_success_clear=(policy_id IN ('password.login_failure_email','oauth.failed_grant')) AND denied_attempt_adds_debt=(policy_id='resume.slug_change') AND ordinary_idle_microseconds=86400000000 AND max_keys_per_partition=10000) FROM public.shared_rate_policies`).Scan(&flags); err != nil || !flags {
		t.Fatalf("catalog flags=%t error=%v", flags, err)
	}
	var p05, peer, enabled, active int
	if err := db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM public.shared_rate_policy_key_shapes WHERE policy_id='auth.provider_start'),(SELECT count(*) FROM public.shared_rate_policy_key_shapes WHERE key_shape='peer_ip'),(SELECT count(*) FROM public.shared_rate_partitions WHERE enabled),(SELECT sum(active_keys) FROM public.shared_rate_partitions)`).Scan(&p05, &peer, &enabled, &active); err != nil || p05 != 3 || peer != 10 || enabled != 0 || active != 0 {
		t.Fatalf("shape/partition seed=%d/%d/%d/%d error=%v", p05, peer, enabled, active, err)
	}
	expectedPolicies := map[string]string{
		"api.outer_request": "token_bucket/300/60000000", "resume.read": "token_bucket/600/60000000", "resume.write": "token_bucket/240/60000000", "resume.photo_upload_rate": "token_bucket/20/3600000000",
		"auth.provider_start": "token_bucket/30/60000000", "password.login_ip": "token_bucket/30/60000000", "password.login_failure_email": "fixed_window/10/900000000", "password.register_forgot_ip": "token_bucket/20/3600000000",
		"password.register_forgot_email": "token_bucket/5/3600000000", "password.verify_reset_ip": "token_bucket/10/3600000000", "password.account_mutation": "token_bucket/10/3600000000", "resume.slug_change": "rolling_slug/30/3600000000",
		"resume.owner_pdf_account": "token_bucket/10/60000000", "resume.owner_pdf_ip": "token_bucket/10/60000000", "account.export": "token_bucket/5/60000000", "account.delete": "token_bucket/5/60000000",
		"public.artifact_request": "token_bucket/300/60000000", "public.render_miss": "token_bucket/20/60000000", "realtime.public_request": "token_bucket/300/60000000", "oauth.register": "token_bucket/5/3600000000",
		"oauth.token": "token_bucket/30/60000000", "oauth.failed_grant": "fixed_window/10/900000000", "mcp.token": "token_bucket/120/60000000", "mcp.user": "token_bucket/240/60000000",
	}
	policyRows, err := db.QueryContext(ctx, `SELECT policy_id,algorithm||'/'||capacity||'/'||window_microseconds FROM public.shared_rate_policies`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := policyRows.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	for policyRows.Next() {
		var policy, tuple string
		if scanErr := policyRows.Scan(&policy, &tuple); scanErr != nil {
			t.Fatal(scanErr)
		}
		if expectedPolicies[policy] != tuple {
			t.Errorf("policy %s tuple=%s want=%s", policy, tuple, expectedPolicies[policy])
		}
		delete(expectedPolicies, policy)
	}
	if rowsErr := policyRows.Err(); rowsErr != nil {
		t.Fatal(rowsErr)
	}
	if len(expectedPolicies) != 0 {
		t.Fatalf("missing policy tuples=%v", expectedPolicies)
	}
	expectedShapes := map[string]bool{}
	for _, tuple := range []string{
		"api.outer_request/ip", "resume.read/account_ip", "resume.write/account_ip", "resume.photo_upload_rate/account_ip", "auth.provider_start/ip", "auth.provider_start/account_ip", "password.login_ip/ip", "password.login_failure_email/email_digest", "password.register_forgot_ip/ip", "password.register_forgot_email/email_digest", "password.verify_reset_ip/ip", "password.account_mutation/account_ip", "resume.slug_change/account", "resume.owner_pdf_account/account", "resume.owner_pdf_ip/ip", "account.export/account_ip", "account.delete/account_ip", "public.artifact_request/ip", "public.render_miss/ip", "realtime.public_request/ip", "oauth.register/ip", "oauth.token/ip", "oauth.failed_grant/oauth_client", "mcp.token/token", "mcp.user/user",
		"api.outer_request/peer_ip", "resume.read/peer_ip", "resume.write/peer_ip", "resume.photo_upload_rate/peer_ip", "auth.provider_start/peer_ip", "resume.owner_pdf_account/peer_ip", "resume.owner_pdf_ip/peer_ip", "account.export/peer_ip", "account.delete/peer_ip", "realtime.public_request/peer_ip",
	} {
		expectedShapes[tuple] = true
	}
	shapeRows, err := db.QueryContext(ctx, `SELECT policy_id||'/'||key_shape FROM public.shared_rate_policy_key_shapes`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := shapeRows.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	for shapeRows.Next() {
		var tuple string
		if err := shapeRows.Scan(&tuple); err != nil {
			t.Fatal(err)
		}
		if !expectedShapes[tuple] {
			t.Errorf("unexpected shape %s", tuple)
		}
		delete(expectedShapes, tuple)
	}
	if err := shapeRows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(expectedShapes) != 0 {
		t.Fatalf("missing shapes=%v", expectedShapes)
	}
	var invalidClock, invalidPartition, invalidOverflow int
	if err := db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM public.shared_policy_clocks WHERE anomaly_count<>0),(SELECT count(*) FROM public.shared_rate_partitions WHERE enabled OR capacity_generation<>1 OR operation_id<>'bootstrap-uncomposed-v1' OR active_keys<>0),(SELECT count(*) FROM public.shared_rate_overflow o JOIN public.shared_rate_policies p USING(policy_id) WHERE NOT ((o.algorithm='token_bucket' AND o.token_numerator=p.capacity::bigint*p.window_microseconds AND o.refill_at=o.last_seen AND o.window_started_at IS NULL AND o.count IS NULL AND o.rolling_events IS NULL) OR (o.algorithm='fixed_window' AND o.token_numerator IS NULL AND o.refill_at IS NULL AND o.window_started_at IS NULL AND o.count=0 AND o.rolling_events IS NULL) OR (o.algorithm='rolling_slug' AND o.token_numerator IS NULL AND o.refill_at IS NULL AND o.window_started_at IS NULL AND o.count=0 AND cardinality(o.rolling_events)=0)))`).Scan(&invalidClock, &invalidPartition, &invalidOverflow); err != nil || invalidClock != 0 || invalidPartition != 0 || invalidOverflow != 0 {
		t.Fatalf("invalid clock/partition/overflow seeds=%d/%d/%d error=%v", invalidClock, invalidPartition, invalidOverflow, err)
	}
}

func TestRuntimeSharedRateSchemaCatalogClosedAndImmutable(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	requireRateError(t, membershipWrite(t, db, `INSERT INTO public.shared_rate_policies VALUES('unknown','token_bucket',1,1,false,false,86400000000,10000)`), "23514", "shared_rate_policies_exact_catalog_check", "")
	requireRateError(t, membershipWrite(t, db, `INSERT INTO public.shared_rate_policy_key_shapes VALUES('mcp.user','ip')`), "23514", "shared_rate_policy_key_shapes_exact_check", "")
	requireRateError(t, membershipWrite(t, db, `UPDATE public.shared_rate_policies SET capacity=capacity+1 WHERE policy_id='mcp.user'`), "55000", "", "")
	requireRateError(t, membershipWrite(t, db, `DELETE FROM public.shared_rate_policy_key_shapes WHERE policy_id='mcp.user'`), "55000", "", "")
}

func TestRuntimeSharedRateSchemaAlgorithmMatricesAndBounds(t *testing.T) {
	tests := []struct {
		name, policy string
		overrides    map[string]string
		constraint   string
	}{
		{"token_negative", "api.outer_request", map[string]string{"token_numerator": "-1"}, "shared_rate_buckets_algorithm_shape_check"},
		{"token_over_capacity", "api.outer_request", map[string]string{"token_numerator": "18000000001"}, "shared_rate_state_exact"},
		{"token_forbidden_count", "api.outer_request", map[string]string{"count": "0"}, "shared_rate_buckets_algorithm_shape_check"},
		{"fixed_negative", "password.login_failure_email", map[string]string{"count": "-1"}, "shared_rate_buckets_algorithm_shape_check"},
		{"fixed_eleven", "password.login_failure_email", map[string]string{"count": "11", "window_started_at": "transaction_timestamp()"}, "shared_rate_buckets_algorithm_shape_check"},
		{"p07_zero_with_window", "password.login_failure_email", map[string]string{"window_started_at": "transaction_timestamp()"}, "shared_rate_state_exact"},
		{"rolling_31", "resume.slug_change", map[string]string{"count": "31", "rolling_events": "array_fill(transaction_timestamp(),ARRAY[31])"}, "shared_rate_buckets_algorithm_shape_check"},
		{"rolling_null", "resume.slug_change", map[string]string{"count": "1", "rolling_events": "ARRAY[NULL]::timestamptz[]"}, "shared_rate_buckets_algorithm_shape_check"},
		{"rolling_count_mismatch", "resume.slug_change", map[string]string{"count": "1", "rolling_events": "ARRAY[]::timestamptz[]"}, "shared_rate_buckets_algorithm_shape_check"},
		{"rolling_reverse", "resume.slug_change", map[string]string{"count": "2", "rolling_events": "ARRAY[transaction_timestamp(),transaction_timestamp()-interval '1 second']"}, "shared_rate_buckets_algorithm_shape_check"},
		{"rolling_shifted_reverse", "resume.slug_change", map[string]string{"count": "2", "rolling_events": "'[2:3]={2026-01-02 00:00:00+00,2026-01-01 00:00:00+00}'::timestamptz[]"}, "shared_rate_buckets_algorithm_shape_check"},
		{"rolling_2d", "resume.slug_change", map[string]string{"count": "4", "rolling_events": "ARRAY[[transaction_timestamp(),transaction_timestamp()],[transaction_timestamp(),transaction_timestamp()]]"}, "shared_rate_buckets_algorithm_shape_check"},
	}
	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, _ := runtimeSharedRateDB(t)
			requireRateError(t, membershipWrite(t, db, rateBucketSQL(test.policy, fmt.Sprintf("%02x", i+1), 1, test.overrides)), "23514", test.constraint, "")
		})
	}
	for _, count := range []int{0, 1, 29, 30} {
		t.Run(fmt.Sprintf("rolling_%d", count), func(t *testing.T) {
			db, _ := runtimeSharedRateDB(t)
			events := "ARRAY[]::timestamptz[]"
			if count > 0 {
				events = fmt.Sprintf("array_fill(transaction_timestamp(),ARRAY[%d])", count)
			}
			if err := membershipWrite(t, db, rateBucketSQL("resume.slug_change", fmt.Sprintf("%02x", count+40), 1, map[string]string{"count": fmt.Sprint(count), "rolling_events": events})); err != nil {
				t.Fatal(err)
			}
		})
	}
	db, _ := runtimeSharedRateDB(t)
	if err := membershipWrite(t, db, rateBucketSQL("resume.slug_change", "7f", 1, map[string]string{"count": "2", "rolling_events": "'[-2:-1]={2026-01-01 00:00:00+00,2026-01-02 00:00:00+00}'::timestamptz[]"})); err != nil {
		t.Fatalf("sorted negative-lower-bound array rejected: %v", err)
	}
}

func TestRuntimeSharedRateSchemaEveryTokenPolicyRejectsNumeratorAboveCapacity(t *testing.T) {
	db, ctx := runtimeSharedRateDB(t)
	rows, err := db.QueryContext(ctx, `SELECT policy_id FROM public.shared_rate_policies WHERE algorithm='token_bucket' ORDER BY policy_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	var policies []string
	for rows.Next() {
		var policy string
		if err := rows.Scan(&policy); err != nil {
			t.Fatal(err)
		}
		policies = append(policies, policy)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(policies) != 21 {
		t.Fatalf("token policies=%d", len(policies))
	}
	for _, policy := range policies {
		t.Run(policy, func(t *testing.T) {
			statement := fmt.Sprintf(`UPDATE public.shared_rate_overflow o SET token_numerator=p.capacity::bigint*p.window_microseconds+1 FROM public.shared_rate_policies p WHERE o.policy_id=p.policy_id AND o.policy_id='%s'`, policy)
			requireRateError(t, membershipWrite(t, db, statement), "23514", "shared_rate_state_exact", "")
		})
	}
}

func TestRuntimeSharedRateSchemaBucketEveryRequiredAndForbiddenField(t *testing.T) {
	tests := []struct {
		name, policy, field, value, code, constraint, column string
	}{
		{"missing_policy", "api.outer_request", "policy_id", "NULL", "23502", "", "policy_id"},
		{"missing_digest", "api.outer_request", "key_digest", "NULL", "23502", "", "key_digest"},
		{"missing_partition", "api.outer_request", "partition", "NULL", "23502", "", "partition"},
		{"missing_algorithm", "api.outer_request", "algorithm", "NULL", "23502", "", "algorithm"},
		{"missing_last_seen", "api.outer_request", "last_seen", "NULL", "23502", "", "last_seen"},
		{"token_missing_numerator", "api.outer_request", "token_numerator", "NULL", "23514", "shared_rate_buckets_algorithm_shape_check", ""},
		{"token_missing_refill", "api.outer_request", "refill_at", "NULL", "23514", "shared_rate_buckets_algorithm_shape_check", ""},
		{"token_window", "api.outer_request", "window_started_at", "transaction_timestamp()", "23514", "shared_rate_buckets_algorithm_shape_check", ""},
		{"token_count", "api.outer_request", "count", "0", "23514", "shared_rate_buckets_algorithm_shape_check", ""},
		{"token_events", "api.outer_request", "rolling_events", "ARRAY[]::timestamptz[]", "23514", "shared_rate_buckets_algorithm_shape_check", ""},
		{"fixed_missing_count", "password.login_failure_email", "count", "NULL", "23514", "shared_rate_buckets_algorithm_shape_check", ""},
		{"fixed_token", "password.login_failure_email", "token_numerator", "0", "23514", "shared_rate_buckets_algorithm_shape_check", ""},
		{"fixed_refill", "password.login_failure_email", "refill_at", "transaction_timestamp()", "23514", "shared_rate_buckets_algorithm_shape_check", ""},
		{"fixed_events", "password.login_failure_email", "rolling_events", "ARRAY[]::timestamptz[]", "23514", "shared_rate_buckets_algorithm_shape_check", ""},
		{"rolling_missing_count", "resume.slug_change", "count", "NULL", "23514", "shared_rate_buckets_algorithm_shape_check", ""},
		{"rolling_missing_events", "resume.slug_change", "rolling_events", "NULL", "23514", "shared_rate_buckets_algorithm_shape_check", ""},
		{"rolling_token", "resume.slug_change", "token_numerator", "0", "23514", "shared_rate_buckets_algorithm_shape_check", ""},
		{"rolling_refill", "resume.slug_change", "refill_at", "transaction_timestamp()", "23514", "shared_rate_buckets_algorithm_shape_check", ""},
		{"rolling_window", "resume.slug_change", "window_started_at", "transaction_timestamp()", "23514", "shared_rate_buckets_algorithm_shape_check", ""},
	}
	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, _ := runtimeSharedRateDB(t)
			err := membershipWrite(t, db, rateBucketSQL(test.policy, fmt.Sprintf("%02x", 130+i), 1, map[string]string{test.field: test.value}))
			requireRateError(t, err, test.code, test.constraint, test.column)
		})
	}
}

func TestRuntimeSharedRateSchemaP07CountWindowPositiveBoundaries(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	if err := membershipWrite(t, db,
		rateBucketSQL("password.login_failure_email", "71", 1, map[string]string{"count": "1", "window_started_at": "transaction_timestamp()"}),
		rateBucketSQL("password.login_failure_email", "72", 1, map[string]string{"count": "10", "window_started_at": "transaction_timestamp()"}),
	); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeSharedRateSchemaPartitionExactCountAndForcedRecheck(t *testing.T) {
	db, ctx := runtimeSharedRateDB(t)
	requireRateError(t, membershipWrite(t, db, `UPDATE public.shared_rate_partitions SET active_keys=1 WHERE policy_id='api.outer_request' AND partition=1`), "23514", "shared_rate_partition_exact_count", "")
	valid := rateBucketSQL("api.outer_request", "aa", 1, nil)
	if err := membershipWrite(t, db, valid); err != nil {
		t.Fatal(err)
	}
	var active int
	if err := db.QueryRowContext(ctx, `SELECT active_keys FROM public.shared_rate_partitions WHERE policy_id='api.outer_request' AND partition=1`).Scan(&active); err != nil || active != 1 {
		t.Fatalf("active=%d error=%v", active, err)
	}
	forcedValid := rateBucketSQL("api.outer_request", "ab", 1, nil)
	requireRateError(t, membershipWrite(t, db, forcedValid, `SET CONSTRAINTS shared_rate_partitions_exact,shared_rate_buckets_partition_exact IMMEDIATE`, `SET CONSTRAINTS shared_rate_partitions_exact,shared_rate_buckets_partition_exact DEFERRED`, `UPDATE public.shared_rate_partitions SET active_keys=active_keys+1 WHERE policy_id='api.outer_request' AND partition=1`), "23514", "shared_rate_partition_exact_count", "")
	if err := membershipWrite(t, db, `UPDATE public.shared_rate_partitions SET enabled=true,operation_id='toggle',capacity_generation=2,updated_at=clock_timestamp() WHERE policy_id='api.outer_request' AND partition=1`); err != nil {
		t.Fatal(err)
	}
	requireRateError(t, membershipWrite(t, db, `UPDATE public.shared_rate_partitions SET active_keys=0 WHERE policy_id='api.outer_request' AND partition=1`), "23514", "shared_rate_partition_exact_count", "")
	requireRateError(t, membershipWrite(t, db, `DELETE FROM public.shared_rate_buckets WHERE policy_id='api.outer_request' AND key_digest=decode(repeat('aa',32),'hex')`), "23514", "shared_rate_partition_exact_count", "")
}

func TestRuntimeSharedRateSchemaForcedStateCheckThenInvalidWindow(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	bucket := rateBucketSQL("oauth.failed_grant", "5a", 1, map[string]string{"window_started_at": "transaction_timestamp()"})
	attempt := pendingAttemptSQL("5a000000-0000-4000-8000-000000000001", "private", "5a", 1, nil)
	requireRateError(t, membershipWrite(t, db, bucket, attempt, `SET CONSTRAINTS shared_rate_buckets_state_exact,shared_admission_attempts_state_exact IMMEDIATE`, `SET CONSTRAINTS shared_rate_buckets_state_exact,shared_admission_attempts_state_exact DEFERRED`, `UPDATE public.shared_rate_buckets SET window_started_at=NULL WHERE policy_id='oauth.failed_grant' AND key_digest=decode(repeat('5a',32),'hex')`), "23514", "shared_rate_state_exact", "")
}

func TestRuntimeSharedRateSchemaClockPartitionAndOverflowIdentity(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	for _, statement := range []string{
		`UPDATE public.shared_policy_clocks SET high_water_at=high_water_at-interval '1 microsecond' WHERE policy_id='api.outer_request'`,
		`DELETE FROM public.shared_policy_clocks WHERE policy_id='api.outer_request'`,
		`UPDATE public.shared_rate_partitions SET partition=2 WHERE policy_id='api.outer_request' AND partition=1`,
		`DELETE FROM public.shared_rate_partitions WHERE policy_id='api.outer_request' AND partition=1`,
		`UPDATE public.shared_rate_overflow SET policy_id='mcp.user' WHERE policy_id='api.outer_request'`,
		`DELETE FROM public.shared_rate_overflow WHERE policy_id='api.outer_request'`,
	} {
		requireRateError(t, membershipWrite(t, db, statement), "55000", "", "")
	}
	for _, statement := range []struct{ sql, constraint string }{
		{`UPDATE public.shared_rate_partitions SET capacity_generation=0 WHERE policy_id='api.outer_request' AND partition=1`, "shared_rate_partitions_generation_check"},
		{`UPDATE public.shared_rate_partitions SET operation_id='' WHERE policy_id='api.outer_request' AND partition=1`, "shared_rate_partitions_operation_check"},
		{`UPDATE public.shared_policy_clocks SET anomaly_count=-1 WHERE policy_id='api.outer_request'`, "shared_policy_clocks_anomaly_check"},
	} {
		requireRateError(t, membershipWrite(t, db, statement.sql), "23514", statement.constraint, "")
	}
}

func TestRuntimeSharedRateSchemaPartitionTenThousandBoundary(t *testing.T) {
	db, ctx := runtimeSharedRateDB(t)
	insert := `INSERT INTO public.shared_rate_buckets(policy_id,key_digest,partition,algorithm,last_seen,token_numerator,refill_at) SELECT 'api.outer_request',sha256(i::text::bytea),1,'token_bucket',transaction_timestamp(),18000000000,transaction_timestamp() FROM generate_series(1,10000) i`
	_, tx := beginRateWrite(t, db, 45*time.Second)
	writeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := tx.ExecContext(writeCtx, insert); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(writeCtx, `UPDATE public.shared_rate_partitions SET active_keys=10000 WHERE policy_id='api.outer_request' AND partition=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(writeCtx, `RESET ROLE; SELECT public.runtime_finish_write()`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var rows, active int
	if err := db.QueryRowContext(ctx, `SELECT count(*),(SELECT active_keys FROM public.shared_rate_partitions WHERE policy_id='api.outer_request' AND partition=1) FROM public.shared_rate_buckets WHERE policy_id='api.outer_request'`).Scan(&rows, &active); err != nil || rows != 10000 || active != 10000 {
		t.Fatalf("boundary rows/active=%d/%d error=%v", rows, active, err)
	}
	requireRateError(t, membershipWrite(t, db, `INSERT INTO public.shared_rate_buckets(policy_id,key_digest,partition,algorithm,last_seen,token_numerator,refill_at) VALUES('api.outer_request',decode(repeat('ff',32),'hex'),1,'token_bucket',transaction_timestamp(),18000000000,transaction_timestamp())`, `UPDATE public.shared_rate_partitions SET active_keys=10001 WHERE policy_id='api.outer_request' AND partition=1`), "23514", "shared_rate_partitions_active_keys_check", "")
}

func TestRuntimeSharedRateSchemaDuplicateAllocationWaitsAndKeepsCount(t *testing.T) {
	db, ctx := runtimeSharedRateDB(t)
	_, first := beginRateWrite(t, db, 20*time.Second)
	_, second := beginRateWrite(t, db, 20*time.Second)
	if _, err := first.ExecContext(ctx, `UPDATE public.shared_rate_partitions SET active_keys=active_keys+1 WHERE policy_id='api.outer_request' AND partition=1`); err != nil {
		t.Fatal(err)
	}
	insert := `INSERT INTO public.shared_rate_buckets(policy_id,key_digest,partition,algorithm,last_seen,token_numerator,refill_at) VALUES('api.outer_request',decode(repeat('81',32),'hex'),1,'token_bucket',transaction_timestamp(),18000000000,transaction_timestamp())`
	if _, err := first.ExecContext(ctx, insert); err != nil {
		t.Fatal(err)
	}
	var secondPID int
	if err := second.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&secondPID); err != nil {
		t.Fatal(err)
	}
	raceCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		if _, err := second.ExecContext(raceCtx, `UPDATE public.shared_rate_partitions SET active_keys=active_keys+1 WHERE policy_id='api.outer_request' AND partition=1`); err != nil {
			result <- err
			return
		}
		_, err := second.ExecContext(raceCtx, insert)
		result <- err
	}()
	joined := false
	defer func() {
		cancel()
		if rollbackErr := first.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			t.Error(rollbackErr)
		}
		if rollbackErr := second.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			t.Error(rollbackErr)
		}
		if !joined {
			select {
			case <-result:
				joined = true
			case <-time.After(5 * time.Second):
				t.Error("duplicate allocation goroutine did not join during cleanup")
			}
		}
	}()
	if err := waitForTransitionCondition(db, `SELECT cardinality(pg_blocking_pids($1))>0 AND EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock')`, secondPID); err != nil {
		t.Fatal(err)
	}
	if _, err := first.ExecContext(ctx, `RESET ROLE; SELECT public.runtime_finish_write()`); err != nil {
		t.Fatal(err)
	}
	if err := first.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		joined = true
		requireRateError(t, err, "23505", "shared_rate_buckets_pkey", "")
	case <-raceCtx.Done():
		t.Fatal("duplicate allocation did not finish")
	}
	if err := second.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		t.Fatal(err)
	}
	var rows, active int
	if err := db.QueryRowContext(ctx, `SELECT count(*),(SELECT active_keys FROM public.shared_rate_partitions WHERE policy_id='api.outer_request' AND partition=1) FROM public.shared_rate_buckets WHERE policy_id='api.outer_request'`).Scan(&rows, &active); err != nil || rows != 1 || active != 1 {
		t.Fatalf("duplicate final rows/active=%d/%d error=%v", rows, active, err)
	}
}

func TestRuntimeSharedRateSchemaP22PendingBindingAndTerminalRetention(t *testing.T) {
	db, ctx := runtimeSharedRateDB(t)
	bucket := rateBucketSQL("oauth.failed_grant", "22", 1, map[string]string{"window_started_at": "transaction_timestamp()"})
	attempt := pendingAttemptSQL("22222222-2222-4222-8222-222222222222", "private", "22", 1, nil)
	if err := membershipWrite(t, db, bucket, attempt); err != nil {
		t.Fatal(err)
	}
	requireRateError(t, membershipWrite(t, db, pendingAttemptSQL("33333333-3333-4333-8333-333333333333", "private", "33", 1, nil)), "23514", "shared_rate_state_exact", "")
	requireRateError(t, membershipWrite(t, db, pendingAttemptSQL("33333333-3333-4333-8333-333333333334", "private", "22", 2, storedPendingWindowOverrides("private", "22"))), "23514", "shared_rate_state_exact", "")
	mismatchedWindow := map[string]string{"window_started_at": "transaction_timestamp()+interval '1 minute'", "reserved_at": "transaction_timestamp()+interval '1 minute'", "effective_until": "transaction_timestamp()+interval '16 minutes'"}
	requireRateError(t, membershipWrite(t, db, pendingAttemptSQL("33333333-3333-4333-8333-333333333335", "private", "22", 1, mismatchedWindow)), "23514", "shared_rate_state_exact", "")
	requireRateError(t, membershipWrite(t, db, `DELETE FROM public.shared_admission_attempts WHERE attempt_id='22222222-2222-4222-8222-222222222222'`), "55000", "", "")
	if err := membershipWrite(t, db, `UPDATE public.shared_admission_attempts SET state='terminal',outcome='neutral',terminal_reason='caller_finish',terminal_at=clock_timestamp() WHERE attempt_id='22222222-2222-4222-8222-222222222222'`, `DELETE FROM public.shared_rate_buckets WHERE policy_id='oauth.failed_grant' AND key_digest=decode(repeat('22',32),'hex')`, `UPDATE public.shared_rate_partitions SET active_keys=active_keys-1 WHERE policy_id='oauth.failed_grant' AND partition=1`); err != nil {
		t.Fatal(err)
	}
	var retained int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM public.shared_admission_attempts WHERE state='terminal'`).Scan(&retained); err != nil || retained != 1 {
		t.Fatalf("terminal retained=%d error=%v", retained, err)
	}
}

func TestRuntimeSharedRateSchemaP22OverflowPendingWindowAndBounds(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	if err := membershipWrite(t, db, `UPDATE public.shared_rate_overflow SET window_started_at=transaction_timestamp() WHERE policy_id='oauth.failed_grant'`, pendingAttemptSQL("58000000-0000-4000-8000-000000000001", "overflow", "00", 1, nil)); err != nil {
		t.Fatal(err)
	}
	mismatchedWindow := map[string]string{"window_started_at": "transaction_timestamp()+interval '1 minute'", "reserved_at": "transaction_timestamp()+interval '1 minute'", "effective_until": "transaction_timestamp()+interval '16 minutes'"}
	requireRateError(t, membershipWrite(t, db, pendingAttemptSQL("58000000-0000-4000-8000-000000000002", "overflow", "00", 1, mismatchedWindow)), "23514", "shared_rate_state_exact", "")

	db, _ = runtimeSharedRateDB(t)
	statements := []string{`UPDATE public.shared_rate_overflow SET window_started_at=transaction_timestamp() WHERE policy_id='oauth.failed_grant'`}
	for i := 0; i < 9; i++ {
		statements = append(statements, pendingAttemptSQL(fmt.Sprintf("59000000-0000-4000-8000-%012d", i+1), "overflow", "00", 1, nil))
	}
	if err := membershipWrite(t, db, statements...); err != nil {
		t.Fatal(err)
	}
	if err := membershipWrite(t, db, pendingAttemptSQL("59000000-0000-4000-8000-000000000010", "overflow", "00", 1, storedPendingWindowOverrides("overflow", "00"))); err != nil {
		t.Fatalf("matching tenth overflow attempt: %v", err)
	}
	requireRateError(t, membershipWrite(t, db, pendingAttemptSQL("59000000-0000-4000-8000-000000000011", "overflow", "00", 1, storedPendingWindowOverrides("overflow", "00"))), "23514", "shared_rate_state_exact", "")

	db, _ = runtimeSharedRateDB(t)
	requireRateError(t, membershipWrite(t, db, `UPDATE public.shared_rate_overflow SET count=10,window_started_at=transaction_timestamp() WHERE policy_id='oauth.failed_grant'`, pendingAttemptSQL("59000000-0000-4000-8000-000000000012", "overflow", "00", 1, nil)), "23514", "shared_rate_state_exact", "")
}

func TestRuntimeSharedRateSchemaP22TenPendingAndUnrelatedClock(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	statements := []string{rateBucketSQL("oauth.failed_grant", "55", 1, map[string]string{"window_started_at": "transaction_timestamp()"})}
	for i := 0; i < 9; i++ {
		statements = append(statements, pendingAttemptSQL(fmt.Sprintf("55000000-0000-4000-8000-%012d", i+1), "private", "55", 1, nil))
	}
	if err := membershipWrite(t, db, statements...); err != nil {
		t.Fatal(err)
	}
	if err := membershipWrite(t, db, pendingAttemptSQL("55000000-0000-4000-8000-000000000010", "private", "55", 1, storedPendingWindowOverrides("private", "55"))); err != nil {
		t.Fatalf("matching tenth private attempt: %v", err)
	}
	requireRateError(t, membershipWrite(t, db, pendingAttemptSQL("55000000-0000-4000-8000-000000000011", "private", "55", 1, storedPendingWindowOverrides("private", "55"))), "23514", "shared_rate_state_exact", "")
	if err := membershipWrite(t, db, `UPDATE public.shared_policy_clocks SET high_water_at=high_water_at+interval '1 hour',last_raw_at=last_raw_at+interval '1 hour' WHERE policy_id='api.outer_request'`); err != nil {
		t.Fatalf("unrelated policy clock invalidated P22 debt: %v", err)
	}
	requireRateError(t, membershipWrite(t, db, `DELETE FROM public.shared_rate_buckets WHERE policy_id='oauth.failed_grant' AND key_digest=decode(repeat('55',32),'hex')`, `UPDATE public.shared_rate_partitions SET active_keys=active_keys-1 WHERE policy_id='oauth.failed_grant' AND partition=1`), "23514", "shared_rate_state_exact", "")
}

func TestRuntimeSharedRateSchemaP22CommittedPlusPendingBound(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	bucket := rateBucketSQL("oauth.failed_grant", "56", 1, map[string]string{"count": "10", "window_started_at": "transaction_timestamp()"})
	attempt := pendingAttemptSQL("56000000-0000-4000-8000-000000000001", "private", "56", 1, nil)
	requireRateError(t, membershipWrite(t, db, bucket, attempt), "23514", "shared_rate_state_exact", "")
}

func TestRuntimeSharedRateSchemaExpiredPendingRemainsBoundUntilResolved(t *testing.T) {
	db, ctx := runtimeSharedRateDB(t)
	window := "transaction_timestamp()-interval '20 minutes'"
	bucket := rateBucketSQL("oauth.failed_grant", "57", 1, map[string]string{"window_started_at": window})
	attempt := pendingAttemptSQL("57000000-0000-4000-8000-000000000001", "private", "57", 1, map[string]string{"window_started_at": window, "reserved_at": window, "effective_until": window + "+interval '15 minutes'"})
	if err := membershipWrite(t, db, bucket, attempt); err != nil {
		t.Fatal(err)
	}
	if err := membershipWrite(t, db, `UPDATE public.shared_policy_clocks SET high_water_at=high_water_at+interval '1 hour',last_raw_at=last_raw_at+interval '1 hour' WHERE policy_id='api.outer_request'`); err != nil {
		t.Fatal(err)
	}
	var pending int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM public.shared_admission_attempts WHERE state='pending' AND effective_until<clock_timestamp()`).Scan(&pending); err != nil || pending != 1 {
		t.Fatalf("expired stored pending=%d error=%v", pending, err)
	}
	requireRateError(t, membershipWrite(t, db, `UPDATE public.shared_rate_buckets SET count=0,window_started_at=NULL WHERE policy_id='oauth.failed_grant' AND key_digest=decode(repeat('57',32),'hex')`), "23514", "shared_rate_state_exact", "")
	if err := membershipWrite(t, db, `UPDATE public.shared_admission_attempts SET state='terminal',outcome='neutral',terminal_reason='window_expired',terminal_at=clock_timestamp() WHERE attempt_id='57000000-0000-4000-8000-000000000001'`, `DELETE FROM public.shared_rate_buckets WHERE policy_id='oauth.failed_grant' AND key_digest=decode(repeat('57',32),'hex')`, `UPDATE public.shared_rate_partitions SET active_keys=active_keys-1 WHERE policy_id='oauth.failed_grant' AND partition=1`); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeSharedRateSchemaOverflowAlgorithmMatrices(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	tests := []struct {
		name, policy, assignment, code, constraint, column string
	}{
		{"missing_policy", "api.outer_request", "policy_id=NULL", "23502", "", "policy_id"},
		{"missing_algorithm", "api.outer_request", "algorithm=NULL", "23502", "", "algorithm"},
		{"missing_last_seen", "api.outer_request", "last_seen=NULL", "23502", "", "last_seen"},
		{"token_missing_numerator", "api.outer_request", "token_numerator=NULL", "23514", "shared_rate_overflow_algorithm_shape_check", ""},
		{"token_missing_refill", "api.outer_request", "refill_at=NULL", "23514", "shared_rate_overflow_algorithm_shape_check", ""},
		{"token_window", "api.outer_request", "window_started_at=transaction_timestamp()", "23514", "shared_rate_overflow_algorithm_shape_check", ""},
		{"token_count", "api.outer_request", "count=0", "23514", "shared_rate_overflow_algorithm_shape_check", ""},
		{"token_events", "api.outer_request", "rolling_events=ARRAY[]::timestamptz[]", "23514", "shared_rate_overflow_algorithm_shape_check", ""},
		{"token_negative", "api.outer_request", "token_numerator=-1", "23514", "shared_rate_overflow_algorithm_shape_check", ""},
		{"token_above_full", "api.outer_request", "token_numerator=18000000001", "23514", "shared_rate_state_exact", ""},
		{"fixed_missing_count", "password.login_failure_email", "count=NULL", "23514", "shared_rate_overflow_algorithm_shape_check", ""},
		{"fixed_token", "password.login_failure_email", "token_numerator=0", "23514", "shared_rate_overflow_algorithm_shape_check", ""},
		{"fixed_refill", "password.login_failure_email", "refill_at=transaction_timestamp()", "23514", "shared_rate_overflow_algorithm_shape_check", ""},
		{"fixed_events", "password.login_failure_email", "rolling_events=ARRAY[]::timestamptz[]", "23514", "shared_rate_overflow_algorithm_shape_check", ""},
		{"fixed_negative", "password.login_failure_email", "count=-1,window_started_at=transaction_timestamp()", "23514", "shared_rate_overflow_algorithm_shape_check", ""},
		{"fixed_eleven", "password.login_failure_email", "count=11,window_started_at=transaction_timestamp()", "23514", "shared_rate_overflow_algorithm_shape_check", ""},
		{"p07_zero_window", "password.login_failure_email", "count=0,window_started_at=transaction_timestamp()", "23514", "shared_rate_state_exact", ""},
		{"p07_nonzero_missing_window", "password.login_failure_email", "count=1,window_started_at=NULL", "23514", "shared_rate_state_exact", ""},
		{"rolling_missing_count", "resume.slug_change", "count=NULL", "23514", "shared_rate_overflow_algorithm_shape_check", ""},
		{"rolling_missing_events", "resume.slug_change", "rolling_events=NULL", "23514", "shared_rate_overflow_algorithm_shape_check", ""},
		{"rolling_token", "resume.slug_change", "token_numerator=0", "23514", "shared_rate_overflow_algorithm_shape_check", ""},
		{"rolling_refill", "resume.slug_change", "refill_at=transaction_timestamp()", "23514", "shared_rate_overflow_algorithm_shape_check", ""},
		{"rolling_window", "resume.slug_change", "window_started_at=transaction_timestamp()", "23514", "shared_rate_overflow_algorithm_shape_check", ""},
		{"rolling_31", "resume.slug_change", "count=31,rolling_events=array_fill(transaction_timestamp(),ARRAY[31])", "23514", "shared_rate_overflow_algorithm_shape_check", ""},
		{"rolling_null", "resume.slug_change", "count=1,rolling_events=ARRAY[NULL]::timestamptz[]", "23514", "shared_rate_overflow_algorithm_shape_check", ""},
		{"rolling_reverse", "resume.slug_change", "count=2,rolling_events=ARRAY[transaction_timestamp(),transaction_timestamp()-interval '1 second']", "23514", "shared_rate_overflow_algorithm_shape_check", ""},
		{"rolling_2d", "resume.slug_change", "count=4,rolling_events=ARRAY[[transaction_timestamp(),transaction_timestamp()],[transaction_timestamp(),transaction_timestamp()]]", "23514", "shared_rate_overflow_algorithm_shape_check", ""},
		{"rolling_count_mismatch", "resume.slug_change", "count=1,rolling_events=ARRAY[]::timestamptz[]", "23514", "shared_rate_overflow_algorithm_shape_check", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			statement := fmt.Sprintf(`UPDATE public.shared_rate_overflow SET %s WHERE policy_id='%s'`, test.assignment, test.policy)
			requireRateError(t, membershipWrite(t, db, statement), test.code, test.constraint, test.column)
		})
	}
	for _, count := range []int{0, 1, 29, 30} {
		events := "ARRAY[]::timestamptz[]"
		if count > 0 {
			events = fmt.Sprintf("array_fill(transaction_timestamp(),ARRAY[%d])", count)
		}
		if err := membershipWrite(t, db, fmt.Sprintf(`UPDATE public.shared_rate_overflow SET count=%d,rolling_events=%s WHERE policy_id='resume.slug_change'`, count, events)); err != nil {
			t.Fatalf("valid rolling overflow count %d: %v", count, err)
		}
	}
	if err := membershipWrite(t, db, `UPDATE public.shared_rate_overflow SET count=1,window_started_at=transaction_timestamp() WHERE policy_id='password.login_failure_email'`); err != nil {
		t.Fatal(err)
	}
	if err := membershipWrite(t, db, `UPDATE public.shared_rate_overflow SET count=10 WHERE policy_id='password.login_failure_email'`); err != nil {
		t.Fatal(err)
	}
	if err := membershipWrite(t, db, `UPDATE public.shared_rate_overflow SET count=0,window_started_at=NULL WHERE policy_id='password.login_failure_email'`); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeSharedRateSchemaAttemptTerminalOutcomeMatrix(t *testing.T) {
	tests := []struct {
		name, kind, outcome, reason, terminal string
		allowed                               bool
	}{
		{"private_failure", "private", "failure", "caller_finish", "transaction_timestamp()", true},
		{"private_neutral", "private", "neutral", "caller_finish", "transaction_timestamp()", true},
		{"overflow_success", "overflow", "success", "caller_finish", "transaction_timestamp()", true},
		{"private_clear", "private", "neutral", "private_success_clear", "transaction_timestamp()", true},
		{"expiry", "overflow", "neutral", "window_expired", "transaction_timestamp()+interval '15 minutes'", true},
		{"clear_overflow", "overflow", "neutral", "private_success_clear", "transaction_timestamp()", false},
		{"expiry_failure", "overflow", "failure", "window_expired", "transaction_timestamp()+interval '15 minutes'", false},
		{"expiry_early", "overflow", "neutral", "window_expired", "transaction_timestamp()", false},
	}
	for i, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, _ := runtimeSharedRateDB(t)
			digest := fmt.Sprintf("%02x", 90+i)
			statements := []string{}
			attempt := pendingAttemptSQL(fmt.Sprintf("66000000-0000-4000-8000-%012d", i+1), test.kind, digest, 1, map[string]string{"state": "'terminal'", "outcome": "'" + test.outcome + "'", "terminal_reason": "'" + test.reason + "'", "terminal_at": test.terminal})
			err := membershipWrite(t, db, append(statements, attempt)...)
			if test.allowed {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				requireRateError(t, err, "23514", "shared_admission_attempts_result_shape_check", "")
			}
		})
	}
}

func TestRuntimeSharedRateSchemaAttemptNullableAndOutcomeMatrices(t *testing.T) {
	invalid := []struct {
		name               string
		overrides          map[string]string
		constraint, column string
	}{
		{"nil_attempt", map[string]string{"attempt_id": "'00000000-0000-0000-0000-000000000000'"}, "shared_admission_attempts_id_non_nil", ""},
		{"nil_client", map[string]string{"client_id": "'00000000-0000-0000-0000-000000000000'"}, "shared_admission_attempts_client_non_nil", ""},
		{"private_missing_partition", map[string]string{"bucket_kind": "'private'", "partition": "NULL", "key_digest": "decode(repeat('44',32),'hex')"}, "shared_admission_attempts_scope_shape_check", ""},
		{"private_missing_digest", map[string]string{"bucket_kind": "'private'", "partition": "1", "key_digest": "NULL"}, "shared_admission_attempts_scope_shape_check", ""},
		{"overflow_digest", map[string]string{"key_digest": "decode(repeat('44',32),'hex')"}, "shared_admission_attempts_scope_shape_check", ""},
		{"overflow_partition", map[string]string{"partition": "1"}, "shared_admission_attempts_scope_shape_check", ""},
		{"pending_outcome", map[string]string{"outcome": "'neutral'"}, "shared_admission_attempts_result_shape_check", ""},
		{"pending_reason", map[string]string{"terminal_reason": "'caller_finish'"}, "shared_admission_attempts_result_shape_check", ""},
		{"pending_terminal_at", map[string]string{"terminal_at": "transaction_timestamp()"}, "shared_admission_attempts_result_shape_check", ""},
		{"wrong_expiry", map[string]string{"effective_until": "transaction_timestamp()+interval '14 minutes'"}, "shared_admission_attempts_time_check", ""},
	}
	for i, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			db, _ := runtimeSharedRateDB(t)
			requireRateError(t, membershipWrite(t, db, pendingAttemptSQL(fmt.Sprintf("40000000-0000-4000-8000-%012d", i+1), "overflow", "44", 1, test.overrides)), "23514", test.constraint, test.column)
		})
	}
}

func TestRuntimeSharedRateSchemaTerminalEveryResultFieldRequired(t *testing.T) {
	for i, field := range []string{"outcome", "terminal_reason", "terminal_at"} {
		t.Run(field, func(t *testing.T) {
			db, _ := runtimeSharedRateDB(t)
			overrides := map[string]string{"state": "'terminal'", "outcome": "'neutral'", "terminal_reason": "'caller_finish'", "terminal_at": "transaction_timestamp()", field: "NULL"}
			err := membershipWrite(t, db, pendingAttemptSQL(fmt.Sprintf("79000000-0000-4000-8000-%012d", i+1), "overflow", "79", 1, overrides))
			requireRateError(t, err, "23514", "shared_admission_attempts_result_shape_check", "")
		})
	}
}

func TestRuntimeSharedRateSchemaAttemptEveryRequiredColumnRejectsNull(t *testing.T) {
	fields := []string{"attempt_id", "policy_id", "client_id", "bucket_kind", "window_started_at", "reserved_at", "effective_until", "state"}
	for i, field := range fields {
		t.Run(field, func(t *testing.T) {
			db, _ := runtimeSharedRateDB(t)
			err := membershipWrite(t, db, pendingAttemptSQL(fmt.Sprintf("77000000-0000-4000-8000-%012d", i+1), "overflow", "77", 1, map[string]string{field: "NULL"}))
			requireRateError(t, err, "23502", "", field)
		})
	}
}

func TestRuntimeSharedRateSchemaTerminalAttemptIsImmutable(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	terminal := pendingAttemptSQL("78000000-0000-4000-8000-000000000001", "overflow", "78", 1, map[string]string{"state": "'terminal'", "outcome": "'neutral'", "terminal_reason": "'caller_finish'", "terminal_at": "transaction_timestamp()"})
	if err := membershipWrite(t, db, terminal); err != nil {
		t.Fatal(err)
	}
	requireRateError(t, membershipWrite(t, db, `UPDATE public.shared_admission_attempts SET outcome='failure' WHERE attempt_id='78000000-0000-4000-8000-000000000001'`), "55000", "", "")
	requireRateError(t, membershipWrite(t, db, `UPDATE public.shared_admission_attempts SET state='pending',outcome=NULL,terminal_reason=NULL,terminal_at=NULL WHERE attempt_id='78000000-0000-4000-8000-000000000001'`), "55000", "", "")
	if err := membershipWrite(t, db, `DELETE FROM public.shared_admission_attempts WHERE attempt_id='78000000-0000-4000-8000-000000000001'`); err != nil {
		t.Fatalf("terminal receipt deletion rejected: %v", err)
	}
}

func TestRuntimeSharedRateSchemaPrivilegesEntryAndCatalog(t *testing.T) {
	db, ctx := runtimeSharedRateDB(t)
	roles := []string{"aboutme_app", "aboutme_maintenance", "aboutme_lifecycle_command", "aboutme_fencing_proof", "aboutme_restore_verify", "aboutme_migrator"}
	for _, role := range roles {
		for _, table := range runtimeSharedRateTables {
			var anyPrivilege bool
			if err := db.QueryRowContext(ctx, `SELECT has_table_privilege($1,'public.'||$2,'SELECT,INSERT,UPDATE,DELETE,TRUNCATE')`, role, table).Scan(&anyPrivilege); err != nil || anyPrivilege {
				t.Errorf("%s %s privilege=%t error=%v", role, table, anyPrivilege, err)
			}
		}
	}
	var owners, entries, truncates, columnACLs int
	if err := db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM pg_class WHERE relnamespace='public'::regnamespace AND relname=ANY($1) AND pg_get_userbyid(relowner)='aboutme_runtime_owner'),(SELECT count(*) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid WHERE c.relname=ANY($1) AND t.tgname LIKE '%_write_entry' AND t.tgfoid='public.runtime_assert_write_entry()'::regprocedure AND t.tgtype=62),(SELECT count(*) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid WHERE c.relname=ANY($1) AND t.tgname LIKE '%_no_truncate' AND t.tgfoid='public.runtime_reject_rate_truncate()'::regprocedure AND t.tgtype=34),(SELECT count(*) FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid WHERE c.relname=ANY($1) AND a.attnum>0 AND a.attacl IS NOT NULL)`, runtimeSharedRateTables).Scan(&owners, &entries, &truncates, &columnACLs); err != nil || owners != 7 || entries != 7 || truncates != 7 || columnACLs != 0 {
		t.Fatalf("catalog owners/entry/truncate/columnACL=%d/%d/%d/%d error=%v", owners, entries, truncates, columnACLs, err)
	}
	var helpersValid bool
	if err := db.QueryRowContext(ctx, `SELECT count(*)=10 AND bool_and(pg_get_userbyid(proowner)='aboutme_runtime_owner' AND prosecdef AND proconfig=ARRAY['search_path=pg_catalog']::text[] AND NOT has_function_privilege('public',oid,'EXECUTE')) FROM pg_proc WHERE pronamespace='public'::regnamespace AND proname=ANY(ARRAY['runtime_rate_events_sorted','runtime_reject_rate_catalog_mutation','runtime_reject_rate_truncate','runtime_validate_policy_clock','runtime_validate_rate_partition','runtime_validate_rate_bucket','runtime_validate_rate_overflow','runtime_validate_admission_attempt','runtime_assert_rate_partition_count','runtime_assert_rate_state'])`).Scan(&helpersValid); err != nil || !helpersValid {
		t.Fatalf("helper catalog valid=%t error=%v", helpersValid, err)
	}
	var attemptIndexes int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_indexes WHERE schemaname='public' AND tablename='shared_admission_attempts' AND ((indexname='shared_admission_attempts_pending_private_idx' AND indexdef LIKE '%(policy_id, key_digest, attempt_id)%state%pending%bucket_kind%private%') OR (indexname='shared_admission_attempts_pending_overflow_idx' AND indexdef LIKE '%(policy_id, attempt_id)%state%pending%bucket_kind%overflow%') OR (indexname='shared_admission_attempts_pending_expiry_idx' AND indexdef LIKE '%(effective_until, attempt_id)%state%pending%') OR (indexname='shared_admission_attempts_terminal_idx' AND indexdef LIKE '%(terminal_at, attempt_id)%state%terminal%'))`).Scan(&attemptIndexes); err != nil || attemptIndexes != 4 {
		t.Fatalf("attempt partial indexes=%d error=%v", attemptIndexes, err)
	}
	var expiryIndex int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_indexes WHERE schemaname='public' AND tablename='shared_rate_buckets' AND indexname='shared_rate_buckets_expiry_idx' AND indexdef LIKE '%(policy_id, last_seen, key_digest)%'`).Scan(&expiryIndex); err != nil || expiryIndex != 1 {
		t.Fatalf("bucket expiry index=%d error=%v", expiryIndex, err)
	}
	var deferredAssertions int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid WHERE c.relnamespace='public'::regnamespace AND c.relname=ANY($1) AND t.tgdeferrable AND t.tginitdeferred AND t.tgfoid IN ('public.runtime_assert_rate_partition_count()'::regprocedure,'public.runtime_assert_rate_state()'::regprocedure)`, runtimeSharedRateTables).Scan(&deferredAssertions); err != nil || deferredAssertions != 5 {
		t.Fatalf("deferred rate assertions=%d error=%v", deferredAssertions, err)
	}
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
	for _, role := range roles {
		if _, err = conn.ExecContext(ctx, `SET SESSION AUTHORIZATION `+role); err != nil {
			t.Fatal(err)
		}
		_, err = conn.ExecContext(ctx, `SELECT * FROM public.shared_rate_policies`)
		requireRateError(t, err, "42501", "", "")
		_, err = conn.ExecContext(ctx, `DELETE FROM public.shared_rate_buckets`)
		requireRateError(t, err, "42501", "", "")
		_, err = conn.ExecContext(ctx, `SELECT public.runtime_rate_events_sorted(ARRAY[]::timestamptz[])`)
		requireRateError(t, err, "42501", "", "")
		_, err = conn.ExecContext(ctx, `SELECT public.runtime_assert_rate_state()`)
		requireRateError(t, err, "42501", "", "")
		if _, err = conn.ExecContext(ctx, `RESET SESSION AUTHORIZATION`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = conn.ExecContext(ctx, `SET SESSION AUTHORIZATION aboutme_runtime_owner`); err != nil {
		t.Fatal(err)
	}
	_, err = conn.ExecContext(ctx, `UPDATE public.shared_policy_clocks SET last_raw_at=clock_timestamp() WHERE policy_id='api.outer_request'`)
	requireRateError(t, err, "AM001", "", "")
	if _, err = conn.ExecContext(ctx, `RESET SESSION AUTHORIZATION`); err != nil {
		t.Fatal(err)
	}
	if err = membershipWrite(t, db, `SET LOCAL search_path=pg_temp,public`, `UPDATE public.shared_policy_clocks SET last_raw_at=clock_timestamp() WHERE policy_id='api.outer_request'`); err != nil {
		t.Fatalf("hostile search path redirected owner helper: %v", err)
	}
}

func TestRuntimeSharedRateSchemaAllTablesRejectTruncate(t *testing.T) {
	db, _ := runtimeSharedRateDB(t)
	for _, table := range runtimeSharedRateTables {
		t.Run(table, func(t *testing.T) {
			requireRateError(t, membershipWrite(t, db, `TRUNCATE public.`+table+` CASCADE`), "AM001", "", "")
		})
	}
	requireRateError(t, membershipWrite(t, db, `TRUNCATE public.shared_rate_policies CASCADE`), "AM001", "", "")
}

func TestRuntimeSharedRateSchemaPreservesPopulatedVersion17(t *testing.T) {
	db := newMigratedTestDatabase(t, 17)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `INSERT INTO public.users(id,email,name) VALUES('18181818-1818-4818-8818-181818181818','rate-preserve@example.test','rate preserved')`); err != nil {
		t.Fatal(err)
	}
	preserved := append(sharedClaimVectorFixture(), transitionFixture()...)
	preserved = append(preserved, transitionAckSQL(initiatorID), `INSERT INTO public.runtime_lifecycle_operations(operation_id,workflow_kind) VALUES('rate-preserve-lifecycle','initial_serving')`)
	if err := membershipWrite(t, db, preserved...); err != nil {
		t.Fatal(err)
	}
	before, capacityBefore, controllerBefore := int64(0), int64(0), int64(0)
	if err := db.QueryRowContext(ctx, `SELECT s.generation,c.generation,c.controller_generation FROM public.runtime_write_state s CROSS JOIN public.runtime_capacity c WHERE s.singleton AND c.singleton`).Scan(&before, &capacityBefore, &controllerBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := applyFS(ctx, db, runtimeTransitionFixtureFS(t, 18), LocalAdminMigratorIdentity()); err != nil {
		t.Fatal(err)
	}
	var after int64
	var name string
	var claims, scopes, summaries, transitions, targets, required, acks, lifecycle int
	var capacityAfter, controllerAfter int64
	if err := db.QueryRowContext(ctx, `SELECT s.generation,u.name,c.generation,c.controller_generation,(SELECT count(*) FROM public.shared_claim_requests),(SELECT count(*) FROM public.shared_claim_scopes),(SELECT count(*) FROM public.shared_claim_scope_summaries),(SELECT count(*) FROM public.public_transitions),(SELECT count(*) FROM public.public_transition_targets),(SELECT count(*) FROM public.public_transition_replicas),(SELECT count(*) FROM public.public_transition_acks),(SELECT count(*) FROM public.runtime_lifecycle_operations WHERE operation_id='rate-preserve-lifecycle') FROM public.runtime_write_state s CROSS JOIN public.runtime_capacity c CROSS JOIN public.users u WHERE s.singleton AND c.singleton AND u.id='18181818-1818-4818-8818-181818181818'`).Scan(&after, &name, &capacityAfter, &controllerAfter, &claims, &scopes, &summaries, &transitions, &targets, &required, &acks, &lifecycle); err != nil || after != before+1 || name != "rate preserved" || capacityAfter != capacityBefore || controllerAfter != controllerBefore || claims != 1 || scopes != 2 || summaries != 2 || transitions != 1 || targets != 2 || required != 1 || acks != 1 || lifecycle != 1 {
		t.Fatalf("generation=%d/%d capacity=%d/%d controller=%d/%d name=%q claims=%d/%d/%d transition=%d/%d/%d/%d lifecycle=%d error=%v", before, after, capacityBefore, capacityAfter, controllerBefore, controllerAfter, name, claims, scopes, summaries, transitions, targets, required, acks, lifecycle, err)
	}
}

func TestRuntimeSharedRateSchemaFailedMigrationRollsBackObjectsAndGeneration(t *testing.T) {
	db := newMigratedTestDatabase(t, 17)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var before int64
	if err := db.QueryRowContext(ctx, `SELECT generation FROM public.runtime_write_state WHERE singleton`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	fixture := runtimeTransitionFixtureFS(t, 18)
	file := fixture["00018_runtime_shared_rate_schema.sql"]
	anchor := "RESET ROLE;\nREVOKE CREATE"
	if !strings.Contains(string(file.Data), anchor) {
		t.Fatal("migration 18 failure anchor is missing")
	}
	file.Data = []byte(strings.Replace(string(file.Data), anchor, "SELECT 1/0;\n"+anchor, 1))
	_, applyErr := applyFS(ctx, db, fixture, LocalAdminMigratorIdentity())
	requireRateError(t, applyErr, "22012", "", "")
	var after int64
	var objects int
	if err := db.QueryRowContext(ctx, `SELECT generation,(SELECT count(*) FROM pg_class WHERE relnamespace='public'::regnamespace AND relname=ANY($1)) FROM public.runtime_write_state WHERE singleton`, runtimeSharedRateTables).Scan(&after, &objects); err != nil || after != before || objects != 0 {
		t.Fatalf("rollback generation=%d/%d objects=%d error=%v", before, after, objects, err)
	}
}

func TestRuntimeSharedRateSchemaObjectsExist(t *testing.T) {
	db, ctx := runtimeSharedRateDB(t)
	for _, name := range runtimeSharedRateTables {
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT to_regclass('public.' || $1) IS NOT NULL`, name).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if !exists {
			t.Errorf("table %s is missing", name)
		}
	}
}
