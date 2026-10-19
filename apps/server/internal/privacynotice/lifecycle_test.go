package privacynotice

import (
	"encoding/json"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func acceptNoticeFixture(t *testing.T, s *Service) {
	t.Helper()
	if _, err := s.pool.Exec(t.Context(), `DELETE FROM auth_email_jobs; UPDATE privacy_notice_recipients SET state='accepted',accepted_at=clock_timestamp(),failure_code=NULL`); err != nil {
		t.Fatal("accepted ledger fixture failed")
	}
}

func TestReviewedRecoveryUsesNewGenerationAndAtomicHistory(t *testing.T) {
	for _, reason := range []string{"expired", "attempts_exhausted", "provider_rejected", "payload_invalid", "job_missing", "delivery_failed"} {
		t.Run(reason, func(t *testing.T) {
			s, enqueue := prepareEnqueueFixture(t, 1)
			enqueue()
			var priorNonce []byte
			if s.pool.QueryRow(t.Context(), `SELECT nonce FROM auth_email_jobs`).Scan(&priorNonce) != nil {
				t.Fatal("prior generation nonce fixture failed")
			}
			if _, err := s.pool.Exec(t.Context(), `DELETE FROM auth_email_jobs; UPDATE privacy_notice_recipients SET state='failed',failed_at=clock_timestamp(),failure_code=$1`, reason); err != nil {
				t.Fatal("recovery failure fixture failed")
			}
			if s.Recover(t.Context(), reason) == nil {
				t.Fatal("recovery crossed existing send authority")
			}
			revokeNoticeFixture(t, s)
			if s.Recover(t.Context(), reason) != nil {
				t.Fatal("reviewed recovery failed")
			}
			var correct bool
			if s.pool.QueryRow(t.Context(), `SELECT c.recovery_count=1 AND r.generation=1 AND r.state='queued' AND r.failure_code IS NULL AND j.attempts=0 AND j.expires_at=j.created_at+interval '24 hours' FROM privacy_notice_campaigns c JOIN privacy_notice_recipients r ON r.campaign_id=c.id JOIN auth_email_jobs j ON j.id=r.job_id`).Scan(&correct) != nil || !correct {
				t.Fatal("recovery generation or history differs")
			}
			if s.pool.QueryRow(t.Context(), `SELECT nonce<>$1 FROM auth_email_jobs`, priorNonce).Scan(&correct) != nil || !correct {
				t.Fatal("new generation reused prior nonce")
			}
			if s.Recover(t.Context(), reason) != nil {
				t.Fatal("repeated recovery read failed")
			}
			if s.pool.QueryRow(t.Context(), `SELECT recovery_count=1 FROM privacy_notice_campaigns`).Scan(&correct) != nil || !correct {
				t.Fatal("queued retry inflated recovery count")
			}
			report, statusErr := s.Status(t.Context())
			if statusErr != nil {
				t.Fatal("recovery aggregate read failed")
			}
			data, encodeErr := report.JSON(false)
			if encodeErr != nil {
				t.Fatal("recovery aggregate encoding failed")
			}
			var output struct {
				RecoveryCounts map[string]int64 `json:"recovery_counts"`
			}
			if json.Unmarshal(data, &output) != nil || output.RecoveryCounts[reason] != 1 {
				t.Fatal("prior failure reason was not counted")
			}
		})
	}
}

func TestRestoreReconciliationStaysFencedAndRetainsRecoveryThroughPurge(t *testing.T) {
	s, enqueue := prepareEnqueueFixture(t, 1)
	enqueue()
	revokeNoticeFixture(t, s)
	files, ok := s.authorityFS.(fstest.MapFS)
	if !ok {
		t.Fatal("restore authority fixture invalid")
	}
	files["restore-reconciliation"] = &fstest.MapFile{Mode: 0444}
	if _, err := s.pool.Exec(t.Context(), `DELETE FROM auth_email_jobs; UPDATE privacy_notice_recipients SET state='failed',failed_at=clock_timestamp(),failure_code='job_missing'`); err != nil {
		t.Fatal("restore failure fixture failed")
	}
	if s.Recover(t.Context(), "job_missing") != nil {
		t.Fatal("reviewed fenced reconciliation failed")
	}
	if _, err := s.Activate(t.Context()); err == nil {
		t.Fatal("restore marker permitted activation")
	}
	if _, err := s.Status(t.Context()); err == nil {
		t.Fatal("restore marker permitted authority status")
	}
	delete(files, "restore-reconciliation")
	acceptNoticeFixture(t, s)
	if _, err := s.Activate(t.Context()); err != nil {
		t.Fatal("reconciled activation failed")
	}
	if s.Retire(t.Context()) != nil {
		t.Fatal("resolved retirement failed")
	}
	if removed, err := s.PurgeRetired(t.Context()); err != nil || removed != 1 {
		t.Fatal("resolved purge failed")
	}
	var retained bool
	if s.pool.QueryRow(t.Context(), `SELECT recovery_count=1 AND recovery_job_missing_count=1 AND activation_count=1 AND first_activated_at IS NOT NULL AND activated_at IS NULL AND accepted_count=1 AND deleted_count=0 FROM privacy_notice_campaigns`).Scan(&retained) != nil || !retained {
		t.Fatal("purge erased activation or recovery history")
	}
}

func TestRecoveryOverflowRollsBack(t *testing.T) {
	s, enqueue := prepareEnqueueFixture(t, 1)
	enqueue()
	revokeNoticeFixture(t, s)
	if _, err := s.pool.Exec(t.Context(), `DELETE FROM auth_email_jobs; UPDATE privacy_notice_recipients SET state='failed',failed_at=clock_timestamp(),failure_code='expired'; UPDATE privacy_notice_campaigns SET recovery_count=9223372036854775807,recovery_expired_count=9223372036854775807`); err != nil {
		t.Fatal("recovery overflow fixture failed")
	}
	if s.Recover(t.Context(), "expired") == nil {
		t.Fatal("recovery counter overflow accepted")
	}
	var unchanged bool
	if s.pool.QueryRow(t.Context(), `SELECT NOT EXISTS(SELECT 1 FROM auth_email_jobs) AND (SELECT generation=0 AND state='failed' FROM privacy_notice_recipients)`).Scan(&unchanged) != nil || !unchanged {
		t.Fatal("recovery overflow partially committed")
	}
}

func TestLateFailureReactivationAndReceiptReconstruction(t *testing.T) {
	s, enqueue := prepareEnqueueFixture(t, 1)
	enqueue()
	acceptNoticeFixture(t, s)
	first, err := s.Activate(t.Context())
	if err != nil || first.Code != 0 {
		t.Fatal("first activation failed")
	}
	var job uuid.UUID
	if s.pool.QueryRow(t.Context(), `SELECT job_id FROM privacy_notice_recipients`).Scan(&job) != nil {
		t.Fatal("late failure fixture failed")
	}
	revokeNoticeFixture(t, s)
	if s.RecordLateFailure(t.Context(), job) != nil {
		t.Fatal("known late failure failed")
	}
	if s.Recover(t.Context(), "delivery_failed") != nil {
		t.Fatal("late failure generation recovery failed")
	}
	if s.AuthorizeSend(t.Context(), store.New(s.pool)) {
		t.Fatal("recovery revived old sending authority")
	}
	acceptNoticeFixture(t, s)
	authorizeNoticeFixture(t, s)
	second, err := s.Activate(t.Context())
	if err != nil || second.Code != 0 || second.Campaign.ActivationCount != 2 || first.Campaign.FirstActivatedAt == nil || second.Campaign.FirstActivatedAt == nil || !first.Campaign.FirstActivatedAt.Equal(*second.Campaign.FirstActivatedAt) {
		t.Fatal("reactivation rewrote first activation history")
	}
	revokeNoticeFixture(t, s)
	repeated, err := s.Activate(t.Context())
	if err != nil || repeated.Campaign.ActivationCount != 2 || repeated.Campaign.ActivatedAt == nil || second.Campaign.ActivatedAt == nil || !repeated.Campaign.ActivatedAt.Equal(*second.Campaign.ActivatedAt) {
		t.Fatal("receipt reconstruction changed activation record")
	}
}

func TestAbortIsBoundedResumableAndRetirementAlonePurges(t *testing.T) {
	s, enqueue := prepareEnqueueFixture(t, 205)
	enqueue()
	revokeNoticeFixture(t, s)
	if _, err := s.PurgeRetired(t.Context()); err == nil {
		t.Fatal("live campaign recipient purge accepted")
	}
	for invocation, want := range []int64{100, 200, 205} {
		if s.Abort(t.Context()) != nil {
			t.Fatal("abort batch failed")
		}
		var count int64
		if s.pool.QueryRow(t.Context(), `SELECT count(*) FROM privacy_notice_recipients WHERE state='cancelled'`).Scan(&count) != nil || count != want {
			t.Fatal("abort cancellation bound differs")
		}
		var complete bool
		if s.pool.QueryRow(t.Context(), `SELECT aborted_at IS NOT NULL FROM privacy_notice_campaigns`).Scan(&complete) != nil || complete != (invocation == 2) {
			t.Fatal("abort completed without full proof")
		}
	}
	if s.Abort(t.Context()) != nil {
		t.Fatal("completed abort retry failed")
	}
	if s.Enqueue(t.Context()) == nil || s.Prepare(t.Context()) == nil {
		t.Fatal("aborted campaign replay accepted")
	}
	if s.Retire(t.Context()) != nil {
		t.Fatal("completed abort retirement failed")
	}
	for _, want := range []int64{200, 5, 0} {
		removed, err := s.PurgeRetired(t.Context())
		if err != nil || removed != want {
			t.Fatal("retirement purge bound differs")
		}
	}
	var retained bool
	if s.pool.QueryRow(t.Context(), `SELECT state='retired' AND expected_count=205 AND aborted_at IS NOT NULL AND purged_at IS NOT NULL FROM privacy_notice_campaigns`).Scan(&retained) != nil || !retained {
		t.Fatal("retirement lost lifetime campaign evidence")
	}
}

func TestAbortInvalidPairKeepsLatchAndRollsBackBatch(t *testing.T) {
	s, enqueue := prepareEnqueueFixture(t, 2)
	enqueue()
	revokeNoticeFixture(t, s)
	if _, err := s.pool.Exec(t.Context(), `UPDATE auth_email_jobs SET kind='password_changed' WHERE id=(SELECT job_id FROM privacy_notice_recipients ORDER BY id LIMIT 1)`); err != nil {
		t.Fatal("invalid abort pair fixture failed")
	}
	if s.Abort(t.Context()) == nil {
		t.Fatal("foreign abort pair accepted")
	}
	var latched bool
	if s.pool.QueryRow(t.Context(), `SELECT state='aborted' AND aborted_at IS NULL AND NOT EXISTS(SELECT 1 FROM privacy_notice_recipients WHERE state='cancelled') FROM privacy_notice_campaigns`).Scan(&latched) != nil || !latched {
		t.Fatal("invalid abort lost latch or partially canceled")
	}
}

func TestAbortOrphanJobStopsCancellationButKeepsLatch(t *testing.T) {
	s, enqueue := prepareEnqueueFixture(t, 101)
	enqueue()
	revokeNoticeFixture(t, s)
	if _, err := s.pool.Exec(t.Context(), `INSERT INTO auth_email_jobs(id,kind,state,user_id,key_id,nonce,ciphertext,created_at,expires_at,next_attempt_at) SELECT $1,kind,state,user_id,key_id,nonce,ciphertext,created_at,expires_at,next_attempt_at FROM auth_email_jobs LIMIT 1`, uuid.New()); err != nil {
		t.Fatal("orphan abort fixture failed")
	}
	if s.Abort(t.Context()) == nil {
		t.Fatal("orphan notice job accepted during abort")
	}
	var unchanged bool
	if s.pool.QueryRow(t.Context(), `SELECT state='aborted' AND aborted_at IS NULL AND NOT EXISTS(SELECT 1 FROM privacy_notice_recipients WHERE state='cancelled') FROM privacy_notice_campaigns`).Scan(&unchanged) != nil || !unchanged {
		t.Fatal("orphan job did not preserve latch and batch rollback")
	}
}

func TestRecoveryRefusesGenerationOverflowAndPreservesLedger(t *testing.T) {
	s, enqueue := prepareEnqueueFixture(t, 1)
	enqueue()
	revokeNoticeFixture(t, s)
	if _, err := s.pool.Exec(t.Context(), `DELETE FROM auth_email_jobs; UPDATE privacy_notice_recipients SET state='failed',failed_at=clock_timestamp(),failure_code='expired',generation=2147483647`); err != nil {
		t.Fatal("generation overflow fixture failed")
	}
	if s.Recover(t.Context(), "expired") == nil {
		t.Fatal("generation overflow accepted")
	}
	var unchanged bool
	if s.pool.QueryRow(t.Context(), `SELECT recovery_count=0 AND NOT EXISTS(SELECT 1 FROM auth_email_jobs) FROM privacy_notice_campaigns`).Scan(&unchanged) != nil || !unchanged {
		t.Fatal("generation overflow partially committed")
	}
}

func TestReviewedAbortRefusesUnsealedCohort(t *testing.T) {
	s, _ := prepareEnqueueFixture(t, 0)
	revokeNoticeFixture(t, s)
	if _, err := s.pool.Exec(t.Context(), `UPDATE privacy_notice_campaigns SET state='aborted',sealed_at=NULL`); err != nil {
		t.Fatal("unsealed abort fixture failed")
	}
	if s.Abort(t.Context()) == nil {
		t.Fatal("unsealed campaign completed abort")
	}
}

func TestLateFailureForDeletedAccountResolvesWithoutContact(t *testing.T) {
	s, enqueue := prepareEnqueueFixture(t, 1)
	enqueue()
	acceptNoticeFixture(t, s)
	revokeNoticeFixture(t, s)
	var job uuid.UUID
	if s.pool.QueryRow(t.Context(), `SELECT job_id FROM privacy_notice_recipients`).Scan(&job) != nil {
		t.Fatal("deleted late failure fixture failed")
	}
	if _, err := s.pool.Exec(t.Context(), `DELETE FROM users`); err != nil {
		t.Fatal("deleted account fixture failed")
	}
	if s.RecordLateFailure(t.Context(), job) != nil {
		t.Fatal("deleted known failure did not resolve")
	}
	var resolved bool
	if s.pool.QueryRow(t.Context(), `SELECT state='deleted' AND user_id IS NULL AND failure_code IS NULL AND deleted_at IS NOT NULL FROM privacy_notice_recipients`).Scan(&resolved) != nil || !resolved {
		t.Fatal("deleted late failure retained contact obligation")
	}
	if s.Recover(t.Context(), "delivery_failed") != nil {
		t.Fatal("empty recovery failed")
	}
	if s.pool.QueryRow(t.Context(), `SELECT recovery_count=0 AND NOT EXISTS(SELECT 1 FROM auth_email_jobs) FROM privacy_notice_campaigns`).Scan(&resolved) != nil || !resolved {
		t.Fatal("deleted account was requeued")
	}
}
