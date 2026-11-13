package viewcount

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	minTokenAge        = 8 * time.Second
	maxTokenAge        = 30 * time.Minute
	hourlyCap          = 30
	signalWindow       = 10 * time.Minute
	defaultMapLimit    = 100_000
	defaultBufferLimit = 10_000
)

var (
	ErrNotFound      = errors.New("viewcount: resume not found")
	ErrBusy          = errors.New("viewcount: busy")
	ErrInvalidConfig = errors.New("viewcount: invalid config")
)

type LiveResume struct {
	ID    uuid.UUID
	Owner uuid.UUID
}

type Store interface {
	LiveResumeByID(ctx context.Context, id uuid.UUID) (LiveResume, error)
	AddCounts(ctx context.Context, days []DayCell, signals []SignalCell) error
}

type Config struct {
	Store       Store
	Logger      *slog.Logger
	Now         func() time.Time
	Random      io.Reader
	MapLimit    int
	BufferLimit int
}

type Counter struct {
	store  Store
	logger *slog.Logger
	now    func() time.Time
	random io.Reader
	sealer *sealer
	prover *prover

	mu          sync.Mutex
	used        *fifo[viewID]
	signals     *fifo[signalKey]
	day         time.Time
	dayKey      []byte
	networks    *fifo[networkKey]
	flags       map[networkKey]networkFlags
	owners      *fifo[networkKey]
	hour        time.Time
	hourCounts  map[uuid.UUID]int
	buffer      *buffer
	mapLimit    int
	bufferLimit int
}

type signalKey struct {
	resumeID uuid.UUID
	name     string
}

func New(config Config) (*Counter, error) {
	if config.Store == nil || config.Logger == nil {
		return nil, ErrInvalidConfig
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.MapLimit <= 0 {
		config.MapLimit = defaultMapLimit
	}
	if config.BufferLimit <= 0 {
		config.BufferLimit = defaultBufferLimit
	}
	s, err := newSealer(config.Random)
	if err != nil {
		return nil, err
	}
	p, err := newProver(config.Random)
	if err != nil {
		return nil, err
	}
	return &Counter{
		store: config.Store, logger: config.Logger, now: config.Now, random: config.Random,
		sealer: s, prover: p,
		used:        newFIFO[viewID](config.MapLimit),
		signals:     newFIFO[signalKey](config.MapLimit),
		networks:    newFIFO[networkKey](config.MapLimit),
		flags:       make(map[networkKey]networkFlags),
		owners:      newFIFO[networkKey](config.MapLimit),
		hourCounts:  make(map[uuid.UUID]int),
		buffer:      newBuffer(config.BufferLimit),
		mapLimit:    config.MapLimit,
		bufferLimit: config.BufferLimit,
	}, nil
}

type Viewer struct {
	Addr    netip.Addr
	Account *uuid.UUID
}

func (v Viewer) owns(resume LiveResume) bool {
	return v.Account != nil && *v.Account == resume.Owner
}

type StartResult struct {
	Owner     bool
	Token     string
	Challenge Challenge
}

func (c *Counter) Start(resume LiveResume, viewer Viewer) (StartResult, error) {
	now := c.now()
	if viewer.owns(resume) {
		c.mu.Lock()
		c.rotate(now)
		c.markOwner(viewer.Addr, resume.ID)
		c.mu.Unlock()
		return StartResult{Owner: true}, nil
	}
	c.mu.Lock()
	c.used.expire(now)
	full := c.used.full()
	c.mu.Unlock()
	if full {
		return StartResult{}, ErrBusy
	}
	token, id, err := c.sealer.seal(resume.ID, now)
	if err != nil {
		return StartResult{}, err
	}
	challenge, err := c.prover.challenge(id)
	if err != nil {
		return StartResult{}, err
	}
	return StartResult{Token: token, Challenge: challenge}, nil
}

type EdgeLabels struct {
	Bot        bool
	Datacenter bool
}

type CollectInput struct {
	Token     string
	Challenge Challenge
	Solution  Solution
}

type Outcome string

const (
	OutcomeNone       Outcome = ""
	OutcomeCounted    Outcome = "counted"
	OutcomeBot        Outcome = "bot"
	OutcomeDatacenter Outcome = "datacenter"
	OutcomeAnomaly    Outcome = "anomaly"
	OutcomeInvalid    Outcome = "invalid"
	OutcomeCrawler    Outcome = "crawler"
)
