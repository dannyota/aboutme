package store_test

import (
	"strings"
	"testing"
	"time"

	"danny.vn/aboutme/apps/server/internal/store"
)

// PostgreSQL prefers a plan without disabled nodes over any cheaper one, so the plan does not depend on table size.
func TestAuthEmailJobQueries_UseMatchingIndexes(t *testing.T) {
	t.Parallel()
	ctx, _, tx, _ := newPasswordStoreTx(t)
	for _, setting := range []string{"enable_seqscan", "enable_bitmapscan", "enable_sort", "enable_incremental_sort"} {
		if _, err := tx.Exec(ctx, "SET LOCAL "+setting+" = off"); err != nil {
			t.Fatalf("SET LOCAL %s: %v", setting, err)
		}
	}

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		query string
		args  []any
		index string
	}{
		{"claim", store.ClaimAuthEmailJobsSQL, []any{"worker", now.Add(time.Minute), now, true, int32(10)}, "auth_email_jobs_claim_order_idx"},
		{"expired lease recovery", store.RequeueExpiredAuthEmailLeasesSQL, []any{now, true, int32(10)}, "auth_email_jobs_lease_expiry_idx"},
		{"finished cleanup", store.CleanupFinishedAuthEmailJobsSQL, []any{now, true, int32(10)}, "auth_email_jobs_finished_idx"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := tx.Query(ctx, "EXPLAIN (COSTS OFF)\n"+tc.query, tc.args...)
			if err != nil {
				t.Fatalf("EXPLAIN: %v", err)
			}
			defer rows.Close()
			var lines []string
			for rows.Next() {
				var line string
				if err := rows.Scan(&line); err != nil {
					t.Fatalf("scan plan line: %v", err)
				}
				lines = append(lines, line)
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("read plan: %v", err)
			}
			plan := strings.Join(lines, "\n")
			if !strings.Contains(plan, " using "+tc.index+" on auth_email_jobs") {
				t.Errorf("plan does not scan %s:\n%s", tc.index, plan)
			}
			if strings.Contains(plan, "Sort") {
				t.Errorf("plan sorts instead of reading index order:\n%s", plan)
			}
		})
	}
}
