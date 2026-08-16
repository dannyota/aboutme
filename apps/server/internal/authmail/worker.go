package authmail

import (
	"bytes"
	"context"
	"errors"
	"html"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// Worker scheduling bounds (D7).
const (
	pollInterval       = 1 * time.Second
	leaseDuration      = 30 * time.Second
	sendDeadline       = 10 * time.Second
	claimBatchSize     = 10
	maxConcurrentSends = 2
	maxAttempts        = 8
	maxBackoff         = 1 * time.Hour
	baseBackoff        = 30 * time.Second
	staleLeaseBatch    = 100
	cleanupBatch       = 200
	finishedJobRetain  = 7 * 24 * time.Hour
)

// WorkerOptions wires the worker to the store pool, the exact T01 transaction
// query surface, the T04 key ring, a Sender, and deterministic clock/jitter.
// Production has no alternate store path; tests inject only package-private
// begin/commit failure hooks and the same *store.Queries transaction binding.
type WorkerOptions struct {
	Pool     *store.Pool
	Queries  *store.Queries
	KeyRing  *KeyRing
	Sender   Sender
	Clock    func() time.Time
	Jitter   func(time.Duration) time.Duration
	Logger   *slog.Logger
	WorkerID uuid.UUID
}

// Worker polls once per second, claims at most ten jobs, and sends at most two
// at once. Every send is authorized and finalized inside one transaction that
// locks the scope row first and the leased job second, holds both locks through
// the at-most-ten-second sender call, and commits sent/terminal/requeue state.
type Worker struct {
	pool    poolBeginner
	queries *store.Queries
	ring    *KeyRing
	sender  Sender
	clock   func() time.Time
	jitter  func(time.Duration) time.Duration
	logger  *slog.Logger
	id      uuid.UUID

	// package-private hooks tests use as deterministic barriers; nil in
	// production.
	hooks workerHooks
}

// poolBeginner is the narrow transaction-opening surface Worker needs. Both
// *store.Pool and test fakes satisfy it.
type poolBeginner interface {
	Begin(context.Context) (pgx.Tx, error)
}

type workerHooks struct {
	afterClaim func(jobs []store.AuthEmailJob)
	beforeAuth func(jobID uuid.UUID)
	beforeSend func(jobID uuid.UUID)
	afterSend  func(jobID uuid.UUID, outcome SendOutcome)
}

// NewWorker validates every option and returns a Worker. A nil pool, queries,
// ring, sender, clock, jitter, logger, or nil worker ID is rejected.
func NewWorker(opts WorkerOptions) (*Worker, error) {
	if opts.Pool == nil || opts.Queries == nil || opts.KeyRing == nil || opts.Sender == nil {
		return nil, ErrWorker
	}
	if opts.Clock == nil || opts.Jitter == nil || opts.Logger == nil {
		return nil, ErrWorker
	}
	if opts.WorkerID == uuid.Nil {
		return nil, ErrWorker
	}
	return &Worker{
		pool:    opts.Pool,
		queries: opts.Queries,
		ring:    opts.KeyRing,
		sender:  opts.Sender,
		clock:   opts.Clock,
		jitter:  opts.Jitter,
		logger:  opts.Logger,
		id:      opts.WorkerID,
	}, nil
}

// Run polls once per second until ctx is canceled. A tick error is logged and
// the loop continues; cancellation returns nil promptly after in-flight sends
// join (each send has its own ten-second deadline).
func (w *Worker) Run(ctx context.Context) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		if err := w.RunOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			w.logger.Error("authmail: worker tick failed", "err", "tick error")
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// RunOnce runs one full tick: requeue expired leases, run the three bounded
// cleanups, claim at most ten jobs, and send each claimed job with at most two
// concurrent sends. It joins every send before returning, so a canceled ctx
// leaves in-flight jobs in a recoverable leased state rather than a partial
// finalize.
func (w *Worker) RunOnce(ctx context.Context) error {
	now := w.clock()

	if err := w.requeueExpired(ctx, now); err != nil {
		return err
	}
	if err := w.cleanup(ctx, now); err != nil {
		return err
	}

	jobs, err := w.claim(ctx, now)
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrentSends)
	for _, job := range jobs {
		job := job
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := w.sendJob(ctx, job); err != nil {
				w.logger.Warn("authmail: send job failed", "job", job.ID, "err", "send failed")
			}
		}()
	}
	wg.Wait()
	return nil //nolint:nilerr // a canceled ctx stops new sends but is not a tick error; Run observes ctx.Done() to stop.
}

// requeueExpired returns every lease whose lease_expires_at has passed back to
// pending in one bounded transaction, rolling back the claim's attempt
// increment (the send never happened).
func (w *Worker) requeueExpired(ctx context.Context, now time.Time) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			w.logger.Warn("authmail: rollback transaction failed")
		}
	}()
	qtx := w.queries.WithTx(tx)
	if _, err := qtx.RequeueExpiredAuthEmailLeases(ctx, store.RequeueExpiredAuthEmailLeasesParams{
		Now:       now,
		LimitRows: staleLeaseBatch,
	}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// cleanup runs the three bounded D3 cleanups, each in its own transaction:
// expired registrations, expired reset tokens, and sent/terminal jobs older
// than seven days. Every delete is capped at cleanupBatch (200).
func (w *Worker) cleanup(ctx context.Context, now time.Time) error {
	if err := w.deleteInTx(ctx, func(qtx *store.Queries) error {
		_, err := qtx.CleanupExpiredPasswordRegistrations(ctx, store.CleanupExpiredPasswordRegistrationsParams{
			Cutoff:    now,
			LimitRows: cleanupBatch,
		})
		return err
	}); err != nil {
		return err
	}
	if err := w.deleteInTx(ctx, func(qtx *store.Queries) error {
		_, err := qtx.CleanupExpiredPasswordResetTokens(ctx, store.CleanupExpiredPasswordResetTokensParams{
			Cutoff:    now,
			LimitRows: cleanupBatch,
		})
		return err
	}); err != nil {
		return err
	}
	return w.deleteInTx(ctx, func(qtx *store.Queries) error {
		_, err := qtx.CleanupFinishedAuthEmailJobs(ctx, store.CleanupFinishedAuthEmailJobsParams{
			Cutoff:    now.Add(-finishedJobRetain),
			LimitRows: cleanupBatch,
		})
		return err
	})
}

func (w *Worker) deleteInTx(ctx context.Context, fn func(*store.Queries) error) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			w.logger.Warn("authmail: rollback transaction failed")
		}
	}()
	if err := fn(w.queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// claim atomically transitions at most ten due pending jobs to leased in one
// transaction, stamping the lease owner and a thirty-second expiry, and returns
// them in (next_attempt_at, created_at, id) order.
func (w *Worker) claim(ctx context.Context, now time.Time) ([]store.AuthEmailJob, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			w.logger.Warn("authmail: rollback transaction failed")
		}
	}()
	jobs, err := w.queries.WithTx(tx).ClaimAuthEmailJobs(ctx, store.ClaimAuthEmailJobsParams{
		LeaseOwner:     w.id.String(),
		LeaseExpiresAt: now.Add(leaseDuration),
		Now:            now,
		LimitRows:      claimBatchSize,
	})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	if w.hooks.afterClaim != nil {
		w.hooks.afterClaim(jobs)
	}
	return jobs, nil
}

// sendJob authorizes and delivers one claimed job. It opens one transaction,
// locks the scoped registration/reset token/user first and the exact leased job
// second, decrypts and confirms current authority and lease ownership, then
// holds both locks through the at-most-ten-second sender call and finalizes
// sent/terminal/requeue state before commit. A stale or unauthorized job
// becomes terminal without any send. Cancellation rolls the transaction back
// and leaves the job leased for the requeue query to recover.
func (w *Worker) sendJob(ctx context.Context, job store.AuthEmailJob) error {
	if w.hooks.beforeAuth != nil {
		w.hooks.beforeAuth(job.ID)
	}

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(context.WithoutCancel(ctx)); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			w.logger.Warn("authmail: rollback transaction failed")
		}
	}()
	qtx := w.queries.WithTx(tx)
	now := w.clock()

	// Lock the scope row first (scope-before-job order, D7). A missing scope
	// means the owning registration/reset token/user was replaced or deleted;
	// the job may have been cascaded already, so terminate it if it still
	// exists and otherwise commit a no-op.
	scope, err := w.lockScope(ctx, qtx, job)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if _, merr := qtx.MarkAuthEmailJobTerminal(ctx, store.MarkAuthEmailJobTerminalParams{
				TerminalAt: now,
				ID:         job.ID,
			}); merr != nil {
				return merr
			}
			return tx.Commit(ctx)
		}
		return err
	}

	// Re-lock the exact leased job by this worker. ErrNoRows means the lease was
	// reassigned to another worker or the job is gone: do not send and do not
	// finalize.
	leased, err := qtx.GetLeasedAuthEmailJobForUpdate(ctx, store.GetLeasedAuthEmailJobForUpdateParams{
		ID:         job.ID,
		LeaseOwner: w.id.String(),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}

	// Current authority: the lease must still be live, the job must not be
	// expired, and the stored token digest must match the scope row. An expired
	// or stale job becomes terminal, and that terminal state is committed here:
	// the deferred rollback must not undo it.
	if !leased.LeaseExpiresAt.After(now) {
		return nil
	}
	if !leased.ExpiresAt.After(now) {
		if terminalErr := w.markTerminal(ctx, qtx, leased.ID, now); terminalErr != nil {
			return terminalErr
		}
		return tx.Commit(ctx)
	}
	if scope != nil && !scope.tokenMatches(leased.TokenDigest) {
		if terminalErr := w.markTerminal(ctx, qtx, leased.ID, now); terminalErr != nil {
			return terminalErr
		}
		return tx.Commit(ctx)
	}

	// Decrypt the sealed payload. The AAD binds job ID, kind, and key ID, so a
	// swapped scope/token/ciphertext/key ID fails authentication here and the
	// job is terminated without a send. Plaintext never reaches a log.
	payload, err := w.ring.Open(leased.ID, Kind(leased.Kind), sealedFromJob(leased))
	if err != nil {
		w.logger.Warn("authmail: decrypt failed", "job", leased.ID)
		return w.markTerminal(ctx, qtx, leased.ID, now)
	}

	// Send under a ten-second deadline while both locks are held.
	if w.hooks.beforeSend != nil {
		w.hooks.beforeSend(leased.ID)
	}
	sendCtx, cancel := context.WithTimeout(ctx, sendDeadline)
	result, sendErr := w.sender.Send(sendCtx, buildMessage(Kind(leased.Kind), payload))
	cancel()
	if w.hooks.afterSend != nil {
		w.hooks.afterSend(leased.ID, result.Outcome)
	}
	if sendErr != nil {
		// An unexpected sender error is ambiguous and therefore temporary; a
		// duplicate delivery is harmless under token single use.
		result.Outcome = SendTemporaryFailure
	}

	// Finalize within the same transaction.
	switch result.Outcome {
	case SendAccepted:
		_, err = qtx.MarkAuthEmailJobSent(ctx, store.MarkAuthEmailJobSentParams{SentAt: now, ID: leased.ID})
	case SendPermanentFailure:
		err = w.markTerminal(ctx, qtx, leased.ID, now)
	case SendTemporaryFailure:
		err = w.requeueOrTerminal(ctx, qtx, leased, now)
	default:
		err = ErrWorker
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// markTerminal records the exact terminal state in the current transaction.
func (w *Worker) markTerminal(ctx context.Context, qtx *store.Queries, jobID uuid.UUID, now time.Time) error {
	_, err := qtx.MarkAuthEmailJobTerminal(ctx, store.MarkAuthEmailJobTerminalParams{TerminalAt: now, ID: jobID})
	return err
}

// requeueOrTerminal applies the D7 temporary-failure policy: failed attempt 8
// or a next attempt at/after expiry marks terminal; otherwise the job returns
// to pending with a full-jitter backoff capped at min(30s*2^(attempt-1), 1h).
func (w *Worker) requeueOrTerminal(ctx context.Context, qtx *store.Queries, job store.AuthEmailJob, now time.Time) error {
	if job.Attempts >= maxAttempts {
		return w.markTerminal(ctx, qtx, job.ID, now)
	}
	next := now.Add(w.jitter(backoffCap(job.Attempts)))
	if !next.Before(job.ExpiresAt) {
		return w.markTerminal(ctx, qtx, job.ID, now)
	}
	_, err := qtx.RequeueAuthEmailJob(ctx, store.RequeueAuthEmailJobParams{NextAttemptAt: next, ID: job.ID})
	return err
}

// lockScope locks the single scope row named by the job's kind in the current
// transaction and returns the authority state (the scope row's token digest for
// verify/reset, nil for password_changed). A missing row surfaces as
// pgx.ErrNoRows.
func (w *Worker) lockScope(ctx context.Context, qtx *store.Queries, job store.AuthEmailJob) (*scopeState, error) {
	switch Kind(job.Kind) {
	case KindVerify:
		if job.RegistrationID == nil {
			return nil, ErrScope
		}
		reg, err := qtx.GetPasswordRegistrationForUpdate(ctx, *job.RegistrationID)
		if err != nil {
			return nil, err
		}
		return &scopeState{tokenDigest: reg.TokenDigest}, nil
	case KindReset:
		if job.ResetTokenID == nil {
			return nil, ErrScope
		}
		tok, err := qtx.GetPasswordResetTokenForUpdate(ctx, *job.ResetTokenID)
		if err != nil {
			return nil, err
		}
		return &scopeState{tokenDigest: tok.TokenDigest}, nil
	case KindPasswordChanged:
		if job.UserID == nil {
			return nil, ErrScope
		}
		if _, err := qtx.GetUserForUpdate(ctx, *job.UserID); err != nil {
			return nil, err
		}
		return &scopeState{}, nil
	default:
		return nil, ErrInvalidKind
	}
}

// scopeState is the authority the send handoff checks against the job.
type scopeState struct {
	tokenDigest []byte
}

// tokenMatches reports whether the job's stored digest is still the scope
// row's current digest. password_changed jobs have no token authority, so any
// digest (including nil) matches.
func (s *scopeState) tokenMatches(jobDigest []byte) bool {
	if s.tokenDigest == nil {
		return true
	}
	return bytes.Equal(s.tokenDigest, jobDigest)
}

// sealedFromJob converts the stored job columns into the T04 Sealed blob.
func sealedFromJob(job store.AuthEmailJob) Sealed {
	var nonce [12]byte
	copy(nonce[:], job.Nonce)
	var keyID string
	if job.KeyID != nil {
		keyID = *job.KeyID
	}
	return Sealed{KeyID: keyID, Nonce: nonce, Ciphertext: job.Ciphertext}
}

// backoffCap returns min(30s * 2^(attempt-1), 1h) for the attempt that just
// failed. Attempt 1 has a 30s cap; each retry doubles until the one-hour cap.
func backoffCap(attempt int32) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	cap := baseBackoff
	for i := int32(1); i < attempt; i++ {
		cap *= 2
		if cap >= maxBackoff {
			cap = maxBackoff
			break
		}
	}
	return cap
}

// buildMessage renders the fixed D7 message templates for a decrypted payload.
// Subjects and copy are fixed code; only the canonical-origin link is
// interpolated, HTML-escaped, and there is no tracking pixel, external
// resource, reply token, or user-controlled subject.
func buildMessage(kind Kind, p Payload) Message {
	switch kind {
	case KindVerify:
		return Message{
			Kind:     kind,
			To:       p.To,
			Subject:  "Verify your email",
			TextBody: "Confirm your email address by opening this link:\n" + p.Link,
			HTMLBody: "<p>Confirm your email address by opening this link:</p><p><a href=\"" + html.EscapeString(p.Link) + "\">" + html.EscapeString(p.Link) + "</a></p>",
		}
	case KindReset:
		return Message{
			Kind:     kind,
			To:       p.To,
			Subject:  "Reset your password",
			TextBody: "Reset your password by opening this link:\n" + p.Link,
			HTMLBody: "<p>Reset your password by opening this link:</p><p><a href=\"" + html.EscapeString(p.Link) + "\">" + html.EscapeString(p.Link) + "</a></p>",
		}
	case KindPasswordChanged:
		return Message{
			Kind:     kind,
			To:       p.To,
			Subject:  "Your password was changed",
			TextBody: "Your password was changed. If you did not make this change, contact support immediately.",
			HTMLBody: "<p>Your password was changed. If you did not make this change, contact support immediately.</p>",
		}
	default:
		return Message{Kind: kind, To: p.To}
	}
}

// compile-time assertion that a *store.Pool opens transactions as expected.
var _ poolBeginner = (*store.Pool)(nil)
