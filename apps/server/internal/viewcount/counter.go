// Package viewcount counts real views of public resumes without storing
// personal data (docs/design/viewer-analytics/counting.md, ADR 0061). It
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
	ID    uuid.UUID
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
	Store  Store
	Logger *slog.Logger
	Now    func() time.Time
	Random io.Reader
	// MapLimit and BufferLimit override the budget defaults in tests.
	MapLimit    int
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
	Addr    netip.Addr
	Account *uuid.UUID
}

func (v Viewer) owns(resume LiveResume) bool {
	return v.Account != nil && *v.Account == resume.Owner
}

// StartResult is either an owner flag or a token with its challenge.
type StartResult struct {
	Owner     bool
	Token     string
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
	Bot        bool
	Datacenter bool
}

// CollectInput is a validated collect body.
type CollectInput struct {
	Token     string
	Challenge Challenge
	Solution  Solution
}

// Outcome is what one collect recorded.
type Outcome string

// Outcomes. OutcomeNone records nothing.
const (
	OutcomeNone       Outcome = ""
	OutcomeCounted    Outcome = "counted"
	OutcomeBot        Outcome = "bot"
	OutcomeDatacenter Outcome = "datacenter"
	OutcomeAnomaly    Outcome = "anomaly"
	OutcomeInvalid    Outcome = "invalid"
	OutcomeCrawler    Outcome = "crawler"
)

// Collect applies layers 5 to 7 and the edge labels to one report and
// returns the outcome it recorded. The checks that need no database run
// first: token age, single use, and proof of work. Only a report that passes
// them costs a live-gate read. Each outcome is recorded at most once per
// network per resume per day. A token that does not open records nothing,
// since it names no resume.
func (c *Counter) Collect(ctx context.Context, in CollectInput, viewer Viewer, labels EdgeLabels) Outcome {
	if !viewer.Addr.IsValid() {
		return OutcomeNone
	}
	claims, err := c.sealer.open(in.Token)
	if err != nil {
		return OutcomeNone
	}
	c.mu.Lock()
	verdict := c.judge(in, claims, c.now(), labels)
	c.mu.Unlock()
	if verdict == OutcomeNone {
		return OutcomeNone
	}
	// A failed check is recorded without the live gate; the flush skips a
	// deleted resume.
	resume := LiveResume{ID: claims.resumeID}
	if verdict != OutcomeInvalid {
		resume, err = c.store.LiveResumeByID(ctx, claims.resumeID)
		if err != nil {
			return OutcomeNone
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	c.rotate(now)
	if viewer.owns(resume) {
		c.markOwner(viewer.Addr, resume.ID)
		return OutcomeNone
	}
	key := deriveNetworkKey(c.dayKey, viewer.Addr, resume.ID)
	outcome := verdict
	if outcome == OutcomeCounted {
		if c.owners.has(key) || c.flags[key]&(flagCounted|flagAnomaly) != 0 {
			return OutcomeNone
		}
		c.hourCounts[resume.ID]++
		if c.hourCounts[resume.ID] > hourlyCap {
			outcome = OutcomeAnomaly
		}
	}
	if !c.markNetwork(key, flagFor(outcome)) {
		return OutcomeNone
	}
	if !c.buffer.addDay(resume.ID, Day(now), outcome) {
		c.logger.WarnContext(ctx, "viewcount: buffer full", "outcome", string(outcome))
		return OutcomeNone
	}
	return outcome
}

// judge applies token age, single use, proof of work, and edge labels. The
// view ID is marked used only after the proof verifies, so reports without
// work cannot fill the used set. Callers hold c.mu.
func (c *Counter) judge(in CollectInput, claims tokenClaims, now time.Time, labels EdgeLabels) Outcome {
	// An expired token records nothing, so a token kept past its life
	// cannot record failures for a resume that has since gone private.
	age := now.Sub(claims.issuedAt)
	if age > maxTokenAge {
		return OutcomeNone
	}
	if age < minTokenAge {
		return OutcomeInvalid
	}
	c.used.expire(now)
	if c.used.has(claims.id) {
		return OutcomeInvalid
	}
	if !c.prover.verify(in.Challenge, in.Solution, claims.id) {
		return OutcomeInvalid
	}
	// Insertion time plus the token lifetime outlasts the token.
	if !c.used.add(claims.id, now.Add(maxTokenAge)) {
		return OutcomeNone
	}
	switch {
	case labels.Bot:
		return OutcomeBot
	case labels.Datacenter:
		return OutcomeDatacenter
	}
	return OutcomeCounted
}

func flagFor(outcome Outcome) networkFlags {
	switch outcome {
	case OutcomeCounted:
		return flagCounted
	case OutcomeBot:
		return flagBot
	case OutcomeDatacenter:
		return flagDatacenter
	case OutcomeAnomaly:
		return flagAnomaly
	default:
		return flagInvalid
	}
}

// markNetwork records flag for key and reports whether it is new. An owner
// network never records anything. When the day's map is full the oldest key
// is dropped, which can only count a network twice; a failed check never
// takes one of the last tenth of the slots, so replayed tokens cannot push
// out the day's marks.
func (c *Counter) markNetwork(key networkKey, flag networkFlags) bool {
	if c.owners.has(key) {
		return false
	}
	current, seen := c.flags[key]
	if current&flag != 0 {
		return false
	}
	// A counted network is not counted again as an anomaly, and the reverse.
	if flag&(flagCounted|flagAnomaly) != 0 && current&(flagCounted|flagAnomaly) != 0 {
		return false
	}
	if !seen {
		if flag == flagInvalid && len(c.flags)*10 >= c.mapLimit*9 {
			return false
		}
		c.insertNetwork(key)
	}
	c.flags[key] = current | flag
	return true
}

// markOwner excludes the owner's network for the rest of the day. Owner
// marks live in their own set, so no other outcome can evict them.
func (c *Counter) markOwner(addr netip.Addr, resumeID uuid.UUID) {
	if !addr.IsValid() {
		return
	}
	key := deriveNetworkKey(c.dayKey, addr, resumeID)
	if c.owners.has(key) {
		return
	}
	if c.owners.full() {
		c.owners.evictOldest()
	}
	c.owners.add(key, c.day.Add(48*time.Hour))
}

func (c *Counter) insertNetwork(key networkKey) {
	if c.networks.full() {
		if evicted, ok := c.networks.evictOldest(); ok {
			delete(c.flags, evicted)
		}
	}
	c.networks.add(key, c.day.Add(48*time.Hour))
}

// rotate starts a new day key at the first request of each Asia/Ho_Chi_Minh
// day, dropping the old key and every network mark, and resets the hourly
// counts at each clock hour. Callers hold c.mu.
func (c *Counter) rotate(now time.Time) {
	if day := Day(now); !day.Equal(c.day) || c.dayKey == nil {
		key := make([]byte, 32)
		if _, err := io.ReadFull(c.random, key); err != nil {
			// Without fresh randomness the old key stays; counts stay correct
			// and only the key's lifetime grows.
			c.logger.Error("viewcount: day key rotation failed")
		} else {
			c.dayKey = key
			c.day = day
			c.networks = newFIFO[networkKey](c.mapLimit)
			c.flags = make(map[networkKey]networkFlags)
			c.owners = newFIFO[networkKey](c.mapLimit)
		}
	}
	if hour := hourOf(now); !hour.Equal(c.hour) {
		c.hour = hour
		c.hourCounts = make(map[uuid.UUID]int)
	}
}

// ObserveHTML records layer 1 for a GET of a live resume page: a
// link-preview fetch becomes a share signal and a declared crawler becomes
// a crawler outcome, each at most once per resume per fetcher per 10
// minutes. Browsers record nothing here; their page script decides.
func (c *Counter) ObserveHTML(ctx context.Context, resumeID uuid.UUID, userAgent string) {
	kind, platform := ClassifyUserAgent(userAgent)
	if kind == AgentBrowser {
		return
	}
	name := string(platform)
	if kind == AgentCrawler {
		name = "crawler"
	}
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.signals.expire(now)
	key := signalKey{resumeID: resumeID, name: name}
	if c.signals.has(key) || !c.signals.add(key, now.Add(signalWindow)) {
		return
	}
	day := Day(now)
	var added bool
	if kind == AgentPreview {
		added = c.buffer.addSignal(resumeID, day, platform)
	} else {
		added = c.buffer.addDay(resumeID, day, OutcomeCrawler)
	}
	if !added {
		c.logger.WarnContext(ctx, "viewcount: buffer full", "outcome", name)
	}
}
