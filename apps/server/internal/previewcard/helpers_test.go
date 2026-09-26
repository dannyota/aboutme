package previewcard

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

type unusedReadStore struct{}

func (unusedReadStore) GetPublicState(context.Context) (store.PublicState, error) {
	return store.PublicState{}, errors.New("unused")
}
func (unusedReadStore) GetPublicResumeBySlug(context.Context, string) (store.Resume, error) {
	return store.Resume{}, errors.New("unused")
}
func (unusedReadStore) GetPublicResumeByOwner(context.Context, store.GetPublicResumeByOwnerParams) (store.Resume, error) {
	return store.Resume{}, errors.New("unused")
}
func (unusedReadStore) ListEligiblePublicSlugs(context.Context) ([]string, error) { return nil, nil }

func testReader(t *testing.T) *publicresume.Reader {
	t.Helper()
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	origin, err := publicresume.ParsePublicOrigin("https://resume.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{
		Store: unusedReadStore{}, Projector: docmigrate.NewIdentityProjector(), Coordinator: coordinator, Origin: origin,
	})
	if err != nil {
		t.Fatal(err)
	}
	return reader
}

// testRow is a live resume row named name with headline and contacts.
func testRow(t *testing.T, id uuid.UUID, name, headline string, contacts []schema.PersonalDetail) store.Resume {
	t.Helper()
	slug, lng := "ada-lovelace", "en"
	document := schema.Resume{
		SchemaVersion:   schema.CurrentVersion,
		PersonalDetails: schema.PersonalDetails{FullName: &name, Details: contacts},
		Content:         map[string]schema.Section{},
	}
	if headline != "" {
		document.PersonalDetails.Headline = &headline
	}
	document.Customization.Colors = schema.Colors{Primary: "#1d4ed8", Text: "#111111", Background: "#ffffff"}
	personal, err := json.Marshal(document.PersonalDetails)
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(document.Content)
	if err != nil {
		t.Fatal(err)
	}
	customization, err := json.Marshal(document.Customization)
	if err != nil {
		t.Fatal(err)
	}
	return store.Resume{
		ID: id, Slug: &slug, Live: true, Revision: 3, Lng: &lng, SchemaVersion: int32(schema.CurrentVersion),
		PersonalDetails: personal, Content: content, Customization: customization,
		UpdatedAt: time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC),
	}
}

func testSnapshot(t *testing.T, name, headline string, contacts []schema.PersonalDetail) publicresume.Snapshot {
	t.Helper()
	snapshot, err := testReader(t).ProjectRow(testRow(t, uuid.New(), name, headline, contacts))
	if err != nil {
		t.Fatalf("ProjectRow() error = %v", err)
	}
	return snapshot
}

// fakeStore is an in-memory CardStore.
type fakeStore struct {
	mu      sync.Mutex
	live    map[uuid.UUID]publicresume.Snapshot
	stored  map[uuid.UUID]StoredCard
	saveErr error
	saves   int
	liveErr error
	// listFailures fails that many LiveCards calls before one succeeds.
	listFailures int
}

func newFakeStore() *fakeStore {
	return &fakeStore{live: map[uuid.UUID]publicresume.Snapshot{}, stored: map[uuid.UUID]StoredCard{}}
}

func (s *fakeStore) setLive(snapshot publicresume.Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.live[snapshot.ResumeID] = snapshot
}

func (s *fakeStore) hasCard(id uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, found := s.stored[id]
	return found
}

func (s *fakeStore) Stored(_ context.Context, id uuid.UUID) (StoredCard, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	card, found := s.stored[id]
	return card, found, nil
}

func (s *fakeStore) Live(_ context.Context, id uuid.UUID) (publicresume.Snapshot, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.liveErr != nil {
		return publicresume.Snapshot{}, false, s.liveErr
	}
	snapshot, found := s.live[id]
	return snapshot, found, nil
}

func (s *fakeStore) LiveCards(context.Context) ([]LiveCard, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listFailures > 0 {
		s.listFailures--
		return nil, errors.New("database down")
	}
	cards := make([]LiveCard, 0, len(s.live))
	for id := range s.live {
		cards = append(cards, LiveCard{ResumeID: id, StoredVersion: s.stored[id].Version})
	}
	return cards, nil
}

// Save follows the real store's rule: live and current, or refused.
func (s *fakeStore) Save(_ context.Context, id uuid.UUID, version string, png []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saves++
	if s.saveErr != nil {
		return s.saveErr
	}
	snapshot, live := s.live[id]
	if !live {
		return ErrNotLive
	}
	if current, err := VersionOf(snapshot); err != nil || current != version {
		return ErrStale
	}
	s.stored[id] = StoredCard{Version: version, PNG: append([]byte(nil), png...)}
	return nil
}

// fakeQueue runs Prepare and ValidateGeneration like the real queue and
// returns a fixed PNG. A non-nil gate blocks each render until it closes.
type fakeQueue struct {
	mu         sync.Mutex
	calls      int
	priorities []renderjob.Priority
	payloads   [][]byte
	gate       chan struct{}
	err        error
}

var testPNG = []byte("\x89PNG\r\n\x1a\ncard")

func (q *fakeQueue) Render(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
	q.mu.Lock()
	q.calls++
	q.priorities = append(q.priorities, request.Priority)
	gate, failure := q.gate, q.err
	q.mu.Unlock()
	if request.Format != renderjob.Card {
		return renderjob.Result{}, renderjob.ErrInvalidRequest
	}
	snapshot, err := request.Prepare(ctx)
	if err != nil {
		return renderjob.Result{}, err
	}
	q.mu.Lock()
	q.payloads = append(q.payloads, snapshot.Payload)
	q.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return renderjob.Result{}, ctx.Err()
		}
	}
	if failure != nil {
		return renderjob.Result{}, failure
	}
	if err := request.ValidateGeneration(ctx, snapshot); err != nil {
		return renderjob.Result{}, err
	}
	return renderjob.Result{Bytes: testPNG, Digest: sha256.Sum256(testPNG), Revision: snapshot.Revision}, nil
}

func (q *fakeQueue) renderCalls() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.calls
}

func waitUntil(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition was not met")
		}
		time.Sleep(time.Millisecond)
	}
}
