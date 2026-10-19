package privacynotice

import "testing"

func TestStatusAndActivationUseLifetimeLedger(t *testing.T) {
	s, enqueue := prepareEnqueueFixture(t, 1)
	enqueue()
	if report, err := s.Status(t.Context()); err != nil || report.Code != 2 {
		t.Fatal("queued notice reported complete")
	}
	if report, err := s.Activate(t.Context()); err == nil && report.Code == 0 {
		t.Fatal("incomplete campaign activated")
	}
	if _, err := s.pool.Exec(t.Context(), `DELETE FROM auth_email_jobs; UPDATE privacy_notice_recipients SET state='accepted',accepted_at=now()-interval '30 days'`); err != nil {
		t.Fatal("accepted fixture failed")
	}
	report, err := s.Status(t.Context())
	if err != nil || report.Code != 0 || report.Totals.Accepted != 1 {
		t.Fatal("lifetime ledger did not establish completion")
	}
	report, err = s.Activate(t.Context())
	if err != nil || report.Campaign.State != "activated" {
		t.Fatal("resolved campaign activation failed")
	}
	first := report.Campaign.ActivatedAt
	revokeNoticeFixture(t, s)
	report, err = s.Activate(t.Context())
	if err != nil || first == nil || report.Campaign.ActivatedAt == nil || !first.Equal(*report.Campaign.ActivatedAt) {
		t.Fatal("activation recovery changed original activation")
	}
}

func TestActivationRefusesInvalidLedgerAndCampaign(t *testing.T) {
	for name, mutation := range map[string]string{
		"count mismatch":       `UPDATE privacy_notice_campaigns SET expected_count=2`,
		"null unresolved user": `UPDATE privacy_notice_recipients SET user_id=NULL`,
		"failed":               `UPDATE privacy_notice_recipients SET state='failed',failed_at=now(),failure_code='expired'`,
		"missing queued job":   `UPDATE privacy_notice_recipients SET state='queued',queued_at=now()`,
		"different digest":     `UPDATE privacy_notice_campaigns SET content_sha256=decode(repeat('ff',32),'hex')`,
		"aborted":              `UPDATE privacy_notice_campaigns SET state='aborted'`,
		"retired":              `UPDATE privacy_notice_campaigns SET state='retired',retired_at=now()`,
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := prepareEnqueueFixture(t, 1)
			if _, err := s.pool.Exec(t.Context(), mutation); err != nil {
				t.Fatal("activation refusal fixture failed")
			}
			if report, err := s.Activate(t.Context()); err == nil && report.Code == 0 {
				t.Fatal("invalid campaign activated")
			}
		})
	}
}

func TestActivationReconstructionRechecksStoredTotalsWithoutReceipts(t *testing.T) {
	s, enqueue := prepareEnqueueFixture(t, 1)
	enqueue()
	acceptNoticeFixture(t, s)
	if _, err := s.Activate(t.Context()); err != nil {
		t.Fatal("activation fixture failed")
	}
	revokeNoticeFixture(t, s)
	if _, err := s.pool.Exec(t.Context(), `UPDATE privacy_notice_campaigns SET accepted_count=0,deleted_count=1,last_accepted_at=NULL`); err != nil {
		t.Fatal("activation mismatch fixture failed")
	}
	if _, err := s.Activate(t.Context()); err == nil {
		t.Fatal("receipt-free recovery trusted stale activation totals")
	}
}

func TestAbortedStatusRetainsProgressAndRecoveryHistory(t *testing.T) {
	s, enqueue := prepareEnqueueFixture(t, 101)
	enqueue()
	revokeNoticeFixture(t, s)
	if s.Abort(t.Context()) != nil {
		t.Fatal("abort fixture failed")
	}
	report, err := s.Status(t.Context())
	if err != nil || report.Code != 1 || report.Campaign.State != "aborted" || report.Totals.Cancelled != 100 { //nolint:misspell // Generated store member follows the fixed database spelling.
		t.Fatal("aborted progress did not return blocked aggregates")
	}
	if _, err := report.JSON(false); err != nil {
		t.Fatal("blocked aggregates could not encode")
	}
}

func TestStatusRejectsForeignCompletedJobPair(t *testing.T) {
	s, enqueue := prepareEnqueueFixture(t, 1)
	enqueue()
	if _, err := s.pool.Exec(t.Context(), `UPDATE auth_email_jobs SET kind='password_changed',state='sent',attempts=1,key_id=NULL,nonce=NULL,ciphertext=NULL,next_attempt_at=NULL,sent_at=clock_timestamp(); UPDATE privacy_notice_recipients SET state='accepted',accepted_at=clock_timestamp()`); err != nil {
		t.Fatal("foreign completed pair fixture failed")
	}
	if _, err := s.Status(t.Context()); err == nil {
		t.Fatal("foreign completed job established campaign completion")
	}
}

func TestActivationFutureAcceptanceDoesNotCommit(t *testing.T) {
	s, enqueue := prepareEnqueueFixture(t, 1)
	enqueue()
	acceptNoticeFixture(t, s)
	if _, err := s.pool.Exec(t.Context(), `UPDATE privacy_notice_recipients SET accepted_at=clock_timestamp()+interval '1 day'`); err != nil {
		t.Fatal("future acceptance fixture failed")
	}
	if _, err := s.Activate(t.Context()); err == nil {
		t.Fatal("future acceptance activated")
	}
	var unchanged bool
	if s.pool.QueryRow(t.Context(), `SELECT state='sealed' AND activation_count=0 AND activated_at IS NULL FROM privacy_notice_campaigns`).Scan(&unchanged) != nil || !unchanged {
		t.Fatal("clock mismatch partially activated")
	}
}
