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
	PDF  Format = "pdf"
	PNG  Format = "png"
	Card Format = "card"

	MaxConcurrentRenders  = 1
	MaxQueueDepth         = 8
	MaxAdmittedJobs       = MaxConcurrentRenders + MaxQueueDepth
	MaxJobTimeout         = 20 * time.Second
	MaxCapabilityTTL      = 60 * time.Second
	MaxSnapshotBytes      = 3_407_872
	PDFMaxBytes           = 16_777_216
	PNGMaxBytes           = 4_194_304
	CardMaxBytes          = 524_288
	LowPriorityMaxWaiting = 2

	capabilityBytes = 32
	audiencePrint   = "nuxt-print"
)

var (
	ErrSaturated         = errors.New("renderjob: saturated")
	ErrNotActive         = errors.New("renderjob: not active")
	ErrClosed            = errors.New("renderjob: closed")
	ErrInvalidRequest    = errors.New("renderjob: invalid request")
	ErrPreparation       = errors.New("renderjob: preparation failed")
	ErrRendering         = errors.New("renderjob: render failed")
	ErrGenerationChanged = errors.New("renderjob: public generation changed")
	ErrOutputTooLarge    = errors.New("renderjob: output too large")
	ErrAuthoritySource   = errors.New("renderjob: authority source failed")
)

type Format string

type Priority uint8

const (
	PriorityNormal Priority = iota
	PriorityLow
)

type Snapshot struct {
	ResumeID         uuid.UUID
	Revision         int64
	SchemaVersion    int
	PublicGeneration int64
	Payload          []byte
	RevisionTime     time.Time
}

type Request struct {
	Format             Format
	Priority           Priority
	Prepare            func(context.Context) (Snapshot, error)
	ValidateGeneration func(context.Context, Snapshot) error
}

type Navigation struct {
	ResumeID     uuid.UUID
	JobID        uuid.UUID
	Capability   string
	Format       Format
	RevisionTime time.Time
}

type Renderer interface {
	Render(context.Context, Navigation) ([]byte, error)
}

type Redemption struct {
	ResumeID   uuid.UUID
	JobID      uuid.UUID
	Audience   string
	Capability string
}

type Result struct {
	Bytes    []byte
	Digest   [32]byte
	Revision int64
}

type Timer interface {
	Stop() bool
}

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
