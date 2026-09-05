// Package renderjob runs bounded, capability-gated browser renders.
package renderjob

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	// PDF is the document export format.
	PDF Format = "pdf"
	// PNG is the fixed 1200 by 630 share-image format.
	PNG Format = "png"

	// MaxConcurrentRenders is the fixed v1 browser concurrency limit.
	MaxConcurrentRenders = 1
	// MaxQueueDepth is the fixed v1 waiting-job limit.
	MaxQueueDepth = 8
	// MaxAdmittedJobs includes the running render and waiting queue.
	MaxAdmittedJobs = MaxConcurrentRenders + MaxQueueDepth
	// MaxJobTimeout sets the cancellation deadline from admission; joined cleanup may finish later.
	MaxJobTimeout = 20 * time.Second
	// MaxCapabilityTTL bounds an unused print capability.
	MaxCapabilityTTL = 60 * time.Second
	// MaxSnapshotBytes bounds one frozen render snapshot.
	MaxSnapshotBytes = 3_407_872
	// PDFMaxBytes bounds one completed PDF artifact.
	PDFMaxBytes = 16_777_216
	// PNGMaxBytes bounds one completed share-image artifact.
	PNGMaxBytes = 4_194_304

	capabilityBytes = 32
	audiencePrint   = "nuxt-print"
)

var (
	// ErrSaturated reports that all render admission slots are reserved.
	ErrSaturated = errors.New("renderjob: saturated")
	// ErrNotActive is the uniform rejection for invalid or inactive authority.
	ErrNotActive = errors.New("renderjob: not active")
	// ErrClosed reports that the queue has begun permanent shutdown.
	ErrClosed = errors.New("renderjob: closed")
	// ErrInvalidRequest reports invalid format, snapshot, or configuration input.
	ErrInvalidRequest = errors.New("renderjob: invalid request")
	// ErrPreparation reports a sanitized snapshot preparation failure.
	ErrPreparation = errors.New("renderjob: preparation failed")
	// ErrRendering reports a sanitized renderer failure.
	ErrRendering = errors.New("renderjob: render failed")
	// ErrGenerationChanged reports a public result discarded after validation.
	ErrGenerationChanged = errors.New("renderjob: public generation changed")
	// ErrOutputTooLarge reports a terminal artifact size violation.
	ErrOutputTooLarge = errors.New("renderjob: output too large")
	// ErrAuthoritySource reports a sanitized entropy or UUID source failure.
	ErrAuthoritySource = errors.New("renderjob: authority source failed")
)

// Format identifies one fixed render output contract.
type Format string

// Snapshot is the frozen, already-authorized renderer input.
type Snapshot struct {
	ResumeID         uuid.UUID
	Revision         int64
	SchemaVersion    int
	PublicGeneration int64
	Payload          []byte
}

// Request supplies a frozen snapshot and optional public generation validator.
type Request struct {
	Format             Format
	Prepare            func(context.Context) (Snapshot, error)
	ValidateGeneration func(context.Context, Snapshot) error
}

// Navigation is the renderer's complete authority for one controlled navigation.
type Navigation struct {
	ResumeID   uuid.UUID
	JobID      uuid.UUID
	Capability string
	Format     Format
}

// Renderer performs one controlled browser navigation and capture.
type Renderer interface {
	Render(context.Context, Navigation) ([]byte, error)
}

// Redemption is the private print route's presented authority.
type Redemption struct {
	ResumeID   uuid.UUID
	JobID      uuid.UUID
	Audience   string
	Capability string
}

// Result is one accepted artifact and its server-computed digest.
type Result struct {
	Bytes    []byte
	Digest   [32]byte
	Revision int64
}

// Timer is the stoppable part of a real or deterministic callback timer.
type Timer interface {
	Stop() bool
}

// Config supplies dependencies and optionally lowers fixed production bounds.
type Config struct {
	Renderer  Renderer
	Entropy   io.Reader
	NewUUID   func() (uuid.UUID, error)
	Now       func() time.Time
	AfterFunc func(time.Duration, func()) Timer

	ConcurrentRenders int
	QueueDepth        int
	JobTimeout        time.Duration
	CapabilityTTL     time.Duration
	SnapshotLimit     int
	PDFLimit          int
	PNGLimit          int
}

type attempt struct {
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	reason    error
	callbacks []*trackedCallback
	jobID     uuid.UUID
}

type job struct {
	attempt            *attempt
	format             Format
	snapshot           Snapshot
	snapshotDigest     [32]byte
	bindingDigest      [32]byte
	capabilityHash     [32]byte
	controllerHash     [32]byte
	expiresAt          time.Time
	redeemed           bool
	completing         bool
	capabilityExpiry   *trackedCallback
	validateGeneration func(context.Context, Snapshot) error
}

type trackedCallback struct {
	stop     func() bool
	stopOnce sync.Once
	doneOnce sync.Once
	done     chan struct{}
}

func newTrackedCallback() *trackedCallback {
	return &trackedCallback{done: make(chan struct{})}
}

func (callback *trackedCallback) finish() {
	callback.doneOnce.Do(func() { close(callback.done) })
}

func (callback *trackedCallback) stopOnly() {
	callback.stopOnce.Do(func() {
		if callback.stop() {
			callback.finish()
		}
	})
}

func (callback *trackedCallback) stopAndWait() {
	callback.stopOnly()
	<-callback.done
}

// Queue owns bounded admission and ephemeral authority state.
type Queue struct {
	mu       sync.Mutex
	sourceMu sync.Mutex

	renderer      Renderer
	entropy       io.Reader
	newUUID       func() (uuid.UUID, error)
	now           func() time.Time
	afterFunc     func(time.Duration, func()) Timer
	capacity      int
	jobTimeout    time.Duration
	capabilityTTL time.Duration
	snapshotLimit int
	pdfLimit      int
	pngLimit      int
	renderPermit  chan struct{}
	attempts      map[*attempt]struct{}
	jobs          map[uuid.UUID]*job
	closed        bool
	closeOnce     sync.Once
	closeDone     chan struct{}
}

// New constructs a bounded render queue.
func New(config Config) (*Queue, error) {
	if config.Renderer == nil {
		return nil, ErrInvalidRequest
	}
	if config.Entropy == nil {
		config.Entropy = rand.Reader
	}
	if config.NewUUID == nil {
		config.NewUUID = func() (uuid.UUID, error) {
			return uuid.NewRandomFromReader(config.Entropy)
		}
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.AfterFunc == nil {
		config.AfterFunc = func(delay time.Duration, fn func()) Timer { return time.AfterFunc(delay, fn) }
	}

	concurrent, err := boundedInt(config.ConcurrentRenders, MaxConcurrentRenders)
	if err != nil || concurrent != MaxConcurrentRenders {
		return nil, ErrInvalidRequest
	}
	queueDepth := config.QueueDepth
	if queueDepth == 0 {
		queueDepth = MaxQueueDepth
	}
	if queueDepth < 1 || queueDepth > MaxQueueDepth {
		return nil, ErrInvalidRequest
	}
	jobTimeout, err := boundedDuration(config.JobTimeout, MaxJobTimeout)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	capabilityTTL, err := boundedDuration(config.CapabilityTTL, MaxCapabilityTTL)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	snapshotLimit, err := boundedInt(config.SnapshotLimit, MaxSnapshotBytes)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	pdfLimit, pngLimit := config.PDFLimit, config.PNGLimit
	pdfLimit, err = boundedInt(pdfLimit, PDFMaxBytes)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	pngLimit, err = boundedInt(pngLimit, PNGMaxBytes)
	if err != nil {
		return nil, ErrInvalidRequest
	}

	queue := &Queue{
		renderer: config.Renderer, entropy: config.Entropy, newUUID: config.NewUUID,
		now: config.Now, afterFunc: config.AfterFunc,
		capacity: concurrent + queueDepth, jobTimeout: jobTimeout, capabilityTTL: capabilityTTL,
		snapshotLimit: snapshotLimit, pdfLimit: pdfLimit, pngLimit: pngLimit,
		renderPermit: make(chan struct{}, concurrent), attempts: make(map[*attempt]struct{}),
		jobs: make(map[uuid.UUID]*job), closeDone: make(chan struct{}),
	}
	queue.renderPermit <- struct{}{}
	return queue, nil
}

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

func (q *Queue) admit(parent context.Context) (*attempt, error) {
	ctx, cancel := context.WithCancel(parent)
	active := &attempt{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		cancel()
		return nil, ErrClosed
	}
	if len(q.attempts) >= q.capacity {
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

func (q *Queue) newJobID() (id uuid.UUID, err error) {
	q.sourceMu.Lock()
	defer q.sourceMu.Unlock()
	defer func() {
		if recover() != nil {
			id, err = uuid.Nil, ErrAuthoritySource
		}
	}()
	id, err = q.newUUID()
	if err != nil || id == uuid.Nil {
		return uuid.Nil, ErrAuthoritySource
	}
	return id, nil
}

func (q *Queue) newAuthority() (token string, hash [32]byte, err error) {
	q.sourceMu.Lock()
	defer q.sourceMu.Unlock()
	defer func() {
		if recover() != nil {
			token, hash, err = "", [32]byte{}, ErrAuthoritySource
		}
	}()
	raw := make([]byte, capabilityBytes)
	if _, err := io.ReadFull(q.entropy, raw); err != nil {
		return "", [32]byte{}, ErrAuthoritySource
	}
	return base64.RawURLEncoding.EncodeToString(raw), sha256.Sum256(raw), nil
}

func (q *Queue) outputLimit(format Format) int {
	if format == PNG {
		return q.pngLimit
	}
	return q.pdfLimit
}

func validFormat(format Format) bool { return format == PDF || format == PNG }

func validSnapshot(snapshot Snapshot, limit int) bool {
	return snapshot.ResumeID != uuid.Nil && snapshot.Revision > 0 && snapshot.SchemaVersion > 0 &&
		snapshot.PublicGeneration >= 0 &&
		(snapshot.PublicGeneration == 0 || snapshot.PublicGeneration == snapshot.Revision) &&
		len(snapshot.Payload) > 0 && len(snapshot.Payload) <= limit
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.Payload = append([]byte(nil), snapshot.Payload...)
	return snapshot
}

func decodeAuthority(token string) ([]byte, bool) {
	if len(token) != base64.RawURLEncoding.EncodedLen(capabilityBytes) {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != capabilityBytes || base64.RawURLEncoding.EncodeToString(raw) != token {
		return nil, false
	}
	return raw, true
}

func bindingDigest(stored *job) [32]byte {
	hash := sha256.New()
	writeDigest(hash, stored.snapshot.ResumeID[:])
	writeDigest(hash, stored.attempt.jobID[:])
	writeInt64(hash, stored.snapshot.Revision)
	writeInt64(hash, int64(stored.snapshot.SchemaVersion))
	writeInt64(hash, stored.snapshot.PublicGeneration)
	writeDigest(hash, []byte(stored.format))
	writeDigest(hash, []byte(audiencePrint))
	writeInt64(hash, stored.expiresAt.UnixNano())
	writeDigest(hash, stored.snapshotDigest[:])
	writeDigest(hash, stored.capabilityHash[:])
	writeDigest(hash, stored.controllerHash[:])
	var result [32]byte
	copy(result[:], hash.Sum(nil))
	return result
}

func writeInt64(writer io.Writer, value int64) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic("renderjob: digest encoding failed")
	}
}

func writeDigest(writer io.Writer, value []byte) {
	if _, err := writer.Write(value); err != nil {
		panic("renderjob: digest write failed")
	}
}

func boundedInt(value, maximum int) (int, error) {
	if value == 0 {
		return maximum, nil
	}
	if value < 1 || value > maximum {
		return 0, ErrInvalidRequest
	}
	return value, nil
}

func boundedDuration(value, maximum time.Duration) (time.Duration, error) {
	if value == 0 {
		return maximum, nil
	}
	if value < time.Nanosecond || value > maximum {
		return 0, ErrInvalidRequest
	}
	return value, nil
}

func callPrepare(ctx context.Context, prepare func(context.Context) (Snapshot, error)) (snapshot Snapshot, err error) {
	defer func() {
		if recover() != nil {
			snapshot, err = Snapshot{}, ErrPreparation
		}
	}()
	return prepare(ctx)
}

func callRenderer(ctx context.Context, renderer Renderer, navigation Navigation) (output []byte, err error) {
	defer func() {
		if recover() != nil {
			output, err = nil, ErrRendering
		}
	}()
	return renderer.Render(ctx, navigation)
}

func callValidate(ctx context.Context, validate func(context.Context, Snapshot) (err error), snapshot Snapshot) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrGenerationChanged
		}
	}()
	return validate(ctx, snapshot)
}
