package publicresume

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

type readerStore struct {
	row   store.Resume
	calls int
}

type photoBackend struct {
	key         string
	body        io.ReadCloser
	contentType string
	err         error
	calls       int
}

func (b *photoBackend) Put(context.Context, string, string, io.Reader, int64) (media.PutOutcome, error) {
	return media.PutNotCreated, errors.New("unused")
}
func (b *photoBackend) Get(ctx context.Context, key string) (io.ReadCloser, string, error) {
	b.calls++
	b.key = key
	if b.err != nil {
		return nil, "", b.err
	}
	return b.body, b.contentType, nil
}
func (b *photoBackend) Delete(context.Context, string) error { return errors.New("unused") }
func (b *photoBackend) ListPage(context.Context, string, string, int) ([]media.Object, string, error) {
	return nil, "", errors.New("unused")
}

type trackingReadCloser struct {
	io.Reader
	closed   bool
	closeErr error
}

func (r *trackingReadCloser) Close() error { r.closed = true; return r.closeErr }

func (s *readerStore) GetPublicState(context.Context) (store.PublicState, error) {
	return store.PublicState{}, errors.New("unused")
}
func (s *readerStore) GetPublicResumeBySlug(context.Context, string) (store.Resume, error) {
	s.calls++
	return s.row, nil
}
func (s *readerStore) GetPublicResumeByOwner(context.Context, store.GetPublicResumeByOwnerParams) (store.Resume, error) {
	return store.Resume{}, errors.New("unused")
}
func (s *readerStore) ListEligiblePublicSlugs(context.Context) ([]string, error) {
	return nil, errors.New("unused")
}

func TestReaderReadResumeAcquiresCurrentRevision(t *testing.T) {
	slug, lng, name := "ada", "en", "Ada"
	doc := schema.Resume{SchemaVersion: schema.CurrentVersion, PersonalDetails: schema.PersonalDetails{FullName: &name}, Content: map[string]schema.Section{}}
	pd, _ := json.Marshal(doc.PersonalDetails)
	content, _ := json.Marshal(doc.Content)
	customization, _ := json.Marshal(doc.Customization)
	row := store.Resume{ID: uuid.New(), Slug: &slug, Live: true, Revision: 4, Lng: &lng, SchemaVersion: int32(schema.CurrentVersion), PersonalDetails: pd, Content: content, Customization: customization}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	origin, _ := ParsePublicOrigin("https://resume.example", "production")
	reader, err := NewReader(ReaderDependencies{Store: &readerStore{row: row}, Projector: docmigrate.NewIdentityProjector(), Coordinator: coordinator, Origin: origin})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, lease, err := reader.ReadResume(context.Background(), slug, publicstate.RepresentationJSON)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	if snapshot.Revision != 4 || snapshot.Public.Revision != "4" || snapshot.Public.Slug != slug {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestPublicReadMismatchRetriesOnceThenUnavailable(t *testing.T) {
	slug, lng, name := "ada", "en", "Ada"
	doc := schema.Resume{SchemaVersion: schema.CurrentVersion, PersonalDetails: schema.PersonalDetails{FullName: &name}, Content: map[string]schema.Section{}}
	pd, _ := json.Marshal(doc.PersonalDetails)
	content, _ := json.Marshal(doc.Content)
	customization, _ := json.Marshal(doc.Customization)
	id := uuid.New()
	row := store.Resume{ID: id, Slug: &slug, Live: true, Revision: 3, Lng: &lng, SchemaVersion: int32(schema.CurrentVersion), PersonalDetails: pd, Content: content, Customization: customization}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := coordinator.AcquireResume(context.Background(), id, 4, publicstate.RepresentationJSON)
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	origin, _ := ParsePublicOrigin("https://resume.example", "production")
	publicStore := &readerStore{row: row}
	reader, err := NewReader(ReaderDependencies{Store: publicStore, Projector: docmigrate.NewIdentityProjector(), Coordinator: coordinator, Origin: origin})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := reader.ReadResume(context.Background(), slug, publicstate.RepresentationJSON); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("ReadResume() error = %v, want unavailable after second mismatch", err)
	}
	if publicStore.calls != 2 {
		t.Fatalf("database reads = %d, want 2", publicStore.calls)
	}
}

func TestReaderReadPhotoKeepsStoredKeyPrivateAndCloses(t *testing.T) {
	for _, contentType := range []string{"image/jpeg", "image/png"} {
		t.Run(contentType, func(t *testing.T) {
			body := &trackingReadCloser{Reader: strings.NewReader("image-bytes")}
			backend := &photoBackend{body: body, contentType: contentType}
			reader := &Reader{media: backend}
			snapshot := Snapshot{photoKey: "private/object-key"}
			got, gotType, err := reader.ReadPhoto(context.Background(), snapshot)
			if err != nil || string(got) != "image-bytes" || gotType != contentType || backend.key != "private/object-key" || !body.closed {
				t.Fatalf("ReadPhoto() = %q, %q, %v; key=%q closed=%v", got, gotType, err, backend.key, body.closed)
			}
			encoded, err := json.Marshal(snapshot.Public)
			if err != nil || strings.Contains(string(encoded), backend.key) || strings.Contains(errString(err), backend.key) {
				t.Fatalf("stored key leaked: json=%s err=%v", encoded, err)
			}
		})
	}
}

func TestReaderReadPhotoRejectsUnavailableMedia(t *testing.T) {
	for _, test := range []struct {
		name     string
		ctx      context.Context
		snapshot Snapshot
		backend  *photoBackend
	}{
		{"missing reference", context.Background(), Snapshot{}, &photoBackend{}},
		{"not found", context.Background(), Snapshot{photoKey: "private/key"}, &photoBackend{err: media.ErrNotFound}},
		{"oversize", context.Background(), Snapshot{photoKey: "private/key"}, &photoBackend{body: io.NopCloser(strings.NewReader(strings.Repeat("x", media.MaxObjectBytes+1))), contentType: "image/png"}},
		{"bad type", context.Background(), Snapshot{photoKey: "private/key"}, &photoBackend{body: io.NopCloser(strings.NewReader("x")), contentType: "image/webp"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			reader := &Reader{media: test.backend}
			_, _, err := reader.ReadPhoto(test.ctx, test.snapshot)
			if !errors.Is(err, ErrUnavailable) || strings.Contains(errString(err), "private/key") {
				t.Fatalf("ReadPhoto() error = %v", err)
			}
		})
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	backend := &photoBackend{body: io.NopCloser(strings.NewReader("x")), contentType: "image/png"}
	if _, _, err := (&Reader{media: backend}).ReadPhoto(canceled, Snapshot{photoKey: "private/key"}); !errors.Is(err, context.Canceled) || backend.calls != 0 {
		t.Fatalf("canceled ReadPhoto() error=%v calls=%d", err, backend.calls)
	}
	closeFail := &trackingReadCloser{Reader: strings.NewReader("x"), closeErr: errors.New("close failure")}
	if _, _, err := (&Reader{media: &photoBackend{body: closeFail, contentType: "image/png"}}).ReadPhoto(context.Background(), Snapshot{photoKey: "private/key"}); !errors.Is(err, ErrUnavailable) || !closeFail.closed {
		t.Fatalf("close failure = %v, closed=%v", err, closeFail.closed)
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
