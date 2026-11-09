package authmail

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

func TestWorker_RejectsNoticeRestoredAfterOrdinaryClaim(t *testing.T) {
	ctx, worker, authority, id := noticeWorkerFixture(t, SendAccepted)
	pool := worker.queriesDB()
	original := readAADJob(ctx, t, pool, id)
	if _, err := pool.Exec(ctx, `UPDATE auth_email_jobs SET kind='password_changed' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	authority.allowed = false
	restored := false
	worker.hooks.afterClaim = func(jobs []store.AuthEmailJob) {
		if len(jobs) != 1 || jobs[0].ID != id || jobs[0].Kind != string(KindPasswordChanged) {
			t.Fatal("notice was not claimed as ordinary mail")
		}
		if _, err := pool.Exec(ctx, `UPDATE auth_email_jobs SET kind='privacy_notice' WHERE id=$1`, id); err != nil {
			t.Fatal(err)
		}
		restored = true
	}
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if !restored || noticeSenderCalls(t, worker) != 0 {
		t.Fatal("restored notice bypassed ordinary claim authentication")
	}
	assertClaimMismatchTerminal(ctx, t, pool, id)
	var unchanged bool
	if err := pool.QueryRow(ctx, `SELECT state='queued' AND accepted_at IS NULL AND failure_code IS NULL
		FROM privacy_notice_recipients WHERE job_id=$1`, id).Scan(&unchanged); err != nil || !unchanged {
		t.Fatalf("ordinary failure changed the notice ledger: %v", err)
	}
	assertClaimAccountUnlocked(ctx, t, pool, *original.UserID)
}

func TestWorker_RejectsUserRestoredAfterClaim(t *testing.T) {
	ctx, pool, q := newWorkerPool(t)
	clock := testutil.NewClockAtEpoch()
	ring := testRing(t, 1)
	user, err := q.CreateUser(ctx, store.CreateUserParams{Email: "original@example.com", Name: "Fixture"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := q.CreateUser(ctx, store.CreateUserParams{Email: "other@example.com", Name: "Fixture"})
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if err := newTestOutbox(t, ring, clock.Now).EnqueueTx(ctx, q, EnqueueRequest{
		JobID: id, Kind: KindPasswordChanged, UserID: &user.ID,
		Payload: validPayloadForKind(KindPasswordChanged), ExpiresAt: clock.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE auth_email_jobs SET user_id=$2 WHERE id=$1`, id, other.ID); err != nil {
		t.Fatal(err)
	}
	sender := &stubSender{result: SendResult{Outcome: SendAccepted}}
	worker := newTestWorker(t, pool, q, ring, sender, clock, nil, uuid.New())
	restoreUserAfterClaim(ctx, t, worker, pool, id, other.ID, user.ID)
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if sender.count() != 0 {
		t.Fatal("restored user sent under a substituted account lock")
	}
	assertClaimMismatchTerminal(ctx, t, pool, id)
	assertClaimAccountUnlocked(ctx, t, pool, other.ID)
}

func TestPrivacyNoticeWorker_RejectsUserRestoredAfterClaim(t *testing.T) {
	ctx, worker, _, id := noticeWorkerFixture(t, SendAccepted)
	pool := worker.queriesDB()
	original := readAADJob(ctx, t, pool, id)
	other, err := worker.queries.CreateUser(ctx, store.CreateUserParams{Email: "other@example.com", Name: "Fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE auth_email_jobs SET user_id=$2 WHERE id=$1`, id, other.ID); err != nil {
		t.Fatal(err)
	}
	restoreUserAfterClaim(ctx, t, worker, pool, id, other.ID, *original.UserID)
	if err := worker.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if noticeSenderCalls(t, worker) != 0 {
		t.Fatal("notice sent with a different claimed account")
	}
	assertClaimMismatchTerminal(ctx, t, pool, id)
	var failed bool
	if err := pool.QueryRow(ctx, `SELECT state='failed' AND failure_code='payload_invalid' AND accepted_at IS NULL
		FROM privacy_notice_recipients WHERE job_id=$1`, id).Scan(&failed); err != nil || !failed {
		t.Fatalf("notice mismatch did not use payload failure: %v", err)
	}
	assertClaimAccountUnlocked(ctx, t, pool, other.ID)
}

func restoreUserAfterClaim(ctx context.Context, t *testing.T, worker *Worker, pool *store.Pool, jobID, substituted, original uuid.UUID) {
	t.Helper()
	restored := false
	worker.hooks.afterClaim = func(jobs []store.AuthEmailJob) {
		if len(jobs) != 1 || jobs[0].ID != jobID || jobs[0].UserID == nil || *jobs[0].UserID != substituted {
			t.Fatal("job was not claimed with the substituted user")
		}
		if _, err := pool.Exec(ctx, `UPDATE auth_email_jobs SET user_id=$2 WHERE id=$1`, jobID, original); err != nil {
			t.Fatal(err)
		}
		restored = true
	}
	t.Cleanup(func() {
		if !restored {
			t.Error("after-claim restoration did not run")
		}
	})
}

func assertClaimMismatchTerminal(ctx context.Context, t *testing.T, pool *store.Pool, id uuid.UUID) {
	t.Helper()
	var terminal bool
	err := pool.QueryRow(ctx, `SELECT state='terminal' AND key_id IS NULL AND nonce IS NULL AND ciphertext IS NULL
		AND lease_owner IS NULL AND lease_expires_at IS NULL AND sent_at IS NULL
		FROM auth_email_jobs WHERE id=$1`, id).Scan(&terminal)
	if err != nil || !terminal {
		t.Fatalf("claim mismatch was not terminal and cleared: %v", err)
	}
}

func assertClaimAccountUnlocked(ctx context.Context, t *testing.T, pool *store.Pool, id uuid.UUID) {
	t.Helper()
	tx := beginWorkerTx(ctx, t, pool)
	var locked uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT`, id).Scan(&locked); err != nil {
		t.Fatalf("substituted account retains a row lock: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSameClaimedAuthority_RequiresMatchingKindAndScope(t *testing.T) {
	user, registration, reset := uuid.New(), uuid.New(), uuid.New()
	other, zero := uuid.New(), uuid.Nil
	copiedUser := user
	original := store.AuthEmailJob{
		Kind: string(KindVerify), UserID: &user, RegistrationID: &registration,
		ResetTokenID: &reset, TokenDigest: []byte{1, 2, 3},
	}
	for _, tc := range []struct {
		name string
		edit func(*store.AuthEmailJob)
		want bool
	}{
		{"unchanged", func(*store.AuthEmailJob) {}, true},
		{"equal UUID value", func(j *store.AuthEmailJob) { j.UserID = &copiedUser }, true},
		{"equal digest value", func(j *store.AuthEmailJob) { j.TokenDigest = []byte{1, 2, 3} }, true},
		{"kind", func(j *store.AuthEmailJob) { j.Kind = string(KindReset) }, false},
		{"user_id", func(j *store.AuthEmailJob) { j.UserID = &other }, false},
		{"registration_id", func(j *store.AuthEmailJob) { j.RegistrationID = &other }, false},
		{"reset_token_id", func(j *store.AuthEmailJob) { j.ResetTokenID = &other }, false},
		{"token_digest", func(j *store.AuthEmailJob) { j.TokenDigest = []byte{1, 2, 4} }, false},
		{"absent user", func(j *store.AuthEmailJob) { j.UserID = nil }, false},
		{"absent registration", func(j *store.AuthEmailJob) { j.RegistrationID = nil }, false},
		{"absent reset token", func(j *store.AuthEmailJob) { j.ResetTokenID = nil }, false},
		{"absent digest", func(j *store.AuthEmailJob) { j.TokenDigest = nil }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := original
			tc.edit(&changed)
			if got := sameClaimedAuthority(original, changed); got != tc.want {
				t.Fatalf("authority equality = %v, want %v", got, tc.want)
			}
			if got := sameClaimedAuthority(changed, original); got != tc.want {
				t.Fatalf("reverse authority equality = %v, want %v", got, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		name string
		job  store.AuthEmailJob
	}{
		{"present zero user", store.AuthEmailJob{UserID: &zero}},
		{"present zero registration", store.AuthEmailJob{RegistrationID: &zero}},
		{"present zero reset token", store.AuthEmailJob{ResetTokenID: &zero}},
		{"present empty digest", store.AuthEmailJob{TokenDigest: []byte{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if sameClaimedAuthority(store.AuthEmailJob{}, tc.job) || sameClaimedAuthority(tc.job, store.AuthEmailJob{}) {
				t.Fatal("present authority matched SQL NULL")
			}
		})
	}
	if !sameClaimedAuthority(store.AuthEmailJob{}, store.AuthEmailJob{}) {
		t.Fatal("absent authorities differ")
	}
}
