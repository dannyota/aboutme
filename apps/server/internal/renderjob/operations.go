package renderjob

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
)

// Render prepares, queues, renders, and completes one attempt.
func (q *Queue) Render(ctx context.Context, request Request) (Result, error) {
	if contextErr := ctx.Err(); contextErr != nil {
		return Result{}, contextErr
	}
	if !validFormat(request.Format) || request.Prepare == nil {
		return Result{}, ErrInvalidRequest
	}
	attempt, err := q.admit(ctx)
	if err != nil {
		return Result{}, err
	}
	defer q.releaseAttempt(attempt)

	// attempt.ctx is the context.WithCancel(ctx) child created by q.admit.
	snapshot, prepareErr := callPrepare(attempt.ctx, request.Prepare) //nolint:contextcheck
	if prepareErr != nil {
		if attemptErr := q.attemptError(attempt); attemptErr != nil {
			return Result{}, attemptErr
		}
		return Result{}, ErrPreparation
	}
	if !validSnapshot(snapshot, q.snapshotLimit) || (snapshot.PublicGeneration != 0 && request.ValidateGeneration == nil) {
		return Result{}, ErrInvalidRequest
	}
	snapshot.Payload = append([]byte(nil), snapshot.Payload...)
	if attemptErr := q.attemptError(attempt); attemptErr != nil {
		return Result{}, attemptErr
	}

	jobID, err := q.newJobID()
	if err != nil {
		return Result{}, err
	}
	capability, capabilityHash, err := q.newAuthority()
	if err != nil {
		return Result{}, err
	}
	controller, controllerHash, err := q.newAuthority()
	if err != nil {
		return Result{}, err
	}
	created, err := q.installJob(attempt, request, snapshot, jobID, capabilityHash, controllerHash)
	if err != nil {
		return Result{}, err
	}

	select {
	case <-attempt.ctx.Done():
		return Result{}, q.terminalError(attempt)
	case <-q.renderPermit:
	}
	defer func() { q.renderPermit <- struct{}{} }()
	if err := q.attemptError(attempt); err != nil {
		return Result{}, err
	}

	// attempt.ctx is the context.WithCancel(ctx) child created by q.admit.
	output, renderErr := callRenderer(attempt.ctx, q.renderer, Navigation{ //nolint:contextcheck
		ResumeID: snapshot.ResumeID, JobID: jobID, Capability: capability, Format: request.Format,
	})
	if renderErr != nil {
		if attemptErr := q.attemptError(attempt); attemptErr != nil {
			return Result{}, attemptErr
		}
		q.discardJob(created)
		return Result{}, ErrRendering
	}
	if err := q.attemptError(attempt); err != nil {
		return Result{}, err
	}
	// attempt.ctx is the context.WithCancel(ctx) child created by q.admit.
	return q.complete(attempt.ctx, jobID, controller, output) //nolint:contextcheck
}

// Redeem atomically consumes a matching one-use capability.
func (q *Queue) Redeem(ctx context.Context, redemption Redemption) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, ErrNotActive
	}
	raw, ok := decodeAuthority(redemption.Capability)
	if !ok {
		return Snapshot{}, ErrNotActive
	}
	presentedHash := sha256.Sum256(raw)

	q.mu.Lock()
	stored := q.jobs[redemption.JobID]
	if stored == nil || stored.redeemed || stored.completing || redemption.ResumeID != stored.snapshot.ResumeID ||
		redemption.Audience != audiencePrint || !q.now().Before(stored.expiresAt) ||
		subtle.ConstantTimeCompare(presentedHash[:], stored.capabilityHash[:]) != 1 ||
		stored.snapshotDigest != sha256.Sum256(stored.snapshot.Payload) ||
		stored.bindingDigest != bindingDigest(stored) {
		q.mu.Unlock()
		return Snapshot{}, ErrNotActive
	}
	if attemptErr := q.attemptErrorLocked(stored.attempt); attemptErr != nil {
		q.cancelAttemptLocked(stored.attempt, attemptErr)
		q.mu.Unlock()
		return Snapshot{}, ErrNotActive
	}
	stored.redeemed = true
	stored.capabilityHash = [32]byte{}
	stored.bindingDigest = bindingDigest(stored)
	expiry := stored.capabilityExpiry
	stored.capabilityExpiry = nil
	snapshot := cloneSnapshot(stored.snapshot)
	q.mu.Unlock()
	if expiry != nil {
		expiry.stopOnly()
	}
	return snapshot, nil
}

// Ready reports permanent shutdown or temporary admission saturation.
func (q *Queue) Ready() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return ErrClosed
	}
	if len(q.attempts) >= q.capacity {
		return ErrSaturated
	}
	return nil
}

// Close permanently stops admission, cancels all attempts, and joins callbacks.
func (q *Queue) Close() error {
	q.closeOnce.Do(func() {
		q.mu.Lock()
		q.closed = true
		attempts := make([]*attempt, 0, len(q.attempts))
		for active := range q.attempts {
			q.cancelAttemptLocked(active, ErrClosed)
			attempts = append(attempts, active)
		}
		q.mu.Unlock()
		for _, active := range attempts {
			<-active.done
		}
		close(q.closeDone)
	})
	<-q.closeDone
	return nil
}
