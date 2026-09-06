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

func singleClaimDigestSQL(claimID, replicaID, policyID, kind, scopeDigestSQL string, workID *string) string {
	return singleClaimDigestCustomSQL(claimID, replicaID, policyID, 1, kind, scopeDigestSQL, workID)
}

func singleClaimDigestCustomSQL(claimID, replicaID, policyID string, ordinal int, kind, scopeDigestSQL string, workID *string) string {
	workFrame := `decode('00','hex')`
	if workID != nil {
		workFrame = fmt.Sprintf(`decode('01','hex')||uuid_send('%s'::uuid)`, *workID)
	}
	return fmt.Sprintf(`sha256(convert_to('aboutme.shared-claim.request.v1','UTF8')||decode('00','hex')||decode('01','hex')||uuid_send('%s'::uuid)||decode(lpad(to_hex(%d),2,'0'),'hex')||convert_to('%s','UTF8')||uuid_send('%s'::uuid)||%s||decode('01','hex')||decode(lpad(to_hex(%d),2,'0'),'hex')||decode(lpad(to_hex(%d),2,'0'),'hex')||convert_to('%s','UTF8')||%s)`, claimID, len(policyID), policyID, replicaID, workFrame, ordinal, len(kind), kind, scopeDigestSQL)
}

func singleClaimSQL(claimID, replicaID, policyID, kind, scopeDigestSQL, state string, allocation int, workID *string) (string, string) {
	workValue, deadline := "NULL", "NULL"
	if workID != nil {
		workValue = "'" + *workID + "'"
		deadline = "transaction_timestamp()+interval '20 seconds'"
	}
	digest := singleClaimDigestSQL(claimID, replicaID, policyID, kind, scopeDigestSQL, workID)
	parent := fmt.Sprintf(`INSERT INTO public.shared_claim_requests(claim_id,policy_id,replica_id,work_id,state,admitted_at,deadline_at,scope_count,request_digest) VALUES('%s','%s','%s',%s,'%s',transaction_timestamp(),%s,1,%s)`, claimID, policyID, replicaID, workValue, state, deadline, digest)
	child := fmt.Sprintf(`INSERT INTO public.shared_claim_scopes(claim_id,request_ordinal,policy_id,scope_kind,scope_digest,allocation_ordinal,state) VALUES('%s',1,'%s','%s',%s,%d,'%s')`, claimID, policyID, kind, scopeDigestSQL, allocation, state)
	return parent, child
}

func dualClaimDigestSQL(claimID, replicaID, ipDigestSQL, accountDigestSQL string) string {
	return twoScopeClaimDigestSQL(claimID, replicaID, "ip", ipDigestSQL, "account", accountDigestSQL)
}

func twoScopeClaimDigestSQL(claimID, replicaID, kind1, digest1SQL, kind2, digest2SQL string) string {
	return fmt.Sprintf(`sha256(convert_to('aboutme.shared-claim.request.v1','UTF8')||decode('00','hex')||decode('01','hex')||uuid_send('%s'::uuid)||decode('14','hex')||convert_to('sse.fleet_account_ip','UTF8')||uuid_send('%s'::uuid)||decode('00','hex')||decode('02','hex')||decode('01','hex')||decode(lpad(to_hex(%d),2,'0'),'hex')||convert_to('%s','UTF8')||%s||decode('02','hex')||decode(lpad(to_hex(%d),2,'0'),'hex')||convert_to('%s','UTF8')||%s)`, claimID, replicaID, len(kind1), kind1, digest1SQL, len(kind2), kind2, digest2SQL)
}

func capacityClaimID(index int) string {
	return fmt.Sprintf("10000000-0000-4000-8000-%012x", index+1)
}

func capacityWorkID(index int) string {
	return fmt.Sprintf("20000000-0000-4000-8000-%012x", index+1)
}

type claimPolicyBoundary struct {
	name, policy, kind, digestByte string
	running, waiting               int
}

var claimPolicyBoundaries = []claimPolicyBoundary{
	{"render_global", "render.global_claim", "global", "10", 1, 8},
	{"password_global", "password.hash", "global", "20", 2, 16},
	{"mail_global", "mail.send", "global", "30", 2, 0},
	{"mcp_user", "mcp.user_concurrent", "user", "40", 4, 0},
	{"sse_ip", "sse.fleet_account_ip", "ip", "50", 100, 0},
	{"sse_account", "sse.fleet_account_ip", "account", "60", 20, 0},
}

func sharedClaimCapacityFixture(boundary claimPolicyBoundary, state string, count int) []string {
	fixture := replicaInsert(claimVectorReplica, "i-00000000000000002", "2")
	running, waiting := 0, 0
	if state == "running" {
		running = count
	} else {
		waiting = count
	}
	commonDigest := fmt.Sprintf(`decode(repeat('%s',32),'hex')`, boundary.digestByte)
	fixture = append(fixture, fmt.Sprintf(`INSERT INTO public.shared_claim_scope_summaries(policy_id,scope_kind,scope_digest,running_count,waiting_count,next_ordinal,updated_at) VALUES('%s','%s',%s,%d,%d,%d,transaction_timestamp())`, boundary.policy, boundary.kind, commonDigest, running, waiting, count+1))
	for i := 0; i < count; i++ {
		claimID := capacityClaimID(i)
		if boundary.kind == "account" {
			ipDigest := fmt.Sprintf(`decode(lpad(to_hex(%d),64,'0'),'hex')`, i+1)
			fixture = append(fixture,
				fmt.Sprintf(`INSERT INTO public.shared_claim_scope_summaries(policy_id,scope_kind,scope_digest,running_count,waiting_count,next_ordinal,updated_at) VALUES('sse.fleet_account_ip','ip',%s,%d,%d,2,transaction_timestamp())`, ipDigest, boolInt(state == "running"), boolInt(state == "waiting")),
				fmt.Sprintf(`INSERT INTO public.shared_claim_requests(claim_id,policy_id,replica_id,state,admitted_at,scope_count,request_digest) VALUES('%s','sse.fleet_account_ip','%s','%s',transaction_timestamp(),2,%s)`, claimID, claimVectorReplica, state, dualClaimDigestSQL(claimID, claimVectorReplica, ipDigest, commonDigest)),
				fmt.Sprintf(`INSERT INTO public.shared_claim_scopes(claim_id,request_ordinal,policy_id,scope_kind,scope_digest,allocation_ordinal,state) VALUES('%s',1,'sse.fleet_account_ip','ip',%s,1,'%s'),('%s',2,'sse.fleet_account_ip','account',%s,%d,'%s')`, claimID, ipDigest, state, claimID, commonDigest, i+1, state),
			)
			continue
		}
		var workID *string
		if boundary.policy == "render.global_claim" {
			value := capacityWorkID(i)
			workID = &value
		}
		parent, child := singleClaimSQL(claimID, claimVectorReplica, boundary.policy, boundary.kind, commonDigest, state, i+1, workID)
		fixture = append(fixture, parent, child)
	}
	return fixture
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func sharedClaimCapacityOverflow(boundary claimPolicyBoundary, state string, allocation int) []string {
	claimID := capacityClaimID(allocation + 1000)
	commonDigest := fmt.Sprintf(`decode(repeat('%s',32),'hex')`, boundary.digestByte)
	if boundary.kind == "account" {
		ipDigest := fmt.Sprintf(`decode(lpad(to_hex(%d),64,'0'),'hex')`, allocation+1000)
		return []string{
			fmt.Sprintf(`INSERT INTO public.shared_claim_scope_summaries(policy_id,scope_kind,scope_digest,running_count,waiting_count,next_ordinal,updated_at) VALUES('sse.fleet_account_ip','ip',%s,%d,%d,2,transaction_timestamp())`, ipDigest, boolInt(state == "running"), boolInt(state == "waiting")),
			fmt.Sprintf(`INSERT INTO public.shared_claim_requests(claim_id,policy_id,replica_id,state,admitted_at,scope_count,request_digest) VALUES('%s','sse.fleet_account_ip','%s','%s',transaction_timestamp(),2,%s)`, claimID, claimVectorReplica, state, dualClaimDigestSQL(claimID, claimVectorReplica, ipDigest, commonDigest)),
			fmt.Sprintf(`INSERT INTO public.shared_claim_scopes(claim_id,request_ordinal,policy_id,scope_kind,scope_digest,allocation_ordinal,state) VALUES('%s',1,'sse.fleet_account_ip','ip',%s,1,'%s'),('%s',2,'sse.fleet_account_ip','account',%s,%d,'%s')`, claimID, ipDigest, state, claimID, commonDigest, allocation, state),
			fmt.Sprintf(`UPDATE public.shared_claim_scope_summaries SET %s_count=%s_count+1,next_ordinal=next_ordinal+1,updated_at=transaction_timestamp() WHERE policy_id='%s' AND scope_kind='%s' AND scope_digest=%s`, state, state, boundary.policy, boundary.kind, commonDigest),
		}
	}
	var workID *string
	if boundary.policy == "render.global_claim" {
		value := capacityWorkID(allocation + 1000)
		workID = &value
	}
	parent, child := singleClaimSQL(claimID, claimVectorReplica, boundary.policy, boundary.kind, commonDigest, state, allocation, workID)
	return []string{parent, child, fmt.Sprintf(`UPDATE public.shared_claim_scope_summaries SET %s_count=%s_count+1,next_ordinal=next_ordinal+1,updated_at=transaction_timestamp() WHERE policy_id='%s' AND scope_kind='%s' AND scope_digest=%s`, state, state, boundary.policy, boundary.kind, commonDigest)}
}

func beginSharedClaimWrite(t *testing.T, db *sql.DB) (*sql.Conn, *sql.Tx) {
	t.Helper()
	return beginTransitionWrite(t, db)
}

func receiveSharedClaimRace(t *testing.T, cancel context.CancelFunc, result <-chan error) error {
	t.Helper()
	defer cancel()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		cancel()
		select {
		case err := <-result:
			t.Fatalf("timed out joining shared claim race: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("shared claim race goroutine did not join after cancellation")
		}
		return nil
	}
}

func finishSharedClaimWrite(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `RESET ROLE; SELECT public.runtime_finish_write()`); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

const (
	claimVectorID      = "00000000-0000-0000-0000-000000000001"
	claimVectorReplica = "00000000-0000-0000-0000-000000000002"
	claimVectorDigest  = "0733ec593ac2751902ebac6d8175187ea5c2e695f47264b36b1608a2576cbbac"
)

func sharedClaimVectorFixture() []string {
	fixture := replicaInsert(claimVectorReplica, "i-00000000000000002", "2")
	return append(fixture,
		`INSERT INTO public.shared_claim_scope_summaries(policy_id,scope_kind,scope_digest,running_count,waiting_count,next_ordinal,updated_at) VALUES('sse.fleet_account_ip','ip',decode(repeat('00',32),'hex'),1,0,2,transaction_timestamp()),('sse.fleet_account_ip','account',decode(repeat('ff',32),'hex'),1,0,2,transaction_timestamp())`,
		fmt.Sprintf(`INSERT INTO public.shared_claim_requests(claim_id,policy_id,replica_id,work_id,state,admitted_at,deadline_at,released_at,release_reason,scope_count,request_digest) VALUES('%s','sse.fleet_account_ip','%s',NULL,'running',transaction_timestamp(),NULL,NULL,NULL,2,decode('%s','hex'))`, claimVectorID, claimVectorReplica, claimVectorDigest),
		fmt.Sprintf(`INSERT INTO public.shared_claim_scopes(claim_id,request_ordinal,policy_id,scope_kind,scope_digest,allocation_ordinal,state) VALUES('%s',1,'sse.fleet_account_ip','ip',decode(repeat('00',32),'hex'),1,'running'),('%s',2,'sse.fleet_account_ip','account',decode(repeat('ff',32),'hex'),1,'running')`, claimVectorID, claimVectorID),
	)
}

func sharedClaimAccountOnlyFixture() []string {
	fixture := replicaInsert(claimVectorReplica, "i-00000000000000002", "2")
	digestExpression := `sha256(convert_to('aboutme.shared-claim.request.v1','UTF8')||decode('00','hex')||decode('01','hex')||uuid_send('00000000-0000-0000-0000-000000000001'::uuid)||decode('14','hex')||convert_to('sse.fleet_account_ip','UTF8')||uuid_send('00000000-0000-0000-0000-000000000002'::uuid)||decode('00','hex')||decode('01','hex')||decode('02','hex')||decode('07','hex')||convert_to('account','UTF8')||decode(repeat('ff',32),'hex'))`
	return append(fixture,
		`INSERT INTO public.shared_claim_scope_summaries(policy_id,scope_kind,scope_digest,running_count,waiting_count,next_ordinal,updated_at) VALUES('sse.fleet_account_ip','account',decode(repeat('ff',32),'hex'),1,0,2,transaction_timestamp())`,
		fmt.Sprintf(`INSERT INTO public.shared_claim_requests(claim_id,policy_id,replica_id,state,admitted_at,scope_count,request_digest) VALUES('%s','sse.fleet_account_ip','%s','running',transaction_timestamp(),1,%s)`, claimVectorID, claimVectorReplica, digestExpression),
		fmt.Sprintf(`INSERT INTO public.shared_claim_scopes(claim_id,request_ordinal,policy_id,scope_kind,scope_digest,allocation_ordinal,state) VALUES('%s',2,'sse.fleet_account_ip','account',decode(repeat('ff',32),'hex'),1,'running')`, claimVectorID),
	)
}

func replaceSharedClaimFixture(t *testing.T, old, replacement string) []string {
	t.Helper()
	fixture := sharedClaimVectorFixture()
	for i, statement := range fixture {
		if strings.Contains(statement, old) {
			fixture[i] = strings.Replace(statement, old, replacement, 1)
			return fixture
		}
	}
	t.Fatalf("fixture does not contain %q", old)
	return nil
}

func requireSharedClaimPGError(t *testing.T, err error, code, constraint, column string) {
	t.Helper()
	if err == nil {
		t.Fatal("invalid shared claim write succeeded")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != code || pgErr.ConstraintName != constraint || pgErr.ColumnName != column {
		t.Fatalf("error code=%q constraint=%q column=%q: %v", pgErrCode(pgErr), pgErrConstraint(pgErr), pgErrColumn(pgErr), err)
	}
}
