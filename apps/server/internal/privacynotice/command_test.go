package privacynotice

import (
	"bytes"
	"io/fs"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
)

func TestCommandPreparePartialCaptureWithoutSendReceipt(t *testing.T) {
	ctx, s := noticeTestService(t)
	s.now = time.Now
	s.authorityFS = fstest.MapFS{".": {Mode: fs.ModeDir | 0755}, "epoch": {Data: []byte(uuid.NewString() + "\n"), Mode: 0444}}
	if _, err := s.pool.Exec(ctx, `INSERT INTO users(email,name) SELECT 'fixture-'||n||'@example.com','Fixture' FROM generate_series(1,205) n`); err != nil {
		t.Fatal("partial capture fixture failed")
	}
	for index, want := range []int64{100, 200, 205, 205} {
		report, err := s.Run(ctx, "prepare")
		if err != nil || report.Totals.Actual != want || (index < 2 && report.Code != 2) || (index >= 2 && report.Code != 0) {
			t.Fatal("prepare command capture progress differs")
		}
	}
}

func TestCommandPrepareRetryAndEnqueueProgress(t *testing.T) {
	s, _ := prepareEnqueueFixture(t, 101)
	revokeNoticeFixture(t, s)
	report, err := s.Run(t.Context(), "prepare")
	if err != nil || report.Code != 0 {
		t.Fatal("sealed prepare retry failed without send authority")
	}
	data, err := report.JSON(false)
	if err != nil || !bytes.Contains(data, []byte(`"result":"sealed"`)) {
		t.Fatal("prepare result differs")
	}
	if _, enqueueErr := s.Run(t.Context(), "enqueue"); enqueueErr == nil {
		t.Fatal("enqueue without authority succeeded")
	}
	authorizeNoticeFixture(t, s)
	report, err = s.Run(t.Context(), "enqueue")
	if err != nil || report.Code != 2 {
		t.Fatal("enqueue progress did not remain pending")
	}
	report, err = s.Run(t.Context(), "enqueue")
	if err != nil || report.Code != 2 {
		t.Fatal("queue cap did not defer")
	}
	data, err = report.JSON(false)
	if err != nil || !bytes.Contains(data, []byte(`"result":"queue_full"`)) {
		t.Fatal("queue cap result differs")
	}
}

func TestCommandBlockedPrecedesQueueCapWithoutMutation(t *testing.T) {
	s, enqueue := prepareEnqueueFixture(t, 101)
	enqueue()
	revokeNoticeFixture(t, s)
	if _, err := s.pool.Exec(t.Context(), `UPDATE privacy_notice_recipients SET state='failed',failed_at=clock_timestamp(),failure_code='job_missing' WHERE state='unqueued'`); err != nil {
		t.Fatal("blocked command fixture failed")
	}
	for _, action := range []string{"prepare", "enqueue", "status", "activate"} {
		report, err := s.Run(t.Context(), action)
		if err != nil || report.Code != 1 {
			t.Fatal("blocked result lost precedence")
		}
		data, encodeErr := report.JSON(action == "activate")
		if encodeErr != nil || !bytes.Contains(data, []byte(`"result":"blocked"`)) {
			t.Fatal("blocked command output differs")
		}
	}
	var unchanged bool
	if s.pool.QueryRow(t.Context(), `SELECT count(*)=100 FROM auth_email_jobs`).Scan(&unchanged) != nil || !unchanged {
		t.Fatal("blocked command mutated queue")
	}
}

func TestLateFailureSentJobSurvivesAbortAndRecoveryReusesID(t *testing.T) {
	for _, abort := range []bool{false, true} {
		t.Run(map[bool]string{false: "recover", true: "abort"}[abort], func(t *testing.T) {
			s, enqueue := prepareEnqueueFixture(t, 1)
			enqueue()
			revokeNoticeFixture(t, s)
			var job uuid.UUID
			if s.pool.QueryRow(t.Context(), `SELECT job_id FROM privacy_notice_recipients`).Scan(&job) != nil {
				t.Fatal("sent late failure fixture failed")
			}
			if _, err := s.pool.Exec(t.Context(), `UPDATE auth_email_jobs SET state='sent',key_id=NULL,nonce=NULL,ciphertext=NULL,next_attempt_at=NULL,sent_at=clock_timestamp(); UPDATE privacy_notice_recipients SET state='accepted',accepted_at=clock_timestamp()`); err != nil {
				t.Fatal("sent late failure fixture failed")
			}
			if s.RecordLateFailure(t.Context(), job) != nil {
				t.Fatal("sent late failure recording failed")
			}
			if abort {
				if s.Abort(t.Context()) != nil {
					t.Fatal("sent late failure blocked abort")
				}
			} else {
				if s.Recover(t.Context(), "delivery_failed") != nil {
					t.Fatal("sent late failure recovery failed")
				}
			}
			var correct bool
			if abort {
				if s.pool.QueryRow(t.Context(), `SELECT j.state='sent' AND r.state='failed' AND r.failure_code='delivery_failed' AND c.aborted_at IS NOT NULL FROM auth_email_jobs j JOIN privacy_notice_recipients r ON r.job_id=j.id JOIN privacy_notice_campaigns c ON c.id=r.campaign_id WHERE j.id=$1`, job).Scan(&correct) != nil || !correct {
					t.Fatal("abort erased sent audit outcome")
				}
			} else {
				if s.pool.QueryRow(t.Context(), `SELECT j.state='pending' AND j.attempts=0 AND j.sent_at IS NULL AND r.state='queued' AND r.generation=1 FROM auth_email_jobs j JOIN privacy_notice_recipients r ON r.job_id=j.id WHERE j.id=$1`, job).Scan(&correct) != nil || !correct {
					t.Fatal("recovery did not reuse logical job")
				}
			}
		})
	}
}

func TestRetiredStatusRetainsHistoryWhileCompletedJobsAwaitCleanup(t *testing.T) {
	s, enqueue := prepareEnqueueFixture(t, 1)
	enqueue()
	revokeNoticeFixture(t, s)
	if _, err := s.pool.Exec(t.Context(), `UPDATE auth_email_jobs SET state='sent',key_id=NULL,nonce=NULL,ciphertext=NULL,next_attempt_at=NULL,sent_at=clock_timestamp(); UPDATE privacy_notice_recipients SET state='accepted',accepted_at=clock_timestamp()`); err != nil {
		t.Fatal("retired audit fixture failed")
	}
	if _, err := s.Activate(t.Context()); err != nil {
		t.Fatal("retired audit activation failed")
	}
	if s.Retire(t.Context()) != nil {
		t.Fatal("retired audit retirement failed")
	}
	if removed, err := s.PurgeRetired(t.Context()); err != nil || removed != 1 {
		t.Fatal("retired audit purge failed")
	}
	report, err := s.Status(t.Context())
	if err != nil || report.Code != 1 || report.Campaign.ActivationCount != 1 {
		t.Fatal("retained completed jobs hid retired aggregates")
	}
	var retained bool
	if s.pool.QueryRow(t.Context(), `SELECT count(*)=1 FROM auth_email_jobs WHERE state='sent'`).Scan(&retained) != nil || !retained {
		t.Fatal("retirement shortened completed job retention")
	}
}
