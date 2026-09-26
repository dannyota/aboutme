package previewcard

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
)

func newTestService(t *testing.T, cards CardStore, queue Queue) *Service {
	t.Helper()
	service, err := NewService(cards, testReader(t), queue)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestBuildStoresTheCardAndJoinsConcurrentRequests(t *testing.T) {
	t.Parallel()
	cards, queue := newFakeStore(), &fakeQueue{gate: make(chan struct{})}
	service := newTestService(t, cards, queue)
	snapshot := testSnapshot(t, "Ada Lovelace", "Engineer", nil)
	cards.setLive(snapshot)
	version := mustVersion(t, snapshot)
	key := flightKey{resumeID: snapshot.ResumeID, version: version, priority: renderjob.PriorityNormal}

	results := make(chan error, 3)
	for range 3 {
		go func() {
			png, err := service.Build(context.Background(), snapshot, renderjob.PriorityNormal)
			if err == nil && !bytes.Equal(png, testPNG) {
				err = errors.New("unexpected bytes")
			}
			results <- err
		}()
	}
	// Every caller must have joined the one flight before the gate opens.
	// Releasing it as soon as the first render starts does not: a caller
	// still on its way to join can arrive after that build finishes and
	// its flight is removed, and start a second, redundant one.
	waitUntil(t, func() bool {
		service.mu.Lock()
		defer service.mu.Unlock()
		pending, found := service.flights[key]
		return found && pending.joined == 3
	})
	close(queue.gate)
	for range 3 {
		if err := <-results; err != nil {
			t.Fatalf("Build() error = %v", err)
		}
	}
	if queue.renderCalls() != 1 {
		t.Fatalf("renders = %d, want one joined build", queue.renderCalls())
	}
	stored, found, err := cards.Stored(context.Background(), snapshot.ResumeID)
	if err != nil || !found || stored.Version != version || !bytes.Equal(stored.PNG, testPNG) {
		t.Fatalf("stored card = %+v, %v, %v", stored, found, err)
	}
	queue.mu.Lock()
	payload := string(queue.payloads[0])
	queue.mu.Unlock()
	if !strings.Contains(payload, `"kind":"card"`) || !strings.Contains(payload, `"name":"Ada Lovelace"`) {
		t.Fatalf("render payload = %s", payload)
	}
}

// A build whose resume is unpublished while it renders stores nothing and
// returns no bytes.
func TestBuildStoresNothingAfterUnpublish(t *testing.T) {
	t.Parallel()
	cards, queue := newFakeStore(), &fakeQueue{gate: make(chan struct{})}
	service := newTestService(t, cards, queue)
	snapshot := testSnapshot(t, "Ada Lovelace", "Engineer", nil)
	cards.setLive(snapshot)

	result := make(chan error, 1)
	var png []byte
	go func() {
		var err error
		png, err = service.Build(context.Background(), snapshot, renderjob.PriorityNormal)
		result <- err
	}()
	waitUntil(t, func() bool { return queue.renderCalls() == 1 })
	cards.mu.Lock()
	delete(cards.live, snapshot.ResumeID)
	cards.mu.Unlock()
	close(queue.gate)
	if err := <-result; !errors.Is(err, renderjob.ErrGenerationChanged) || png != nil {
		t.Fatalf("Build() after unpublish = %d bytes, %v, want no bytes and a changed generation", len(png), err)
	}
	if cards.hasCard(snapshot.ResumeID) {
		t.Fatal("a card was stored after unpublish")
	}
}

func TestBuildRefusedByTheStoreReturnsNoBytesUnlessOnlyStale(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		saveErr   error
		wantBytes bool
	}{
		{saveErr: ErrStale, wantBytes: true},
		{saveErr: ErrNotLive, wantBytes: false},
		{saveErr: errors.New("database down"), wantBytes: false},
	} {
		cards, queue := newFakeStore(), &fakeQueue{}
		cards.saveErr = test.saveErr
		service := newTestService(t, cards, queue)
		snapshot := testSnapshot(t, "Ada Lovelace", "", nil)
		cards.setLive(snapshot)
		png, err := service.Build(context.Background(), snapshot, renderjob.PriorityNormal)
		if !errors.Is(err, test.saveErr) || (png != nil) != test.wantBytes {
			t.Fatalf("Build() with save error %v = %d bytes, %v", test.saveErr, len(png), err)
		}
	}
}

// A caller that leaves does not cancel the build for the others: the card
// is still stored.
func TestBuildOutlivesALeavingCaller(t *testing.T) {
	t.Parallel()
	cards, queue := newFakeStore(), &fakeQueue{gate: make(chan struct{})}
	service := newTestService(t, cards, queue)
	snapshot := testSnapshot(t, "Ada Lovelace", "Engineer", nil)
	cards.setLive(snapshot)

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := service.Build(ctx, snapshot, renderjob.PriorityLow)
		result <- err
	}()
	waitUntil(t, func() bool { return queue.renderCalls() == 1 })
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("Build() after the caller left = %v, want context.Canceled", err)
	}
	close(queue.gate)
	waitUntil(t, func() bool {
		return cards.hasCard(snapshot.ResumeID)
	})
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if queue.priorities[0] != renderjob.PriorityLow {
		t.Fatalf("render priority = %d, want low", queue.priorities[0])
	}
}

func TestBuildRejectsARenderAboveTheCardLimit(t *testing.T) {
	t.Parallel()
	cards := newFakeStore()
	oversized := func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
		if _, err := request.Prepare(ctx); err != nil {
			return renderjob.Result{}, err
		}
		return renderjob.Result{Bytes: make([]byte, MaxPNGBytes+1)}, nil
	}
	service := newTestService(t, cards, queueFunc(oversized))
	snapshot := testSnapshot(t, "Ada Lovelace", "", nil)
	cards.setLive(snapshot)
	if _, err := service.Build(context.Background(), snapshot, renderjob.PriorityNormal); !errors.Is(err, ErrRender) {
		t.Fatalf("Build() with an oversized render = %v, want ErrRender", err)
	}
	if cards.saves != 0 {
		t.Fatalf("saves = %d, want 0", cards.saves)
	}
}

type queueFunc func(context.Context, renderjob.Request) (renderjob.Result, error)

func (f queueFunc) Render(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
	return f(ctx, request)
}

// A request never waits on a background build that owner exports may keep
// out of the queue: it starts its own normal-priority build.
func TestNormalBuildDoesNotJoinALowPriorityBuild(t *testing.T) {
	t.Parallel()
	cards, queue := newFakeStore(), &fakeQueue{gate: make(chan struct{})}
	service := newTestService(t, cards, queue)
	snapshot := testSnapshot(t, "Ada Lovelace", "", nil)
	cards.setLive(snapshot)
	low := make(chan error, 1)
	go func() {
		_, err := service.Build(context.Background(), snapshot, renderjob.PriorityLow)
		low <- err
	}()
	waitUntil(t, func() bool { return queue.renderCalls() == 1 })
	normal := make(chan error, 1)
	go func() {
		_, err := service.Build(context.Background(), snapshot, renderjob.PriorityNormal)
		normal <- err
	}()
	waitUntil(t, func() bool { return queue.renderCalls() == 2 })
	// A later low-priority caller joins the normal build.
	if _, started := service.join(snapshot.ResumeID, mustVersion(t, snapshot), renderjob.PriorityLow); started != nil {
		t.Fatal("a low-priority caller started a third build")
	}
	close(queue.gate)
	for _, result := range []chan error{low, normal} {
		if err := <-result; err != nil {
			t.Fatalf("Build() error = %v", err)
		}
	}
	if queue.renderCalls() != 2 {
		t.Fatalf("renders = %d, want one low and one normal build", queue.renderCalls())
	}
}
