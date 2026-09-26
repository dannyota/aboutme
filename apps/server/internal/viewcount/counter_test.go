package viewcount

import (
	"context"
	"errors"
	"io"
	"log/slog"
	mathrand "math/rand/v2"
	"net/netip"
	"sync"
	"testing"
	"time"

	altcha "github.com/altcha-org/altcha-lib-go/v2"
	"github.com/google/uuid"
)

// Tests follow docs/design/viewer-analytics/counting.md, "Layers" and
// "Aggregation", and ADR 0061.

type fakeStore struct {
	mu      sync.Mutex
	resumes map[string]LiveResume
	gone    map[uuid.UUID]bool
	days    []DayCell
	signals []SignalCell
	fail    error
}

func (s *fakeStore) LiveResumeBySlug(_ context.Context, slug string) (LiveResume, error) {
	resume, ok := s.resumes[slug]
	if !ok {
		return LiveResume{}, ErrNotFound
	}
	return resume, nil
}

func (s *fakeStore) LiveResumeByID(_ context.Context, id uuid.UUID) (LiveResume, error) {
	for _, resume := range s.resumes {
		if resume.ID == id && !s.gone[id] {
			return resume, nil
		}
	}
	return LiveResume{}, ErrNotFound
}

func (s *fakeStore) AddCounts(_ context.Context, days []DayCell, signals []SignalCell) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	s.days = append(s.days, days...)
	s.signals = append(s.signals, signals...)
	return nil
}

type fixture struct {
	t       *testing.T
	store   *fakeStore
	counter *Counter
	now     time.Time
	resume  LiveResume
	owner   uuid.UUID
}

// start is 10:00 in Asia/Ho_Chi_Minh.
var fixtureStart = time.Date(2026, time.September, 26, 3, 0, 0, 0, time.UTC)

func newFixture(t *testing.T, mapLimit int) *fixture {
	t.Helper()
	owner := uuid.MustParse("018f5b6a-9a3e-7c21-8b1e-0000000000a1")
	resume := LiveResume{ID: uuid.MustParse("018f5b6a-9a3e-7c21-8b1e-0000000000b1"), Owner: owner}
	f := &fixture{
		t: t, now: fixtureStart, resume: resume, owner: owner,
		store: &fakeStore{resumes: map[string]LiveResume{"ada-lovelace": resume}, gone: map[uuid.UUID]bool{}},
	}
	counter, err := New(Config{
		Store: f.store, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now: func() time.Time { return f.now }, Random: mathrand.NewChaCha8([32]byte{7}),
		MapLimit: mapLimit,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	f.counter = counter
	return f
}

func anon(addr string) Viewer {
	return Viewer{Addr: netip.MustParseAddr(addr)}
}

func solve(t *testing.T, challenge Challenge) Solution {
	t.Helper()
	solution, err := altcha.SolveChallenge(altcha.SolveChallengeOptions{Challenge: challenge, DeriveKey: altcha.DeriveKeyPBKDF2()})
	if err != nil || solution == nil {
		t.Fatalf("SolveChallenge: %v", err)
	}
	return *solution
}

// view runs start and, after wait, collect, and returns the outcome.
func (f *fixture) view(viewer Viewer, wait time.Duration, labels EdgeLabels) Outcome {
	f.t.Helper()
	in := f.started(viewer)
	f.now = f.now.Add(wait)
	return f.counter.Collect(context.Background(), in, viewer, labels)
}

func (f *fixture) started(viewer Viewer) CollectInput {
	f.t.Helper()
	result, err := f.counter.Start(context.Background(), "ada-lovelace", viewer)
	if err != nil || result.Owner {
		f.t.Fatalf("Start = %+v, %v", result, err)
	}
	return CollectInput{Token: result.Token, Challenge: result.Challenge, Solution: solve(f.t, result.Challenge)}
}

func (f *fixture) flushed() DayCell {
	f.t.Helper()
	if err := f.counter.Flush(context.Background()); err != nil {
		f.t.Fatalf("Flush: %v", err)
	}
	var total DayCell
	for _, cell := range f.store.days {
		total.Counted += cell.Counted
		total.Bot += cell.Bot
		total.Datacenter += cell.Datacenter
		total.Anomaly += cell.Anomaly
		total.Invalid += cell.Invalid
		total.Crawler += cell.Crawler
	}
	return total
}

func TestCollectCountsAValidViewOnce(t *testing.T) {
	f := newFixture(t, 0)
	if got := f.view(anon("203.0.113.5"), 9*time.Second, EdgeLabels{}); got != OutcomeCounted {
		t.Fatalf("first view = %q, want counted", got)
	}
	// Same network, same day: nothing.
	if got := f.view(anon("203.0.113.5"), 9*time.Second, EdgeLabels{}); got != OutcomeNone {
		t.Fatalf("second view = %q, want none", got)
	}
	// Another address on the same IPv6 /64 is the same network.
	if got := f.view(anon("2001:db8:1:2::10"), 9*time.Second, EdgeLabels{}); got != OutcomeCounted {
		t.Fatalf("ipv6 view = %q, want counted", got)
	}
	if got := f.view(anon("2001:db8:1:2:ffff::1"), 9*time.Second, EdgeLabels{}); got != OutcomeNone {
		t.Fatalf("same /64 view = %q, want none", got)
	}
	if total := f.flushed(); total.Counted != 2 {
		t.Fatalf("counted = %d, want 2", total.Counted)
	}
	if got := f.counter.buffer.size(); got != 0 {
		t.Fatalf("buffer after flush = %d cells, want 0", got)
	}
}

func TestCollectDedupeKeyRollsAtVietnamMidnight(t *testing.T) {
	f := newFixture(t, 0)
	f.now = time.Date(2026, time.September, 26, 16, 58, 0, 0, time.UTC) // 23:58 in Vietnam.
	if got := f.view(anon("203.0.113.5"), 9*time.Second, EdgeLabels{}); got != OutcomeCounted {
		t.Fatalf("before midnight = %q, want counted", got)
	}
	f.now = time.Date(2026, time.September, 26, 17, 0, 1, 0, time.UTC) // 00:00:01 the next day.
	if got := f.view(anon("203.0.113.5"), 9*time.Second, EdgeLabels{}); got != OutcomeCounted {
		t.Fatalf("after midnight = %q, want counted", got)
	}
	f.flushed()
	if len(f.store.days) != 2 || f.store.days[0].Day.Equal(f.store.days[1].Day) {
		t.Fatalf("cells = %+v, want two different days", f.store.days)
	}
}

func TestCollectTokenAgeReplayAndProof(t *testing.T) {
	f := newFixture(t, 0)
	if got := f.view(anon("198.51.100.1"), 7*time.Second, EdgeLabels{}); got != OutcomeInvalid {
		t.Fatalf("7 s = %q, want invalid", got)
	}
	if got := f.view(anon("198.51.100.2"), 31*time.Minute, EdgeLabels{}); got != OutcomeNone {
		t.Fatalf("31 min = %q, want none", got)
	}

	in := f.started(anon("198.51.100.3"))
	f.now = f.now.Add(9 * time.Second)
	if got := f.counter.Collect(context.Background(), in, anon("198.51.100.3"), EdgeLabels{}); got != OutcomeCounted {
		t.Fatalf("first use = %q, want counted", got)
	}
	if got := f.counter.Collect(context.Background(), in, anon("198.51.100.4"), EdgeLabels{}); got != OutcomeInvalid {
		t.Fatalf("replay = %q, want invalid", got)
	}

	wrong := f.started(anon("198.51.100.5"))
	wrong.Solution.DerivedKey = "00" + wrong.Solution.DerivedKey[2:]
	f.now = f.now.Add(9 * time.Second)
	if got := f.counter.Collect(context.Background(), wrong, anon("198.51.100.5"), EdgeLabels{}); got != OutcomeInvalid {
		t.Fatalf("wrong solution = %q, want invalid", got)
	}

	// A solved challenge from another token does not transfer.
	first := f.started(anon("198.51.100.6"))
	second := f.started(anon("198.51.100.6"))
	first.Challenge, first.Solution = second.Challenge, second.Solution
	f.now = f.now.Add(9 * time.Second)
	if got := f.counter.Collect(context.Background(), first, anon("198.51.100.6"), EdgeLabels{}); got != OutcomeInvalid {
		t.Fatalf("borrowed challenge = %q, want invalid", got)
	}

	forged := f.started(anon("198.51.100.7"))
	forged.Token = forged.Token[:len(forged.Token)-2] + "AA"
	f.now = f.now.Add(9 * time.Second)
	if got := f.counter.Collect(context.Background(), forged, anon("198.51.100.7"), EdgeLabels{}); got != OutcomeNone {
		t.Fatalf("forged token = %q, want none", got)
	}
}

func TestCollectRejectsChallengeWithoutKeySignature(t *testing.T) {
	f := newFixture(t, 0)
	in := f.started(anon("198.51.100.9"))
	in.Challenge.Parameters.KeySignature = ""
	f.now = f.now.Add(9 * time.Second)
	if got := f.counter.Collect(context.Background(), in, anon("198.51.100.9"), EdgeLabels{}); got != OutcomeInvalid {
		t.Fatalf("stripped key signature = %q, want invalid", got)
	}
}

func TestCollectEdgeLabels(t *testing.T) {
	f := newFixture(t, 0)
	if got := f.view(anon("192.0.2.1"), 9*time.Second, EdgeLabels{Bot: true, Datacenter: true}); got != OutcomeBot {
		t.Fatalf("bot and dc = %q, want bot", got)
	}
	if got := f.view(anon("192.0.2.2"), 9*time.Second, EdgeLabels{Datacenter: true}); got != OutcomeDatacenter {
		t.Fatalf("dc = %q, want datacenter", got)
	}
	// Each filtered outcome is recorded once per network per day.
	if got := f.view(anon("192.0.2.2"), 9*time.Second, EdgeLabels{Datacenter: true}); got != OutcomeNone {
		t.Fatalf("repeat dc = %q, want none", got)
	}
	if total := f.flushed(); total.Bot != 1 || total.Datacenter != 1 || total.Counted != 0 {
		t.Fatalf("totals = %+v", total)
	}
}

func TestOwnerIsNeverCounted(t *testing.T) {
	f := newFixture(t, 0)
	owner := Viewer{Addr: netip.MustParseAddr("203.0.113.9"), Account: &f.owner}
	result, err := f.counter.Start(context.Background(), "ada-lovelace", owner)
	if err != nil || !result.Owner || result.Token != "" {
		t.Fatalf("owner Start = %+v, %v; want owner flag and no token", result, err)
	}
	// The owner's later signed-out view from the same network.
	if got := f.view(anon("203.0.113.9"), 9*time.Second, EdgeLabels{}); got != OutcomeNone {
		t.Fatalf("owner network view = %q, want none", got)
	}
	// A collect carrying the owner's session from another network.
	in := f.started(anon("203.0.113.10"))
	f.now = f.now.Add(9 * time.Second)
	ownerElsewhere := Viewer{Addr: netip.MustParseAddr("203.0.113.10"), Account: &f.owner}
	if got := f.counter.Collect(context.Background(), in, ownerElsewhere, EdgeLabels{}); got != OutcomeNone {
		t.Fatalf("owner collect = %q, want none", got)
	}
	if got := f.view(anon("203.0.113.10"), 9*time.Second, EdgeLabels{}); got != OutcomeNone {
		t.Fatalf("network marked by owner collect = %q, want none", got)
	}
	other := uuid.MustParse("018f5b6a-9a3e-7c21-8b1e-0000000000c1")
	stranger := Viewer{Addr: netip.MustParseAddr("203.0.113.11"), Account: &other}
	if got := f.view(stranger, 9*time.Second, EdgeLabels{}); got != OutcomeCounted {
		t.Fatalf("another account = %q, want counted", got)
	}
}

func TestHourlyCapTurnsBurstsIntoAnomalies(t *testing.T) {
	f := newFixture(t, 0)
	for i := range hourlyCap {
		addr := netip.AddrFrom4([4]byte{10, 0, 1, byte(i + 1)})
		if got := f.view(Viewer{Addr: addr}, 9*time.Second, EdgeLabels{}); got != OutcomeCounted {
			t.Fatalf("view %d = %q, want counted", i, got)
		}
	}
	if got := f.view(anon("10.0.2.1"), 9*time.Second, EdgeLabels{}); got != OutcomeAnomaly {
		t.Fatalf("31st view = %q, want anomaly", got)
	}
	f.now = f.now.Add(time.Hour)
	if got := f.view(anon("10.0.2.2"), 9*time.Second, EdgeLabels{}); got != OutcomeCounted {
		t.Fatalf("next hour = %q, want counted", got)
	}
}

func TestCollectRechecksTheLiveGate(t *testing.T) {
	f := newFixture(t, 0)
	in := f.started(anon("203.0.113.20"))
	f.store.gone[f.resume.ID] = true
	f.now = f.now.Add(9 * time.Second)
	if got := f.counter.Collect(context.Background(), in, anon("203.0.113.20"), EdgeLabels{}); got != OutcomeNone {
		t.Fatalf("unpublished resume = %q, want none", got)
	}
	if _, err := f.counter.Start(context.Background(), "missing", anon("203.0.113.20")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Start(missing) = %v, want ErrNotFound", err)
	}
}

func TestStartFailsClosedWhenTheNonceSetIsFull(t *testing.T) {
	f := newFixture(t, 2)
	for i := range 2 {
		addr := netip.AddrFrom4([4]byte{10, 0, 3, byte(i + 1)})
		if got := f.view(Viewer{Addr: addr}, 9*time.Second, EdgeLabels{}); got != OutcomeCounted {
			t.Fatalf("view %d = %q, want counted", i, got)
		}
	}
	if _, err := f.counter.Start(context.Background(), "ada-lovelace", anon("10.0.3.9")); !errors.Is(err, ErrBusy) {
		t.Fatalf("Start with a full nonce set = %v, want ErrBusy", err)
	}
	f.now = f.now.Add(31 * time.Minute)
	if _, err := f.counter.Start(context.Background(), "ada-lovelace", anon("10.0.3.9")); err != nil {
		t.Fatalf("Start after expiry = %v", err)
	}
}

func TestObserveHTMLRecordsPreviewsAndCrawlers(t *testing.T) {
	f := newFixture(t, 0)
	ctx := context.Background()
	f.counter.ObserveHTML(ctx, f.resume.ID, "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/140.0")
	f.counter.ObserveHTML(ctx, f.resume.ID, "facebookexternalhit/1.1")
	f.counter.ObserveHTML(ctx, f.resume.ID, "facebookexternalhit/1.1")
	f.counter.ObserveHTML(ctx, f.resume.ID, "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)")
	f.now = f.now.Add(11 * time.Minute)
	f.counter.ObserveHTML(ctx, f.resume.ID, "facebookexternalhit/1.1")
	total := f.flushed()
	if total.Crawler != 1 || total.Counted != 0 {
		t.Fatalf("day totals = %+v, want one crawler", total)
	}
	var fetches int32
	for _, cell := range f.store.signals {
		if cell.Platform != PlatformFacebook {
			t.Fatalf("signal platform = %q", cell.Platform)
		}
		fetches += cell.Fetches
	}
	if fetches != 2 {
		t.Fatalf("facebook fetches = %d, want 2 (one per 10 minutes)", fetches)
	}
}

func TestFlushDropsTheBatchOnStoreError(t *testing.T) {
	f := newFixture(t, 0)
	f.view(anon("203.0.113.30"), 9*time.Second, EdgeLabels{})
	f.store.fail = errors.New("database down")
	if err := f.counter.Flush(context.Background()); err == nil {
		t.Fatal("Flush error = nil, want the store error")
	}
	f.store.fail = nil
	if total := f.flushed(); total.Counted != 0 {
		t.Fatalf("counted after a failed flush = %d, want 0", total.Counted)
	}
}

func TestRunFlushesOnShutdown(t *testing.T) {
	f := newFixture(t, 0)
	f.view(anon("203.0.113.40"), 9*time.Second, EdgeLabels{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.counter.Run(ctx)
	if len(f.store.days) != 1 || f.store.days[0].Counted != 1 {
		t.Fatalf("days after shutdown = %+v, want one counted view", f.store.days)
	}
}

func TestBufferFullRecordsNothing(t *testing.T) {
	f := newFixture(t, 0)
	f.counter.bufferLimit = 1
	f.counter.buffer = newBuffer(1)
	f.counter.ObserveHTML(context.Background(), f.resume.ID, "LinkedInBot/1.0")
	if got := f.view(anon("203.0.113.50"), 9*time.Second, EdgeLabels{}); got != OutcomeNone {
		t.Fatalf("view with a full buffer = %q, want none", got)
	}
}

func TestNetworkMapEvictsOldestWhenFull(t *testing.T) {
	f := newFixture(t, 0)
	f.counter.mapLimit = 1
	f.counter.networks = newFIFO[networkKey](1)
	f.counter.used = newFIFO[viewID](100)
	if got := f.view(anon("203.0.113.60"), 9*time.Second, EdgeLabels{}); got != OutcomeCounted {
		t.Fatalf("first = %q", got)
	}
	if got := f.view(anon("203.0.113.61"), 9*time.Second, EdgeLabels{}); got != OutcomeCounted {
		t.Fatalf("second = %q", got)
	}
	// The first network was evicted, so it can count again: the design's
	// accepted failure is a double count, never a lost view.
	if got := f.view(anon("203.0.113.60"), 9*time.Second, EdgeLabels{}); got != OutcomeCounted {
		t.Fatalf("evicted network = %q, want counted", got)
	}
	if len(f.counter.flags) != 1 {
		t.Fatalf("flags = %d entries, want 1", len(f.counter.flags))
	}
}

func TestJunkCollectsCannotFillTheUsedSet(t *testing.T) {
	f := newFixture(t, 1)
	for i := range 3 {
		in := f.started(anon("10.0.4.1"))
		in.Solution.DerivedKey = "00"
		f.now = f.now.Add(9 * time.Second)
		if got := f.counter.Collect(context.Background(), in, anon("10.0.4.1"), EdgeLabels{}); got == OutcomeCounted {
			t.Fatalf("junk collect %d counted", i)
		}
	}
	if _, err := f.counter.Start(context.Background(), "ada-lovelace", anon("10.0.4.2")); err != nil {
		t.Fatalf("Start after junk collects = %v, want a token", err)
	}
}

func TestReplayedTokensCannotEvictOwnerMarks(t *testing.T) {
	f := newFixture(t, 10)
	owner := Viewer{Addr: netip.MustParseAddr("203.0.113.9"), Account: &f.owner}
	if result, err := f.counter.Start(context.Background(), "ada-lovelace", owner); err != nil || !result.Owner {
		t.Fatalf("owner Start = %+v, %v", result, err)
	}
	used := f.started(anon("10.0.5.1"))
	f.now = f.now.Add(9 * time.Second)
	f.counter.Collect(context.Background(), used, anon("10.0.5.1"), EdgeLabels{})
	for i := range 20 {
		addr := netip.AddrFrom16([16]byte{0x20, 0x01, 0x0d, 0xb8, 0, byte(i)})
		f.counter.Collect(context.Background(), used, Viewer{Addr: addr}, EdgeLabels{})
	}
	if len(f.counter.flags) > 9 {
		t.Fatalf("flags = %d entries, want failed checks kept out of the last tenth", len(f.counter.flags))
	}
	if got := f.view(anon("203.0.113.9"), 9*time.Second, EdgeLabels{}); got != OutcomeNone {
		t.Fatalf("owner network after replays = %q, want none", got)
	}
}
