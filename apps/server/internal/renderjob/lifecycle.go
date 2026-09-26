package renderjob

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"time"

	"github.com/google/uuid"
)

func (q *Queue) admit(parent context.Context, priority Priority) (*attempt, error) {
	ctx, cancel := context.WithCancel(parent)
	active := &attempt{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		cancel()
		return nil, ErrClosed
	}
	if len(q.attempts) >= q.capacity ||
		(priority == PriorityLow && len(q.attempts) > MaxConcurrentRenders+LowPriorityMaxWaiting) {
		q.mu.Unlock()
		cancel()
		return nil, ErrSaturated
	}
	q.attempts[active] = struct{}{}
	q.trackParentCancellationLocked(parent, active)
	q.trackTimerLocked(active, q.jobTimeout, func() { q.cancelAttempt(active, context.DeadlineExceeded) })
	q.mu.Unlock()
	return active, nil
}

func (q *Queue) installJob(active *attempt, request Request, snapshot Snapshot, jobID uuid.UUID,
	capabilityHash, controllerHash [32]byte,
) (*job, error) {
	created := &job{
		attempt: active, format: request.Format, snapshot: snapshot,
		snapshotDigest: sha256.Sum256(snapshot.Payload), capabilityHash: capabilityHash,
		controllerHash: controllerHash, expiresAt: q.now().Add(q.capabilityTTL),
		validateGeneration: request.ValidateGeneration,
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, live := q.attempts[active]; !live || active.reason != nil || active.ctx.Err() != nil {
		return nil, q.attemptErrorLocked(active)
	}
	if _, exists := q.jobs[jobID]; exists {
		return nil, ErrAuthoritySource
	}
	active.jobID = jobID
	created.bindingDigest = bindingDigest(created)
	q.jobs[jobID] = created
	created.capabilityExpiry = q.trackTimerLocked(active, q.capabilityTTL, func() { q.expireCapability(created) })
	return created, nil
}

func (q *Queue) complete(ctx context.Context, jobID uuid.UUID, controller string, output []byte) (Result, error) {
	raw, ok := decodeAuthority(controller)
	if !ok {
		return Result{}, ErrNotActive
	}
	presentedHash := sha256.Sum256(raw)

	q.mu.Lock()
	stored := q.jobs[jobID]
	if stored == nil || !stored.redeemed || stored.completing ||
		subtle.ConstantTimeCompare(presentedHash[:], stored.controllerHash[:]) != 1 ||
		stored.snapshotDigest != sha256.Sum256(stored.snapshot.Payload) || stored.bindingDigest != bindingDigest(stored) {
		q.mu.Unlock()
		return Result{}, ErrNotActive
	}
	if cancelErr := q.completionCancellationLocked(ctx, stored); cancelErr != nil {
		q.mu.Unlock()
		return Result{}, cancelErr
	}
	stored.completing = true
	limit := q.outputLimit(stored.format)
	if len(output) == 0 || len(output) > limit {
		q.removeJobLocked(stored)
		q.mu.Unlock()
		return Result{}, ErrOutputTooLarge
	}
	validator := stored.validateGeneration
	snapshot := cloneSnapshot(stored.snapshot)
	q.mu.Unlock()

	if snapshot.PublicGeneration != 0 {
		validationErr := callValidate(ctx, validator, snapshot)
		q.mu.Lock()
		if q.jobs[jobID] != stored {
			err := q.attemptErrorLocked(stored.attempt)
			q.mu.Unlock()
			if err != nil {
				return Result{}, err
			}
			return Result{}, ErrNotActive
		}
		if cancelErr := q.completionCancellationLocked(ctx, stored); cancelErr != nil {
			q.mu.Unlock()
			return Result{}, cancelErr
		}
		if validationErr != nil {
			q.removeJobLocked(stored)
			q.mu.Unlock()
			return Result{}, ErrGenerationChanged
		}
		q.mu.Unlock()
	}

	artifact := append([]byte(nil), output...)
	result := Result{Bytes: artifact, Digest: sha256.Sum256(artifact), Revision: snapshot.Revision}
	q.mu.Lock()
	if q.jobs[jobID] != stored {
		err := q.attemptErrorLocked(stored.attempt)
		q.mu.Unlock()
		if err != nil {
			return Result{}, err
		}
		return Result{}, ErrNotActive
	}
	if cancelErr := q.completionCancellationLocked(ctx, stored); cancelErr != nil {
		q.mu.Unlock()
		return Result{}, cancelErr
	}
	q.removeJobLocked(stored)
	q.mu.Unlock()
	return result, nil
}

func (q *Queue) completionCancellationLocked(ctx context.Context, stored *job) error {
	if err := q.attemptErrorLocked(stored.attempt); err != nil {
		q.cancelAttemptLocked(stored.attempt, err)
		return err
	}
	if err := ctx.Err(); err != nil {
		q.cancelAttemptLocked(stored.attempt, err)
		return err
	}
	return nil
}

func (q *Queue) expireCapability(stored *job) {
	q.mu.Lock()
	if stored.attempt.jobID != uuid.Nil && q.jobs[stored.attempt.jobID] == stored && !stored.redeemed {
		q.cancelAttemptLocked(stored.attempt, ErrNotActive)
	}
	q.mu.Unlock()
}

func (q *Queue) cancelAttempt(active *attempt, reason error) {
	q.mu.Lock()
	q.cancelAttemptLocked(active, reason)
	q.mu.Unlock()
}

func (q *Queue) cancelAttemptLocked(active *attempt, reason error) {
	if _, live := q.attempts[active]; !live {
		return
	}
	if active.reason == nil {
		active.reason = reason
	}
	if stored := q.jobs[active.jobID]; stored != nil {
		q.removeJobLocked(stored)
	}
	active.cancel()
}

func (q *Queue) discardJob(stored *job) {
	q.mu.Lock()
	if q.jobs[stored.attempt.jobID] == stored {
		q.removeJobLocked(stored)
	}
	q.mu.Unlock()
}

func (q *Queue) removeJobLocked(stored *job) {
	delete(q.jobs, stored.attempt.jobID)
	stored.capabilityHash = [32]byte{}
	stored.controllerHash = [32]byte{}
	stored.capabilityExpiry = nil
}

func (q *Queue) releaseAttempt(active *attempt) {
	q.mu.Lock()
	if stored := q.jobs[active.jobID]; stored != nil {
		q.removeJobLocked(stored)
	}
	callbacks := append([]*trackedCallback(nil), active.callbacks...)
	delete(q.attempts, active)
	active.cancel()
	q.mu.Unlock()
	for _, callback := range callbacks {
		callback.stopAndWait()
	}
	close(active.done)
}

func (q *Queue) trackParentCancellationLocked(parent context.Context, active *attempt) {
	callback := newTrackedCallback()
	callback.stop = context.AfterFunc(parent, func() {
		defer callback.finish()
		q.cancelAttempt(active, parent.Err())
	})
	active.callbacks = append(active.callbacks, callback)
}

func (q *Queue) trackTimerLocked(active *attempt, delay time.Duration, fn func()) *trackedCallback {
	callback := newTrackedCallback()
	timer := q.afterFunc(delay, func() {
		defer callback.finish()
		fn()
	})
	callback.stop = timer.Stop
	active.callbacks = append(active.callbacks, callback)
	return callback
}

func (q *Queue) attemptError(active *attempt) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.attemptErrorLocked(active)
}

func (q *Queue) attemptErrorLocked(active *attempt) error {
	if active.reason != nil {
		return active.reason
	}
	return active.ctx.Err()
}

func (q *Queue) terminalError(active *attempt) error {
	if err := q.attemptError(active); err != nil {
		return err
	}
	return ErrNotActive
}
