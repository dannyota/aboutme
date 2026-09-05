package publicapi

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
)

type artifactQueueFunc func(context.Context, renderjob.Request) (renderjob.Result, error)

func (f artifactQueueFunc) Render(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
	return f(ctx, request)
}

func newArtifactHarness(t *testing.T, queue RenderQueue, cacheEntries int, now func() time.Time) (*artifactHandlers, *publicServiceStore, *publicstate.Coordinator) {
	t.Helper()
	handlers, backing, coordinator, _ := newArtifactHarnessWithCache(t, queue, cacheEntries, now)
	return handlers, backing, coordinator
}

func newArtifactHarnessWithCache(t *testing.T, queue RenderQueue, cacheEntries int, now func() time.Time) (*artifactHandlers, *publicServiceStore, *publicstate.Coordinator, *publiccache.Cache) {
	t.Helper()
	_, reader, coordinator, backing := publicServiceReaderWithStore(t)
	backing.row.DownloadEnabled = true
	backing.row.SEOGeoEnabled = true
	cache, err := publiccache.New(cacheEntries, time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	handlers, err := newArtifactHandlers(ArtifactDependencies{
		Reader: reader, Cache: cache, Queue: queue, AppDigest: "sha256:app", RendererDigest: "sha256:renderer", Clock: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handlers, backing, coordinator, cache
}

func TestPublicArtifactsSelectExactResponsesAndCacheAfterLiveGate(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC) }
	queueCalls := 0
	queue := artifactQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
		queueCalls++
		snapshot, err := request.Prepare(ctx)
		if err != nil {
			return renderjob.Result{}, err
		}
		if snapshot.PublicGeneration != snapshot.Revision || snapshot.SchemaVersion <= 0 || len(snapshot.Payload) == 0 || request.ValidateGeneration == nil {
			t.Fatalf("prepared public snapshot = %+v", snapshot)
		}
		if err := request.ValidateGeneration(ctx, snapshot); err != nil {
			return renderjob.Result{}, err
		}
		body := []byte("%PDF-1.7\npublic")
		if request.Format == renderjob.PNG {
			body = []byte("\x89PNG\r\npublic")
		}
		return renderjob.Result{Bytes: body, Digest: sha256.Sum256(body), Revision: snapshot.Revision}, nil
	})
	handlers, backing, _ := newArtifactHarness(t, queue, 4, now)

	tests := []struct {
		name        string
		handler     http.Handler
		path        string
		contentType string
		disposition string
	}{
		{name: "pdf", handler: handlers.pdf, path: "/api/v1/public/resumes/ada-lovelace/pdf", contentType: "application/pdf", disposition: "attachment; filename=\"resume.pdf\""},
		{name: "png", handler: handlers.png, path: "/api/v1/public/resumes/ada-lovelace/og.png", contentType: "image/png"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			get := httptest.NewRequest(http.MethodGet, test.path, nil)
			get.RemoteAddr = "192.0.2.1:1234"
			response := httptest.NewRecorder()
			test.handler.ServeHTTP(response, get)
			if response.Code != http.StatusOK || response.Header().Get("Content-Type") != test.contentType || response.Header().Get("Content-Disposition") != test.disposition || response.Header().Get("Cache-Control") != "no-cache, must-revalidate" {
				t.Fatalf("GET response = %d headers=%v body=%q", response.Code, response.Header(), response.Body.Bytes())
			}
			if response.Header().Get("Content-Length") != strconv.Itoa(response.Body.Len()) || response.Header().Get("ETag") == "" {
				t.Fatalf("GET length/etag headers=%v", response.Header())
			}
			tag := response.Header().Get("ETag")

			head := httptest.NewRequest(http.MethodHead, test.path, nil)
			head.RemoteAddr = "192.0.2.1:1234"
			headResponse := httptest.NewRecorder()
			test.handler.ServeHTTP(headResponse, head)
			if headResponse.Code != http.StatusOK || headResponse.Body.Len() != 0 || headResponse.Header().Get("ETag") != tag || headResponse.Header().Get("Content-Length") == "" {
				t.Fatalf("HEAD response = %d headers=%v body=%q", headResponse.Code, headResponse.Header(), headResponse.Body.Bytes())
			}

			conditional := httptest.NewRequest(http.MethodGet, test.path, nil)
			conditional.RemoteAddr = "192.0.2.1:1234"
			conditional.Header.Set("If-None-Match", tag)
			conditionalResponse := httptest.NewRecorder()
			test.handler.ServeHTTP(conditionalResponse, conditional)
			if conditionalResponse.Code != http.StatusNotModified || conditionalResponse.Body.Len() != 0 || conditionalResponse.Header().Get("Content-Length") != "" {
				t.Fatalf("conditional response = %d headers=%v body=%q", conditionalResponse.Code, conditionalResponse.Header(), conditionalResponse.Body.Bytes())
			}
		})
	}
	if queueCalls != 2 {
		t.Fatalf("queue calls = %d, want one cache miss per format", queueCalls)
	}
	if backing.calls != 8 {
		t.Fatalf("live-state reads = %d, want request and generation validation reads", backing.calls)
	}
}

func TestPublicArtifactsRejectOptionsMethodsAndDisabledPDFBeforeQueue(t *testing.T) {
	queueCalls := 0
	queue := artifactQueueFunc(func(context.Context, renderjob.Request) (renderjob.Result, error) {
		queueCalls++
		return renderjob.Result{}, errors.New("must not render")
	})
	handlers, backing, _ := newArtifactHarness(t, queue, 2, time.Now)
	backing.row.DownloadEnabled = false

	for _, test := range []struct {
		name   string
		method string
		path   string
		body   io.Reader
	}{
		{name: "method", method: http.MethodPost, path: "/api/v1/public/resumes/ada-lovelace/og.png"},
		{name: "query", method: http.MethodGet, path: "/api/v1/public/resumes/ada-lovelace/og.png?width=1"},
		{name: "body", method: http.MethodGet, path: "/api/v1/public/resumes/ada-lovelace/og.png", body: strings.NewReader("x")},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(test.method, test.path, test.body)
			req.RemoteAddr = "192.0.2.2:1234"
			response := httptest.NewRecorder()
			handlers.png.ServeHTTP(response, req)
			want := http.StatusBadRequest
			if test.name == "method" {
				want = http.StatusMethodNotAllowed
				if response.Header().Get("Allow") != "GET, HEAD" {
					t.Fatalf("Allow = %q", response.Header().Get("Allow"))
				}
			}
			if response.Code != want {
				t.Fatalf("response = %d %s", response.Code, response.Body.String())
			}
		})
	}
	pdfRequest := httptest.NewRequest(http.MethodGet, "/api/v1/public/resumes/ada-lovelace/pdf", nil)
	pdfRequest.RemoteAddr = "192.0.2.2:1234"
	pdfResponse := httptest.NewRecorder()
	handlers.pdf.ServeHTTP(pdfResponse, pdfRequest)
	if pdfResponse.Code != http.StatusNotFound || queueCalls != 0 {
		t.Fatalf("disabled PDF response = %d queue calls=%d", pdfResponse.Code, queueCalls)
	}
}

func TestPublicArtifactRejectsMalformedConditionalAfterGateBeforeCacheOrRender(t *testing.T) {
	queueCalls := 0
	queue := artifactQueueFunc(func(context.Context, renderjob.Request) (renderjob.Result, error) {
		queueCalls++
		return renderjob.Result{}, errors.New("must not render")
	})
	handlers, backing, _ := newArtifactHarness(t, queue, 2, time.Now)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/resumes/ada-lovelace/pdf", nil)
	request.RemoteAddr = "192.0.2.12:1234"
	request.Header.Set("If-None-Match", "W/\"weak\"")
	response := httptest.NewRecorder()
	handlers.pdf.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || queueCalls != 0 || backing.calls != 1 || response.Header().Get("ETag") != "" {
		t.Fatalf("response = %d headers=%v calls=%d reads=%d", response.Code, response.Header(), queueCalls, backing.calls)
	}
}

type blockingArtifactRequestBody struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingArtifactRequestBody) Read([]byte) (int, error) {
	close(b.started)
	<-b.release
	return 0, io.EOF
}

func (b *blockingArtifactRequestBody) Close() error { return nil }

func TestPublicArtifactRejectsIndeterminateBodyWithoutReading(t *testing.T) {
	handlers, _, _ := newArtifactHarness(t, artifactQueueFunc(func(context.Context, renderjob.Request) (renderjob.Result, error) {
		return renderjob.Result{}, errors.New("must not render")
	}), 2, time.Now)
	for _, test := range []struct {
		name   string
		mutate func(*http.Request)
	}{
		{name: "unknown content length", mutate: func(request *http.Request) { request.ContentLength = -1 }},
		{name: "transfer encoding", mutate: func(request *http.Request) {
			request.ContentLength = 0
			request.TransferEncoding = []string{"chunked"}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := &blockingArtifactRequestBody{started: make(chan struct{}), release: make(chan struct{})}
			request := httptest.NewRequest(http.MethodGet, "/api/v1/public/resumes/ada-lovelace/pdf", body)
			request.RemoteAddr = "192.0.2.17:1234"
			test.mutate(request)
			response := httptest.NewRecorder()
			done := make(chan struct{})
			go func() {
				handlers.pdf.ServeHTTP(response, request)
				close(done)
			}()
			select {
			case <-body.started:
				close(body.release)
				<-done
				t.Fatal("request validation read an indeterminate request body")
			case <-done:
				if response.Code != http.StatusBadRequest {
					t.Fatalf("response = %d body=%q", response.Code, response.Body.String())
				}
			case <-time.After(time.Second):
				close(body.release)
				t.Fatal("request validation did not terminate")
			}
		})
	}
}

func TestPublicArtifactRejectsEmptyQueryMarker(t *testing.T) {
	handlers, _, _ := newArtifactHarness(t, artifactQueueFunc(func(context.Context, renderjob.Request) (renderjob.Result, error) {
		return renderjob.Result{}, errors.New("must not render")
	}), 2, time.Now)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/resumes/ada-lovelace/og.png?", nil)
	request.RemoteAddr = "192.0.2.18:1234"
	response := httptest.NewRecorder()
	handlers.png.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("response = %d body=%q", response.Code, response.Body.String())
	}
}

type artifactPhotoBackend struct {
	media.Backend
	body        []byte
	contentType string
	key         string
}

func (b *artifactPhotoBackend) Get(_ context.Context, key string) (io.ReadCloser, string, error) {
	b.key = key
	return io.NopCloser(bytes.NewReader(b.body)), b.contentType, nil
}

func TestPublicArtifactPrepareReadsNormalizedPhotoUnderLease(t *testing.T) {
	_, _, coordinator, backing := publicServiceReaderWithStore(t)
	backing.row.DownloadEnabled = true
	var personal schema.PersonalDetails
	if err := json.Unmarshal(backing.row.PersonalDetails, &personal); err != nil {
		t.Fatal(err)
	}
	personal.Photo = &schema.Photo{Key: "private/photo.png"}
	encoded, err := json.Marshal(personal)
	if err != nil {
		t.Fatal(err)
	}
	backing.row.PersonalDetails = encoded
	var photo bytes.Buffer
	if err := png.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	backend := &artifactPhotoBackend{body: photo.Bytes(), contentType: "image/png"}
	origin := mustPublicOrigin(t)
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{
		Store: backing, Projector: docmigrate.NewIdentityProjector(), Coordinator: coordinator, Media: backend, Origin: origin,
	})
	if err != nil {
		t.Fatal(err)
	}
	cache, err := publiccache.New(2, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	queue := artifactQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
		snapshot, err := request.Prepare(ctx)
		if err != nil {
			return renderjob.Result{}, err
		}
		if !bytes.Contains(snapshot.Payload, []byte("\"url\":\"data:image/png;base64,")) {
			t.Fatalf("snapshot does not contain an inline normalized photo: %s", snapshot.Payload)
		}
		return renderjob.Result{Bytes: []byte("png"), Revision: snapshot.Revision}, nil
	})
	handlers, err := newArtifactHandlers(ArtifactDependencies{
		Reader: reader, Cache: cache, Queue: queue, AppDigest: "sha256:app", RendererDigest: "sha256:renderer", Clock: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/resumes/ada-lovelace/og.png", nil)
	request.RemoteAddr = "192.0.2.13:1234"
	response := httptest.NewRecorder()
	handlers.png.ServeHTTP(response, request)
	if response.Code != http.StatusOK || backend.key != "private/photo.png" {
		t.Fatalf("response = %d photo key=%q body=%q", response.Code, backend.key, response.Body.String())
	}
}

func TestPublicArtifactsRejectChangedCompletion(t *testing.T) {
	var backing *publicServiceStore
	var coordinator *publicstate.Coordinator
	queue := artifactQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
		snapshot, err := request.Prepare(ctx)
		if err != nil {
			return renderjob.Result{}, err
		}
		transition, err := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: backing.row.ID, ExpectedRevision: backing.row.Revision, Class: publicstate.NonDraining}}})
		if err != nil {
			return renderjob.Result{}, err
		}
		if err := transition.Close(context.Background(), time.Now().Add(time.Second)); err != nil {
			return renderjob.Result{}, err
		}
		backing.row.Revision++
		if err := transition.Commit(publicstate.CommittedState{ResumeRevisions: map[uuid.UUID]int64{backing.row.ID: backing.row.Revision}}); err != nil {
			return renderjob.Result{}, err
		}
		if err := request.ValidateGeneration(ctx, snapshot); err == nil {
			t.Fatal("generation validator accepted changed revision")
		}
		return renderjob.Result{}, renderjob.ErrGenerationChanged
	})
	handlers, store, state := newArtifactHarness(t, queue, 2, time.Now)
	backing, coordinator = store, state
	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/resumes/ada-lovelace/pdf", nil)
	request.RemoteAddr = "192.0.2.3:1234"
	response := httptest.NewRecorder()
	handlers.pdf.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "generation") {
		t.Fatalf("response = %d body=%q", response.Code, response.Body.String())
	}
}

func TestPublicPDFRejectsEligibilityRevokedAtCompletion(t *testing.T) {
	var backing *publicServiceStore
	queue := artifactQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
		snapshot, err := request.Prepare(ctx)
		if err != nil {
			return renderjob.Result{}, err
		}
		backing.row.DownloadEnabled = false
		if err := request.ValidateGeneration(ctx, snapshot); err == nil {
			t.Fatal("generation validator accepted a download-revoked PDF")
		}
		return renderjob.Result{}, renderjob.ErrGenerationChanged
	})
	handlers, store, _ := newArtifactHarness(t, queue, 2, time.Now)
	backing = store
	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/resumes/ada-lovelace/pdf", nil)
	request.RemoteAddr = "192.0.2.15:1234"
	response := httptest.NewRecorder()
	handlers.pdf.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "1" {
		t.Fatalf("response = %d headers=%v body=%q", response.Code, response.Header(), response.Body.String())
	}
}

func TestPublicArtifactUnavailableQueueReturnsOpaque503AfterLiveGate(t *testing.T) {
	handlers, backing, _ := newArtifactHarness(t, nil, 2, time.Now)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/resumes/ada-lovelace/og.png", nil)
	request.RemoteAddr = "192.0.2.16:1234"
	response := httptest.NewRecorder()
	handlers.png.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "1" || backing.calls != 1 {
		t.Fatalf("response = %d headers=%v reads=%d body=%q", response.Code, response.Header(), backing.calls, response.Body.String())
	}
}

func TestPublicArtifactOutputBoundsAndErrorsStayOpaque(t *testing.T) {
	for _, test := range []struct {
		name   string
		format renderjob.Format
		body   []byte
		err    error
	}{
		{name: "empty PDF", format: renderjob.PDF},
		{name: "oversize PDF", format: renderjob.PDF, body: make([]byte, renderjob.PDFMaxBytes+1)},
		{name: "oversize PNG", format: renderjob.PNG, body: make([]byte, renderjob.PNGMaxBytes+1)},
		{name: "queue saturation", format: renderjob.PNG, err: renderjob.ErrSaturated},
		{name: "private error", format: renderjob.PDF, err: errors.New("private-browser-secret")},
	} {
		t.Run(test.name, func(t *testing.T) {
			queue := artifactQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
				if test.err != nil {
					return renderjob.Result{}, test.err
				}
				snapshot, err := request.Prepare(ctx)
				if err != nil {
					return renderjob.Result{}, err
				}
				return renderjob.Result{Bytes: test.body, Revision: snapshot.Revision}, nil
			})
			handlers, _, _ := newArtifactHarness(t, queue, 2, time.Now)
			handler, path := handlers.pdf, "/api/v1/public/resumes/ada-lovelace/pdf"
			if test.format == renderjob.PNG {
				handler, path = handlers.png, "/api/v1/public/resumes/ada-lovelace/og.png"
			}
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.RemoteAddr = "192.0.2.4:1234"
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "1" || strings.Contains(response.Body.String(), "secret") || strings.Contains(response.Body.String(), "renderjob") {
				t.Fatalf("response = %d headers=%v body=%q", response.Code, response.Header(), response.Body.String())
			}
		})
	}
}

func TestPublicArtifactMissLimitIsSharedAcrossFormats(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC) }
	queueCalls := 0
	queue := artifactQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
		queueCalls++
		snapshot, err := request.Prepare(ctx)
		if err != nil {
			return renderjob.Result{}, err
		}
		body := []byte("pdf")
		if request.Format == renderjob.PNG {
			body = []byte("png")
		}
		return renderjob.Result{Bytes: body, Revision: snapshot.Revision}, nil
	})
	handlers, _, _ := newArtifactHarness(t, queue, 1, now)
	for index := 0; index < 21; index++ {
		handler, path := handlers.pdf, "/api/v1/public/resumes/ada-lovelace/pdf"
		if index%2 == 1 {
			handler, path = handlers.png, "/api/v1/public/resumes/ada-lovelace/og.png"
		}
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.RemoteAddr = "192.0.2.5:1234"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if index < 20 && response.Code != http.StatusOK {
			t.Fatalf("request %d = %d body=%q", index+1, response.Code, response.Body.String())
		}
		if index == 20 && (response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "") {
			t.Fatalf("limit response = %d headers=%v body=%q", response.Code, response.Header(), response.Body.String())
		}
	}
	if queueCalls != 20 {
		t.Fatalf("queue calls = %d, want 20", queueCalls)
	}
}

func TestPublicArtifactHitLimitRunsBeforeLiveStateRead(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC) }
	queue := artifactQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
		snapshot, err := request.Prepare(ctx)
		return renderjob.Result{Bytes: []byte("pdf"), Revision: snapshot.Revision}, err
	})
	handlers, backing, _ := newArtifactHarness(t, queue, 2, now)
	for index := 0; index <= publicArtifactRequestsPerMinute; index++ {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/public/resumes/ada-lovelace/pdf", nil)
		request.RemoteAddr = "192.0.2.6:1234"
		response := httptest.NewRecorder()
		handlers.pdf.ServeHTTP(response, request)
		if index < publicArtifactRequestsPerMinute && response.Code != http.StatusOK {
			t.Fatalf("request %d = %d", index+1, response.Code)
		}
		if index == publicArtifactRequestsPerMinute && (response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "") {
			t.Fatalf("overflow response = %d headers=%v", response.Code, response.Header())
		}
	}
	if backing.calls != publicArtifactRequestsPerMinute {
		t.Fatalf("live-state reads = %d, want one admission per allowed request", backing.calls)
	}
}

func TestPublicArtifactOldGenerationCacheCannotAnswer(t *testing.T) {
	queueCalls := 0
	queue := artifactQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
		queueCalls++
		snapshot, err := request.Prepare(ctx)
		return renderjob.Result{Bytes: []byte("current"), Revision: snapshot.Revision}, err
	})
	handlers, backing, _, cache := newArtifactHarnessWithCache(t, queue, 4, time.Now)
	backing.row.Revision = 2
	stale, err := newSelectedResponseWithLimit(http.StatusOK, "application/pdf", "no-cache, must-revalidate", []byte("stale"), http.Header{"Content-Disposition": {"attachment; filename=\"resume.pdf\""}}, renderjob.PDFMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	cache.Put(publiccache.Key{
		RouteClass: "resume", Representation: publicstate.RepresentationPDF, Variant: "default",
		ResumeID: backing.row.ID, Generation: 1, FormatVersion: publicPDFFormatVersion,
		AppDigest: "sha256:app", RendererDigest: "sha256:renderer",
	}, publiccache.Value{Status: stale.Status, Header: stale.Header, Body: stale.Body})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/resumes/ada-lovelace/pdf", nil)
	request.RemoteAddr = "192.0.2.7:1234"
	response := httptest.NewRecorder()
	handlers.pdf.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "current" || queueCalls != 1 {
		t.Fatalf("response = %d %q queue calls=%d", response.Code, response.Body.String(), queueCalls)
	}
}

func TestPublicArtifactRenameUnpublishAndDeleteAreIndistinguishable(t *testing.T) {
	queue := artifactQueueFunc(func(context.Context, renderjob.Request) (renderjob.Result, error) {
		return renderjob.Result{}, errors.New("must not render")
	})
	handlers, backing, _ := newArtifactHarness(t, queue, 2, time.Now)
	request := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/public/resumes/ada-lovelace/og.png", nil)
		req.RemoteAddr = "192.0.2.8:1234"
		response := httptest.NewRecorder()
		handlers.png.ServeHTTP(response, req)
		return response
	}
	original := *backing.row.Slug
	other := "renamed-resume"
	backing.row.Slug = &other
	rename := request()
	backing.row.Slug = &original
	backing.row.Live = false
	unpublish := request()
	backing.row.Live = true
	backing.row.Slug = nil
	deleted := request()
	if rename.Code != http.StatusNotFound || unpublish.Code != http.StatusNotFound || deleted.Code != http.StatusNotFound ||
		rename.Body.String() != unpublish.Body.String() || rename.Body.String() != deleted.Body.String() {
		t.Fatalf("rename=%d %q unpublish=%d %q delete=%d %q", rename.Code, rename.Body.String(), unpublish.Code, unpublish.Body.String(), deleted.Code, deleted.Body.String())
	}
}

func TestPublicArtifactLeaseCancellationStopsQueueStages(t *testing.T) {
	for _, stage := range []string{"prepare", "render"} {
		t.Run(stage, func(t *testing.T) {
			started := make(chan struct{})
			queue := artifactQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
				if stage == "render" {
					if _, err := request.Prepare(ctx); err != nil {
						return renderjob.Result{}, err
					}
				}
				close(started)
				<-ctx.Done()
				return renderjob.Result{}, ctx.Err()
			})
			handlers, backing, coordinator := newArtifactHarness(t, queue, 2, time.Now)
			request := httptest.NewRequest(http.MethodGet, "/api/v1/public/resumes/ada-lovelace/og.png", nil)
			request.RemoteAddr = "192.0.2.9:1234"
			response := httptest.NewRecorder()
			handlerDone := make(chan struct{})
			go func() {
				handlers.png.ServeHTTP(response, request)
				close(handlerDone)
			}()
			<-started
			transition, err := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{{
				ID: backing.row.ID, ExpectedRevision: backing.row.Revision, Class: publicstate.Revoking,
			}}})
			if err != nil {
				t.Fatal(err)
			}
			if err := transition.Close(context.Background(), time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			select {
			case <-handlerDone:
			case <-time.After(time.Second):
				t.Fatal("handler did not join canceled queue work")
			}
			if err := transition.Rollback(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCanceledPublicArtifactResultIsNotCachedOrServed(t *testing.T) {
	started := make(chan struct{})
	queueCalls := 0
	queue := artifactQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
		queueCalls++
		snapshot, err := request.Prepare(ctx)
		if err != nil {
			return renderjob.Result{}, err
		}
		if queueCalls == 1 {
			close(started)
			<-ctx.Done()
			return renderjob.Result{Bytes: []byte("canceled"), Revision: snapshot.Revision}, nil
		}
		return renderjob.Result{Bytes: []byte("current"), Revision: snapshot.Revision}, nil
	})
	handlers, backing, coordinator := newArtifactHarness(t, queue, 2, time.Now)
	request := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/public/resumes/ada-lovelace/pdf", nil)
		r.RemoteAddr = "192.0.2.14:1234"
		return r
	}
	first := httptest.NewRecorder()
	handlerDone := make(chan struct{})
	go func() {
		handlers.pdf.ServeHTTP(first, request())
		close(handlerDone)
	}()
	<-started
	transition, err := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{{
		ID: backing.row.ID, ExpectedRevision: backing.row.Revision, Class: publicstate.Revoking,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := transition.Close(context.Background(), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	<-handlerDone
	assertCanceledArtifactResponse(t, first, "canceled")
	if err := transition.Rollback(); err != nil {
		t.Fatal(err)
	}
	second := httptest.NewRecorder()
	handlers.pdf.ServeHTTP(second, request())
	if second.Code != http.StatusOK || second.Body.String() != "current" || queueCalls != 2 {
		t.Fatalf("second response = %d %q queue calls=%d", second.Code, second.Body.String(), queueCalls)
	}
}

func TestCanceledPublicArtifactCacheHitReturns503WithoutCachedBytes(t *testing.T) {
	fixedNow := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	cacheGetStarted := make(chan struct{})
	resumeCacheGet := make(chan struct{})
	var clockCalls atomic.Int32
	now := func() time.Time {
		if clockCalls.Add(1) == 3 {
			close(cacheGetStarted)
			<-resumeCacheGet
		}
		return fixedNow
	}
	queue := artifactQueueFunc(func(context.Context, renderjob.Request) (renderjob.Result, error) {
		return renderjob.Result{}, errors.New("cache hit must not render")
	})
	handlers, backing, coordinator, cache := newArtifactHarnessWithCache(t, queue, 2, now)
	cached, err := newSelectedResponseWithLimit(
		http.StatusOK,
		"application/pdf",
		"no-cache, must-revalidate",
		[]byte("cached-old-generation"),
		http.Header{"Content-Disposition": {"attachment; filename=\"resume.pdf\""}},
		renderjob.PDFMaxBytes,
	)
	if err != nil {
		t.Fatal(err)
	}
	cache.Put(publiccache.Key{
		RouteClass: "resume", Representation: publicstate.RepresentationPDF, Variant: "default",
		ResumeID: backing.row.ID, Generation: backing.row.Revision, FormatVersion: publicPDFFormatVersion,
		AppDigest: "sha256:app", RendererDigest: "sha256:renderer",
	}, publiccache.Value{Status: cached.Status, Header: cached.Header, Body: cached.Body})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/resumes/ada-lovelace/pdf", nil)
	request.RemoteAddr = "192.0.2.15:1234"
	response := httptest.NewRecorder()
	handlerDone := make(chan struct{})
	go func() {
		handlers.pdf.ServeHTTP(response, request)
		close(handlerDone)
	}()
	<-cacheGetStarted
	transition, err := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{{
		ID: backing.row.ID, ExpectedRevision: backing.row.Revision, Class: publicstate.Revoking,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	closeDone := make(chan error, 1)
	go func() {
		closeDone <- transition.Close(context.Background(), fixedNow.Add(time.Second))
	}()
	for {
		probe, acquireErr := coordinator.AcquireResume(
			context.Background(), backing.row.ID, backing.row.Revision, publicstate.RepresentationPDF,
		)
		if errors.Is(acquireErr, publicstate.ErrAdmissionClosed) {
			break
		}
		if acquireErr != nil {
			t.Fatal(acquireErr)
		}
		probe.Release()
	}
	close(resumeCacheGet)
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("transition did not drain canceled cache hit")
	}
	<-handlerDone
	assertCanceledArtifactResponse(t, response, "cached-old-generation")
	if err := transition.Rollback(); err != nil {
		t.Fatal(err)
	}
}

func assertCanceledArtifactResponse(t *testing.T, response *httptest.ResponseRecorder, forbidden string) {
	t.Helper()
	wantBody := "{\"error\":{\"code\":\"temporarily_unavailable\",\"message\":\"service temporarily unavailable\"}}\n"
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "1" ||
		response.Header().Get("Cache-Control") != "no-cache, must-revalidate" ||
		response.Header().Get("Content-Type") != "application/json; charset=utf-8" ||
		response.Header().Get("Content-Length") != strconv.Itoa(len(wantBody)) ||
		response.Header().Get("ETag") != "" || response.Header().Get("Content-Disposition") != "" ||
		response.Body.String() != wantBody || strings.Contains(response.Body.String(), forbidden) {
		t.Fatalf("canceled artifact response = %d headers=%v body=%q", response.Code, response.Header(), response.Body.String())
	}
}

type deadlineEnforcingArtifactWriter struct {
	header        http.Header
	body          bytes.Buffer
	onFirstHeader func()
	headerOnce    sync.Once
	deadlineOnce  sync.Once
	deadlineSet   chan struct{}
	deadlineCalls atomic.Int32
	expired       atomic.Bool
	status        int
}

func (w *deadlineEnforcingArtifactWriter) Header() http.Header {
	w.headerOnce.Do(w.onFirstHeader)
	return w.header
}

func (w *deadlineEnforcingArtifactWriter) WriteHeader(status int) {
	if status != http.StatusServiceUnavailable {
		<-w.deadlineSet
	}
	if !w.expired.Load() {
		w.status = status
	}
}

func (w *deadlineEnforcingArtifactWriter) Write(body []byte) (int, error) {
	if w.expired.Load() {
		return 0, errors.New("expired write deadline")
	}
	return w.body.Write(body)
}

func (w *deadlineEnforcingArtifactWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadlineCalls.Add(1)
	if !deadline.After(time.Now()) {
		w.expired.Store(true)
	}
	w.deadlineOnce.Do(func() { close(w.deadlineSet) })
	return nil
}

func TestPublicArtifactPrecommitRevocationWrites503BeforeArmingDeadline(t *testing.T) {
	_, _, coordinator, backing := publicServiceReaderWithStore(t)
	lease, err := coordinator.AcquireResume(
		context.Background(), backing.row.ID, backing.row.Revision, publicstate.RepresentationPDF,
	)
	if err != nil {
		t.Fatal(err)
	}
	transition, err := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{{
		ID: backing.row.ID, ExpectedRevision: backing.row.Revision, Class: publicstate.Revoking,
	}}})
	if err != nil {
		lease.Release()
		t.Fatal(err)
	}
	leaseCanceled := make(chan struct{})
	if err := lease.OnCancel(func() { close(leaseCanceled) }); err != nil {
		lease.Release()
		t.Fatal(err)
	}
	closeDone := make(chan error, 1)
	writer := &deadlineEnforcingArtifactWriter{
		header: make(http.Header), deadlineSet: make(chan struct{}),
		onFirstHeader: func() {
			go func() {
				closeDone <- transition.Close(context.Background(), time.Now().Add(time.Second))
			}()
			<-leaseCanceled
		},
	}
	response, err := newSelectedResponseWithLimit(
		http.StatusOK,
		"application/pdf",
		"no-cache, must-revalidate",
		[]byte("revoked-pdf"),
		http.Header{"Content-Disposition": {"attachment; filename=\"resume.pdf\""}},
		renderjob.PDFMaxBytes,
	)
	if err != nil {
		lease.Release()
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/resumes/ada-lovelace/pdf", nil)
	serveLeasedArtifact(writer, request, lease, response)
	lease.Release()
	if err := <-closeDone; err != nil {
		t.Fatal(err)
	}
	if err := transition.Rollback(); err != nil {
		t.Fatal(err)
	}
	wantBody := "{\"error\":{\"code\":\"temporarily_unavailable\",\"message\":\"service temporarily unavailable\"}}\n"
	if writer.status != http.StatusServiceUnavailable || writer.deadlineCalls.Load() != 0 ||
		writer.body.String() != wantBody || writer.header.Get("Retry-After") != "1" {
		t.Fatalf("response = %d deadline calls=%d headers=%v body=%q", writer.status, writer.deadlineCalls.Load(), writer.header, writer.body.String())
	}
}

type deadlineBlockingWriter struct {
	header      http.Header
	status      int
	writeStart  chan struct{}
	deadlineSet chan struct{}
	release     chan struct{}
	once        sync.Once
}

func newDeadlineBlockingWriter() *deadlineBlockingWriter {
	return &deadlineBlockingWriter{
		header: make(http.Header), writeStart: make(chan struct{}),
		deadlineSet: make(chan struct{}), release: make(chan struct{}),
	}
}

func (w *deadlineBlockingWriter) Header() http.Header { return w.header }
func (w *deadlineBlockingWriter) WriteHeader(status int) {
	w.status = status
}
func (w *deadlineBlockingWriter) Write([]byte) (int, error) {
	w.once.Do(func() { close(w.writeStart) })
	select {
	case <-w.deadlineSet:
		return 0, errors.New("write deadline")
	case <-w.release:
		return 0, errors.New("viewer closed")
	}
}
func (w *deadlineBlockingWriter) SetWriteDeadline(time.Time) error {
	select {
	case <-w.deadlineSet:
	default:
		close(w.deadlineSet)
	}
	return nil
}

type unsupportedBlockingWriter struct {
	header     http.Header
	status     int
	writeStart chan struct{}
	release    chan struct{}
}

func (w *unsupportedBlockingWriter) Header() http.Header { return w.header }
func (w *unsupportedBlockingWriter) WriteHeader(status int) {
	w.status = status
}
func (w *unsupportedBlockingWriter) Write([]byte) (int, error) {
	close(w.writeStart)
	<-w.release
	return 0, errors.New("released")
}

func TestPublicArtifactRevocationCancelsAndJoinsBlockedOriginWrite(t *testing.T) {
	queue := artifactQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
		snapshot, err := request.Prepare(ctx)
		return renderjob.Result{Bytes: []byte("png"), Revision: snapshot.Revision}, err
	})
	handlers, backing, coordinator := newArtifactHarness(t, queue, 2, time.Now)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/resumes/ada-lovelace/og.png", nil)
	request.RemoteAddr = "192.0.2.10:1234"
	writer := newDeadlineBlockingWriter()
	handlerDone := make(chan struct{})
	go func() {
		handlers.png.ServeHTTP(writer, request)
		close(handlerDone)
	}()
	<-writer.writeStart
	transition, err := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{{
		ID: backing.row.ID, ExpectedRevision: backing.row.Revision, Class: publicstate.Revoking,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := transition.Close(context.Background(), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("handler did not return after its write deadline callback")
	}
	select {
	case <-writer.deadlineSet:
	default:
		t.Fatal("handler returned before its cancellation callback completed")
	}
}

func TestPublicArtifactUnsupportedWriteCancellationPreservesDrainFailure(t *testing.T) {
	queue := artifactQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
		snapshot, err := request.Prepare(ctx)
		return renderjob.Result{Bytes: []byte("pdf"), Revision: snapshot.Revision}, err
	})
	handlers, backing, coordinator := newArtifactHarness(t, queue, 2, time.Now)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/public/resumes/ada-lovelace/pdf", nil)
	request.RemoteAddr = "192.0.2.11:1234"
	writer := &unsupportedBlockingWriter{header: make(http.Header), writeStart: make(chan struct{}), release: make(chan struct{})}
	handlerDone := make(chan struct{})
	go func() {
		handlers.pdf.ServeHTTP(writer, request)
		close(handlerDone)
	}()
	<-writer.writeStart
	transition, err := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{{
		ID: backing.row.ID, ExpectedRevision: backing.row.Revision, Class: publicstate.Revoking,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := transition.Close(context.Background(), time.Now().Add(25*time.Millisecond)); err == nil {
		t.Fatal("drain succeeded while unsupported socket cancellation left the handler active")
	}
	close(writer.release)
	select {
	case <-handlerDone:
	case <-time.After(time.Second):
		t.Fatal("handler did not finish after the viewer write was released")
	}
}

func TestPublicArtifactRealHTTPSlowAndAbortedViewersReleaseLease(t *testing.T) {
	body := bytes.Repeat([]byte("p"), renderjob.PDFMaxBytes)
	queue := artifactQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
		snapshot, err := request.Prepare(ctx)
		return renderjob.Result{Bytes: body, Revision: snapshot.Revision}, err
	})
	handlers, backing, coordinator := newArtifactHarness(t, queue, 2, time.Now)
	server := httptest.NewServer(handlers.pdf)
	defer server.Close()

	address := strings.TrimPrefix(server.URL, "http://")
	for _, abortFirst := range []bool{false, true} {
		connection, err := net.Dial("tcp", address)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintf(connection, "GET /api/v1/public/resumes/ada-lovelace/pdf HTTP/1.1\r\nHost: test\r\n\r\n"); err != nil {
			t.Fatal(err)
		}
		reader := bufio.NewReader(connection)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			if line == "\r\n" {
				break
			}
		}
		if abortFirst {
			_ = connection.Close()
		}
		transition, err := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{{
			ID: backing.row.ID, ExpectedRevision: backing.row.Revision, Class: publicstate.Revoking,
		}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := transition.Close(context.Background(), time.Now().Add(2*time.Second)); err != nil {
			t.Fatalf("abortFirst=%t: real HTTP viewer did not release lease: %v", abortFirst, err)
		}
		_ = connection.Close()
		if err := transition.Rollback(); err != nil {
			t.Fatal(err)
		}
	}
}
