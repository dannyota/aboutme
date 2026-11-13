package previewcard

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/printsnapshot"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/renderjob"
)

const buildTimeout = renderjob.MaxJobTimeout + 10*time.Second

var ErrRender = errors.New("previewcard: render failed")

type Queue interface {
	Render(context.Context, renderjob.Request) (renderjob.Result, error)
}

type CardStore interface {
	Stored(context.Context, uuid.UUID) (StoredCard, bool, error)
	Live(context.Context, uuid.UUID) (publicresume.Snapshot, bool, error)
	LiveCards(context.Context) ([]LiveCard, error)
	Save(context.Context, uuid.UUID, string, []byte) error
}

var _ CardStore = (*Store)(nil)

type Service struct {
	store  CardStore
	reader *publicresume.Reader
	queue  Queue

	mu      sync.Mutex
	flights map[flightKey]*flight
}

type flightKey struct {
	resumeID uuid.UUID
	version  string
	priority renderjob.Priority
}

type flight struct {
	done   chan struct{}
	png    []byte
	err    error
	joined int
}

func NewService(cards CardStore, reader *publicresume.Reader, queue Queue) (*Service, error) {
	if cards == nil || reader == nil || queue == nil {
		return nil, errors.New("previewcard: invalid service dependencies")
	}
	return &Service{store: cards, reader: reader, queue: queue, flights: make(map[flightKey]*flight)}, nil
}

func (s *Service) Version(snapshot publicresume.Snapshot) (string, error) {
	return VersionOf(snapshot)
}

func (s *Service) Stored(ctx context.Context, resumeID uuid.UUID) (StoredCard, bool, error) {
	return s.store.Stored(ctx, resumeID)
}

func (s *Service) Build(ctx context.Context, snapshot publicresume.Snapshot, priority renderjob.Priority) ([]byte, error) {
	card, err := FromSnapshot(snapshot)
	if err != nil {
		return nil, err
	}
	version, err := card.Version()
	if err != nil {
		return nil, err
	}
	pending, started := s.join(snapshot.ResumeID, version, priority)
	if started != nil {
		buildCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), buildTimeout)
		go func() {
			defer cancel()
			s.run(buildCtx, *started, pending, snapshot, card)
		}()
	}
	select {
	case <-pending.done:
		return pending.png, pending.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Service) join(resumeID uuid.UUID, version string, priority renderjob.Priority) (*flight, *flightKey) {
	s.mu.Lock()
	defer s.mu.Unlock()
	normal := flightKey{resumeID: resumeID, version: version, priority: renderjob.PriorityNormal}
	if pending, found := s.flights[normal]; found {
		pending.joined++
		return pending, nil
	}
	key := flightKey{resumeID: resumeID, version: version, priority: priority}
	if pending, found := s.flights[key]; found {
		pending.joined++
		return pending, nil
	}
	pending := &flight{done: make(chan struct{}), joined: 1}
	s.flights[key] = pending
	return pending, &key
}

func (s *Service) run(ctx context.Context, key flightKey, pending *flight, snapshot publicresume.Snapshot, card Card) {
	png, err := s.render(ctx, snapshot, card, key.version, key.priority)
	if err == nil {
		err = s.store.Save(ctx, key.resumeID, key.version, png)
	}
	if err != nil && !errors.Is(err, ErrStale) {
		png = nil
	}
	pending.png, pending.err = png, err
	s.mu.Lock()
	delete(s.flights, key)
	s.mu.Unlock()
	close(pending.done)
}

func (s *Service) render(ctx context.Context, snapshot publicresume.Snapshot, card Card, version string, priority renderjob.Priority) ([]byte, error) {
	result, err := s.queue.Render(ctx, renderjob.Request{
		Format:   renderjob.Card,
		Priority: priority,
		Prepare: func(prepareCtx context.Context) (renderjob.Snapshot, error) {
			var photo []byte
			var contentType string
			if card.PhotoKeyDigest != "" {
				var readErr error
				photo, contentType, readErr = s.reader.ReadPhoto(prepareCtx, snapshot)
				if readErr != nil {
					return renderjob.Snapshot{}, readErr
				}
			}
			envelope, envelopeErr := printsnapshot.NewCardEnvelope(snapshot.ResumeID, card.Input(), photo, contentType)
			if envelopeErr != nil {
				return renderjob.Snapshot{}, envelopeErr
			}
			payload, marshalErr := printsnapshot.MarshalCard(envelope)
			if marshalErr != nil {
				return renderjob.Snapshot{}, marshalErr
			}
			return renderjob.Snapshot{
				ResumeID: snapshot.ResumeID, Revision: snapshot.Revision,
				SchemaVersion:    int(snapshot.Public.Document.SchemaVersion),
				PublicGeneration: snapshot.Revision, Payload: payload, RevisionTime: snapshot.RevisionTime,
			}, nil
		},
		ValidateGeneration: func(validateCtx context.Context, frozen renderjob.Snapshot) error {
			current, live, readErr := s.store.Live(validateCtx, frozen.ResumeID)
			if readErr != nil || !live {
				return renderjob.ErrGenerationChanged
			}
			if currentVersion, versionErr := VersionOf(current); versionErr != nil || currentVersion != version {
				return renderjob.ErrGenerationChanged
			}
			return nil
		},
	})
	if err != nil {
		return nil, err
	}
	if len(result.Bytes) == 0 || len(result.Bytes) > MaxPNGBytes {
		return nil, ErrRender
	}
	return result.Bytes, nil
}
