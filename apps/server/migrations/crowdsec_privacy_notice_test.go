package migrations_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/migrations"
)

func TestPrivacyNoticeLedgerColumns(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	for table, want := range map[string][]string{
		"privacy_notice_campaigns":  {"id", "content_sha256", "cutoff_at", "expected_count", "capture_cursor", "state", "sealed_at", "first_activated_at", "activated_at", "aborted_at", "retired_at", "purged_at", "activation_count", "accepted_count", "deleted_count", "last_accepted_at", "recovery_count", "recovery_expired_count", "recovery_attempts_exhausted_count", "recovery_provider_rejected_count", "recovery_payload_invalid_count", "recovery_job_missing_count", "recovery_delivery_failed_count"},
		"privacy_notice_recipients": {"id", "campaign_id", "user_id", "job_id", "state", "generation", "queued_at", "accepted_at", "deleted_at", "failed_at", "cancelled_at", "failure_code"}, //nolint:misspell // Exact database column spelling.
	} {
		rows, err := tx.Query(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema='public' AND table_name=$1 ORDER BY ordinal_position`, table)
		if err != nil {
			t.Fatal("column query failed")
		}
		var got []string
		for rows.Next() {
			var name string
			if rows.Scan(&name) != nil {
				t.Fatal("column scan failed")
			}
			got = append(got, name)
		}
		rows.Close()
		if rows.Err() != nil || !reflect.DeepEqual(got, want) {
			t.Fatal("ledger column set differs from contract")
		}
	}
}

func TestPrivacyNoticeLedgerConstraintsAndDeletion(t *testing.T) {
	t.Parallel()
	tx, ctx := newResumeSchemaTx(t)
	const campaign = "crowdsec-postgres-paths-v1"
	if _, err := tx.Exec(ctx, `INSERT INTO privacy_notice_campaigns(id,content_sha256,cutoff_at,expected_count,state) VALUES ($1,$2,now(),1,'capturing')`, campaign, make([]byte, 32)); err != nil {
		t.Fatal("campaign insert failed")
	}
	for _, query := range []string{
		`UPDATE privacy_notice_campaigns SET id='other'`,
		`UPDATE privacy_notice_campaigns SET content_sha256=decode('00','hex')`,
		`UPDATE privacy_notice_campaigns SET expected_count=-1`,
		`UPDATE privacy_notice_campaigns SET state='sealed'`,
		`UPDATE privacy_notice_campaigns SET purged_at=now()`,
		`UPDATE privacy_notice_campaigns SET accepted_count=1`,
		`UPDATE privacy_notice_campaigns SET recovery_count=1`,
		`UPDATE privacy_notice_campaigns SET recovery_expired_count=-1`,
		`UPDATE privacy_notice_campaigns SET activation_count=1`,
		`UPDATE privacy_notice_campaigns SET activated_at=now()`,
	} {
		if withSavepoint(ctx, t, tx, func(sp pgx.Tx) error { _, err := sp.Exec(ctx, query); return err }) == nil {
			t.Fatal("invalid campaign state accepted")
		}
	}
	var user uuid.UUID
	if tx.QueryRow(ctx, `INSERT INTO users(email,name) VALUES ($1,'Fixture') RETURNING id`, uuid.NewString()+"@example.com").Scan(&user) != nil {
		t.Fatal("user insert failed")
	}
	recipient, job := uuid.New(), uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO privacy_notice_recipients(id,campaign_id,user_id,job_id,state) VALUES ($1,$2,$3,$4,'unqueued')`, recipient, campaign, user, job); err != nil {
		t.Fatal("recipient insert failed")
	}
	for _, query := range []string{
		`UPDATE privacy_notice_recipients SET state='accepted'`,
		`UPDATE privacy_notice_recipients SET state='deleted',deleted_at=now()`,
		`UPDATE privacy_notice_recipients SET generation=-1`,
		`UPDATE privacy_notice_recipients SET state='cancelled'`,
		`UPDATE privacy_notice_recipients SET state='failed',failed_at=now(),failure_code='other'`,
		`UPDATE privacy_notice_recipients SET state='accepted',accepted_at=now(),failure_code='expired'`,
		`DELETE FROM privacy_notice_campaigns`,
	} {
		if withSavepoint(ctx, t, tx, func(sp pgx.Tx) error { _, err := sp.Exec(ctx, query); return err }) == nil {
			t.Fatal("invalid recipient state accepted")
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id=$1`, user); err != nil {
		t.Fatal("user deletion failed")
	}
	var retained bool
	if tx.QueryRow(ctx, `SELECT user_id IS NULL AND job_id=$2 FROM privacy_notice_recipients WHERE id=$1`, recipient, job).Scan(&retained) != nil || !retained {
		t.Fatal("deletion lost lifetime ledger evidence")
	}
	var indexes int
	if tx.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE tablename='privacy_notice_recipients' AND (indexdef LIKE '%(campaign_id, state, id)%' OR indexdef LIKE '%(user_id)%')`).Scan(&indexes) != nil || indexes != 2 {
		t.Fatal("recipient lookup indexes missing")
	}
}

func TestPrivacyNoticeDownPreservesLifetimeEvidence(t *testing.T) {
	t.Parallel()
	db := openTestDB(t, newTestDatabase(t))
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	provider, err := migrations.NewProvider(db, migrations.FS)
	if err != nil {
		t.Fatal("migration provider failed")
	}
	if _, err := provider.UpTo(ctx, 13); err != nil {
		t.Fatal("migration up failed")
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO privacy_notice_campaigns(id,content_sha256,cutoff_at,expected_count,state) VALUES ('crowdsec-postgres-paths-v1',$1,now(),0,'capturing')`, make([]byte, 32)); err != nil {
		t.Fatal("campaign insert failed")
	}
	if _, err := provider.Down(ctx); err == nil {
		t.Fatal("down migration erased lifetime campaign evidence")
	}
}
