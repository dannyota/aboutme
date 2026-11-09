// Package viewcount counts real views of public resumes without storing
// personal data (docs/design/viewer-analytics/counting.md, ADR 0022). It
// keeps tokens, dedupe keys, and pending counts in memory, and writes only
// daily aggregates.
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

// Bounds from docs/design/budgets.md, "Viewer analytics".
const (
	minTokenAge        = 8 * time.Second
	maxTokenAge        = 30 * time.Minute
	hourlyCap          = 30
	signalWindow       = 10 * time.Minute
	defaultMapLimit    = 100_000
	defaultBufferLimit = 10_000
)

var (
	// ErrNotFound means the resume is not live under that slug.
	ErrNotFound = errors.New("viewcount: resume not found")
	// ErrBusy means the one-time token store is full, so no token is issued.
	ErrBusy = errors.New("viewcount: busy")
	// ErrInvalidConfig reports a missing dependency.
	ErrInvalidConfig = errors.New("viewcount: invalid config")
)

// LiveResume is a live resume as the live-state gate sees it.
type LiveResume struct {
	// ID is the resume's ID.
	ID uuid.UUID
	// Owner is the ID of the account that owns the resume.
	Owner uuid.UUID
}

// Store is the database boundary.
type Store interface {
	// LiveResumeBySlug returns ErrNotFound unless the slug names a live resume.
	LiveResumeBySlug(ctx context.Context, slug string) (LiveResume, error)
	// LiveResumeByID returns ErrNotFound unless the resume is live.
	LiveResumeByID(ctx context.Context, id uuid.UUID) (LiveResume, error)
	// AddCounts adds the cells to the daily rows in one statement per table.
	AddCounts(ctx context.Context, days []DayCell, signals []SignalCell) error
}

// Config supplies a Counter's dependencies. Now and Random default to the
// system clock and crypto/rand.
type Config struct {
	// Store reads the live-state gate and writes daily counts. It is required.
	Store Store
	// Logger receives buffer, flush, and day key failures. It is required.
	Logger *slog.Logger
	// Now returns the current time; nil uses time.Now.
	Now func() time.Time
	// Random supplies key and challenge entropy; nil uses crypto/rand.
	Random io.Reader
	// MapLimit overrides the budget default for each in-memory key set in
	// tests; zero or less keeps the default.
	MapLimit int
	// BufferLimit overrides the budget default for pending count cells in
	// tests; zero or less keeps the default.
	BufferLimit int
}

// Counter applies layers 1 and 4 to 7 and buffers the outcomes.
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

// New builds a Counter with fresh per-process keys.
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

// Viewer is what the HTTP layer knows about the request: its canonical
// client address and, when a valid session cookie came with it, the account.
type Viewer struct {
	// Addr is the canonical client address; the zero value records nothing
	// in Collect.
	Addr netip.Addr
	// Account is the signed-in account ID, or nil without a valid session.
	Account *uuid.UUID
}

func (v Viewer) owns(resume LiveResume) bool {
	return v.Account != nil && *v.Account == resume.Owner
}

// StartResult is either an owner flag or a token with its challenge.
type StartResult struct {
	// Owner reports that the viewer owns the resume; Token and Challenge are
	// then empty.
	Owner bool
	// Token is the sealed one-time view token.
	Token string
	// Challenge is the proof-of-work challenge bound to Token.
	Challenge Challenge
}

// Start issues a one-time token and a proof-of-work challenge for a live
// resume. The owner gets no token, and the owner's network is excluded from
// the resume's count for the rest of the day.
func (c *Counter) Start(ctx context.Context, slug string, viewer Viewer) (StartResult, error) {
	resume, err := c.store.LiveResumeBySlug(ctx, slug)
	if err != nil {
		return StartResult{}, err
	}
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

// EdgeLabels are the WAF labels Caddy passes from the CloudFront edge.
type EdgeLabels struct {
	// Bot reports the edge bot label.
	Bot bool
	// Datacenter reports the edge hosting-network label.
	Datacenter bool
}

// CollectInput is a validated collect body.
type CollectInput struct {
	// Token is the one-time view token that Start issued.
	Token string
	// Challenge is the proof-of-work challenge returned with Token.
	Challenge Challenge
	// Solution is the client's answer to Challenge.
	Solution Solution
}

// Outcome is what one collect recorded.
type Outcome string

// Outcomes. OutcomeNone records nothing.
const (
	// OutcomeNone records nothing.
	OutcomeNone Outcome = ""
	// OutcomeCounted is a view that passed every check.
	OutcomeCounted Outcome = "counted"
	// OutcomeBot is a view the edge labeled as a bot.
	OutcomeBot Outcome = "bot"
	// OutcomeDatacenter is a view the edge labeled as a hosting network.
	OutcomeDatacenter Outcome = "datacenter"
	// OutcomeAnomaly is a valid view above the resume's hourly cap.
	OutcomeAnomaly Outcome = "anomaly"
	// OutcomeInvalid is a report that failed token age, single use, or
	// proof of work.
	OutcomeInvalid Outcome = "invalid"
	// OutcomeCrawler is a declared crawler's fetch of the resume page.
	OutcomeCrawler Outcome = "crawler"
)
