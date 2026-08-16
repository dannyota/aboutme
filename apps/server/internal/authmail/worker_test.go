package authmail

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// --- Unit tests (no database) ---

func TestNewWorkerRejectsInvalidOptions(t *testing.T) {
	clock := testutil.NewClockAtEpoch()
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	pool := &store.Pool{}
	queries := store.New(pool)
	sender := &stubSender{result: SendResult{Outcome: SendAccepted}}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	valid := WorkerOptions{
		Pool: pool, Queries: queries, KeyRing: ring, Sender: sender,
		Clock: clock.Now, Jitter: func(d time.Duration) time.Duration { return 0 },
		Logger: logger, WorkerID: uuid.New(),
	}
	if _, err := NewWorker(valid); err != nil {
		t.Fatalf("NewWorker(valid) = %v, want nil", err)
	}
	cases := []struct {
		name string
		mut  func(*WorkerOptions)
	}{
		{"nil pool", func(o *WorkerOptions) { o.Pool = nil }},
		{"nil queries", func(o *WorkerOptions) { o.Queries = nil }},
		{"nil ring", func(o *WorkerOptions) { o.KeyRing = nil }},
		{"nil sender", func(o *WorkerOptions) { o.Sender = nil }},
		{"nil clock", func(o *WorkerOptions) { o.Clock = nil }},
		{"nil jitter", func(o *WorkerOptions) { o.Jitter = nil }},
		{"nil logger", func(o *WorkerOptions) { o.Logger = nil }},
		{"nil worker id", func(o *WorkerOptions) { o.WorkerID = uuid.Nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := valid
			tc.mut(&opts)
			if _, err := NewWorker(opts); !errors.Is(err, ErrWorker) {
				t.Fatalf("err = %v, want ErrWorker", err)
			}
		})
	}
}

func TestBackoffCap(t *testing.T) {
	cases := []struct {
		attempt int32
		want    time.Duration
	}{
		{0, 30 * time.Second},
		{1, 30 * time.Second},
		{2, 60 * time.Second},
		{3, 120 * time.Second},
		{4, 240 * time.Second},
		{5, 480 * time.Second},
		{6, 960 * time.Second},
		{7, 1920 * time.Second},
		{8, maxBackoff},
		{9, maxBackoff},
		{20, maxBackoff},
	}
	for _, tc := range cases {
		if got := backoffCap(tc.attempt); got != tc.want {
			t.Errorf("backoffCap(%d) = %v, want %v", tc.attempt, got, tc.want)
		}
	}
}

func TestBuildMessageTemplatesAndEscaping(t *testing.T) {
	p := Payload{Version: payloadVersion, To: "alice@example.com", Link: verifyLinkPrefix + `t<">&`}
	msg := buildMessage(KindVerify, p)
	if msg.Kind != KindVerify || msg.To != "alice@example.com" {
		t.Fatalf("msg = %+v, want verify to alice@example.com", msg)
	}
	if msg.Subject != "Verify your email" {
		t.Errorf("subject = %q, want fixed subject", msg.Subject)
	}
	if msg.TextBody != "Confirm your email address by opening this link:\n"+p.Link {
		t.Errorf("text body missing link: %q", msg.TextBody)
	}
	// The HTML body must escape the link value (which carries an attacker
	// controlled token suffix).
	if !strings.Contains(msg.HTMLBody, "t&lt;&#34;&gt;&amp;") {
		t.Errorf("html body does not escape link: %q", msg.HTMLBody)
	}
	if strings.Contains(msg.HTMLBody, p.Link) {
		t.Errorf("html body leaks raw link: %q", msg.HTMLBody)
	}

	reset := buildMessage(KindReset, Payload{Version: 1, To: "b@c.d", Link: resetLinkPrefix + "tok"})
	if reset.Subject != "Reset your password" {
		t.Errorf("reset subject = %q", reset.Subject)
	}
	changed := buildMessage(KindPasswordChanged, Payload{Version: 1, To: "b@c.d"})
	if changed.Subject != "Your password was changed" {
		t.Errorf("password_changed subject = %q", changed.Subject)
	}
	if strings.Contains(changed.HTMLBody, "http") {
		t.Errorf("password_changed html must not contain a link: %q", changed.HTMLBody)
	}
}

func TestSealedFromJob(t *testing.T) {
	nonce := make([]byte, 12)
	for i := range nonce {
		nonce[i] = byte(i)
	}
	keyID := "k-active"
	job := store.AuthEmailJob{
		ID: uuid.New(), Kind: "verify", KeyID: &keyID,
		Nonce: nonce, Ciphertext: []byte{1, 2, 3},
	}
	got := sealedFromJob(job)
	if got.KeyID != keyID || got.Nonce != [12]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11} || !bytes.Equal(got.Ciphertext, []byte{1, 2, 3}) {
		t.Fatalf("sealedFromJob = %+v", got)
	}
}

// --- Live-database tests (skip unless TEST_DATABASE_URL is set) ---

type stubSender struct {
	mu     sync.Mutex
	calls  []Message
	result SendResult
	err    error
}

func (s *stubSender) Send(_ context.Context, m Message) (SendResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, m)
	return s.result, s.err
}

func (s *stubSender) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// countingSender records the peak number of concurrent Send calls.
type countingSender struct {
	mu        sync.Mutex
	active    int
	maxActive int
	calls     int
	result    SendResult
}

func (s *countingSender) Send(ctx context.Context, _ Message) (SendResult, error) {
	s.mu.Lock()
	s.active++
	if s.active > s.maxActive {
		s.maxActive = s.active
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.active--
		s.mu.Unlock()
	}()
	select {
	case <-ctx.Done():
		return SendResult{Outcome: SendTemporaryFailure}, ctx.Err()
	case <-time.After(60 * time.Millisecond):
	}
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	return s.result, nil
}

func (s *countingSender) resultCount() (calls, maxActive int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls, s.maxActive
}

// hangSender enters Send, signals, and blocks until ctx is done.
type hangSender struct {
	entered chan struct{}
	result  SendResult
}

func (s *hangSender) Send(ctx context.Context, _ Message) (SendResult, error) {
	close(s.entered)
	<-ctx.Done()
	return s.result, ctx.Err()
}

// deadlineProbeSender returns the send context's deadline to the test via a
// channel and immediately accepts, proving the send runs under a deadline.
type deadlineProbeSender struct {
	deadline chan time.Time
}

func (s *deadlineProbeSender) Send(ctx context.Context, _ Message) (SendResult, error) {
	if dl, ok := ctx.Deadline(); ok {
		s.deadline <- dl
	} else {
		s.deadline <- time.Time{}
	}
	return SendResult{Outcome: SendAccepted}, nil
}

// testRing returns a key ring whose nonce source can Seal at least n messages.
// A ring from fixedNonce() carries exactly one nonce (Seal consumes it), so
// tests that enqueue several jobs must build a larger reader.
func testRing(t *testing.T, n int) *KeyRing {
	t.Helper()
	return mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, bytes.Repeat(fixedNonce(), n+1))
}

// beginWorkerTx begins a transaction and guarantees it is rolled back when the
// test ends, so a mid-test failure releases the pool connection instead of
// hanging the pool.Close cleanup.
func beginWorkerTx(t *testing.T, ctx context.Context, pool *store.Pool) pgx.Tx {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(ctx)) })
	return tx
}

func newWorkerPool(t *testing.T) (context.Context, *store.Pool, *store.Queries) {
	t.Helper()
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	sp := &store.Pool{Pool: pool}
	return ctx, sp, store.New(sp)
}

// uniqueDigest returns 32 bytes that differ per call. password_registrations
// has a UNIQUE token_digest, so tests creating several registrations must not
// reuse the fixed testDigest.
func uniqueDigest() [32]byte {
	var d [32]byte
	copy(d[:], []byte(uuid.NewString()+uuid.NewString()))
	return d
}

func newWorkerRegistration(ctx context.Context, t *testing.T, sp *store.Pool, now time.Time) (uuid.UUID, [32]byte) {
	t.Helper()
	q := store.New(sp)
	digest := uniqueDigest()
	reg, err := q.CreatePasswordRegistration(ctx, store.CreatePasswordRegistrationParams{
		Email:       uuid.NewString() + "@example.com",
		Name:        "Worker Test",
		EncodedHash: []byte("hash"),
		TokenDigest: digest[:],
		CreatedAt:   now,
		ExpiresAt:   now.Add(24 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CreatePasswordRegistration: %v", err)
	}
	t.Cleanup(func() {
		// A bounded context so a leftover referenced row (an abandoned test tx)
		// surfaces as an error instead of hanging the whole suite forever.
		dctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := store.New(sp).DeletePasswordRegistration(dctx, reg.ID); err != nil {
			t.Errorf("cleanup registration: %v", err)
		}
	})
	return reg.ID, digest
}

func enqueueVerifyJob(ctx context.Context, t *testing.T, qtx *store.Queries, ring *KeyRing, clock func() time.Time, regID uuid.UUID, digest [32]byte) uuid.UUID {
	t.Helper()
	o := newTestOutbox(t, ring, clock)
	jobID := uuid.New()
	if err := o.EnqueueTx(ctx, qtx, EnqueueRequest{
		JobID:          jobID,
		Kind:           KindVerify,
		RegistrationID: &regID,
		TokenDigest:    &digest,
		Payload:        validVerifyPayload(),
		ExpiresAt:      clock().Add(time.Hour),
	}); err != nil {
		t.Fatalf("EnqueueTx: %v", err)
	}
	return jobID
}

func newTestWorker(t *testing.T, sp *store.Pool, q *store.Queries, ring *KeyRing, sender Sender, clock *testutil.Clock, jitter func(time.Duration) time.Duration, id uuid.UUID) *Worker {
	t.Helper()
	if jitter == nil {
		jitter = func(d time.Duration) time.Duration { return 0 }
	}
	w, err := NewWorker(WorkerOptions{
		Pool: sp, Queries: q, KeyRing: ring, Sender: sender,
		Clock: clock.Now, Jitter: jitter,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		WorkerID: id,
	})
	if err != nil {
		t.Fatalf("NewWorker: %v", err)
	}
	return w
}

func jobState(ctx context.Context, t *testing.T, sp *store.Pool, jobID uuid.UUID) (state string, leasedTo string) {
	t.Helper()
	var leaseOwner *string
	if err := sp.QueryRow(ctx,
		`SELECT state, lease_owner FROM auth_email_jobs WHERE id = $1`, jobID,
	).Scan(&state, &leaseOwner); err != nil {
		t.Fatalf("read job state: %v", err)
	}
	if leaseOwner != nil {
		leasedTo = *leaseOwner
	}
	return state, leasedTo
}

func near(a, b time.Time) bool {
	return a.Sub(b).Abs() < time.Millisecond
}

func TestWorkerRunOnceSendsAndMarksSent(t *testing.T) {
	ctx, sp, q := newWorkerPool(t)
	clock := testutil.NewClockAtEpoch()
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	regID, digest := newWorkerRegistration(ctx, t, sp, clock.Now())

	tx := beginWorkerTx(t, ctx, sp)
	jobID := enqueueVerifyJob(ctx, t, q.WithTx(tx), ring, clock.Now, regID, digest)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	sender := &stubSender{result: SendResult{Outcome: SendAccepted}}
	w := newTestWorker(t, sp, q, ring, sender, clock, nil, uuid.New())
	if err := w.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if sender.count() != 1 {
		t.Fatalf("sends = %d, want 1", sender.count())
	}
	msg := sender.calls[0]
	if msg.To != "alice@example.com" || msg.Subject != "Verify your email" {
		t.Fatalf("message = %+v", msg)
	}
	state, leased := jobState(ctx, t, sp, jobID)
	if state != "sent" {
		t.Fatalf("state = %q, want sent", state)
	}
	if leased != "" {
		t.Fatalf("lease still set on sent job: %q", leased)
	}
	var keyID, nonce, ct *[]byte
	if err := sp.QueryRow(ctx, `SELECT key_id, nonce, ciphertext FROM auth_email_jobs WHERE id=$1`, jobID).Scan(&keyID, &nonce, &ct); err != nil {
		t.Fatalf("read job columns: %v", err)
	}
	if keyID != nil || nonce != nil || ct != nil {
		t.Fatalf("sent job retains sealed/lease fields: key=%v nonce=%v ct=%v", keyID != nil, nonce != nil, ct != nil)
	}
}

func TestWorkerTemporaryFailureRequeuesWithBackoff(t *testing.T) {
	ctx, sp, q := newWorkerPool(t)
	clock := testutil.NewClockAtEpoch()
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	regID, digest := newWorkerRegistration(ctx, t, sp, clock.Now())
	tx := beginWorkerTx(t, ctx, sp)
	jobID := enqueueVerifyJob(ctx, t, q.WithTx(tx), ring, clock.Now, regID, digest)
	_ = tx.Commit(ctx)

	// Identity jitter: next = now + backoffCap(attempt). First claim makes
	// attempts = 1, so the cap is 30s.
	sender := &stubSender{result: SendResult{Outcome: SendTemporaryFailure}}
	w := newTestWorker(t, sp, q, ring, sender, clock, func(d time.Duration) time.Duration { return d }, uuid.New())
	if err := w.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	state, _ := jobState(ctx, t, sp, jobID)
	if state != "pending" {
		t.Fatalf("state = %q, want pending (requeued)", state)
	}
	var attempts int32
	var next *time.Time
	if err := sp.QueryRow(ctx, `SELECT attempts, next_attempt_at FROM auth_email_jobs WHERE id=$1`, jobID).Scan(&attempts, &next); err != nil {
		t.Fatalf("read: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (claim increment, not rolled back)", attempts)
	}
	if next == nil || !near(*next, clock.Now().Add(30*time.Second)) {
		t.Fatalf("next_attempt_at = %v, want ~%v", next, clock.Now().Add(30*time.Second))
	}
}

func TestWorkerSenderErrorIsTemporary(t *testing.T) {
	ctx, sp, q := newWorkerPool(t)
	clock := testutil.NewClockAtEpoch()
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	regID, digest := newWorkerRegistration(ctx, t, sp, clock.Now())
	tx := beginWorkerTx(t, ctx, sp)
	jobID := enqueueVerifyJob(ctx, t, q.WithTx(tx), ring, clock.Now, regID, digest)
	_ = tx.Commit(ctx)

	// The sender claims success but returns a transport error: ambiguous, so the
	// outcome is forced temporary and the job is requeued, not marked sent.
	sender := &stubSender{result: SendResult{Outcome: SendAccepted}, err: errors.New("ambiguous transport failure")}
	w := newTestWorker(t, sp, q, ring, sender, clock, nil, uuid.New())
	if err := w.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if sender.count() != 1 {
		t.Fatalf("sends = %d, want 1", sender.count())
	}
	state, _ := jobState(ctx, t, sp, jobID)
	if state != "pending" {
		t.Fatalf("state = %q, want pending (sender error is temporary)", state)
	}
}

func TestWorkerTemporaryFailureAttempt8Terminal(t *testing.T) {
	ctx, sp, q := newWorkerPool(t)
	clock := testutil.NewClockAtEpoch()
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	regID, digest := newWorkerRegistration(ctx, t, sp, clock.Now())
	tx := beginWorkerTx(t, ctx, sp)
	jobID := enqueueVerifyJob(ctx, t, q.WithTx(tx), ring, clock.Now, regID, digest)
	_ = tx.Commit(ctx)
	// Force attempts to 7 (after commit, so the row is visible) so the claim
	// reaches 8, which must go terminal.
	if _, err := sp.Exec(ctx, `UPDATE auth_email_jobs SET attempts = 7 WHERE id = $1`, jobID); err != nil {
		t.Fatalf("set attempts: %v", err)
	}

	sender := &stubSender{result: SendResult{Outcome: SendTemporaryFailure}}
	w := newTestWorker(t, sp, q, ring, sender, clock, nil, uuid.New())
	if err := w.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	state, _ := jobState(ctx, t, sp, jobID)
	if state != "terminal" {
		t.Fatalf("state = %q, want terminal at attempt 8", state)
	}
}

func TestWorkerPermanentFailureTerminal(t *testing.T) {
	ctx, sp, q := newWorkerPool(t)
	clock := testutil.NewClockAtEpoch()
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	regID, digest := newWorkerRegistration(ctx, t, sp, clock.Now())
	tx := beginWorkerTx(t, ctx, sp)
	jobID := enqueueVerifyJob(ctx, t, q.WithTx(tx), ring, clock.Now, regID, digest)
	_ = tx.Commit(ctx)

	sender := &stubSender{result: SendResult{Outcome: SendPermanentFailure}}
	w := newTestWorker(t, sp, q, ring, sender, clock, nil, uuid.New())
	if err := w.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	state, _ := jobState(ctx, t, sp, jobID)
	if state != "terminal" {
		t.Fatalf("state = %q, want terminal", state)
	}
}

func TestWorkerExpiryTerminalWithoutSend(t *testing.T) {
	ctx, sp, q := newWorkerPool(t)
	clock := testutil.NewClockAtEpoch()
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	regID, digest := newWorkerRegistration(ctx, t, sp, clock.Now())
	tx := beginWorkerTx(t, ctx, sp)
	jobID := enqueueVerifyJob(ctx, t, q.WithTx(tx), ring, clock.Now, regID, digest)
	_ = tx.Commit(ctx)

	clock.Advance(2 * time.Hour) // job expires after 1h

	sender := &stubSender{result: SendResult{Outcome: SendAccepted}}
	w := newTestWorker(t, sp, q, ring, sender, clock, nil, uuid.New())
	if err := w.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	if sender.count() != 0 {
		t.Fatalf("sends = %d, want 0 (expired job must not send)", sender.count())
	}
	state, _ := jobState(ctx, t, sp, jobID)
	if state != "terminal" {
		t.Fatalf("state = %q, want terminal", state)
	}
}

func TestWorkerReplacementTerminalWithoutSend(t *testing.T) {
	ctx, sp, q := newWorkerPool(t)
	clock := testutil.NewClockAtEpoch()
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	regID, digest := newWorkerRegistration(ctx, t, sp, clock.Now())
	tx := beginWorkerTx(t, ctx, sp)
	jobID := enqueueVerifyJob(ctx, t, q.WithTx(tx), ring, clock.Now, regID, digest)
	_ = tx.Commit(ctx)

	// Claim the job to this worker, then simulate a replacement that rotated
	// the scope's token digest after the claim (scope-before-job ordering).
	sender := &stubSender{result: SendResult{Outcome: SendAccepted}}
	w := newTestWorker(t, sp, q, ring, sender, clock, nil, uuid.New())
	claimed, err := w.claim(ctx, clock.Now())
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed = %d, want 1", len(claimed))
	}
	newDigest := make([]byte, 32)
	for i := range newDigest {
		newDigest[i] = 0xcc
	}
	if _, err := sp.Exec(ctx, `UPDATE password_registrations SET token_digest = $2 WHERE id = $1`, regID, newDigest); err != nil {
		t.Fatalf("rotate digest: %v", err)
	}

	if err := w.sendJob(ctx, claimed[0]); err != nil {
		t.Fatalf("sendJob: %v", err)
	}
	if sender.count() != 0 {
		t.Fatalf("sends = %d, want 0 (stale token must not send)", sender.count())
	}
	state, _ := jobState(ctx, t, sp, jobID)
	if state != "terminal" {
		t.Fatalf("state = %q, want terminal", state)
	}
}

func TestWorkerMissingScopeTerminalOrCancelled(t *testing.T) {
	ctx, sp, q := newWorkerPool(t)
	clock := testutil.NewClockAtEpoch()
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	regID, digest := newWorkerRegistration(ctx, t, sp, clock.Now())
	tx := beginWorkerTx(t, ctx, sp)
	jobID := enqueueVerifyJob(ctx, t, q.WithTx(tx), ring, clock.Now, regID, digest)
	_ = tx.Commit(ctx)

	sender := &stubSender{result: SendResult{Outcome: SendAccepted}}
	w := newTestWorker(t, sp, q, ring, sender, clock, nil, uuid.New())
	claimed, err := w.claim(ctx, clock.Now())
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	// Delete the scope; the cascade removes the job, exactly the replacement
	// "cancel" path.
	if _, err := sp.Exec(ctx, `DELETE FROM password_registrations WHERE id = $1`, regID); err != nil {
		t.Fatalf("delete scope: %v", err)
	}

	if err := w.sendJob(ctx, claimed[0]); err != nil {
		t.Fatalf("sendJob: %v", err)
	}
	if sender.count() != 0 {
		t.Fatalf("sends = %d, want 0", sender.count())
	}
	var n int
	if err := sp.QueryRow(ctx, `SELECT count(*) FROM auth_email_jobs WHERE id = $1`, jobID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Fatalf("job rows = %d, want 0 (cascaded by replacement)", n)
	}
}

func TestWorkerStaleLeaseDoesNotSendOrFinalize(t *testing.T) {
	ctx, sp, q := newWorkerPool(t)
	clock := testutil.NewClockAtEpoch()
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	regID, digest := newWorkerRegistration(ctx, t, sp, clock.Now())
	tx := beginWorkerTx(t, ctx, sp)
	jobID := enqueueVerifyJob(ctx, t, q.WithTx(tx), ring, clock.Now, regID, digest)
	_ = tx.Commit(ctx)

	// Worker A claims the job, its lease lapses, and worker B claims it before
	// A's send handoff runs. A must not send or finalize.
	workerA := uuid.New()
	workerB := uuid.New()
	sender := &stubSender{result: SendResult{Outcome: SendAccepted}}
	wA := newTestWorker(t, sp, q, ring, sender, clock, nil, workerA)
	claimed, err := wA.claim(ctx, clock.Now())
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed = %d, want 1", len(claimed))
	}
	// Requeue the expired lease, then let B claim it.
	if _, err := sp.Exec(ctx, `UPDATE auth_email_jobs SET state='pending', attempts=attempts-1, next_attempt_at=$2, lease_owner=NULL, lease_expires_at=NULL WHERE id=$1`, jobID, clock.Now()); err != nil {
		t.Fatalf("requeue: %v", err)
	}
	if _, err := sp.Exec(ctx, `UPDATE auth_email_jobs SET state='leased', attempts=attempts+1, next_attempt_at=NULL, lease_owner=$2, lease_expires_at=$3 WHERE id=$1`, jobID, workerB.String(), clock.Now().Add(leaseDuration)); err != nil {
		t.Fatalf("lease to B: %v", err)
	}

	if err := wA.sendJob(ctx, claimed[0]); err != nil {
		t.Fatalf("sendJob: %v", err)
	}
	if sender.count() != 0 {
		t.Fatalf("sends = %d, want 0 (stale worker must not send)", sender.count())
	}
	state, leasedTo := jobState(ctx, t, sp, jobID)
	if state != "leased" {
		t.Fatalf("state = %q, want leased to B", state)
	}
	if leasedTo != workerB.String() {
		t.Fatalf("lease_owner = %q, want worker B", leasedTo)
	}
}

func TestWorkerRequeueExpiredRestoresPending(t *testing.T) {
	ctx, sp, q := newWorkerPool(t)
	clock := testutil.NewClockAtEpoch()
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	regID, digest := newWorkerRegistration(ctx, t, sp, clock.Now())
	tx := beginWorkerTx(t, ctx, sp)
	jobID := enqueueVerifyJob(ctx, t, q.WithTx(tx), ring, clock.Now, regID, digest)
	_ = tx.Commit(ctx)

	w := newTestWorker(t, sp, q, ring, &stubSender{result: SendResult{Outcome: SendAccepted}}, clock, nil, uuid.New())
	claimed, err := w.claim(ctx, clock.Now())
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed = %d, want 1", len(claimed))
	}
	// The claim's lease is 30s; advance past it, then requeue.
	clock.Advance(leaseDuration + time.Second)
	if err := w.requeueExpired(ctx, clock.Now()); err != nil {
		t.Fatalf("requeueExpired: %v", err)
	}
	state, leasedTo := jobState(ctx, t, sp, jobID)
	if state != "pending" {
		t.Fatalf("state = %q, want pending", state)
	}
	if leasedTo != "" {
		t.Fatalf("lease_owner = %q, want cleared", leasedTo)
	}
	var attempts int32
	var next *time.Time
	if err := sp.QueryRow(ctx, `SELECT attempts, next_attempt_at FROM auth_email_jobs WHERE id=$1`, jobID).Scan(&attempts, &next); err != nil {
		t.Fatalf("read: %v", err)
	}
	if attempts != 0 {
		t.Fatalf("attempts = %d, want 0 (roll back the claim increment)", attempts)
	}
	if next == nil || !near(*next, clock.Now()) {
		t.Fatalf("next_attempt_at = %v, want ~%v", next, clock.Now())
	}
}

func TestWorkerClaimStampsThirtySecondLease(t *testing.T) {
	ctx, sp, q := newWorkerPool(t)
	clock := testutil.NewClockAtEpoch()
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	regID, digest := newWorkerRegistration(ctx, t, sp, clock.Now())
	tx := beginWorkerTx(t, ctx, sp)
	jobID := enqueueVerifyJob(ctx, t, q.WithTx(tx), ring, clock.Now, regID, digest)
	_ = tx.Commit(ctx)

	w := newTestWorker(t, sp, q, ring, &stubSender{result: SendResult{Outcome: SendAccepted}}, clock, nil, uuid.New())
	claimed, err := w.claim(ctx, clock.Now())
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("claimed = %d, want 1", len(claimed))
	}
	var leaseExpiresAt time.Time
	if err := sp.QueryRow(ctx, `SELECT lease_expires_at FROM auth_email_jobs WHERE id=$1`, jobID).Scan(&leaseExpiresAt); err != nil {
		t.Fatalf("read: %v", err)
	}
	if !near(leaseExpiresAt, clock.Now().Add(leaseDuration)) {
		t.Fatalf("lease_expires_at = %v, want ~%v (30s lease)", leaseExpiresAt, clock.Now().Add(leaseDuration))
	}
}

func TestWorkerSendDeadlineSet(t *testing.T) {
	ctx, sp, q := newWorkerPool(t)
	clock := testutil.NewClockAtEpoch()
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	regID, digest := newWorkerRegistration(ctx, t, sp, clock.Now())
	tx := beginWorkerTx(t, ctx, sp)
	jobID := enqueueVerifyJob(ctx, t, q.WithTx(tx), ring, clock.Now, regID, digest)
	_ = tx.Commit(ctx)

	probe := &deadlineProbeSender{deadline: make(chan time.Time, 1)}
	w := newTestWorker(t, sp, q, ring, probe, clock, nil, uuid.New())
	if err := w.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	select {
	case dl := <-probe.deadline:
		if dl.IsZero() {
			t.Fatal("sender saw no deadline; want a 10s deadline")
		}
		remaining := time.Until(dl)
		if remaining > sendDeadline || remaining <= 0 {
			t.Fatalf("deadline %v is not within %v of now", dl, sendDeadline)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("sender never called")
	}
	state, _ := jobState(ctx, t, sp, jobID)
	if state != "sent" {
		t.Fatalf("state = %q, want sent", state)
	}
}

func TestWorkerTwoConcurrentSendsCap(t *testing.T) {
	ctx, sp, q := newWorkerPool(t)
	clock := testutil.NewClockAtEpoch()
	ring := testRing(t, 5)

	const n = 5
	tx := beginWorkerTx(t, ctx, sp)
	qtx := q.WithTx(tx)
	for i := 0; i < n; i++ {
		regID, digest := newWorkerRegistration(ctx, t, sp, clock.Now())
		enqueueVerifyJob(ctx, t, qtx, ring, clock.Now, regID, digest)
	}
	_ = tx.Commit(ctx)

	sender := &countingSender{result: SendResult{Outcome: SendAccepted}}
	w := newTestWorker(t, sp, q, ring, sender, clock, nil, uuid.New())
	if err := w.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	calls, maxActive := sender.resultCount()
	if calls != n {
		t.Fatalf("calls = %d, want %d", calls, n)
	}
	if maxActive > maxConcurrentSends {
		t.Fatalf("max concurrent sends = %d, want <= %d", maxActive, maxConcurrentSends)
	}
}

func TestWorkerConcurrentWorkersSendEachJobOnce(t *testing.T) {
	ctx, sp, q := newWorkerPool(t)
	clock := testutil.NewClockAtEpoch()
	ring := testRing(t, 4)

	const n = 4
	tx := beginWorkerTx(t, ctx, sp)
	qtx := q.WithTx(tx)
	for i := 0; i < n; i++ {
		regID, digest := newWorkerRegistration(ctx, t, sp, clock.Now())
		enqueueVerifyJob(ctx, t, qtx, ring, clock.Now, regID, digest)
	}
	_ = tx.Commit(ctx)

	shared := &countingSender{result: SendResult{Outcome: SendAccepted}}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		w := newTestWorker(t, sp, q, ring, shared, clock, nil, uuid.New())
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := w.RunOnce(ctx); err != nil {
				t.Errorf("RunOnce: %v", err)
			}
		}()
	}
	wg.Wait()
	calls, _ := shared.resultCount()
	if calls != n {
		t.Fatalf("total sends = %d, want %d (each job exactly once)", calls, n)
	}
}

func TestWorkerCleanupBounded(t *testing.T) {
	ctx, sp, q := newWorkerPool(t)
	clock := testutil.NewClockAtEpoch()

	// Create expired registrations that the tick must delete.
	regs := make([]uuid.UUID, 0, 5)
	for i := 0; i < 5; i++ {
		digest := uniqueDigest()
		reg, err := q.CreatePasswordRegistration(ctx, store.CreatePasswordRegistrationParams{
			Email:       uuid.NewString() + "@example.com",
			Name:        "Cleanup",
			EncodedHash: []byte("hash"),
			TokenDigest: digest[:],
			CreatedAt:   clock.Now().Add(-48 * time.Hour),
			ExpiresAt:   clock.Now().Add(-24 * time.Hour),
		})
		if err != nil {
			t.Fatalf("CreatePasswordRegistration: %v", err)
		}
		regs = append(regs, reg.ID)
	}

	sender := &stubSender{result: SendResult{Outcome: SendAccepted}}
	w := newTestWorker(t, sp, q, mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce()), sender, clock, nil, uuid.New())
	if err := w.RunOnce(ctx); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	for _, id := range regs {
		var n int
		if err := sp.QueryRow(ctx, `SELECT count(*) FROM password_registrations WHERE id = $1`, id).Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 0 {
			t.Errorf("expired registration %s not cleaned up", id)
		}
	}
}

func TestWorkerCancellationLeavesRecoverableLease(t *testing.T) {
	ctx, sp, q := newWorkerPool(t)
	clock := testutil.NewClockAtEpoch()
	ring := mustRing(t, "k-active", map[string][32]byte{"k-active": fixedKey()}, fixedNonce())
	regID, digest := newWorkerRegistration(ctx, t, sp, clock.Now())
	tx := beginWorkerTx(t, ctx, sp)
	jobID := enqueueVerifyJob(ctx, t, q.WithTx(tx), ring, clock.Now, regID, digest)
	_ = tx.Commit(ctx)

	hang := &hangSender{entered: make(chan struct{}), result: SendResult{Outcome: SendAccepted}}
	w := newTestWorker(t, sp, q, ring, hang, clock, nil, uuid.New())

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- w.Run(runCtx) }()

	select {
	case <-hang.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("sender never entered")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v, want nil on shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}

	// The cancelled send rolled back, so the job is still leased and therefore
	// recoverable by the requeue query, not sent or terminal.
	state, leasedTo := jobState(ctx, t, sp, jobID)
	if state != "leased" {
		t.Fatalf("state = %q, want leased (recoverable)", state)
	}
	if leasedTo == "" {
		t.Fatal("lease_owner is empty on a leased job")
	}
}

func TestWorkerClaimOrderAndBatch(t *testing.T) {
	ctx, sp, q := newWorkerPool(t)
	clock := testutil.NewClockAtEpoch()
	ring := testRing(t, 3)

	// Create three jobs with distinct next_attempt_at, then advance the clock so
	// all are due; claim must return them in (next_attempt_at, created_at, id).
	order := make([]uuid.UUID, 0, 3)
	for i := 0; i < 3; i++ {
		regID, digest := newWorkerRegistration(ctx, t, sp, clock.Now())
		tx := beginWorkerTx(t, ctx, sp)
		jobID := enqueueVerifyJob(ctx, t, q.WithTx(tx), ring, func() time.Time { return clock.Now().Add(time.Duration(i+1) * time.Minute) }, regID, digest)
		_ = tx.Commit(ctx)
		order = append(order, jobID)
	}
	clock.Advance(5 * time.Minute)

	sender := &stubSender{result: SendResult{Outcome: SendAccepted}}
	w := newTestWorker(t, sp, q, ring, sender, clock, nil, uuid.New())
	claimed, err := w.claim(ctx, clock.Now())
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 3 {
		t.Fatalf("claimed = %d, want 3", len(claimed))
	}
	for i, j := range claimed {
		if j.ID != order[i] {
			t.Errorf("claim[%d] = %s, want %s (next_attempt_at order)", i, j.ID, order[i])
		}
	}
}
