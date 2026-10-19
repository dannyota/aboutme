package privacynotice

import "testing"

func TestAbortPreservesEveryAcceptedRecipientJobPair(t *testing.T) {
	for _, pair := range []struct{ recipient, job string }{
		{"unqueued", "absent"}, {"queued", "pending"}, {"queued", "leased"}, {"queued", "absent"},
		{"accepted", "sent"}, {"accepted", "absent"}, {"deleted", "absent"},
		{"failed", "terminal"}, {"failed", "sent"}, {"failed", "absent"},
		{"cancelled", "terminal"}, {"cancelled", "absent"}, //nolint:misspell // Exact database outcome spelling.
	} {
		t.Run(pair.recipient+"/"+pair.job, func(t *testing.T) {
			s, enqueue := prepareEnqueueFixture(t, 1)
			enqueue()
			revokeNoticeFixture(t, s)
			switch pair.job {
			case "absent":
				if _, err := s.pool.Exec(t.Context(), `DELETE FROM auth_email_jobs`); err != nil {
					t.Fatal("absent abort fixture failed")
				}
			case "sent", "terminal":
				if _, err := s.pool.Exec(t.Context(), `UPDATE auth_email_jobs SET state=$1,attempts=CASE WHEN $1='sent' THEN 1 ELSE attempts END,key_id=NULL,nonce=NULL,ciphertext=NULL,next_attempt_at=NULL,sent_at=CASE WHEN $1='sent' THEN clock_timestamp() END,terminal_at=CASE WHEN $1='terminal' THEN clock_timestamp() END`, pair.job); err != nil {
					t.Fatal("completed abort fixture failed")
				}
			case "leased":
				if _, err := s.pool.Exec(t.Context(), `UPDATE auth_email_jobs SET state='leased',next_attempt_at=NULL,lease_owner='fixture',lease_expires_at=clock_timestamp()+interval '30 seconds',attempts=1`); err != nil {
					t.Fatal("leased abort fixture failed")
				}
			}
			if _, err := s.pool.Exec(t.Context(), `UPDATE privacy_notice_recipients SET state=$1,user_id=CASE WHEN $1='deleted' THEN NULL ELSE user_id END,accepted_at=CASE WHEN $1='accepted' THEN clock_timestamp() END,deleted_at=CASE WHEN $1='deleted' THEN clock_timestamp() END,failed_at=CASE WHEN $1='failed' THEN clock_timestamp() END,cancelled_at=CASE WHEN $1='cancelled' THEN clock_timestamp() END,failure_code=CASE WHEN $1='failed' THEN 'expired' END`, pair.recipient); err != nil { //nolint:misspell // Exact database column and outcome spellings.
				t.Fatal("abort outcome fixture failed")
			}
			if s.Abort(t.Context()) != nil {
				t.Fatal("valid abort pair rejected")
			}
			want := pair.recipient
			if want == "unqueued" || want == "queued" {
				want = "cancelled" //nolint:misspell // Exact database outcome spelling.
			}
			var correct bool
			if s.pool.QueryRow(t.Context(), `SELECT c.state='aborted' AND c.aborted_at IS NOT NULL AND r.state=$1 AND (r.state<>'failed' OR r.failure_code='expired') FROM privacy_notice_campaigns c JOIN privacy_notice_recipients r ON r.campaign_id=c.id`, want).Scan(&correct) != nil || !correct {
				t.Fatal("abort altered a retained outcome")
			}
			if pair.job == "pending" || pair.job == "leased" {
				if s.pool.QueryRow(t.Context(), `SELECT j.state='terminal' AND j.terminal_at=r.cancelled_at AND j.sent_at IS NULL AND j.key_id IS NULL AND j.nonce IS NULL AND j.ciphertext IS NULL AND j.next_attempt_at IS NULL AND j.lease_owner IS NULL AND j.lease_expires_at IS NULL AND j.attempts=$1 AND j.expires_at=j.created_at+interval '24 hours' FROM auth_email_jobs j JOIN privacy_notice_recipients r ON r.job_id=j.id`, map[string]int{"pending": 0, "leased": 1}[pair.job]).Scan(&correct) != nil || !correct {
					t.Fatal("abort terminalization changed lifetime or failed to clear fields")
				}
			} else if pair.job != "absent" {
				if s.pool.QueryRow(t.Context(), `SELECT state=$1 FROM auth_email_jobs`, pair.job).Scan(&correct) != nil || !correct {
					t.Fatal("abort changed completed job state")
				}
			}
		})
	}
}
