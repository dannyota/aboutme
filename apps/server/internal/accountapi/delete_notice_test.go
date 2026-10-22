package accountapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/privacynotice"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

func TestAccountDeletionRetainsNoticeObligationOutcome(t *testing.T) {
	for _, state := range []string{"unqueued", "queued", "accepted", "failed"} {
		t.Run(state, func(t *testing.T) {
			dsn, _ := testutil.NewMigratedTestDatabase(t)
			t.Setenv("TEST_DATABASE_URL", dsn)
			env := newDeletionEnvironment(t)
			if _, err := env.service.pool.Exec(env.ctx, `INSERT INTO privacy_notice_campaigns(id,content_sha256,cutoff_at,expected_count,state,sealed_at) VALUES ($1,$2,now(),1,'sealed',now())`, authmail.PrivacyNoticeID, privacynotice.ContentDigest()); err != nil {
				t.Fatal("notice campaign fixture failed")
			}
			id, job := uuid.New(), uuid.New()
			if _, err := env.service.pool.Exec(env.ctx, `INSERT INTO privacy_notice_recipients(id,campaign_id,user_id,job_id,state,queued_at,accepted_at,failed_at,failure_code) VALUES ($1,$2,$3,$4,$5,CASE WHEN $5<>'unqueued' THEN now() END,CASE WHEN $5='accepted' THEN now() END,CASE WHEN $5='failed' THEN now() END,CASE WHEN $5='failed' THEN 'expired' END)`, id, authmail.PrivacyNoticeID, env.user.ID, job, state); err != nil {
				t.Fatal("notice recipient fixture failed")
			}
			if err := env.service.deleteAccount(env.ctx, env.session); err != nil {
				t.Fatal("account deletion failed")
			}
			want := "deleted"
			if state == "accepted" {
				want = "accepted"
			}
			var resolved bool
			if env.service.pool.QueryRow(env.ctx, `SELECT user_id IS NULL AND state=$2 AND failure_code IS NULL AND (state='accepted' OR deleted_at IS NOT NULL) FROM privacy_notice_recipients WHERE id=$1`, id, want).Scan(&resolved) != nil || !resolved {
				t.Fatal("notice obligation lost or misclassified")
			}
			var unchanged bool
			if env.service.pool.QueryRow(env.ctx, `SELECT expected_count=1 FROM privacy_notice_campaigns`).Scan(&unchanged) != nil || !unchanged {
				t.Fatal("account deletion changed original cohort count")
			}
		})
	}
}

type deletionNoticeAuthority struct{}

// AuthorizeSend isolates database lock behavior from host receipt parsing.
func (deletionNoticeAuthority) AuthorizeSend(context.Context, *store.Queries) bool { return true }

// ContentDigest matches the compiled campaign fixture.
func (deletionNoticeAuthority) ContentDigest() []byte { return privacynotice.ContentDigest() }

func TestAccountDeletionJoinsNoticeSendAndPreservesOutcome(t *testing.T) {
	for _, outcome := range []authmail.SendOutcome{authmail.SendAccepted, authmail.SendTemporaryFailure} {
		name := "accepted"
		if outcome != authmail.SendAccepted {
			name = "temporary"
		}
		t.Run(name, func(t *testing.T) {
			dsn, _ := testutil.NewMigratedTestDatabase(t)
			t.Setenv("TEST_DATABASE_URL", dsn)
			env := newDeletionEnvironment(t)
			ctx, cancel := context.WithTimeout(env.ctx, 10*time.Second)
			defer cancel()
			if _, err := env.service.pool.Exec(ctx, `INSERT INTO privacy_notice_campaigns(id,content_sha256,cutoff_at,expected_count,state,sealed_at) VALUES ($1,$2,now(),1,'sealed',now())`, authmail.PrivacyNoticeID, privacynotice.ContentDigest()); err != nil {
				t.Fatal("notice campaign fixture failed")
			}
			job, recipient := uuid.New(), uuid.New()
			if _, err := env.service.pool.Exec(ctx, `INSERT INTO privacy_notice_recipients(id,campaign_id,user_id,job_id,state,queued_at) VALUES ($1,$2,$3,$4,'queued',now())`, recipient, authmail.PrivacyNoticeID, env.user.ID, job); err != nil {
				t.Fatal("notice recipient fixture failed")
			}
			ring := deletionMailKeyRing(t)
			outbox, err := authmail.NewOutbox(ring, time.Now)
			if err != nil {
				t.Fatal("notice outbox fixture failed")
			}
			if outbox.EnqueueTx(ctx, env.queries, authmail.EnqueueRequest{JobID: job, Kind: authmail.KindPrivacyNotice, UserID: &env.user.ID, Payload: authmail.Payload{Version: 3, To: env.user.Email, UserID: env.user.ID.String(), NoticeID: authmail.PrivacyNoticeID}}) != nil {
				t.Fatal("notice enqueue fixture failed")
			}
			sender := &deletionMailSender{entered: make(chan authmail.Message, 1), release: make(chan struct{}), returned: make(chan struct{}), result: authmail.SendResult{Outcome: outcome}}
			worker, err := authmail.NewWorker(authmail.WorkerOptions{Pool: env.service.pool, Queries: env.queries, KeyRing: ring, Sender: sender, Clock: time.Now, Jitter: func(time.Duration) time.Duration { return 0 }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), WorkerID: uuid.New(), NoticeAuthority: deletionNoticeAuthority{}})
			if err != nil {
				t.Fatal("notice worker fixture failed")
			}
			workerDone := make(chan error, 1)
			go func() { workerDone <- worker.RunOnce(ctx) }()
			select {
			case <-sender.entered:
			case <-ctx.Done():
				t.Fatal("notice send did not start")
			}
			pidChannel := make(chan int, 1)
			begin := env.service.beginTx
			env.service.beginTx = func(ctx context.Context) (pgx.Tx, error) {
				tx, beginErr := begin(ctx)
				if beginErr != nil {
					return nil, beginErr
				}
				var pid int
				if scanErr := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); scanErr != nil {
					if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
						return nil, errors.New("notice fixture rollback failed")
					}
					return nil, errors.New("notice fixture backend probe failed")
				}
				pidChannel <- pid
				return tx, nil
			}
			deleteDone := make(chan error, 1)
			go func() { deleteDone <- env.service.deleteAccount(ctx, env.session) }()
			var pid int
			select {
			case pid = <-pidChannel:
			case <-ctx.Done():
				t.Fatal("notice deletion transaction did not start")
			}
			for {
				var blocked bool
				if env.service.pool.QueryRow(ctx, `SELECT COALESCE(wait_event_type='Lock',false) AND cardinality(pg_blocking_pids(pid))>0 FROM pg_stat_activity WHERE pid=$1`, pid).Scan(&blocked) != nil {
					t.Fatal("notice deletion wait probe failed")
				}
				if blocked {
					break
				}
				if ctx.Err() != nil {
					t.Fatal("notice deletion did not wait for send")
				}
				runtime.Gosched()
			}
			close(sender.release)
			if <-workerDone != nil {
				t.Fatal("notice worker failed")
			}
			if <-deleteDone != nil {
				t.Fatal("notice deletion failed")
			}
			want := "accepted"
			if outcome != authmail.SendAccepted {
				want = "deleted"
			}
			var resolved bool
			if env.service.pool.QueryRow(ctx, `SELECT user_id IS NULL AND state=$2 AND failure_code IS NULL FROM privacy_notice_recipients WHERE id=$1`, recipient, want).Scan(&resolved) != nil || !resolved {
				t.Fatal("in-flight notice deletion outcome differs")
			}
		})
	}
}
