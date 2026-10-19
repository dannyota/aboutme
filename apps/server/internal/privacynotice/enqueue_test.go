package privacynotice

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/authmail"
)

func prepareEnqueueFixture(t *testing.T, count int) (*Service, func()) {
	t.Helper()
	ctx, s := noticeTestService(t)
	if _, err := s.pool.Exec(ctx, `INSERT INTO users(email,name) SELECT 'fixture-'||n||'@example.com','Fixture' FROM generate_series(1,$1::integer) AS n`, count); err != nil {
		t.Fatal("enqueue users failed")
	}
	for i := 0; i <= count/100; i++ {
		if s.Prepare(ctx) != nil {
			t.Fatal("enqueue capture failed")
		}
		var sealed bool
		if s.pool.QueryRow(ctx, `SELECT state='sealed' FROM privacy_notice_campaigns`).Scan(&sealed) != nil {
			t.Fatal("enqueue campaign read failed")
		}
		if sealed {
			break
		}
	}
	var nonceBytes []byte
	for i := 0; i < count+2; i++ {
		nonceBytes = append(nonceBytes, bytes.Repeat([]byte{byte(i % 256)}, 12)...)
	}
	ring, err := authmail.NewKeyRing("fixture", map[string][32]byte{"fixture": {}}, bytes.NewReader(nonceBytes))
	if err != nil {
		t.Fatal("enqueue ring failed")
	}
	s.outbox, err = authmail.NewOutbox(ring, func() time.Time { return s.now() })
	if err != nil {
		t.Fatal("enqueue outbox failed")
	}
	authorizeNoticeFixture(t, s)
	return s, func() {
		if s.Enqueue(ctx) != nil {
			t.Fatal("enqueue invocation failed")
		}
	}
}

func TestEnqueueBoundsAndRepeatedCalls(t *testing.T) {
	s, enqueue := prepareEnqueueFixture(t, 205)
	enqueue()
	if !errors.Is(s.Enqueue(t.Context()), ErrQueueFull) {
		t.Fatal("full notice backlog accepted another batch")
	}
	var correct bool
	if s.pool.QueryRow(t.Context(), `SELECT (SELECT count(*)=100 FROM auth_email_jobs WHERE kind='privacy_notice') AND (SELECT count(*)=100 FROM privacy_notice_recipients WHERE state='queued') AND (SELECT count(*)=105 FROM privacy_notice_recipients WHERE state='unqueued')`).Scan(&correct) != nil || !correct {
		t.Fatal("enqueue batch or backlog bound differs")
	}
}

func TestEnqueueConcurrentCallsShareLogicalJobs(t *testing.T) {
	s, _ := prepareEnqueueFixture(t, 100)
	var wg sync.WaitGroup
	errorsFound := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); errorsFound <- s.Enqueue(t.Context()) }()
	}
	wg.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil && !errors.Is(err, ErrQueueFull) {
			t.Fatal("concurrent enqueue failed")
		}
	}
	var exact bool
	if s.pool.QueryRow(t.Context(), `SELECT (SELECT count(*)=100 FROM auth_email_jobs) AND NOT EXISTS(SELECT 1 FROM privacy_notice_recipients r LEFT JOIN auth_email_jobs j ON j.id=r.job_id WHERE j.id IS NULL)`).Scan(&exact) != nil || !exact {
		t.Fatal("concurrent enqueue duplicated or lost jobs")
	}
}

func TestEnqueueRollsBackOutboxWhenLedgerWriteFails(t *testing.T) {
	s, _ := prepareEnqueueFixture(t, 1)
	if _, err := s.pool.Exec(t.Context(), `CREATE FUNCTION reject_notice_queue() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture'; END $$; CREATE TRIGGER reject_notice_queue BEFORE UPDATE ON privacy_notice_recipients FOR EACH ROW EXECUTE FUNCTION reject_notice_queue()`); err != nil {
		t.Fatal("rollback fixture failed")
	}
	if s.Enqueue(t.Context()) == nil {
		t.Fatal("failed ledger update committed")
	}
	var unchanged bool
	if s.pool.QueryRow(t.Context(), `SELECT NOT EXISTS(SELECT 1 FROM auth_email_jobs) AND (SELECT state='unqueued' FROM privacy_notice_recipients)`).Scan(&unchanged) != nil || !unchanged {
		t.Fatal("enqueue rollback left a partial mutation")
	}
}

func TestEnqueueRequiresKeysAndHonorsCancellation(t *testing.T) {
	s, _ := prepareEnqueueFixture(t, 1)
	outbox := s.outbox
	s.outbox = nil
	if s.Enqueue(t.Context()) == nil {
		t.Fatal("missing encryption keys accepted")
	}
	var count int
	if s.pool.QueryRow(t.Context(), `SELECT count(*) FROM auth_email_jobs`).Scan(&count) != nil || count != 0 {
		t.Fatal("enqueue job count failed")
	}
	s.outbox = outbox
	if s.Enqueue(deadlineNoticeContext(t)) == nil {
		t.Fatal("expired command context accepted")
	}
}

func TestEnqueueLockWaitIsBounded(t *testing.T) {
	s, _ := prepareEnqueueFixture(t, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal("lock fixture failed")
	}
	defer func() {
		if rollbackErr := rollback(ctx, tx); rollbackErr != nil {
			t.Error("lock fixture rollback failed")
		}
	}()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('crowdsec-postgres-paths-v1',0))`); err != nil {
		t.Fatal("campaign lock fixture failed")
	}
	if s.Enqueue(ctx) == nil || ctx.Err() != nil {
		t.Fatal("enqueue lock timeout did not fail within its bound")
	}
}

func TestEnqueueStatementTimeoutRollsBack(t *testing.T) {
	s, _ := prepareEnqueueFixture(t, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if _, err := s.pool.Exec(ctx, `CREATE FUNCTION delay_notice_queue() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(11); RETURN NEW; END $$; CREATE TRIGGER delay_notice_queue BEFORE UPDATE ON privacy_notice_recipients FOR EACH ROW EXECUTE FUNCTION delay_notice_queue()`); err != nil {
		t.Fatal("statement timeout fixture failed")
	}
	if s.Enqueue(ctx) == nil || ctx.Err() != nil {
		t.Fatal("enqueue statement timeout did not fail within its bound")
	}
	var unchanged bool
	if s.pool.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM auth_email_jobs) AND (SELECT state='unqueued' FROM privacy_notice_recipients)`).Scan(&unchanged) != nil || !unchanged {
		t.Fatal("statement timeout left a partial enqueue")
	}
}

func deadlineNoticeContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithDeadline(t.Context(), time.Unix(0, 0))
	t.Cleanup(cancel)
	return ctx
}
