// Package renderjob runs bounded, capability-gated browser renders.
package renderjob

import (
	"context"
	"crypto/rand"
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
	// Card is the 1200 by 630 link-preview card, captured like PNG from a
	// card envelope (docs/design/link-previews.md, "Preview card").
	Card Format = "card"

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
	// CardMaxBytes bounds one completed preview card, under WhatsApp's
	// 600 KB image limit.
	CardMaxBytes = 524_288
	// LowPriorityMaxWaiting is how many queue places may be taken when a
	// low-priority render enters. The places above it stay free for owner
	// exports, and low-priority work alone can never saturate readiness.
	LowPriorityMaxWaiting = 2

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

// Priority orders admission between owner work and background work.
type Priority uint8

const (
	// PriorityNormal admits a render while any queue place is free.
	PriorityNormal Priority = iota
	// PriorityLow admits a background render only while at most
	// LowPriorityMaxWaiting places behind the running render are taken.
	PriorityLow
)

// Snapshot is the frozen, already-authorized renderer input.
type Snapshot struct {
	ResumeID         uuid.UUID
	Revision         int64
	SchemaVersion    int
	PublicGeneration int64
	Payload          []byte
	// RevisionTime is when the rendered revision was saved. A PDF carries it
	// as its creation and modification date, so the same revision always
	// renders the same bytes. The zero time writes the Unix epoch.
	RevisionTime time.Time
}

// Request supplies a frozen snapshot and optional public generation validator.
type Request struct {
	Format             Format
	Priority           Priority
	Prepare            func(context.Context) (Snapshot, error)
	ValidateGeneration func(context.Context, Snapshot) error
}

// Navigation is the renderer's complete authority for one controlled navigation.
type Navigation struct {
	ResumeID     uuid.UUID
	JobID        uuid.UUID
	Capability   string
	Format       Format
	RevisionTime time.Time
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
	CardLimit         int
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
	cardLimit     int
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
	cardLimit, err := boundedInt(config.CardLimit, CardMaxBytes)
	if err != nil {
		return nil, ErrInvalidRequest
	}

	queue := &Queue{
		renderer: config.Renderer, entropy: config.Entropy, newUUID: config.NewUUID,
		now: config.Now, afterFunc: config.AfterFunc,
		capacity: concurrent + queueDepth, jobTimeout: jobTimeout, capabilityTTL: capabilityTTL,
		snapshotLimit: snapshotLimit, pdfLimit: pdfLimit, pngLimit: pngLimit, cardLimit: cardLimit,
		renderPermit: make(chan struct{}, concurrent), attempts: make(map[*attempt]struct{}),
		jobs: make(map[uuid.UUID]*job), closeDone: make(chan struct{}),
	}
	queue.renderPermit <- struct{}{}
	return queue, nil
}
