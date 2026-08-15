package publicapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func TestMarkdownHandlerRejectsClosedAdmission(t *testing.T) {
	// This fails if a pre-revocation cache value can bypass the live-state lease.
	slug, reader, coordinator := formatTestReader(t, true)
	cache, err := publiccache.New(4, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewMarkdownHandler(MarkdownDependencies{Reader: reader, Cache: cache, AppDigest: "sha256:app"})
	if err != nil {
		t.Fatal(err)
	}
	transition, err := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: formatResumeID, ExpectedRevision: 1, Class: publicstate.Revoking}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := transition.Close(context.Background(), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/"+slug+".md", nil))
	if w.Code != http.StatusServiceUnavailable || w.Body.String() != "Service temporarily unavailable.\n" || w.Header().Get("Retry-After") != "1" {
		t.Fatalf("response = %d %#v %q, want 503 with retry", w.Code, w.Header(), w.Body.String())
	}
	if err := transition.Rollback(); err != nil {
		t.Fatal(err)
	}
}

func TestMarkdownHandlerClassifiesReaderAbsenceAndFailure(t *testing.T) {
	for _, test := range []struct {
		name       string
		readErr    error
		wantStatus int
		wantBody   string
	}{
		{name: "absent", readErr: pgx.ErrNoRows, wantStatus: http.StatusNotFound, wantBody: "Not found.\n"},
		{name: "unavailable", readErr: errors.New("database unavailable"), wantStatus: http.StatusServiceUnavailable, wantBody: "Service temporarily unavailable.\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			slug, reader, _ := formatTestReaderWithError(t, true, test.readErr)
			cache, err := publiccache.New(4, time.Minute, time.Now)
			if err != nil {
				t.Fatal(err)
			}
			handler, err := NewMarkdownHandler(MarkdownDependencies{Reader: reader, Cache: cache, AppDigest: "sha256:app"})
			if err != nil {
				t.Fatal(err)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/"+slug+".md", nil))
			if w.Code != test.wantStatus || w.Body.String() != test.wantBody {
				t.Fatalf("response = %d %q, want %d %q", w.Code, w.Body.String(), test.wantStatus, test.wantBody)
			}
			if test.wantStatus == http.StatusServiceUnavailable && w.Header().Get("Retry-After") != "1" {
				t.Fatalf("Retry-After = %q, want 1", w.Header().Get("Retry-After"))
			}
		})
	}
}

func TestMarkdownHandlerRequiresDiscovery(t *testing.T) {
	// This fails if a live noindex resume can receive Markdown.
	slug, reader, _ := formatTestReader(t, false)
	cache, err := publiccache.New(4, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewMarkdownHandler(MarkdownDependencies{Reader: reader, Cache: cache, AppDigest: "sha256:app"})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/"+slug+".md", nil))
	if w.Code != http.StatusNotFound || w.Body.String() != "Not found.\n" {
		t.Fatalf("response = %d %q", w.Code, w.Body.String())
	}
}

func TestMarkdownHandlerHoldsLeaseThroughResponseWrite(t *testing.T) {
	// This fails if revocation can finish while an admitted Markdown body is still writing.
	slug, reader, coordinator := formatTestReader(t, true)
	cache, err := publiccache.New(4, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewMarkdownHandler(MarkdownDependencies{Reader: reader, Cache: cache, AppDigest: "sha256:app"})
	if err != nil {
		t.Fatal(err)
	}
	writer := &formatBlockingWriter{header: make(http.Header), wrote: make(chan struct{}), unblock: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(writer, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/"+slug+".md", nil))
		close(done)
	}()
	select {
	case <-writer.wrote:
	case <-time.After(time.Second):
		t.Fatal("response never began writing")
	}
	transition, err := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: formatResumeID, ExpectedRevision: 1, Class: publicstate.Revoking}}})
	if err != nil {
		t.Fatal(err)
	}
	drained := make(chan error, 1)
	go func() { drained <- transition.Close(context.Background(), time.Now().Add(time.Second)) }()
	select {
	case err := <-drained:
		t.Fatalf("revocation drained before response completed: %v", err)
	default:
	}
	close(writer.unblock)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handler did not finish response")
	}
	if err := <-drained; err != nil {
		t.Fatal(err)
	}
	if err := transition.Rollback(); err != nil {
		t.Fatal(err)
	}
}

type formatBlockingWriter struct {
	header  http.Header
	wrote   chan struct{}
	unblock chan struct{}
}

func (w *formatBlockingWriter) Header() http.Header { return w.header }
func (w *formatBlockingWriter) WriteHeader(int)     {}
func (w *formatBlockingWriter) Write(body []byte) (int, error) {
	select {
	case <-w.wrote:
	default:
		close(w.wrote)
	}
	<-w.unblock
	return len(body), nil
}

var formatResumeID = uuid.MustParse("00000000-0000-0000-0000-000000000008")

func formatTestReader(t *testing.T, discovery bool) (string, *publicresume.Reader, *publicstate.Coordinator) {
	return formatTestReaderWithError(t, discovery, nil)
}

func formatTestReaderWithError(t *testing.T, discovery bool, readErr error) (string, *publicresume.Reader, *publicstate.Coordinator) {
	t.Helper()
	slug, lng, name := "ada", "en", "Ada"
	document := schema.Resume{SchemaVersion: schema.CurrentVersion, PersonalDetails: schema.PersonalDetails{FullName: &name}, Content: map[string]schema.Section{}}
	pd, err := json.Marshal(document.PersonalDetails)
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
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	origin, err := publicresume.ParsePublicOrigin("https://aboutme.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{Store: formatReadStore{row: store.Resume{ID: formatResumeID, Slug: &slug, Live: true, SEOGeoEnabled: discovery, Revision: 1, Lng: &lng, SchemaVersion: int32(schema.CurrentVersion), PersonalDetails: pd, Content: content, Customization: customization}, err: readErr}, Projector: docmigrate.NewIdentityProjector(), Coordinator: coordinator, Origin: origin})
	if err != nil {
		t.Fatal(err)
	}
	return slug, reader, coordinator
}

type formatReadStore struct {
	row store.Resume
	err error
}

func (s formatReadStore) GetPublicState(context.Context) (store.PublicState, error) {
	return store.PublicState{Singleton: true, DiscoveryGeneration: 1}, nil
}
func (s formatReadStore) GetPublicResumeBySlug(context.Context, string) (store.Resume, error) {
	return s.row, s.err
}
func (s formatReadStore) GetPublicResumeByOwner(context.Context, store.GetPublicResumeByOwnerParams) (store.Resume, error) {
	return store.Resume{}, errors.New("unused")
}
func (s formatReadStore) ListEligiblePublicSlugs(context.Context) ([]string, error) { return nil, nil }
