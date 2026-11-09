package viewcount

import (
	"context"
	"io"
	"net/netip"
	"time"

	"github.com/google/uuid"
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
