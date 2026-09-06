package accountapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/authmail"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

type deletionMailSender struct {
	entered  chan authmail.Message
	release  chan struct{}
	returned chan struct{}
	result   authmail.SendResult
	err      error
	once     sync.Once
}

type recordingDeletionMailSender struct {
	mu       sync.Mutex
	messages []authmail.Message
}

func (s *recordingDeletionMailSender) Send(_ context.Context, message authmail.Message) (authmail.SendResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, message)
	return authmail.SendResult{Outcome: authmail.SendAccepted}, nil
}

func (s *recordingDeletionMailSender) sentTo(recipient string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, message := range s.messages {
		if message.To == recipient {
			return true
		}
	}
	return false
}

func (s *deletionMailSender) Send(ctx context.Context, message authmail.Message) (authmail.SendResult, error) {
	s.entered <- message
	select {
	case <-s.release:
	case <-ctx.Done():
		s.once.Do(func() { close(s.returned) })
		return authmail.SendResult{Outcome: authmail.SendTemporaryFailure}, ctx.Err()
	}
	s.once.Do(func() { close(s.returned) })
	return s.result, s.err
}

func TestDeleteAccountMailInProgressSendDrainsBeforeCommit(t *testing.T) {
	for _, kind := range []authmail.Kind{
		authmail.KindVerify,
		authmail.KindReset,
		authmail.KindPasswordChanged,
	} {
		for _, outcome := range []struct {
			name   string
			result authmail.SendResult
			err    error
		}{
			{name: "accepted", result: authmail.SendResult{Outcome: authmail.SendAccepted}},
			{name: "ambiguous", result: authmail.SendResult{Outcome: authmail.SendTemporaryFailure}, err: errors.New("delivery outcome unknown")},
		} {
			t.Run(string(kind)+"/"+outcome.name, func(t *testing.T) {
				env := newDeletionEnvironment(t)
				runCtx, cancel := context.WithTimeout(env.ctx, 10*time.Second)
				defer cancel()
				ring := deletionMailKeyRing(t)
				digest, jobID := enqueueDeletionMail(t, env, ring, kind)
				sender := &deletionMailSender{
					entered: make(chan authmail.Message, 1), release: make(chan struct{}),
					returned: make(chan struct{}), result: outcome.result, err: outcome.err,
				}
				worker, err := authmail.NewWorker(authmail.WorkerOptions{
					Pool: env.service.pool, Queries: env.queries, KeyRing: ring, Sender: sender,
					Clock: time.Now, Jitter: func(time.Duration) time.Duration { return 0 },
					Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), WorkerID: uuid.New(),
				})
				if err != nil {
					t.Fatalf("NewWorker: %v", err)
				}

				workerDone := make(chan error, 1)
				go func() { workerDone <- worker.RunOnce(runCtx) }()
				var message authmail.Message
				select {
				case message = <-sender.entered:
				case <-runCtx.Done():
					t.Fatalf("wait for sender: %v", runCtx.Err())
				}
				if message.Kind != kind {
					t.Fatalf("sender kind = %q, want %q", message.Kind, kind)
				}

				deletionPID := make(chan int, 1)
				beginTx := env.service.beginTx
				env.service.beginTx = func(ctx context.Context) (pgx.Tx, error) {
					tx, beginErr := beginTx(ctx)
					if beginErr != nil {
						return nil, beginErr
					}
					var pid int
					if scanErr := tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); scanErr != nil {
						if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
							return nil, errors.Join(scanErr, rollbackErr)
						}
						return nil, scanErr
					}
					select {
					case deletionPID <- pid:
						return tx, nil
					case <-ctx.Done():
						if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
							return nil, errors.Join(ctx.Err(), rollbackErr)
						}
						return nil, ctx.Err()
					}
				}
				env.service.commitTx = func(ctx context.Context, tx pgx.Tx) error {
					select {
					case <-sender.returned:
					default:
						return errors.New("account deletion reached commit before the active mail send returned")
					}
					return tx.Commit(ctx)
				}
				deleteDone := make(chan error, 1)
				go func() { deleteDone <- env.service.deleteAccount(runCtx, env.session) }()
				var pid int
				select {
				case pid = <-deletionPID:
				case <-runCtx.Done():
					t.Fatalf("wait for deletion transaction: %v", runCtx.Err())
				}
				if err := waitForDeletionMailLock(runCtx, env.service.pool, pid); err != nil {
					t.Fatal(err)
				}
				close(sender.release)

				select {
				case err := <-workerDone:
					if err != nil {
						t.Fatalf("RunOnce: %v", err)
					}
				case <-runCtx.Done():
					t.Fatalf("wait for worker: %v", runCtx.Err())
				}
				select {
				case err := <-deleteDone:
					if err != nil {
						t.Fatalf("deleteAccount: %v", err)
					}
				case <-runCtx.Done():
					t.Fatalf("wait for deletion: %v", runCtx.Err())
				}
				assertDeletedMailScope(t, env, kind, digest, jobID)
			})
		}
	}
}

func waitForDeletionMailLock(ctx context.Context, pool *store.Pool, pid int) error {
	for {
		var blocked bool
		var query string
		err := pool.QueryRow(ctx, `
			SELECT deletion.wait_event_type = 'Lock' AND EXISTS (
				SELECT 1
				FROM pg_stat_activity AS blocker
				WHERE blocker.pid = ANY(pg_blocking_pids(deletion.pid))
				  AND blocker.state = 'idle in transaction'
				  AND blocker.query LIKE '%auth_email_jobs%'
			), deletion.query
			FROM pg_stat_activity AS deletion
			WHERE deletion.pid = $1`, pid).Scan(&blocked, &query)
		if err != nil {
			return errors.New("observe deletion mail lock: " + err.Error())
		}
		if blocked {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("deletion never waited on the active mail scope lock; last query: " + query)
		default:
			runtime.Gosched()
		}
	}
}

func TestDeleteAccountMailClaimedBeforeSendCannotSendAfterDeletion(t *testing.T) {
	for _, kind := range []authmail.Kind{
		authmail.KindVerify,
		authmail.KindReset,
		authmail.KindPasswordChanged,
	} {
		t.Run(string(kind), func(t *testing.T) {
			env := newDeletionEnvironment(t)
			ring := deletionMailKeyRing(t)
			_, jobID := enqueueDeletionMail(t, env, ring, kind)
			now := time.Now().UTC()
			tx, err := env.service.pool.Begin(env.ctx)
			if err != nil {
				t.Fatalf("Begin claim: %v", err)
			}
			claimed, err := env.queries.WithTx(tx).ClaimAuthEmailJobs(env.ctx, store.ClaimAuthEmailJobsParams{
				LeaseOwner: "deletion-mail-proof", LeaseExpiresAt: now.Add(30 * time.Second),
				Now: now, LimitRows: 1,
			})
			if err != nil {
				if rollbackErr := tx.Rollback(env.ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
					t.Fatalf("ClaimAuthEmailJobs: %v; Rollback: %v", err, rollbackErr)
				}
				t.Fatalf("ClaimAuthEmailJobs: %v", err)
			}
			if commitErr := tx.Commit(env.ctx); commitErr != nil {
				t.Fatalf("Commit claim: %v", commitErr)
			}
			if len(claimed) != 1 || claimed[0].ID != jobID {
				t.Fatalf("claimed jobs = %+v, want only %s", claimed, jobID)
			}

			if deleteErr := env.service.deleteAccount(env.ctx, env.session); deleteErr != nil {
				t.Fatalf("deleteAccount: %v", deleteErr)
			}
			sender := &recordingDeletionMailSender{}
			worker, err := authmail.NewWorker(authmail.WorkerOptions{
				Pool: env.service.pool, Queries: env.queries, KeyRing: ring, Sender: sender,
				Clock:  func() time.Time { return now.Add(31 * time.Second) },
				Jitter: func(time.Duration) time.Duration { return 0 },
				Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), WorkerID: uuid.New(),
			})
			if err != nil {
				t.Fatalf("NewWorker: %v", err)
			}
			if err := worker.RunOnce(env.ctx); err != nil {
				t.Fatalf("RunOnce after deletion: %v", err)
			}
			if sender.sentTo(env.user.Email) {
				t.Fatalf("worker sent %s mail after deleting its claimed scope", kind)
			}
			var count int
			if err := env.service.pool.QueryRow(env.ctx, `SELECT count(*) FROM auth_email_jobs WHERE id=$1`, jobID).Scan(&count); err != nil || count != 0 {
				t.Fatalf("claimed mail job after deletion: count=%d error=%v, want absent", count, err)
			}
		})
	}
}

func deletionMailKeyRing(t *testing.T) *authmail.KeyRing {
	t.Helper()
	var key [32]byte
	copy(key[:], []byte("phase-8-account-mail-test-key-000"))
	ring, err := authmail.NewKeyRing("active", map[string][32]byte{"active": key}, bytes.NewReader(bytes.Repeat([]byte{7}, 12)))
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	return ring
}

func enqueueDeletionMail(t *testing.T, env deletionEnvironment, ring *authmail.KeyRing, kind authmail.Kind) ([32]byte, uuid.UUID) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	var digest [32]byte
	copy(digest[:], deletionDigest())
	jobID := uuid.New()
	tx, err := env.service.pool.Begin(env.ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(context.Background()); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("Rollback fixture: %v", rollbackErr)
		}
	}()
	qtx := env.queries.WithTx(tx)
	req := authmail.EnqueueRequest{
		JobID: jobID, Kind: kind, Payload: authmail.Payload{Version: 1, To: env.user.Email},
		ExpiresAt: now.Add(time.Hour),
	}
	switch kind {
	case authmail.KindVerify:
		registration, createErr := qtx.CreatePasswordRegistration(env.ctx, store.CreatePasswordRegistrationParams{
			Email: env.user.Email, Name: "Deleted registration", EncodedHash: []byte("hash"),
			TokenDigest: digest[:], CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
		})
		if createErr != nil {
			t.Fatalf("CreatePasswordRegistration: %v", createErr)
		}
		req.RegistrationID = &registration.ID
		req.TokenDigest = &digest
		req.Payload.Link = "https://aboutme.vn/verify-email#token=deleted-token"
	case authmail.KindReset:
		reset, createErr := qtx.CreatePasswordResetToken(env.ctx, store.CreatePasswordResetTokenParams{
			UserID: env.user.ID, TokenDigest: digest[:], CreatedAt: now, ExpiresAt: now.Add(30 * time.Minute),
		})
		if createErr != nil {
			t.Fatalf("CreatePasswordResetToken: %v", createErr)
		}
		req.ResetTokenID = &reset.ID
		req.TokenDigest = &digest
		req.Payload.Link = "https://aboutme.vn/reset-password#token=deleted-token"
	case authmail.KindPasswordChanged:
		req.UserID = &env.user.ID
	default:
		t.Fatalf("unsupported mail kind %q", kind)
	}
	outbox, err := authmail.NewOutbox(ring, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewOutbox: %v", err)
	}
	if err := outbox.EnqueueTx(env.ctx, qtx, req); err != nil {
		t.Fatalf("EnqueueTx: %v", err)
	}
	if err := tx.Commit(env.ctx); err != nil {
		t.Fatalf("Commit fixture: %v", err)
	}
	if _, err := env.service.pool.Exec(env.ctx,
		`UPDATE auth_email_jobs SET next_attempt_at=$2 WHERE id=$1`, jobID, now.Add(-100*365*24*time.Hour),
	); err != nil {
		t.Fatalf("prioritize mail fixture: %v", err)
	}
	return digest, jobID
}

func assertDeletedMailScope(t *testing.T, env deletionEnvironment, kind authmail.Kind, digest [32]byte, jobID uuid.UUID) {
	t.Helper()
	var count int
	if err := env.service.pool.QueryRow(env.ctx, `SELECT count(*) FROM auth_email_jobs WHERE id=$1`, jobID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("mail job after deletion: count=%d error=%v, want absent", count, err)
	}
	switch kind {
	case authmail.KindVerify:
		_, err := env.queries.GetPasswordRegistrationByDigest(env.ctx, digest[:])
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("verification token lookup after deletion = %v, want token invalid", err)
		}
	case authmail.KindReset:
		_, err := env.queries.GetPasswordResetTokenByDigest(env.ctx, digest[:])
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("reset token lookup after deletion = %v, want token invalid", err)
		}
	case authmail.KindPasswordChanged:
		_, err := env.queries.GetUserByID(env.ctx, env.user.ID)
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("password-changed user lookup after deletion = %v, want absent", err)
		}
	}
}
