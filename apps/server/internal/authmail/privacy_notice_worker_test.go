package authmail

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

type noticeFixtureAuthority struct{ allowed bool }

// AuthorizeSend models revocable authority without host files.
func (a *noticeFixtureAuthority) AuthorizeSend(context.Context, *store.Queries) bool {
	return a.allowed
}

// ContentDigest matches the isolated fixture campaign.
func (*noticeFixtureAuthority) ContentDigest() []byte { return make([]byte, 32) }

func noticeWorkerFixture(t *testing.T, outcome SendOutcome) (context.Context, *Worker, *noticeFixtureAuthority, uuid.UUID) {
	t.Helper()
	dsn, _ := testutil.NewMigratedTestDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	t.Cleanup(cancel)
	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatal("notice pool failed")
	}
	t.Cleanup(func() {
		cleanup, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancelCleanup()
		pool.Close(cleanup)
	})
	now := time.Now().UTC().Truncate(time.Microsecond)
	user, job, recipient := uuid.New(), uuid.New(), uuid.New()
	if _, execErr := pool.Exec(ctx, `INSERT INTO users(id,email,name) VALUES ($1,'fixture@example.com','Fixture')`, user); execErr != nil {
		t.Fatal("notice user failed")
	}
	if _, execErr := pool.Exec(ctx, `INSERT INTO privacy_notice_campaigns(id,content_sha256,cutoff_at,expected_count,state,sealed_at) VALUES ($1,$2,$3,1,'sealed',$3)`, PrivacyNoticeID, make([]byte, 32), now); execErr != nil {
		t.Fatal("notice campaign failed")
	}
	if _, execErr := pool.Exec(ctx, `INSERT INTO privacy_notice_recipients(id,campaign_id,user_id,job_id,state,queued_at) VALUES ($1,$2,$3,$4,'queued',$5)`, recipient, PrivacyNoticeID, user, job, now); execErr != nil {
		t.Fatal("notice recipient failed")
	}
	ring := testRing(t, 2)
	outbox := newTestOutbox(t, ring, func() time.Time { return now })
	if enqueueErr := outbox.EnqueueTx(ctx, store.New(pool), EnqueueRequest{JobID: job, Kind: Kind("privacy_notice"), UserID: &user, Payload: Payload{Version: 3, To: "fixture@example.com", UserID: user.String(), NoticeID: PrivacyNoticeID}, ExpiresAt: now.Add(24 * time.Hour)}); enqueueErr != nil {
		t.Fatal("notice enqueue failed")
	}
	authority := &noticeFixtureAuthority{allowed: true}
	worker, err := NewWorker(WorkerOptions{Pool: pool, Queries: store.New(pool), KeyRing: ring, Sender: &stubSender{result: SendResult{Outcome: outcome}}, Clock: func() time.Time { return now }, Jitter: func(time.Duration) time.Duration { return 0 }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), WorkerID: uuid.New(), NoticeAuthority: authority})
	if err != nil {
		t.Fatal("notice worker failed")
	}
	return ctx, worker, authority, job
}

func TestPrivacyNoticeWorkerAtomicOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name                               string
		outcome                            SendOutcome
		mutation                           string
		wantJob, wantRecipient, wantReason string
	}{
		{"accepted", SendAccepted, "", "sent", "accepted", ""},
		{"temporary", SendTemporaryFailure, "", "pending", "queued", ""},
		{"permanent", SendPermanentFailure, "", "terminal", "failed", "provider_rejected"},
		{"expired", SendAccepted, `UPDATE auth_email_jobs SET expires_at=created_at`, "terminal", "failed", "expired"},
		{"exhausted", SendTemporaryFailure, `UPDATE auth_email_jobs SET attempts=7`, "terminal", "failed", "attempts_exhausted"},
		{"invalid ciphertext", SendAccepted, `UPDATE auth_email_jobs SET ciphertext=decode('00','hex')`, "terminal", "failed", "payload_invalid"},
		{"generation replay", SendAccepted, `UPDATE privacy_notice_recipients SET generation=1`, "terminal", "failed", "payload_invalid"},
		{"reparented job", SendAccepted, `WITH added AS (INSERT INTO users(email,name) VALUES ('other@example.com','Fixture') RETURNING id) UPDATE auth_email_jobs SET user_id=added.id FROM added`, "terminal", "failed", "payload_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, w, _, job := noticeWorkerFixture(t, tc.outcome)
			if tc.mutation != "" {
				if _, err := w.queriesDB().Exec(ctx, tc.mutation); err != nil {
					t.Fatal("notice mutation failed")
				}
			}
			if w.RunOnce(ctx) != nil {
				t.Fatal("notice tick failed")
			}
			if (tc.wantReason == "payload_invalid" || tc.name == "expired") && noticeSenderCalls(t, w) != 0 {
				t.Fatal("invalid or expired notice reached SMTP")
			}
			var matches bool
			if w.queriesDB().QueryRow(ctx, `SELECT j.state=$2 AND r.state=$3 AND COALESCE(r.failure_code,'')=$4 AND (j.state IN ('pending','leased') OR j.ciphertext IS NULL) FROM auth_email_jobs j JOIN privacy_notice_recipients r ON r.job_id=j.id WHERE j.id=$1`, job, tc.wantJob, tc.wantRecipient, tc.wantReason).Scan(&matches) != nil || !matches {
				t.Fatal("notice outbox and ledger outcomes differ")
			}
		})
	}
}

func (w *Worker) queriesDB() *store.Pool {
	pool, ok := w.pool.(*store.Pool)
	if !ok {
		panic("notice fixture pool invalid")
	}
	return pool
}

func noticeSenderCalls(t *testing.T, w *Worker) int {
	t.Helper()
	sender, ok := w.sender.(*stubSender)
	if !ok {
		t.Fatal("notice fixture sender invalid")
	}
	return sender.count()
}

func TestPrivacyNoticeAuthorityMissingOrRevokedBlocksSend(t *testing.T) {
	for _, revokedAfterClaim := range []bool{false, true} {
		ctx, w, authority, job := noticeWorkerFixture(t, SendAccepted)
		if revokedAfterClaim {
			w.hooks.afterClaim = func([]store.AuthEmailJob) { authority.allowed = false }
		} else {
			w.noticeAuthority = nil
		}
		if w.RunOnce(ctx) != nil {
			t.Fatal("notice gate tick failed")
		}
		var unsent bool
		if w.queriesDB().QueryRow(ctx, `SELECT sent_at IS NULL AND ciphertext IS NOT NULL FROM auth_email_jobs WHERE id=$1`, job).Scan(&unsent) != nil || !unsent {
			t.Fatal("unauthorized notice sent")
		}
	}
}

func TestPrivacyNoticeMissingAuthorityPreservesOrdinaryMail(t *testing.T) {
	ctx, w, _, noticeJob := noticeWorkerFixture(t, SendAccepted)
	pool := w.queriesDB()
	var user uuid.UUID
	if pool.QueryRow(ctx, `SELECT user_id FROM privacy_notice_recipients WHERE job_id=$1`, noticeJob).Scan(&user) != nil {
		t.Fatal("ordinary fixture user missing")
	}
	ordinary := uuid.New()
	outbox := newTestOutbox(t, w.ring, w.clock)
	if outbox.EnqueueTx(ctx, w.queries, EnqueueRequest{JobID: ordinary, Kind: KindPasswordChanged, UserID: &user, Payload: Payload{Version: 1, To: "fixture@example.com"}, ExpiresAt: w.clock().Add(time.Hour)}) != nil {
		t.Fatal("ordinary fixture enqueue failed")
	}
	w.noticeAuthority = nil
	if w.RunOnce(ctx) != nil {
		t.Fatal("ordinary tick failed")
	}
	var preserved bool
	if pool.QueryRow(ctx, `SELECT (SELECT state='sent' FROM auth_email_jobs WHERE id=$1) AND (SELECT state='pending' FROM auth_email_jobs WHERE id=$2)`, ordinary, noticeJob).Scan(&preserved) != nil || !preserved {
		t.Fatal("notice authority blocked ordinary mail")
	}
}

func TestPrivacyNoticeRevokedImmediatelyBeforeSMTP(t *testing.T) {
	ctx, w, authority, job := noticeWorkerFixture(t, SendAccepted)
	w.hooks.beforeSend = func(uuid.UUID) { authority.allowed = false }
	if w.RunOnce(ctx) != nil {
		t.Fatal("revocation tick failed")
	}
	if noticeSenderCalls(t, w) != 0 {
		t.Fatal("revoked notice reached SMTP")
	}
	var leased bool
	if w.queriesDB().QueryRow(ctx, `SELECT state='leased' AND ciphertext IS NOT NULL FROM auth_email_jobs WHERE id=$1`, job).Scan(&leased) != nil || !leased {
		t.Fatal("revocation erased recoverable job")
	}
}

func TestPrivacyNoticeClaimKeepsAuthenticationMailAhead(t *testing.T) {
	ctx, w, _, noticeJob := noticeWorkerFixture(t, SendAccepted)
	var user uuid.UUID
	if w.queriesDB().QueryRow(ctx, `SELECT user_id FROM privacy_notice_recipients WHERE job_id=$1`, noticeJob).Scan(&user) != nil {
		t.Fatal("priority fixture user missing")
	}
	w.ring = testRing(t, 12)
	outbox := newTestOutbox(t, w.ring, w.clock)
	for range 10 {
		if outbox.EnqueueTx(ctx, w.queries, EnqueueRequest{JobID: uuid.New(), Kind: KindPasswordChanged, UserID: &user, Payload: Payload{Version: 1, To: "fixture@example.com"}, ExpiresAt: w.clock().Add(time.Hour)}) != nil {
			t.Fatal("priority fixture enqueue failed")
		}
	}
	if _, err := w.queriesDB().Exec(ctx, `UPDATE auth_email_jobs SET next_attempt_at=created_at-interval '1 hour' WHERE id=$1`, noticeJob); err != nil {
		t.Fatal("priority due time failed")
	}
	jobs, err := w.claim(ctx, w.clock())
	if err != nil || len(jobs) != 10 {
		t.Fatal("priority claim batch differs")
	}
	for _, job := range jobs {
		if job.Kind == string(KindPrivacyNotice) {
			t.Fatal("older notice displaced authentication mail")
		}
	}
}

func TestPrivacyNoticeMixedClaimOrdersOrdinaryMailFirst(t *testing.T) {
	ctx, w, _, noticeJob := noticeWorkerFixture(t, SendAccepted)
	var user uuid.UUID
	if w.queriesDB().QueryRow(ctx, `SELECT user_id FROM privacy_notice_recipients WHERE job_id=$1`, noticeJob).Scan(&user) != nil {
		t.Fatal("mixed claim user fixture failed")
	}
	outbox := newTestOutbox(t, w.ring, w.clock)
	if outbox.EnqueueTx(ctx, w.queries, EnqueueRequest{JobID: uuid.New(), Kind: KindPasswordChanged, UserID: &user, Payload: Payload{Version: 1, To: "fixture@example.com"}, ExpiresAt: w.clock().Add(time.Hour)}) != nil {
		t.Fatal("mixed claim ordinary fixture failed")
	}
	jobs, err := w.claim(ctx, w.clock())
	if err != nil || len(jobs) != 2 || jobs[0].Kind == string(KindPrivacyNotice) || jobs[1].Kind != string(KindPrivacyNotice) {
		t.Fatal("mixed claim did not prioritize ordinary mail")
	}
}

type noticeCommitFailurePool struct {
	pool *store.Pool
	job  uuid.UUID
}

// Begin wraps the real transaction for a deterministic finalization failure.
func (p noticeCommitFailurePool) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return noticeCommitFailureTx{Tx: tx, job: p.job}, nil
}

type noticeCommitFailureTx struct {
	pgx.Tx
	job uuid.UUID
}

// Commit rolls back an accepted send instead of recording its outcome.
func (tx noticeCommitFailureTx) Commit(ctx context.Context) error {
	var accepted bool
	if tx.QueryRow(ctx, `SELECT state='sent' FROM auth_email_jobs WHERE id=$1`, tx.job).Scan(&accepted) != nil {
		return errors.New("fixture commit probe failed")
	}
	if accepted {
		if tx.Rollback(ctx) != nil {
			return errors.New("fixture rollback failed")
		}
		return errors.New("fixture commit failed")
	}
	return tx.Tx.Commit(ctx)
}

func TestPrivacyNoticeProviderAcceptanceWithCommitFailureRemainsUnresolved(t *testing.T) {
	ctx, w, _, job := noticeWorkerFixture(t, SendAccepted)
	pool := w.queriesDB()
	w.pool = noticeCommitFailurePool{pool: pool, job: job}
	if w.RunOnce(ctx) != nil {
		t.Fatal("commit failure tick failed")
	}
	if noticeSenderCalls(t, w) != 1 {
		t.Fatal("acceptance crash fixture did not reach provider")
	}
	var unresolved bool
	if pool.QueryRow(ctx, `SELECT j.state='leased' AND r.state='queued' AND r.accepted_at IS NULL FROM auth_email_jobs j JOIN privacy_notice_recipients r ON r.job_id=j.id WHERE j.id=$1`, job).Scan(&unresolved) != nil || !unresolved {
		t.Fatal("failed commit recorded an accepted outcome")
	}
}

func TestPrivacyNoticeExpiredLeaseWaitsForCurrentAuthority(t *testing.T) {
	ctx, w, authority, job := noticeWorkerFixture(t, SendAccepted)
	jobs, err := w.claim(ctx, w.clock())
	if err != nil || len(jobs) != 1 {
		t.Fatal("stale notice claim failed")
	}
	if _, err := w.queriesDB().Exec(ctx, `UPDATE auth_email_jobs SET lease_expires_at=$2 WHERE id=$1`, job, w.clock().Add(-time.Second)); err != nil {
		t.Fatal("stale notice lease fixture failed")
	}
	authority.allowed = false
	if w.RunOnce(ctx) != nil {
		t.Fatal("stale notice tick failed")
	}
	var unchanged bool
	if w.queriesDB().QueryRow(ctx, `SELECT state='leased' AND attempts=1 FROM auth_email_jobs WHERE id=$1`, job).Scan(&unchanged) != nil || !unchanged {
		t.Fatal("stale notice lease changed without authority")
	}
}

func TestPrivacyNoticeSevenDayOutboxCleanupRetainsLifetimeEvidence(t *testing.T) {
	ctx, w, authority, job := noticeWorkerFixture(t, SendAccepted)
	if w.RunOnce(ctx) != nil {
		t.Fatal("accepted notice tick failed")
	}
	now := w.clock()
	w.clock = func() time.Time { return now.Add(8 * 24 * time.Hour) }
	authority.allowed = false
	if w.RunOnce(ctx) != nil {
		t.Fatal("notice cleanup tick failed")
	}
	var retained bool
	if w.queriesDB().QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM auth_email_jobs WHERE id=$1) AND EXISTS(SELECT 1 FROM privacy_notice_recipients WHERE job_id=$1 AND state='accepted' AND accepted_at IS NOT NULL)`, job).Scan(&retained) != nil || !retained {
		t.Fatal("outbox cleanup erased lifetime acceptance")
	}
}

func TestPrivacyNoticeAcceptanceTimeFollowsProviderAcceptance(t *testing.T) {
	ctx, w, _, job := noticeWorkerFixture(t, SendAccepted)
	acceptedAt := w.clock().Add(5 * time.Second)
	w.hooks.afterSend = func(uuid.UUID, SendOutcome) { w.clock = func() time.Time { return acceptedAt } }
	if w.RunOnce(ctx) != nil {
		t.Fatal("acceptance timestamp tick failed")
	}
	var correct bool
	if w.queriesDB().QueryRow(ctx, `SELECT r.accepted_at=$2 AND j.sent_at=$2 FROM privacy_notice_recipients r JOIN auth_email_jobs j ON j.id=r.job_id WHERE j.id=$1`, job, acceptedAt).Scan(&correct) != nil || !correct {
		t.Fatal("notice acceptance time preceded provider acceptance")
	}
}
